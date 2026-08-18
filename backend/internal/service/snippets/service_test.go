package snippets

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// memoryRepo is the in-memory stand-in for the file store. It records every
// save so a test can assert that a read did — or did not — write anything.
type memoryRepo struct {
	documents map[Owner][]Snippet
	saves     int
	loadErr   error
	saveErr   error
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{documents: map[Owner][]Snippet{}}
}

func (r *memoryRepo) Load(_ context.Context, owner Owner) ([]Snippet, bool, error) {
	if r.loadErr != nil {
		return nil, false, r.loadErr
	}
	list, found := r.documents[owner]
	return append([]Snippet(nil), list...), found, nil
}

func (r *memoryRepo) Save(_ context.Context, owner Owner, list []Snippet) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saves++
	r.documents[owner] = append([]Snippet(nil), list...)
	return nil
}

func testService(repo Repository) *Service {
	tick := int64(0)
	return New(repo, WithClock(func() time.Time {
		tick++
		return time.UnixMilli(1_700_000_000_000 + tick)
	}))
}

func TestOwnerFromSession(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		sub     string
		want    Owner
		wantErr error
	}{
		{name: "subject wins", email: "A@Example.com", sub: "google-123", want: "sub:google-123"},
		{name: "email is the local fallback", email: "  Admin@Example.com ", want: "email:admin@example.com"},
		{name: "no identity is refused", wantErr: ErrInvalidOwner},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, err := OwnerFromSession(tt.email, tt.sub)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if owner != tt.want {
				t.Fatalf("owner = %q, want %q", owner, tt.want)
			}
		})
	}
}

func TestListSeedsOnceAndNeverAgain(t *testing.T) {
	repo := newMemoryRepo()
	service := testService(repo)
	ctx := context.Background()

	first, err := service.List(ctx, "sub:one")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(first) != len(Seed(0)) {
		t.Fatalf("seeded %d templates, want %d", len(first), len(Seed(0)))
	}
	if repo.saves != 1 {
		t.Fatalf("saves = %d after the first read, want 1", repo.saves)
	}
	for _, item := range first {
		if item.Audience != AudienceClient {
			t.Fatalf("seeded %q with audience %q, want client", item.ID, item.Audience)
		}
		if strings.TrimSpace(item.Variants.AR) == "" || strings.TrimSpace(item.Variants.EN) == "" {
			t.Fatalf("seeded %q is not bilingual", item.ID)
		}
	}

	if _, err := service.List(ctx, "sub:one"); err != nil {
		t.Fatalf("second List: %v", err)
	}
	if repo.saves != 1 {
		t.Fatalf("saves = %d after a second read, want the seed to be written once", repo.saves)
	}

	// A user who deletes everything keeps an empty library: the document
	// exists, so the seed must not come back.
	for _, item := range first {
		if err := service.Delete(ctx, "sub:one", item.ID); err != nil {
			t.Fatalf("Delete %q: %v", item.ID, err)
		}
	}
	emptied, err := service.List(ctx, "sub:one")
	if err != nil {
		t.Fatalf("List after emptying: %v", err)
	}
	if len(emptied) != 0 {
		t.Fatalf("library re-seeded itself with %d entries", len(emptied))
	}
}

func TestLibrariesAreIsolatedPerOwner(t *testing.T) {
	repo := newMemoryRepo()
	service := testService(repo)
	ctx := context.Background()

	mine, err := service.Create(ctx, "sub:one", Input{Title: "WP fix", Body: "Fix {{project}}", Shortcut: "wpfix"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Create(ctx, "sub:two", Input{Title: "Theirs", Body: "Other"}); err != nil {
		t.Fatalf("Create for the second owner: %v", err)
	}

	theirs, err := service.List(ctx, "sub:two")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, item := range theirs {
		if item.ID == mine.ID && item.Title == mine.Title {
			t.Fatalf("owner two can see owner one's snippet %q", item.ID)
		}
	}

	if _, err := service.Update(ctx, "sub:two", mine.ID, Input{Title: "Hijack", Body: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Update error = %v, want ErrNotFound", err)
	}
	if err := service.Delete(ctx, "sub:two", mine.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Delete error = %v, want ErrNotFound", err)
	}
	if _, err := service.MarkUsed(ctx, "sub:two", mine.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner MarkUsed error = %v, want ErrNotFound", err)
	}

	still, err := service.List(ctx, "sub:one")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if findByID(still, mine.ID) == nil {
		t.Fatal("owner one's snippet disappeared after another owner touched it")
	}
}

func TestCreateUpdateDeleteLifecycle(t *testing.T) {
	repo := newMemoryRepo()
	service := testService(repo)
	ctx := context.Background()
	const owner Owner = "email:me@example.com"

	created, err := service.Create(ctx, owner, Input{
		Title:    "  Deploy checklist  ",
		Body:     "  Deploy {{project}} to staging.  ",
		Tags:     []string{" Deploy ", "deploy", ""},
		Shortcut: "/s-DEPLOY",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "deploy-checklist" {
		t.Fatalf("id = %q, want a slug of the title", created.ID)
	}
	if created.Shortcut != "deploy" {
		t.Fatalf("shortcut = %q, want the bare word", created.Shortcut)
	}
	if len(created.Tags) != 1 || created.Tags[0] != "deploy" {
		t.Fatalf("tags = %v, want one deduplicated tag", created.Tags)
	}
	if created.Audience != AudienceAgent {
		t.Fatalf("audience = %q, want agent by default", created.Audience)
	}
	if created.CreatedAt == 0 || created.UpdatedAt == 0 {
		t.Fatal("timestamps were not stamped")
	}

	twin, err := service.Create(ctx, owner, Input{Title: "Deploy checklist", Body: "again"})
	if err != nil {
		t.Fatalf("Create twin: %v", err)
	}
	if twin.ID == created.ID {
		t.Fatalf("two snippets share the id %q", twin.ID)
	}

	updated, err := service.Update(ctx, owner, created.ID, Input{Title: "Renamed", Body: "New body"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	switch {
	case updated.ID != created.ID:
		t.Fatalf("Update changed the id to %q", updated.ID)
	case updated.CreatedAt != created.CreatedAt:
		t.Fatal("Update rewrote the creation time")
	case updated.UpdatedAt <= created.UpdatedAt:
		t.Fatal("Update did not move the edit time")
	case updated.Title != "Renamed":
		t.Fatalf("title = %q", updated.Title)
	}

	if err := service.Delete(ctx, owner, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := service.Delete(ctx, owner, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete error = %v, want ErrNotFound", err)
	}
}

func TestMarkUsedSortsMostUsedFirst(t *testing.T) {
	repo := newMemoryRepo()
	service := testService(repo)
	ctx := context.Background()
	const owner Owner = "sub:sorter"

	quiet, err := service.Create(ctx, owner, Input{Title: "Quiet", Body: "q"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	busy, err := service.Create(ctx, owner, Input{Title: "Busy", Body: "b"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for range 3 {
		if _, err := service.MarkUsed(ctx, owner, busy.ID); err != nil {
			t.Fatalf("MarkUsed: %v", err)
		}
	}
	used, err := service.MarkUsed(ctx, owner, quiet.ID)
	if err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	if used.Uses != 1 {
		t.Fatalf("uses = %d, want 1", used.Uses)
	}

	list, err := service.List(ctx, owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list[0].ID != busy.ID {
		t.Fatalf("first entry is %q, want the most used %q", list[0].ID, busy.ID)
	}
	if found := findByID(list, busy.ID); found == nil || found.Uses != 3 {
		t.Fatalf("busy snippet = %+v, want 3 uses", found)
	}
	// A use is not an edit: it must not move the snippet's UpdatedAt.
	if found := findByID(list, busy.ID); found != nil && found.UpdatedAt != busy.UpdatedAt {
		t.Fatal("MarkUsed rewrote the edit time")
	}
}

func TestImportMergesWithoutDuplicating(t *testing.T) {
	repo := newMemoryRepo()
	service := testService(repo)
	ctx := context.Background()
	const owner Owner = "sub:importer"

	seeded, err := service.List(ctx, owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Re-importing the very document a user exported changes nothing but the
	// wording of the entries it carries.
	merged, err := service.Import(ctx, owner, seeded, false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(merged) != len(seeded) {
		t.Fatalf("re-import produced %d entries, want %d", len(merged), len(seeded))
	}

	added, err := service.Import(ctx, owner, []Snippet{
		{Title: "Imported", Body: "hello", Shortcut: seeded[0].Shortcut},
		{Title: "", Body: ""},
	}, false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(added) != len(seeded)+1 {
		t.Fatalf("import added %d entries, want exactly one", len(added)-len(seeded))
	}
	imported := findByTitle(added, "Imported")
	if imported == nil {
		t.Fatal("the imported snippet is missing")
	}
	if imported.Shortcut == seeded[0].Shortcut {
		t.Fatalf("import kept a colliding shortcut %q", imported.Shortcut)
	}

	replaced, err := service.Import(ctx, owner, []Snippet{{Title: "Only", Body: "one"}}, true)
	if err != nil {
		t.Fatalf("Import replace: %v", err)
	}
	if len(replaced) != 1 || replaced[0].Title != "Only" {
		t.Fatalf("replace left %d entries, want exactly the imported one", len(replaced))
	}
}

func TestValidationRefusesBrokenLibraries(t *testing.T) {
	tests := []struct {
		name string
		list []Snippet
	}{
		{
			name: "no id",
			list: []Snippet{{Title: "T", Body: "b"}},
		},
		{
			name: "duplicate id",
			list: []Snippet{{ID: "a", Title: "T", Body: "b"}, {ID: "a", Title: "U", Body: "c"}},
		},
		{
			name: "duplicate shortcut",
			list: []Snippet{
				{ID: "a", Title: "T", Body: "b", Shortcut: "x"},
				{ID: "b", Title: "U", Body: "c", Shortcut: "x"},
			},
		},
		{
			name: "no title",
			list: []Snippet{{ID: "a", Body: "b"}},
		},
		{
			name: "no text at all",
			list: []Snippet{{ID: "a", Title: "T"}},
		},
		{
			name: "unreadable shortcut",
			list: []Snippet{{ID: "a", Title: "T", Body: "b", Shortcut: "with space"}},
		},
		{
			name: "body over the cap",
			list: []Snippet{{ID: "a", Title: "T", Body: strings.Repeat("x", maxBodyLength+1)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.list); !errors.Is(err, ErrInvalidSnippet) {
				t.Fatalf("Validate error = %v, want ErrInvalidSnippet", err)
			}
		})
	}

	valid := []Snippet{
		{ID: "a", Title: "T", Body: "b", Shortcut: "x"},
		{ID: "b", Title: "U", Variants: Variants{AR: "مرحبا"}},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("Validate rejected a good library: %v", err)
	}
}

func TestClientTemplateTextFallsBack(t *testing.T) {
	tests := []struct {
		name     string
		snippet  Snippet
		language Language
		want     string
	}{
		{
			name:     "arabic variant",
			snippet:  Snippet{Audience: AudienceClient, Variants: Variants{AR: "مرحبا", EN: "Hello"}},
			language: LanguageArabic,
			want:     "مرحبا",
		},
		{
			name:     "english variant",
			snippet:  Snippet{Audience: AudienceClient, Variants: Variants{AR: "مرحبا", EN: "Hello"}},
			language: LanguageEnglish,
			want:     "Hello",
		},
		{
			name:     "a half-translated template falls back to the other language",
			snippet:  Snippet{Audience: AudienceClient, Variants: Variants{EN: "Hello"}},
			language: LanguageArabic,
			want:     "Hello",
		},
		{
			name:     "a client template with no variants falls back to the body",
			snippet:  Snippet{Audience: AudienceClient, Body: "Plain"},
			language: LanguageArabic,
			want:     "Plain",
		},
		{
			name:     "an agent snippet ignores the language",
			snippet:  Snippet{Audience: AudienceAgent, Body: "Prompt", Variants: Variants{AR: "مرحبا"}},
			language: LanguageArabic,
			want:     "Prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.snippet.Text(tt.language); got != tt.want {
				t.Fatalf("Text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewIDAvoidsReservedRouteWords(t *testing.T) {
	if id := NewID("import", nil); id == "import" {
		t.Fatal("a snippet took the id the import route owns")
	}
	if id := NewID("!!!", nil); id != "snippet" {
		t.Fatalf("untitled snippet id = %q, want a usable fallback", id)
	}
}

func TestMissingOwnerAndStoreAreRefused(t *testing.T) {
	ctx := context.Background()
	if _, err := testService(newMemoryRepo()).List(ctx, "  "); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("error = %v, want ErrInvalidOwner", err)
	}
	if _, err := New(nil).List(ctx, "sub:one"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func findByID(list []Snippet, id string) *Snippet {
	for index := range list {
		if list[index].ID == id {
			return &list[index]
		}
	}
	return nil
}

func findByTitle(list []Snippet, title string) *Snippet {
	for index := range list {
		if list[index].Title == title {
			return &list[index]
		}
	}
	return nil
}
