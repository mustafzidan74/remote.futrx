package antigravity

import (
	"regexp"
	"strings"
	"sync"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
)

// deniedPermissionLine is how agy explains a headless run it stopped:
//
//	jetski: no output produced — a tool required the "command" permission
//	that headless mode cannot prompt for, so it was auto-denied. …
var deniedPermissionLine = regexp.MustCompile(`required the "([A-Za-z_]+)" permission that headless mode cannot prompt for`)

func deniedPermission(stderr string) (string, bool) {
	match := deniedPermissionLine.FindStringSubmatch(stderr)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// permissionStopReason is what the chat shows when agy stopped on a denied
// permission. Plan mode is the usual cause: it reads, and nothing else.
func permissionStopReason(mode agent.RunMode, permission string) string {
	if mode == agent.RunModePlan {
		if permission == "command" {
			return "Plan mode only lets Antigravity read files, so it stopped when it tried to run a command. Turn off Plan mode to let it run commands."
		}
		return "Plan mode only lets Antigravity read files, so it stopped when it tried to change something. Turn off Plan mode to let it make changes."
	}
	return "Antigravity stopped because a tool needed the " + permission + " permission, which a chat run cannot grant."
}

// stderrLines collects agy's stderr, which arrives on RunProcess's reader
// goroutine.
type stderrLines struct {
	mu    sync.Mutex
	lines strings.Builder
}

func (s *stderrLines) add(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lines.Len() < 64<<10 {
		s.lines.WriteString(line)
		s.lines.WriteByte('\n')
	}
}

func (s *stderrLines) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lines.String()
}
