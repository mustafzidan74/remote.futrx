package journal

import (
	"context"
	"errors"
	"strings"
)

// Store is the per-project journal file.
type Store interface {
	Append(entry Entry) error
	Query(query Query) (Page, error)
	// Export writes the project's entries, newest first, as Markdown.
	Export(projectID string) (string, error)
}

// ErrProjectRequired is returned when a call names no project.
var ErrProjectRequired = errors.New("project id is required")

// Service reads and writes project journals.
type Service struct {
	store Store
}

func New(store Store) *Service {
	return &Service{store: store}
}

// Record appends one run. A journal write never fails a run, so callers log
// and move on; the error is returned for that log line.
func (s *Service) Record(_ context.Context, entry Entry) error {
	if s == nil || s.store == nil {
		return nil
	}
	entry.ProjectID = strings.TrimSpace(entry.ProjectID)
	if entry.ProjectID == "" {
		return ErrProjectRequired
	}
	if entry.Status == "" {
		entry.Status = StatusDone
	}
	return s.store.Append(entry)
}

// Query answers one page of a project's journal.
func (s *Service) Query(_ context.Context, query Query) (Page, error) {
	if s == nil || s.store == nil {
		return Page{Entries: []Entry{}}, nil
	}
	query.ProjectID = strings.TrimSpace(query.ProjectID)
	if query.ProjectID == "" {
		return Page{}, ErrProjectRequired
	}
	query.Limit = NormalizeLimit(query.Limit)
	return s.store.Query(query)
}

// Export renders a project's whole journal as Markdown, for download.
func (s *Service) Export(_ context.Context, projectID string) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", ErrProjectRequired
	}
	return s.store.Export(projectID)
}
