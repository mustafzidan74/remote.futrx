package service

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicejournal "github.com/futrx-com/remote.futrx.com/internal/service/journal"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// JournalDriver writes one dated record per settled run into the project's
// change journal: the request as the user wrote it, what the agent reported,
// and the files and commands its tool calls touched.
//
// It lives in the composition package for the same reason AuxJobDriver does:
// it is the one place allowed to hold the chat repository and another service
// at once. A journal write never blocks or fails a run.
type JournalDriver struct {
	journal *servicejournal.Service
	chats   servicechat.Repository
	// background runs the transcript read off the run goroutine.
	background func(func())
}

var _ prompt.RunObserver = (*JournalDriver)(nil)

func newJournalDriver(journal *servicejournal.Service, chats servicechat.Repository) *JournalDriver {
	return &JournalDriver{
		journal:    journal,
		chats:      chats,
		background: func(work func()) { go work() },
	}
}

// journalTimeout bounds the transcript read and the append that follows it.
const journalTimeout = time.Minute

// Limits on what one entry keeps. A journal is a history, not a second copy of
// the transcript: the chat is one click away for anything longer.
const (
	journalRequestLimit = 2000
	journalSummaryLimit = 2000
	journalCommandLimit = 200
	journalMaxFiles     = 50
	journalMaxCommands  = 20
	journalErrorLimit   = 500
)

// RunSettled is called on the run goroutine, so it returns immediately.
func (d *JournalDriver) RunSettled(_ context.Context, outcome prompt.RunOutcome) {
	if d == nil || d.journal == nil || d.chats == nil {
		return
	}
	d.background(func() { d.record(outcome) })
}

// RunToolStarted is not interesting to this driver; the transcript already
// holds every tool call by the time the run settles.
func (d *JournalDriver) RunToolStarted(context.Context, servicechat.ID, string) {}

func (d *JournalDriver) record(outcome prompt.RunOutcome) {
	ctx, cancel := context.WithTimeout(context.Background(), journalTimeout)
	defer cancel()

	meta, err := d.chats.Get(ctx, outcome.ChatID)
	if err != nil || meta.ProjectID == "" {
		// A chat outside any project has no project history to write to.
		return
	}
	events, err := d.chats.ReadEvents(ctx, outcome.ChatID)
	if err != nil {
		log.Printf("journal: read %s: %v", outcome.ChatID, err)
		return
	}

	turn := lastTurn(events)
	entry := servicejournal.Entry{
		At:         time.Now().UnixMilli(),
		ProjectID:  string(meta.ProjectID),
		ChatID:     string(outcome.ChatID),
		ChatTitle:  strings.TrimSpace(meta.Title),
		Provider:   string(meta.Provider),
		Model:      strings.TrimSpace(meta.Model),
		Request:    clip(turn.request, journalRequestLimit),
		Summary:    clip(strings.TrimSpace(outcome.Output), journalSummaryLimit),
		Files:      turn.files,
		Commands:   turn.commands,
		Status:     servicejournal.StatusDone,
		Scheduled:  outcome.ScheduledTaskID != "",
		Synthetic:  outcome.Synthetic,
		DurationMs: turn.durationMs,
	}
	if turn.at > 0 {
		entry.At = turn.at
	}
	switch {
	case outcome.Cancelled:
		entry.Status = servicejournal.StatusCancelled
	case outcome.Err != nil:
		entry.Status = servicejournal.StatusFailed
		entry.Error = clip(outcome.Err.Error(), journalErrorLimit)
	}
	// A run that neither asked for anything nor did anything is bookkeeping,
	// not history.
	if entry.Request == "" && entry.Summary == "" && len(entry.Files) == 0 && entry.Error == "" {
		return
	}
	if err := d.journal.Record(ctx, entry); err != nil {
		log.Printf("journal: append for project %s: %v", entry.ProjectID, err)
	}
}

// turnRecord is what the last turn of a transcript says about itself.
type turnRecord struct {
	request    string
	at         int64
	durationMs int64
	files      []string
	commands   []string
}

// lastTurn reads the transcript backwards to the user message that started the
// final turn, collecting the tool calls in between.
func lastTurn(events []servicechat.Event) turnRecord {
	start := 0
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type == "user" {
			start = index
			break
		}
	}
	if len(events) == 0 {
		return turnRecord{}
	}
	turn := turnRecord{
		request: strings.TrimSpace(events[start].Text),
		at:      events[start].T,
	}
	if last := events[len(events)-1].T; turn.at > 0 && last > turn.at {
		turn.durationMs = last - turn.at
	}
	seenFile := map[string]bool{}
	seenCommand := map[string]bool{}
	for _, event := range events[start:] {
		if event.Type != "tool_use_start" {
			continue
		}
		if path := toolFilePath(event.Name, event.Input); path != "" && !seenFile[path] {
			seenFile[path] = true
			if len(turn.files) < journalMaxFiles {
				turn.files = append(turn.files, path)
			}
		}
		if command := toolCommand(event.Name, event.Input); command != "" && !seenCommand[command] {
			seenCommand[command] = true
			if len(turn.commands) < journalMaxCommands {
				turn.commands = append(turn.commands, command)
			}
		}
	}
	return turn
}

// writingTools are the tool names, across providers, whose call means a file
// was created or changed. Read-only tools are deliberately absent: a journal
// records what changed, not what was looked at.
var writingTools = map[string]bool{
	"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true,
	"Patch": true, "apply_patch": true, "file_change": true,
	"write_to_file": true, "replace_file_content": true, "multi_replace_file_content": true,
	"sed_file": true, "str_replace_editor": true,
}

var shellTools = map[string]bool{
	"Bash": true, "shell": true, "run_command": true, "command_execution": true, "PowerShell": true,
}

// toolFilePath pulls the path out of a writing tool's input, whichever of the
// provider-specific key names it used.
func toolFilePath(name string, input json.RawMessage) string {
	if !writingTools[name] {
		return ""
	}
	fields := decodeToolInput(input)
	for _, key := range []string{"file_path", "path", "TargetFile", "filePath", "file"} {
		if value := strings.TrimSpace(stringField(fields[key])); value != "" {
			return workspaceRelative(value)
		}
	}
	return ""
}

// toolCommand pulls the command line out of a shell tool's input, first line
// only: the rest is usually a heredoc of the file it just wrote.
func toolCommand(name string, input json.RawMessage) string {
	if !shellTools[name] {
		return ""
	}
	fields := decodeToolInput(input)
	for _, key := range []string{"command", "CommandLine", "cmd"} {
		value := strings.TrimSpace(stringField(fields[key]))
		if value == "" {
			continue
		}
		if line, _, found := strings.Cut(value, "\n"); found {
			value = strings.TrimSpace(line) + " …"
		}
		return clip(value, journalCommandLimit)
	}
	return ""
}

func decodeToolInput(input json.RawMessage) map[string]any {
	if len(input) == 0 {
		return nil
	}
	var fields map[string]any
	if json.Unmarshal(input, &fields) != nil {
		return nil
	}
	return fields
}

func stringField(value any) string {
	text, _ := value.(string)
	return text
}

// workspaceRelative shortens the paths agents use inside the container, so a
// journal reads like a repository history rather than like a filesystem.
func workspaceRelative(path string) string {
	trimmed := strings.TrimPrefix(path, "/workspace/")
	if trimmed == path {
		trimmed = strings.TrimPrefix(path, "./")
	}
	return strings.TrimSpace(trimmed)
}

func clip(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// journalRunObserver keeps a nil driver out of the observer list, the way
// auxRunObserver does for the auxiliary jobs.
func journalRunObserver(driver *JournalDriver) prompt.RunObserver {
	if driver == nil {
		return nil
	}
	return driver
}
