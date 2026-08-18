package github

import (
	"strings"
	"testing"
)

func TestParseRepoReference(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{name: "owner/repo", input: "futrx-com/remote.futrx", wantOwner: "futrx-com", wantRepo: "remote.futrx"},
		{name: "https url", input: "https://github.com/futrx-com/remote.futrx", wantOwner: "futrx-com", wantRepo: "remote.futrx"},
		{name: "https url with .git", input: "https://github.com/o/r.git", wantOwner: "o", wantRepo: "r"},
		{name: "https url with trailing slash", input: "https://github.com/o/r/", wantOwner: "o", wantRepo: "r"},
		{name: "www host", input: "https://www.github.com/o/r", wantOwner: "o", wantRepo: "r"},
		{name: "ssh remote", input: "git@github.com:o/r.git", wantOwner: "o", wantRepo: "r"},
		{name: "surrounding space", input: "  o/r  ", wantOwner: "o", wantRepo: "r"},
		{name: "empty", input: "", wantErr: true},
		{name: "one segment", input: "justrepo", wantErr: true},
		{name: "three segments", input: "a/b/c", wantErr: true},
		{name: "deep github url", input: "https://github.com/o/r/pull/3", wantErr: true},
		{name: "another host", input: "https://gitlab.com/o/r", wantErr: true},
		{name: "traversal in the owner", input: "../../etc/passwd", wantErr: true},
		{name: "leading dash would look like a flag", input: "-o/r", wantErr: true},
		{name: "space inside a segment", input: "o/r r", wantErr: true},
		{name: "shell metacharacters", input: "o/$(whoami)", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			owner, repo, err := ParseRepoReference(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseRepoReference(%q) = %q/%q, want an error", test.input, owner, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRepoReference(%q) returned %v", test.input, err)
			}
			if owner != test.wantOwner || repo != test.wantRepo {
				t.Fatalf("ParseRepoReference(%q) = %q/%q, want %q/%q",
					test.input, owner, repo, test.wantOwner, test.wantRepo)
			}
		})
	}
}

func TestValidBranch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "simple", input: "main", want: true},
		{name: "with a slash", input: "feat/github-integration", want: true},
		{name: "with dots and dashes", input: "release-1.2.x", want: true},
		{name: "empty", input: ""},
		{name: "leading dash reads as a flag", input: "--force"},
		{name: "double dot is a git range", input: "a..b"},
		{name: "lock suffix collides with git internals", input: "main.lock"},
		{name: "trailing slash", input: "feat/"},
		{name: "double slash", input: "feat//x"},
		{name: "space", input: "my branch"},
		{name: "shell metacharacters", input: "x;rm -rf /"},
		{name: "too long", input: strings.Repeat("a", 300)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidBranch(test.input); got != test.want {
				t.Fatalf("ValidBranch(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}

// argIndex finds a flag in an argv slice, returning -1 when absent.
func argIndex(argv []string, flag string) int {
	for index, value := range argv {
		if value == flag {
			return index
		}
	}
	return -1
}

// argValue returns the element after a flag.
func argValue(t *testing.T, argv []string, flag string) string {
	t.Helper()
	index := argIndex(argv, flag)
	if index < 0 || index+1 >= len(argv) {
		t.Fatalf("argv %v has no value for %q", argv, flag)
	}
	return argv[index+1]
}

func TestPRCreateArgv(t *testing.T) {
	tests := []struct {
		name       string
		in         CreatePRInput
		head       string
		base       string
		wantFill   bool
		wantStdin  string
		wantBody   string
		wantNoBase bool
	}{
		{
			name: "no title falls back to --fill",
			head: "feat/x", base: "main", wantFill: true,
		},
		{
			name: "title and body",
			in:   CreatePRInput{Title: "Add a flag", Body: "It does the thing."},
			head: "feat/x", base: "main", wantStdin: "It does the thing.",
		},
		{
			name: "title with no body sends an explicit empty body",
			in:   CreatePRInput{Title: "Add a flag"},
			head: "feat/x", base: "main", wantBody: "",
		},
		{
			name: "no base omits the flag",
			in:   CreatePRInput{Title: "T"},
			head: "feat/x", wantNoBase: true,
		},
		{
			// A title that begins with a dash must stay a title. It is an argv
			// element after --title, so gh never reads it as an option.
			name: "title that looks like a flag",
			in:   CreatePRInput{Title: "--force everything"},
			head: "feat/x", base: "main",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			argv, stdin := prCreateArgv(test.in, test.head, test.base)

			if got := strings.Join(argv[:3], " "); got != "gh pr create" {
				t.Fatalf("argv starts with %q, want %q", got, "gh pr create")
			}
			if got := argValue(t, argv, "--head"); got != test.head {
				t.Fatalf("--head = %q, want %q", got, test.head)
			}
			if test.wantNoBase {
				if argIndex(argv, "--base") >= 0 {
					t.Fatalf("argv %v should not carry --base", argv)
				}
			} else if got := argValue(t, argv, "--base"); got != test.base {
				t.Fatalf("--base = %q, want %q", got, test.base)
			}

			if test.wantFill {
				if argIndex(argv, "--fill") < 0 {
					t.Fatalf("argv %v should carry --fill", argv)
				}
				if argIndex(argv, "--title") >= 0 {
					t.Fatalf("argv %v should not carry --title alongside --fill", argv)
				}
				if stdin != "" {
					t.Fatalf("stdin = %q, want empty for --fill", stdin)
				}
				return
			}

			if got := argValue(t, argv, "--title"); got != test.in.Title {
				t.Fatalf("--title = %q, want %q", got, test.in.Title)
			}
			if test.wantStdin != "" {
				if got := argValue(t, argv, "--body-file"); got != "-" {
					t.Fatalf("--body-file = %q, want %q", got, "-")
				}
				if stdin != test.wantStdin {
					t.Fatalf("stdin = %q, want %q", stdin, test.wantStdin)
				}
				return
			}
			if got := argValue(t, argv, "--body"); got != "" {
				t.Fatalf("--body = %q, want an empty string", got)
			}
			if stdin != "" {
				t.Fatalf("stdin = %q, want empty", stdin)
			}
		})
	}
}

func TestCommitArgvCarriesAPlatformIdentity(t *testing.T) {
	argv := commitArgv("Changes from Remote — 2026-08-18")
	if argv[0] != "git" {
		t.Fatalf("argv[0] = %q, want git", argv[0])
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "user.name="+commitAuthorName) {
		t.Fatalf("argv %v does not set the author name", argv)
	}
	if !strings.Contains(joined, "user.email="+commitAuthorEmail) {
		t.Fatalf("argv %v does not set the author email", argv)
	}
	// The message must be the last element, whole and unquoted: it is an argv
	// element, so no shell ever sees it.
	if argv[len(argv)-1] != "Changes from Remote — 2026-08-18" {
		t.Fatalf("argv ends with %q, want the message verbatim", argv[len(argv)-1])
	}
	if argv[len(argv)-2] != "-m" {
		t.Fatalf("message is not preceded by -m: %v", argv)
	}
}

func TestReadOnlyArgvShapes(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want []string
	}{
		{name: "clone into the mount", argv: cloneArgv("o/r"), want: []string{"gh", "repo", "clone", "o/r", "."}},
		{name: "push sets upstream", argv: pushArgv("feat/x"), want: []string{"git", "push", "--set-upstream", "origin", "feat/x"}},
		{name: "stage all", argv: stageAllArgv(), want: []string{"git", "add", "-A"}},
		{
			name: "review comments", argv: reviewCommentsArgv("o/r", 12),
			want: []string{"gh", "api", "--paginate", "repos/o/r/pulls/12/comments"},
		},
		{
			name: "issue comments", argv: issueCommentsArgv("o/r", 12),
			want: []string{"gh", "api", "--paginate", "repos/o/r/issues/12/comments"},
		},
		{
			name: "issue comment posts on stdin", argv: issueCommentArgv("o/r", 12),
			want: []string{"gh", "issue", "comment", "12", "--repo", "o/r", "--body-file", "-"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if strings.Join(test.argv, " ") != strings.Join(test.want, " ") {
				t.Fatalf("argv = %v, want %v", test.argv, test.want)
			}
		})
	}
}

func TestRepoViewArgvAsksForTheDefaultBranch(t *testing.T) {
	argv := repoViewArgv("o/r")
	if strings.Join(argv[:4], " ") != "gh repo view o/r" {
		t.Fatalf("argv = %v, want it to start with gh repo view o/r", argv)
	}
	// The default branch is what a pull request targets when the caller names
	// no base, so link validation has to read it.
	if !strings.Contains(argValue(t, argv, "--json"), "defaultBranchRef") {
		t.Fatalf("argv %v must request defaultBranchRef", argv)
	}
}

func TestPRListArgvClampsTheLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  string
	}{
		{name: "in range", limit: 5, want: "5"},
		{name: "zero clamps to the maximum", limit: 0, want: "20"},
		{name: "negative clamps to the maximum", limit: -3, want: "20"},
		{name: "over the maximum clamps down", limit: 500, want: "20"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			argv := prListArgv("o/r", test.limit)
			if got := argValue(t, argv, "--limit"); got != test.want {
				t.Fatalf("--limit = %q, want %q", got, test.want)
			}
			if !strings.Contains(argValue(t, argv, "--json"), "statusCheckRollup") {
				t.Fatalf("argv %v must request statusCheckRollup for the checks column", argv)
			}
			// Without --repo, gh infers the repository from the workspace's
			// own origin, which is not necessarily the one we are linked to.
			if got := argValue(t, argv, "--repo"); got != "o/r" {
				t.Fatalf("--repo = %q, want the linked repository", got)
			}
		})
	}
}
