package globalsecrets

import (
	"strings"
	"testing"
)

func TestRenderSSHConfigProducesOneBlockPerTargetInNameOrder(t *testing.T) {
	config := RenderSSHConfig([]SSHTarget{
		{Name: "zeta", Host: "zeta.example.com", User: "root", Port: 2222},
		{Name: "hestia", Host: "203.0.113.10", User: "admin"},
	})

	if got := strings.Count(config, "\nHost ") + strings.Count(config, "Host hestia"); got == 0 {
		t.Fatalf("expected Host blocks, got %q", config)
	}
	if strings.Count(config, "Host ") != 2 {
		t.Fatalf("expected exactly two Host blocks, got %q", config)
	}
	if strings.Index(config, "Host hestia") > strings.Index(config, "Host zeta") {
		t.Fatalf("blocks are not in name order: %q", config)
	}

	want := []string{
		"Host hestia",
		"    HostName 203.0.113.10",
		"    User admin",
		"    Port 22",
		"    IdentityFile /root/.ssh/hestia_key",
		"    IdentitiesOnly yes",
		"    StrictHostKeyChecking accept-new",
		"    Port 2222",
	}
	for _, line := range want {
		if !strings.Contains(config, line+"\n") {
			t.Fatalf("config missing %q:\n%s", line, config)
		}
	}
}

func TestRenderSSHConfigPinsHostKeyWhenKnownHostsLineIsGiven(t *testing.T) {
	config := RenderSSHConfig([]SSHTarget{{
		Name: "hestia", Host: "h.example.com", User: "root",
		KnownHostsLine: "h.example.com ssh-ed25519 AAAAC3Nz",
	}})
	if !strings.Contains(config, "StrictHostKeyChecking yes") {
		t.Fatalf("expected strict checking with a pinned key:\n%s", config)
	}
	if strings.Contains(config, "accept-new") {
		t.Fatalf("pinned target must not accept new keys:\n%s", config)
	}
}

func TestRenderSSHConfigIsIdempotent(t *testing.T) {
	targets := []SSHTarget{
		{Name: "b", Host: "b.example.com", User: "root"},
		{Name: "a", Host: "a.example.com", User: "deploy", Port: 22022},
	}
	first := RenderSSHConfig(targets)
	// Re-rendering from a differently ordered copy of the same input has to
	// produce the same bytes, otherwise every sync would rewrite the file.
	second := RenderSSHConfig([]SSHTarget{targets[1], targets[0]})
	if first != second {
		t.Fatalf("regeneration is not stable:\n%q\n%q", first, second)
	}
	if RenderSSHConfig(nil) != "" {
		t.Fatal("an empty target list must render an empty region")
	}
}

func TestSSHConfigValueQuotesOnlyWhatNeedsIt(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain host", value: "203.0.113.10", want: "203.0.113.10"},
		{name: "path", value: "/root/.ssh/hestia_key", want: "/root/.ssh/hestia_key"},
		{name: "user at host", value: "deploy@example.com", want: "deploy@example.com"},
		{name: "space", value: "my host", want: `"my host"`},
		{name: "quote", value: `a"b`, want: `"a\"b"`},
		{name: "backslash", value: `a\b`, want: `"a\\b"`},
		{name: "empty", value: "", want: `""`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sshConfigValue(test.value); got != test.want {
				t.Fatalf("sshConfigValue(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestRenderKnownHostsKeepsOnePinnedLinePerTarget(t *testing.T) {
	known := RenderKnownHosts([]SSHTarget{
		{Name: "b", KnownHostsLine: "b.example.com ssh-ed25519 BBB"},
		{Name: "a", KnownHostsLine: "a.example.com ssh-ed25519 AAA"},
		{Name: "c"},
	})
	if known != "a.example.com ssh-ed25519 AAA\nb.example.com ssh-ed25519 BBB\n" {
		t.Fatalf("unexpected known_hosts region: %q", known)
	}
	if RenderKnownHosts([]SSHTarget{{Name: "c"}}) != "" {
		t.Fatal("a target without a pinned key must contribute nothing")
	}
}

func TestStaleFilesAndEnvKeysComeFromThePreviousManifest(t *testing.T) {
	previous := Manifest{
		Version: ManifestVersion,
		EnvKeys: []string{"NPM_TOKEN", "OLD_TOKEN", "SSH_TARGET_OLD_HOST"},
		Files:   []string{"/root/.npmrc", "/root/.ssh/old_key", "/root/gone.json"},
	}
	material := Material{
		EnvKeys: []string{"NPM_TOKEN"},
		Files:   []MaterialFile{{Path: "/root/.npmrc", Content: "x"}},
	}

	staleFiles := StaleFiles(previous, material)
	if len(staleFiles) != 2 || staleFiles[0] != "/root/.ssh/old_key" || staleFiles[1] != "/root/gone.json" {
		t.Fatalf("stale files = %v", staleFiles)
	}
	staleEnv := StaleEnvKeys(previous, material)
	if len(staleEnv) != 2 || staleEnv[0] != "OLD_TOKEN" || staleEnv[1] != "SSH_TARGET_OLD_HOST" {
		t.Fatalf("stale env keys = %v", staleEnv)
	}
	if got := StaleFiles(Manifest{}, material); got != nil {
		t.Fatalf("a container with no manifest has nothing stale, got %v", got)
	}
}

func TestManifestForRecordsEverythingMaterialized(t *testing.T) {
	manifest := ManifestFor(Material{
		EnvKeys:  []string{"B", "A"},
		Files:    []MaterialFile{{Path: "/root/z"}, {Path: "/root/a"}},
		SSHNames: []string{"hestia"},
	})
	if manifest.Version != ManifestVersion {
		t.Fatalf("version = %d", manifest.Version)
	}
	if manifest.EnvKeys[0] != "A" || manifest.Files[0] != "/root/a" {
		t.Fatalf("manifest is not sorted: %+v", manifest)
	}
	if len(manifest.SSH) != 1 || manifest.SSH[0] != "hestia" {
		t.Fatalf("ssh names = %v", manifest.SSH)
	}
}

func TestMaskShowsAtMostTheLastFourCharacters(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty stays empty", value: "", want: ""},
		{name: "short value reveals nothing", value: "abcd", want: "••••••••"},
		{name: "long value reveals the tail", value: "ghp_abcdef123456", want: "••••••••3456"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Mask(test.value); got != test.want {
				t.Fatalf("Mask(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestEnvNamesForSSHFollowTheDocumentedContract(t *testing.T) {
	host, user, port := EnvNamesForSSH("hestia-prod.eu")
	if host != "SSH_TARGET_HESTIA_PROD_EU_HOST" {
		t.Fatalf("host var = %q", host)
	}
	if user != "SSH_TARGET_HESTIA_PROD_EU_USER" || port != "SSH_TARGET_HESTIA_PROD_EU_PORT" {
		t.Fatalf("user/port vars = %q %q", user, port)
	}
	if ContainerKeyPath("hestia") != "/root/.ssh/hestia_key" {
		t.Fatalf("key path = %q", ContainerKeyPath("hestia"))
	}
}
