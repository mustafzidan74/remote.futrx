package providerpool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// The exact failures this feature exists to end. Each of these ids was
// configured, worked, and then stopped working when the vendor retired it
// mid-week; every one surfaced as a 404 in the middle of real work.
func TestDiscoveryNamesTheModelsThatHaveGone(t *testing.T) {
	configured := []string{"gemini-2.5-flash", "gemini-flash-latest"}
	available := []string{"gemini-flash-latest", "gemini-flash-lite-latest", "gemini-3.7-flash"}

	missing, unlisted := compare(configured, available)

	if !slices.Equal(missing, []string{"gemini-2.5-flash"}) {
		t.Fatalf("missing = %v, want the retired id", missing)
	}
	if !slices.Contains(unlisted, "gemini-3.7-flash") {
		t.Errorf("unlisted = %v, want the newer model an operator could adopt", unlisted)
	}
	// A configured model that still exists is not news.
	if slices.Contains(missing, "gemini-flash-latest") {
		t.Error("a working model was reported as missing")
	}
}

// Google's listing prefixes every id with "models/", and its completion route
// rejects that prefix. Passing the listing through untouched would hand an
// operator ids that cannot be called, which is the same 404 in a new costume.
func TestGooglesModelPrefixIsStripped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"models/gemini-flash-latest"},{"id":"models/gemma-4-31b-it"}]}`)
	}))
	defer server.Close()

	ids, err := NewHTTPModelLister(server.Client()).ListModels(context.Background(), server.URL, "k")
	if err != nil {
		t.Fatalf("ListModels() = %v", err)
	}
	if !slices.Equal(ids, []string{"gemini-flash-latest", "gemma-4-31b-it"}) {
		t.Fatalf("ids = %v, want the prefix gone", ids)
	}
}

// Not every gateway answers OpenAI's shape.
func TestBothListingShapesAreRead(t *testing.T) {
	for name, body := range map[string]string{
		"openai": `{"data":[{"id":"a"},{"id":"b"}]}`,
		"plain":  `{"models":[{"id":"a"},{"id":"b"}]}`,
		"named":  `{"data":[{"name":"a"},{"name":"b"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, body)
			}))
			defer server.Close()

			ids, err := NewHTTPModelLister(server.Client()).ListModels(context.Background(), server.URL, "k")
			if err != nil {
				t.Fatalf("ListModels() = %v", err)
			}
			if !slices.Equal(ids, []string{"a", "b"}) {
				t.Fatalf("ids = %v", ids)
			}
		})
	}
}

// A provider that refuses must say so rather than read as "you have no models",
// which would invite an operator to adopt an empty list.
func TestARefusalIsAnErrorNotAnEmptyCatalog(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"401": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"message":"Invalid API Key"}}`)
		},
		"html": func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "<html>nope</html>")
		},
		"empty": func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"data":[]}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()

			if _, err := NewHTTPModelLister(server.Client()).ListModels(context.Background(), server.URL, "k"); err == nil {
				t.Fatal("ListModels() = nil, want an error the operator can act on")
			}
		})
	}
}

// The base URLs in this registry are complete API roots, so /models is
// appended exactly as /chat/completions is.
func TestModelsURLMatchesTheCompletionsURL(t *testing.T) {
	for _, base := range []string{
		"https://api.groq.com/openai/v1",
		"https://api.groq.com/openai/v1/",
	} {
		if got := ModelsURL(base); got != "https://api.groq.com/openai/v1/models" {
			t.Errorf("ModelsURL(%q) = %q", base, got)
		}
	}
}
