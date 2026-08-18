package screenshot

import (
	"context"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// Repository is the storage port for the per-project screenshot index.
// Implementations persist atomically and serialize concurrent callers per
// project, exactly like the share and snapshot indexes.
type Repository interface {
	List(ctx context.Context, projectID serviceproject.ID) ([]Screenshot, error)
	// Update hands the project's stored records to fn and persists whatever fn
	// returns. fn runs while the project's index is locked, so callers can
	// read-modify-write without racing another request.
	Update(
		ctx context.Context,
		projectID serviceproject.ID,
		fn func([]Screenshot) ([]Screenshot, error),
	) ([]Screenshot, error)
}

// Blobs is the host-filesystem port holding the PNGs themselves. Every path
// decision below DATA_DIR/screenshots lives behind it, so the service never
// touches the filesystem directly.
type Blobs interface {
	Write(projectID serviceproject.ID, file string, data []byte) error
	Read(projectID serviceproject.ID, file string) ([]byte, error)
	Remove(projectID serviceproject.ID, file string) error
}

// Capturer runs the headless browser inside a project container and returns
// the PNG. Implementations own the container CLI; the service owns the policy
// about what may be photographed.
type Capturer interface {
	// Available reports whether the host can reach a container runtime at all.
	Available() bool
	Capture(ctx context.Context, req CaptureRequest) ([]byte, error)
}

// CaptureRequest is the resolved, validated instruction handed to the browser.
type CaptureRequest struct {
	ContainerName string
	// URL is always in-container loopback; see LoopbackURL.
	URL      string
	Width    int
	Height   int
	FullPage bool
	// RemotePath is the throwaway file the browser writes inside the
	// container. The adapter removes it afterwards.
	RemotePath string
}

// Projects is the lookup the screenshot service needs: a container name to
// shell into and a status to refuse a stopped project with.
type Projects interface {
	Get(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
}

// Image is one picture handed to the notification fan-out.
type Image struct {
	Filename string
	Data     []byte
	Caption  string
	// LinkURL is the login-less link a sink that cannot carry binary content
	// falls back to. Empty means no such sink is configured.
	LinkURL string
}

// Notifier is the outbound port for "send this picture to my phone". It is a
// port rather than a direct dependency on the notification service so this
// package stays free of sink vocabulary.
type Notifier interface {
	// Configured reports whether any sink could receive the picture at all.
	Configured() bool
	// NeedsPublicLink reports whether at least one configured sink can only
	// deliver text, so the service knows whether minting a 24-hour link is
	// justified. Minting one for a Telegram-only install would publish the
	// capture for nothing.
	NeedsPublicLink() bool
	SendImage(ctx context.Context, image Image) []DeliveryResult
}
