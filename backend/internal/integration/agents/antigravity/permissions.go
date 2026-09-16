package antigravity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
)

// readOnlyAllowRules let a headless run read the workspace without asking.
//
// A run in Plan mode deliberately goes without --dangerously-skip-permissions,
// and a headless agy cannot ask, so every tool it tried was denied: the model
// could not open a single file, spent minutes retrying, and apologised for a
// permission prompt nobody ever saw. Verified on this platform: with these
// rules a plan-mode run lists and reads /workspace, while write_file and
// run_command are still refused and nothing is written.
var readOnlyAllowRules = []string{
	"read_file",
	"read_file(/workspace)",
	"read_file(/workspace/**)",
}

// ensureReadOnlyPermissions adds readOnlyAllowRules to the host settings.json
// that every project container is seeded from. It runs before each seed, so a
// sign-in that replaces the file, or a container that writes its own copy back,
// cannot drop the rules for long.
//
// It only ever adds entries: the operator's trusted folders, telemetry choice
// and any rules of their own stay as they are. A host that has not signed in
// yet has no file, and a file that is not valid JSON is left for the operator —
// neither is a reason to fail a run.
func ensureReadOnlyPermissions(provisioning.Profile) error {
	return addAllowRules(hostStateDir+settingsFile, readOnlyAllowRules)
}

func addAllowRules(path string, rules []string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read antigravity settings: %w", err)
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(data, &settings) != nil || settings == nil {
		return nil
	}
	var permissions map[string]json.RawMessage
	if raw, ok := settings["permissions"]; ok && json.Unmarshal(raw, &permissions) != nil {
		return nil
	}
	if permissions == nil {
		permissions = map[string]json.RawMessage{}
	}
	var allow []string
	if raw, ok := permissions["allow"]; ok && json.Unmarshal(raw, &allow) != nil {
		return nil
	}

	changed := false
	for _, rule := range rules {
		if !containsString(allow, rule) {
			allow = append(allow, rule)
			changed = true
		}
	}
	if !changed {
		return nil
	}

	if permissions["allow"], err = json.Marshal(allow); err != nil {
		return err
	}
	if settings["permissions"], err = json.Marshal(permissions); err != nil {
		return err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		return err
	}
	return writeFileAtomic(path, out.Bytes())
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return fmt.Errorf("write antigravity settings: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write antigravity settings: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write antigravity settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write antigravity settings: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
