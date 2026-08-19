package httphandlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serviceauxmodel "github.com/futrx-com/remote.futrx.com/internal/service/auxmodel"
)

// auxModelTestService answers with whatever the test set up and records what
// the handler asked for.
type auxModelTestService struct {
	public         serviceauxmodel.PublicConfig
	client         serviceauxmodel.ClientConfig
	saveErr        error
	lastInput      serviceauxmodel.UpdateInput
	saves          int
	testResult     serviceauxmodel.TestResult
	tests          int
	translation    string
	translateErr   error
	translations   int
	lastTarget     serviceauxmodel.TranslationTarget
	lastTranslated string
}

func (s *auxModelTestService) PublicConfig() serviceauxmodel.PublicConfig { return s.public }

func (s *auxModelTestService) ClientConfig() serviceauxmodel.ClientConfig { return s.client }

func (s *auxModelTestService) Save(
	_ context.Context,
	input serviceauxmodel.UpdateInput,
) (serviceauxmodel.PublicConfig, error) {
	s.lastInput = input
	if s.saveErr != nil {
		return serviceauxmodel.PublicConfig{}, s.saveErr
	}
	s.saves++
	return s.public, nil
}

func (s *auxModelTestService) Test(context.Context) serviceauxmodel.TestResult {
	s.tests++
	return s.testResult
}

func (s *auxModelTestService) Translate(
	_ context.Context,
	target serviceauxmodel.TranslationTarget,
	text string,
) (string, error) {
	s.translations++
	s.lastTarget = target
	s.lastTranslated = text
	return s.translation, s.translateErr
}

func newAuxModelTestHandler(
	service AuxModelService,
	caller auxModelCaller,
) *http.ServeMux {
	handler := &AuxModelHandler{aux: service, caller: caller}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

func TestAuxModelAdminRoutesRequireAnAdmin(t *testing.T) {
	routes := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/admin/aux-model"},
		{method: http.MethodPut, path: "/api/admin/aux-model", body: `{"enabled":true}`},
		{method: http.MethodPost, path: "/api/admin/aux-model/test"},
	}
	callers := []struct {
		name   string
		caller monitoringTestCaller
		want   int
	}{
		{
			name:   "no session",
			caller: monitoringTestCaller{err: errors.New("no session")},
			want:   http.StatusUnauthorized,
		},
		{
			name:   "a signed-in member is not an operator",
			caller: monitoringTestCaller{email: "member@example.com"},
			want:   http.StatusForbidden,
		},
	}

	for _, route := range routes {
		for _, caller := range callers {
			t.Run(route.method+" "+route.path+" / "+caller.name, func(t *testing.T) {
				service := &auxModelTestService{}
				mux := newAuxModelTestHandler(service, caller.caller)

				recorder := httptest.NewRecorder()
				mux.ServeHTTP(recorder, httptest.NewRequest(
					route.method, route.path, strings.NewReader(route.body)))

				if recorder.Code != caller.want {
					t.Fatalf("status = %d, want %d", recorder.Code, caller.want)
				}
				if service.saves != 0 || service.tests != 0 {
					t.Fatal("the service was reached by a caller that should have been refused")
				}
			})
		}
	}
}

func TestAuxModelSettingsRoundTrip(t *testing.T) {
	service := &auxModelTestService{
		public: serviceauxmodel.Config{
			Enabled:  true,
			Provider: serviceauxmodel.ProviderOpenAICompatible,
			BaseURL:  "https://api.example.com",
			Model:    "gpt-4o-mini",
			APIKey:   "sk-secret-9999",
		}.Normalize().Public(),
	}
	mux := newAuxModelTestHandler(service, monitoringTestCaller{email: "admin@example.com", isAdmin: true})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/aux-model", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "sk-secret-9999") {
		t.Fatalf("the admin view returned the stored key: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "9999") {
		t.Fatal("the admin view dropped the mask an operator recognizes the key by")
	}

	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/admin/aux-model",
		strings.NewReader(`{"enabled":true,"provider":"ollama","baseUrl":"http://127.0.0.1:11434","model":"qwen2.5:3b","jobs":{"commitMessage":false}}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if service.lastInput.Model != "qwen2.5:3b" || service.lastInput.Jobs["commitMessage"] {
		t.Fatalf("the handler forwarded %+v", service.lastInput)
	}
}

func TestAuxModelSaveMapsInvalidConfigTo400(t *testing.T) {
	service := &auxModelTestService{saveErr: serviceauxmodel.ErrInvalidConfig}
	mux := newAuxModelTestHandler(service, monitoringTestCaller{email: "admin@example.com", isAdmin: true})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/admin/aux-model",
		strings.NewReader(`{"enabled":true}`)))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a configuration the operator can fix", recorder.Code)
	}
}

func TestAuxModelTestReportsTheFailureInTheBodyNotAsA500(t *testing.T) {
	service := &auxModelTestService{testResult: serviceauxmodel.TestResult{
		Provider: serviceauxmodel.ProviderOllama,
		Model:    "qwen2.5:3b",
		Error:    "the endpoint responded 500",
	}}
	mux := newAuxModelTestHandler(service, monitoringTestCaller{email: "admin@example.com", isAdmin: true})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/aux-model/test", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with the outcome in the body", recorder.Code)
	}
	var result serviceauxmodel.TestResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if result.OK || result.Error == "" {
		t.Fatalf("result = %+v, want the failure reported", result)
	}
}

func TestAuxModelClientConfigNeedsOnlyASession(t *testing.T) {
	service := &auxModelTestService{client: serviceauxmodel.ClientConfig{
		Enabled: true,
		Jobs:    map[string]bool{"translate": true},
	}}

	t.Run("a member may read it", func(t *testing.T) {
		mux := newAuxModelTestHandler(service, monitoringTestCaller{email: "member@example.com"})
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/aux-model/config", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
	})

	t.Run("an anonymous caller may not", func(t *testing.T) {
		mux := newAuxModelTestHandler(service, monitoringTestCaller{err: errors.New("no session")})
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/aux-model/config", nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", recorder.Code)
		}
	})
}

func TestAuxModelTranslate(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		service    *auxModelTestService
		wantStatus int
		wantTarget serviceauxmodel.TranslationTarget
	}{
		{
			name:       "a message is translated into Arabic",
			body:       `{"text":"your site is ready","target":"ar"}`,
			service:    &auxModelTestService{translation: "موقعك جاهز"},
			wantStatus: http.StatusOK,
			wantTarget: serviceauxmodel.TargetArabic,
		},
		{
			name:       "an unknown target falls back to English rather than failing",
			body:       `{"text":"موقعك جاهز","target":"fr"}`,
			service:    &auxModelTestService{translation: "your site is ready"},
			wantStatus: http.StatusOK,
			wantTarget: serviceauxmodel.TargetEnglish,
		},
		{
			name:       "an empty message is refused before anything is dialled",
			body:       `{"text":"   ","target":"ar"}`,
			service:    &auxModelTestService{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a switched-off model reports 503, never a silent no-op",
			body:       `{"text":"hello","target":"ar"}`,
			service:    &auxModelTestService{translateErr: serviceauxmodel.ErrDisabled},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "an open breaker reports 503 and says where to look",
			body:       `{"text":"hello","target":"ar"}`,
			service:    &auxModelTestService{translateErr: serviceauxmodel.ErrBreakerOpen},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "a misbehaving endpoint is a bad gateway",
			body:       `{"text":"hello","target":"ar"}`,
			service:    &auxModelTestService{translateErr: errors.New("the endpoint responded 500")},
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mux := newAuxModelTestHandler(test.service, monitoringTestCaller{email: "member@example.com"})

			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(
				http.MethodPost, "/api/aux-model/translate", strings.NewReader(test.body)))

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)",
					recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.wantStatus != http.StatusOK {
				return
			}
			if test.service.lastTarget != test.wantTarget {
				t.Fatalf("target = %q, want %q", test.service.lastTarget, test.wantTarget)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["text"] != test.service.translation {
				t.Fatalf("text = %q, want the translation", body["text"])
			}
		})
	}
}

func TestAuxModelTranslateRefusesAnAnonymousCaller(t *testing.T) {
	service := &auxModelTestService{translation: "unused"}
	mux := newAuxModelTestHandler(service, monitoringTestCaller{err: errors.New("no session")})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/aux-model/translate",
		strings.NewReader(`{"text":"hello","target":"ar"}`)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if service.translations != 0 {
		t.Fatal("an anonymous caller reached the model")
	}
}

func TestAuxModelRoutesReport503WithoutAService(t *testing.T) {
	mux := newAuxModelTestHandler(nil, monitoringTestCaller{email: "admin@example.com", isAdmin: true})

	for _, path := range []string{"/api/admin/aux-model", "/api/aux-model/config"} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503", path, recorder.Code)
		}
	}
}
