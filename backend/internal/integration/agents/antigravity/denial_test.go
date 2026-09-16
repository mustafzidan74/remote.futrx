package antigravity

import (
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
)

// Recorded from agy on this platform: a plan-mode run that tried
// `wp plugin list` exited 0 with this on stderr and no result line.
const recordedDenial = `jetski: no output produced — a tool required the "command" permission that headless mode cannot prompt for, so it was auto-denied. Add an allow-rule under permissions.allow in settings.json (e.g. command(<target>)). Alternatively, re-run with --dangerously-skip-permissions to auto-approve all tools.`

func TestDeniedPermissionIsReadFromAgysStderr(t *testing.T) {
	permission, denied := deniedPermission("some log line\n" + recordedDenial + "\n")
	if !denied || permission != "command" {
		t.Fatalf("got %q, %v", permission, denied)
	}
	if _, denied := deniedPermission("jetski: update available\n"); denied {
		t.Fatal("unrelated stderr read as a denial")
	}
}

func TestPermissionStopReasonNamesWhatToChange(t *testing.T) {
	if got := permissionStopReason(agent.RunModePlan, "command"); !strings.Contains(got, "run a command") || !strings.Contains(got, "Turn off Plan mode") {
		t.Fatalf("plan/command = %q", got)
	}
	if got := permissionStopReason(agent.RunModePlan, "write_file"); !strings.Contains(got, "change something") {
		t.Fatalf("plan/write = %q", got)
	}
	if got := permissionStopReason("", "write_file"); !strings.Contains(got, "write_file permission") {
		t.Fatalf("default = %q", got)
	}
}

func TestFailOpenToolsClosesOnlyTheCardsStillRunning(t *testing.T) {
	parser := NewParser(agent.RunRequest{ConversationID: "c"})
	lines := []string{
		`{"event":"step_update","step_update":{"step_index":3,"state":"ACTIVE","step_type":"tool","tool_name":"view_file","tool_info":{"name":"view_file","parameters":{"AbsolutePath":"/workspace/AGENTS.md"}}}}`,
		`{"event":"step_update","step_update":{"step_index":3,"state":"DONE","step_type":"tool","tool_name":"view_file","tool_info":{"name":"view_file","output":"ok"}}}`,
		`{"event":"step_update","step_update":{"step_index":5,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"wp plugin list"}}}}`,
	}
	for _, line := range lines {
		if _, err := parser.ParseLine([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	events := parser.FailOpenTools("stopped")
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	if ev := events[0]; ev.Type != agent.EventToolCompleted || ev.ItemID != "agy-step-5" || !ev.IsError || ev.Output != "stopped" {
		t.Fatalf("closed card = %+v", ev)
	}
	if again := parser.FailOpenTools("stopped"); len(again) != 0 {
		t.Fatalf("a card was closed twice: %+v", again)
	}
}
