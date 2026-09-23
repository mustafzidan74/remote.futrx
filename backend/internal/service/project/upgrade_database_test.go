package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// upgradeDatabase stands in for MariaDB inside the container: it remembers
// what was imported and can be told to fail either direction.
type upgradeDatabase struct {
	lifecycle *startTestLifecycle
	dump      []byte
	dumpErr   error
	importErr error

	dumpedWhile ContainerState
	imported    []byte
	importedTo  string
}

func (d *upgradeDatabase) Dump(ctx context.Context, name string) ([]byte, string, error) {
	d.dumpedWhile, _ = d.lifecycle.State(ctx, name)
	return d.dump, "mysql", d.dumpErr
}

func (d *upgradeDatabase) Import(_ context.Context, name, engine string, dump []byte) error {
	if d.importErr != nil {
		return d.importErr
	}
	d.imported, d.importedTo = dump, name+"/"+engine
	return nil
}

func newUpgradeService(t *testing.T, db *upgradeDatabase) (*Service, *startTestRepository, *startTestLifecycle) {
	t.Helper()
	original := upgradeDumpDir
	upgradeDumpDir = t.TempDir()
	t.Cleanup(func() { upgradeDumpDir = original })

	repo := &startTestRepository{meta: Meta{
		ID: ID("abcd"), Name: "project", ContainerName: "project", Status: StatusRunning,
	}}
	lifecycle := &startTestLifecycle{state: ContainerStateRunning}
	db.lifecycle = lifecycle
	service := New(repo, ContainerDependencies{
		Lifecycle: lifecycle, Browser: &upgradeTestBrowser{}, Database: db,
	}, nil, nil)
	return service, repo, lifecycle
}

func dumpsLeft(t *testing.T) []string {
	t.Helper()
	entries, _ := filepath.Glob(filepath.Join(upgradeDumpDir, "*.sql"))
	return entries
}

func TestUpgradeCarriesTheDatabaseIntoTheNewContainer(t *testing.T) {
	db := &upgradeDatabase{dump: []byte("CREATE DATABASE center2;")}
	service, repo, _ := newUpgradeService(t, db)

	if _, err := service.Upgrade(context.Background(), repo.meta.ID, false); err != nil {
		t.Fatal(err)
	}
	if db.dumpedWhile != ContainerStateRunning {
		t.Fatalf("dumped while the container was %q", db.dumpedWhile)
	}
	if string(db.imported) != "CREATE DATABASE center2;" || db.importedTo != "project/mysql" {
		t.Fatalf("imported %q into %q", db.imported, db.importedTo)
	}
	if left := dumpsLeft(t); len(left) != 0 {
		t.Fatalf("host copy not cleaned up after a good import: %v", left)
	}
}

func TestUpgradeLeavesTheContainerAloneWhenTheDumpFails(t *testing.T) {
	db := &upgradeDatabase{dumpErr: errors.New("mysqldump: lost connection")}
	service, repo, lifecycle := newUpgradeService(t, db)

	_, err := service.Upgrade(context.Background(), repo.meta.ID, false)
	if err == nil || !strings.Contains(err.Error(), "container left in place") {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if state, _ := lifecycle.State(context.Background(), "project"); state != ContainerStateRunning {
		t.Fatalf("container was replaced after a failed dump: state %q", state)
	}
}

func TestUpgradeKeepsTheDumpWhenTheImportFails(t *testing.T) {
	db := &upgradeDatabase{
		dump:      []byte("CREATE DATABASE saas;"),
		importErr: errors.New("mariadb not ready"),
	}
	service, repo, _ := newUpgradeService(t, db)

	_, err := service.Upgrade(context.Background(), repo.meta.ID, false)
	left := dumpsLeft(t)
	if len(left) != 1 {
		t.Fatalf("dump files left = %v, want the one copy kept", left)
	}
	if err == nil || !strings.Contains(err.Error(), left[0]) {
		t.Fatalf("error must name the kept dump %s: %v", left[0], err)
	}
	if kept, _ := os.ReadFile(left[0]); string(kept) != "CREATE DATABASE saas;" {
		t.Fatalf("kept dump = %q", kept)
	}
}

func TestUpgradeWithoutADatabaseChangesNothingElse(t *testing.T) {
	db := &upgradeDatabase{}
	service, repo, lifecycle := newUpgradeService(t, db)

	if _, err := service.Upgrade(context.Background(), repo.meta.ID, false); err != nil {
		t.Fatal(err)
	}
	if db.imported != nil || len(dumpsLeft(t)) != 0 {
		t.Fatalf("imported %q, files %v", db.imported, dumpsLeft(t))
	}
	if lifecycle.launchCalls != 2 {
		t.Fatalf("Ensure() calls = %d, want 2", lifecycle.launchCalls)
	}
}
