package antigravity

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
)

// parseFixture feeds a recorded agy stream through the parser line by line,
// the way RunProcess does.
func parseFixture(t *testing.T, req agent.RunRequest) ([]agent.Event, *Parser) {
	t.Helper()
	file, err := os.Open("testdata/stream-json-tools.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	parser := NewParser(req)
	var events []agent.Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		parsed, err := parser.ParseLine(scanner.Bytes())
		if err != nil {
			t.Fatalf("ParseLine(%s): %v", scanner.Text(), err)
		}
		events = append(events, parsed...)
	}
	return events, parser
}

func TestRecordedStreamBecomesSessionToolCardsTextAndUsage(t *testing.T) {
	events, parser := parseFixture(t, agent.RunRequest{ConversationID: "chat-1", Model: "gemini-3-pro"})

	var sessions, texts []string
	type toolCall struct {
		name   string
		input  map[string]any
		output string
		done   bool
	}
	tools := map[string]*toolCall{}
	var order []string
	var completed *agent.Event
	for i := range events {
		ev := events[i]
		switch ev.Type {
		case agent.EventSessionUpdated:
			sessions = append(sessions, ev.SessionID)
		case agent.EventAssistantTextDelta:
			texts = append(texts, ev.Text)
		case agent.EventToolStarted:
			var input map[string]any
			if err := json.Unmarshal(ev.Input, &input); err != nil {
				t.Fatalf("tool input is not JSON: %s", ev.Input)
			}
			tools[ev.ItemID] = &toolCall{name: ev.ToolName, input: input}
			order = append(order, ev.ItemID)
		case agent.EventToolCompleted:
			call := tools[ev.ItemID]
			if call == nil {
				t.Fatalf("tool %s completed before it started", ev.ItemID)
			}
			call.output, call.done = ev.Output, true
		case agent.EventRunCompleted:
			completed = &ev
		}
	}

	if len(sessions) != 1 || sessions[0] != "b5bf4cba-fcd0-4a17-8111-da5e0c24ffea" {
		t.Fatalf("sessions = %v, want the stream's conversation id once", sessions)
	}
	if got := strings.Join(texts, ""); got != "done\n" {
		t.Fatalf("reply text = %q", got)
	}

	names := make([]string, 0, len(order))
	for _, id := range order {
		names = append(names, tools[id].name)
		if !tools[id].done {
			t.Fatalf("tool %s (%s) never completed", id, tools[id].name)
		}
	}
	want := []string{"Glob", "Bash", "Write", "Bash", "Read", "Edit"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tool cards = %v, want %v", names, want)
	}
	ls := tools[order[3]]
	if ls.input["command"] != "ls -la" || !strings.Contains(ls.output, "hello.txt") || strings.Contains(ls.output, "\r") {
		t.Fatalf("Bash card = %+v", ls)
	}
	if path := tools[order[5]].input["file_path"]; !strings.HasSuffix(path.(string), "/hello.txt") {
		t.Fatalf("Edit card file_path = %v", path)
	}

	if completed == nil || !parser.Completed() {
		t.Fatal("result line did not complete the run")
	}
	var usage agent.Usage
	if err := json.Unmarshal(completed.Usage, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 89295 || usage.OutputTokens != 1514 || usage.CacheReadTokens != 12181 ||
		usage.ReasoningTokens != 1069 || usage.Turns != 1 || usage.Model != "gemini-3-pro" {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestResumedConversationIsNotReportedAsNew(t *testing.T) {
	events, _ := parseFixture(t, agent.RunRequest{ResumeID: "b5bf4cba-fcd0-4a17-8111-da5e0c24ffea"})
	for _, ev := range events {
		if ev.Type == agent.EventSessionUpdated {
			t.Fatalf("resumed run re-announced its session: %+v", ev)
		}
	}
}

func TestFailedResultAndFailedToolStep(t *testing.T) {
	parser := NewParser(agent.RunRequest{ConversationID: "c"})
	lines := []string{
		`{"event":"step_update","step_update":{"step_index":2,"state":"ERROR","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"false"},"error":"exit status 1"}}}`,
		`{"event":"result","result":{"status":"ERROR","error":"model quota exhausted"}}`,
		`not json from an update notice`,
	}
	var events []agent.Event
	for _, line := range lines[:2] {
		parsed, err := parser.ParseLine([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, parsed...)
	}
	if _, err := parser.ParseLine([]byte(lines[2])); err == nil {
		t.Fatal("a non-JSON line must be reported so the runner can skip it")
	}
	if len(events) != 3 {
		t.Fatalf("events = %+v", events)
	}
	if events[1].Type != agent.EventToolCompleted || !events[1].IsError || events[1].Output != "exit status 1" {
		t.Fatalf("failed tool = %+v", events[1])
	}
	if events[2].Type != agent.EventRunFailed || events[2].Message != "model quota exhausted" {
		t.Fatalf("failed result = %+v", events[2])
	}
}

// A denied tool is the one failure a headless run hits constantly, and agy
// reports it as an object rather than a string. Recorded from agy on this
// platform: a plan-mode run whose read_file had no allow-rule.
func TestDeniedToolCarriesItsMessageInsteadOfFailingTheLine(t *testing.T) {
	parser := NewParser(agent.RunRequest{ConversationID: "c"})
	line := `{"event":"step_update","step_update":{"conversation_id":"5e3c6b96","step_index":2,"state":"ERROR","step_type":"tool","tool_name":"list_dir","duration_seconds":0.23,"tool_info":{"name":"list_dir","parameters":{"DirectoryPath":"/workspace"},"error":{"type":"TOOL_ERROR","message":"permission check failed for read_file \"/workspace\": user denied permission for read_file(/workspace)"}}}}`
	events, err := parser.ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("an object-shaped error must not fail the line: %v", err)
	}
	done := events[len(events)-1]
	if done.Type != agent.EventToolCompleted || !done.IsError {
		t.Fatalf("denied tool = %+v", done)
	}
	if !strings.Contains(done.Output, "user denied permission for read_file") {
		t.Fatalf("denial message lost: %q", done.Output)
	}
}
