// Package antigravity adapts Google's Antigravity CLI (`agy`) as a headless
// agent provider. The CLI has a print mode but no structured event stream, so
// runs surface as plain streaming text; conversation ids are recovered from
// the CLI's on-disk brain directory because print mode never emits them
// (github.com/google-antigravity/antigravity-cli issue #7).
package antigravity

import (
	"fmt"
	"os"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
)

// containerAgentHome is HOME inside a project container: agy keeps its state
// (auth fallback tokens, conversation brain) under $HOME/.gemini.
const containerAgentHome = "/root"

// stateDirUnderHome is where agy stores conversations and headless auth state.
const stateDirUnderHome = ".gemini/antigravity-cli"

// The two files that make a container signed in. Everything else under the
// state directory — logs, the conversation database, the brain, the per-install
// id — is local to one container and must not travel.
//
//   - antigravity-oauth-token is the credential: {"auth_method", "token"}.
//   - settings.json is not a credential, but a container without it stops on
//     agy's first-run "do you trust this folder?" prompt, which a headless run
//     cannot answer. It also carries the telemetry choice, which should follow
//     the operator's decision rather than be re-asked per project.
//
// Verified on this platform: seeding these two into a container that had never
// run agy made `agy --print` answer on the first try.
const (
	hostStateDir      = "/root/" + stateDirUnderHome
	containerStateDir = "/root/" + stateDirUnderHome

	authTokenFile = "/antigravity-oauth-token"
	settingsFile  = "/settings.json"
)

// releaseBaseURL contains version-addressed Antigravity CLI assets.
const releaseBaseURL = "https://github.com/google-antigravity/antigravity-cli/releases/download"

// containerInstructions is agy's global rules file. Verified on this platform:
// a rule placed there is followed in a workspace that has no rules of its own.
const (
	containerInstructions     = "/root/.gemini/GEMINI.md"
	containerInstructionsHash = "/root/.gemini/.agents-md.sha256"
)

var antigravityProfile = provisioning.Profile{
	ID: string(agent.ProviderAntigravity),
	Instructions: &provisioning.InstructionTarget{
		Path:     containerInstructions,
		HashPath: containerInstructionsHash,
	},
	CLI: provisioning.CLISpec{
		Name:               "antigravity",
		ImageLabel:         "antigravity",
		Binary:             "agy",
		VersionArgs:        []string{"--version"},
		Version:            provisioning.MustCLIVersion("ANTIGRAVITY_CLI_VERSION"),
		ReportVersion:      true,
		CheckVersion:       true,
		VerifyAfterInstall: true,
		InstallMode:        provisioning.InstallWithScript,
		InstallScript: installScript(
			provisioning.MustCLIVersion("ANTIGRAVITY_CLI_VERSION"),
			provisioning.MustPin("ANTIGRAVITY_LINUX_X64_SHA512"),
			provisioning.MustPin("ANTIGRAVITY_LINUX_ARM64_SHA512"),
		),
		InstallTimeout: 8 * time.Minute,
		WaitTimeout:    5 * time.Minute,
	},
	// Sign in once, in any project, and every other container inherits it.
	//
	// Google documents no token path, so this was established by observation:
	// a login writes exactly one credential file into the state directory. The
	// platform pulls it up to the host after a successful run and seeds it into
	// containers on launch, the same shape as codex and kimi.
	//
	// Nothing here is Required in either direction, which is deliberate. Before
	// the first sign-in the host has no token and a launch must still succeed —
	// the per-container `agy` login is what an operator does *next*, and a hard
	// failure would take away the only route to getting a credential at all.
	// After a run, a container the operator never signed into has no file to
	// pull, and that is also not an error.
	Credentials: provisioning.CredentialSpec{
		Name:         "antigravity",
		HostDir:      hostStateDir,
		ContainerDir: containerStateDir,
		Files: []provisioning.CredentialFile{
			{
				HostPath:      hostStateDir + authTokenFile,
				ContainerPath: containerStateDir + authTokenFile,
				Mode:          "600",
			},
			{
				HostPath:      hostStateDir + settingsFile,
				ContainerPath: containerStateDir + settingsFile,
				Mode:          "600",
			},
		},
		SeedOnLaunch: true,
	},
	// qa keeps the state directory on a per-project persistent device so a
	// sign-in survives the container being replaced. The two are compatible:
	// the seed above writes into that directory on every launch, so the host's
	// credential still wins and "sign in once, everywhere" still holds.
	PersistentState: []provisioning.PersistentDirectory{{
		Device:        "antigravity-home",
		HostDirectory: "antigravity",
		ContainerPath: "/root/" + stateDirUnderHome,
	}},
}

// Profile returns Antigravity's provisioning policy as a defensive copy.
func Profile() provisioning.Profile {
	return antigravityProfile.Clone()
}

// installScript downloads the pinned agy release for the target execution
// environment's architecture, verifies its repository-pinned checksum, and installs
// the path supplied by the execution environment. Container builds retain the
// /usr/local/bin/agy default. It never consults Antigravity's moving latest
// manifest.
func installScript(version, linuxX64SHA512, linuxARM64SHA512 string) string {
	return fmt.Sprintf(`set -euo pipefail
install_path="${FUTRX_HOST_CLI_INSTALL_PATH:-/usr/local/bin/agy}"
if [ -x "$install_path" ] && [ "$("$install_path" --version 2>/dev/null)" = %[1]q ]; then
    exit 0
fi
case "$(uname -m)" in
    x86_64|amd64)
        asset="agy_cli_linux_x64.tar.gz"
        sha512=%[3]q
        ;;
    aarch64|arm64)
        asset="agy_cli_linux_arm64.tar.gz"
        sha512=%[4]q
        ;;
    *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
url="%[2]s/%[1]s/${asset}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url" -o "$tmp/agy.tar.gz"
echo "${sha512}  $tmp/agy.tar.gz" | sha512sum -c - >/dev/null
tar -xzf "$tmp/agy.tar.gz" -C "$tmp" antigravity
install -d -m 0755 "$(dirname "$install_path")"
install -m 0755 "$tmp/antigravity" "$install_path"
"$install_path" --version`, version, releaseBaseURL, linuxX64SHA512, linuxARM64SHA512)
}

// Authenticated reports whether the host holds a captured sign-in, which is
// what makes every container inherit one. It reads the file's presence and
// never its contents: this answers a settings page, and a settings page has no
// business touching a token.
func Authenticated() bool {
	info, err := os.Stat(hostStateDir + authTokenFile)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

// SignInHint is the one-time instruction shown next to a disconnected status.
const SignInHint = signInHint
