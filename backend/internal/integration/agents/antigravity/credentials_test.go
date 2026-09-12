package antigravity

import (
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
)

// TestCredentialSpecCarriesOnlyWhatTravels pins the two files that make a
// container signed in, and — more importantly — that nothing else does.
//
// The state directory also holds logs, a conversation database, the brain, and
// a per-install id. Those belong to one container. Copying them between
// projects would mix one project's history into another's, so the risk this
// test guards is a well-meaning widening of the list.
func TestCredentialSpecCarriesOnlyWhatTravels(t *testing.T) {
	spec := Profile().Credentials

	if spec.Empty() {
		t.Fatal("antigravity no longer syncs anything: a sign-in would be stuck in whichever container made it")
	}
	if len(spec.Files) != 2 {
		t.Fatalf("syncing %d files, want exactly the token and settings.json: %+v", len(spec.Files), spec.Files)
	}

	wantPaths := map[string]bool{
		"/root/.gemini/antigravity-cli/antigravity-oauth-token": false,
		"/root/.gemini/antigravity-cli/settings.json":           false,
	}
	for _, file := range spec.Files {
		seen, known := wantPaths[file.ContainerPath]
		if !known {
			t.Errorf("unexpected file in the sync set: %s", file.ContainerPath)
			continue
		}
		if seen {
			t.Errorf("%s is listed twice", file.ContainerPath)
		}
		wantPaths[file.ContainerPath] = true

		if file.HostPath != file.ContainerPath {
			t.Errorf("%s: host path %q differs from the container path; agy resolves both from $HOME",
				file.ContainerPath, file.HostPath)
		}
		if file.Mode != "600" {
			t.Errorf("%s: mode %q, want 600 — one of these is a credential", file.ContainerPath, file.Mode)
		}
	}
	for path, seen := range wantPaths {
		if !seen {
			t.Errorf("%s is not synced", path)
		}
	}
}

// TestNothingIsRequiredInEitherDirection covers the bootstrap, which is the
// part that is easy to get wrong.
//
// Before anyone has signed in, the host has no token. If the push were
// required, launching a container would fail — and the terminal sign-in that
// creates the token in the first place happens *inside* a container, so the
// feature would have locked out its own setup path. The pull is optional for
// the mirror-image reason: a project the operator never signed into has no
// file to give back, and that is ordinary, not an error.
func TestNothingIsRequiredInEitherDirection(t *testing.T) {
	for _, file := range Profile().Credentials.Files {
		if file.PushRequired {
			t.Errorf("%s is PushRequired: a container could not launch before the first sign-in", file.ContainerPath)
		}
		if file.PullRequired {
			t.Errorf("%s is PullRequired: a run in a project that never signed in would report a failure", file.ContainerPath)
		}
	}
}

// TestSeedOnLaunchIsSet is the difference between "sign in once" and "sign in
// once per project": without it a container starts with no credential and the
// operator is sent back to the terminal.
func TestSeedOnLaunchIsSet(t *testing.T) {
	if !Profile().Credentials.SeedOnLaunch {
		t.Fatal("SeedOnLaunch is off, so a new container would not inherit the sign-in")
	}
}

// TestTheSignInHintSaysItIsOnlyOnce guards the sentence an operator reads when
// a run fails unauthenticated. It used to say sign-in was per workspace, which
// stopped being true when the credential started travelling.
func TestTheSignInHintSaysItIsOnlyOnce(t *testing.T) {
	if !strings.Contains(signInHint, "agy") {
		t.Error("the hint no longer names the command to run")
	}
	if !strings.Contains(strings.ToLower(signInHint), "once") {
		t.Error("the hint should say the sign-in is needed only once")
	}
}

// TestProfileIsACopy repeats the guarantee the package documents: callers
// compose profiles, and one caller's edit must not reach the definition.
func TestProfileIsACopy(t *testing.T) {
	first := Profile()
	first.Credentials.Files = append(first.Credentials.Files, provisioning.CredentialFile{
		ContainerPath: "/root/.gemini/antigravity-cli/conversation_summaries.db",
	})
	if len(Profile().Credentials.Files) != 2 {
		t.Fatal("mutating a returned profile changed the package's definition")
	}
}
