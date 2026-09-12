package auth

import (
	"context"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

type loginAuditRecorder struct {
	entries []audit.Entry
}

func (r *loginAuditRecorder) Record(_ context.Context, entry audit.Entry) {
	r.entries = append(r.entries, entry)
}

// Sign-ins are the trail an operator reads after something went wrong, so the
// claim, a good password, and a bad one must each leave an entry naming who
// tried — including the failed attempt, which has no session to name anyone.
func TestSignInsAndTheAdminClaimAreAudited(t *testing.T) {
	recorder := &loginAuditRecorder{}
	options := authTestOptions()
	options.Audit = recorder
	service := newAuthTestServiceWithOptions(t, &authTestStore{}, newAuthTestUsers(), User{}, options)
	token := issueSetupTokenForTest(t, service)
	ctx := context.Background()

	if _, err := service.ClaimLocalAdmin(ctx, ClaimRequest{
		Email: "admin@example.com", Password: "correct horse battery staple", SetupToken: token,
	}); err != nil {
		t.Fatalf("ClaimLocalAdmin: %v", err)
	}
	if _, err := service.LoginLocal(ctx, "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatalf("LoginLocal: %v", err)
	}
	if _, err := service.LoginLocal(ctx, "Admin@Example.com", "wrong password"); err == nil {
		t.Fatal("wrong password was accepted")
	}

	want := []struct {
		action string
		ok     bool
	}{
		{audit.ActionAuthAdminClaim, true},
		{audit.ActionAuthLoginSuccess, true},
		{audit.ActionAuthLoginFailure, false},
	}
	if len(recorder.entries) != len(want) {
		t.Fatalf("recorded %d entries, want %d: %+v", len(recorder.entries), len(want), recorder.entries)
	}
	for i, expected := range want {
		entry := recorder.entries[i]
		if entry.Action != expected.action || entry.OK != expected.ok {
			t.Errorf("entry %d = %s ok=%v, want %s ok=%v", i, entry.Action, entry.OK, expected.action, expected.ok)
		}
		if entry.Actor.Email != "admin@example.com" {
			t.Errorf("entry %d actor = %q, want the attempted identity", i, entry.Actor.Email)
		}
	}
	if method := recorder.entries[2].Meta["method"]; method != "local" {
		t.Errorf("failed login method = %v, want local", method)
	}
}
