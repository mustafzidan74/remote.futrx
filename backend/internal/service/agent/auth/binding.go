package auth

import (
	"context"
	"errors"
	"sync"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
)

type Flow string

const (
	FlowCode   Flow = "code"
	FlowDevice Flow = "device"
)

var ErrUnsupportedFlow = errors.New("operation is not supported by this agent auth flow")

// Binding is the transport-neutral view of one configured agent auth caller.
// It lets inbound adapters expose HTTP or WebSocket protocols without importing
// a concrete agent package or knowing its CLI policy.
type Binding struct {
	id            agent.ProviderID
	flow          Flow
	status        func() any
	subscribe     func() Subscription
	authenticated func() bool

	startCode        func(context.Context) (CodeStartResult, error)
	submitCode       func(context.Context, string) error
	cancelCode       func(context.Context) error
	isCodeInputError func(error) bool
	startDevice      func(context.Context) (DeviceState, error)
}

func NewCodeBinding(id agent.ProviderID, service *CodeService) Binding {
	binding := Binding{id: id, flow: FlowCode}
	if service == nil {
		return binding
	}
	binding.status = func() any { return service.Status() }
	binding.subscribe = statusSubscription(service.Subscribe)
	binding.authenticated = service.Authenticated
	binding.startCode = service.Start
	binding.submitCode = service.SubmitCode
	binding.cancelCode = service.Cancel
	binding.isCodeInputError = service.IsInputError
	return binding
}

// ExternalStatus is what a binding reports for an agent whose sign-in this
// platform does not drive. There is no login to start and nothing to poll, so
// it carries the one fact a settings page needs — is there a usable credential
// — plus the sentence telling an operator how to create one.
type ExternalStatus struct {
	Authenticated bool `json:"authenticated"`
	// External marks the flow as the agent's own, so a UI renders instructions
	// rather than a Connect button that could not work.
	External bool   `json:"external"`
	Hint     string `json:"hint,omitempty"`
}

// NewExternalBinding describes an agent that signs itself in.
//
// Antigravity is the case it exists for: `agy` signs in through a terminal UI
// that never exits, so it cannot run under the code or device services the
// other agents share. What the platform *can* do is answer whether a
// credential has been captured, which is the difference between a settings
// page that says "not connected" forever and one that tells the truth.
//
// authenticated is consulted per call rather than cached: the credential
// appears on disk when a run in some container succeeds, and nothing notifies
// this package when it does.
func NewExternalBinding(id agent.ProviderID, authenticated func() bool, hint string) Binding {
	binding := Binding{id: id, flow: FlowCode}
	if authenticated == nil {
		authenticated = func() bool { return false }
	}
	binding.authenticated = authenticated
	binding.status = func() any {
		signedIn := authenticated()
		status := ExternalStatus{Authenticated: signedIn, External: true}
		// The hint is the sign-in instruction. Sending it alongside
		// authenticated:true would have the payload contradict itself.
		if !signedIn {
			status.Hint = hint
		}
		return status
	}
	// Deliberately no subscribe: Available() stays false, so the routes that
	// drive a login are never registered for this binding.
	return binding
}

func NewDeviceBinding[S any](id agent.ProviderID, service *DeviceService[S]) Binding {
	binding := Binding{id: id, flow: FlowDevice}
	if service == nil {
		return binding
	}
	binding.status = func() any { return service.Status() }
	binding.subscribe = statusSubscription(service.Subscribe)
	binding.authenticated = service.Authenticated
	binding.startDevice = service.StartDeviceLogin
	return binding
}

func (b Binding) ID() agent.ProviderID { return b.id }

func (b Binding) Flow() Flow { return b.flow }

func (b Binding) Available() bool { return b.status != nil && b.subscribe != nil }

func (b Binding) Authenticated() bool {
	return b.authenticated != nil && b.authenticated()
}

func (b Binding) Status() any {
	if b.status == nil {
		return nil
	}
	return b.status()
}

// Subscribe returns a type-erased view over the caller's original status
// channel. No bridge channel is introduced, so buffering and slow-subscriber
// behavior remain owned by the underlying auth service.
func (b Binding) Subscribe() (Subscription, error) {
	if b.subscribe == nil {
		return Subscription{}, ErrUnsupportedFlow
	}
	return b.subscribe(), nil
}

func (b Binding) StartCode(ctx context.Context) (CodeStartResult, error) {
	if b.startCode == nil {
		return CodeStartResult{}, ErrUnsupportedFlow
	}
	return b.startCode(ctx)
}

func (b Binding) SubmitCode(ctx context.Context, code string) error {
	if b.submitCode == nil {
		return ErrUnsupportedFlow
	}
	return b.submitCode(ctx, code)
}

func (b Binding) CancelCode(ctx context.Context) error {
	if b.cancelCode == nil {
		return ErrUnsupportedFlow
	}
	return b.cancelCode(ctx)
}

func (b Binding) IsCodeInputError(err error) bool {
	return b.isCodeInputError != nil && b.isCodeInputError(err)
}

func (b Binding) StartDevice(ctx context.Context) (DeviceState, error) {
	if b.startDevice == nil {
		return DeviceState{}, ErrUnsupportedFlow
	}
	return b.startDevice(ctx)
}

// Subscription reads concrete provider status values from their original
// channel while exposing a transport-neutral value to callers.
type Subscription struct {
	next  func(context.Context) (any, bool)
	close func()
}

func (s Subscription) Next(ctx context.Context) (any, bool) {
	if s.next == nil {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.next(ctx)
}

func (s Subscription) Close() {
	if s.close != nil {
		s.close()
	}
}

func statusSubscription[S any](subscribe func() (<-chan S, func())) func() Subscription {
	return func() Subscription {
		statuses, unsubscribe := subscribe()
		var closeOnce sync.Once
		return Subscription{
			next: func(ctx context.Context) (any, bool) {
				select {
				case status, ok := <-statuses:
					return status, ok
				case <-ctx.Done():
					return nil, false
				}
			},
			close: func() {
				closeOnce.Do(unsubscribe)
			},
		}
	}
}
