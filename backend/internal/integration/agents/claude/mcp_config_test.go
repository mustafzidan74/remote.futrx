package claude

import (
	"reflect"
	"testing"
)

// The Claude CLI's --mcp-config option is variadic: it takes one or more
// space-separated files. Repeating the flag is not the same thing, so a
// second config joins the existing group instead of opening a new one — and
// the group has to stay at the end of the command line, where a variadic
// option cannot swallow a later flag's value.
func TestAppendMCPConfig(t *testing.T) {
	tests := []struct {
		name string
		args []string
		path string
		want []string
	}{
		{
			name: "an empty path changes nothing",
			args: []string{"-p", "--verbose"},
			path: "",
			want: []string{"-p", "--verbose"},
		},
		{
			name: "the first config opens the group",
			args: []string{"-p", "--verbose"},
			path: "/root/.claude/mcp-servers.json",
			want: []string{"-p", "--verbose", "--mcp-config", "/root/.claude/mcp-servers.json"},
		},
		{
			name: "a second config joins the browser's group rather than replacing it",
			args: []string{"-p", "--mcp-config", browserMCPConfigPath},
			path: "/root/.claude/mcp-servers.json",
			want: []string{
				"-p", "--mcp-config", browserMCPConfigPath, "/root/.claude/mcp-servers.json",
			},
		},
		{
			name: "a group that is not last is lifted to the end, keeping both files",
			args: []string{"--mcp-config", browserMCPConfigPath, "--model", "opus"},
			path: "/root/.claude/mcp-servers.json",
			want: []string{
				"--model", "opus",
				"--mcp-config", browserMCPConfigPath, "/root/.claude/mcp-servers.json",
			},
		},
		{
			name: "only one --mcp-config flag is ever emitted",
			args: []string{"-p", "--mcp-config", "a.json", "--verbose", "--mcp-config", "b.json"},
			path: "c.json",
			want: []string{"-p", "--verbose", "--mcp-config", "a.json", "b.json", "c.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appendMCPConfig(tt.args, tt.path); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("appendMCPConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}
