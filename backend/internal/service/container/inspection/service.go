// Package inspection assembles best-effort diagnostic snapshots of project
// containers.
package inspection

import (
	"context"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// StateReader reports the lifecycle state that determines which probes are
// safe and meaningful for a container.
type StateReader interface {
	State(ctx context.Context, containerName string) (serviceproject.ContainerState, error)
}

type ConfigurationProbe interface {
	InspectConfiguration(ctx context.Context, containerName string, out *serviceproject.ContainerInspect)
}

type RuntimeProbe interface {
	InspectRuntime(ctx context.Context, containerName string, out *serviceproject.ContainerInspect)
}

type GuestProbe interface {
	InspectGuest(ctx context.Context, containerName string) (*serviceproject.OSInfo, []serviceproject.DiskUsage)
}

type AgentProbe interface {
	InspectAgents(ctx context.Context, containerName string) []serviceproject.AgentContainerStatus
}

type CredentialProbe interface {
	InspectCredentials(
		ctx context.Context,
		containerName string,
		state serviceproject.ContainerState,
	) []serviceproject.AuthBundleStatus
}

// Dependencies groups the independently replaceable state source and probes.
type Dependencies struct {
	States        StateReader
	Configuration ConfigurationProbe
	Runtime       RuntimeProbe
	Guest         GuestProbe
	Agents        AgentProbe
	Credentials   CredentialProbe
}

// Service owns inspection sequencing and lifecycle-state gating. Individual
// probes remain best-effort and populate only the fields they can observe.
type Service struct {
	states        StateReader
	configuration ConfigurationProbe
	runtime       RuntimeProbe
	guest         GuestProbe
	agents        AgentProbe
	credentials   CredentialProbe
}

func NewService(deps Dependencies) *Service {
	return &Service{
		states:        deps.States,
		configuration: deps.Configuration,
		runtime:       deps.Runtime,
		guest:         deps.Guest,
		agents:        deps.Agents,
		credentials:   deps.Credentials,
	}
}

// Inspect gathers a diagnostic snapshot in stable dependency order:
// configuration for every existing container, live guest probes only while
// running, and credential timestamps for every existing state.
func (s *Service) Inspect(ctx context.Context, containerName string) (serviceproject.ContainerInspect, error) {
	out := serviceproject.ContainerInspect{Name: containerName}

	state, err := s.states.State(ctx, containerName)
	if err != nil {
		return out, err
	}
	out.State = state
	if state == serviceproject.ContainerStateMissing {
		return out, nil
	}

	s.configuration.InspectConfiguration(ctx, containerName, &out)

	if state == serviceproject.ContainerStateRunning {
		s.runtime.InspectRuntime(ctx, containerName, &out)
		out.OS, out.Disks = s.guest.InspectGuest(ctx, containerName)
		out.SetAgentStatuses(s.agents.InspectAgents(ctx, containerName))
	}

	out.AuthBundles = s.credentials.InspectCredentials(ctx, containerName, state)
	return out, nil
}

// Vitals gathers only the cheap half of an inspection: lifecycle state,
// configured limits, and the live instance counters. It runs no command inside
// the container, which is what makes it safe for the health monitor to call on
// a timer for every running project — the agent-version and credential probes
// of a full Inspect shell into the guest a dozen times.
func (s *Service) Vitals(
	ctx context.Context,
	containerName string,
) (serviceproject.ContainerInspect, error) {
	out := serviceproject.ContainerInspect{Name: containerName}

	state, err := s.states.State(ctx, containerName)
	if err != nil {
		return out, err
	}
	out.State = state
	if state == serviceproject.ContainerStateMissing {
		return out, nil
	}

	s.configuration.InspectConfiguration(ctx, containerName, &out)
	if state == serviceproject.ContainerStateRunning {
		s.runtime.InspectRuntime(ctx, containerName, &out)
	}
	return out, nil
}
