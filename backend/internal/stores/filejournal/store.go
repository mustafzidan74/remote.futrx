// Package filejournal stores each project's change journal as append-only
// JSONL at <dataDir>/projectjournal/<projectId>.jsonl.
//
// One file per project — rather than the audit log's file per month — because
// a journal is always read for exactly one project, and a project's history is
// small: one line per agent run. Appends use O_APPEND with one write per line,
// so a crash leaves whole lines behind, and reads walk the file backwards
// through a decoded slice so the newest run comes first.
package filejournal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	servicejournal "github.com/futrx-com/remote.futrx.com/internal/service/journal"
)

var _ servicejournal.Store = (*Store)(nil)

const (
	// DirName holds one journal per project inside DATA_DIR.
	DirName      = "projectjournal"
	fileSuffix   = ".jsonl"
	dirMode      = 0o700
	fileMode     = 0o600
	maxLineBytes = 1 << 20
	scanBufBytes = 64 << 10
)

// projectIDPattern guards the path join: a project id reaches this store from
// a URL, and a traversal here would let a caller read or write anywhere under
// DATA_DIR.
var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

var (
	// ErrInvalidProjectID rejects an id that cannot safely name a file.
	ErrInvalidProjectID = errors.New("invalid project id")
	// ErrInvalidCursor is returned for a cursor that did not come from this store.
	ErrInvalidCursor = errors.New("invalid journal cursor")
)

type Store struct {
	root string
	mu   sync.Mutex
}

func New(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, DirName)
	if err := os.MkdirAll(root, dirMode); err != nil {
		return nil, fmt.Errorf("create journal dir: %w", err)
	}
	return &Store{root: root}, nil
}

func (s *Store) path(projectID string) (string, error) {
	if !projectIDPattern.MatchString(projectID) {
		return "", ErrInvalidProjectID
	}
	return filepath.Join(s.root, projectID+fileSuffix), nil
}

func (s *Store) Append(entry servicejournal.Entry) error {
	path, err := s.path(entry.ProjectID)
	if err != nil {
		return err
	}
	if entry.At == 0 {
		entry.At = time.Now().UnixMilli()
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal journal entry: %w", err)
	}
	line = append(line, byte('\n'))

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.root, dirMode); err != nil {
		return fmt.Errorf("create journal dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		return fmt.Errorf("write journal: %w", err)
	}
	return nil
}

// Query returns one page, newest first. The cursor is the line number the next
// page starts at, counted from the end of the file, so it stays valid while
// runs keep appending.
func (s *Store) Query(query servicejournal.Query) (servicejournal.Page, error) {
	entries, err := s.read(query.ProjectID)
	if err != nil {
		return servicejournal.Page{}, err
	}
	skip, err := parseCursor(query.Cursor)
	if err != nil {
		return servicejournal.Page{}, err
	}
	limit := servicejournal.NormalizeLimit(query.Limit)

	page := servicejournal.Page{Entries: make([]servicejournal.Entry, 0, limit)}
	matched := 0
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if !matches(entry, query) {
			continue
		}
		matched++
		if matched <= skip {
			continue
		}
		if len(page.Entries) == limit {
			page.NextCursor = strconv.Itoa(skip + limit)
			break
		}
		page.Entries = append(page.Entries, entry)
	}
	return page, nil
}

// Export renders the whole journal as Markdown, newest run first.
func (s *Store) Export(projectID string) (string, error) {
	entries, err := s.read(projectID)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("# Project change journal\n\n")
	if len(entries) == 0 {
		out.WriteString("No agent runs recorded yet.\n")
		return out.String(), nil
	}
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		at := time.UnixMilli(entry.At).UTC().Format("2006-01-02 15:04 UTC")
		out.WriteString("## " + at)
		if entry.ChatTitle != "" {
			out.WriteString(" — " + entry.ChatTitle)
		}
		out.WriteString("\n\n")
		out.WriteString("- Status: " + entry.Status + "\n")
		if entry.Provider != "" {
			agentLine := entry.Provider
			if entry.Model != "" {
				agentLine += " · " + entry.Model
			}
			out.WriteString("- Agent: " + agentLine + "\n")
		}
		if entry.Request != "" {
			out.WriteString("\n**Requested**\n\n" + quote(entry.Request) + "\n")
		}
		if entry.Summary != "" {
			out.WriteString("\n**Reported**\n\n" + quote(entry.Summary) + "\n")
		}
		if entry.Error != "" {
			out.WriteString("\n**Error**\n\n" + quote(entry.Error) + "\n")
		}
		if len(entry.Files) > 0 {
			out.WriteString("\n**Files**\n\n")
			for _, file := range entry.Files {
				out.WriteString("- `" + file + "`\n")
			}
		}
		out.WriteString("\n")
	}
	return out.String(), nil
}

func quote(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for index, line := range lines {
		lines[index] = "> " + line
	}
	return strings.Join(lines, "\n") + "\n"
}

func (s *Store) read(projectID string) ([]servicejournal.Entry, error) {
	path, err := s.path(projectID)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()

	var entries []servicejournal.Entry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, scanBufBytes), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry servicejournal.Entry
		// A line this store cannot read is one run's record, not the file:
		// skipping it keeps the rest of the history readable.
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read journal: %w", err)
	}
	return entries, nil
}

func matches(entry servicejournal.Entry, query servicejournal.Query) bool {
	if query.From > 0 && entry.At < query.From {
		return false
	}
	if query.To > 0 && entry.At > query.To {
		return false
	}
	return entry.Matches(query.Text)
}

func parseCursor(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(cursor)
	if err != nil || value < 0 {
		return 0, ErrInvalidCursor
	}
	return value, nil
}
