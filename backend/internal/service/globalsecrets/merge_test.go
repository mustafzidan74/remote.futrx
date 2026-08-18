package globalsecrets

import (
	"reflect"
	"testing"
)

func vaultFixture() []Secret {
	return []Secret{
		{
			Key:   "GITHUB_TOKEN",
			Kind:  KindEnv,
			Value: "ghp_global",
			Scope: Scope{All: true},
		},
		{
			Key:   "ACF_LICENCE",
			Kind:  KindEnv,
			Value: "acf-key",
			Scope: Scope{ProjectIDs: []string{"p1"}},
		},
		{
			Key:   "NPMRC",
			Kind:  KindFile,
			Path:  "/root/.npmrc",
			Value: "//registry.npmjs.org/:_authToken=tok",
			Scope: Scope{All: true},
		},
		{
			Key:   "HESTIA",
			Kind:  KindSSH,
			Scope: Scope{ProjectIDs: []string{"p2"}},
			SSH: &SSHTarget{
				Name:       "hestia",
				Host:       "203.0.113.10",
				User:       "admin",
				Port:       2222,
				PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nkey",
			},
		},
	}
}

func TestEnvForAppliesScopeAndProjectShadowing(t *testing.T) {
	tests := []struct {
		name      string
		projectID string
		ownKeys   []string
		want      map[string]string
	}{
		{
			name:      "in-scope project gets the all-projects entry",
			projectID: "p3",
			want:      map[string]string{"GITHUB_TOKEN": "ghp_global"},
		},
		{
			name:      "project-scoped entry reaches only its project",
			projectID: "p1",
			want: map[string]string{
				"GITHUB_TOKEN": "ghp_global",
				"ACF_LICENCE":  "acf-key",
			},
		},
		{
			name:      "a project secret of the same key wins",
			projectID: "p1",
			ownKeys:   []string{"GITHUB_TOKEN"},
			want:      map[string]string{"ACF_LICENCE": "acf-key"},
		},
		{
			name:      "an ssh target publishes its connection contract",
			projectID: "p2",
			want: map[string]string{
				"GITHUB_TOKEN":           "ghp_global",
				"SSH_TARGET_HESTIA_HOST": "203.0.113.10",
				"SSH_TARGET_HESTIA_USER": "admin",
				"SSH_TARGET_HESTIA_PORT": "2222",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EnvFor(vaultFixture(), test.projectID, test.ownKeys)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("EnvFor = %v, want %v", got, test.want)
			}
		})
	}
}

func TestInheritedForReportsShadowingWithoutHidingTheEntry(t *testing.T) {
	inherited := InheritedFor(vaultFixture(), "p1", []string{"GITHUB_TOKEN"})

	byKey := map[string]Inherited{}
	for _, entry := range inherited {
		byKey[entry.Key] = entry
	}
	if len(byKey) != 3 {
		t.Fatalf("expected the three in-scope entries, got %v", byKey)
	}
	if !byKey["GITHUB_TOKEN"].Shadowed {
		t.Fatal("a project secret of the same key must be reported as shadowing")
	}
	if byKey["ACF_LICENCE"].Shadowed {
		t.Fatal("an unshadowed entry must not be flagged")
	}
	if byKey["NPMRC"].Path != "/root/.npmrc" || byKey["NPMRC"].Kind != KindFile {
		t.Fatalf("file entry lost its destination: %+v", byKey["NPMRC"])
	}
	if byKey["GITHUB_TOKEN"].Source != SourceGlobal {
		t.Fatalf("source = %q", byKey["GITHUB_TOKEN"].Source)
	}
	if _, present := byKey["HESTIA"]; present {
		t.Fatal("an out-of-scope entry must not be inherited")
	}
}

func TestInheritedForNeverShadowsAFileOrSSHEntry(t *testing.T) {
	// A project secret named NPMRC is an environment variable; it has nothing
	// to do with the file the vault writes, so it must not be flagged.
	inherited := InheritedFor(vaultFixture(), "p1", []string{"NPMRC"})
	for _, entry := range inherited {
		if entry.Key == "NPMRC" && entry.Shadowed {
			t.Fatal("a file entry cannot be shadowed by an environment secret")
		}
	}
}

func TestMaterialForRendersFilesKeysAndOwnedEnvNames(t *testing.T) {
	material := MaterialFor(vaultFixture(), "p2", nil)

	wantFiles := map[string]string{
		"/root/.npmrc":          "//registry.npmjs.org/:_authToken=tok",
		"/root/.ssh/hestia_key": "-----BEGIN OPENSSH PRIVATE KEY-----\nkey\n",
	}
	if len(material.Files) != len(wantFiles) {
		t.Fatalf("files = %+v", material.Files)
	}
	for _, file := range material.Files {
		if wantFiles[file.Path] != file.Content {
			t.Fatalf("file %s content = %q", file.Path, file.Content)
		}
	}
	wantEnv := []string{
		"GITHUB_TOKEN",
		"SSH_TARGET_HESTIA_HOST",
		"SSH_TARGET_HESTIA_PORT",
		"SSH_TARGET_HESTIA_USER",
	}
	if !reflect.DeepEqual(material.EnvKeys, wantEnv) {
		t.Fatalf("env keys = %v, want %v", material.EnvKeys, wantEnv)
	}
	if len(material.SSHNames) != 1 || material.SSHNames[0] != "hestia" {
		t.Fatalf("ssh names = %v", material.SSHNames)
	}
	if material.SSHConfig == "" {
		t.Fatal("an inherited ssh target must produce a config region")
	}
}

func TestMaterialForDropsEntriesWithNothingToMaterialize(t *testing.T) {
	secrets := []Secret{
		{Key: "CLEARED_ENV", Kind: KindEnv, Scope: Scope{All: true}},
		{Key: "CLEARED_FILE", Kind: KindFile, Path: "/root/.npmrc", Scope: Scope{All: true}},
		{Key: "CLEARED_SSH", Kind: KindSSH, Scope: Scope{All: true}, SSH: &SSHTarget{
			Name: "hestia", Host: "h", User: "u",
		}},
	}
	material := MaterialFor(secrets, "p1", nil)
	if !material.Empty() {
		t.Fatalf("cleared entries must materialize nothing, got %+v", material)
	}
}

func TestScopeNormalizeCollapsesDuplicatesAndAllProjects(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		want  Scope
	}{
		{
			name:  "all wins over an explicit list",
			scope: Scope{All: true, ProjectIDs: []string{"p1"}},
			want:  Scope{All: true},
		},
		{
			name:  "duplicates and blanks are dropped, order is stable",
			scope: Scope{ProjectIDs: []string{"p2", "p1", "p2", " "}},
			want:  Scope{ProjectIDs: []string{"p1", "p2"}},
		},
		{
			name:  "an empty list normalizes to the zero scope",
			scope: Scope{ProjectIDs: []string{" "}},
			want:  Scope{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.scope.Normalize(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Normalize() = %+v, want %+v", got, test.want)
			}
		})
	}
}
