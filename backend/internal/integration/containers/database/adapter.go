// Package database dumps and restores a project template's in-container
// database.
//
// A template's database is the one piece of durable project state that does
// not live on the host: the WordPress stack runs MariaDB inside the container
// rootfs, which is disposable by design. A snapshot that only archived
// /workspace would restore a WordPress installation with an empty database, so
// the dump is taken through the container runtime and travels inside the same
// archive.
package database

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
)

const (
	probeTimeout = 15 * time.Second
	// dumpTimeout bounds one dump or import. A WordPress database on a small
	// host is seconds; the budget exists to stop a wedged client from hanging
	// a delete.
	dumpTimeout = 10 * time.Minute
)

// EngineMySQL and EnginePostgres name the dump format inside an archive, so a
// restore knows which client to feed it to.
const (
	EngineMySQL    = "mysql"
	EnginePostgres = "postgres"
)

// engine is one supported database, described entirely by the commands it
// needs. Adding another engine is adding a row here.
type engine struct {
	name string
	// dumpTool is probed with `command -v`; its absence means this container
	// does not run that engine.
	dumpTool string
	dump     []string
	restore  []string
}

// engines are probed in order. MariaDB installs both `mysqldump` and
// `mariadb-dump`; probing the classic name covers both because the Debian
// package keeps the compatibility symlinks.
var engines = []engine{
	{
		name:     EngineMySQL,
		dumpTool: "mysqldump",
		dump: []string{
			"mysqldump", "-u", "root", "--all-databases",
			"--single-transaction", "--routines", "--events",
		},
		restore: []string{"mysql", "-u", "root"},
	},
	{
		name:     EnginePostgres,
		dumpTool: "pg_dumpall",
		dump:     []string{"su", "-", "postgres", "-c", "pg_dumpall"},
		restore:  []string{"su", "-", "postgres", "-c", "psql -f -"},
	},
}

// Adapter runs database tooling inside a project container.
type Adapter struct {
	runner command.Runner
}

// NewAdapter returns an adapter backed by runner.
func NewAdapter(runner command.Runner) *Adapter {
	return &Adapter{runner: runner}
}

func (a *Adapter) Available() bool {
	return a != nil && a.runner != nil && a.runner.Available()
}

// Dump returns a logical dump of every database in the container together
// with the engine that produced it. A container without a dump tool answers
// with no bytes, no engine and no error: a template that ships no database is
// the normal case, not a failure.
func (a *Adapter) Dump(ctx context.Context, containerName string) ([]byte, string, error) {
	if !a.Available() || containerName == "" {
		return nil, "", nil
	}
	selected, ok := a.detect(ctx, containerName)
	if !ok {
		return nil, "", nil
	}
	dumpCtx, cancel := context.WithTimeout(ctx, dumpTimeout)
	defer cancel()
	args := append([]string{"exec", containerName, "--"}, selected.dump...)
	out, err := a.runner.Run(dumpCtx, args...)
	if err != nil {
		return nil, "", fmt.Errorf("dump %s database in %s: %w; output: %s",
			selected.name, containerName, err, tail(out))
	}
	if strings.TrimSpace(out) == "" {
		return nil, "", nil
	}
	return []byte(out), selected.name, nil
}

// Import feeds a dump back into the container. The engine recorded with the
// snapshot picks the client; an unknown engine is re-detected so an archive
// written before the engine was recorded still restores.
func (a *Adapter) Import(ctx context.Context, containerName, engineName string, dump []byte) error {
	if !a.Available() || containerName == "" || len(dump) == 0 {
		return nil
	}
	selected, ok := lookup(engineName)
	if !ok {
		selected, ok = a.detect(ctx, containerName)
		if !ok {
			return fmt.Errorf("no database client in %s to import the dump into", containerName)
		}
	}
	importCtx, cancel := context.WithTimeout(ctx, dumpTimeout)
	defer cancel()
	args := append([]string{"exec", containerName, "--"}, selected.restore...)
	out, err := a.runner.RunStdin(importCtx, bytes.NewReader(dump), args...)
	if err != nil {
		return fmt.Errorf("import %s dump into %s: %w; output: %s",
			selected.name, containerName, err, tail(out))
	}
	return nil
}

// detect finds the first engine whose dump tool exists in the container.
func (a *Adapter) detect(ctx context.Context, containerName string) (engine, bool) {
	for _, candidate := range engines {
		out, err := command.RunWithTimeout(
			ctx, a.runner, probeTimeout,
			"exec", containerName, "--", "sh", "-c", "command -v "+candidate.dumpTool,
		)
		if err == nil && strings.TrimSpace(out) != "" {
			return candidate, true
		}
	}
	return engine{}, false
}

func lookup(name string) (engine, bool) {
	for _, candidate := range engines {
		if candidate.name == name {
			return candidate, true
		}
	}
	return engine{}, false
}

// tail keeps an error message readable when a client prints a wall of SQL.
func tail(out string) string {
	out = strings.TrimSpace(out)
	if len(out) <= 500 {
		return out
	}
	return "..." + out[len(out)-500:]
}
