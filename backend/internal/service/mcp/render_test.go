package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderClaudeConfig(t *testing.T) {
	tests := []struct {
		name    string
		servers []Server
		want    string
	}{
		{
			name:    "no servers render nothing so the file is removed",
			servers: nil,
			want:    "",
		},
		{
			name: "a stdio server carries command, args, and env",
			servers: []Server{{
				Name:      "playwright",
				Transport: TransportStdio,
				Command:   "npx",
				Args:      []string{"@playwright/mcp@latest"},
				Env:       map[string]string{"PW_HEADLESS": "1"},
			}},
			want: `{
  "mcpServers": {
    "playwright": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "@playwright/mcp@latest"
      ],
      "env": {
        "PW_HEADLESS": "1"
      }
    }
  }
}
`,
		},
		{
			name: "an http server carries url and headers",
			servers: []Server{{
				Name:      "jira",
				Transport: TransportHTTP,
				URL:       "https://jira.example.com/mcp",
				Headers:   map[string]string{"Authorization": "Bearer token-value"},
			}},
			want: `{
  "mcpServers": {
    "jira": {
      "type": "http",
      "url": "https://jira.example.com/mcp",
      "headers": {
        "Authorization": "Bearer token-value"
      }
    }
  }
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderClaudeConfig(tt.servers); got != tt.want {
				t.Fatalf("RenderClaudeConfig() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestRenderClaudeConfigIsOrderIndependentAndValidJSON(t *testing.T) {
	forward := RenderClaudeConfig([]Server{
		{Name: "alpha", Transport: TransportStdio, Command: "a"},
		{Name: "zeta", Transport: TransportStdio, Command: "z"},
	})
	reversed := RenderClaudeConfig([]Server{
		{Name: "zeta", Transport: TransportStdio, Command: "z"},
		{Name: "alpha", Transport: TransportStdio, Command: "a"},
	})
	if forward != reversed {
		t.Fatalf("rendering is not order independent:\n%s\n---\n%s", forward, reversed)
	}
	var decoded map[string]map[string]map[string]any
	if err := json.Unmarshal([]byte(forward), &decoded); err != nil {
		t.Fatalf("rendered config is not valid JSON: %v", err)
	}
	if _, ok := decoded["mcpServers"]["alpha"]; !ok {
		t.Fatalf("expected an alpha entry, got %s", forward)
	}
}

func TestRenderCodexRegion(t *testing.T) {
	tests := []struct {
		name    string
		servers []Server
		want    string
	}{
		{
			name:    "no servers render nothing so the region disappears",
			servers: nil,
			want:    "",
		},
		{
			name: "a stdio server renders one table plus its env table",
			servers: []Server{{
				Name:      "postgres",
				Transport: TransportStdio,
				Command:   "npx",
				Args:      []string{"-y", "@modelcontextprotocol/server-postgres"},
				Env:       map[string]string{"PGPASSWORD": "hunter2", "PGUSER": "app"},
			}},
			want: `[mcp_servers."postgres"]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-postgres"]

[mcp_servers."postgres".env]
"PGPASSWORD" = "hunter2"
"PGUSER" = "app"
`,
		},
		{
			name: "an http server renders url plus its header table",
			servers: []Server{{
				Name:      "jira",
				Transport: TransportHTTP,
				URL:       "https://jira.example.com/mcp",
				Headers:   map[string]string{"Authorization": "Bearer t"},
			}},
			want: `[mcp_servers."jira"]
url = "https://jira.example.com/mcp"

[mcp_servers."jira".http_headers]
"Authorization" = "Bearer t"
`,
		},
		{
			name: "two servers are emitted in name order, separated by a blank line",
			servers: []Server{
				{Name: "zeta", Transport: TransportStdio, Command: "z"},
				{Name: "alpha", Transport: TransportStdio, Command: "a"},
			},
			want: `[mcp_servers."alpha"]
command = "a"

[mcp_servers."zeta"]
command = "z"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderCodexRegion(tt.servers); got != tt.want {
				t.Fatalf("RenderCodexRegion() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestRenderCodexRegionIsIdempotent(t *testing.T) {
	servers := []Server{{
		Name:      "fetch",
		Transport: TransportStdio,
		Command:   "uvx",
		Args:      []string{"mcp-server-fetch"},
		Env:       map[string]string{"B": "2", "A": "1"},
	}}
	first := RenderCodexRegion(servers)
	for attempt := 0; attempt < 5; attempt++ {
		if again := RenderCodexRegion(servers); again != first {
			t.Fatalf("render %d differs:\n%q\n%q", attempt, again, first)
		}
	}
}

func TestTOMLStringEscapesEverythingThatWouldBreakTheFile(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "npx", want: `"npx"`},
		{name: "quote", value: `a"b`, want: `"a\"b"`},
		{name: "backslash", value: `a\b`, want: `"a\\b"`},
		{name: "tab", value: "a\tb", want: `"a\tb"`},
		{name: "newline", value: "a\nb", want: `"a\nb"`},
		{name: "control", value: "a\x01b", want: `"a\u0001b"`},
		{name: "dot in a table key stays one key", value: "my.server", want: `"my.server"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tomlString(tt.value); got != tt.want {
				t.Fatalf("tomlString(%q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}

func TestRenderCodexRegionQuotesAValueCarryingATableHeader(t *testing.T) {
	// A value that looks like a TOML table header must not be able to end the
	// managed region's own table and start a new one.
	region := RenderCodexRegion([]Server{{
		Name:      "custom",
		Transport: TransportStdio,
		Command:   "sh",
		Args:      []string{"-c", "echo\n[mcp_servers.evil]\ncommand = \"rm\""},
	}})
	if strings.Contains(region, "\n[mcp_servers.evil]") {
		t.Fatalf("an injected table header survived escaping:\n%s", region)
	}
	if !strings.Contains(region, `\n[mcp_servers.evil]`) {
		t.Fatalf("expected the newline to be escaped, got:\n%s", region)
	}
}
