package screenshot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// Service is the policy layer over screenshots: it decides what may be
// photographed, bounds the capture, keeps the per-project archive small, and
// mints the one login-less link a text-only chat sink needs.
type Service struct {
	repo     Repository
	blobs    Blobs
	capturer Capturer
	projects Projects
	notifier Notifier
	audit    audit.Recorder
	baseURL  string
	now      func() time.Time
}

// Option customizes a Service at construction.
type Option func(*Service)

// WithAudit attaches the audit recorder.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// WithClock replaces the wall clock so timestamps and expiry are testable.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithBaseURL supplies the public origin the minted link is built on. Without
// it the service still captures and stores; it just cannot hand out a
// login-less link, so a text-only sink gets the picture's caption alone.
func WithBaseURL(baseURL string) Option {
	return func(s *Service) { s.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/") }
}

// WithNotifier enables the "send it to my phone" half of the feature.
func WithNotifier(notifier Notifier) Option {
	return func(s *Service) { s.notifier = notifier }
}

func New(repo Repository, blobs Blobs, capturer Capturer, projects Projects, options ...Option) *Service {
	service := &Service{
		repo:     repo,
		blobs:    blobs,
		capturer: capturer,
		projects: projects,
		audit:    audit.Nop{},
		now:      time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Available reports whether stored captures can be listed, read, and sent.
// It deliberately says nothing about the container runtime: a host whose `lxc`
// is missing or wedged can take no new pictures, but the ones it already has
// are files on disk and must keep being served — otherwise the session-gated
// route would be less available than the login-less public link.
func (s *Service) Available() bool {
	return s != nil && s.repo != nil && s.blobs != nil && s.projects != nil
}

// CanCapture reports whether a new picture can be taken right now, which is
// Available plus a reachable container runtime.
func (s *Service) CanCapture() bool {
	return s.Available() && s.capturer != nil && s.capturer.Available()
}

// NotificationsConfigured reports whether the "send it" buttons have anywhere
// to send to, so the UI can hide them rather than offer a guaranteed failure.
func (s *Service) NotificationsConfigured() bool {
	return s != nil && s.notifier != nil && s.notifier.Configured()
}

// ReadPath is the session-gated route one stored capture is served on. It
// lives here so the record's own URL and the handler's route cannot drift.
func ReadPath(projectID serviceproject.ID, id ID) string {
	return "/api/projects/" + url.PathEscape(string(projectID)) + "/screenshots/" + url.PathEscape(string(id)) + ".png"
}

// PublicPath is the login-less route a minted link is served on. The token
// carries the project id as its first segment, so resolving a link needs no
// global index: hex project ids contain no "-", which makes the split
// unambiguous.
func PublicPath(token string) string {
	return "/s/screenshot/" + url.PathEscape(token) + ".png"
}

// List returns one project's captures, newest first, with their read URLs
// filled in and their link digests stripped.
func (s *Service) List(ctx context.Context, projectID serviceproject.ID) ([]Screenshot, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return nil, serviceproject.ErrInvalidID
	}
	records, err := s.repo.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	sortNewestFirst(records)
	out := make([]Screenshot, 0, len(records))
	for _, record := range records {
		out = append(out, s.decorate(projectID, record))
	}
	return out, nil
}

// Open returns the stored PNG for one capture. The caller has already been
// checked for project membership.
func (s *Service) Open(ctx context.Context, projectID serviceproject.ID, id ID) ([]byte, Screenshot, error) {
	if s == nil || s.repo == nil || s.blobs == nil {
		return nil, Screenshot{}, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return nil, Screenshot{}, serviceproject.ErrInvalidID
	}
	records, err := s.repo.List(ctx, projectID)
	if err != nil {
		return nil, Screenshot{}, err
	}
	record, ok := find(records, id)
	if !ok {
		return nil, Screenshot{}, ErrNotFound
	}
	data, err := s.blobs.Read(projectID, record.File)
	if err != nil {
		return nil, Screenshot{}, ErrNotFound
	}
	return data, s.decorate(projectID, record), nil
}

// Capture photographs one preview port and stores the result.
//
// It is deliberately synchronous. Unlike a snapshot, a capture is seconds of
// work whose whole point is that the user is looking at it right now: a
// pending record they have to poll for would be worse than waiting.
func (s *Service) Capture(
	ctx context.Context,
	projectID serviceproject.ID,
	in CaptureInput,
	actor string,
) (CaptureResult, error) {
	result, err := s.capture(ctx, projectID, in, actor)
	s.record(ctx, audit.ActionProjectScreenshot, audit.Target{
		Type: audit.TargetProject,
		ID:   string(projectID),
		Name: string(result.Screenshot.ID),
	}, audit.Meta{
		"port":        in.Port,
		"path":        in.Path,
		"fullPage":    in.FullPage,
		"notify":      in.Notify,
		"bytes":       result.Screenshot.Bytes,
		"notifyError": result.NotifyError,
	}, err)
	return result, err
}

func (s *Service) capture(
	ctx context.Context,
	projectID serviceproject.ID,
	in CaptureInput,
	actor string,
) (CaptureResult, error) {
	if !s.CanCapture() {
		return CaptureResult{}, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return CaptureResult{}, serviceproject.ErrInvalidID
	}
	normalized, err := in.Normalize()
	if err != nil {
		return CaptureResult{}, err
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return CaptureResult{}, err
	}
	if meta.Status != serviceproject.StatusRunning || meta.ContainerName == "" {
		return CaptureResult{}, ErrNotRunning
	}

	id := newID()
	captureCtx, cancel := context.WithTimeout(ctx, CaptureTimeout)
	defer cancel()
	data, err := s.capturer.Capture(captureCtx, CaptureRequest{
		ContainerName: meta.ContainerName,
		URL:           LoopbackURL(normalized.Port, normalized.Path),
		Width:         normalized.Width,
		Height:        normalized.Height,
		FullPage:      normalized.FullPage,
		RemotePath:    "/tmp/remote-shot-" + string(id) + ".png",
	})
	if err != nil {
		return CaptureResult{}, err
	}
	if len(data) > MaxBytes {
		return CaptureResult{}, ErrTooLarge
	}
	// The bytes came back through a container CLI, so "is this really a PNG?"
	// is also the check that nothing else ended up on that stream.
	width, height, err := DecodePNGSize(data)
	if err != nil {
		return CaptureResult{}, err
	}

	now := s.now().UnixMilli()
	record := Screenshot{
		ID:        id,
		File:      fileName(now, id),
		Port:      normalized.Port,
		Path:      normalized.Path,
		Width:     width,
		Height:    height,
		FullPage:  normalized.FullPage,
		Bytes:     int64(len(data)),
		CreatedBy: strings.ToLower(strings.TrimSpace(actor)),
		CreatedAt: now,
	}
	if err := s.blobs.Write(projectID, record.File, data); err != nil {
		return CaptureResult{}, fmt.Errorf("store screenshot: %w", err)
	}
	if err := s.append(ctx, projectID, record); err != nil {
		// The index is the source of truth; an orphaned PNG would never be
		// listed, pruned, or served, so it goes with the failed record.
		_ = s.blobs.Remove(projectID, record.File)
		return CaptureResult{}, err
	}

	result := CaptureResult{
		Screenshot:    s.decorate(projectID, record),
		Notifications: s.NotificationsConfigured(),
	}
	if !normalized.Notify {
		return result, nil
	}
	delivered, publicURL, err := s.deliver(ctx, projectID, record, meta, data)
	if err != nil {
		// The picture exists, is stored, and has already spent one of the
		// project's retention slots. A sink that would not take it is a second
		// outcome to report, not a reason to pretend the capture never
		// happened — which is what returning an error here would mean.
		result.NotifyError = err.Error()
		return result, nil
	}
	result.Delivered = delivered
	result.PublicURL = publicURL
	return result, nil
}

// Send pushes an already-stored capture out through the notification sinks.
//
// It is separate from Capture because "send me that picture" and "take another
// picture" are different requests: re-capturing to deliver would photograph a
// later moment than the one the user is looking at.
func (s *Service) Send(
	ctx context.Context,
	projectID serviceproject.ID,
	id ID,
) (CaptureResult, error) {
	result, err := s.send(ctx, projectID, id)
	s.record(ctx, audit.ActionProjectScreenshot, audit.Target{
		Type: audit.TargetProject,
		ID:   string(projectID),
		Name: string(id),
	}, audit.Meta{"notify": true, "resend": true}, err)
	return result, err
}

func (s *Service) send(
	ctx context.Context,
	projectID serviceproject.ID,
	id ID,
) (CaptureResult, error) {
	if !s.Available() {
		return CaptureResult{}, ErrUnavailable
	}
	data, record, err := s.Open(ctx, projectID, id)
	if err != nil {
		return CaptureResult{}, err
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return CaptureResult{}, err
	}
	delivered, publicURL, err := s.deliver(ctx, projectID, record, meta, data)
	if err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{
		Screenshot:    record,
		Delivered:     delivered,
		PublicURL:     publicURL,
		Notifications: true,
	}, nil
}

// deliver fans one capture out over the notification sinks, minting a
// login-less link only when a sink that cannot carry pictures is configured.
func (s *Service) deliver(
	ctx context.Context,
	projectID serviceproject.ID,
	record Screenshot,
	meta serviceproject.Meta,
	data []byte,
) ([]DeliveryResult, string, error) {
	if s.notifier == nil || !s.notifier.Configured() {
		return nil, "", ErrNoNotification
	}
	publicURL := ""
	if s.notifier.NeedsPublicLink() {
		link, err := s.MintLink(ctx, projectID, record.ID)
		if err != nil {
			return nil, "", err
		}
		publicURL = link
	}
	image := Image{
		Filename: record.File,
		Data:     data,
		Caption:  Caption(meta, record),
		LinkURL:  publicURL,
	}
	return s.notifier.SendImage(ctx, image), publicURL, nil
}

// Caption is the one line that travels with the picture. It names the project
// and the exact address photographed, because a screenshot forwarded to a
// phone with no context is just a picture of a web page.
func Caption(meta serviceproject.Meta, record Screenshot) string {
	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = strings.TrimSpace(meta.Slug)
	}
	target := fmt.Sprintf(":%d%s", record.Port, record.Path)
	if name == "" {
		return "Preview " + target
	}
	return name + " — preview " + target
}

// MintLink issues (or re-issues) the 24-hour login-less link for one capture.
// The plaintext token is returned exactly once; only its digest is stored.
func (s *Service) MintLink(ctx context.Context, projectID serviceproject.ID, id ID) (string, error) {
	if !s.Available() {
		return "", ErrUnavailable
	}
	if s.baseURL == "" {
		return "", nil
	}
	secret, err := newToken()
	if err != nil {
		return "", err
	}
	token := string(projectID) + "-" + secret
	expires := s.now().Add(PublicLinkTTL).UnixMilli()

	updated := false
	_, err = s.repo.Update(ctx, projectID, func(records []Screenshot) ([]Screenshot, error) {
		for i := range records {
			if records[i].ID != id {
				continue
			}
			records[i].LinkTokenHash = hashToken(token)
			records[i].LinkExpiresAt = expires
			updated = true
			return records, nil
		}
		return nil, ErrNotFound
	})
	if err != nil {
		return "", err
	}
	if !updated {
		return "", ErrNotFound
	}
	return s.baseURL + PublicPath(token), nil
}

// ResolveLink answers a login-less request. Nothing about it consults the
// session: the token is the whole credential, which is why it is compared in
// constant time and why an expired one is refused before any file is read.
func (s *Service) ResolveLink(ctx context.Context, token string) ([]byte, Screenshot, error) {
	if s == nil || s.repo == nil || s.blobs == nil {
		return nil, Screenshot{}, ErrUnavailable
	}
	projectID, ok := projectFromToken(token)
	if !ok {
		return nil, Screenshot{}, ErrNotFound
	}
	records, err := s.repo.List(ctx, projectID)
	if err != nil {
		return nil, Screenshot{}, ErrNotFound
	}
	digest := hashToken(token)
	now := s.now().UnixMilli()
	for _, record := range records {
		if record.LinkTokenHash == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(record.LinkTokenHash), []byte(digest)) != 1 {
			continue
		}
		if !record.LinkActive(now) {
			return nil, Screenshot{}, ErrLinkExpired
		}
		data, readErr := s.blobs.Read(projectID, record.File)
		if readErr != nil {
			return nil, Screenshot{}, ErrNotFound
		}
		return data, record.Public(), nil
	}
	return nil, Screenshot{}, ErrNotFound
}

// append stores one record and evicts everything past the retention count.
func (s *Service) append(ctx context.Context, projectID serviceproject.ID, record Screenshot) error {
	var evicted []string
	_, err := s.repo.Update(ctx, projectID, func(records []Screenshot) ([]Screenshot, error) {
		next := append([]Screenshot{}, records...)
		next = append(next, record)
		sortNewestFirst(next)
		if len(next) > RetentionCount {
			for _, dropped := range next[RetentionCount:] {
				evicted = append(evicted, dropped.File)
			}
			next = next[:RetentionCount]
		}
		return next, nil
	})
	if err != nil {
		return err
	}
	// The index no longer references these, so a failed unlink costs disk, not
	// correctness — it must not fail the capture the user is waiting for.
	for _, file := range evicted {
		_ = s.blobs.Remove(projectID, file)
	}
	return nil
}

func (s *Service) decorate(projectID serviceproject.ID, record Screenshot) Screenshot {
	out := record.Public()
	out.URL = ReadPath(projectID, record.ID)
	return out
}

func (s *Service) record(
	ctx context.Context,
	action string,
	target audit.Target,
	meta audit.Meta,
	err error,
) {
	if s == nil || s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Result(action, target, meta, err))
}

func find(records []Screenshot, id ID) (Screenshot, bool) {
	for _, record := range records {
		if record.ID == id {
			return record, true
		}
	}
	return Screenshot{}, false
}

func sortNewestFirst(records []Screenshot) {
	sort.SliceStable(records, func(left, right int) bool {
		if records[left].CreatedAt != records[right].CreatedAt {
			return records[left].CreatedAt > records[right].CreatedAt
		}
		return records[left].ID > records[right].ID
	})
}

// fileName is "<unix-ms>-<id>.png": the timestamp makes the directory readable
// to a human browsing DATA_DIR, and the id keeps two captures taken in the
// same millisecond from colliding.
func fileName(createdAt int64, id ID) string {
	return fmt.Sprintf("%d-%s.png", createdAt, id)
}

func newID() ID {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return ID(fmt.Sprintf("%016x", time.Now().UnixNano()))
	}
	return ID(hex.EncodeToString(buf))
}

func newToken() (string, error) {
	buf := make([]byte, TokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate screenshot link token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// projectFromToken splits "<projectID>-<secret>". Project ids are lowercase
// hex, so the first "-" is always the boundary.
func projectFromToken(token string) (serviceproject.ID, bool) {
	index := strings.Index(token, "-")
	if index <= 0 {
		return "", false
	}
	id := serviceproject.ID(token[:index])
	if !serviceproject.ValidID(id) || token[index+1:] == "" {
		return "", false
	}
	return id, true
}
