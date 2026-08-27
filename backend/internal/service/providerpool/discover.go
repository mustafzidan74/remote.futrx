package providerpool

// Asking a provider what it actually serves, instead of trusting a list.
//
// The model ids in this registry are typed once and then rot. Within a single
// week of running this platform, Google retired gemini-2.5-flash, Groq dropped
// llama-3.3-70b-versatile, and Zhipu moved from GLM-4.6 to 4.7. Every one of
// them surfaced as a 404 in the middle of real work, and every one was fixed by
// hand — which is the part that does not scale.
//
// Every OpenAI-compatible provider publishes GET {baseUrl}/models, so the
// platform can read the truth rather than remember a guess. What it does with
// that truth is deliberately limited: it reports, and an operator adopts. A
// provider having a model does not mean the operator's key may call it, and
// silently rewriting a configured list from a discovery response would be the
// platform overruling a choice it does not fully understand.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// discoverTimeout bounds one listing. It is short because /models is a static
// read on every provider that offers it, and an operator is waiting.
const discoverTimeout = 20 * time.Second

// ModelsURL is where an OpenAI-compatible provider lists what it serves. The
// base URLs here are complete API roots, so the path is appended exactly as
// ChatCompletionsURL does.
func ModelsURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/models"
}

// Discovery is what one provider reports back.
type Discovery struct {
	ProviderID string `json:"providerId"`
	Label      string `json:"label"`
	// Available is every model id the provider listed, sorted.
	Available []string `json:"available"`
	// Missing is the configured models the provider did not list. These are
	// the ones that answer 404 in the middle of a job.
	Missing []string `json:"missing"`
	// Unlisted is available models the registry does not carry. Purely
	// informational: most providers list far more than anyone wants offered.
	Unlisted []string `json:"unlisted"`
	Error    string   `json:"error,omitempty"`
}

// ModelLister reads a provider's catalog. It is its own port rather than a
// method on Completer because a provider can serve completions without
// publishing a catalog, and the pool must keep working when it does not.
type ModelLister interface {
	ListModels(ctx context.Context, baseURL, apiKey string) ([]string, error)
}

type httpModelLister struct{ http *http.Client }

// NewHTTPModelLister reads GET {baseUrl}/models.
func NewHTTPModelLister(client *http.Client) ModelLister {
	if client == nil {
		client = http.DefaultClient
	}
	return httpModelLister{http: client}
}

// modelsResponse covers both shapes seen in the wild: OpenAI's {"data":[...]}
// and the plain {"models":[...]} some gateways return.
type modelsResponse struct {
	Data []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data"`
	Models []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"models"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (l httpModelLister) ListModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, ModelsURL(baseURL), nil)
	if err != nil {
		return nil, err
	}
	if key := strings.TrimSpace(apiKey); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}

	response, err := l.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var decoded modelsResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("the provider did not answer /models with JSON")
	}
	if message := strings.TrimSpace(decoded.Error.Message); message != "" {
		return nil, fmt.Errorf("the provider responded %d: %s", response.StatusCode, message)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("the provider responded %d to /models", response.StatusCode)
	}

	rows := decoded.Data
	if len(rows) == 0 {
		rows = decoded.Models
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.ID)
		if id == "" {
			id = strings.TrimSpace(row.Name)
		}
		// Google prefixes ids with "models/" in its listing and rejects that
		// prefix on the completion route, so a listing taken literally would
		// hand an operator ids that cannot be used.
		id = strings.TrimPrefix(id, "models/")
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("the provider listed no models")
	}
	sort.Strings(ids)
	return ids, nil
}

// compare works out which configured models have gone and what is on offer.
//
// Ids are matched literally, suffix and all. That looked wrong at first:
// OpenRouter lists liquid/lfm-2.5-2.6b:free with its :free suffix and
// nvidia/nemotron-3-nano-30b-a3b without one, which reads like an
// inconsistency worth smoothing over. It is not. Calling each of them proves
// the catalog is exact — the :free route answers only for the ids listed with
// :free, and 404s for the ones listed without. The suffix is part of the
// model's identity here, not an alias for it.
//
// Treating them as the same model hid a genuinely dead id that was configured
// and in use. A false negative in this direction is the expensive one: the
// whole feature exists to catch exactly that.
func compare(configured, available []string) (missing, unlisted []string) {
	live := make(map[string]bool, len(available))
	for _, id := range available {
		live[id] = true
	}
	known := make(map[string]bool, len(configured))
	for _, id := range configured {
		known[id] = true
		if !live[id] {
			missing = append(missing, id)
		}
	}
	for _, id := range available {
		if !known[id] {
			unlisted = append(unlisted, id)
		}
	}
	return missing, unlisted
}
