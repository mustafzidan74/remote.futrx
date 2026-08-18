package fileproject

import (
	"context"
	"errors"
	"testing"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// TestSlugReservedByTheTrash pins the collision rule. A live project's name
// collision is resolved with a numeric suffix, exactly as it always was; a
// collision with a project in the trash is refused, because renaming the
// trashed project would change the container name and preview hostnames it is
// restored under.
func TestSlugReservedByTheTrash(t *testing.T) {
	tests := []struct {
		name     string
		trash    bool
		restore  bool
		wantSlug string
		wantErr  error
	}{
		{name: "live collision still suffixes", wantSlug: "my-project-2"},
		{name: "trashed collision is refused", trash: true, wantErr: serviceproject.ErrSlugInTrash},
		{name: "restoring frees nothing, the original is live again", trash: true, restore: true, wantSlug: "my-project-2"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newTrashTestStore(t)
			first := mustCreate(t, store, "My Project")

			if test.trash {
				if _, err := store.Update(context.Background(), first.ID, func(m *serviceproject.Meta) {
					m.DeletedAt = 1_700_000_000_000
				}); err != nil {
					t.Fatal(err)
				}
			}
			if test.restore {
				if _, err := store.Update(context.Background(), first.ID, func(m *serviceproject.Meta) {
					m.DeletedAt = 0
				}); err != nil {
					t.Fatal(err)
				}
			}

			second, err := store.Create(context.Background(), serviceproject.Meta{
				Name: "My Project", Slug: serviceproject.Slugify("My Project"),
			})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Create() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if second.Slug != test.wantSlug {
				t.Fatalf("second slug = %q, want %q", second.Slug, test.wantSlug)
			}
		})
	}
}

// TestTrashedSlugSurvivesAReopen guards the index rebuild: a restart must
// still know that a slug belongs to the trash.
func TestTrashedSlugSurvivesAReopen(t *testing.T) {
	dataDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store, err := NewWithWorkspaceRoot(dataDir, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	first := mustCreate(t, store, "My Project")
	if _, err := store.Update(context.Background(), first.ID, func(m *serviceproject.Meta) {
		m.DeletedAt = 1_700_000_000_000
	}); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewWithWorkspaceRoot(dataDir, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reopened.Create(context.Background(), serviceproject.Meta{
		Name: "My Project", Slug: serviceproject.Slugify("My Project"),
	})
	if !errors.Is(err, serviceproject.ErrSlugInTrash) {
		t.Fatalf("Create() after reopen = %v, want ErrSlugInTrash", err)
	}

	// The trashed project is still resolvable by slug at the store level; the
	// project service is what hides it from hostname lookups.
	got, err := reopened.GetBySlug(context.Background(), "my-project")
	if err != nil || got.ID != first.ID {
		t.Fatalf("GetBySlug() = %+v, %v", got, err)
	}
}

func newTrashTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewWithWorkspaceRoot(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func mustCreate(t *testing.T, store *Store, name string) serviceproject.Meta {
	t.Helper()
	meta, err := store.Create(context.Background(), serviceproject.Meta{
		Name: name, Slug: serviceproject.Slugify(name),
	})
	if err != nil {
		t.Fatal(err)
	}
	return meta
}
