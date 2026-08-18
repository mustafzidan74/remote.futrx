package httphandlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
)

// monitoringTestService answers with whatever the test set up, and records
// what the handler asked for.
type monitoringTestService struct {
	report      servicemonitoring.Report
	public      servicemonitoring.PublicConfig
	saveErr     error
	pingResult  servicemonitoring.PingResult
	pingErr     error
	reports     int
	sawProxied  bool
	lastInput   servicemonitoring.UpdateInput
	pingsIssued int
}

func (s *monitoringTestService) Report(_ context.Context, proxied bool) servicemonitoring.Report {
	s.reports++
	s.sawProxied = proxied
	return s.report
}

func (s *monitoringTestService) PublicConfig() servicemonitoring.PublicConfig { return s.public }

func (s *monitoringTestService) Save(
	_ context.Context,
	input servicemonitoring.UpdateInput,
) (servicemonitoring.PublicConfig, error) {
	s.lastInput = input
	if s.saveErr != nil {
		return servicemonitoring.PublicConfig{}, s.saveErr
	}
	return s.public, nil
}

func (s *monitoringTestService) Ping(context.Context) (servicemonitoring.PingResult, error) {
	s.pingsIssued++
	return s.pingResult, s.pingErr
}

// monitoringTestCaller stands in for the principal resolver.
type monitoringTestCaller struct {
	email   string
	isAdmin bool
	err     error
}

func (c monitoringTestCaller) EmailAndAdmin(context.Context, *http.Request) (string, bool, error) {
	return c.email, c.isAdmin, c.err
}

func newMonitoringTestHandler(
	service MonitoringService,
	caller monitoringCaller,
) (*http.ServeMux, *MonitoringHandler) {
	handler := &MonitoringHandler{
		monitoring: service,
		caller:     caller,
		limiter:    newFixedWindowLimiter(healthzRateLimit, healthzRateWindow),
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux, handler
}

func okReport() servicemonitoring.Report {
	return servicemonitoring.Report{
		Status:  servicemonitoring.StatusOK,
		Version: "v1.2.3",
		Checks: servicemonitoring.Checks{
			Backend: servicemonitoring.StatusOK,
			LXD:     servicemonitoring.StatusOK,
			Caddy:   servicemonitoring.StatusOK,
		},
	}
}

func TestHealthzAnswersWithoutASession(t *testing.T) {
	cases := []struct {
		name       string
		report     servicemonitoring.Report
		wantStatus int
	}{
		{name: "healthy", report: okReport(), wantStatus: http.StatusOK},
		{
			name: "degraded LXD",
			report: servicemonitoring.Report{
				Status:  servicemonitoring.StatusDegraded,
				Version: "v1.2.3",
				Checks: servicemonitoring.Checks{
					Backend: servicemonitoring.StatusOK,
					LXD:     servicemonitoring.StatusDegraded,
					Caddy:   servicemonitoring.StatusOK,
				},
				Details: []string{servicemonitoring.DetailLXD},
			},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "unusable store",
			report: servicemonitoring.Report{
				Status:  servicemonitoring.StatusDegraded,
				Version: "v1.2.3",
				Checks: servicemonitoring.Checks{
					Backend: servicemonitoring.StatusDegraded,
					LXD:     servicemonitoring.StatusOK,
					Caddy:   servicemonitoring.StatusOK,
				},
				Details: []string{servicemonitoring.DetailStore},
			},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mux, _ := newMonitoringTestHandler(
				&monitoringTestService{report: testCase.report},
				// No caller may be resolved: /healthz must never consult one.
				monitoringTestCaller{err: errors.New("no session")},
			)

			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, testCase.wantStatus)
			}
			if cache := recorder.Header().Get("Cache-Control"); cache != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", cache)
			}
			var body servicemonitoring.Report
			if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Status != testCase.report.Status || body.Version != "v1.2.3" {
				t.Fatalf("body = %+v", body)
			}
			if body.Checks != testCase.report.Checks {
				t.Fatalf("checks = %+v, want %+v", body.Checks, testCase.report.Checks)
			}
		})
	}
}

func TestHealthzBodyIsTheUptimeRobotKeyword(t *testing.T) {
	mux, _ := newMonitoringTestHandler(&monitoringTestService{report: okReport()}, monitoringTestCaller{})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	// The documented UptimeRobot keyword check matches this exact substring.
	if !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("body %q does not carry the documented keyword", recorder.Body.String())
	}
}

func TestHealthzSupportsHEADWithTheSameStatus(t *testing.T) {
	service := &monitoringTestService{report: okReport()}
	mux, _ := newMonitoringTestHandler(service, monitoringTestCaller{})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", recorder.Code)
	}
	if service.reports != 1 {
		t.Fatalf("HEAD produced %d reports, want 1", service.reports)
	}
}

func TestHealthzRefusesOtherMethods(t *testing.T) {
	mux, _ := newMonitoringTestHandler(&monitoringTestService{report: okReport()}, monitoringTestCaller{})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
	if allow := recorder.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow = %q", allow)
	}
}

func TestHealthzReadsCaddysForwardingHeadersAsProofTheEdgeIsUp(t *testing.T) {
	service := &monitoringTestService{report: okReport()}
	mux, _ := newMonitoringTestHandler(service, monitoringTestCaller{})

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	mux.ServeHTTP(httptest.NewRecorder(), request)

	if !service.sawProxied {
		t.Fatal("a forwarded request was not reported as proxied")
	}
}

func TestHealthzRateLimitsOneIPWithoutTouchingAnother(t *testing.T) {
	service := &monitoringTestService{report: okReport()}
	mux, handler := newMonitoringTestHandler(service, monitoringTestCaller{})
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	handler.WithClock(func() time.Time { return clock })

	hit := func(ip string) int {
		request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		request.Header.Set("X-Forwarded-For", ip)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		return recorder.Code
	}

	for i := 0; i < healthzRateLimit; i++ {
		if code := hit("203.0.113.9"); code != http.StatusOK {
			t.Fatalf("hit %d returned %d, want 200", i+1, code)
		}
	}
	if code := hit("203.0.113.9"); code != http.StatusTooManyRequests {
		t.Fatalf("hit past the limit returned %d, want 429", code)
	}
	// The report is what costs money; a refused hit must not produce one.
	if service.reports != healthzRateLimit {
		t.Fatalf("produced %d reports, want %d", service.reports, healthzRateLimit)
	}
	// A second client is unaffected by the first one's burst.
	if code := hit("198.51.100.4"); code != http.StatusOK {
		t.Fatalf("a different IP returned %d, want 200", code)
	}
	// And the window tumbles.
	clock = clock.Add(healthzRateWindow + time.Second)
	if code := hit("203.0.113.9"); code != http.StatusOK {
		t.Fatalf("after the window returned %d, want 200", code)
	}
}

func TestHealthzReportsUnavailableWithoutAMonitoringService(t *testing.T) {
	mux, _ := newMonitoringTestHandler(nil, monitoringTestCaller{})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestMonitoringSettingsAreAdminOnly(t *testing.T) {
	cases := []struct {
		name       string
		caller     monitoringTestCaller
		wantStatus int
	}{
		{name: "anonymous", caller: monitoringTestCaller{}, wantStatus: http.StatusUnauthorized},
		{
			name:       "signed in but not an admin",
			caller:     monitoringTestCaller{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "admin",
			caller:     monitoringTestCaller{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusOK,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mux, _ := newMonitoringTestHandler(&monitoringTestService{
				public: servicemonitoring.PublicConfig{
					Configured:         true,
					HeartbeatURLMasked: "hc-ping.com/••••1234",
				},
			}, testCase.caller)

			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/monitoring", nil))

			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, testCase.wantStatus)
			}
			if testCase.wantStatus != http.StatusOK {
				return
			}
			if !strings.Contains(recorder.Body.String(), "••••1234") {
				t.Fatalf("body did not carry the mask: %s", recorder.Body.String())
			}
		})
	}
}

func TestSaveMonitoringSettingsMapsAValidationFailureTo400(t *testing.T) {
	service := &monitoringTestService{
		saveErr: errors.New("invalid monitoring settings: nope"),
	}
	admin := monitoringTestCaller{email: "admin@example.com", isAdmin: true}

	mux, _ := newMonitoringTestHandler(service, admin)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"enabled":true,"heartbeatUrl":"nope","intervalMinutes":5}`)
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/admin/monitoring", body))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("a generic error should be 500, got %d", recorder.Code)
	}

	service.saveErr = servicemonitoring.ErrInvalidConfig
	mux, _ = newMonitoringTestHandler(service, admin)
	recorder = httptest.NewRecorder()
	body = strings.NewReader(`{"enabled":true,"heartbeatUrl":"nope","intervalMinutes":5}`)
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/admin/monitoring", body))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("a validation error should be 400, got %d", recorder.Code)
	}
	if service.lastInput.HeartbeatURL != "nope" {
		t.Fatalf("handler did not forward the body: %+v", service.lastInput)
	}
}

func TestPingNowReportsTheDeliveryOutcomeRatherThanFailing(t *testing.T) {
	service := &monitoringTestService{pingResult: servicemonitoring.PingResult{
		Delivered: false,
		At:        1755500000000,
		Status:    servicemonitoring.PingFailed,
		Error:     "the heartbeat URL responded 404",
	}}
	mux, _ := newMonitoringTestHandler(
		service,
		monitoringTestCaller{email: "admin@example.com", isAdmin: true},
	)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/monitoring/ping", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if service.pingsIssued != 1 {
		t.Fatalf("issued %d pings, want 1", service.pingsIssued)
	}
	var result servicemonitoring.PingResult
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if result.Delivered || result.Status != servicemonitoring.PingFailed {
		t.Fatalf("result = %+v", result)
	}
}
