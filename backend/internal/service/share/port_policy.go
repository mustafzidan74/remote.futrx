package share

import configconstants "github.com/futrx-com/remote.futrx.com/internal/config/constants"

// ShareablePort reports whether port may back a public preview link.
func ShareablePort(port int) error {
	if port < configconstants.ProjectPreviewMinPort || port > configconstants.ProjectPreviewMaxPort {
		return ErrInvalidPort
	}

	// These in-container listeners are platform plumbing, not user
	// applications. Public access to any of them would bypass its intended edge
	// authorization boundary.
	switch port {
	case configconstants.ProjectPreviewAgentBrowserPort,
		configconstants.ProjectPreviewIDEProxyPort,
		configconstants.ProjectPreviewIDEDirectPort,
		configconstants.ProjectPreviewBrowserDevToolsPort,
		// The files preview serves all of /workspace, secrets mirrored into
		// .env among them; a public link would publish them.
		configconstants.ProjectPreviewFilesPort:
		return ErrPortNotShareable
	default:
		return nil
	}
}
