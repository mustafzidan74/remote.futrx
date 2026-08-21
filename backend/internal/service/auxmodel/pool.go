package auxmodel

import "context"

// The free-tier provider pool, seen from here.
//
// The auxiliary model started as one endpoint — the operator's local Ollama —
// and that is still what it is for a job set to "local". A job set to "pool"
// instead goes through the platform's pool of third-party free tiers, which
// picks a provider, counts what it spent, and moves to the next one when a
// quota runs out.
//
// This package deliberately does not import the pool. It declares the two
// things it needs and lets the composition root wire the real service in, so
// the dependency points one way and a deployment without a pool is a nil
// field rather than a build tag.

// PoolRequest is one job handed to the pool.
type PoolRequest struct {
	// Job is the ledger label, so the pool's usage log can say which chore
	// spent the tokens.
	Job string
	// Capability narrows which of a provider's models may take the job:
	// "text", "code", or "bulk".
	Capability   string
	SystemPrompt string
	UserText     string
	MaxTokens    int
}

// Pool is the narrow view of the provider pool this package uses.
type Pool interface {
	// Available reports whether any provider could take a job right now.
	Available() bool
	// Complete runs one job through the pool. Which provider answered, and
	// how many refused first, is the pool's business — this package only
	// needs the text.
	Complete(ctx context.Context, request PoolRequest) (string, error)
}

// WithPool attaches the provider pool. Leaving it unset means every job set
// to "pool" quietly falls back to the local endpoint, which is exactly the
// behaviour a deployment with no pool should have.
func WithPool(pool Pool) Option {
	return func(s *Service) {
		if pool != nil {
			s.pool = pool
		}
	}
}

// capabilityForJob maps a platform chore onto the kind of model that should
// take it. A commit subject is code-shaped; everything else here is ordinary
// prose, and translation is the one that most wants a capable model rather
// than the cheapest one available.
func capabilityForJob(job Job) string {
	switch job {
	case JobCommitMessage:
		return "code"
	default:
		return "text"
	}
}

// poolAvailable reports whether the pool could take anything.
func (s *Service) poolAvailable() bool {
	return s != nil && s.pool != nil && s.pool.Available()
}
