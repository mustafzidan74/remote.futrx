package share

import (
	"errors"
	"testing"

	configconstants "github.com/futrx-com/remote.futrx.com/internal/config/constants"
)

func TestPlatformListenersCannotBackAPublicLink(t *testing.T) {
	for _, port := range []int{
		configconstants.ProjectPreviewAgentBrowserPort,
		configconstants.ProjectPreviewIDEProxyPort,
		configconstants.ProjectPreviewIDEDirectPort,
		configconstants.ProjectPreviewBrowserDevToolsPort,
		// Serves all of /workspace, including the .env secrets mirror.
		configconstants.ProjectPreviewFilesPort,
	} {
		if err := ShareablePort(port); !errors.Is(err, ErrPortNotShareable) {
			t.Fatalf("ShareablePort(%d) = %v, want ErrPortNotShareable", port, err)
		}
	}
	if err := ShareablePort(5173); err != nil {
		t.Fatalf("ShareablePort(5173) = %v, want nil for an application port", err)
	}
}
