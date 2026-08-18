// Package hostbackup reads the marker directory the host's `remote-backup`
// timer writes into. It is a read-only observer: the platform neither takes
// those backups nor prunes them, it only reports when the last one landed so
// the dashboard can raise "nothing has been backed up in two days".
//
// The layout it reads is the one infra/backup/remote-backup.sh produces:
// one directory per snapshot named for its UTC instant (20060102T150405Z),
// with an in-progress snapshot parked under the same name plus ".partial".
// Only completed directories count.
package hostbackup

import (
	"context"
	"os"
	"strings"
	"time"

	serviceserverinfo "github.com/futrx-com/remote.futrx.com/internal/service/serverinfo"
)

// DefaultRoot is where infra/steps/08-backup.sh installs the snapshots. It is
// overridable because remote-backup.sh honours BACKUP_ROOT.
const DefaultRoot = "/var/backups/remote"

// stampLayout is the directory name remote-backup.sh mints per snapshot.
const stampLayout = "20060102T150405Z"

// partialSuffix marks a snapshot still being packed.
const partialSuffix = ".partial"

// Prober reads one backup root.
type Prober struct {
	root string
}

// New builds a prober for root, falling back to DefaultRoot when root is
// empty so the common deployment needs no configuration.
func New(root string) *Prober {
	if root == "" {
		root = DefaultRoot
	}
	return &Prober{root: root}
}

// Backup lists the root and reports the newest completed snapshot.
//
// Every failure to read is reported as "not readable" rather than as an
// error: the backend runs as root, so the realistic causes are "this host
// never installed the backup step" and "the directory was removed", and
// neither is something the platform should alarm about. Only a root that
// answers can produce a stale-backup finding.
func (p *Prober) Backup(_ context.Context) serviceserverinfo.BackupInfo {
	if p == nil {
		return serviceserverinfo.BackupInfo{}
	}
	info := serviceserverinfo.BackupInfo{Root: p.root}
	entries, err := os.ReadDir(p.root)
	if err != nil {
		return info
	}
	info.Readable = true
	for _, entry := range entries {
		at, ok := snapshotInstant(entry)
		if !ok {
			continue
		}
		info.Snapshots++
		if milli := at.UnixMilli(); milli > info.LastAt {
			info.LastAt = milli
		}
	}
	return info
}

// snapshotInstant reads a directory entry as a completed snapshot. A name
// that does not parse as a timestamp is something else living in the same
// directory (a README, an operator's manual copy) and is ignored rather than
// guessed at from its modification time.
func snapshotInstant(entry os.DirEntry) (time.Time, bool) {
	if !entry.IsDir() {
		return time.Time{}, false
	}
	name := entry.Name()
	if strings.HasSuffix(name, partialSuffix) {
		return time.Time{}, false
	}
	at, err := time.ParseInLocation(stampLayout, name, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return at, true
}
