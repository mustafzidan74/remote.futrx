package agent

// Endpoint is one third-party, vendor-published compatibility endpoint a
// single run is pointed at instead of the vendor's own default.
//
// It arrives already rendered: the service layer resolved the profile, read
// its key out of the Secrets vault, and produced exactly the environment and
// the extra arguments this CLI needs. A provider adapter applies both
// verbatim and adds nothing — which is what keeps the knowledge of *which*
// vendor documents *which* variable in one place instead of spread across
// four command builders.
//
// Two properties are the whole safety story and both are enforced by the
// adapters that read this type:
//
//   - it is per run. Nothing here is written to a file inside the container,
//     so the next chat in the same project is unaffected;
//   - it displaces the operator's own credentials rather than joining them. A
//     run carrying an Endpoint does not seed the platform's Anthropic or
//     ChatGPT credentials into the container and does not sync them back out
//     afterwards.
type Endpoint struct {
	// ID and Label identify the profile for the audit log and the chat
	// header's badge. Neither is secret.
	ID    string
	Label string
	// CLI is which command line runs. It is authoritative: a chat whose
	// stored provider disagrees with its endpoint runs the endpoint's CLI,
	// because the endpoint is what the models were listed under.
	CLI ProviderID
	// Model is the model id this run asks the endpoint for, already reconciled
	// against the profile's own list. Empty leaves the CLI on whatever the
	// endpoint defaults to.
	Model string
	// Env is published to the CLI process. It always includes the blanking
	// entries that keep a stray host credential from reaching a third party.
	Env map[string]string
	// Args are appended to the CLI's own argument list. Empty for the claude
	// CLI, whose compatibility mode is environment-only.
	Args []string
}

// EndpointEnvironment returns deterministic KEY=value entries for one
// endpoint, or nil when there is none. Invalid environment names are ignored,
// exactly as they are for backend-issued runtime capabilities.
func EndpointEnvironment(endpoint *Endpoint) []string {
	if endpoint == nil {
		return nil
	}
	return RuntimeEnvironment(endpoint.Env)
}

// WithEndpointEnvironment overlays an endpoint's environment on a base
// environment without leaving duplicate keys whose lookup order varies by
// executable. It is the host-side counterpart of EndpointEnvironment, used by
// the loose-chat path where the CLI runs as a child process rather than
// through `lxc exec`.
func WithEndpointEnvironment(base []string, endpoint *Endpoint) []string {
	if endpoint == nil {
		return append([]string(nil), base...)
	}
	return WithRuntimeEnvironment(base, endpoint.Env)
}

// EndpointArgs returns the extra CLI arguments this endpoint contributes.
func EndpointArgs(endpoint *Endpoint) []string {
	if endpoint == nil {
		return nil
	}
	return append([]string(nil), endpoint.Args...)
}

// EndpointIssued reports whether an endpoint supplies this environment name.
//
// The adapters consult it before forwarding a project secret of the same
// name: a project that happens to define ANTHROPIC_BASE_URL must not be able
// to redirect a run the platform pointed somewhere specific, and one that
// defines the key variable must not be able to substitute its own credential.
func EndpointIssued(endpoint *Endpoint, key string) bool {
	if endpoint == nil || len(endpoint.Env) == 0 {
		return false
	}
	_, issued := endpoint.Env[key]
	return issued
}
