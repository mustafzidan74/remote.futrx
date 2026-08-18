package agentprefs

import "errors"

// ErrUnavailable reports a deployment with no preferences store. Handlers map
// it to 503 rather than pretending a write succeeded.
var ErrUnavailable = errors.New("agent preferences are unavailable")
