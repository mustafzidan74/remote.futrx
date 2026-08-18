package portal

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	servicegithistory "github.com/futrx-com/remote.futrx.com/internal/service/githistory"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceshare "github.com/futrx-com/remote.futrx.com/internal/service/share"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
)

const testProjectID = serviceproject.ID("9f2a1c04")

type memoryRepo struct {
	mu      sync.Mutex
	records map[serviceproject.ID]Portal
	saveErr error
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{records: map[serviceproject.ID]Portal{}}
}

func (r *memoryRepo) Get(_ context.Context, id serviceproject.ID) (Portal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.records[id], nil
}

func (r *memoryRepo) Save(_ context.Context, id serviceproject.ID, record Portal) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[id] = record
	return nil
}

type stubProjects struct {
	meta serviceproject.Meta
	err  error
}

func (p stubProjects) Get(context.Context, serviceproject.ID) (serviceproject.Meta, error) {
	return p.meta, p.err
}

type stubShares struct {
	shares []serviceshare.Share
	err    error
}

func (s stubShares) List(context.Context, serviceproject.ID) ([]serviceshare.Share, error) {
	return s.shares, s.err
}

type stubHistory struct {
	commits []servicegithistory.Commit
	err     error
}

func (h stubHistory) Commits(
	context.Context, string, string, int,
) (servicegithistory.Commits, error) {
	if h.err != nil {
		return servicegithistory.Commits{}, h.err
	}
	return servicegithistory.Commits{Commits: h.commits}, nil
}

type stubUsage struct {
	runs int64
	err  error
}

func (u stubUsage) ProjectSummary(
	context.Context, string, int64, int64, string, bool,
) (serviceusage.Summary, error) {
	if u.err != nil {
		return serviceusage.Summary{}, u.err
	}
	return serviceusage.Summary{Totals: serviceusage.Totals{Runs: u.runs}}, nil
}

type recordingAudit struct {
	mu      sync.Mutex
	entries []serviceaudit.Entry
}

func (a *recordingAudit) Record(_ context.Context, entry serviceaudit.Entry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, entry)
}

func (a *recordingAudit) actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.entries))
	for _, entry := range a.entries {
		out = append(out, entry.Action)
	}
	return out
}

func runningProject() serviceproject.Meta {
	return serviceproject.Meta{
		ID:     testProjectID,
		Name:   "Acme Shop",
		Slug:   "acme-shop",
		Cwd:    "/var/lib/remote/projects/acme-shop/workspace",
		Status: serviceproject.StatusRunning,
	}
}

func newTestService(t *testing.T, repo Repository, options ...Option) *Service {
	t.Helper()
	base := []Option{
		WithClock(func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) }),
	}
	return New(repo, stubProjects{meta: runningProject()}, "https://remote.example.com",
		append(base, options...)...)
}

func enable(t *testing.T, service *Service, input UpdateInput) Settings {
	t.Helper()
	settings, err := service.Save(context.Background(), testProjectID, input)
	if err != nil {
		t.Fatalf("Save() = %v", err)
	}
	return settings
}

func tokenFrom(t *testing.T, settings Settings) string {
	t.Helper()
	if settings.URL == "" {
		t.Fatal("expected a one-time URL")
	}
	_, token, found := strings.Cut(settings.URL, "?t=")
	if !found || token == "" {
		t.Fatalf("URL %q carries no token", settings.URL)
	}
	return token
}

func TestEnableReturnsTheURLExactlyOnce(t *testing.T) {
	service := newTestService(t, newMemoryRepo())

	created := enable(t, service, UpdateInput{Enabled: true, ShowPreview: true, ShowChangelog: true})
	if !strings.HasPrefix(created.URL, "https://remote.example.com/portal/9f2a1c04?t=") {
		t.Fatalf("url = %q", created.URL)
	}

	// A plain read never carries it, and neither does a save that only edits
	// the settings.
	read, err := service.Get(context.Background(), testProjectID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if read.URL != "" {
		t.Fatalf("Get() leaked the URL: %q", read.URL)
	}
	edited := enable(t, service, UpdateInput{Enabled: true, ShowPreview: true, Note: "hello"})
	if edited.URL != "" {
		t.Fatalf("an ordinary save re-issued the URL: %q", edited.URL)
	}
}

func TestViewAuthorizesOnlyTheCurrentToken(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(t, repo)
	created := enable(t, service, UpdateInput{Enabled: true, ShowPreview: true, ShowChangelog: true})
	token := tokenFrom(t, created)

	if _, err := service.View(context.Background(), testProjectID, token, "1.2.3.4"); err != nil {
		t.Fatalf("View() with the issued token = %v", err)
	}

	tests := []struct {
		name  string
		id    serviceproject.ID
		token string
	}{
		{name: "wrong token", id: testProjectID, token: "not-the-token"},
		{name: "empty token", id: testProjectID, token: ""},
		{name: "token of another portal", id: testProjectID, token: strings.ToUpper(token)},
		{name: "unknown project id", id: serviceproject.ID("deadbeef"), token: token},
		{name: "malformed project id", id: serviceproject.ID("../../etc"), token: token},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.View(context.Background(), test.id, test.token, "")
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("View() = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestRotateInvalidatesThePreviousLink(t *testing.T) {
	service := newTestService(t, newMemoryRepo())
	first := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true, ShowPreview: true}))

	rotated := enable(t, service, UpdateInput{Enabled: true, Rotate: true, ShowPreview: true})
	second := tokenFrom(t, rotated)
	if second == first {
		t.Fatal("rotate reissued the same token")
	}
	if _, err := service.View(context.Background(), testProjectID, first, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old token still works: %v", err)
	}
	if _, err := service.View(context.Background(), testProjectID, second, ""); err != nil {
		t.Fatalf("the new token does not work: %v", err)
	}
}

func TestDisableClosesThePortalAndDropsTheToken(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(t, repo)
	token := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true, ShowPreview: true}))

	enable(t, service, UpdateInput{Enabled: false, ShowPreview: true})

	if _, err := service.View(context.Background(), testProjectID, token, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a disabled portal still served: %v", err)
	}
	stored, _ := repo.Get(context.Background(), testProjectID)
	if stored.TokenHash != "" {
		t.Fatal("disabling must drop the stored token digest")
	}

	// Re-enabling mints a fresh link rather than resurrecting the old one.
	revived := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true, ShowPreview: true}))
	if revived == token {
		t.Fatal("re-enabling reissued the original token")
	}
}

func TestViewRateLimitsRepeatedAttemptsFromOneClient(t *testing.T) {
	service := newTestService(t, newMemoryRepo())
	enable(t, service, UpdateInput{Enabled: true})

	limited := false
	for attempt := 0; attempt < limiterBudget+5; attempt++ {
		_, err := service.View(context.Background(), testProjectID, "guess", "9.9.9.9")
		if errors.Is(err, ErrRateLimited) {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatalf("token guessing was never throttled within %d attempts", limiterBudget+5)
	}
	// A different client is unaffected by the first one's budget.
	if _, err := service.View(context.Background(), testProjectID, "guess", "8.8.8.8"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a second client was throttled by the first: %v", err)
	}
}

func TestPreviewsOnlyListPortsWithALivePublicShare(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	shares := stubShares{shares: []serviceshare.Share{
		{ID: "a", Port: 3000, ExpiresAt: now.Add(time.Hour).UnixMilli()},
		{ID: "b", Port: 3000, ExpiresAt: now.Add(2 * time.Hour).UnixMilli()},
		{ID: "c", Port: 5173, ExpiresAt: now.Add(time.Hour).UnixMilli()},
	}}
	service := newTestService(t, newMemoryRepo(), WithShares(shares))
	token := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true, ShowPreview: true}))

	page, err := service.View(context.Background(), testProjectID, token, "")
	if err != nil {
		t.Fatalf("View() = %v", err)
	}
	if len(page.Previews) != 2 {
		t.Fatalf("previews = %+v, want one per distinct port", page.Previews)
	}
	want := []string{
		"https://acme-shop--3000.dev.remote.example.com/",
		"https://acme-shop--5173.dev.remote.example.com/",
	}
	for index, link := range page.Previews {
		if link.URL != want[index] {
			t.Fatalf("preview %d = %q, want %q", index, link.URL, want[index])
		}
	}
}

func TestPreviewsAreOmittedWithoutALiveShare(t *testing.T) {
	tests := []struct {
		name    string
		options []Option
	}{
		{name: "no share service wired up"},
		{name: "share service reports nothing", options: []Option{WithShares(stubShares{})}},
		{
			name:    "share service fails",
			options: []Option{WithShares(stubShares{err: errors.New("store down")})},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t, newMemoryRepo(), test.options...)
			token := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true, ShowPreview: true}))

			page, err := service.View(context.Background(), testProjectID, token, "")
			if err != nil {
				t.Fatalf("View() = %v", err)
			}
			if len(page.Previews) != 0 {
				t.Fatalf("previews = %+v, want none", page.Previews)
			}
			if page.PreviewsNote == "" {
				t.Fatal("expected a friendly note in place of the links")
			}
		})
	}
}

func TestPreviewSectionIsSkippedWhenTheToggleIsOff(t *testing.T) {
	shares := stubShares{shares: []serviceshare.Share{
		{ID: "a", Port: 3000, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()},
	}}
	service := newTestService(t, newMemoryRepo(), WithShares(shares))
	token := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true, ShowPreview: false}))

	page, err := service.View(context.Background(), testProjectID, token, "")
	if err != nil {
		t.Fatalf("View() = %v", err)
	}
	if len(page.Previews) != 0 || page.PreviewsNote != "" {
		t.Fatalf("preview section leaked with the toggle off: %+v / %q", page.Previews, page.PreviewsNote)
	}
}

func TestChangelogGroupsByDayAndDropsAuthorEmails(t *testing.T) {
	day := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	history := stubHistory{commits: []servicegithistory.Commit{
		{
			SHA: "aaa", ShortSHA: "aaa1111", Subject: "Add checkout",
			AuthorName: "Mostafa", AuthorEmail: "dev@example.com",
			AuthorDate: day.Add(15 * time.Hour).UnixMilli(),
		},
		{
			SHA: "bbb", ShortSHA: "bbb2222", Subject: "Fix totals",
			AuthorName: "Mostafa", AuthorEmail: "dev@example.com",
			AuthorDate: day.Add(11 * time.Hour).UnixMilli(),
		},
		{
			SHA: "ccc", ShortSHA: "ccc3333", Subject: "Seed data",
			AuthorName: "Mostafa", AuthorEmail: "dev@example.com",
			AuthorDate: day.AddDate(0, 0, -1).Add(9 * time.Hour).UnixMilli(),
		},
	}}
	service := newTestService(t, newMemoryRepo(), WithHistory(history))
	token := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true, ShowChangelog: true}))

	page, err := service.View(context.Background(), testProjectID, token, "")
	if err != nil {
		t.Fatalf("View() = %v", err)
	}
	if len(page.Changelog) != 2 {
		t.Fatalf("changelog days = %d, want 2: %+v", len(page.Changelog), page.Changelog)
	}
	if len(page.Changelog[0].Commits) != 2 || len(page.Changelog[1].Commits) != 1 {
		t.Fatalf("day grouping = %+v", page.Changelog)
	}
	if page.Changelog[0].Label != "Monday, 17 August 2026" {
		t.Fatalf("day label = %q", page.Changelog[0].Label)
	}
	for _, day := range page.Changelog {
		for _, commit := range day.Commits {
			if strings.Contains(commit.Author, "@") {
				t.Fatalf("commit exposes an email: %+v", commit)
			}
		}
	}
}

func TestChangelogFallsBackToAFriendlyNote(t *testing.T) {
	tests := []struct {
		name    string
		history History
	}{
		{name: "no repository", history: stubHistory{err: errors.New("not a git repository")}},
		{name: "no commits yet", history: stubHistory{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t, newMemoryRepo(), WithHistory(test.history))
			token := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true, ShowChangelog: true}))

			page, err := service.View(context.Background(), testProjectID, token, "")
			if err != nil {
				t.Fatalf("View() = %v", err)
			}
			if len(page.Changelog) != 0 {
				t.Fatalf("changelog = %+v, want none", page.Changelog)
			}
			if page.ChangelogNote == "" {
				t.Fatal("expected a friendly note")
			}
		})
	}
}

func TestUsageSectionShowsRunsOnlyWhenEnabled(t *testing.T) {
	service := newTestService(t, newMemoryRepo(), WithUsage(stubUsage{runs: 12}))

	off := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true}))
	page, err := service.View(context.Background(), testProjectID, off, "")
	if err != nil {
		t.Fatalf("View() = %v", err)
	}
	if page.ShowUsage || page.UsageRuns != 0 {
		t.Fatalf("usage leaked while the toggle is off: %+v", page)
	}

	enable(t, service, UpdateInput{Enabled: true, ShowUsage: true})
	page, err = service.View(context.Background(), testProjectID, off, "")
	if err != nil {
		t.Fatalf("View() = %v", err)
	}
	if !page.ShowUsage || page.UsageRuns != 12 {
		t.Fatalf("usage = %t / %d", page.ShowUsage, page.UsageRuns)
	}
}

func TestDefaultsBeforeAnythingIsStored(t *testing.T) {
	service := newTestService(t, newMemoryRepo())

	settings, err := service.Get(context.Background(), testProjectID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if settings.Enabled {
		t.Fatal("a project must start with its portal closed")
	}
	if !settings.ShowPreview || !settings.ShowChangelog {
		t.Fatalf("expected preview and changelog on by default: %+v", settings)
	}
	if settings.ShowUsage {
		t.Fatal("usage must default to off")
	}
}

func TestSaveSanitizesTheBrandTitleAndNote(t *testing.T) {
	service := newTestService(t, newMemoryRepo())

	settings := enable(t, service, UpdateInput{
		Enabled:    true,
		BrandTitle: "  Acme\x00 Shop\r\nportal  ",
		Note:       "line one\r\n\r\nline two\x07",
	})

	if strings.ContainsAny(settings.BrandTitle, "\r\n\x00") {
		t.Fatalf("brand title kept control characters: %q", settings.BrandTitle)
	}
	if strings.Contains(settings.Note, "\r") || strings.Contains(settings.Note, "\x07") {
		t.Fatalf("note kept control characters: %q", settings.Note)
	}
	if !strings.Contains(settings.Note, "line one") || !strings.Contains(settings.Note, "line two") {
		t.Fatalf("note lost its content: %q", settings.Note)
	}
}

func TestPageDirectionFollowsTheOperatorsText(t *testing.T) {
	tests := []struct {
		name  string
		brand string
		note  string
		want  string
	}{
		{name: "latin brand title", brand: "Acme Shop", want: "ltr"},
		{name: "arabic brand title", brand: "متجر أكمي", want: "rtl"},
		{name: "arabic note with no brand", note: "تم تحديث الصفحة", want: "rtl"},
		{name: "nothing written falls back to the project name", want: "ltr"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t, newMemoryRepo())
			token := tokenFrom(t, enable(t, service, UpdateInput{
				Enabled: true, BrandTitle: test.brand, Note: test.note,
			}))
			page, err := service.View(context.Background(), testProjectID, token, "")
			if err != nil {
				t.Fatalf("View() = %v", err)
			}
			if page.Direction != test.want {
				t.Fatalf("direction = %q, want %q", page.Direction, test.want)
			}
		})
	}
}

func TestPageTitleFallsBackToTheProjectName(t *testing.T) {
	service := newTestService(t, newMemoryRepo())
	token := tokenFrom(t, enable(t, service, UpdateInput{Enabled: true}))

	page, err := service.View(context.Background(), testProjectID, token, "")
	if err != nil {
		t.Fatalf("View() = %v", err)
	}
	if page.Title != "Acme Shop" {
		t.Fatalf("title = %q", page.Title)
	}
	if !page.Running || page.StatusLabel != "Running" {
		t.Fatalf("status = %q / %t", page.StatusLabel, page.Running)
	}
}

func TestLifecycleActionsAreAudited(t *testing.T) {
	recorder := &recordingAudit{}
	service := newTestService(t, newMemoryRepo(), WithAudit(recorder))

	enable(t, service, UpdateInput{Enabled: true})
	enable(t, service, UpdateInput{Enabled: true, Note: "just a note edit"})
	enable(t, service, UpdateInput{Enabled: true, Rotate: true})
	enable(t, service, UpdateInput{Enabled: false})

	got := recorder.actions()
	want := []string{ActionPortalEnable, ActionPortalRotate, ActionPortalDisable}
	if len(got) != len(want) {
		t.Fatalf("audit actions = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("audit actions = %v, want %v", got, want)
		}
	}
}

func TestSaveWithoutAStoreIsUnavailable(t *testing.T) {
	var service *Service
	if _, err := service.Get(context.Background(), testProjectID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Get() = %v, want ErrUnavailable", err)
	}
	if _, err := service.Save(context.Background(), testProjectID, UpdateInput{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Save() = %v, want ErrUnavailable", err)
	}
	if _, err := service.View(context.Background(), testProjectID, "t", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("View() = %v, want ErrNotFound", err)
	}
}
