package mcp

import (
	"reflect"
	"strings"
	"testing"
)

func platformServer(name string, scope Scope) Server {
	return Server{Name: name, Transport: TransportStdio, Command: "npx", Scope: scope}
}

func TestAvailableMergesPlatformScopeWithProjectOverrides(t *testing.T) {
	platform := []Server{
		platformServer("fetch", Scope{All: true}),
		platformServer("jira", Scope{ProjectIDs: []string{"p1"}}),
		platformServer("odoo", Scope{ProjectIDs: []string{"p2"}}),
	}

	tests := []struct {
		name        string
		projectID   string
		settings    ProjectSettings
		wantNames   []string
		wantSources map[string]string
		wantEnabled map[string]bool
	}{
		{
			name:        "an all-projects entry reaches every project",
			projectID:   "p3",
			wantNames:   []string{"fetch"},
			wantSources: map[string]string{"fetch": SourcePlatform},
			wantEnabled: map[string]bool{"fetch": true},
		},
		{
			name:        "an explicit scope reaches only the projects it names",
			projectID:   "p1",
			wantNames:   []string{"fetch", "jira"},
			wantSources: map[string]string{"fetch": SourcePlatform, "jira": SourcePlatform},
			wantEnabled: map[string]bool{"fetch": true, "jira": true},
		},
		{
			name:        "a project may switch an inherited entry off",
			projectID:   "p1",
			settings:    ProjectSettings{Disabled: []string{"jira"}},
			wantNames:   []string{"fetch", "jira"},
			wantEnabled: map[string]bool{"fetch": true, "jira": false},
		},
		{
			name:      "a project entry of the same name shadows the platform one",
			projectID: "p1",
			settings: ProjectSettings{Servers: []Server{
				{Name: "jira", Transport: TransportHTTP, URL: "https://only-here.example.com"},
			}},
			wantNames:   []string{"fetch", "jira"},
			wantSources: map[string]string{"fetch": SourcePlatform, "jira": SourceProject},
			wantEnabled: map[string]bool{"fetch": true, "jira": true},
		},
		{
			name:      "a project-only entry is added to what it inherits",
			projectID: "p3",
			settings: ProjectSettings{Servers: []Server{
				{Name: "wc", Transport: TransportHTTP, URL: "https://shop.example.com/mcp"},
			}},
			wantNames:   []string{"fetch", "wc"},
			wantSources: map[string]string{"wc": SourceProject},
			wantEnabled: map[string]bool{"fetch": true, "wc": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := Available(platform, tt.settings, tt.projectID)

			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name)
			}
			if !reflect.DeepEqual(names, tt.wantNames) {
				t.Fatalf("names = %v, want %v", names, tt.wantNames)
			}
			for _, entry := range entries {
				if want, ok := tt.wantSources[entry.Name]; ok && entry.Source != want {
					t.Errorf("%s source = %q, want %q", entry.Name, entry.Source, want)
				}
				if want, ok := tt.wantEnabled[entry.Name]; ok && entry.Enabled != want {
					t.Errorf("%s enabled = %v, want %v", entry.Name, entry.Enabled, want)
				}
			}
		})
	}
}

func TestAvailableStripsAScopeFromAProjectOnlyEntry(t *testing.T) {
	entries := Available(nil, ProjectSettings{Servers: []Server{{
		Name:      "local",
		Transport: TransportStdio,
		Command:   "npx",
		Scope:     Scope{ProjectIDs: []string{"someone-else"}},
	}}}, "p1")
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Scope.All || len(entries[0].Scope.ProjectIDs) != 0 {
		t.Fatalf("project entry kept a scope: %#v", entries[0].Scope)
	}
}

func TestSecretKeysCollectsOnlyEnabledEntries(t *testing.T) {
	entries := []Entry{
		{Server: Server{Name: "a", SecretRefs: []string{"TOKEN_A"}}, Enabled: true},
		{Server: Server{Name: "b", SecretRefs: []string{"TOKEN_B"}}, Enabled: false},
		{Server: Server{Name: "c", SecretRefs: []string{"TOKEN_A", "TOKEN_C"}}, Enabled: true},
	}
	if got, want := SecretKeys(entries), []string{"TOKEN_A", "TOKEN_C"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SecretKeys() = %v, want %v", got, want)
	}
}

func TestResolveSubstitutesEverywhereAValueMayAppear(t *testing.T) {
	entries := []Entry{{
		Enabled: true,
		Server: Server{
			Name:       "jira",
			Transport:  TransportHTTP,
			URL:        "https://jira.example.com/mcp?site=${SITE_ID}",
			Headers:    map[string]string{"Authorization": "Bearer ${JIRA_TOKEN}"},
			SecretRefs: []string{"JIRA_TOKEN", "SITE_ID"},
		},
	}, {
		Enabled: true,
		Server: Server{
			Name:       "pg",
			Transport:  TransportStdio,
			Command:    "npx",
			Args:       []string{"server-postgres", "postgres://app:${PG_PASSWORD}@db/app"},
			Env:        map[string]string{"PGPASSWORD": "${PG_PASSWORD}"},
			SecretRefs: []string{"PG_PASSWORD"},
		},
	}}

	resolution := Resolve(entries, map[string]string{
		"JIRA_TOKEN":  "tok-123",
		"SITE_ID":     "acme",
		"PG_PASSWORD": "s3cr3t",
	})
	if len(resolution.Skipped) != 0 {
		t.Fatalf("skipped = %v, want none", resolution.Skipped)
	}
	byName := map[string]Server{}
	for _, server := range resolution.Servers {
		byName[server.Name] = server
	}
	if got := byName["jira"].URL; got != "https://jira.example.com/mcp?site=acme" {
		t.Errorf("url = %q", got)
	}
	if got := byName["jira"].Headers["Authorization"]; got != "Bearer tok-123" {
		t.Errorf("header = %q", got)
	}
	if got := byName["pg"].Args[1]; got != "postgres://app:s3cr3t@db/app" {
		t.Errorf("arg = %q", got)
	}
	if got := byName["pg"].Env["PGPASSWORD"]; got != "s3cr3t" {
		t.Errorf("env = %q", got)
	}
}

func TestResolveSkipsAnEntryWhoseSecretIsOutOfReach(t *testing.T) {
	entries := []Entry{
		{Enabled: true, Server: Server{
			Name: "jira", Transport: TransportHTTP,
			URL:        "https://jira.example.com/mcp",
			Headers:    map[string]string{"Authorization": "Bearer ${JIRA_TOKEN}"},
			SecretRefs: []string{"JIRA_TOKEN"},
		}},
		{Enabled: true, Server: Server{Name: "fetch", Transport: TransportStdio, Command: "uvx"}},
	}

	resolution := Resolve(entries, nil)
	if !reflect.DeepEqual(resolution.Skipped, []string{"jira"}) {
		t.Fatalf("skipped = %v, want [jira]", resolution.Skipped)
	}
	if len(resolution.Servers) != 1 || resolution.Servers[0].Name != "fetch" {
		t.Fatalf("servers = %#v", resolution.Servers)
	}
	// The skipped entry must not appear anywhere in what gets written.
	material := MaterialFor(resolution)
	for _, file := range material.Files {
		if strings.Contains(file.Content, "jira") || strings.Contains(file.Content, "${") {
			t.Fatalf("a skipped entry or a literal placeholder was rendered: %s", file.Content)
		}
	}
	if strings.Contains(material.CodexRegion, "${") {
		t.Fatalf("a literal placeholder reached the codex region: %s", material.CodexRegion)
	}
}

func TestResolveSkipsDisabledEntries(t *testing.T) {
	resolution := Resolve([]Entry{
		{Enabled: false, Server: Server{Name: "off", Transport: TransportStdio, Command: "npx"}},
	}, nil)
	if len(resolution.Servers) != 0 || len(resolution.Skipped) != 0 {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func TestForProviderSkipsProvidersAnEntryDoesNotName(t *testing.T) {
	servers := []Server{
		{Name: "both"},
		{Name: "claude-only", Providers: []string{ProviderClaude}},
		{Name: "codex-only", Providers: []string{ProviderCodex}},
	}
	tests := []struct {
		provider string
		want     []string
	}{
		{provider: ProviderClaude, want: []string{"both", "claude-only"}},
		{provider: ProviderCodex, want: []string{"both", "codex-only"}},
		// A provider with no MCP support is never asked for, and if it were,
		// it would receive nothing rather than everything.
		{provider: "kimi", want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			if got := Names(ForProvider(servers, tt.provider)); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ForProvider(%q) = %v, want %v", tt.provider, got, tt.want)
			}
		})
	}
}

func TestUnsupportedNamedReportsProvidersThisPlatformCannotConfigure(t *testing.T) {
	got := UnsupportedNamed(Server{Providers: []string{ProviderClaude, "kimi", "antigravity"}})
	if !reflect.DeepEqual(got, []string{"antigravity", "kimi"}) {
		t.Fatalf("UnsupportedNamed() = %v", got)
	}
}
