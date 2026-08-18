package hostbackup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mkdirs creates one directory per name inside root.
func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
}

func TestBackup(t *testing.T) {
	tests := []struct {
		name string
		// dirs are snapshot directories to create; files are plain files.
		dirs  []string
		files []string
		// missing skips creating the root entirely.
		missing bool

		wantReadable  bool
		wantSnapshots int
		wantLast      string
	}{
		{
			name:         "a host without the backup step is not readable",
			missing:      true,
			wantReadable: false,
		},
		{
			name:          "an empty backup root is readable with nothing in it",
			wantReadable:  true,
			wantSnapshots: 0,
		},
		{
			name:          "the newest completed snapshot wins",
			dirs:          []string{"20260301T033000Z", "20260303T033000Z", "20260302T033000Z"},
			wantReadable:  true,
			wantSnapshots: 3,
			wantLast:      "20260303T033000Z",
		},
		{
			name: "a snapshot still being packed does not count as one",
			dirs: []string{"20260301T033000Z", "20260304T033000Z.partial"},
			// The partial run is the newest thing on disk, and reporting it
			// would vouch for a backup that does not exist yet.
			wantReadable:  true,
			wantSnapshots: 1,
			wantLast:      "20260301T033000Z",
		},
		{
			name:          "directories that are not timestamps are ignored",
			dirs:          []string{"20260301T033000Z", "manual-copy", "lost+found"},
			files:         []string{"README", "SHA256SUMS"},
			wantReadable:  true,
			wantSnapshots: 1,
			wantLast:      "20260301T033000Z",
		},
		{
			name:          "a root holding only unparseable names reports none",
			dirs:          []string{"backup-old", "tmp"},
			wantReadable:  true,
			wantSnapshots: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "backups")
			if !test.missing {
				if err := os.MkdirAll(root, 0o755); err != nil {
					t.Fatalf("mkdir root: %v", err)
				}
				mkdirs(t, root, test.dirs...)
				for _, name := range test.files {
					if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
						t.Fatalf("write %s: %v", name, err)
					}
				}
			}

			info := New(root).Backup(context.Background())

			if info.Root != root {
				t.Fatalf("root = %q, want %q", info.Root, root)
			}
			if info.Readable != test.wantReadable {
				t.Fatalf("readable = %t, want %t", info.Readable, test.wantReadable)
			}
			if info.Snapshots != test.wantSnapshots {
				t.Fatalf("snapshots = %d, want %d", info.Snapshots, test.wantSnapshots)
			}

			if test.wantLast == "" {
				if info.LastAt != 0 {
					t.Fatalf("lastAt = %d, want 0", info.LastAt)
				}
				return
			}
			want, err := time.ParseInLocation(stampLayout, test.wantLast, time.UTC)
			if err != nil {
				t.Fatalf("parse want: %v", err)
			}
			if info.LastAt != want.UnixMilli() {
				t.Fatalf("lastAt = %d, want %d (%s)", info.LastAt, want.UnixMilli(), test.wantLast)
			}
		})
	}
}

// A nil prober is what a deployment that wired no backup root has, and it must
// answer rather than panic.
func TestNilProberReportsNothing(t *testing.T) {
	var prober *Prober
	if info := prober.Backup(context.Background()); info.Readable || info.LastAt != 0 {
		t.Fatalf("nil prober reported %+v, want an empty unreadable result", info)
	}
}

func TestEmptyRootFallsBackToTheInstalledDefault(t *testing.T) {
	if got := New("").root; got != DefaultRoot {
		t.Fatalf("root = %q, want %q", got, DefaultRoot)
	}
}
