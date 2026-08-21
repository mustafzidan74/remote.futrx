package filechat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// The endpoint handle is what makes a chat run somewhere other than its
// vendor's own endpoint. Losing it in a round trip would silently move a
// chat's next turn back to a first-party model — a change of who is being
// paid and who sees the code, made invisibly.
func TestStoreRoundTripsTheEndpointHandle(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, servicechat.Meta{
		Title:      "Brochure site copy",
		Provider:   servicechat.ProviderClaude,
		Model:      "glm-4.6",
		EndpointID: "zhipu-glm",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.EndpointID != "zhipu-glm" {
		t.Fatalf("Create returned endpointId %q, want zhipu-glm", created.EndpointID)
	}

	loaded, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EndpointID != "zhipu-glm" {
		t.Errorf("reloaded endpointId = %q, want zhipu-glm", loaded.EndpointID)
	}
	if loaded.Model != "glm-4.6" {
		t.Errorf("reloaded model = %q, want glm-4.6", loaded.Model)
	}

	// The stored document must carry the handle by name, so an operator
	// reading meta.json can see where a chat's work is going.
	data, err := os.ReadFile(filepath.Join(root, "chats", string(created.ID), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"endpointId": "zhipu-glm"`) {
		t.Errorf("meta.json does not name the endpoint:\n%s", data)
	}
	// And it must never carry anything resembling a credential: the handle is
	// all a chat ever stores.
	for _, forbidden := range []string{"apiKeyRef", "ANTHROPIC_AUTH_TOKEN", "baseUrl"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("meta.json carries %q, which belongs to the register alone", forbidden)
		}
	}
}

// Clearing the handle has to survive too: pointing a chat back at the
// vendor's own endpoint is the escape hatch, and a field that reappeared
// after a restart would make it useless.
func TestStoreRoundTripsClearingTheEndpointHandle(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, servicechat.Meta{
		Title:      "Brochure site copy",
		Provider:   servicechat.ProviderClaude,
		EndpointID: "zhipu-glm",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(ctx, created.ID, func(m *servicechat.Meta) {
		m.EndpointID = ""
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EndpointID != "" {
		t.Errorf("reloaded endpointId = %q, want it cleared", loaded.EndpointID)
	}
}
