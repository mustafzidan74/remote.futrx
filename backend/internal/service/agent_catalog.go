package service

import (
	"github.com/futrx-com/remote.futrx.com/internal/agent"
	antigravityagent "github.com/futrx-com/remote.futrx.com/internal/agent/antigravity"
	claudeagent "github.com/futrx-com/remote.futrx.com/internal/agent/claude"
	codexagent "github.com/futrx-com/remote.futrx.com/internal/agent/codex"
	kimiagent "github.com/futrx-com/remote.futrx.com/internal/agent/kimi"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
	agentauth "github.com/futrx-com/remote.futrx.com/internal/service/agent/auth"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// agentDefinition is the single composition catalog for one agent. Adding an
// agent means adding one entry with its provider, provisioning profile, and
// shared-auth caller configuration.
type agentDefinition struct {
	profile     func() provisioning.Profile
	provider    func(*serviceproject.Service, provisioning.ContainerDependencies) agent.Provider
	authBinding func() agentauth.Binding
}

func agentDefinitions() []agentDefinition {
	return []agentDefinition{
		{
			profile: claudeagent.Profile,
			provider: func(projects *serviceproject.Service, containerDeps provisioning.ContainerDependencies) agent.Provider {
				return claudeagent.New(projects, containerDeps)
			},
			authBinding: func() agentauth.Binding {
				return agentauth.NewCodeBinding(agent.ProviderClaude, claudeagent.NewAuth())
			},
		},
		{
			profile: codexagent.Profile,
			provider: func(projects *serviceproject.Service, containerDeps provisioning.ContainerDependencies) agent.Provider {
				return codexagent.New(projects, containerDeps)
			},
			authBinding: func() agentauth.Binding {
				return agentauth.NewDeviceBinding(agent.ProviderCodex, codexagent.NewAuth())
			},
		},
		{
			profile: kimiagent.Profile,
			provider: func(projects *serviceproject.Service, containerDeps provisioning.ContainerDependencies) agent.Provider {
				return kimiagent.New(projects, containerDeps)
			},
			authBinding: func() agentauth.Binding {
				return agentauth.NewDeviceBinding(agent.ProviderKimi, kimiagent.NewAuth())
			},
		},
		{
			profile: antigravityagent.Profile,
			provider: func(projects *serviceproject.Service, containerDeps provisioning.ContainerDependencies) agent.Provider {
				return antigravityagent.New(projects, containerDeps)
			},
			// agy's bare-launch sign-in never exits its TUI, so it cannot run
			// under the shared code/device auth services: there is no Connect
			// button this platform could honour. Sign-in is a one-time `agy`
			// run in a chat terminal — one, not one per project, because the
			// credential is pulled to the host afterwards and seeded into every
			// container. The binding reports whether that has happened.
			authBinding: func() agentauth.Binding {
				return agentauth.NewExternalBinding(
					agent.ProviderAntigravity,
					antigravityagent.Authenticated,
					antigravityagent.SignInHint,
				)
			},
		},
	}
}

// AgentProfiles returns the container-facing profiles from the same catalog
// used to register providers and their auth callers.
func AgentProfiles() []provisioning.Profile {
	return profilesFromDefinitions(agentDefinitions())
}

func profilesFromDefinitions(definitions []agentDefinition) []provisioning.Profile {
	profiles := make([]provisioning.Profile, 0, len(definitions))
	for _, definition := range definitions {
		profiles = append(profiles, definition.profile())
	}
	return profiles
}
