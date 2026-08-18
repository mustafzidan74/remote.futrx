package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func provisioningTemplate() Template {
	return Template{
		Definition: Definition{Name: "stack", Title: "Stack"},
		Script:     []byte("echo install\n"),
	}
}

func TestDecide(t *testing.T) {
	blank := Template{Definition: Definition{Name: "blank"}}
	stack := provisioningTemplate()
	seedOnly := Template{
		Definition: Definition{Name: "seeded"},
		Seeds:      []Seed{{Target: "/workspace/README.md", Mode: "644", Content: []byte("x")}},
	}

	tests := []struct {
		name        string
		template    Template
		observation Observation
		wantRun     bool
		wantStatus  Status
	}{
		{
			name:       "template without work never touches the container",
			template:   blank,
			wantRun:    false,
			wantStatus: StatusNone,
		},
		{
			name:        "marker wins over everything",
			template:    stack,
			observation: Observation{MarkerPresent: true, FailurePresent: true, InFlight: true},
			wantRun:     false,
			wantStatus:  StatusDone,
		},
		{
			name:        "already running is not started twice",
			template:    stack,
			observation: Observation{InFlight: true},
			wantRun:     false,
			wantStatus:  StatusRunning,
		},
		{
			name:        "a previous failure is retried",
			template:    stack,
			observation: Observation{FailurePresent: true},
			wantRun:     true,
			wantStatus:  StatusRunning,
		},
		{
			name:       "a fresh container is provisioned",
			template:   stack,
			wantRun:    true,
			wantStatus: StatusRunning,
		},
		{
			name:       "seeds alone are enough work to provision",
			template:   seedOnly,
			wantRun:    true,
			wantStatus: StatusRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Decide(tt.template, tt.observation)
			if got.Run != tt.wantRun || got.Status != tt.wantStatus {
				t.Fatalf("Decide() = %+v, want Run=%t Status=%q", got, tt.wantRun, tt.wantStatus)
			}
			if got.Reason == "" {
				t.Fatal("Decide() must explain itself")
			}
		})
	}
}

func TestObservedStatus(t *testing.T) {
	blank := Template{Definition: Definition{Name: "blank"}}
	stack := provisioningTemplate()

	tests := []struct {
		name        string
		template    Template
		observation Observation
		want        Status
	}{
		{name: "nothing to install", template: blank, want: StatusNone},
		{name: "done", template: stack, observation: Observation{MarkerPresent: true}, want: StatusDone},
		{name: "running", template: stack, observation: Observation{InFlight: true}, want: StatusRunning},
		{
			name:        "an interrupted run reads as failed, not pending",
			template:    stack,
			observation: Observation{FailurePresent: true},
			want:        StatusFailed,
		},
		{name: "pending", template: stack, want: StatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ObservedStatus(tt.template, tt.observation); got != tt.want {
				t.Fatalf("ObservedStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProvisionProgram(t *testing.T) {
	program := ProvisionProgram(provisioningTemplate())

	for _, want := range []string{
		"set -eu",
		"DEBIAN_FRONTEND=noninteractive",
		LogPath,
		MarkerPath,
		FailurePath,
		"apt_retry()",
		"echo install",
	} {
		if !strings.Contains(program, want) {
			t.Fatalf("program is missing %q:\n%s", want, program)
		}
	}
	// The marker check must precede the payload so a re-run is a no-op, and
	// the marker write must follow it so a failure never records success.
	markerCheck := strings.Index(program, `if [ -f "$REMOTE_TEMPLATE_MARKER" ]`)
	payload := strings.Index(program, "echo install")
	markerWrite := strings.LastIndex(program, `> "$REMOTE_TEMPLATE_MARKER"`)
	if markerCheck < 0 || payload < 0 || markerWrite < 0 {
		t.Fatalf("program is missing the marker protocol:\n%s", program)
	}
	if !(markerCheck < payload && payload < markerWrite) {
		t.Fatalf("marker protocol is out of order (%d, %d, %d)", markerCheck, payload, markerWrite)
	}
}

func TestProvisionProgramForTemplatesWithoutAScript(t *testing.T) {
	// Nothing to do at all: no program, so no runtime call.
	blank := Template{Definition: Definition{Name: "blank"}}
	if got := ProvisionProgram(blank); got != "" {
		t.Fatalf("ProvisionProgram(blank) = %q, want empty", got)
	}

	// Seeds but no script: the harness alone still runs, so the marker is
	// written once and the seeding is not retried on every container start.
	seeded := Template{
		Definition: Definition{Name: "seeded"},
		Seeds:      []Seed{{Target: "/workspace/README.md", Mode: "644", Content: []byte("x")}},
	}
	program := ProvisionProgram(seeded)
	if !strings.Contains(program, MarkerPath) {
		t.Fatalf("seed-only program must still write the marker:\n%s", program)
	}
}

func TestShippedProvisionScriptsUseTheHarness(t *testing.T) {
	catalog := MustLoad()
	for _, template := range catalog.List() {
		if len(template.Script) == 0 {
			continue
		}
		script := string(template.Script)
		// The harness owns the shebang, `set -eu`, the log, and the marker;
		// a payload that redefines them would defeat the idempotency contract.
		// (Heredocs inside a payload may of course carry their own shebang.)
		if strings.HasPrefix(script, "#!") {
			t.Errorf("template %q: payload must not start with a shebang", template.Name)
		}
		for _, forbidden := range []string{"set -e", MarkerPath, "DEBIAN_FRONTEND="} {
			if strings.Contains(script, forbidden) {
				t.Errorf("template %q: payload must not contain %q (the harness owns it)",
					template.Name, forbidden)
			}
		}
		// Network-touching apt calls must go through the retry helper.
		for _, line := range strings.Split(script, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "apt-get ") || strings.HasPrefix(trimmed, "apt ") {
				t.Errorf("template %q: use apt_retry instead of a bare apt call: %s",
					template.Name, trimmed)
			}
		}
	}
}

func TestShippedProvisionProgramsParse(t *testing.T) {
	// `bash -n` is the only cheap way to catch an unbalanced quote or a stray
	// `fi` in a payload we cannot run here. It checks the assembled program,
	// harness included, because that is what the container executes.
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	for _, template := range MustLoad().List() {
		program := ProvisionProgram(template)
		if program == "" {
			continue
		}
		t.Run(template.Name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), template.Name+".sh")
			if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bash, "-n", path).CombinedOutput()
			if err != nil {
				t.Fatalf("bash -n = %v; output: %s", err, out)
			}
		})
	}
}

func TestImageAlias(t *testing.T) {
	if got := ImageAlias("wordpress"); got != "futrx-remote-wordpress-base" {
		t.Fatalf("ImageAlias() = %q", got)
	}
	declared := Template{Definition: Definition{Name: "wordpress", PrebuiltImage: true}}
	if got := declared.ImageAlias(); got != "futrx-remote-wordpress-base" {
		t.Fatalf("declared ImageAlias() = %q", got)
	}
	undeclared := Template{Definition: Definition{Name: "node"}}
	if got := undeclared.ImageAlias(); got != "" {
		t.Fatalf("undeclared ImageAlias() = %q, want empty", got)
	}
}
