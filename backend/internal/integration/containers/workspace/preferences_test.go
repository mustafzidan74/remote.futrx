package workspace

import (
	"strings"
	"testing"
)

func TestApplyManagedBlock(t *testing.T) {
	const userText = "# My project\n\nRun `make dev` before anything else.\n"
	const body = "## Reply preferences (managed by remote.futrx)\n\n- Reply in Egyptian Arabic."
	const otherBody = "## Reply preferences (managed by remote.futrx)\n\n- Reply in English."

	tests := []struct {
		name     string
		existing string
		body     string
		want     []string
		absent   []string
	}{
		{
			name:     "inserts into an empty file",
			existing: "",
			body:     body,
			want:     []string{BlockOpenMarker, "Egyptian Arabic", BlockCloseMarker},
		},
		{
			name:     "appends below existing user text",
			existing: userText,
			body:     body,
			want:     []string{"# My project", "make dev", BlockOpenMarker, "Egyptian Arabic"},
		},
		{
			name: "replaces only the managed region",
			existing: userText + "\n" + BlockOpenMarker + "\n" + body + "\n" + BlockCloseMarker +
				"\n\n## Notes\n\nKeep this.\n",
			body:   otherBody,
			want:   []string{"# My project", "Reply in English", "## Notes", "Keep this."},
			absent: []string{"Egyptian Arabic"},
		},
		{
			name:     "an empty body removes the block and its markers",
			existing: userText + "\n" + BlockOpenMarker + "\n" + body + "\n" + BlockCloseMarker + "\n",
			body:     "",
			want:     []string{"# My project", "make dev"},
			absent:   []string{BlockOpenMarker, BlockCloseMarker, "Egyptian Arabic"},
		},
		{
			name:     "an empty body on a file that never had a block is a no-op",
			existing: userText,
			body:     "",
			want:     []string{"# My project"},
			absent:   []string{BlockOpenMarker},
		},
		{
			name:     "an unterminated block is replaced, keeping the user text above it",
			existing: userText + "\n" + BlockOpenMarker + "\nhalf a block\n",
			body:     body,
			want:     []string{"# My project", "make dev", BlockCloseMarker, "Egyptian Arabic"},
			absent:   []string{"half a block"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ApplyManagedBlock(test.existing, test.body)
			for _, fragment := range test.want {
				if !strings.Contains(got, fragment) {
					t.Errorf("ApplyManagedBlock() = %q, missing %q", got, fragment)
				}
			}
			for _, fragment := range test.absent {
				if strings.Contains(got, fragment) {
					t.Errorf("ApplyManagedBlock() = %q, unexpectedly contains %q", got, fragment)
				}
			}

			// The writer runs on every prompt, so a second pass over its own
			// output must be byte-identical or the file would churn forever.
			if again := ApplyManagedBlock(got, test.body); again != got {
				t.Errorf("ApplyManagedBlock() is not idempotent:\nfirst:  %q\nsecond: %q", got, again)
			}
		})
	}
}

func TestApplyManagedBlockKeepsExactlyOneBlock(t *testing.T) {
	body := "## Reply preferences (managed by remote.futrx)\n\n- Reply in English."
	content := ""
	for round := 0; round < 3; round++ {
		content = ApplyManagedBlock(content, body)
	}
	if got := strings.Count(content, BlockOpenMarker); got != 1 {
		t.Fatalf("open markers = %d, want 1 (content: %q)", got, content)
	}
	if got := strings.Count(content, BlockCloseMarker); got != 1 {
		t.Fatalf("close markers = %d, want 1 (content: %q)", got, content)
	}
}
