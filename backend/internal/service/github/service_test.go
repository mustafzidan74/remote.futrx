package github

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

/* ------------------------------------------------------------------ *
 * Fakes
 * ------------------------------------------------------------------ */

// scriptedCLI answers each command from a table keyed on the program plus its
// first argument ("gh repo view", "git status"), so a test names only the
// commands its case actually exercises.
type scriptedCLI struct {
	mu        sync.Mutex
	responses map[string]cliResponse
	calls     []Command
	stdins    []string
}

type cliResponse struct {
	out string
	err error
}

func newScriptedCLI(responses map[string]cliResponse) *scriptedCLI {
	if responses == nil {
		responses = map[string]cliResponse{}
	}
	return &scriptedCLI{responses: responses}
}

func (c *scriptedCLI) Available() bool { return true }

func (c *scriptedCLI) Run(_ context.Context, cmd Command) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, cmd)
	c.stdins = append(c.stdins, cmd.Stdin)
	if response, ok := c.responses[cliKey(cmd.Argv)]; ok {
		return response.out, response.err
	}
	return "", nil
}

func (c *scriptedCLI) ran(prefix string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, call := range c.calls {
		if strings.HasPrefix(strings.Join(call.Argv, " "), prefix) {
			return true
		}
	}
	return false
}

func cliKey(argv []string) string {
	if len(argv) >= 2 {
		return argv[0] + " " + argv[1]
	}
	if len(argv) == 1 {
		return argv[0]
	}
	return ""
}

type fakeProjects struct {
	mu      sync.Mutex
	meta    serviceproject.Meta
	linked  *serviceproject.GitHubLink
	started int
	getErr  error
	// onStart lets a test model what a real Start does to the stored project.
	onStart func(*serviceproject.Meta)
}

func (p *fakeProjects) Get(context.Context, serviceproject.ID) (serviceproject.Meta, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.getErr != nil {
		return serviceproject.Meta{}, p.getErr
	}
	return p.meta, nil
}

func (p *fakeProjects) SetGitHubLink(
	_ context.Context,
	_ serviceproject.ID,
	link serviceproject.GitHubLink,
	actor string,
) (serviceproject.Meta, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	link.LinkedBy = actor
	p.linked = &link
	p.meta.GitHub = &link
	return p.meta, nil
}

func (p *fakeProjects) ClearGitHubLink(
	context.Context,
	serviceproject.ID,
) (serviceproject.Meta, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.linked = nil
	p.meta.GitHub = nil
	return p.meta, nil
}

func (p *fakeProjects) Start(context.Context, serviceproject.ID) (serviceproject.Meta, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started++
	if p.onStart != nil {
		p.onStart(&p.meta)
	}
	return p.meta, nil
}

type fakeChats struct {
	mu      sync.Mutex
	chats   []servicechat.Meta
	created []servicechat.CreateInput
}

func (c *fakeChats) List(context.Context) ([]servicechat.Meta, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]servicechat.Meta(nil), c.chats...), nil
}

func (c *fakeChats) Get(_ context.Context, id servicechat.ID) (servicechat.Meta, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, chat := range c.chats {
		if chat.ID == id {
			return chat, nil
		}
	}
	return servicechat.Meta{}, servicechat.ErrNotFound
}

func (c *fakeChats) Create(
	_ context.Context,
	in servicechat.CreateInput,
) (servicechat.Meta, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created = append(c.created, in)
	chat := servicechat.Meta{
		ID:        servicechat.ID("chat-" + in.Title),
		Title:     in.Title,
		ProjectID: in.ProjectID,
	}
	c.chats = append(c.chats, chat)
	return chat, nil
}

type fakeStarter struct {
	mu     sync.Mutex
	inputs []prompt.StartInput
	err    error
}

func (s *fakeStarter) Start(
	input prompt.StartInput,
	_ func(servicechat.Event),
) (prompt.RunHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, input)
	if s.err != nil {
		return prompt.RunHandle{}, s.err
	}
	done := make(chan prompt.RunResult, 1)
	done <- prompt.RunResult{Output: "done"}
	close(done)
	return prompt.RunHandle{ID: 1, Done: done}, nil
}

func (s *fakeStarter) last() (prompt.StartInput, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inputs) == 0 {
		return prompt.StartInput{}, false
	}
	return s.inputs[len(s.inputs)-1], true
}

type memoryStore struct {
	mu      sync.Mutex
	records map[serviceproject.ID]Settings
}

func newMemoryStore() *memoryStore {
	return &memoryStore{records: map[serviceproject.ID]Settings{}}
}

func (s *memoryStore) Get(_ context.Context, id serviceproject.ID) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records[id], nil
}

func (s *memoryStore) Save(_ context.Context, id serviceproject.ID, record Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[id] = record
	return nil
}

func (s *memoryStore) Delete(_ context.Context, id serviceproject.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

type recordingNotifier struct {
	mu     sync.Mutex
	events []NotifyEvent
}

func (n *recordingNotifier) PublishChatEvent(
	_ context.Context,
	_ servicechat.ID,
	event NotifyEvent,
) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, event)
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.events)
}

const testProjectID = serviceproject.ID("abcd1234")

// linkedProject is a running project already bound to o/r.
func linkedProject() *fakeProjects {
	return &fakeProjects{meta: serviceproject.Meta{
		ID:            testProjectID,
		Name:          "Demo",
		Slug:          "demo",
		ContainerName: "demo",
		Status:        serviceproject.StatusRunning,
		GitHub: &serviceproject.GitHubLink{
			Owner: "o", Repo: "r", DefaultBranch: "main", LinkedBy: "owner@example.test",
		},
	}}
}

func fixedClock() func() time.Time {
	moment := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return moment }
}

/* ------------------------------------------------------------------ *
 * Status
 * ------------------------------------------------------------------ */

func TestApplyGitStatus(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		wantBranch     string
		wantUpstream   string
		wantAhead      int
		wantBehind     int
		wantDirtyCount int
	}{
		{
			name:       "clean and in sync",
			raw:        "## main...origin/main\n",
			wantBranch: "main", wantUpstream: "origin/main",
		},
		{
			name:       "ahead only",
			raw:        "## feat/x...origin/feat/x [ahead 2]\n",
			wantBranch: "feat/x", wantUpstream: "origin/feat/x", wantAhead: 2,
		},
		{
			name:       "ahead and behind",
			raw:        "## main...origin/main [ahead 1, behind 3]\n M a.go\n?? b.go\n",
			wantBranch: "main", wantUpstream: "origin/main",
			wantAhead: 1, wantBehind: 3, wantDirtyCount: 2,
		},
		{
			name:       "no upstream configured",
			raw:        "## feat/new\n M a.go\n",
			wantBranch: "feat/new", wantDirtyCount: 1,
		},
		{
			name:       "fresh repository with no commits",
			raw:        "## No commits yet on main\n?? README.md\n",
			wantBranch: "main", wantDirtyCount: 1,
		},
		{name: "empty output"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var status Status
			applyGitStatus(&status, test.raw)
			if status.Branch != test.wantBranch {
				t.Fatalf("Branch = %q, want %q", status.Branch, test.wantBranch)
			}
			if status.Upstream != test.wantUpstream {
				t.Fatalf("Upstream = %q, want %q", status.Upstream, test.wantUpstream)
			}
			if status.Ahead != test.wantAhead || status.Behind != test.wantBehind {
				t.Fatalf("ahead/behind = %d/%d, want %d/%d",
					status.Ahead, status.Behind, test.wantAhead, test.wantBehind)
			}
			if status.DirtyCount != test.wantDirtyCount {
				t.Fatalf("DirtyCount = %d, want %d", status.DirtyCount, test.wantDirtyCount)
			}
			if status.Dirty != (test.wantDirtyCount > 0) {
				t.Fatalf("Dirty = %v with %d changed paths", status.Dirty, status.DirtyCount)
			}
		})
	}
}

func TestApplyWorkspaceProbe(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantRepo  bool
		wantEmpty bool
	}{
		{name: "repository with files", raw: "IS_REPO=true\nENTRIES=12\nREMOTE=x\n", wantRepo: true},
		{name: "empty directory", raw: "IS_REPO=false\nENTRIES=0\nREMOTE=\n", wantEmpty: true},
		{name: "files but no repository", raw: "IS_REPO=false\nENTRIES=3\nREMOTE=\n"},
		{name: "garbled output", raw: "???"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var status Status
			applyWorkspaceProbe(&status, test.raw)
			if status.WorkspaceRepo != test.wantRepo {
				t.Fatalf("WorkspaceRepo = %v, want %v", status.WorkspaceRepo, test.wantRepo)
			}
			if status.WorkspaceEmpty != test.wantEmpty {
				t.Fatalf("WorkspaceEmpty = %v, want %v", status.WorkspaceEmpty, test.wantEmpty)
			}
		})
	}
}

func TestStatusOnAStoppedContainerAsksTheGuestNothing(t *testing.T) {
	projects := linkedProject()
	projects.meta.Status = serviceproject.StatusStopped
	cli := newScriptedCLI(nil)
	service := New(newMemoryStore(), cli, projects, WithClock(fixedClock()))

	status, err := service.Status(context.Background(), testProjectID)
	if err != nil {
		t.Fatalf("Status returned %v", err)
	}
	if !status.Linked || status.ContainerRunning {
		t.Fatalf("status = %+v, want linked and not running", status)
	}
	if len(cli.calls) != 0 {
		t.Fatalf("a stopped project must not be shelled into; ran %v", cli.calls)
	}
	if status.DefaultCommitMessage != "Changes from Remote — 2026-08-18" {
		t.Fatalf("DefaultCommitMessage = %q", status.DefaultCommitMessage)
	}
}

func TestSummarizeCheck(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		conclusion string
		state      string
		want       string
	}{
		{name: "successful run", conclusion: "SUCCESS", want: ChecksPassing},
		{name: "skipped run counts as fine", conclusion: "SKIPPED", want: ChecksPassing},
		{name: "neutral run counts as fine", conclusion: "NEUTRAL", want: ChecksPassing},
		{name: "failed run", conclusion: "FAILURE", want: ChecksFailing},
		{name: "timed out run", conclusion: "TIMED_OUT", want: ChecksFailing},
		{name: "in progress", status: "IN_PROGRESS", want: ChecksPending},
		{name: "legacy commit status", state: "SUCCESS", want: ChecksPassing},
		{name: "legacy pending status", state: "PENDING", want: ChecksPending},
		{
			name:   "completed with no conclusion is not green",
			status: "COMPLETED", want: ChecksFailing,
		},
		{
			name:       "an unknown conclusion is never green",
			conclusion: "SOMETHING_NEW", want: ChecksFailing,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := summarizeCheck(test.status, test.conclusion, test.state)
			if got != test.want {
				t.Fatalf("summarizeCheck = %q, want %q", got, test.want)
			}
		})
	}
}

/* ------------------------------------------------------------------ *
 * Settings
 * ------------------------------------------------------------------ */

func TestSaveSettings(t *testing.T) {
	ctx := context.Background()
	enabled := true

	t.Run("arming automation mints a secret and shows it once", func(t *testing.T) {
		store := newMemoryStore()
		service := New(store, newScriptedCLI(nil), linkedProject(),
			WithClock(fixedClock()), WithBaseURL("https://remote.example.test"))

		saved, err := service.SaveSettings(ctx, testProjectID,
			SettingsInput{AutoRun: &enabled}, "admin@example.test")
		if err != nil {
			t.Fatalf("SaveSettings returned %v", err)
		}
		if saved.Secret == "" {
			t.Fatal("arming automation must issue a webhook secret")
		}
		if !saved.AutoRun || !saved.WebhookConfigured {
			t.Fatalf("saved = %+v, want autoRun and a configured webhook", saved)
		}
		if saved.WebhookURL != "https://remote.example.test/hooks/github/abcd1234" {
			t.Fatalf("WebhookURL = %q", saved.WebhookURL)
		}

		// Reading it back must never repeat the secret.
		reread, err := service.Settings(ctx, testProjectID)
		if err != nil {
			t.Fatalf("Settings returned %v", err)
		}
		if reread.Secret != "" {
			t.Fatal("the stored secret must never be returned again")
		}
		if !reread.WebhookConfigured {
			t.Fatal("the panel still has to know a secret exists")
		}
	})

	t.Run("disabling clears the secret and disarms automation", func(t *testing.T) {
		store := newMemoryStore()
		service := New(store, newScriptedCLI(nil), linkedProject(), WithClock(fixedClock()))
		if _, err := service.SaveSettings(ctx, testProjectID,
			SettingsInput{AutoRun: &enabled}, "admin@example.test"); err != nil {
			t.Fatalf("arm: %v", err)
		}
		saved, err := service.SaveSettings(ctx, testProjectID,
			SettingsInput{Disable: true}, "admin@example.test")
		if err != nil {
			t.Fatalf("disable: %v", err)
		}
		if saved.WebhookConfigured || saved.AutoRun {
			t.Fatalf("saved = %+v, want no secret and no autoRun", saved)
		}
		stored, _ := store.Get(ctx, testProjectID)
		if stored.Secret != "" {
			t.Fatal("the secret has to be gone from the store, not just from the view")
		}
	})

	t.Run("rotating replaces the secret", func(t *testing.T) {
		store := newMemoryStore()
		service := New(store, newScriptedCLI(nil), linkedProject(), WithClock(fixedClock()))
		first, err := service.SaveSettings(ctx, testProjectID,
			SettingsInput{AutoRun: &enabled}, "admin@example.test")
		if err != nil {
			t.Fatalf("arm: %v", err)
		}
		second, err := service.SaveSettings(ctx, testProjectID,
			SettingsInput{Rotate: true}, "admin@example.test")
		if err != nil {
			t.Fatalf("rotate: %v", err)
		}
		if second.Secret == "" || second.Secret == first.Secret {
			t.Fatal("rotation must issue a different secret")
		}
	})

	t.Run("the label is normalized and defaulted", func(t *testing.T) {
		store := newMemoryStore()
		service := New(store, newScriptedCLI(nil), linkedProject(), WithClock(fixedClock()))

		defaulted, err := service.Settings(ctx, testProjectID)
		if err != nil {
			t.Fatalf("Settings returned %v", err)
		}
		if defaulted.Label != DefaultLabel {
			t.Fatalf("Label = %q, want %q", defaulted.Label, DefaultLabel)
		}

		custom := "  Agent-Please  "
		saved, err := service.SaveSettings(ctx, testProjectID,
			SettingsInput{Label: &custom}, "member@example.test")
		if err != nil {
			t.Fatalf("SaveSettings returned %v", err)
		}
		if saved.Label != "agent-please" {
			t.Fatalf("Label = %q, want it trimmed and lowercased", saved.Label)
		}
	})

	t.Run("autoRun cannot be left armed without a secret", func(t *testing.T) {
		store := newMemoryStore()
		_ = store.Save(ctx, testProjectID, Settings{AutoRun: true})
		service := New(store, newScriptedCLI(nil), linkedProject(), WithClock(fixedClock()))
		saved, err := service.SaveSettings(ctx, testProjectID, SettingsInput{}, "a@b.test")
		if err != nil {
			t.Fatalf("SaveSettings returned %v", err)
		}
		if saved.AutoRun {
			t.Fatal("autoRun must be forced off when no secret can verify a delivery")
		}
	})
}

/* ------------------------------------------------------------------ *
 * Delivery
 * ------------------------------------------------------------------ */

// deliver signs a body with the project's secret and pushes it through.
func deliver(
	t *testing.T,
	service *Service,
	store *memoryStore,
	event string,
	body string,
) (DeliveryOutcome, error) {
	t.Helper()
	stored, _ := store.Get(context.Background(), testProjectID)
	return service.HandleDelivery(context.Background(), testProjectID, DeliveryRequest{
		Event:     event,
		ID:        "delivery-1",
		Signature: Sign(stored.Secret, []byte(body)),
		Body:      []byte(body),
	})
}

const labelledIssueBody = `{
  "action": "opened",
  "repository": {"full_name": "o/r"},
  "sender": {"login": "alice"},
  "issue": {
    "number": 7, "title": "Broken build", "body": "make test fails",
    "html_url": "https://github.com/o/r/issues/7",
    "labels": [{"name": "remote-agent"}]
  }
}`

func armedService(t *testing.T, autoRun bool) (*Service, *memoryStore, *fakeChats, *fakeStarter, *recordingNotifier, *fakeProjects) {
	t.Helper()
	store := newMemoryStore()
	chats := &fakeChats{}
	starter := &fakeStarter{}
	notifier := &recordingNotifier{}
	projects := linkedProject()
	service := New(store, newScriptedCLI(nil), projects,
		WithClock(fixedClock()),
		WithChats(chats),
		WithStarter(starter),
		WithNotifier(notifier),
		WithBaseURL("https://remote.example.test"),
	)
	if _, err := service.SaveSettings(context.Background(), testProjectID,
		SettingsInput{Rotate: true}, "admin@example.test"); err != nil {
		t.Fatalf("mint secret: %v", err)
	}
	if autoRun {
		enabled := true
		if _, err := service.SaveSettings(context.Background(), testProjectID,
			SettingsInput{AutoRun: &enabled}, "admin@example.test"); err != nil {
			t.Fatalf("arm autoRun: %v", err)
		}
	}
	return service, store, chats, starter, notifier, projects
}

func TestHandleDeliveryRejections(t *testing.T) {
	ctx := context.Background()

	t.Run("no secret configured", func(t *testing.T) {
		service := New(newMemoryStore(), newScriptedCLI(nil), linkedProject(), WithClock(fixedClock()))
		_, err := service.HandleDelivery(ctx, testProjectID, DeliveryRequest{
			Event: "issues", Body: []byte(labelledIssueBody),
		})
		if !errors.Is(err, ErrWebhookDisabled) {
			t.Fatalf("err = %v, want ErrWebhookDisabled", err)
		}
	})

	t.Run("bad signature", func(t *testing.T) {
		service, _, _, starter, _, _ := armedService(t, true)
		_, err := service.HandleDelivery(ctx, testProjectID, DeliveryRequest{
			Event: "issues", Signature: "sha256=deadbeef", Body: []byte(labelledIssueBody),
		})
		if !errors.Is(err, ErrBadSignature) {
			t.Fatalf("err = %v, want ErrBadSignature", err)
		}
		if len(starter.inputs) != 0 {
			t.Fatal("a forged delivery must never reach the prompt service")
		}
	})

	t.Run("forged deliveries do not evict the delivery log", func(t *testing.T) {
		service, store, _, _, _, _ := armedService(t, true)
		if _, err := deliver(t, service, store, "issues", labelledIssueBody); err != nil {
			t.Fatalf("genuine delivery: %v", err)
		}
		for i := 0; i < MaxDeliveries+5; i++ {
			_, _ = service.HandleDelivery(ctx, testProjectID, DeliveryRequest{
				Event: "issues", Signature: "sha256=00", Body: []byte(labelledIssueBody),
			})
		}
		settings, _ := service.Settings(ctx, testProjectID)
		if len(settings.Deliveries) != 1 {
			t.Fatalf("delivery log holds %d rows, want only the genuine one", len(settings.Deliveries))
		}
	})

	t.Run("payload over the cap", func(t *testing.T) {
		service, store, _, _, _, _ := armedService(t, true)
		stored, _ := store.Get(ctx, testProjectID)
		body := make([]byte, MaxPayloadBytes+1)
		_, err := service.HandleDelivery(ctx, testProjectID, DeliveryRequest{
			Event: "issues", Signature: Sign(stored.Secret, body), Body: body,
		})
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
		}
	})

	t.Run("delivery for another repository is ignored", func(t *testing.T) {
		service, store, _, starter, _, _ := armedService(t, true)
		body := strings.Replace(labelledIssueBody, `"o/r"`, `"someone/else"`, 1)
		outcome, err := deliver(t, service, store, "issues", body)
		if err != nil {
			t.Fatalf("HandleDelivery returned %v", err)
		}
		if outcome.Outcome != OutcomeIgnored {
			t.Fatalf("outcome = %+v, want ignored", outcome)
		}
		if len(starter.inputs) != 0 {
			t.Fatal("a delivery for another repository must not start a run")
		}
	})
}

func TestHandleDeliveryAutoRunOff(t *testing.T) {
	service, store, chats, starter, notifier, _ := armedService(t, false)

	outcome, err := deliver(t, service, store, "issues", labelledIssueBody)
	if err != nil {
		t.Fatalf("HandleDelivery returned %v", err)
	}
	if outcome.Outcome != OutcomeChatOnly || outcome.Started {
		t.Fatalf("outcome = %+v, want a chat and no run", outcome)
	}
	if len(chats.created) != 1 || chats.created[0].Title != "GH #7: Broken build" {
		t.Fatalf("created chats = %+v, want one titled GH #7: Broken build", chats.created)
	}
	if len(starter.inputs) != 0 {
		t.Fatal("autoRun is off, so nothing may be started")
	}
	if notifier.count() != 1 {
		t.Fatalf("published %d notifications, want one telling the operator to look", notifier.count())
	}
}

func TestHandleDeliveryAutoRunOn(t *testing.T) {
	service, store, chats, starter, _, projects := armedService(t, true)

	outcome, err := deliver(t, service, store, "issues", labelledIssueBody)
	if err != nil {
		t.Fatalf("HandleDelivery returned %v", err)
	}
	if outcome.Outcome != OutcomeRan || !outcome.Started {
		t.Fatalf("outcome = %+v, want a started run", outcome)
	}
	input, ok := starter.last()
	if !ok {
		t.Fatal("no run was started")
	}
	// The run is attributed to the person who linked the repository, not to
	// the GitHub account that opened the issue.
	if input.Actor.Email != "owner@example.test" {
		t.Fatalf("run actor = %q, want the linker", input.Actor.Email)
	}
	if input.Synthetic != SyntheticKind {
		t.Fatalf("Synthetic = %q, want %q", input.Synthetic, SyntheticKind)
	}
	if !strings.Contains(input.Prompt, "make test fails") {
		t.Fatalf("prompt does not carry the issue body:\n%s", input.Prompt)
	}
	if !strings.Contains(input.Prompt, "untrusted input") {
		t.Fatal("the prompt must fence the issue body as untrusted")
	}
	if projects.started == 0 {
		t.Fatal("the project container has to be started before a run")
	}
	if len(chats.created) != 1 {
		t.Fatalf("created %d chats, want one", len(chats.created))
	}
}

func TestHandleDeliveryReusesTheIssueChat(t *testing.T) {
	service, store, chats, _, _, _ := armedService(t, true)

	if _, err := deliver(t, service, store, "issues", labelledIssueBody); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	comment := `{
	  "action": "created",
	  "repository": {"full_name": "o/r"},
	  "issue": {"number": 7, "title": "Broken build"},
	  "comment": {"body": "/remote try again", "author_association": "OWNER"}
	}`
	outcome, err := deliver(t, service, store, "issue_comment", comment)
	if err != nil {
		t.Fatalf("second delivery: %v", err)
	}
	if len(chats.created) != 1 {
		t.Fatalf("created %d chats, want the first one reused", len(chats.created))
	}
	if outcome.ChatID != "chat-GH #7: Broken build" {
		t.Fatalf("ChatID = %q, want the existing chat", outcome.ChatID)
	}
}

func TestHandleDeliveryRecordsEveryOutcome(t *testing.T) {
	service, store, _, _, _, _ := armedService(t, false)

	// An unlabelled issue: acted on by nothing, but still recorded with a
	// reason, because "why did nothing happen?" is the question operators ask.
	unlabelled := `{"action":"opened","repository":{"full_name":"o/r"},
	  "issue":{"number":8,"title":"Nope","labels":[]}}`
	outcome, err := deliver(t, service, store, "issues", unlabelled)
	if err != nil {
		t.Fatalf("HandleDelivery returned %v", err)
	}
	if outcome.Outcome != OutcomeIgnored || outcome.Reason == "" {
		t.Fatalf("outcome = %+v, want ignored with a reason", outcome)
	}
	settings, err := service.Settings(context.Background(), testProjectID)
	if err != nil {
		t.Fatalf("Settings returned %v", err)
	}
	if len(settings.Deliveries) != 1 {
		t.Fatalf("recorded %d deliveries, want 1", len(settings.Deliveries))
	}
	if settings.Deliveries[0].Outcome != OutcomeIgnored {
		t.Fatalf("delivery = %+v, want the ignored outcome", settings.Deliveries[0])
	}
}

func TestDeliveryLogIsBounded(t *testing.T) {
	service, store, _, _, _, _ := armedService(t, false)
	for i := 0; i < MaxDeliveries+7; i++ {
		if _, err := deliver(t, service, store, "push", `{"repository":{"full_name":"o/r"}}`); err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}
	settings, _ := service.Settings(context.Background(), testProjectID)
	if len(settings.Deliveries) != MaxDeliveries {
		t.Fatalf("recorded %d deliveries, want the ring capped at %d",
			len(settings.Deliveries), MaxDeliveries)
	}
}

/* ------------------------------------------------------------------ *
 * Link and pull requests
 * ------------------------------------------------------------------ */

func TestLinkUsesTheContainersOwnCredential(t *testing.T) {
	projects := &fakeProjects{meta: serviceproject.Meta{
		ID: testProjectID, ContainerName: "demo", Status: serviceproject.StatusRunning,
	}}
	cli := newScriptedCLI(map[string]cliResponse{
		"gh repo": {out: `{"name":"Remote","owner":{"login":"FutrX-com"},
			"defaultBranchRef":{"name":"main"},"isPrivate":true}`},
	})
	service := New(newMemoryStore(), cli, projects, WithClock(fixedClock()))

	if _, err := service.Link(context.Background(), testProjectID,
		LinkInput{Repo: "https://github.com/futrx-com/remote"}, "linker@example.test"); err != nil {
		t.Fatalf("Link returned %v", err)
	}
	if projects.linked == nil {
		t.Fatal("nothing was persisted")
	}
	// GitHub's own casing wins over what the human typed.
	if projects.linked.Owner != "FutrX-com" || projects.linked.Repo != "Remote" {
		t.Fatalf("stored link = %+v, want GitHub's casing", projects.linked)
	}
	if projects.linked.DefaultBranch != "main" {
		t.Fatalf("DefaultBranch = %q, want main", projects.linked.DefaultBranch)
	}
	if projects.linked.LinkedBy != "linker@example.test" {
		t.Fatalf("LinkedBy = %q", projects.linked.LinkedBy)
	}
	if !cli.ran("gh repo view") {
		t.Fatal("the link was never validated inside the container")
	}
}

func TestLinkRejectsAnUnreadableRepository(t *testing.T) {
	projects := &fakeProjects{meta: serviceproject.Meta{
		ID: testProjectID, ContainerName: "demo", Status: serviceproject.StatusRunning,
	}}
	cli := newScriptedCLI(map[string]cliResponse{
		"gh repo": {out: "gh: Could not resolve to a Repository", err: errors.New("exit 1")},
	})
	service := New(newMemoryStore(), cli, projects, WithClock(fixedClock()))

	_, err := service.Link(context.Background(), testProjectID, LinkInput{Repo: "o/nope"}, "a@b.test")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth for a credential that cannot see the repository", err)
	}
	if projects.linked != nil {
		t.Fatal("nothing may be persisted when validation fails")
	}
}

func TestCreatePRRefusesADirtyWorkspaceWithoutConfirmation(t *testing.T) {
	cli := newScriptedCLI(map[string]cliResponse{
		"sh -c":      {out: "IS_REPO=true\nENTRIES=8\nREMOTE=x\n"},
		"gh auth":    {out: "Logged in"},
		"git status": {out: "## feat/x...origin/feat/x\n M a.go\n"},
	})
	service := New(newMemoryStore(), cli, linkedProject(), WithClock(fixedClock()))

	_, err := service.CreatePR(context.Background(), testProjectID, CreatePRInput{Title: "T"})
	if !errors.Is(err, ErrDirtyWorkspace) {
		t.Fatalf("err = %v, want ErrDirtyWorkspace", err)
	}
	if cli.ran("git push") {
		t.Fatal("a refused pull request must never have pushed")
	}
}

func TestCreatePRCommitsWhenConfirmed(t *testing.T) {
	cli := newScriptedCLI(map[string]cliResponse{
		"sh -c":      {out: "IS_REPO=true\nENTRIES=8\nREMOTE=x\n"},
		"gh auth":    {out: "Logged in"},
		"git status": {out: "## feat/x...origin/feat/x\n M a.go\n"},
		"gh pr":      {out: "Creating pull request\nhttps://github.com/o/r/pull/12\n"},
	})
	service := New(newMemoryStore(), cli, linkedProject(), WithClock(fixedClock()))

	result, err := service.CreatePR(context.Background(), testProjectID,
		CreatePRInput{Title: "T", Commit: true})
	if err != nil {
		t.Fatalf("CreatePR returned %v", err)
	}
	if result.URL != "https://github.com/o/r/pull/12" {
		t.Fatalf("URL = %q", result.URL)
	}
	if !result.Committed || result.Branch != "feat/x" {
		t.Fatalf("result = %+v", result)
	}
	for _, want := range []string{"git add -A", "git -c user.name", "git push", "gh pr create"} {
		if !cli.ran(want) {
			t.Fatalf("expected the flow to run %q; calls: %v", want, cli.calls)
		}
	}
}

func TestCreatePRRefusesHeadEqualToBase(t *testing.T) {
	cli := newScriptedCLI(map[string]cliResponse{
		"sh -c":      {out: "IS_REPO=true\nENTRIES=8\n"},
		"gh auth":    {out: "ok"},
		"git status": {out: "## main...origin/main\n"},
	})
	service := New(newMemoryStore(), cli, linkedProject(), WithClock(fixedClock()))

	_, err := service.CreatePR(context.Background(), testProjectID, CreatePRInput{Title: "T"})
	if !errors.Is(err, ErrHeadIsBase) {
		t.Fatalf("err = %v, want ErrHeadIsBase", err)
	}
}

func TestCreatePRRejectsABranchThatLooksLikeAFlag(t *testing.T) {
	service := New(newMemoryStore(), newScriptedCLI(nil), linkedProject(), WithClock(fixedClock()))
	_, err := service.CreatePR(context.Background(), testProjectID,
		CreatePRInput{Title: "T", Head: "--upload-pack=touch /tmp/x"})
	if !errors.Is(err, ErrInvalidBranch) {
		t.Fatalf("err = %v, want ErrInvalidBranch", err)
	}
}

func TestCloneRefusesANonEmptyWorkspace(t *testing.T) {
	cli := newScriptedCLI(map[string]cliResponse{
		"sh -c":   {out: "IS_REPO=false\nENTRIES=4\n"},
		"gh auth": {out: "ok"},
	})
	service := New(newMemoryStore(), cli, linkedProject(), WithClock(fixedClock()))

	err := service.Clone(context.Background(), testProjectID)
	if !errors.Is(err, ErrWorkspaceNotEmpty) {
		t.Fatalf("err = %v, want ErrWorkspaceNotEmpty", err)
	}
	if cli.ran("gh repo clone") {
		t.Fatal("a refused clone must never have run")
	}
}

func TestUnlinkForgetsTheSecret(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, projects := armedService(t, true)

	if err := service.Unlink(ctx, testProjectID); err != nil {
		t.Fatalf("Unlink returned %v", err)
	}
	if projects.linked != nil {
		t.Fatal("the repository link survived an unlink")
	}
	stored, _ := store.Get(ctx, testProjectID)
	if stored.Secret != "" || stored.AutoRun {
		t.Fatalf("stored settings = %+v, want the webhook secret gone", stored)
	}
}

func TestFirstPullRequestURL(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "url among chatter",
			output: "Warning: 3 uncommitted changes\nhttps://github.com/o/r/pull/12\n",
			want:   "https://github.com/o/r/pull/12",
		},
		{
			name:   "compare link is not a pull request",
			output: "https://github.com/o/r/compare/main...feat\n",
		},
		{name: "no url at all", output: "something went wrong"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := firstPullRequestURL(test.output); got != test.want {
				t.Fatalf("firstPullRequestURL = %q, want %q", got, test.want)
			}
		})
	}
}

// waitFor polls until the predicate holds, so a test can assert on work the
// run-watcher goroutine does after a delivery has already been answered.
func waitFor(t *testing.T, what string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestHandleDeliveryStartsAStoppedProjectAndStillCommentsBack(t *testing.T) {
	service, store, _, _, _, projects := armedService(t, true)
	enabled := true
	if _, err := service.SaveSettings(context.Background(), testProjectID,
		SettingsInput{CommentBack: &enabled}, "admin@example.test"); err != nil {
		t.Fatalf("enable commentBack: %v", err)
	}

	// The project is asleep when the delivery lands, which is the normal case
	// for a repository nobody has touched today.
	projects.mu.Lock()
	projects.meta.Status = serviceproject.StatusStopped
	projects.mu.Unlock()
	// Starting it is what a real project service does; the fake has to move
	// the status too, or the test would not exercise the bug this guards.
	projects.onStart = func(meta *serviceproject.Meta) {
		meta.Status = serviceproject.StatusRunning
	}

	outcome, err := deliver(t, service, store, "issues", labelledIssueBody)
	if err != nil {
		t.Fatalf("HandleDelivery returned %v", err)
	}
	if outcome.Outcome != OutcomeRan {
		t.Fatalf("outcome = %+v, want a started run", outcome)
	}

	cli, ok := service.cli.(*scriptedCLI)
	if !ok {
		t.Fatal("the test service is not backed by the scripted CLI")
	}
	// The comment is posted by the watcher goroutine once the run settles.
	waitFor(t, "the issue comment", func() bool { return cli.ran("gh issue comment") })
}

func TestStatusOnAnUnlinkedProjectAsksTheGuestNothing(t *testing.T) {
	projects := linkedProject()
	projects.meta.GitHub = nil
	cli := newScriptedCLI(nil)
	service := New(newMemoryStore(), cli, projects, WithClock(fixedClock()))

	status, err := service.Status(context.Background(), testProjectID)
	if err != nil {
		t.Fatalf("Status returned %v", err)
	}
	if status.Linked {
		t.Fatal("an unlinked project must not report as linked")
	}
	// `gh auth status` is a 90-second network call. Running it for a project
	// with nothing to talk to would make every visit to the panel slow.
	if len(cli.calls) != 0 {
		t.Fatalf("an unlinked project must not be shelled into; ran %v", cli.calls)
	}
}

func TestCreatePRDoesNotClaimACommitItDidNotMake(t *testing.T) {
	cli := newScriptedCLI(map[string]cliResponse{
		"sh -c":      {out: "IS_REPO=true\nENTRIES=8\n"},
		"gh auth":    {out: "ok"},
		"git status": {out: "## feat/x...origin/feat/x\n M a.go\n"},
		// Something else committed between the status read and here.
		"git -c": {out: "nothing to commit, working tree clean", err: errors.New("exit 1")},
		"gh pr":  {out: "https://github.com/o/r/pull/12\n"},
	})
	service := New(newMemoryStore(), cli, linkedProject(), WithClock(fixedClock()))

	result, err := service.CreatePR(context.Background(), testProjectID,
		CreatePRInput{Title: "T", Commit: true})
	if err != nil {
		t.Fatalf("CreatePR returned %v", err)
	}
	if result.Committed {
		t.Fatal("Committed must be false when there was nothing to commit")
	}
	if result.URL == "" {
		t.Fatal("the pull request should still have been opened")
	}
}

func TestCreatePRRefusesADetachedHead(t *testing.T) {
	cli := newScriptedCLI(map[string]cliResponse{
		"sh -c":   {out: "IS_REPO=true\nENTRIES=8\n"},
		"gh auth": {out: "ok"},
		// What git prints with `-sb` while HEAD is detached.
		"git status": {out: "## HEAD (no branch)\n"},
	})
	service := New(newMemoryStore(), cli, linkedProject(), WithClock(fixedClock()))

	_, err := service.CreatePR(context.Background(), testProjectID, CreatePRInput{Title: "T"})
	if !errors.Is(err, ErrInvalidBranch) {
		t.Fatalf("err = %v, want ErrInvalidBranch", err)
	}
	if cli.ran("git push") {
		t.Fatal("a detached HEAD must never reach a push")
	}
}

func TestListPullRequestsNamesTheLinkedRepository(t *testing.T) {
	cli := newScriptedCLI(map[string]cliResponse{
		"gh pr": {out: `[{"number":12,"title":"T","url":"u","statusCheckRollup":[
			{"status":"COMPLETED","conclusion":"SUCCESS"},
			{"status":"IN_PROGRESS"}]}]`},
	})
	service := New(newMemoryStore(), cli, linkedProject(), WithClock(fixedClock()))

	list, err := service.ListPullRequests(context.Background(), testProjectID)
	if err != nil {
		t.Fatalf("ListPullRequests returned %v", err)
	}
	if len(list) != 1 || list[0].Number != 12 {
		t.Fatalf("list = %+v, want one pull request", list)
	}
	// One passing and one still running: not green yet.
	if list[0].Checks != ChecksPending || list[0].ChecksTotal != 2 || list[0].ChecksPassed != 1 {
		t.Fatalf("checks = %+v", list[0])
	}
	if !cli.ran("gh pr list --repo o/r") {
		t.Fatalf("gh pr list must name the linked repository; calls: %v", cli.calls)
	}
}

func TestImportedCommentsReportTheRunWithThePullRequestLink(t *testing.T) {
	notifier := &recordingNotifier{}
	starter := &fakeStarter{}
	chats := &fakeChats{chats: []servicechat.Meta{{
		ID: "chat-1", Title: "Work", ProjectID: servicechat.ProjectID(testProjectID),
	}}}
	cli := newScriptedCLI(map[string]cliResponse{
		"gh api": {out: `[{"body":"please rename this","user":{"login":"reviewer"},
			"created_at":"2026-08-10T09:00:00Z"}]`},
	})
	service := New(newMemoryStore(), cli, linkedProject(),
		WithClock(fixedClock()),
		WithChats(chats),
		WithStarter(starter),
		WithNotifier(notifier),
		WithBaseURL("https://remote.example.test"),
	)

	result, err := service.ImportComments(context.Background(), testProjectID, 12,
		ImportInput{ChatID: "chat-1"}, "member@example.test")
	if err != nil {
		t.Fatalf("ImportComments returned %v", err)
	}
	if !result.Started || result.Comments == 0 {
		t.Fatalf("result = %+v, want a started run with comments", result)
	}
	input, _ := starter.last()
	if input.Synthetic != SyntheticKind {
		t.Fatalf("Synthetic = %q, want %q", input.Synthetic, SyntheticKind)
	}

	// The generic run observer skips these runs, so this report is the only
	// one they produce — and it has to carry the pull request, not the chat.
	waitFor(t, "the run notification", func() bool { return notifier.count() > 0 })
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if got := notifier.events[0].URL; got != "https://github.com/o/r/pull/12" {
		t.Fatalf("notification URL = %q, want the pull request", got)
	}
}

func TestPullRequestURL(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		number   int
		want     string
	}{
		{name: "normal", fullName: "o/r", number: 12, want: "https://github.com/o/r/pull/12"},
		{name: "no repository", number: 12},
		{name: "no number", fullName: "o/r"},
		{name: "negative number", fullName: "o/r", number: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pullRequestURL(test.fullName, test.number); got != test.want {
				t.Fatalf("pullRequestURL = %q, want %q", got, test.want)
			}
		})
	}
}
