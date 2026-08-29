package lighthouse

import (
	"context"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// Repository is the storage port for one project's audit history.
//
// There are no blobs: the parsed summary is a few kilobytes and lives in the
// index itself. Lighthouse's own 300 KB report is deliberately not kept — see
// the package comment.
type Repository interface {
	Load(ctx context.Context, projectID serviceproject.ID) (State, error)
	// Update hands the stored state to fn and persists what fn returns. fn
	// runs while the project's record is locked, so a background run writing
	// its latest page cannot lose a delete made by the request beside it.
	Update(
		ctx context.Context,
		projectID serviceproject.ID,
		fn func(State) (State, error),
	) (State, error)
}

// Runner drives the Lighthouse CLI inside a project's container. The adapter
// owns the container CLI; this service owns the policy about what may be
// audited and what is kept.
type Runner interface {
	// Available reports whether the host can reach a container runtime at all.
	Available() bool
	// Installed reports whether one container has the CLI.
	Installed(ctx context.Context, containerName string) (bool, error)
	// Install adds it to a container that predates the image that ships it.
	Install(ctx context.Context, containerName string) error
	Audit(ctx context.Context, req AuditRequest) ([]byte, error)
}

// AuditRequest is the resolved, validated instruction handed to the CLI.
type AuditRequest struct {
	ContainerName string
	// URL is always in-container loopback; see LoopbackURL.
	URL        string
	FormFactor FormFactor
	// RemotePath is the throwaway file the CLI writes inside the container.
	// The adapter removes it afterwards.
	RemotePath string
}

// Projects is the lookup this service needs: a container name to shell into,
// and a status that lets it refuse a stopped project up front rather than
// after a browser timeout.
type Projects interface {
	Get(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
}

// LoopbackURL is the address the in-container browser is pointed at.
//
// It is loopback on purpose: Lighthouse runs in the same container as the app,
// so this reaches the dev server directly and never meets the platform's
// sign-in page or the edge's share-token check. It also means the numbers are
// the application's own, with no proxy, no TLS handshake and no public network
// in the measurement.
func LoopbackURL(port int, path string) string {
	if path == "" {
		path = "/"
	}
	return "http://127.0.0.1:" + itoa(port) + path
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := [8]byte{}
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
