package fileschedule

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
)

var _ serviceschedule.HistoryRepository = (*HistoryStore)(nil)

// HistoryStore keeps one append-only JSONL file per task under
// <data-dir>/scheduled-tasks/history/. Each append trims the file back to the
// newest serviceschedule.MaxHistoryRuns lines, so the log is bounded without a
// background sweeper and without ever loading an unbounded file.
type HistoryStore struct {
	dir string
	mu  sync.Mutex
}

// NewHistory returns the run-history store rooted in the same directory as the
// task catalog.
func NewHistory(dataDir string) (*HistoryStore, error) {
	dir := filepath.Join(dataDir, "scheduled-tasks", "history")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create scheduled task history directory: %w", err)
	}
	return &HistoryStore{dir: dir}, nil
}

func (s *HistoryStore) Append(
	ctx context.Context,
	taskID serviceschedule.ID,
	record serviceschedule.RunRecord,
) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	path, err := s.pathFor(taskID)
	if err != nil {
		return err
	}
	record.TaskID = taskID

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := readRecords(path)
	if err != nil {
		return err
	}
	existing = append(existing, record)
	if len(existing) > serviceschedule.MaxHistoryRuns {
		existing = existing[len(existing)-serviceschedule.MaxHistoryRuns:]
	}
	return writeRecords(path, existing)
}

// List returns the recorded runs oldest first, which is the order they were
// appended in.
func (s *HistoryStore) List(
	ctx context.Context,
	taskID serviceschedule.ID,
) ([]serviceschedule.RunRecord, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	path, err := s.pathFor(taskID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return readRecords(path)
}

func (s *HistoryStore) Delete(ctx context.Context, taskID serviceschedule.ID) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	path, err := s.pathFor(taskID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete scheduled task history: %w", err)
	}
	return nil
}

// pathFor rejects anything that is not a well-formed task id, so a caller can
// never steer this store outside its own directory.
func (s *HistoryStore) pathFor(taskID serviceschedule.ID) (string, error) {
	if !serviceschedule.ValidID(taskID) {
		return "", serviceschedule.ErrInvalidID
	}
	return filepath.Join(s.dir, string(taskID)+".jsonl"), nil
}

func readRecords(path string) ([]serviceschedule.RunRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []serviceschedule.RunRecord{}, nil
		}
		return nil, fmt.Errorf("read scheduled task history: %w", err)
	}
	defer file.Close()

	records := make([]serviceschedule.RunRecord, 0, serviceschedule.MaxHistoryRuns)
	scanner := bufio.NewScanner(file)
	// One record holds a 500-character summary plus a diff stat; a megabyte is
	// far beyond that and still bounds a corrupted file.
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record serviceschedule.RunRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			// A truncated tail (a crash mid-append) must not make the whole
			// history unreadable: skip the damaged line and keep going.
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read scheduled task history: %w", err)
	}
	return records, nil
}

func writeRecords(path string, records []serviceschedule.RunRecord) error {
	var buffer strings.Builder
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("encode scheduled task run: %w", err)
		}
		buffer.Write(encoded)
		buffer.WriteByte('\n')
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(buffer.String()), 0o600); err != nil {
		return fmt.Errorf("write scheduled task history: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace scheduled task history: %w", err)
	}
	return nil
}
