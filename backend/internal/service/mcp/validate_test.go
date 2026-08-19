package mcp

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeRejectsWhatWouldProduceAnUnusableConfig(t *testing.T) {
	tests := []struct {
		name    string
		server  Server
		wantErr error
	}{
		{
			name:    "an empty name",
			server:  Server{Transport: TransportStdio, Command: "npx", Scope: Scope{All: true}},
			wantErr: ErrInvalidName,
		},
		{
			name:    "a name with a space",
			server:  Server{Name: "my server", Transport: TransportStdio, Command: "npx", Scope: Scope{All: true}},
			wantErr: ErrInvalidName,
		},
		{
			name:    "an unknown transport",
			server:  Server{Name: "x", Transport: "sse", Scope: Scope{All: true}},
			wantErr: ErrInvalidKind,
		},
		{
			name:    "a stdio entry with no command",
			server:  Server{Name: "x", Transport: TransportStdio, Scope: Scope{All: true}},
			wantErr: ErrNoCommand,
		},
		{
			name:    "an http entry with no URL",
			server:  Server{Name: "x", Transport: TransportHTTP, Scope: Scope{All: true}},
			wantErr: ErrNoURL,
		},
		{
			name:    "an http entry with a non-http scheme",
			server:  Server{Name: "x", Transport: TransportHTTP, URL: "file:///etc/passwd", Scope: Scope{All: true}},
			wantErr: ErrNoURL,
		},
		{
			name: "an argument spanning lines",
			server: Server{Name: "x", Transport: TransportStdio, Command: "npx",
				Args: []string{"a\nb"}, Scope: Scope{All: true}},
			wantErr: ErrInvalidArg,
		},
		{
			name: "an environment name that is not a POSIX name",
			server: Server{Name: "x", Transport: TransportStdio, Command: "npx",
				Env: map[string]string{"not-a-name": "v"}, Scope: Scope{All: true}},
			wantErr: ErrInvalidEnv,
		},
		{
			name: "a header name with a colon",
			server: Server{Name: "x", Transport: TransportHTTP, URL: "https://e.example.com",
				Headers: map[string]string{"Auth:orization": "v"}, Scope: Scope{All: true}},
			wantErr: ErrInvalidHeader,
		},
		{
			name:    "a platform entry scoped to nothing",
			server:  Server{Name: "x", Transport: TransportStdio, Command: "npx"},
			wantErr: ErrInvalidScope,
		},
		{
			name: "a provider that cannot be configured",
			server: Server{Name: "x", Transport: TransportStdio, Command: "npx",
				Providers: []string{"kimi"}, Scope: Scope{All: true}},
			wantErr: ErrProvider,
		},
		{
			name: "a secret ref that is not a vault key",
			server: Server{Name: "x", Transport: TransportStdio, Command: "npx",
				SecretRefs: []string{"not a key"}, Scope: Scope{All: true}},
			wantErr: ErrSecretRef,
		},
		{
			name: "a placeholder nobody declared",
			server: Server{Name: "x", Transport: TransportStdio, Command: "npx",
				Env: map[string]string{"TOKEN": "${UNDECLARED}"}, Scope: Scope{All: true}},
			wantErr: ErrSecretRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Normalize(tt.server, true); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Normalize() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeCanonicalizesAValidEntry(t *testing.T) {
	got, err := Normalize(Server{
		Name:        "  playwright  ",
		Transport:   TransportStdio,
		Command:     " npx ",
		Args:        []string{"@playwright/mcp@latest"},
		Env:         map[string]string{"TOKEN": "${B_KEY}", "OTHER": "${A_KEY}"},
		Headers:     map[string]string{"Authorization": "dropped for stdio"},
		URL:         "https://dropped.example.com",
		Providers:   []string{"codex", "claude", "codex"},
		SecretRefs:  []string{"B_KEY", "A_KEY", "B_KEY"},
		Scope:       Scope{ProjectIDs: []string{"p2", "p1", "p1"}},
		Description: "  browser tools  ",
	}, true)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if got.Name != "playwright" || got.Command != "npx" {
		t.Errorf("name/command = %q/%q", got.Name, got.Command)
	}
	if got.URL != "" || got.Headers != nil {
		t.Errorf("http fields survived on a stdio entry: %q %v", got.URL, got.Headers)
	}
	// Providers are stored in the platform's order, not the caller's, so two
	// equal selections serialize equally.
	if !reflect.DeepEqual(got.Providers, []string{ProviderClaude, ProviderCodex}) {
		t.Errorf("providers = %v", got.Providers)
	}
	if !reflect.DeepEqual(got.SecretRefs, []string{"A_KEY", "B_KEY"}) {
		t.Errorf("secretRefs = %v", got.SecretRefs)
	}
	if !reflect.DeepEqual(got.Scope.ProjectIDs, []string{"p1", "p2"}) {
		t.Errorf("scope = %v", got.Scope)
	}
	if got.Description != "browser tools" {
		t.Errorf("description = %q", got.Description)
	}
}

func TestNormalizeDropsEnvFromAnHTTPEntry(t *testing.T) {
	got, err := Normalize(Server{
		Name:      "jira",
		Transport: TransportHTTP,
		URL:       "https://jira.example.com/mcp",
		Env:       map[string]string{"IGNORED": "1"},
		Command:   "npx",
		Scope:     Scope{All: true},
	}, true)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Env != nil || got.Command != "" {
		t.Fatalf("stdio fields survived on an http entry: %v %q", got.Env, got.Command)
	}
}

func TestNormalizeAcceptsAProjectEntryWithoutAScope(t *testing.T) {
	got, err := Normalize(Server{Name: "local", Transport: TransportStdio, Command: "npx"}, false)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Scope.All || len(got.Scope.ProjectIDs) != 0 {
		t.Fatalf("a project entry gained a scope: %#v", got.Scope)
	}
}

func TestEffectiveProvidersDefaultsToEverySupportedCLI(t *testing.T) {
	if got := (Server{}).EffectiveProviders(); !reflect.DeepEqual(got, SupportedProviders()) {
		t.Fatalf("EffectiveProviders() = %v", got)
	}
	if got := (Server{Providers: []string{ProviderCodex}}).EffectiveProviders(); !reflect.DeepEqual(got, []string{ProviderCodex}) {
		t.Fatalf("EffectiveProviders() = %v", got)
	}
}

func TestPlaceholdersFindsEveryReference(t *testing.T) {
	got := Placeholders(Server{
		URL:     "https://e.example.com/${SITE}",
		Args:    []string{"--token=${TOKEN}"},
		Env:     map[string]string{"A": "${TOKEN}"},
		Headers: map[string]string{"X-Key": "${HEADER_KEY}"},
	})
	if !reflect.DeepEqual(got, []string{"HEADER_KEY", "SITE", "TOKEN"}) {
		t.Fatalf("Placeholders() = %v", got)
	}
}

func TestScopeIncludes(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		id    string
		want  bool
	}{
		{name: "all covers anything", scope: Scope{All: true}, id: "p9", want: true},
		{name: "explicit hit", scope: Scope{ProjectIDs: []string{"p1", "p2"}}, id: "p2", want: true},
		{name: "explicit miss", scope: Scope{ProjectIDs: []string{"p1"}}, id: "p2", want: false},
		{name: "empty covers nothing", scope: Scope{}, id: "p1", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.Includes(tt.id); got != tt.want {
				t.Fatalf("Includes(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
