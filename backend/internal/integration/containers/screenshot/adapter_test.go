package screenshot

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
)

func TestScreenshotArgs(t *testing.T) {
	tests := []struct {
		name string
		req  servicescreenshot.CaptureRequest
		want string
	}{
		{
			name: "viewport capture",
			req: servicescreenshot.CaptureRequest{
				ContainerName: "demo", URL: "http://127.0.0.1:3000/",
				Width: 1280, Height: 800, RemotePath: "/tmp/remote-shot-abc.png",
			},
			want: "exec demo --env HOME=/root --env PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers " +
				"--env NODE_PATH=/usr/lib/node_modules -- npx --no-install playwright screenshot " +
				"--browser chromium --viewport-size 1280,800 " +
				"http://127.0.0.1:3000/ /tmp/remote-shot-abc.png",
		},
		{
			name: "full page capture",
			req: servicescreenshot.CaptureRequest{
				ContainerName: "demo", URL: "http://127.0.0.1:5173/pricing",
				Width: 390, Height: 844, FullPage: true, RemotePath: "/tmp/remote-shot-def.png",
			},
			want: "exec demo --env HOME=/root --env PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers " +
				"--env NODE_PATH=/usr/lib/node_modules -- npx --no-install playwright screenshot " +
				"--browser chromium --viewport-size 390,844 --full-page " +
				"http://127.0.0.1:5173/pricing /tmp/remote-shot-def.png",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := strings.Join(screenshotArgs(test.req), " "); got != test.want {
				t.Fatalf("screenshotArgs() =\n%s\nwant\n%s", got, test.want)
			}
		})
	}
}

// TestCapturePullsTheImageAndCleansUp also pins the shape of the pull: the
// bytes must come off a host file, never off the runner's merged stdout+stderr
// string, which would splice any warning `lxc` printed into the image.
func TestCapturePullsTheImageAndCleansUp(t *testing.T) {
	const png = "\x89PNG\r\n\x1a\nbody"
	runner := &fakeRunner{
		responses: map[string]string{"exec demo -- sh -c command -v npx": "/usr/bin/npx\n"},
		writes:    map[string]string{"file pull demo/tmp/remote-shot-abc.png": png},
	}
	adapter := NewAdapter(runner)

	data, err := adapter.Capture(context.Background(), servicescreenshot.CaptureRequest{
		ContainerName: "demo", URL: "http://127.0.0.1:3000/",
		Width: 1280, Height: 800, RemotePath: "/tmp/remote-shot-abc.png",
	})
	if err != nil {
		t.Fatalf("Capture(): %v", err)
	}
	if string(data) != png {
		t.Fatalf("Capture() returned %q, want the pulled bytes verbatim", data)
	}
	if !runner.ran("exec demo -- rm -f /tmp/remote-shot-abc.png") {
		t.Fatalf("the throwaway file was not removed; ran: %v", runner.calls)
	}
	if runner.pullDestination == "" || runner.pullDestination == "-" {
		t.Fatalf("pulled to %q, want a host file path", runner.pullDestination)
	}
	if _, err := os.Stat(runner.pullDestination); !os.IsNotExist(err) {
		t.Fatalf("the host staging file was left behind at %s", runner.pullDestination)
	}
}

func TestCaptureReportsMissingTooling(t *testing.T) {
	tests := []struct {
		name      string
		responses map[string]string
		failures  map[string]bool
	}{
		{
			name:      "no npx in the container",
			responses: map[string]string{"exec demo -- sh -c command -v npx": ""},
		},
		{
			name: "playwright package absent",
			responses: map[string]string{
				"exec demo -- sh -c command -v npx": "/usr/bin/npx\n",
				"exec demo --env HOME=/root --env PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers " +
					"--env NODE_PATH=/usr/lib/node_modules -- npx --no-install playwright screenshot --browser chromium " +
					"--viewport-size 1280,800 http://127.0.0.1:3000/ /tmp/remote-shot-abc.png": "npm error could not determine executable to run",
			},
			failures: map[string]bool{
				"exec demo --env HOME=/root --env PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers " +
					"--env NODE_PATH=/usr/lib/node_modules -- npx --no-install playwright screenshot --browser chromium " +
					"--viewport-size 1280,800 http://127.0.0.1:3000/ /tmp/remote-shot-abc.png": true,
			},
		},
		{
			name: "chromium binary absent",
			responses: map[string]string{
				"exec demo -- sh -c command -v npx": "/usr/bin/npx\n",
				"exec demo --env HOME=/root --env PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers " +
					"--env NODE_PATH=/usr/lib/node_modules -- npx --no-install playwright screenshot --browser chromium " +
					"--viewport-size 1280,800 http://127.0.0.1:3000/ /tmp/remote-shot-abc.png": "Executable doesn't exist at /root/.cache/ms-playwright/chromium/headless_shell",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := NewAdapter(&fakeRunner{responses: test.responses, failures: test.failures})
			_, err := adapter.Capture(context.Background(), servicescreenshot.CaptureRequest{
				ContainerName: "demo", URL: "http://127.0.0.1:3000/",
				Width: 1280, Height: 800, RemotePath: "/tmp/remote-shot-abc.png",
			})
			if !errors.Is(err, servicescreenshot.ErrToolingMissing) {
				t.Fatalf("Capture() error = %v, want ErrToolingMissing", err)
			}
		})
	}
}

type fakeRunner struct {
	mu        sync.Mutex
	calls     []string
	responses map[string]string
	failures  map[string]bool
	// writes maps a call minus its last argument to content the "runner" drops
	// at that last argument, standing in for `lxc file pull <src> <dst>`.
	writes map[string]string
	// pullDestination records where the pull was told to land.
	pullDestination string
}

func (r *fakeRunner) Available() bool { return true }

func (r *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	key := strings.Join(args, " ")
	r.mu.Lock()
	r.calls = append(r.calls, key)
	r.mu.Unlock()
	if len(args) > 1 {
		if content, ok := r.writes[strings.Join(args[:len(args)-1], " ")]; ok {
			r.pullDestination = args[len(args)-1]
			if err := os.WriteFile(r.pullDestination, []byte(content), 0o600); err != nil {
				return "", err
			}
			return "", nil
		}
	}
	out := r.responses[key]
	if r.failures[key] {
		return out, errors.New("exit status 1")
	}
	return out, nil
}

func (r *fakeRunner) RunStdin(_ context.Context, _ io.Reader, args ...string) (string, error) {
	return r.Run(context.Background(), args...)
}

func (r *fakeRunner) ran(call string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, seen := range r.calls {
		if seen == call {
			return true
		}
	}
	return false
}
