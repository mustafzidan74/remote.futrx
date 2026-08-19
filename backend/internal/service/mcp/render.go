package mcp

// Rendering the two config dialects this platform writes.
//
// Both renderers are pure and deterministic: entries are emitted in name
// order and every map is emitted in key order, so regenerating from unchanged
// input is byte-identical and the container-side hash check can skip the
// write entirely.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// claudeServer is the shape Claude Code's --mcp-config file expects under
// "mcpServers". The stdio form is the one this repository already ships for
// the Agent Browser (internal/agent/claude/assets/mcp.json); the http form
// follows the same file's documented schema.
type claudeServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type claudeConfig struct {
	MCPServers map[string]claudeServer `json:"mcpServers"`
}

// RenderClaudeConfig renders the JSON document passed to `claude
// --mcp-config`. An empty list renders "" so the caller removes the file
// instead of leaving an empty config behind.
func RenderClaudeConfig(servers []Server) string {
	if len(servers) == 0 {
		return ""
	}
	config := claudeConfig{MCPServers: make(map[string]claudeServer, len(servers))}
	for _, server := range sortedByName(servers) {
		entry := claudeServer{}
		switch server.Transport {
		case TransportHTTP:
			entry.Type = "http"
			entry.URL = server.URL
			entry.Headers = copyMap(server.Headers)
		default:
			entry.Type = "stdio"
			entry.Command = server.Command
			entry.Args = append([]string(nil), server.Args...)
			entry.Env = copyMap(server.Env)
		}
		config.MCPServers[server.Name] = entry
	}
	// encoding/json emits map keys in sorted order, which is what makes this
	// stable across runs.
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		// The struct above contains only strings, slices, and maps of
		// strings; there is no reachable encoding failure.
		return ""
	}
	return string(encoded) + "\n"
}

// RenderCodexRegion renders the platform-owned region of
// /root/.codex/config.toml: one [mcp_servers."<name>"] table per entry. An
// empty list renders "" so the merge drops the region entirely.
//
// The stdio keys (command/args/env) are the ones the codex CLI is already
// driven with elsewhere in this repository (`-c mcp_servers.browser.command`).
// The http keys (url/http_headers) follow the same table and are best-effort:
// a codex build without remote MCP support ignores the table rather than
// failing to start.
func RenderCodexRegion(servers []Server) string {
	if len(servers) == 0 {
		return ""
	}
	var out strings.Builder
	for index, server := range sortedByName(servers) {
		if index > 0 {
			out.WriteByte('\n')
		}
		fmt.Fprintf(&out, "[mcp_servers.%s]\n", tomlString(server.Name))
		switch server.Transport {
		case TransportHTTP:
			fmt.Fprintf(&out, "url = %s\n", tomlString(server.URL))
			if len(server.Headers) > 0 {
				fmt.Fprintf(&out, "\n[mcp_servers.%s.http_headers]\n", tomlString(server.Name))
				writeTOMLTable(&out, server.Headers)
			}
		default:
			fmt.Fprintf(&out, "command = %s\n", tomlString(server.Command))
			if len(server.Args) > 0 {
				fmt.Fprintf(&out, "args = %s\n", tomlStringArray(server.Args))
			}
			if len(server.Env) > 0 {
				fmt.Fprintf(&out, "\n[mcp_servers.%s.env]\n", tomlString(server.Name))
				writeTOMLTable(&out, server.Env)
			}
		}
	}
	return out.String()
}

func writeTOMLTable(out *strings.Builder, values map[string]string) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "%s = %s\n", tomlString(name), tomlString(values[name]))
	}
}

func tomlStringArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, tomlString(value))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tomlString renders a TOML basic string. Table keys go through it too: a
// quoted key is always legal, so a name that would be a bare key is quoted
// anyway rather than deciding per value.
func tomlString(value string) string {
	var out strings.Builder
	out.Grow(len(value) + 2)
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&out, `\u%04X`, r)
				continue
			}
			if r == utf8.RuneError {
				out.WriteString(`�`)
				continue
			}
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
	return out.String()
}

func sortedByName(servers []Server) []Server {
	ordered := append([]Server(nil), servers...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	return ordered
}

func copyMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
