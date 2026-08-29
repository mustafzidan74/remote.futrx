package visualdiff

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
)

// Service is the policy layer over visual comparison: it decides what may be
// photographed, runs the captures in the background, diffs them, and keeps the
// per-project archive small.
type Service struct {
	repo     Repository
	blobs    Blobs
	capturer Capturer
	projects Projects
	audit    audit.Recorder
	now      func() time.Time

	// busy holds the projects with a run in flight. A second run against the
	// same project would photograph a container already under a browser and
	// report the interference as a change, so it is refused rather than
	// queued: the operator wants the comparison they asked for, not one taken
	// while something else was loading pages.
	mu   sync.Mutex
	busy map[serviceproject.ID]struct{}
	// wait lets a shutdown drain runs that are mid-flight instead of leaving
	// half-written records claiming to be running forever.
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

func New(repo Repository, blobs Blobs, capturer Capturer, projects Projects, options ...Option) *Service {
	service := &Service{
		repo:     repo,
		blobs:    blobs,
		capturer: capturer,
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

// Available reports whether comparisons can be run on this host.
func (s *Service) Available() bool {
	return s != nil && s.repo != nil && s.blobs != nil &&
		s.capturer != nil && s.capturer.Available() && s.projects != nil
}

// Wait blocks until every in-flight run has finished. Shutdown calls it so a
// stored record never outlives the process still claiming to be running.
func (s *Service) Wait() {
	if s != nil {
		s.wait.Wait()
	}
}

// ReadPath is the session-gated route one stored image is served on. It lives
// here so a record's own URL and the handler's route cannot drift.
func ReadPath(projectID serviceproject.ID, file string) string {
	return "/api/projects/" + url.PathEscape(string(projectID)) + "/visual/images/" + url.PathEscape(file)
}

// Overview returns everything the project's visual panel renders.
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

	out := Overview{Comparisons: make([]Comparison, 0, len(state.Comparisons)), Running: s.isBusy(projectID)}
	if state.Baseline != nil {
		decorated := s.decorateBaseline(projectID, *state.Baseline)
		out.Baseline = &decorated
	}
	comparisons := append([]Comparison(nil), state.Comparisons...)
	sortNewestFirst(comparisons)
	for _, comparison := range comparisons {
		out.Comparisons = append(out.Comparisons, s.decorateComparison(projectID, comparison))
	}
	return out, nil
}

// Image returns one stored PNG. The caller has already been checked for
// project membership.
//
// The file name is matched against the project's own records rather than
// joined onto a directory: the only readable files are the ones this service
// wrote, so a crafted name has nothing to reach for.
func (s *Service) Image(ctx context.Context, projectID serviceproject.ID, file string) ([]byte, error) {
	if !s.Available() {
		return nil, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return nil, serviceproject.ErrInvalidID
	}
	state, err := s.repo.Load(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !knownFile(state, file) {
		return nil, ErrNotFound
	}
	data, err := s.blobs.Read(projectID, file)
	if err != nil {
		return nil, ErrNotFound
	}
	return data, nil
}

// SetBaseline photographs the given pages and makes them the project's new
// reference.
//
// The previous baseline and every comparison against it are discarded in the
// same write. Keeping them would leave the panel showing percentages measured
// against an image that is no longer what "correct" means, which is worse than
// showing nothing: the operator would read those numbers as current.
func (s *Service) SetBaseline(
	ctx context.Context,
	projectID serviceproject.ID,
	in BaselineInput,
	actor string,
) (Baseline, error) {
	baseline, err := s.setBaseline(ctx, projectID, in, actor)
	s.record(ctx, projectID, string(baseline.ID), audit.Meta{
		"kind":  "baseline",
		"port":  in.Port,
		"pages": len(in.Paths),
	}, err)
	return baseline, err
}

func (s *Service) setBaseline(
	ctx context.Context,
	projectID serviceproject.ID,
	in BaselineInput,
	actor string,
) (Baseline, error) {
	normalized, meta, err := s.prepare(ctx, projectID, in)
	if err != nil {
		return Baseline{}, err
	}
	if !s.acquire(projectID) {
		return Baseline{}, ErrBusy
	}

	baseline := Baseline{
		ID:        newID(),
		Status:    StatusRunning,
		Port:      normalized.Port,
		Paths:     normalized.Paths,
		Width:     normalized.Width,
		Height:    normalized.Height,
		FullPage:  normalized.FullPage,
		Threshold: normalized.Threshold,
		Pages:     make([]Shot, 0, len(normalized.Paths)),
		CreatedBy: strings.ToLower(strings.TrimSpace(actor)),
		CreatedAt: s.now().UnixMilli(),
	}
	// The old images go once the new record is stored, not before: a crash
	// between the two leaves orphaned files, which is recoverable, where the
	// other order leaves a baseline pointing at deleted images, which is not.
	previous, err := s.replaceBaseline(ctx, projectID, baseline)
	if err != nil {
		s.release(projectID)
		return Baseline{}, err
	}
	s.removeFiles(projectID, previous)

	s.wait.Add(1)
	go func() {
		defer s.wait.Done()
		defer s.release(projectID)
		// The run outlives the request that started it: twelve page loads is
		// minutes of work, and a browser tab closing must not abandon it.
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), RunTimeout)
		defer cancel()
		s.runBaseline(runCtx, projectID, meta, baseline)
	}()
	return baseline, nil
}

// Compare re-photographs the baseline's pages and diffs them against it.
func (s *Service) Compare(
	ctx context.Context,
	projectID serviceproject.ID,
	label string,
	actor string,
) (Comparison, error) {
	comparison, err := s.compare(ctx, projectID, label, actor)
	s.record(ctx, projectID, string(comparison.ID), audit.Meta{
		"kind":  "comparison",
		"label": label,
	}, err)
	return comparison, err
}

func (s *Service) compare(
	ctx context.Context,
	projectID serviceproject.ID,
	label string,
	actor string,
) (Comparison, error) {
	if !s.Available() {
		return Comparison{}, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return Comparison{}, serviceproject.ErrInvalidID
	}
	state, err := s.repo.Load(ctx, projectID)
	if err != nil {
		return Comparison{}, err
	}
	if state.Baseline == nil || state.Baseline.Status != StatusReady {
		return Comparison{}, ErrNoBaseline
	}
	baseline := *state.Baseline

	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return Comparison{}, err
	}
	if meta.Status != serviceproject.StatusRunning || meta.ContainerName == "" {
		return Comparison{}, ErrNotRunning
	}
	if !s.acquire(projectID) {
		return Comparison{}, ErrBusy
	}

	comparison := Comparison{
		ID:         newID(),
		BaselineID: baseline.ID,
		Status:     StatusRunning,
		Label:      trimLabel(label),
		Pages:      make([]Diff, 0, len(baseline.Pages)),
		CreatedBy:  strings.ToLower(strings.TrimSpace(actor)),
		CreatedAt:  s.now().UnixMilli(),
	}
	if err := s.appendComparison(ctx, projectID, comparison); err != nil {
		s.release(projectID)
		return Comparison{}, err
	}

	s.wait.Add(1)
	go func() {
		defer s.wait.Done()
		defer s.release(projectID)
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), RunTimeout)
		defer cancel()
		s.runComparison(runCtx, projectID, meta, baseline, comparison)
	}()
	return comparison, nil
}

// Delete removes one comparison and its images.
func (s *Service) Delete(ctx context.Context, projectID serviceproject.ID, id ID) error {
	if !s.Available() {
		return ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return serviceproject.ErrInvalidID
	}
	var removed []string
	_, err := s.repo.Update(ctx, projectID, func(state State) (State, error) {
		kept := make([]Comparison, 0, len(state.Comparisons))
		found := false
		for _, comparison := range state.Comparisons {
			if comparison.ID == id {
				found = true
				removed = append(removed, comparisonFiles(comparison)...)
				continue
			}
			kept = append(kept, comparison)
		}
		if !found {
			return state, ErrNotFound
		}
		state.Comparisons = kept
		return state, nil
	})
	if err != nil {
		return err
	}
	for _, file := range removed {
		_ = s.blobs.Remove(projectID, file)
	}
	return nil
}

// prepare is the validation both entry points share.
func (s *Service) prepare(
	ctx context.Context,
	projectID serviceproject.ID,
	in BaselineInput,
) (BaselineInput, serviceproject.Meta, error) {
	if !s.Available() {
		return BaselineInput{}, serviceproject.Meta{}, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return BaselineInput{}, serviceproject.Meta{}, serviceproject.ErrInvalidID
	}
	normalized, err := in.Normalize()
	if err != nil {
		return BaselineInput{}, serviceproject.Meta{}, err
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return BaselineInput{}, serviceproject.Meta{}, err
	}
	if meta.Status != serviceproject.StatusRunning || meta.ContainerName == "" {
		return BaselineInput{}, serviceproject.Meta{}, ErrNotRunning
	}
	return normalized, meta, nil
}

// runBaseline photographs every path and stores the finished record.
func (s *Service) runBaseline(
	ctx context.Context,
	projectID serviceproject.ID,
	meta serviceproject.Meta,
	baseline Baseline,
) {
	for _, path := range baseline.Paths {
		shot := Shot{Path: path}
		data, err := s.capture(ctx, meta, baseline, path)
		switch {
		case err != nil:
			shot.Error = captureMessage(err)
		default:
			file := imageFile(baseline.ID, "base", path)
			if width, height, decodeErr := servicescreenshot.DecodePNGSize(data); decodeErr != nil {
				shot.Error = decodeErr.Error()
			} else if writeErr := s.blobs.Write(projectID, file, data); writeErr != nil {
				shot.Error = writeErr.Error()
			} else {
				shot.File, shot.Width, shot.Height, shot.Bytes = file, width, height, int64(len(data))
			}
		}
		baseline.Pages = append(baseline.Pages, shot)

		// The record is written after every page so the panel fills in while
		// the run is still going. A twelve-page run that only reports at the
		// end looks identical to one that has hung.
		if err := s.saveBaseline(ctx, projectID, baseline); err != nil {
			return
		}
		if ctx.Err() != nil {
			break
		}
	}

	baseline.FinishedAt = s.now().UnixMilli()
	baseline.Status = StatusReady
	// A baseline where nothing could be photographed is a failure. One where
	// some pages worked is not: the operator can still compare the pages that
	// did, and the failures are named on the record.
	if !anyCaptured(baseline.Pages) {
		baseline.Status = StatusFailed
		baseline.Error = firstError(baseline.Pages, ctx.Err())
	}
	_ = s.saveBaseline(ctx, projectID, baseline)
}

// runComparison re-photographs the baseline's captured pages and diffs each
// against its stored image.
func (s *Service) runComparison(
	ctx context.Context,
	projectID serviceproject.ID,
	meta serviceproject.Meta,
	baseline Baseline,
	comparison Comparison,
) {
	for _, page := range baseline.Pages {
		// A page the baseline never managed to photograph has nothing to
		// compare against, so it is skipped rather than reported as 100%
		// changed — which is what comparing against a missing image would
		// otherwise say.
		if !page.Captured() {
			continue
		}
		comparison.Pages = append(comparison.Pages, s.diffPage(ctx, projectID, meta, baseline, comparison.ID, page))
		summarize(&comparison)
		if err := s.saveComparison(ctx, projectID, comparison); err != nil {
			return
		}
		if ctx.Err() != nil {
			break
		}
	}

	comparison.FinishedAt = s.now().UnixMilli()
	comparison.Status = StatusReady
	if len(comparison.Pages) == 0 || allFailed(comparison.Pages) {
		comparison.Status = StatusFailed
		comparison.Error = firstDiffError(comparison.Pages, ctx.Err())
	}
	summarize(&comparison)
	if err := s.saveComparison(ctx, projectID, comparison); err != nil {
		return
	}
	s.prune(ctx, projectID)
}

func (s *Service) diffPage(
	ctx context.Context,
	projectID serviceproject.ID,
	meta serviceproject.Meta,
	baseline Baseline,
	comparisonID ID,
	page Shot,
) Diff {
	out := Diff{Path: page.Path, BeforeFile: page.File}

	before, err := s.blobs.Read(projectID, page.File)
	if err != nil {
		out.Error = "the baseline image for this page is missing"
		return out
	}
	after, err := s.capture(ctx, meta, baseline, page.Path)
	if err != nil {
		out.Error = captureMessage(err)
		return out
	}

	result, err := Compare(before, after, baseline.Threshold)
	if err != nil {
		out.Error = err.Error()
		return out
	}

	afterFile := imageFile(comparisonID, "after", page.Path)
	diffFile := imageFile(comparisonID, "diff", page.Path)
	if err := s.blobs.Write(projectID, afterFile, after); err != nil {
		out.Error = err.Error()
		return out
	}
	if err := s.blobs.Write(projectID, diffFile, result.DiffPNG); err != nil {
		// The "after" image alone is still worth keeping: the operator can
		// look at the two pictures even when the overlay could not be stored.
		out.Error = err.Error()
		out.AfterFile = afterFile
		return out
	}

	out.AfterFile = afterFile
	out.DiffFile = diffFile
	out.ChangedPercent = result.ChangedPercent
	out.ChangedPixels = result.ChangedPixels
	out.Width = result.Width
	out.Height = result.Height
	out.SizeChanged = result.SizeChanged
	return out
}

// capture photographs one page under the baseline's fixed viewport.
func (s *Service) capture(
	ctx context.Context,
	meta serviceproject.Meta,
	baseline Baseline,
	path string,
) ([]byte, error) {
	pageCtx, cancel := context.WithTimeout(ctx, PageTimeout)
	defer cancel()
	return s.capturer.Capture(pageCtx, servicescreenshot.CaptureRequest{
		ContainerName: meta.ContainerName,
		URL:           servicescreenshot.LoopbackURL(baseline.Port, path),
		Width:         baseline.Width,
		Height:        baseline.Height,
		FullPage:      baseline.FullPage,
		RemotePath:    "/tmp/remote-visual-" + string(newID()) + ".png",
	})
}

// prune drops the oldest comparisons and their images once the archive is over
// its retention count.
func (s *Service) prune(ctx context.Context, projectID serviceproject.ID) {
	var removed []string
	_, err := s.repo.Update(ctx, projectID, func(state State) (State, error) {
		if len(state.Comparisons) <= RetentionCount {
			return state, nil
		}
		sortNewestFirst(state.Comparisons)
		for _, comparison := range state.Comparisons[RetentionCount:] {
			removed = append(removed, comparisonFiles(comparison)...)
		}
		state.Comparisons = state.Comparisons[:RetentionCount]
		return state, nil
	})
	if err != nil {
		return
	}
	for _, file := range removed {
		_ = s.blobs.Remove(projectID, file)
	}
}

// replaceBaseline installs a new baseline and returns the files the old one
// and its comparisons owned.
func (s *Service) replaceBaseline(
	ctx context.Context,
	projectID serviceproject.ID,
	baseline Baseline,
) ([]string, error) {
	var orphaned []string
	_, err := s.repo.Update(ctx, projectID, func(state State) (State, error) {
		if state.Baseline != nil {
			for _, page := range state.Baseline.Pages {
				if page.File != "" {
					orphaned = append(orphaned, page.File)
				}
			}
		}
		for _, comparison := range state.Comparisons {
			orphaned = append(orphaned, comparisonFiles(comparison)...)
		}
		return State{Baseline: &baseline}, nil
	})
	return orphaned, err
}

func (s *Service) saveBaseline(ctx context.Context, projectID serviceproject.ID, baseline Baseline) error {
	_, err := s.repo.Update(ctx, projectID, func(state State) (State, error) {
		// A baseline replaced while this run was in flight wins: the operator
		// asked for the newer one, and writing this one back would resurrect
		// the reference they just discarded.
		if state.Baseline == nil || state.Baseline.ID != baseline.ID {
			return state, errSuperseded
		}
		state.Baseline = &baseline
		return state, nil
	})
	return err
}

func (s *Service) appendComparison(ctx context.Context, projectID serviceproject.ID, comparison Comparison) error {
	_, err := s.repo.Update(ctx, projectID, func(state State) (State, error) {
		state.Comparisons = append(state.Comparisons, comparison)
		return state, nil
	})
	return err
}

func (s *Service) saveComparison(ctx context.Context, projectID serviceproject.ID, comparison Comparison) error {
	_, err := s.repo.Update(ctx, projectID, func(state State) (State, error) {
		for index, stored := range state.Comparisons {
			if stored.ID == comparison.ID {
				state.Comparisons[index] = comparison
				return state, nil
			}
		}
		return state, errSuperseded
	})
	return err
}

func (s *Service) removeFiles(projectID serviceproject.ID, files []string) {
	for _, file := range files {
		_ = s.blobs.Remove(projectID, file)
	}
}

func (s *Service) decorateBaseline(projectID serviceproject.ID, baseline Baseline) Baseline {
	pages := make([]Shot, 0, len(baseline.Pages))
	for _, page := range baseline.Pages {
		if page.File != "" {
			page.URL = ReadPath(projectID, page.File)
		}
		pages = append(pages, page)
	}
	baseline.Pages = pages
	return baseline
}

func (s *Service) decorateComparison(projectID serviceproject.ID, comparison Comparison) Comparison {
	pages := make([]Diff, 0, len(comparison.Pages))
	for _, page := range comparison.Pages {
		if page.BeforeFile != "" {
			page.BeforeURL = ReadPath(projectID, page.BeforeFile)
		}
		if page.AfterFile != "" {
			page.AfterURL = ReadPath(projectID, page.AfterFile)
		}
		if page.DiffFile != "" {
			page.DiffURL = ReadPath(projectID, page.DiffFile)
		}
		pages = append(pages, page)
	}
	comparison.Pages = pages
	return comparison
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
		audit.ActionProjectVisualDiff,
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

// errSuperseded reports that the record a background run was writing is no
// longer the one the project holds. It stops the run quietly rather than
// surfacing an error nobody can act on.
var errSuperseded = errors.New("visualdiff: record superseded")

// summarize recomputes the two stored headline numbers from the pages.
func summarize(comparison *Comparison) {
	changed := 0
	worst := 0.0
	for _, page := range comparison.Pages {
		if page.Changed() {
			changed++
		}
		if page.Error == "" && page.ChangedPercent > worst {
			worst = page.ChangedPercent
		}
	}
	comparison.ChangedPages = changed
	comparison.MaxChangedPercent = worst
}

func anyCaptured(pages []Shot) bool {
	for _, page := range pages {
		if page.Captured() {
			return true
		}
	}
	return false
}

func allFailed(pages []Diff) bool {
	for _, page := range pages {
		if page.Error == "" {
			return false
		}
	}
	return true
}

func firstError(pages []Shot, ctxErr error) string {
	for _, page := range pages {
		if page.Error != "" {
			return page.Error
		}
	}
	if ctxErr != nil {
		return "the run ran out of time before any page was photographed"
	}
	return "no page could be photographed"
}

func firstDiffError(pages []Diff, ctxErr error) string {
	for _, page := range pages {
		if page.Error != "" {
			return page.Error
		}
	}
	if ctxErr != nil {
		return "the run ran out of time before any page was compared"
	}
	return "no page could be compared"
}

// captureMessage keeps the browser's own actionable hints and flattens
// everything else into one line.
func captureMessage(err error) string {
	if errors.Is(err, servicescreenshot.ErrToolingMissing) {
		return servicescreenshot.ErrToolingMissing.Error()
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

func knownFile(state State, file string) bool {
	if file == "" {
		return false
	}
	if state.Baseline != nil {
		for _, page := range state.Baseline.Pages {
			if page.File == file {
				return true
			}
		}
	}
	for _, comparison := range state.Comparisons {
		for _, page := range comparison.Pages {
			if page.AfterFile == file || page.DiffFile == file || page.BeforeFile == file {
				return true
			}
		}
	}
	return false
}

func comparisonFiles(comparison Comparison) []string {
	files := make([]string, 0, len(comparison.Pages)*2)
	for _, page := range comparison.Pages {
		// BeforeFile belongs to the baseline, so it is never listed here: a
		// comparison being deleted must not take the reference with it.
		if page.AfterFile != "" {
			files = append(files, page.AfterFile)
		}
		if page.DiffFile != "" {
			files = append(files, page.DiffFile)
		}
	}
	return files
}

func sortNewestFirst(comparisons []Comparison) {
	sort.SliceStable(comparisons, func(left, right int) bool {
		if comparisons[left].CreatedAt != comparisons[right].CreatedAt {
			return comparisons[left].CreatedAt > comparisons[right].CreatedAt
		}
		return comparisons[left].ID > comparisons[right].ID
	})
}

func trimLabel(label string) string {
	trimmed := strings.TrimSpace(label)
	const limit = 120
	if len(trimmed) > limit {
		return trimmed[:limit]
	}
	return trimmed
}

// imageFile names one stored PNG. The path is folded into the name so a
// directory listing is readable, and hashed rather than embedded so a page at
// /products/some-very-long-slug cannot produce a file name the filesystem
// refuses.
func imageFile(owner ID, kind, path string) string {
	return fmt.Sprintf("%s-%s-%s.png", owner, kind, pathKey(path))
}

func pathKey(path string) string {
	sum := uint64(1469598103934665603)
	for index := 0; index < len(path); index++ {
		sum ^= uint64(path[index])
		sum *= 1099511628211
	}
	return fmt.Sprintf("%016x", sum)
}

func newID() ID {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return ID(fmt.Sprintf("%016x", time.Now().UnixNano()))
	}
	return ID(hex.EncodeToString(buf))
}
