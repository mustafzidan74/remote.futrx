package project

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type deleteTestRepository struct {
	Repository
	meta    Meta
	deleted bool
}

func (repository *deleteTestRepository) Get(context.Context, ID) (Meta, error) {
	return repository.meta, nil
}

func (repository *deleteTestRepository) Delete(context.Context, ID) error {
	repository.deleted = true
	return nil
}

type deleteTestChatCleanup struct {
	projectID ID
	err       error
}

func (cleanup *deleteTestChatCleanup) DeleteProjectChats(_ context.Context, projectID ID) error {
	cleanup.projectID = projectID
	return cleanup.err
}

// Delete moves a project to the Trash, where its chats must still be there to
// reconnect on restore. They go when the project is purged for good.
func TestPurgeRemovesAssociatedChats(t *testing.T) {
	repository := &deleteTestRepository{meta: Meta{ID: "deadbeef", DeletedAt: 1}}
	chats := &deleteTestChatCleanup{}
	service := New(
		repository,
		ContainerDependencies{},
		nil,
		nil,
		WithChatCleanup(chats),
	)

	if err := service.Purge(context.Background(), repository.meta.ID); err != nil {
		t.Fatal(err)
	}
	if chats.projectID != repository.meta.ID {
		t.Fatalf("chat cleanup project = %q, want %q", chats.projectID, repository.meta.ID)
	}
	if !repository.deleted {
		t.Fatal("project record was not deleted after chat cleanup")
	}
}

func TestPurgePreservesProjectWhenChatCleanupFails(t *testing.T) {
	repository := &deleteTestRepository{meta: Meta{ID: "deadbeef", DeletedAt: 1}}
	wantErr := errors.New("disk full")
	service := New(
		repository,
		ContainerDependencies{},
		nil,
		nil,
		WithChatCleanup(&deleteTestChatCleanup{err: wantErr}),
	)

	err := service.Purge(context.Background(), repository.meta.ID)
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "delete project chats") {
		t.Fatalf("Purge() error = %v, want wrapped chat cleanup error", err)
	}
	if repository.deleted {
		t.Fatal("project record was deleted after chat cleanup failed")
	}
}
