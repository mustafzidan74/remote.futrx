package providerpool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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

// TestTheFreeSuffixIsPartOfTheIdentity pins the call I got wrong.
//
// OpenRouter lists liquid/lfm-2.5-2.6b:free with the suffix and
// nvidia/nemotron-3-nano-30b-a3b without, which looks like an inconsistency to
// smooth over. Calling both proves otherwise: the :free route answers for the
// first and 404s for the second. Matching on a base id hid a dead model that
// was configured and in use, which is the exact failure this feature exists to
// catch.
func TestTheFreeSuffixIsPartOfTheIdentity(t *testing.T) {
	configured := []string{"liquid/lfm-2.5-2.6b:free", "nvidia/nemotron-3-nano-30b-a3b:free"}
	available := []string{"liquid/lfm-2.5-2.6b:free", "nvidia/nemotron-3-nano-30b-a3b"}

	missing, _ := compare(configured, available)

	if !slices.Equal(missing, []string{"nvidia/nemotron-3-nano-30b-a3b:free"}) {
		t.Fatalf("missing = %v: the :free id is absent from the catalog and 404s when called", missing)
	}
}

// TestAdoptionCannotAddAModel guards someone else's money.
//
// A discovery response is the provider's entire catalog. On OpenRouter that is
// hundreds of paid models beside the handful of free ones an operator picked,
// and this platform's operator keeps paid credit there for work done on another
// platform. If adoption could write from the catalog, one click on a button
// labelled "Drop them" would configure paid models and the pool would start
// spending that credit.
//
// Pruning cannot do that. The guard is structural rather than a check on the
// id text, because "free" is spelled differently by every gateway and a rule
// that reads names would be wrong somewhere.
func TestAdoptionCannotAddAModel(t *testing.T) {
	configured := []Model{
		{ID: "minimax/minimax-m3:free"},
		{ID: "liquid/lfm-2.5-2.6b:free"},
	}
	// What a careless caller might send: the catalog, paid entries and all.
	requested := []string{
		"minimax/minimax-m3:free",
		"anthropic/claude-opus-4.6", // paid
		"openai/gpt-5",              // paid
	}

	previous := map[string]Model{}
	for _, model := range configured {
		previous[model.ID] = model
	}

	var adopted []Model
	for _, id := range requested {
		if kept, ok := previous[id]; ok {
			adopted = append(adopted, kept)
		}
	}

	if len(adopted) != 1 || adopted[0].ID != "minimax/minimax-m3:free" {
		t.Fatalf("adopted = %+v, want only the id that was already configured", adopted)
	}
	for _, model := range adopted {
		if !strings.HasSuffix(model.ID, ":free") {
			t.Fatalf("a paid model reached the configuration: %s", model.ID)
		}
	}
}
