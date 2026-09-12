package antigravity

import (
	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
	agentauth "github.com/futrx-com/remote.futrx.com/internal/service/agent/auth"
	agentmodule "github.com/futrx-com/remote.futrx.com/internal/service/agent/module"
)

// NewFactory returns Antigravity's complete module definition. Authentication is
// provider-owned and external, while the shared profile supplies deterministic
// host and project provisioning policy.
func NewFactory() (agentmodule.Factory, error) {
	profile := Profile()
	return agentmodule.NewFactory(agentmodule.Descriptor{
		ID:               agent.ProviderAntigravity,
		Label:            "Antigravity",
		ExecutionScopes:  []agentmodule.ExecutionScope{agentmodule.ScopeHost, agentmodule.ScopeProject},
		Auth:             agentmodule.AuthExternal,
		AuthInstructions: "Open any project terminal, run `agy`, and complete its sign-in flow. You only need to do this once: the sign-in is copied to the platform and every other project inherits it.",
		Features: agentmodule.Features{
			Sessions:       agentmodule.SessionSupport{Resume: true},
			Skills:         agentmodule.SkillsInstructions,
			ScheduledTools: true,
		},
	}, &profile, func(deps agentmodule.Dependencies, validatedProfile *provisioning.Profile) (agentmodule.Components, error) {
		// agy's bare-launch sign-in never exits its TUI, so the platform cannot
		// drive it. Sign-in is one `agy` run in a chat terminal — one, not one
		// per project, because the credential is pulled to the host afterwards
		// and seeded into every container. The binding reports whether that
		// has happened.
		binding := agentauth.NewExternalBinding(agent.ProviderAntigravity).WithExternalSignIn(Authenticated)
		return agentmodule.Components{
			Provider: newProvider(
				deps.ProjectPreparer,
				deps.CredentialCollector,
				*validatedProfile,
			),
			Auth: &binding,
		}, nil
	})
}

var (
	_ agent.Provider             = (*Provider)(nil)
	_ agentmodule.FactoryBuilder = NewFactory
)
