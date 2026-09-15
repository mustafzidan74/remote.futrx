package antigravity

import "testing"

// agy reads its global rules from ~/.gemini/GEMINI.md. Without this target the
// platform instructions (durable dev servers, routed preview URLs) never
// reach Antigravity runs.
func TestProfilePushesPlatformInstructionsToAgyGlobalRules(t *testing.T) {
	target := Profile().Instructions
	if target == nil {
		t.Fatal("antigravity profile has no instructions target")
	}
	if target.Path != "/root/.gemini/GEMINI.md" {
		t.Fatalf("instructions path = %q, want /root/.gemini/GEMINI.md", target.Path)
	}
	if target.HashPath == "" {
		t.Fatal("instructions target has no hash path, so every run would rewrite it")
	}
}
