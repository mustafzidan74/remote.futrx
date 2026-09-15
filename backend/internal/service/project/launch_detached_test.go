package project

import (
	"context"
	"testing"
)

// cancellingLifecycle cancels the caller's request while the container is
// being launched, the way a closed tab or a dropped connection does.
type cancellingLifecycle struct {
	*startTestLifecycle
	cancelRequest context.CancelFunc
	launchErr     error
}

func (l *cancellingLifecycle) Ensure(ctx context.Context, meta Meta) error {
	l.cancelRequest()
	l.launchErr = ctx.Err()
	return l.startTestLifecycle.Ensure(ctx, meta)
}

func TestCreateKeepsLaunchingWhenTheRequestIsCancelled(t *testing.T) {
	repo := &startTestRepository{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lifecycle := &cancellingLifecycle{
		startTestLifecycle: &startTestLifecycle{state: ContainerStateMissing},
		cancelRequest:      cancel,
	}
	service := New(repo, ContainerDependencies{Lifecycle: lifecycle, Templates: newTemplateCatalogStub("blank")}, newMemorySecrets(), nil)

	meta, err := service.Create(ctx, CreateInput{Name: "HTML Test", Template: "blank"}, "owner@example.com")
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if lifecycle.launchErr != nil {
		t.Fatalf("launch context was cancelled with the request: %v", lifecycle.launchErr)
	}
	if meta.Status != StatusRunning {
		t.Fatalf("status = %q, want %q", meta.Status, StatusRunning)
	}
}
