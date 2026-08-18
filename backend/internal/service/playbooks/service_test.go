package playbooks

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type memoryRepository struct {
	list   []Playbook
	found  bool
	loads  int
	saves  int
	saveEr error
	loadEr error
}

func (r *memoryRepository) Load(context.Context) ([]Playbook, bool, error) {
	r.loads++
	if r.loadEr != nil {
		return nil, false, r.loadEr
	}
	return append([]Playbook(nil), r.list...), r.found, nil
}

func (r *memoryRepository) Save(_ context.Context, list []Playbook) error {
	r.saves++
	if r.saveEr != nil {
		return r.saveEr
	}
	r.list = append([]Playbook(nil), list...)
	r.found = true
	return nil
}

func TestSeedIsIdempotent(t *testing.T) {
	tests := []struct {
		name        string
		repo        *memoryRepository
		wantSeeded  int
		wantSaves   int
		wantEntries int
	}{
		{
			name:        "first run writes the built-in library",
			repo:        &memoryRepository{},
			wantSeeded:  len(Seed()),
			wantSaves:   1,
			wantEntries: len(Seed()),
		},
		{
			name: "an existing document is never rewritten",
			repo: &memoryRepository{
				found: true,
				list:  []Playbook{{ID: "mine", Title: "Mine", Prompt: "Do the thing", Order: 0}},
			},
			wantSeeded:  0,
			wantSaves:   0,
			wantEntries: 1,
		},
		{
			name:        "a deliberately emptied library stays empty",
			repo:        &memoryRepository{found: true, list: []Playbook{}},
			wantSeeded:  0,
			wantSaves:   0,
			wantEntries: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := New(tt.repo)
			ctx := context.Background()

			seeded, err := service.Ensure(ctx)
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			if seeded != tt.wantSeeded {
				t.Fatalf("Ensure seeded %d, want %d", seeded, tt.wantSeeded)
			}
			if tt.repo.saves != tt.wantSaves {
				t.Fatalf("Ensure saved %d times, want %d", tt.repo.saves, tt.wantSaves)
			}

			again, err := service.Ensure(ctx)
			if err != nil {
				t.Fatalf("second Ensure: %v", err)
			}
			if again != 0 {
				t.Fatalf("second Ensure seeded %d, want 0", again)
			}
			if tt.repo.saves != tt.wantSaves {
				t.Fatalf("second Ensure saved again (%d writes)", tt.repo.saves)
			}

			list, err := service.List(ctx)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(list) != tt.wantEntries {
				t.Fatalf("List returned %d entries, want %d", len(list), tt.wantEntries)
			}
		})
	}
}

func TestSeedIsValidAndOrdered(t *testing.T) {
	seed := Seed()
	if len(seed) != 7 {
		t.Fatalf("Seed returned %d playbooks, want 7", len(seed))
	}
	if err := Validate(seed); err != nil {
		t.Fatalf("Seed does not pass Validate: %v", err)
	}
	for i, item := range seed {
		if item.Order != i {
			t.Fatalf("seed[%d] order = %d, want %d", i, item.Order, i)
		}
		if item.Icon == "" || item.Hint == "" {
			t.Fatalf("seed %q needs an icon and a hint for the composer menu", item.ID)
		}
	}
}

func TestListSeedsLazilyWithoutPersistingTwice(t *testing.T) {
	repo := &memoryRepository{}
	service := New(repo)

	list, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != len(Seed()) {
		t.Fatalf("List returned %d entries before Ensure, want the seed", len(list))
	}
	if repo.saves != 0 {
		t.Fatal("List must not write the library; Ensure owns that")
	}
}

func TestReplaceValidatesAndRenumbers(t *testing.T) {
	tests := []struct {
		name      string
		input     []Playbook
		wantErr   bool
		wantIDs   []string
		wantSkill int
	}{
		{
			name: "sorts by order and reindexes",
			input: []Playbook{
				{ID: "second", Title: "Second", Prompt: "p", Order: 9},
				{ID: "first", Title: "First", Prompt: "p", Order: 2},
			},
			wantIDs: []string{"first", "second"},
		},
		{
			name: "drops duplicate skill refs and fills command from name",
			input: []Playbook{{
				ID: "one", Title: "One", Prompt: "p",
				Skills: []SkillRef{
					{Name: "wp-guard", Source: "global"},
					{Command: "wp-guard", Source: "global"},
					{Name: "test-guard", Source: "global"},
				},
			}},
			wantIDs:   []string{"one"},
			wantSkill: 2,
		},
		{
			name:    "rejects a missing title",
			input:   []Playbook{{ID: "one", Prompt: "p"}},
			wantErr: true,
		},
		{
			name:    "rejects a missing prompt",
			input:   []Playbook{{ID: "one", Title: "One"}},
			wantErr: true,
		},
		{
			name:    "rejects an id with spaces",
			input:   []Playbook{{ID: "not an id", Title: "One", Prompt: "p"}},
			wantErr: true,
		},
		{
			name: "rejects duplicate ids",
			input: []Playbook{
				{ID: "one", Title: "One", Prompt: "p"},
				{ID: "one", Title: "Other", Prompt: "p"},
			},
			wantErr: true,
		},
		{
			name:    "rejects an unknown mode",
			input:   []Playbook{{ID: "one", Title: "One", Prompt: "p", Mode: "yolo"}},
			wantErr: true,
		},
		{
			name:    "rejects an unknown provider",
			input:   []Playbook{{ID: "one", Title: "One", Prompt: "p", Provider: "gpt"}},
			wantErr: true,
		},
		{
			name:    "rejects an oversized library",
			input:   oversizedLibrary(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &memoryRepository{found: true}
			service := New(repo)

			stored, err := service.Replace(context.Background(), tt.input, "admin@example.com")
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidPlaybooks) {
					t.Fatalf("Replace error = %v, want ErrInvalidPlaybooks", err)
				}
				if repo.saves != 0 {
					t.Fatal("a rejected library must not be persisted")
				}
				return
			}
			if err != nil {
				t.Fatalf("Replace: %v", err)
			}
			if len(stored) != len(tt.wantIDs) {
				t.Fatalf("Replace stored %d entries, want %d", len(stored), len(tt.wantIDs))
			}
			for i, want := range tt.wantIDs {
				if stored[i].ID != want {
					t.Fatalf("entry %d = %q, want %q", i, stored[i].ID, want)
				}
				if stored[i].Order != i {
					t.Fatalf("entry %d order = %d, want %d", i, stored[i].Order, i)
				}
			}
			if tt.wantSkill > 0 && len(stored[0].Skills) != tt.wantSkill {
				t.Fatalf("entry kept %d skills, want %d", len(stored[0].Skills), tt.wantSkill)
			}
		})
	}
}

func TestReplaceNormalizesCaseAndWhitespace(t *testing.T) {
	repo := &memoryRepository{found: true}
	service := New(repo)

	stored, err := service.Replace(context.Background(), []Playbook{{
		ID:       "  Ship-IT  ",
		Title:    "  Ship it  ",
		Prompt:   "  do the thing  ",
		Mode:     " Review ",
		Provider: " CLAUDE ",
		Skills:   []SkillRef{{Name: " wp-guard ", Provider: " Claude ", Source: " global "}},
	}}, "")
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got := stored[0]
	if got.ID != "ship-it" || got.Title != "Ship it" || got.Prompt != "do the thing" {
		t.Fatalf("normalized entry = %#v", got)
	}
	if got.Mode != "review" || got.Provider != "claude" {
		t.Fatalf("mode/provider = %q/%q, want review/claude", got.Mode, got.Provider)
	}
	skill := got.Skills[0]
	if skill.Name != "wp-guard" || skill.Command != "wp-guard" || skill.Provider != "claude" || skill.Source != "global" {
		t.Fatalf("normalized skill = %#v", skill)
	}
}

func TestListSurfacesRepositoryFailure(t *testing.T) {
	service := New(&memoryRepository{loadEr: errors.New("disk gone")})
	if _, err := service.List(context.Background()); err == nil {
		t.Fatal("List must surface a repository failure")
	}
}

func oversizedLibrary() []Playbook {
	list := make([]Playbook, 0, MaxPlaybooks+1)
	for i := 0; i <= MaxPlaybooks; i++ {
		list = append(list, Playbook{
			ID:     "pb-" + strings.Repeat("x", i%5) + itoa(i),
			Title:  "Entry",
			Prompt: "p",
			Order:  i,
		})
	}
	return list
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
