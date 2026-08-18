package project

import (
	"errors"
	"testing"
)

func TestValidateAgentBrowserURL(t *testing.T) {
	const host = "mz-ss.tech"

	tests := []struct {
		name    string
		url     string
		slug    string
		want    string
		wantErr bool
	}{
		{
			name: "container loopback with a port",
			url:  "http://127.0.0.1:3000/",
			slug: "demo",
			want: "http://127.0.0.1:3000/",
		},
		{
			name: "loopback with a path and query",
			url:  "http://127.0.0.1:5173/admin?tab=1",
			slug: "demo",
			want: "http://127.0.0.1:5173/admin?tab=1",
		},
		{
			name: "localhost is loopback too",
			url:  "http://localhost:8080/",
			slug: "demo",
			want: "http://localhost:8080/",
		},
		{
			name: "ipv6 loopback",
			url:  "http://[::1]:3000/",
			slug: "demo",
			want: "http://[::1]:3000/",
		},
		{
			name: "the project's own preview host",
			url:  "https://demo--3000.dev." + host + "/",
			slug: "demo",
			want: "https://demo--3000.dev." + host + "/",
		},
		{
			name: "the preview host is matched case-insensitively",
			url:  "https://DEMO--3000.dev." + host + "/",
			slug: "demo",
			want: "https://DEMO--3000.dev." + host + "/",
		},
		{
			name:    "another project's preview host",
			url:     "https://other--3000.dev." + host + "/",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "the platform's own host",
			url:     "https://" + host + "/",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "the project's IDE host is not a preview host",
			url:     "https://demo.code." + host + "/",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "an arbitrary internet host",
			url:     "https://evil.example.com/",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "a private LAN address",
			url:     "http://10.0.0.5:3000/",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "file scheme",
			url:     "file:///etc/passwd",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "javascript scheme",
			url:     "javascript:alert(1)",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "data scheme",
			url:     "data:text/html,<h1>hi</h1>",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "embedded credentials",
			url:     "http://user:pass@127.0.0.1:3000/",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "whitespace smuggling a second argument",
			url:     "http://127.0.0.1:3000/ --output /tmp/x",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "a newline in the url",
			url:     "http://127.0.0.1:3000/\nHost: evil",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "empty url",
			url:     "",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "relative url has no host",
			url:     "/admin",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "preview host with a non-numeric port label",
			url:     "https://demo--abc.dev." + host + "/",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "preview host without the port label",
			url:     "https://demo.dev." + host + "/",
			slug:    "demo",
			wantErr: true,
		},
		{
			name:    "a host that only ends with the preview suffix",
			url:     "https://demo--3000.dev." + host + ".evil.com/",
			slug:    "demo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateAgentBrowserURL(tt.url, tt.slug, host)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidBrowserURL) {
					t.Fatalf("error = %v, want ErrInvalidBrowserURL", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateAgentBrowserURL: %v", err)
			}
			if got != tt.want {
				t.Fatalf("url = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateAgentBrowserURLWithoutAConfiguredHostnameStillAllowsLoopback(t *testing.T) {
	if _, err := ValidateAgentBrowserURL("http://127.0.0.1:3000/", "demo", ""); err != nil {
		t.Fatalf("loopback must stay reachable without a public hostname: %v", err)
	}
	if _, err := ValidateAgentBrowserURL("https://demo--3000.dev.example.com/", "demo", ""); !errors.Is(err, ErrInvalidBrowserURL) {
		t.Fatal("a preview host cannot be trusted when no public hostname is configured")
	}
}

func TestAgentBrowserLoopbackURL(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		want    string
		wantErr bool
	}{
		{name: "dev server port", port: 3000, want: "http://127.0.0.1:3000/"},
		{name: "highest port", port: 65535, want: "http://127.0.0.1:65535/"},
		{name: "zero is not a port", port: 0, wantErr: true},
		{name: "negative is not a port", port: -1, wantErr: true},
		{name: "above the range", port: 70000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AgentBrowserLoopbackURL(tt.port)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidBrowserURL) {
					t.Fatalf("error = %v, want ErrInvalidBrowserURL", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("AgentBrowserLoopbackURL: %v", err)
			}
			if got != tt.want {
				t.Fatalf("url = %q, want %q", got, tt.want)
			}
		})
	}
}
