package project

import (
	"context"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// Option configures optional Service collaborators without widening New's
// required argument list.
type Option func(*Service)

// WithAudit attaches the audit recorder. Project mutations, container
// lifecycle transitions, membership edits, and secret reads are recorded
// through it; a nil recorder keeps the service silent.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// WithGlobalSecrets attaches the platform secrets vault. Without it a project
// container receives only the project's own secrets, which is exactly how the
// platform behaved before the vault existed.
func WithGlobalSecrets(secrets GlobalSecrets) Option {
	return func(s *Service) { s.globalSecrets = secrets }
}

// auditTargetID labels a project the caller named but whose metadata was not
// (or could not be) loaded.
func auditTargetID(id ID) audit.Target {
	return audit.Target{Type: audit.TargetProject, ID: string(id)}
}

// auditProjectTarget labels a project by the id the caller asked for, adding
// whatever name could still be resolved. The name is captured at write time so
// a deleted or renamed project stays identifiable in the trail.
func auditProjectTarget(id ID, m Meta) audit.Target {
	return audit.Target{Type: audit.TargetProject, ID: string(id), Name: m.Name}
}

func (s *Service) record(ctx context.Context, action string, target audit.Target, meta audit.Meta, err error) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Result(action, target, meta, err))
}
