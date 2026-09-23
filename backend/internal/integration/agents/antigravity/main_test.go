package antigravity

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the host settings file at a scratch copy, so a test run —
// as root on a real host, or as anyone in CI — never reads or edits the
// operator's actual Antigravity settings.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "antigravity-settings-")
	if err != nil {
		panic(err)
	}
	hostSettingsPath = filepath.Join(dir, "settings.json")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
