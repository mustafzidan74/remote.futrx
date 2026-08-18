package monitoring

import "testing"

func TestMaskURLKeepsTheHostAndTheLastFourCharacters(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{
			name: "healthchecks uuid",
			raw:  "https://hc-ping.com/9f3a1c72-5b6d-4e21-9f0c-2b7ad4e51234",
			want: "hc-ping.com/••••1234",
		},
		{
			name: "uptimerobot heartbeat",
			raw:  "https://heartbeat.uptimerobot.com/m794512345-abcdef",
			want: "heartbeat.uptimerobot.com/••••cdef",
		},
		{
			name: "better stack with a trailing slash",
			raw:  "https://uptime.betterstack.com/api/v1/heartbeat/aBcDeF12/",
			want: "uptime.betterstack.com/••••eF12",
		},
		{
			name: "token in the query string",
			raw:  "https://example.com/?token=secret9876",
			want: "example.com/••••9876",
		},
		{
			name: "token too short to reveal any of it",
			raw:  "https://example.com/abc",
			want: "example.com/••••",
		},
		{
			name: "port survives because it is not the secret",
			raw:  "http://127.0.0.1:8080/ping/token1234",
			want: "127.0.0.1:8080/••••1234",
		},
		{name: "unparseable", raw: "not a url", want: "••••"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := MaskURL(testCase.raw); got != testCase.want {
				t.Fatalf("MaskURL(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}

func TestPublicNeverEchoesTheStoredURL(t *testing.T) {
	const secret = "https://hc-ping.com/9f3a1c72-5b6d-4e21-9f0c-2b7ad4e51234"
	public := Config{Enabled: true, HeartbeatURL: secret, IntervalMinutes: 7}.Public()

	if !public.Configured || !public.Enabled {
		t.Fatalf("public view lost the configured/enabled flags: %+v", public)
	}
	if public.HeartbeatURLMasked != "hc-ping.com/••••1234" {
		t.Fatalf("masked URL = %q", public.HeartbeatURLMasked)
	}
	if public.HeartbeatHost != "hc-ping.com" {
		t.Fatalf("host = %q", public.HeartbeatHost)
	}
	if public.IntervalMinutes != 7 {
		t.Fatalf("interval = %d, want 7", public.IntervalMinutes)
	}
	if public.HealthPath != HealthPath {
		t.Fatalf("health path = %q, want %q", public.HealthPath, HealthPath)
	}
	// The whole point of the mask: the secret must not appear in any field.
	for name, value := range map[string]string{
		"masked": public.HeartbeatURLMasked,
		"host":   public.HeartbeatHost,
		"error":  public.LastPingError,
	} {
		if value == secret {
			t.Fatalf("field %s leaked the stored URL", name)
		}
	}
}

func TestNormalizeClampsTheInterval(t *testing.T) {
	cases := []struct {
		name  string
		given int
		want  int
	}{
		{name: "zero takes the default", given: 0, want: DefaultIntervalMinutes},
		{name: "negative takes the default", given: -5, want: DefaultIntervalMinutes},
		{name: "minimum is kept", given: MinIntervalMinutes, want: MinIntervalMinutes},
		{name: "maximum is kept", given: MaxIntervalMinutes, want: MaxIntervalMinutes},
		{name: "above the maximum is clamped", given: 1440, want: MaxIntervalMinutes},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := Config{IntervalMinutes: testCase.given}.Normalize().IntervalMinutes
			if got != testCase.want {
				t.Fatalf("interval = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestApplyFollowsWriteOnlyURLSemantics(t *testing.T) {
	const stored = "https://hc-ping.com/token-aaaa"
	current := Config{
		Enabled:        true,
		HeartbeatURL:   stored,
		LastPingAt:     1700,
		LastPingStatus: PingOK,
	}

	cases := []struct {
		name           string
		input          UpdateInput
		wantURL        string
		wantEnabled    bool
		wantPingRecord bool
	}{
		{
			name:           "a blank URL keeps the stored one",
			input:          UpdateInput{Enabled: true, IntervalMinutes: 5},
			wantURL:        stored,
			wantEnabled:    true,
			wantPingRecord: true,
		},
		{
			name:           "a new URL replaces it and forgets the old outcome",
			input:          UpdateInput{Enabled: true, HeartbeatURL: "  https://hc-ping.com/token-bbbb  ", IntervalMinutes: 5},
			wantURL:        "https://hc-ping.com/token-bbbb",
			wantEnabled:    true,
			wantPingRecord: false,
		},
		{
			name:           "clearing removes the URL and forces the toggle off",
			input:          UpdateInput{Enabled: true, ClearHeartbeat: true, IntervalMinutes: 5},
			wantURL:        "",
			wantEnabled:    false,
			wantPingRecord: false,
		},
		{
			name:           "turning it off keeps the URL",
			input:          UpdateInput{Enabled: false, IntervalMinutes: 5},
			wantURL:        stored,
			wantEnabled:    false,
			wantPingRecord: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			next := current.Apply(testCase.input)
			if next.HeartbeatURL != testCase.wantURL {
				t.Fatalf("URL = %q, want %q", next.HeartbeatURL, testCase.wantURL)
			}
			if next.Enabled != testCase.wantEnabled {
				t.Fatalf("enabled = %t, want %t", next.Enabled, testCase.wantEnabled)
			}
			if kept := next.LastPingAt != 0; kept != testCase.wantPingRecord {
				t.Fatalf("kept last-ping record = %t, want %t", kept, testCase.wantPingRecord)
			}
		})
	}
}
