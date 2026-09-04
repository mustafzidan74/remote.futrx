package globalsecrets

import (
	"context"
	"errors"
	"testing"
)

func sshInput(key, name, domain string, projects ...string) Input {
	return Input{
		Key:   key,
		Kind:  KindSSH,
		Scope: Scope{ProjectIDs: projects},
		SSH: &SSHTarget{
			Name: name, Domain: domain,
			Host: "203.0.113.9", User: "deploy", Port: 65002,
			PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nx\n-----END OPENSSH PRIVATE KEY-----\n",
		},
	}
}

// The switch has to be mechanical, not advisory: off means the container has
// no key, which is what makes it a control rather than a request.
func TestTurningDeployOffTakesTheProjectOutOfScope(t *testing.T) {
	ctx := context.Background()
	service := New(&memoryStore{}, WithClock(fixedClock()))
	if _, err := service.Create(ctx, sshInput("SSH_LIVE", "live", "client.example", "p1"), "ops@example.com"); err != nil {
		t.Fatalf("create: %v", err)
	}

	off, err := service.SetDeployAccess(ctx, "p1", "SSH_LIVE", false, "ops@example.com")
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if off.Enabled {
		t.Fatal("target still reports enabled after being switched off")
	}
	// The material must be out of reach, not merely flagged.
	stored, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stored[0].Scope.Includes("p1") {
		t.Fatal("project is still in scope, so the key would stay in its container")
	}

	on, err := service.SetDeployAccess(ctx, "p1", "SSH_LIVE", true, "ops@example.com")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !on.Enabled {
		t.Fatal("target did not come back on")
	}
}

// One project's switch must not touch another's access.
func TestTheSwitchOnlyMovesTheProjectItNames(t *testing.T) {
	ctx := context.Background()
	service := New(&memoryStore{}, WithClock(fixedClock()))
	if _, err := service.Create(ctx, sshInput("SSH_LIVE", "live", "client.example", "p1", "p2"), "ops@example.com"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.SetDeployAccess(ctx, "p1", "SSH_LIVE", false, "ops@example.com"); err != nil {
		t.Fatalf("disable p1: %v", err)
	}

	targets, err := service.DeployTargets(ctx, "p2")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || !targets[0].Enabled {
		t.Fatalf("p2 lost access when p1 was switched off: %+v", targets)
	}
}

// An "all projects" grant was made somewhere else and is wider than this
// switch can express. Silently narrowing it would revoke every other project.
func TestTheSwitchRefusesToNarrowAnAllProjectsGrant(t *testing.T) {
	ctx := context.Background()
	service := New(&memoryStore{}, WithClock(fixedClock()))
	input := sshInput("SSH_LIVE", "live", "client.example")
	input.Scope = Scope{All: true}
	if _, err := service.Create(ctx, input, "ops@example.com"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.SetDeployAccess(ctx, "p1", "SSH_LIVE", false, "ops@example.com"); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}
}

// The switch grants server access, so it only applies to entries that carry
// server access. An env secret is not a deploy target.
func TestTheSwitchRefusesANonSSHEntry(t *testing.T) {
	ctx := context.Background()
	service := New(&memoryStore{}, WithClock(fixedClock()))
	if _, err := service.Create(ctx, Input{
		Key: "TOKEN", Kind: KindEnv, Value: "x", Scope: Scope{ProjectIDs: []string{"p1"}},
	}, "ops@example.com"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.SetDeployAccess(ctx, "p1", "TOKEN", true, "ops@example.com"); !errors.Is(err, ErrNotSSHSecret) {
		t.Fatalf("expected ErrNotSSHSecret, got %v", err)
	}
}

// The operator asks "whose site is this?", not "which IP is this?".
func TestATargetIsLabelledByItsDomainWhenItHasOne(t *testing.T) {
	ctx := context.Background()
	service := New(&memoryStore{}, WithClock(fixedClock()))
	if _, err := service.Create(ctx, sshInput("SSH_A", "a", "client.example", "p1"), "ops@example.com"); err != nil {
		t.Fatal(err)
	}
	bare := sshInput("SSH_B", "b", "", "p1")
	if _, err := service.Create(ctx, bare, "ops@example.com"); err != nil {
		t.Fatal(err)
	}

	targets, err := service.DeployTargets(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, target := range targets {
		labels[target.Key] = target.Label()
	}
	if labels["SSH_A"] != "client.example" {
		t.Fatalf("expected the domain, got %q", labels["SSH_A"])
	}
	if labels["SSH_B"] != "203.0.113.9" {
		t.Fatalf("expected the host as a fallback, got %q", labels["SSH_B"])
	}
}

func TestDeployTargetsIgnoreEverythingThatIsNotAServer(t *testing.T) {
	ctx := context.Background()
	service := New(&memoryStore{}, WithClock(fixedClock()))
	if _, err := service.Create(ctx, Input{
		Key: "TOKEN", Kind: KindEnv, Value: "x", Scope: Scope{},
	}, "ops@example.com"); err != nil {
		t.Fatal(err)
	}
	targets, err := service.DeployTargets(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 {
		t.Fatalf("an env secret was offered as a deploy target: %+v", targets)
	}
}
