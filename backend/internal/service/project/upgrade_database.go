package project

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// An upgrade replaces the project's container with a fresh one from the
// current image. /workspace survives because it is bind-mounted from the
// host; the template's database does not, because MariaDB (WordPress,
// Laravel) keeps its files in the container rootfs. Before this, every
// upgrade — including a major in-app update, which recycles every idle
// container — silently dropped those databases, and the WordPress template
// then found an empty database and installed a blank site over the real one.
//
// So an upgrade now dumps every database first, keeps the dump on the host,
// and imports it into the new container. If the dump fails, the old container
// is left exactly as it was.

// upgradeDumpDir holds dumps between the old container's deletion and the new
// one's import. It is on the host disk, so a backend crash in between leaves
// the dump behind instead of losing it.
var upgradeDumpDir = filepath.Join(os.TempDir(), "remote-upgrade-databases")

type upgradeDump struct {
	path   string
	engine string
	data   []byte
}

// saveDatabaseForUpgrade dumps the running container's databases to the host.
// A nil dump with a nil error means there is no database to carry over.
func (s *Service) saveDatabaseForUpgrade(ctx context.Context, m Meta) (*upgradeDump, error) {
	if s.containerDatabase == nil || m.ContainerName == "" {
		return nil, nil
	}
	data, engine, err := s.containerDatabase.Dump(ctx, m.ContainerName)
	if err != nil {
		return nil, fmt.Errorf("container left in place, its database could not be saved: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(upgradeDumpDir, 0o700); err != nil {
		return nil, fmt.Errorf("container left in place, no room to keep its database: %w", err)
	}
	path := filepath.Join(upgradeDumpDir, fmt.Sprintf("%s-%d.sql", m.ID, time.Now().Unix()))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("container left in place, its database could not be kept: %w", err)
	}
	return &upgradeDump{path: path, engine: engine, data: data}, nil
}

// restoreDatabaseAfterUpgrade imports the saved dump into the new container
// and deletes the host copy only once that has worked. On failure the error
// names the file, which is the only remaining copy.
func (s *Service) restoreDatabaseAfterUpgrade(ctx context.Context, m Meta, dump *upgradeDump) error {
	if dump == nil {
		return nil
	}
	if err := s.containerDatabase.Import(ctx, m.ContainerName, dump.engine, dump.data); err != nil {
		return fmt.Errorf("database not restored, the dump is kept at %s: %w", dump.path, err)
	}
	if err := os.Remove(dump.path); err != nil {
		log.Printf("projects: remove upgrade dump %s: %v", dump.path, err)
	}
	return nil
}
