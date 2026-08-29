package lighthouse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// Service is the policy layer over local Lighthouse audits: it decides what
// may be audited, runs the pages in the background, parses each report down to
// what is worth keeping, and keeps the per-project history bounded.
type Service struct {
	repo     Repository
	runner   Runner
	projects Projects
	audit    audit.Recorder
	now      func() time.Time

	// busy holds the projects with a run in flight. Two Lighthouse runs in one
	// container measure each other: the second one's numbers would include the
	// first one's browser competing for the same CPU, which is worse than no
	// numbers because they look real.
	mu   sync.Mutex
	busy map[serviceproject.ID]struct{}
	wait sync.WaitGroup
}

type Option func(*Service)

// WithAudit attaches the audit recorder.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// WithClock replaces the wall clock so timestamps are testable.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func New(repo Repository, runner Runner, projects Projects, options ...Option) *Service {
	service := &Service{
		repo:     repo,
		runner:   runner,
		projects: projects,
		audit:    audit.Nop{},
		now:      time.Now,
		busy:     map[serviceproject.ID]struct{}{},
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Available reports whether audits can be run on this host.
func (s *Service) Available() bool {
	return s != nil && s.repo != nil && s.runner != nil && s.runner.Available() && s.projects != nil
}

// Wait blocks until every in-flight run has finished, so a stored record never
// outlives the process still claiming to be running.
func (s *Service) Wait() {
	if s != nil {
		s.wait.Wait()
	}
}

// Overview returns the project's audit history and the state of its tooling.
func (s *Service) Overview(ctx context.Context, projectID serviceproject.ID) (Overview, error) {
	if !s.Available() {
		return Overview{}, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return Overview{}, serviceproject.ErrInvalidID
	}
	state, err := s.repo.Load(ctx, projectID)
	if err != nil {
		return Overview{}, err
	}
	runs := append([]Run(nil), state.Runs...)
	sortNewestFirst(runs)

	out := Overview{Runs: runs, Running: s.isBusy(projectID)}
	if out.Runs == nil {
		out.Runs = []Run{}
	}

	// The tooling question is only meaningful for a container that exists and
	// is up. Asking a stopped project answers "no CLI", which would put an
	// install button in front of an operator whose actual problem is that the
	// project is off.
	meta, err := s.projects.Get(ctx, projectID)
	if err == nil && meta.Status == serviceproject.StatusRunning && meta.ContainerName != "" {
		if installed, probeErr := s.runner.Installed(ctx, meta.ContainerName); probeErr == nil {
			out.Installed = &installed
		}
	}
	return out, nil
}

// Install adds the Lighthouse CLI to one project's container.
func (s *Service) Install(ctx context.Context, projectID serviceproject.ID, actor string) error {
	err := s.install(ctx, projectID)
	s.record(ctx, projectID, "install", audit.Meta{"actor": actor}, err)
	return err
}

func (s *Service) install(ctx context.Context, projectID serviceproject.ID) error {
	if !s.Available() {
		return ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return serviceproject.ErrInvalidID
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return err
	}
	if meta.Status != serviceproject.StatusRunning || meta.ContainerName == "" {
		return ErrNotRunning
	}
	// The install is synchronous: it is under a minute, the operator pressed a
	// button expecting it to be done afterwards, and a pending state for one
	// npm package would be more machinery than the thing it describes.
	installCtx, cancel := context.WithTimeout(ctx, InstallTimeout)
	defer cancel()
	return s.runner.Install(installCtx, meta.ContainerName)
}

// Start audits the given pages in the background.
func (s *Service) Start(
	ctx context.Context,
	projectID serviceproject.ID,
	in RunInput,
	actor string,
) (Run, error) {
	run, err := s.start(ctx, projectID, in, actor)
	s.record(ctx, projectID, string(run.ID), audit.Meta{
		"port":       in.Port,
		"pages":      len(in.Paths),
		"formFactor": run.FormFactor,
	}, err)
	return run, err
}

func (s *Service) start(
	ctx context.Context,
	projectID serviceproject.ID,
	in RunInput,
	actor string,
) (Run, error) {
	if !s.Available() {
		return Run{}, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return Run{}, serviceproject.ErrInvalidID
	}
	normalized, err := in.Normalize()
	if err != nil {
		return Run{}, err
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return Run{}, err
	}
	if meta.Status != serviceproject.StatusRunning || meta.ContainerName == "" {
		return Run{}, ErrNotRunning
	}
	// Checking the CLI before starting turns "six pages each failed the same
	// way" into one sentence with a fix in it.
	installed, err := s.runner.Installed(ctx, meta.ContainerName)
	if err != nil {
		return Run{}, err
	}
	if !installed {
		return Run{}, ErrToolingMissing
	}
	if !s.acquire(projectID) {
		return Run{}, ErrBusy
	}

	run := Run{
		ID:         newID(),
		Status:     StatusRunning,
		Label:      normalized.Label,
		Port:       normalized.Port,
		Paths:      normalized.Paths,
		FormFactor: FormFactor(normalized.FormFactor),
		Reports:    make([]Report, 0, len(normalized.Paths)),
		CreatedBy:  strings.ToLower(strings.TrimSpace(actor)),
		CreatedAt:  s.now().UnixMilli(),
	}
	if err := s.append(ctx, projectID, run); err != nil {
		s.release(projectID)
		return Run{}, err
	}

	s.wait.Add(1)
	go func() {
		defer s.wait.Done()
		defer s.release(projectID)
		// The run outlives the request that started it: six pages under
		// simulated throttling is minutes, and a closed tab must not abandon
		// work the container is already doing.
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), RunTimeout)
		defer cancel()
		s.run(runCtx, projectID, meta, run)
	}()
	return run, nil
}

// Delete removes one run from the history.
func (s *Service) Delete(ctx context.Context, projectID serviceproject.ID, id ID) error {
	if !s.Available() {
		return ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return serviceproject.ErrInvalidID
	}
	_, err := s.repo.Update(ctx, projectID, func(state State) (State, error) {
		kept := make([]Run, 0, len(state.Runs))
		found := false
		for _, run := range state.Runs {
			if run.ID == id {
				found = true
				continue
			}
			kept = append(kept, run)
		}
		if !found {
			return state, ErrNotFound
		}
		state.Runs = kept
		return state, nil
	})
	return err
}

func (s *Service) run(
	ctx context.Context,
	projectID serviceproject.ID,
	meta serviceproject.Meta,
	run Run,
) {
	for _, path := range run.Paths {
		run.Reports = append(run.Reports, s.auditPage(ctx, meta, run, path))

		// The record is written after every page so the panel fills in while
		// the run is still going. A six-page run that only reports at the end
		// is indistinguishable from one that has hung.
		if err := s.save(ctx, projectID, run); err != nil {
			return
		}
		if ctx.Err() != nil {
			break
		}
	}

	run.FinishedAt = s.now().UnixMilli()
	run.Status = StatusReady
	// A run where nothing could be measured is a failure. One where some pages
	// worked is not: those numbers are real and the failures are named on the
	// record beside them.
	if !anyMeasured(run.Reports) {
		run.Status = StatusFailed
		run.Error = firstError(run.Reports, ctx.Err())
	}
	if err := s.save(ctx, projectID, run); err != nil {
		return
	}
	s.prune(ctx, projectID)
}

func (s *Service) auditPage(
	ctx context.Context,
	meta serviceproject.Meta,
	run Run,
	path string,
) Report {
	pageCtx, cancel := context.WithTimeout(ctx, PageTimeout)
	defer cancel()

	data, err := s.runner.Audit(pageCtx, AuditRequest{
		ContainerName: meta.ContainerName,
		URL:           LoopbackURL(run.Port, path),
		FormFactor:    run.FormFactor,
		RemotePath:    "/tmp/remote-lh-" + string(newID()) + ".json",
	})
	if err != nil {
		return Report{Path: path, Error: message(err), FetchedAt: s.now().UnixMilli()}
	}
	report, err := Parse(path, data, s.now().UnixMilli())
	if err != nil {
		return Report{Path: path, Error: message(err), FetchedAt: s.now().UnixMilli()}
	}
	return report
}

func (s *Service) prune(ctx context.Context, projectID serviceproject.ID) {
	_, _ = s.repo.Update(ctx, projectID, func(state State) (State, error) {
		if len(state.Runs) <= RetentionCount {
			return state, nil
		}
		sortNewestFirst(state.Runs)
		state.Runs = state.Runs[:RetentionCount]
		return state, nil
	})
}

func (s *Service) append(ctx context.Context, projectID serviceproject.ID, run Run) error {
	_, err := s.repo.Update(ctx, projectID, func(state State) (State, error) {
		state.Runs = append(state.Runs, run)
		return state, nil
	})
	return err
}

func (s *Service) save(ctx context.Context, projectID serviceproject.ID, run Run) error {
	_, err := s.repo.Update(ctx, projectID, func(state State) (State, error) {
		for index, stored := range state.Runs {
			if stored.ID == run.ID {
				state.Runs[index] = run
				return state, nil
			}
		}
		// The run was deleted while it was working. Writing it back would
		// resurrect a record the operator discarded.
		return state, errSuperseded
	})
	return err
}

func (s *Service) record(
	ctx context.Context,
	projectID serviceproject.ID,
	name string,
	meta audit.Meta,
	err error,
) {
	if s == nil || s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Result(
		audit.ActionProjectLighthouse,
		audit.Target{Type: audit.TargetProject, ID: string(projectID), Name: name},
		meta,
		err,
	))
}

func (s *Service) acquire(projectID serviceproject.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, running := s.busy[projectID]; running {
		return false
	}
	s.busy[projectID] = struct{}{}
	return true
}

func (s *Service) release(projectID serviceproject.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.busy, projectID)
}

func (s *Service) isBusy(projectID serviceproject.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, running := s.busy[projectID]
	return running
}

// errSuperseded reports that the record a background run was writing is gone.
// It stops the run quietly rather than surfacing an error nobody can act on.
var errSuperseded = errors.New("lighthouse: run superseded")

func anyMeasured(reports []Report) bool {
	for _, report := range reports {
		if report.Measured() {
			return true
		}
	}
	return false
}

func firstError(reports []Report, ctxErr error) string {
	for _, report := range reports {
		if report.Error != "" {
			return report.Error
		}
	}
	if ctxErr != nil {
		return "the run ran out of time before any page was measured"
	}
	return "no page could be measured"
}

// message keeps the CLI's actionable hint and flattens everything else into
// one line.
func message(err error) string {
	if errors.Is(err, ErrToolingMissing) {
		return ErrToolingMissing.Error()
	}
	text := strings.TrimSpace(err.Error())
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	const limit = 300
	if len(text) > limit {
		return text[:limit] + "…"
	}
	return text
}

func sortNewestFirst(runs []Run) {
	sort.SliceStable(runs, func(left, right int) bool {
		if runs[left].CreatedAt != runs[right].CreatedAt {
			return runs[left].CreatedAt > runs[right].CreatedAt
		}
		return runs[left].ID > runs[right].ID
	})
}

func newID() ID {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return ID(fmt.Sprintf("%016x", time.Now().UnixNano()))
	}
	return ID(hex.EncodeToString(buf))
}
