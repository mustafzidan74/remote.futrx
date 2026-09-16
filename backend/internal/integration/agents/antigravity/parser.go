package antigravity

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
)

// Parser converts `agy --print --output-format stream-json` NDJSON into
// normalized agent events. The stream has three line kinds:
//
//	{"event":"init","conversation_id":"…","init":{…}}
//	{"event":"step_update","step_update":{"step_index":4,"state":"ACTIVE|DONE","step_type":"tool|agent_response|user_input",…}}
//	{"event":"result","result":{"status":"SUCCESS","response":"…","usage":{…}}}
//
// A tool step arrives ACTIVE (with its parameters) and again DONE (with its
// output); agent_response steps carry the reply text as text_delta. Tool names
// are agy's own (run_command, view_file, replace_file_content, …) and are
// mapped onto the names the chat already renders as rich cards. The shape was
// recorded from agy on this platform; see testdata/stream-json-tools.jsonl.
type Parser struct {
	req            agent.RunRequest
	sessionEmitted bool
	sessionID      string
	startedTools   map[int]bool
	finishedTools  map[int]bool
	completed      bool
}

// SessionID is the conversation the stream reported, resumed or new.
func (p *Parser) SessionID() string {
	if p.sessionID != "" {
		return p.sessionID
	}
	return p.req.ResumeID
}

func NewParser(req agent.RunRequest) *Parser {
	if req.Provider == "" {
		req.Provider = agent.ProviderAntigravity
	}
	return &Parser{req: req, startedTools: map[int]bool{}, finishedTools: map[int]bool{}}
}

// Completed reports whether the stream ended with a result line, so the
// provider knows whether it still has to close the run itself.
func (p *Parser) Completed() bool {
	return p.completed
}

type wireLine struct {
	Event          string      `json:"event"`
	ConversationID string      `json:"conversation_id"`
	StepUpdate     *wireStep   `json:"step_update"`
	Result         *wireResult `json:"result"`
}

type wireStep struct {
	ConversationID string        `json:"conversation_id"`
	StepIndex      int           `json:"step_index"`
	State          string        `json:"state"`
	StepType       string        `json:"step_type"`
	ToolName       string        `json:"tool_name"`
	ToolInfo       *wireToolInfo `json:"tool_info"`
	TextDelta      string        `json:"text_delta"`
	Error          wireError     `json:"error"`
}

type wireToolInfo struct {
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
	Output     *string        `json:"output"`
	Error      wireError      `json:"error"`
}

type wireResult struct {
	ConversationID  string    `json:"conversation_id"`
	Status          string    `json:"status"`
	Response        string    `json:"response"`
	Error           wireError `json:"error"`
	DurationSeconds float64   `json:"duration_seconds"`
	NumTurns        int64     `json:"num_turns"`
	Usage           wireUsage `json:"usage"`
}

// wireError is agy's error field, which is a bare string on some lines and an
// object on others — a denied tool arrives as
// {"type":"TOOL_ERROR","message":"permission check failed for read_file …"}.
// Decoding it as a string made the whole line fail to parse, so the step that
// failed never reached the UI and its card sat there looking like it was still
// running. Anything unrecognised is kept verbatim rather than dropped.
type wireError string

func (e *wireError) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		*e = ""
		return nil
	}
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*e = wireError(asString)
		return nil
	}
	var asObject struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Error   string `json:"error"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(data, &asObject); err == nil {
		if message := firstNonEmpty(asObject.Message, asObject.Error, asObject.Detail, asObject.Type); message != "" {
			*e = wireError(message)
			return nil
		}
	}
	*e = wireError(text)
	return nil
}

type wireUsage struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ThinkingTokens  int64 `json:"thinking_tokens"`
	CacheReadTokens int64 `json:"cache_read_tokens"`
}

func (p *Parser) ParseLine(line []byte) ([]agent.Event, error) {
	var raw wireLine
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}
	switch raw.Event {
	case "init":
		return p.session(raw.ConversationID), nil
	case "step_update":
		if raw.StepUpdate == nil {
			return nil, nil
		}
		return p.step(*raw.StepUpdate), nil
	case "result":
		if raw.Result == nil {
			return nil, nil
		}
		return p.result(*raw.Result), nil
	default:
		return nil, nil
	}
}

func (p *Parser) event(kind agent.EventType) agent.Event {
	return agent.Event{
		T:              time.Now().UnixMilli(),
		Type:           kind,
		Provider:       agent.ProviderAntigravity,
		ConversationID: p.req.ConversationID,
	}
}

func (p *Parser) session(id string) []agent.Event {
	id = strings.TrimSpace(id)
	if id != "" {
		p.sessionID = id
	}
	if id == "" || p.sessionEmitted || id == p.req.ResumeID {
		return nil
	}
	p.sessionEmitted = true
	ev := p.event(agent.EventSessionUpdated)
	ev.SessionID = id
	return []agent.Event{ev}
}

func (p *Parser) step(step wireStep) []agent.Event {
	events := p.session(step.ConversationID)
	switch step.StepType {
	case "agent_response":
		if step.TextDelta != "" {
			ev := p.event(agent.EventAssistantTextDelta)
			ev.ItemKind = agent.ItemMessage
			ev.ItemID = fmt.Sprintf("agy-step-%d", step.StepIndex)
			ev.Text = step.TextDelta
			events = append(events, ev)
		}
	case "tool":
		events = append(events, p.tool(step)...)
	}
	return events
}

func (p *Parser) tool(step wireStep) []agent.Event {
	name := step.ToolName
	var params map[string]any
	var output *string
	errText := string(step.Error)
	if step.ToolInfo != nil {
		if name == "" {
			name = step.ToolInfo.Name
		}
		params = step.ToolInfo.Parameters
		output = step.ToolInfo.Output
		if errText == "" {
			errText = string(step.ToolInfo.Error)
		}
	}
	itemID := fmt.Sprintf("agy-step-%d", step.StepIndex)

	var events []agent.Event
	if !p.startedTools[step.StepIndex] {
		p.startedTools[step.StepIndex] = true
		mappedName, input := mapTool(name, params)
		started := p.event(agent.EventToolStarted)
		started.ItemKind = agent.ItemToolCall
		started.ItemID = itemID
		started.ToolName = mappedName
		started.Input = input
		events = append(events, started)
	}
	if step.State == "ACTIVE" {
		return events
	}
	p.finishedTools[step.StepIndex] = true
	done := p.event(agent.EventToolCompleted)
	done.ItemKind = agent.ItemToolCall
	done.ItemID = itemID
	if output != nil {
		done.Output = strings.ReplaceAll(*output, "\r\n", "\n")
	}
	if errText != "" || (step.State != "DONE" && step.State != "") {
		done.IsError = true
		if done.Output == "" {
			done.Output = firstNonEmpty(errText, strings.ToLower(step.State))
		}
	}
	return append(events, done)
}

// FailOpenTools closes every tool card that started but never finished. agy
// can stop mid-step — a headless run that hits a permission it cannot ask for
// exits without reporting the step — and a card left open reads as a tool
// that is still running.
func (p *Parser) FailOpenTools(reason string) []agent.Event {
	var open []int
	for index := range p.startedTools {
		if !p.finishedTools[index] {
			open = append(open, index)
		}
	}
	sort.Ints(open)
	events := make([]agent.Event, 0, len(open))
	for _, index := range open {
		p.finishedTools[index] = true
		done := p.event(agent.EventToolCompleted)
		done.ItemKind = agent.ItemToolCall
		done.ItemID = fmt.Sprintf("agy-step-%d", index)
		done.IsError = true
		done.Output = reason
		events = append(events, done)
	}
	return events
}

func (p *Parser) result(result wireResult) []agent.Event {
	events := p.session(result.ConversationID)
	p.completed = true
	if !strings.EqualFold(result.Status, "SUCCESS") {
		failed := p.event(agent.EventRunFailed)
		failed.Message = firstNonEmpty(string(result.Error), strings.TrimSpace(result.Response), "agy run ended with status "+result.Status)
		return append(events, failed)
	}
	completed := p.event(agent.EventRunCompleted)
	completed.Usage = agent.Usage{
		InputTokens:     result.Usage.InputTokens,
		OutputTokens:    result.Usage.OutputTokens,
		CacheReadTokens: result.Usage.CacheReadTokens,
		ReasoningTokens: result.Usage.ThinkingTokens,
		DurationMs:      int64(result.DurationSeconds * 1000),
		Turns:           result.NumTurns,
		Model:           p.req.Model,
	}.Raw()
	return append(events, completed)
}

// mapTool renames agy tools and their parameters to the Claude-shaped names
// the chat renders as dedicated cards (Bash, Read, Write, Edit, Grep, Glob).
// The original parameters travel along, so nothing agy reported is dropped.
func mapTool(name string, params map[string]any) (string, json.RawMessage) {
	input := map[string]any{}
	for key, value := range params {
		input[key] = value
	}
	set := func(to, from string) {
		if value, ok := params[from]; ok {
			input[to] = value
		}
	}
	mapped := name
	switch name {
	case "run_command":
		mapped = "Bash"
		set("command", "CommandLine")
	case "view_file":
		mapped = "Read"
		set("file_path", "AbsolutePath")
	case "write_to_file":
		mapped = "Write"
		set("file_path", "TargetFile")
		set("content", "CodeContent")
	case "replace_file_content", "multi_replace_file_content", "sed_file":
		mapped = "Edit"
		set("file_path", "TargetFile")
		set("old_string", "TargetContent")
		set("new_string", "ReplacementContent")
	case "grep_search":
		mapped = "Grep"
		set("pattern", "Query")
		set("path", "SearchPath")
	case "find_by_name":
		mapped = "Glob"
		set("pattern", "Pattern")
		set("path", "SearchDirectory")
	case "list_dir":
		mapped = "Glob"
		set("path", "DirectoryPath")
		if _, ok := input["pattern"]; !ok {
			input["pattern"] = "*"
		}
	case "search_web":
		mapped = "WebSearch"
		set("query", "query")
		set("query", "Query")
	case "read_url_content":
		mapped = "WebFetch"
		set("url", "Url")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return mapped, json.RawMessage(`{}`)
	}
	return mapped, encoded
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
