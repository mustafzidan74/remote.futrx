package providerpool

import "context"

// Store persists the registry document at DATA_DIR/providers.json.
type Store interface {
	Load(ctx context.Context) (Registry, error)
	Save(ctx context.Context, registry Registry) error
}

// UsageLog is the append-only per-month ledger under
// DATA_DIR/providerpool/usage-YYYY-MM.jsonl.
//
// It is separate from the registry store because the two have opposite write
// patterns: one document rewritten rarely, one line appended per request.
type UsageLog interface {
	Append(ctx context.Context, record UsageRecord) error
	// Scan visits every record in one month, oldest first. It is used once,
	// at startup, to rebuild the day and month counters a restart would
	// otherwise lose; the minute windows are simply allowed to start empty.
	Scan(ctx context.Context, month string, visit func(UsageRecord) bool) error
}

// SecretResolver reads the value behind a Secrets-vault key name. It is the
// port behind a provider's apiKeyRef.
//
// It is deliberately a one-way read that never surfaces over HTTP: the
// registry hands back a mask, and the only thing that ever sees a value is
// the outbound request this package makes.
type SecretResolver interface {
	// Value returns the secret behind one key. A missing key is ("", false,
	// nil) rather than an error: a provider whose key has been deleted from
	// the vault is skipped like any other keyless provider, not a crash.
	Value(ctx context.Context, key string) (string, bool, error)
}

// Completer performs one completion against one resolved provider. It is an
// interface so the tests can assert what was asked for without speaking any
// vendor's wire format, and so a future wire shape is one type rather than a
// change to the pool.
type Completer interface {
	Complete(ctx context.Context, call Call) (CallResult, error)
}
