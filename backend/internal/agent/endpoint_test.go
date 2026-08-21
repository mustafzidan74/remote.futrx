package agent

import (
	"strings"
	"testing"
)

func TestEndpointEnvironmentIsSortedAndComplete(t *testing.T) {
	t.Parallel()

	endpoint := &Endpoint{
		ID:  "zhipu-glm",
		CLI: ProviderClaude,
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":   "https://open.bigmodel.cn/api/anthropic",
			"ANTHROPIC_AUTH_TOKEN": "vendor-key",
			"ANTHROPIC_API_KEY":    "",
		},
	}
	got := EndpointEnvironment(endpoint)
	want := []string{
		"ANTHROPIC_API_KEY=",
		"ANTHROPIC_AUTH_TOKEN=vendor-key",
		"ANTHROPIC_BASE_URL=https://open.bigmodel.cn/api/anthropic",
	}
	if len(got) != len(want) {
		t.Fatalf("EndpointEnvironment = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("entry %d = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestEndpointEnvironmentOfNilIsNothing(t *testing.T) {
	t.Parallel()

	if got := EndpointEnvironment(nil); got != nil {
		t.Errorf("EndpointEnvironment(nil) = %v, want nil", got)
	}
	if got := EndpointArgs(nil); got != nil {
		t.Errorf("EndpointArgs(nil) = %v, want nil", got)
	}
	if EndpointIssued(nil, "ANTHROPIC_BASE_URL") {
		t.Error("EndpointIssued(nil, ...) = true, want false")
	}
}

// The host path overlays the endpoint on os.Environ(). A stray first-party
// key in that environment must be replaced, not duplicated: which of two
// entries with the same name wins varies by executable.
func TestWithEndpointEnvironmentReplacesRatherThanDuplicates(t *testing.T) {
	t.Parallel()

	base := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-ant-oat01-OPERATOR-FIRST-PARTY-TOKEN",
		"ANTHROPIC_BASE_URL=https://api.anthropic.com",
	}
	endpoint := &Endpoint{
		CLI: ProviderClaude,
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":   "https://api.moonshot.ai/anthropic",
			"ANTHROPIC_AUTH_TOKEN": "vendor-key",
			"ANTHROPIC_API_KEY":    "",
		},
	}

	merged := WithEndpointEnvironment(base, endpoint)
	counts := map[string]int{}
	values := map[string]string{}
	for _, entry := range merged {
		name, value, _ := strings.Cut(entry, "=")
		counts[name]++
		values[name] = value
	}
	for name, count := range counts {
		if count != 1 {
			t.Errorf("%s appears %d times, want exactly once", name, count)
		}
	}
	if values["ANTHROPIC_API_KEY"] != "" {
		t.Errorf("the operator's first-party key survived: %q", values["ANTHROPIC_API_KEY"])
	}
	if values["ANTHROPIC_BASE_URL"] != "https://api.moonshot.ai/anthropic" {
		t.Errorf("base URL = %q, want the endpoint's", values["ANTHROPIC_BASE_URL"])
	}
	if values["PATH"] != "/usr/bin" {
		t.Errorf("an unrelated variable was disturbed: PATH=%q", values["PATH"])
	}
}

func TestEndpointIssued(t *testing.T) {
	t.Parallel()

	endpoint := &Endpoint{Env: map[string]string{"ANTHROPIC_BASE_URL": "https://x.test", "ANTHROPIC_API_KEY": ""}}
	cases := []struct {
		key  string
		want bool
	}{
		{"ANTHROPIC_BASE_URL", true},
		// Blanked entries count: a project secret must not be able to refill
		// a variable the platform deliberately emptied.
		{"ANTHROPIC_API_KEY", true},
		{"SOME_PROJECT_SECRET", false},
	}
	for _, testCase := range cases {
		if got := EndpointIssued(endpoint, testCase.key); got != testCase.want {
			t.Errorf("EndpointIssued(%q) = %v, want %v", testCase.key, got, testCase.want)
		}
	}
}
