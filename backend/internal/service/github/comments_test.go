package github

import (
	"strconv"
	"strings"
	"testing"
)

const reviewCommentsPage = `[
  {
    "body": "This allocates on every call.",
    "path": "internal/service/github/service.go",
    "line": 42,
    "diff_hunk": "@@ -40,3 +40,5 @@ func x() {\n-old\n+new",
    "html_url": "https://github.com/o/r/pull/3#discussion_r1",
    "created_at": "2026-08-10T09:00:00Z",
    "user": {"login": "reviewer"}
  }
]`

const issueCommentsPage = `[
  {
    "body": "Also please add a test.",
    "html_url": "https://github.com/o/r/pull/3#issuecomment-2",
    "created_at": "2026-08-11T09:00:00Z",
    "user": {"login": "maintainer"}
  }
]`

func TestParseComments(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		want      int
		wantFirst Comment
		wantErr   bool
	}{
		{
			name: "review comment keeps its anchor",
			raw:  reviewCommentsPage,
			want: 1,
			wantFirst: Comment{
				Author: "reviewer",
				Body:   "This allocates on every call.",
				Path:   "internal/service/github/service.go",
				Line:   42,
				Diff:   "@@ -40,3 +40,5 @@ func x() {",
			},
		},
		{
			name: "issue comment has no anchor",
			raw:  issueCommentsPage,
			want: 1,
			wantFirst: Comment{
				Author: "maintainer",
				Body:   "Also please add a test.",
			},
		},
		{
			// `gh api --paginate` concatenates one array per page rather than
			// merging them, so the decoder has to keep reading.
			name: "concatenated pages are all read",
			raw:  reviewCommentsPage + "\n" + reviewCommentsPage,
			want: 2,
		},
		{name: "empty array", raw: "[]"},
		{name: "empty output", raw: "   "},
		{
			name: "blank bodies are dropped",
			raw:  `[{"body":"   ","user":{"login":"x"}},{"body":"kept","user":{"login":"y"}}]`,
			want: 1,
			wantFirst: Comment{
				Author: "y",
				Body:   "kept",
			},
		},
		{
			name: "line falls back to original_line after a force push",
			raw:  `[{"body":"b","path":"a.go","line":0,"original_line":17,"user":{"login":"x"}}]`,
			want: 1,
			wantFirst: Comment{
				Author: "x", Body: "b", Path: "a.go", Line: 17,
			},
		},
		{name: "not an array", raw: `{"message":"Not Found"}`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseComments(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseComments(%q) = %v, want an error", test.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseComments returned %v", err)
			}
			if len(got) != test.want {
				t.Fatalf("ParseComments returned %d comments, want %d", len(got), test.want)
			}
			if test.want == 0 || test.wantFirst.Body == "" {
				return
			}
			first := got[0]
			if first.Author != test.wantFirst.Author || first.Body != test.wantFirst.Body ||
				first.Path != test.wantFirst.Path || first.Line != test.wantFirst.Line ||
				first.Diff != test.wantFirst.Diff {
				t.Fatalf("first comment = %+v, want %+v", first, test.wantFirst)
			}
		})
	}
}

func TestMergeComments(t *testing.T) {
	older := Comment{Author: "a", Body: "first", CreatedAt: "2026-01-01T00:00:00Z"}
	newer := Comment{Author: "b", Body: "second", CreatedAt: "2026-02-01T00:00:00Z"}

	t.Run("orders oldest first across both sources", func(t *testing.T) {
		merged := MergeComments([]Comment{newer}, []Comment{older})
		if len(merged) != 2 || merged[0].Body != "first" || merged[1].Body != "second" {
			t.Fatalf("merged = %+v, want first then second", merged)
		}
	})

	t.Run("keeps the newest when there are too many", func(t *testing.T) {
		many := make([]Comment, 0, MaxCommentsImported+10)
		for index := 0; index < MaxCommentsImported+10; index++ {
			many = append(many, Comment{
				Author:    "a",
				Body:      strconv.Itoa(index),
				CreatedAt: "2026-01-01T00:00:" + strconv.Itoa(100+index),
			})
		}
		merged := MergeComments(many, nil)
		if len(merged) != MaxCommentsImported {
			t.Fatalf("merged %d comments, want %d", len(merged), MaxCommentsImported)
		}
		// The last comment in the input has to survive: the recent ones are
		// the ones still unaddressed.
		if merged[len(merged)-1].Body != strconv.Itoa(MaxCommentsImported+9) {
			t.Fatalf("last kept comment = %q, want the newest", merged[len(merged)-1].Body)
		}
	})
}

func TestComposeReviewPrompt(t *testing.T) {
	comments := []Comment{
		{
			Author: "reviewer", Body: "This allocates on every call.",
			Path: "service.go", Line: 42, Diff: "@@ -40,3 +40,5 @@",
		},
		{Author: "maintainer", Body: "Also please\nadd a test."},
	}
	got := ComposeReviewPrompt("o/r", 3, comments)

	for _, want := range []string{
		"Address these PR review comments on #3 (o/r):",
		"1. @reviewer on service.go:42",
		"@@ -40,3 +40,5 @@",
		"   > This allocates on every call.",
		"2. @maintainer",
		"   > Also please",
		"   > add a test.",
		"Do not push or open a pull request",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, got)
		}
	}
}

func TestComposeReviewPromptTruncatesALongBody(t *testing.T) {
	long := strings.Repeat("x", MaxCommentBodyChars+500)
	got := ComposeReviewPrompt("o/r", 1, []Comment{{Author: "a", Body: long}})
	if !strings.Contains(got, "(comment truncated)") {
		t.Fatal("an over-long comment must be marked as truncated")
	}
	if strings.Contains(got, strings.Repeat("x", MaxCommentBodyChars+1)) {
		t.Fatal("the comment body was not actually clipped")
	}
}

func TestComposeReviewPromptWithoutARepositoryName(t *testing.T) {
	got := ComposeReviewPrompt("", 9, []Comment{{Author: "a", Body: "b"}})
	if !strings.HasPrefix(got, "Address these PR review comments on #9:") {
		t.Fatalf("prompt starts with %q", strings.SplitN(got, "\n", 2)[0])
	}
}
