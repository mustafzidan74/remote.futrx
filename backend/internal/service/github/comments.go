package github

// Turning a review thread into a prompt.
//
// Two GitHub endpoints answer with two different shapes — diff-anchored review
// comments carry a file and a line, conversation comments do not — and both
// are folded into one ordered list here so the agent reads a single, coherent
// brief instead of two disjoint dumps.

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// rawComment is the union of the fields the two comment endpoints supply.
// GitHub sends far more; everything not listed is ignored.
type rawComment struct {
	Body      string `json:"body"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	StartLine int    `json:"start_line"`
	// OriginalLine survives a force-push that moved the anchored line, so it
	// is the fallback when `line` came back null.
	OriginalLine int    `json:"original_line"`
	DiffHunk     string `json:"diff_hunk"`
	HTMLURL      string `json:"html_url"`
	CreatedAt    string `json:"created_at"`
	User         struct {
		Login string `json:"login"`
	} `json:"user"`
}

// ParseComments decodes one `gh api --paginate` response.
//
// `--paginate` concatenates pages, and gh emits them as separate JSON arrays
// back to back rather than as one merged array, so the decoder is run in a
// stream until the input is exhausted instead of unmarshalling once.
func ParseComments(raw string) ([]Comment, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	out := []Comment{}
	for {
		var page []rawComment
		if err := decoder.Decode(&page); err != nil {
			// io.EOF ends a well-formed stream. Anything else means gh printed
			// something that is not a comment array, which the caller reports.
			if strings.Contains(err.Error(), "EOF") {
				break
			}
			return nil, err
		}
		for _, entry := range page {
			body := strings.TrimSpace(entry.Body)
			if body == "" {
				continue
			}
			line := entry.Line
			if line == 0 {
				line = entry.OriginalLine
			}
			out = append(out, Comment{
				Author:    entry.User.Login,
				Body:      body,
				Path:      entry.Path,
				Line:      line,
				Diff:      firstDiffLine(entry.DiffHunk),
				CreatedAt: entry.CreatedAt,
				URL:       entry.HTMLURL,
			})
		}
	}
	return out, nil
}

// firstDiffLine keeps only the hunk header (`@@ -1,4 +1,6 @@`), which locates
// the comment without pasting a diff the agent can read from the repository
// itself.
func firstDiffLine(hunk string) string {
	for _, line := range strings.Split(hunk, "\n") {
		if strings.HasPrefix(line, "@@") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// MergeComments orders two sets of comments oldest-first and caps the result
// at MaxCommentsImported, keeping the newest when there are too many — a long
// thread's recent comments are the ones still unaddressed.
func MergeComments(review, issue []Comment) []Comment {
	merged := make([]Comment, 0, len(review)+len(issue))
	merged = append(merged, review...)
	merged = append(merged, issue...)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].CreatedAt < merged[j].CreatedAt
	})
	if len(merged) > MaxCommentsImported {
		merged = merged[len(merged)-MaxCommentsImported:]
	}
	return merged
}

// ComposeReviewPrompt renders the imported comments as the prompt that lands
// in the chat.
//
// The comments are quoted rather than paraphrased, and the header names the
// pull request they came from, so the agent is never guessing which change is
// under review. The closing instruction is fixed text: what to do with review
// feedback should not vary with who imported it.
func ComposeReviewPrompt(fullName string, number int, comments []Comment) string {
	var b strings.Builder
	b.WriteString("Address these PR review comments on #")
	b.WriteString(strconv.Itoa(number))
	if fullName != "" {
		b.WriteString(" (")
		b.WriteString(fullName)
		b.WriteString(")")
	}
	b.WriteString(":\n")
	for index, comment := range comments {
		b.WriteString("\n")
		b.WriteString(strconv.Itoa(index + 1))
		b.WriteString(". ")
		b.WriteString(commentHeading(comment))
		b.WriteString("\n")
		for _, line := range strings.Split(truncate(comment.Body, MaxCommentBodyChars), "\n") {
			b.WriteString("   > ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString(
		"\nWork through every comment above: change the code where the comment is right, " +
			"and where it is not, say so and explain why. " +
			"Do not push or open a pull request — report what you changed when you are done.\n",
	)
	return b.String()
}

// commentHeading is the one-line locator above each quoted comment.
func commentHeading(comment Comment) string {
	author := strings.TrimSpace(comment.Author)
	if author == "" {
		author = "unknown"
	}
	heading := "@" + author
	if comment.Path != "" {
		heading += " on " + comment.Path
		if comment.Line > 0 {
			heading += ":" + strconv.Itoa(comment.Line)
		}
	}
	if comment.Diff != "" {
		heading += "  " + comment.Diff
	}
	return heading
}

// truncate clips one comment body, marking the cut so the agent knows the text
// it is reading is not the whole of what the reviewer wrote.
func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n… (comment truncated)"
}
