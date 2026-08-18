package health

import (
	"errors"
	"reflect"
	"testing"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

const gib = int64(1) << 30

func TestEvaluateThresholds(t *testing.T) {
	running := serviceproject.ContainerStateRunning

	cases := []struct {
		name        string
		signals     Signals
		wantStatus  Status
		wantReasons []string
		wantPreview *bool
	}{
		{
			name: "quiet container is ok",
			signals: Signals{
				ProjectStatus:    serviceproject.StatusRunning,
				ContainerState:   running,
				MemoryUsedBytes:  gib / 2,
				MemoryLimitBytes: 2 * gib,
			},
			wantStatus: StatusOK,
		},
		{
			name: "memory just under the warn threshold stays ok",
			signals: Signals{
				ContainerState:   running,
				MemoryUsedBytes:  799 * gib / 1000,
				MemoryLimitBytes: gib,
			},
			wantStatus: StatusOK,
		},
		{
			name: "memory at the warn threshold warns",
			signals: Signals{
				ContainerState:   running,
				MemoryUsedBytes:  800 * gib / 1000,
				MemoryLimitBytes: gib,
			},
			wantStatus:  StatusWarn,
			wantReasons: []string{"memory 80% (0.8/1 GiB)"},
		},
		{
			name: "memory at the crit threshold is critical",
			signals: Signals{
				ContainerState:   running,
				MemoryUsedBytes:  920 * gib / 1000,
				MemoryLimitBytes: gib,
			},
			wantStatus:  StatusCrit,
			wantReasons: []string{"memory 92% (0.92/1 GiB)"},
		},
		{
			name: "a five hundred response warns",
			signals: Signals{
				ContainerState:   running,
				MemoryUsedBytes:  gib / 4,
				MemoryLimitBytes: gib,
				Listeners:        []int{3000},
				Preview:          Probe{Attempted: true, Port: 3000, StatusCode: 502},
			},
			wantStatus:  StatusWarn,
			wantReasons: []string{"the app on port 3000 returned HTTP 502"},
			wantPreview: boolPtr(false),
		},
		{
			name: "a four hundred response is the app working",
			signals: Signals{
				ContainerState:   running,
				MemoryUsedBytes:  gib / 4,
				MemoryLimitBytes: gib,
				Listeners:        []int{3000},
				Preview:          Probe{Attempted: true, Port: 3000, StatusCode: 404},
			},
			wantStatus:  StatusOK,
			wantPreview: boolPtr(true),
		},
		{
			name: "an unreachable listener is critical",
			signals: Signals{
				ContainerState:   running,
				MemoryUsedBytes:  gib / 4,
				MemoryLimitBytes: gib,
				Listeners:        []int{3000},
				Preview:          Probe{Attempted: true, Port: 3000, Err: errors.New("connection refused")},
			},
			wantStatus:  StatusCrit,
			wantReasons: []string{"the app on port 3000 is not answering"},
			wantPreview: boolPtr(false),
		},
		{
			name: "an errored project is critical whatever it measures",
			signals: Signals{
				ProjectStatus:    serviceproject.StatusError,
				ContainerState:   running,
				MemoryUsedBytes:  gib / 4,
				MemoryLimitBytes: gib,
			},
			wantStatus:  StatusCrit,
			wantReasons: []string{"the project is in an error state"},
		},
		{
			name: "a vanished container is critical",
			signals: Signals{
				ProjectStatus:  serviceproject.StatusRunning,
				ContainerState: serviceproject.ContainerStateMissing,
			},
			wantStatus:  StatusCrit,
			wantReasons: []string{"the container is missing"},
		},
		{
			name:       "nothing measured is unknown",
			signals:    Signals{ProjectStatus: serviceproject.StatusRunning},
			wantStatus: StatusUnknown,
		},
		{
			name: "a running container with no counters is unknown, not healthy",
			signals: Signals{
				ProjectStatus:  serviceproject.StatusRunning,
				ContainerState: running,
			},
			wantStatus: StatusUnknown,
		},
		{
			name: "both failures are reported under the worse status",
			signals: Signals{
				ContainerState:   running,
				MemoryUsedBytes:  950 * gib / 1000,
				MemoryLimitBytes: gib,
				Listeners:        []int{3000},
				Preview:          Probe{Attempted: true, Port: 3000, StatusCode: 503},
			},
			wantStatus: StatusCrit,
			wantReasons: []string{
				"memory 95% (0.95/1 GiB)",
				"the app on port 3000 returned HTTP 503",
			},
			wantPreview: boolPtr(false),
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := Evaluate(testCase.signals)
			if got.Status != testCase.wantStatus {
				t.Fatalf("status = %q, want %q (reasons %v)", got.Status, testCase.wantStatus, got.Reasons)
			}
			if testCase.wantReasons != nil && !reflect.DeepEqual(got.Reasons, testCase.wantReasons) {
				t.Fatalf("reasons = %#v, want %#v", got.Reasons, testCase.wantReasons)
			}
			if !reflect.DeepEqual(got.PreviewOK, testCase.wantPreview) {
				t.Fatalf("previewOk = %v, want %v", derefBool(got.PreviewOK), derefBool(testCase.wantPreview))
			}
		})
	}
}

func TestStateMachineHysteresis(t *testing.T) {
	type step struct {
		observed   Status
		wantStatus Status
		wantAlert  bool
	}

	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "the first reading is adopted without confirmation",
			steps: []step{
				{observed: StatusOK, wantStatus: StatusOK, wantAlert: false},
			},
		},
		{
			name: "a first reading that is already bad alerts immediately",
			steps: []step{
				{observed: StatusCrit, wantStatus: StatusCrit, wantAlert: true},
			},
		},
		{
			name: "a single bad sweep does not move the status",
			steps: []step{
				{observed: StatusOK, wantStatus: StatusOK},
				{observed: StatusCrit, wantStatus: StatusOK},
				{observed: StatusOK, wantStatus: StatusOK},
			},
		},
		{
			name: "two agreeing sweeps move the status and alert once",
			steps: []step{
				{observed: StatusOK, wantStatus: StatusOK},
				{observed: StatusWarn, wantStatus: StatusOK},
				{observed: StatusWarn, wantStatus: StatusWarn, wantAlert: true},
				{observed: StatusWarn, wantStatus: StatusWarn},
				{observed: StatusWarn, wantStatus: StatusWarn},
			},
		},
		{
			name: "flapping between two bad statuses never settles",
			steps: []step{
				{observed: StatusOK, wantStatus: StatusOK},
				{observed: StatusWarn, wantStatus: StatusOK},
				{observed: StatusCrit, wantStatus: StatusOK},
				{observed: StatusWarn, wantStatus: StatusOK},
				{observed: StatusCrit, wantStatus: StatusOK},
			},
		},
		{
			name: "escalation and recovery each alert once",
			steps: []step{
				{observed: StatusWarn, wantStatus: StatusWarn, wantAlert: true},
				{observed: StatusCrit, wantStatus: StatusWarn},
				{observed: StatusCrit, wantStatus: StatusCrit, wantAlert: true},
				{observed: StatusOK, wantStatus: StatusCrit},
				{observed: StatusOK, wantStatus: StatusOK, wantAlert: true},
			},
		},
		{
			name: "losing contact with lxd is never an alert",
			steps: []step{
				{observed: StatusOK, wantStatus: StatusOK},
				{observed: StatusUnknown, wantStatus: StatusOK},
				{observed: StatusUnknown, wantStatus: StatusUnknown},
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			machine := stateMachine{}
			for index, step := range testCase.steps {
				status, alert := machine.Observe(step.observed)
				if status != step.wantStatus || alert != step.wantAlert {
					t.Fatalf(
						"step %d observing %q: got (%q, alert=%t), want (%q, alert=%t)",
						index, step.observed, status, alert, step.wantStatus, step.wantAlert,
					)
				}
			}
		})
	}
}

func TestAlertSummary(t *testing.T) {
	cases := []struct {
		name    string
		project string
		health  ProjectHealth
		want    string
	}{
		{
			name:    "a critical memory alert names what is running",
			project: "wp-project",
			health: ProjectHealth{
				Status:           StatusCrit,
				MemoryUsedBytes:  1513975726,
				MemoryLimitBytes: 1610612736,
				MemoryPct:        94,
				Listeners:        []int{6080, 8842, 9222},
				Reasons:          []string{"memory 94% (1.41/1.5 GiB)"},
			},
			want: "wp-project memory 94% (1.41/1.5 GiB) — agent browser + code-server running",
		},
		{
			name:    "an app port is named by its number",
			project: "shop",
			health: ProjectHealth{
				Status:    StatusCrit,
				Listeners: []int{3000},
				Reasons:   []string{"the app on port 3000 is not answering"},
			},
			want: "shop the app on port 3000 is not answering — :3000 running",
		},
		{
			name:    "a recovery reports the number it recovered to",
			project: "wp-project",
			health: ProjectHealth{
				Status:           StatusOK,
				MemoryUsedBytes:  gib / 2,
				MemoryLimitBytes: 2 * gib,
				MemoryPct:        25,
			},
			want: "wp-project back to normal, memory 25% (0.5/2 GiB)",
		},
		{
			name:    "a recovery without accounting still says something",
			project: "wp-project",
			health:  ProjectHealth{Status: StatusOK},
			want:    "wp-project back to normal, all health checks are passing",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := AlertSummary(testCase.project, testCase.health); got != testCase.want {
				t.Fatalf("summary = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFirstAppPortSkipsPlatformPlumbing(t *testing.T) {
	cases := []struct {
		name     string
		ports    []int
		wantPort int
		wantOK   bool
	}{
		{name: "no listeners", ports: nil},
		{name: "only platform plumbing", ports: []int{6080, 8842, 8081, 9222}},
		{name: "lowest application port wins", ports: []int{8842, 5173, 3000}, wantPort: 3000, wantOK: true},
		{name: "an application port above the plumbing", ports: []int{6080, 9000}, wantPort: 9000, wantOK: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			port, ok := firstAppPort(testCase.ports)
			if port != testCase.wantPort || ok != testCase.wantOK {
				t.Fatalf("firstAppPort(%v) = (%d, %t), want (%d, %t)",
					testCase.ports, port, ok, testCase.wantPort, testCase.wantOK)
			}
		})
	}
}

func boolPtr(value bool) *bool { return &value }

func derefBool(value *bool) string {
	if value == nil {
		return "<nil>"
	}
	if *value {
		return "true"
	}
	return "false"
}
