package antigravity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAllowRulesAreAddedWithoutTouchingTheOperatorsSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"trustedWorkspaces":["/workspace"],"telemetry":false,"permissions":{"allow":["command(git status)"],"deny":["command(rm)"]}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := addAllowRules(path, readOnlyAllowRules); err != nil {
		t.Fatal(err)
	}

	var got struct {
		TrustedWorkspaces []string `json:"trustedWorkspaces"`
		Telemetry         *bool    `json:"telemetry"`
		Permissions       struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("settings no longer JSON: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(got.TrustedWorkspaces, []string{"/workspace"}) || got.Telemetry == nil || *got.Telemetry {
		t.Fatalf("operator settings changed: %s", data)
	}
	wantAllow := append([]string{"command(git status)"}, readOnlyAllowRules...)
	if !reflect.DeepEqual(got.Permissions.Allow, wantAllow) {
		t.Fatalf("allow = %v, want %v", got.Permissions.Allow, wantAllow)
	}
	if !reflect.DeepEqual(got.Permissions.Deny, []string{"command(rm)"}) {
		t.Fatalf("deny changed: %v", got.Permissions.Deny)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}

	// A second pass has nothing to add and must leave the file alone.
	before, _ := os.ReadFile(path)
	if err := addAllowRules(path, readOnlyAllowRules); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("second pass rewrote the file:\n%s\n%s", before, after)
	}
}

func TestAllowRulesLeaveMissingOrUnreadableSettingsAlone(t *testing.T) {
	dir := t.TempDir()
	if err := addAllowRules(filepath.Join(dir, "absent.json"), readOnlyAllowRules); err != nil {
		t.Fatalf("a host that never signed in must not fail a run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "absent.json")); !os.IsNotExist(err) {
		t.Fatal("must not create settings before sign-in")
	}

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte(`{"trustedWorkspaces":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := addAllowRules(broken, readOnlyAllowRules); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(broken); string(data) != `{"trustedWorkspaces":` {
		t.Fatalf("broken settings were rewritten: %s", data)
	}
}
