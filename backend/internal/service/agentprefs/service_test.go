package agentprefs

import (
	"context"
	"strings"
	"testing"
)

type memoryRepo struct {
	prefs Preferences
	found bool
}

func (m *memoryRepo) Load(context.Context) (Preferences, bool, error) {
	return m.prefs, m.found, nil
}

func (m *memoryRepo) Save(_ context.Context, prefs Preferences) error {
	m.prefs, m.found = prefs, true
	return nil
}

type stubOverrides map[string]string

func (s stubOverrides) ReplyLanguage(_ context.Context, identity Identity) string {
	if language, ok := s["sub:"+identity.Sub]; ok && identity.Sub != "" {
		return language
	}
	return s["email:"+identity.Email]
}

type stubProjects map[string]int64

func (s stubProjects) CreatedAt(_ context.Context, projectID string) (int64, bool) {
	createdAt, ok := s[projectID]
	return createdAt, ok
}

func TestUpdateValidatesAndStamps(t *testing.T) {
	repo := &memoryRepo{}
	service := New(repo)
	language := "ar-EG"
	tone := ToneConcise

	got, err := service.Update(
		context.Background(),
		UpdateInput{ReplyLanguage: &language, Tone: &tone},
		"Admin@Example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReplyLanguage != LanguageEgyptianArabic || got.Tone != ToneConcise {
		t.Fatalf("Update() = %+v", got)
	}
	if got.UpdatedBy != "admin@example.com" {
		t.Errorf("UpdatedBy = %q, want the lower-cased actor", got.UpdatedBy)
	}
	if got.UpdatedAt == 0 {
		t.Error("UpdatedAt was not stamped")
	}
	if got.ApplyTo != ApplyToAll {
		t.Errorf("ApplyTo = %q, want the default to survive a partial edit", got.ApplyTo)
	}

	oversized := strings.Repeat("x", MaxExtraInstructionsLength+1)
	if _, err := service.Update(
		context.Background(),
		UpdateInput{ExtraInstructions: &oversized},
		"admin@example.com",
	); err == nil {
		t.Fatal("Update() accepted extra instructions above the cap")
	}
	if repo.prefs.ExtraInstructions != "" {
		t.Error("a rejected edit was persisted anyway")
	}
}

func TestRunPreambleResolution(t *testing.T) {
	platform := Preferences{
		ReplyLanguage: LanguageEnglish,
		Tone:          ToneConcise,
		ApplyTo:       ApplyToAll,
		UpdatedAt:     1_000,
	}

	tests := []struct {
		name      string
		prefs     Preferences
		overrides stubOverrides
		projects  stubProjects
		identity  Identity
		projectID string
		want      string
		wantEmpty bool
	}{
		{
			name:      "unconfigured deployment injects nothing",
			prefs:     Defaults(),
			wantEmpty: true,
		},
		{
			name:  "platform language applies with no override",
			prefs: platform,
			want:  "Reply in English",
		},
		{
			name:      "a subject-keyed override wins",
			prefs:     platform,
			overrides: stubOverrides{"sub:google-1": "ar-EG"},
			identity:  Identity{Email: "user@example.com", Sub: "google-1"},
			want:      "Egyptian Arabic",
		},
		{
			name:      "an email-keyed override wins when there is no subject",
			prefs:     platform,
			overrides: stubOverrides{"email:admin@example.com": "ar"},
			identity:  Identity{Email: "admin@example.com"},
			want:      "Modern Standard Arabic",
		},
		{
			name:      "an auto override falls back to the platform value",
			prefs:     platform,
			overrides: stubOverrides{"email:admin@example.com": "auto"},
			identity:  Identity{Email: "admin@example.com"},
			want:      "Reply in English",
		},
		{
			name: "newProjectsOnly skips a project older than the setting",
			prefs: Preferences{
				ReplyLanguage: LanguageEnglish,
				Tone:          ToneDefault,
				ApplyTo:       ApplyToNewProjects,
				UpdatedAt:     1_000,
			},
			projects:  stubProjects{"old": 500},
			projectID: "old",
			wantEmpty: true,
		},
		{
			name: "newProjectsOnly covers a project created after the setting",
			prefs: Preferences{
				ReplyLanguage: LanguageEnglish,
				Tone:          ToneDefault,
				ApplyTo:       ApplyToNewProjects,
				UpdatedAt:     1_000,
			},
			projects:  stubProjects{"new": 2_000},
			projectID: "new",
			want:      "Reply in English",
		},
		{
			name: "newProjectsOnly excludes loose chats",
			prefs: Preferences{
				ReplyLanguage: LanguageEnglish,
				Tone:          ToneDefault,
				ApplyTo:       ApplyToNewProjects,
				UpdatedAt:     1_000,
			},
			wantEmpty: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := []Option{}
			if test.overrides != nil {
				options = append(options, WithUserOverrides(test.overrides))
			}
			if test.projects != nil {
				options = append(options, WithProjects(test.projects))
			}
			service := New(&memoryRepo{prefs: test.prefs, found: true}, options...)

			got := service.RunPreamble(context.Background(), test.identity, test.projectID)
			if test.wantEmpty {
				if got != "" {
					t.Fatalf("RunPreamble() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, test.want) {
				t.Fatalf("RunPreamble() = %q, want it to contain %q", got, test.want)
			}
		})
	}
}

func TestWorkspaceBlockIgnoresPersonalOverrides(t *testing.T) {
	service := New(
		&memoryRepo{
			prefs: Preferences{ReplyLanguage: LanguageEnglish, Tone: ToneDefault, ApplyTo: ApplyToAll},
			found: true,
		},
		WithUserOverrides(stubOverrides{"email:someone@example.com": "ar-EG"}),
	)

	block, err := service.WorkspaceBlock(context.Background(), "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "Reply in English") {
		t.Fatalf("WorkspaceBlock() = %q, want the platform language", block)
	}
	if strings.Contains(block, "Egyptian") {
		t.Error("WorkspaceBlock() leaked one user's personal override into a shared file")
	}
}

func TestGetWithoutRepositoryFallsBackToDefaults(t *testing.T) {
	var service *Service
	prefs, err := service.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prefs != Defaults() {
		t.Fatalf("Get() = %+v, want defaults", prefs)
	}
	if got := service.RunPreamble(context.Background(), Identity{}, ""); got != "" {
		t.Fatalf("RunPreamble() = %q, want empty", got)
	}
}
