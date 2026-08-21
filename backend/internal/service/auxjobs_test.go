package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	serviceauxmodel "github.com/futrx-com/remote.futrx.com/internal/service/auxmodel"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// auxChatStore is the slice of the chat repository the auxiliary driver
// touches. Everything else is embedded and nil, so an accidental dependency
// panics rather than passing quietly.
type auxChatStore struct {
	servicechat.Repository

	mu     sync.Mutex
	meta   servicechat.Meta
	events []servicechat.Event
	getErr error
	writes int
}

func (s *auxChatStore) Get(context.Context, servicechat.ID) (servicechat.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta, s.getErr
}

func (s *auxChatStore) ReadEvents(context.Context, servicechat.ID) ([]servicechat.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]servicechat.Event(nil), s.events...), nil
}

func (s *auxChatStore) Update(
	_ context.Context,
	_ servicechat.ID,
	fn func(*servicechat.Meta),
) (servicechat.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	fn(&s.meta)
	return s.meta, nil
}

func (s *auxChatStore) snapshot() (servicechat.Meta, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta, s.writes
}

// auxStubCompleter answers every job with one canned string.
type auxStubCompleter struct {
	mu     sync.Mutex
	answer string
	err    error
	calls  int
}

func (c *auxStubCompleter) Complete(
	context.Context,
	serviceauxmodel.Completion,
) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.answer, c.err
}

func (c *auxStubCompleter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// auxConfigStore hands the service one fixed configuration.
type auxConfigStore struct{ config serviceauxmodel.Config }

func (s auxConfigStore) Load(context.Context) (serviceauxmodel.Config, error) {
	return s.config, nil
}

func (s auxConfigStore) Save(context.Context, serviceauxmodel.Config) error { return nil }

func auxEnabledConfig() serviceauxmodel.Config {
	config := serviceauxmodel.DefaultConfig()
	config.Enabled = true
	return config
}

// newAuxTestDriver builds a driver whose background work runs inline, so a
// test can assert on the result without polling.
func newAuxTestDriver(
	t *testing.T,
	config serviceauxmodel.Config,
	completer serviceauxmodel.Completer,
	store *auxChatStore,
) *AuxJobDriver {
	t.Helper()
	aux := serviceauxmodel.New(
		context.Background(),
		auxConfigStore{config: config},
		serviceauxmodel.WithCompleter(completer),
	)
	driver := newAuxJobDriver(aux, store)
	driver.background = func(work func()) { work() }
	return driver
}

func userEvent(text string) servicechat.Event {
	return servicechat.Event{Type: "user", Text: text}
}

func TestRunSettledReplacesOnlyAMachineMadeTitle(t *testing.T) {
	const firstPrompt = "the login page redirects in a loop after the last deploy, please fix it"

	tests := []struct {
		name      string
		title     string
		wantTitle string
	}{
		{
			name:      "the truncated title is replaced",
			title:     servicechat.TitleFromPrompt(firstPrompt),
			wantTitle: "Fix the login redirect loop",
		},
		{
			name:      "a title a human typed is left alone",
			title:     "Client escalation — Monday",
			wantTitle: "Client escalation — Monday",
		},
		{
			name:      "a title this driver already wrote is not rewritten",
			title:     "Fix the login redirect loop",
			wantTitle: "Fix the login redirect loop",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &auxChatStore{
				meta:   servicechat.Meta{ID: "c1", Title: test.title},
				events: []servicechat.Event{userEvent(firstPrompt)},
			}
			completer := &auxStubCompleter{answer: "Fix the login redirect loop"}
			driver := newAuxTestDriver(t, auxEnabledConfig(), completer, store)

			driver.RunSettled(context.Background(), prompt.RunOutcome{
				ChatID: "c1", Output: "Done: the redirect loop is gone.",
			})

			meta, _ := store.snapshot()
			if meta.Title != test.wantTitle {
				t.Fatalf("title = %q, want %q", meta.Title, test.wantTitle)
			}
		})
	}
}

func TestRunSettledStoresTheSearchSubtitle(t *testing.T) {
	store := &auxChatStore{
		meta:   servicechat.Meta{ID: "c1", Title: "Client escalation"},
		events: []servicechat.Event{userEvent("fix the login loop")},
	}
	completer := &auxStubCompleter{answer: "Login redirect loop fixed"}
	driver := newAuxTestDriver(t, auxEnabledConfig(), completer, store)

	driver.RunSettled(context.Background(), prompt.RunOutcome{
		ChatID: "c1", Output: "I removed the duplicate redirect rule.",
	})

	meta, _ := store.snapshot()
	if meta.Summary != "Login redirect loop fixed" {
		t.Fatalf("summary = %q, want the model's one-liner", meta.Summary)
	}
}

func TestRunSettledDoesNothingWhenTheModelCannotHelp(t *testing.T) {
	const firstPrompt = "fix the login loop"
	disabledTitles := auxEnabledConfig()
	disabledTitles.Jobs = serviceauxmodel.JobSettings{
		serviceauxmodel.JobChatTitle:   serviceauxmodel.SourceOff,
		serviceauxmodel.JobChatSummary: serviceauxmodel.SourceOff,
	}

	tests := []struct {
		name      string
		config    serviceauxmodel.Config
		completer *auxStubCompleter
		outcome   prompt.RunOutcome
		wantCalls int
	}{
		{
			name:      "the whole service is switched off",
			config:    serviceauxmodel.DefaultConfig(),
			completer: &auxStubCompleter{answer: "never asked"},
			outcome:   prompt.RunOutcome{ChatID: "c1", Output: "done"},
		},
		{
			name:      "both chat jobs are switched off",
			config:    disabledTitles,
			completer: &auxStubCompleter{answer: "never asked"},
			outcome:   prompt.RunOutcome{ChatID: "c1", Output: "done"},
		},
		{
			name:      "the run failed, so there is nothing to report",
			config:    auxEnabledConfig(),
			completer: &auxStubCompleter{answer: "never asked"},
			outcome:   prompt.RunOutcome{ChatID: "c1", Output: "done", Err: errors.New("boom")},
		},
		{
			name:      "the run was cancelled",
			config:    auxEnabledConfig(),
			completer: &auxStubCompleter{answer: "never asked"},
			outcome:   prompt.RunOutcome{ChatID: "c1", Output: "done", Cancelled: true},
		},
		{
			name:      "the endpoint is unreachable, so the old title stands",
			config:    auxEnabledConfig(),
			completer: &auxStubCompleter{err: errors.New("connection refused")},
			outcome:   prompt.RunOutcome{ChatID: "c1", Output: "done"},
			wantCalls: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := servicechat.TitleFromPrompt(firstPrompt)
			store := &auxChatStore{
				meta:   servicechat.Meta{ID: "c1", Title: original},
				events: []servicechat.Event{userEvent(firstPrompt)},
			}
			driver := newAuxTestDriver(t, test.config, test.completer, store)

			driver.RunSettled(context.Background(), test.outcome)

			meta, writes := store.snapshot()
			if meta.Title != original {
				t.Fatalf("title = %q, want the untouched %q", meta.Title, original)
			}
			if meta.Summary != "" {
				t.Fatalf("summary = %q, want nothing stored", meta.Summary)
			}
			if writes != 0 {
				t.Fatalf("the chat was written %d times, want none", writes)
			}
			if test.completer.count() != test.wantCalls {
				t.Fatalf("endpoint dialled %d times, want %d",
					test.completer.count(), test.wantCalls)
			}
		})
	}
}

func TestRunSettledIgnoresSyntheticPromptsWhenNamingAChat(t *testing.T) {
	// An autopilot nudge is the platform talking to itself; naming a chat
	// after it would describe the machinery rather than the work.
	const human = "migrate the shop to the new checkout"
	store := &auxChatStore{
		meta: servicechat.Meta{ID: "c1", Title: servicechat.TitleFromPrompt(human)},
		events: []servicechat.Event{
			{Type: "user", Text: "Continue.", Synthetic: "autopilot"},
			userEvent(human),
		},
	}
	completer := &auxStubCompleter{answer: "Migrate shop checkout"}
	driver := newAuxTestDriver(t, auxEnabledConfig(), completer, store)

	driver.RunSettled(context.Background(), prompt.RunOutcome{ChatID: "c1", Output: "done"})

	if meta, _ := store.snapshot(); meta.Title != "Migrate shop checkout" {
		t.Fatalf("title = %q, want the one derived from the human prompt", meta.Title)
	}
}

func TestRegenerateTitleAlwaysRewritesAndReportsFailures(t *testing.T) {
	t.Run("a hand-written title is replaced on request", func(t *testing.T) {
		store := &auxChatStore{
			meta:   servicechat.Meta{ID: "c1", Title: "Whatever I called it"},
			events: []servicechat.Event{userEvent("set up the staging database")},
		}
		driver := newAuxTestDriver(t, auxEnabledConfig(),
			&auxStubCompleter{answer: "Set up staging database"}, store)

		meta, err := driver.RegenerateTitle(context.Background(), "c1")
		if err != nil {
			t.Fatalf("RegenerateTitle() = %v", err)
		}
		if meta.Title != "Set up staging database" {
			t.Fatalf("title = %q", meta.Title)
		}
	})

	t.Run("a chat with nothing in it says so", func(t *testing.T) {
		store := &auxChatStore{meta: servicechat.Meta{ID: "c1"}}
		driver := newAuxTestDriver(t, auxEnabledConfig(),
			&auxStubCompleter{answer: "unused"}, store)

		if _, err := driver.RegenerateTitle(context.Background(), "c1"); err == nil {
			t.Fatal("RegenerateTitle() = nil, want a reported reason")
		}
	})

	t.Run("a switched-off model reports ErrDisabled rather than pretending", func(t *testing.T) {
		store := &auxChatStore{
			meta:   servicechat.Meta{ID: "c1", Title: "x"},
			events: []servicechat.Event{userEvent("do a thing")},
		}
		driver := newAuxTestDriver(t, serviceauxmodel.DefaultConfig(),
			&auxStubCompleter{answer: "unused"}, store)

		_, err := driver.RegenerateTitle(context.Background(), "c1")
		if !errors.Is(err, serviceauxmodel.ErrDisabled) {
			t.Fatalf("RegenerateTitle() = %v, want ErrDisabled", err)
		}
	})

	t.Run("a nil driver is the no-auxiliary-model deployment", func(t *testing.T) {
		var driver *AuxJobDriver
		if _, err := driver.RegenerateTitle(context.Background(), "c1"); !errors.Is(err, serviceauxmodel.ErrDisabled) {
			t.Fatalf("RegenerateTitle() on a nil driver = %v, want ErrDisabled", err)
		}
	})
}

func TestAuxRunSummarizerFallsBackToNothing(t *testing.T) {
	tests := []struct {
		name      string
		config    serviceauxmodel.Config
		completer *auxStubCompleter
		want      string
	}{
		{
			name:      "a working model answers with one sentence",
			config:    auxEnabledConfig(),
			completer: &auxStubCompleter{answer: "The checkout migration is finished."},
			want:      "The checkout migration is finished.",
		},
		{
			name:      "a switched-off model answers with nothing",
			config:    serviceauxmodel.DefaultConfig(),
			completer: &auxStubCompleter{answer: "never asked"},
		},
		{
			name:      "an unreachable endpoint answers with nothing",
			config:    auxEnabledConfig(),
			completer: &auxStubCompleter{err: errors.New("refused")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			aux := serviceauxmodel.New(context.Background(),
				auxConfigStore{config: test.config},
				serviceauxmodel.WithCompleter(test.completer))

			got := auxRunSummarizer{aux: aux}.Summarize(context.Background(), "a long agent answer")
			if got != test.want {
				t.Fatalf("Summarize() = %q, want %q", got, test.want)
			}
		})
	}

	if got := (auxRunSummarizer{}).Summarize(context.Background(), "text"); got != "" {
		t.Fatalf("a nil model summarized to %q, want an empty fallback", got)
	}
}

func TestAuxCommitMessagesReportsAvailability(t *testing.T) {
	off := serviceauxmodel.DefaultConfig()
	offAux := serviceauxmodel.New(context.Background(), auxConfigStore{config: off},
		serviceauxmodel.WithCompleter(&auxStubCompleter{answer: "x"}))
	if (auxCommitMessages{aux: offAux}).Available() {
		t.Fatal("a switched-off model reported itself available for commit subjects")
	}
	if (auxCommitMessages{}).Available() {
		t.Fatal("a nil model reported itself available")
	}

	onAux := serviceauxmodel.New(context.Background(), auxConfigStore{config: auxEnabledConfig()},
		serviceauxmodel.WithCompleter(&auxStubCompleter{answer: "feat(shop): add checkout"}))
	subjects := auxCommitMessages{aux: onAux}
	if !subjects.Available() {
		t.Fatal("a working model is not available for commit subjects")
	}
	subject, err := subjects.Subject(context.Background(), "shop/checkout.go | 12 +++")
	if err != nil || !strings.HasPrefix(subject, "feat(shop)") {
		t.Fatalf("Subject() = %q, %v", subject, err)
	}
}
