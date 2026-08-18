// Package templates owns the project stack presets ("templates") a project
// container is created from.
//
// A template is deliberately a LAYER on top of the single shared base image
// rather than its own full image: the target deployment is one small server,
// so baking N complete images is expensive in build time and disk. A template
// therefore is:
//
//  1. the shared base image (futrx-remote-dev-base), plus
//  2. a post-provision script executed once inside the new container, plus
//  3. optional seed files written into the durable /workspace mount, plus
//  4. an optional agent-instructions snippet seeded as /workspace/AGENTS.md.
//
// A template MAY additionally declare a dedicated pre-built image alias
// (futrx-remote-<name>-base). When that alias exists in the container runtime
// the lifecycle launches from it directly (fast path); otherwise it falls back
// to base image + provision script (slow path). Both paths converge on the
// same rootfs contents, and both leave the same marker file behind.
package templates

import "strings"

// DefaultName is the template used by projects created before templates
// existed and by projects that do not request one. It installs nothing, which
// is exactly the pre-template behaviour.
const DefaultName = "blank"

// Container paths owned by template provisioning. They live in the disposable
// rootfs on purpose: replacing a container (workspace upgrade) must re-run the
// provisioning, because everything the script installed outside /workspace is
// gone with the old rootfs.
const (
	// LogPath collects every provisioning run's output inside the container.
	LogPath = "/var/log/remote-template.log"
	// MarkerPath exists only after a provisioning run completed successfully.
	// Its presence makes every later run a no-op.
	MarkerPath = "/var/lib/remote-template.done"
	// FailurePath exists while a provisioning run is in flight and is removed
	// on success, so a backend restart can still tell "failed" from "pending".
	FailurePath = "/var/lib/remote-template.failed"
	// InstructionsPath is where a template's agent-instructions snippet is
	// seeded. Never overwritten when the file already exists.
	InstructionsPath = "/workspace/AGENTS.md"
)

// EnvPrefix namespaces the environment variables a template's inputs are
// exposed under inside the provisioning program. Prefixing keeps the template
// contract obvious in a script and cannot collide with the project secrets
// LXD already injects as plain environment.* config.
const EnvPrefix = "TPL_"

// PreviewURLEnv carries the project's public preview origin into the
// provisioning program. It is supplied by the runner rather than declared as
// an input: the slug and the public hostname are platform facts, not values an
// operator types into the new-project dialog.
const PreviewURLEnv = EnvPrefix + "PREVIEW_URL"

// imageAliasPrefix and imageAliasSuffix bracket the dedicated pre-built image
// alias of a template that declares one.
const (
	imageAliasPrefix = "futrx-remote-"
	imageAliasSuffix = "-base"
)

// Status is the observable state of one container's template provisioning.
type Status string

const (
	// StatusNone means the template has nothing to provision (e.g. "blank").
	StatusNone Status = "none"
	// StatusPending means provisioning is required but has not started (or the
	// backend restarted before it could finish).
	StatusPending Status = "pending"
	// StatusRunning means the provision script is executing.
	StatusRunning Status = "running"
	// StatusDone means the marker file is present.
	StatusDone Status = "done"
	// StatusFailed means the last provisioning attempt exited non-zero.
	StatusFailed Status = "failed"
)

// SeedFile declares one file copied into the container when it is absent.
// Seeding never overwrites: a workspace is durable and may already hold the
// user's own file at that path.
type SeedFile struct {
	// Source is a file name relative to the template's directory.
	Source string `json:"source"`
	// Target is an absolute path inside the container. It must live under
	// /workspace so the seed survives container replacement.
	Target string `json:"target"`
	// Mode is the octal file mode passed to the runtime. Defaults to 644.
	Mode string `json:"mode,omitempty"`
}

// InputType is the declared form control (and coercion rule) of one template
// input. The set is deliberately small: every type must be renderable by the
// new-project dialog and coercible from JSON without ambiguity.
type InputType string

const (
	InputText     InputType = "text"
	InputEmail    InputType = "email"
	InputPassword InputType = "password"
	InputSelect   InputType = "select"
	InputCheckbox InputType = "checkbox"
)

// InputOption is one choice of a select input.
type InputOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// DefaultSource names a server-side value used as an input's default when the
// template cannot know it (the project name, the creating user's email). The
// dialog prefills the same values client-side; the server applies them again
// so an API caller that omits the field lands on the same result.
type DefaultSource string

const (
	// DefaultFromProjectName prefills the name of the project being created.
	DefaultFromProjectName DefaultSource = "projectName"
	// DefaultFromUserEmail prefills the creating user's email address.
	DefaultFromUserEmail DefaultSource = "userEmail"
)

// Input is one operator-supplied value a template collects in the new-project
// dialog and receives as an environment variable during provisioning.
//
// The env var name is derived from Key, never declared: EnvName turns
// "adminPassword" into TPL_ADMIN_PASSWORD. One key therefore has exactly one
// spelling on the wire, in metadata, and in the script.
type Input struct {
	Key      string        `json:"key"`
	Label    string        `json:"label"`
	Type     InputType     `json:"type"`
	Required bool          `json:"required,omitempty"`
	Default  string        `json:"default,omitempty"`
	Options  []InputOption `json:"options,omitempty"`
	Help     string        `json:"help,omitempty"`
	// DefaultFrom names a server-supplied default (see DefaultSource). It is
	// consulted only when Default is empty and the caller sent nothing.
	DefaultFrom DefaultSource `json:"defaultFrom,omitempty"`
	// Secret marks a value that must never be persisted in project metadata.
	// It is written to the project secrets store under SecretName instead, so
	// it appears in the Secrets tab and nowhere else.
	Secret bool `json:"secret,omitempty"`
	// SecretName is the project-secret key a secret input is stored under.
	// Required when Secret is set.
	SecretName string `json:"secretName,omitempty"`
	// Generate makes the server mint a strong random value when a secret
	// input is left empty, instead of rejecting it or provisioning without one.
	Generate bool `json:"generate,omitempty"`
}

// EnvName is the environment variable this input is passed as.
func (i Input) EnvName() string { return EnvName(i.Key) }

// EnvName converts a camelCase input key to its TPL_ environment variable. A
// capital starts a new word unless it follows another capital, so "siteTitle"
// becomes TPL_SITE_TITLE and "installWooCommerce" becomes
// TPL_INSTALL_WOOCOMMERCE.
func EnvName(key string) string {
	var out strings.Builder
	out.WriteString(EnvPrefix)
	runes := []rune(key)
	for index, r := range runes {
		switch {
		case r >= 'A' && r <= 'Z':
			previous := rune(0)
			if index > 0 {
				previous = runes[index-1]
			}
			lower := previous >= 'a' && previous <= 'z'
			digit := previous >= '0' && previous <= '9'
			if lower || digit {
				out.WriteByte('_')
			}
			out.WriteRune(r)
		case r >= 'a' && r <= 'z':
			out.WriteRune(r - 32)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
	}
	return out.String()
}

// AdminAccess describes the credentialed entry point a template installs, so
// the project page can show "here is your admin URL, user, and password"
// without hard-coding any one stack.
type AdminAccess struct {
	// Label names the destination, e.g. "WordPress admin".
	Label string `json:"label"`
	// Port is the in-container port the admin UI is served on. It selects the
	// <slug>--<port>.dev.<host> preview origin.
	Port int `json:"port"`
	// Path is appended to that origin, e.g. "/wp-admin".
	Path string `json:"path,omitempty"`
	// UserInput is the input key holding the admin user name.
	UserInput string `json:"userInput,omitempty"`
	// PasswordSecret is the project-secret key holding the admin password.
	PasswordSecret string `json:"passwordSecret,omitempty"`
}

// Definition is the on-disk shape of a template's template.json.
type Definition struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Icon is a stable key the frontend maps to a glyph. Unknown keys fall
	// back to a generic icon, so adding a template never breaks the UI.
	Icon string `json:"icon"`
	// ProvisionScript is a file name relative to the template's directory.
	// Empty means the template installs nothing.
	ProvisionScript string `json:"provisionScript,omitempty"`
	// AgentInstructions is a file name relative to the template's directory
	// whose contents are seeded as /workspace/AGENTS.md.
	AgentInstructions string `json:"agentInstructions,omitempty"`
	// SeedFiles are additional files written into /workspace when absent.
	SeedFiles []SeedFile `json:"seedFiles,omitempty"`
	// DefaultPorts are the TCP ports this stack is expected to listen on.
	// Purely informational: previews are discovered, never declared.
	DefaultPorts []int `json:"defaultPorts,omitempty"`
	// PrebuiltImage declares that this template has a dedicated image alias
	// that `build-template-image` can publish.
	PrebuiltImage bool `json:"prebuiltImage,omitempty"`
	// WorkspaceMarker is an absolute path under /workspace whose absence
	// forces a provisioning run even when the rootfs marker says a previous
	// run succeeded.
	//
	// It exists because the rootfs marker cannot speak for /workspace: a
	// container launched from a pre-built template image carries the marker,
	// but its durable workspace is mounted over whatever the image baked, so
	// without this the stack would report "done" over an empty site.
	WorkspaceMarker string `json:"workspaceMarker,omitempty"`
	// Inputs are the operator-supplied values collected in the new-project
	// dialog and passed to the provision script as TPL_* environment
	// variables. Empty for templates that collect nothing.
	Inputs []Input `json:"inputs,omitempty"`
	// AdminAccess describes the credentialed entry point the template
	// installs, surfaced on the project page once provisioning is done.
	AdminAccess *AdminAccess `json:"adminAccess,omitempty"`
}

// Template is a validated definition with its referenced files resolved.
type Template struct {
	Definition
	// Script is the provision program's payload (without the shared harness).
	Script []byte
	// Instructions is the agent-instructions snippet, if any.
	Instructions []byte
	// Seeds carries the resolved contents of Definition.SeedFiles.
	Seeds []Seed
}

// Seed is a SeedFile with its content resolved from the embedded files.
type Seed struct {
	Target  string
	Mode    string
	Content []byte
}

// ImageAlias returns the dedicated pre-built image alias for a template, or an
// empty string when the template does not declare one.
func (t Template) ImageAlias() string {
	if !t.PrebuiltImage {
		return ""
	}
	return ImageAlias(t.Name)
}

// ImageAlias returns the conventional dedicated image alias for a template
// name. Exported so the build CLI can name its publish target without loading
// the catalog twice.
func ImageAlias(name string) string {
	return imageAliasPrefix + name + imageAliasSuffix
}

// AdminPort is the container port a template's preview URL should point at:
// the admin entry point's port when one is declared, otherwise the first
// declared default port. Zero means the template has no obvious front door,
// and no preview URL is derived.
func (t Template) AdminPort() int {
	if t.AdminAccess != nil && t.AdminAccess.Port > 0 {
		return t.AdminAccess.Port
	}
	if len(t.DefaultPorts) > 0 {
		return t.DefaultPorts[0]
	}
	return 0
}

// Provisions reports whether this template has any post-launch work at all.
// A template without work never issues a single runtime call.
func (t Template) Provisions() bool {
	return len(t.Script) > 0 || len(t.Seeds) > 0 || len(t.Instructions) > 0
}

// allSeeds returns the file seeds plus the implicit agent-instructions seed,
// in the order they are written.
func (t Template) allSeeds() []Seed {
	seeds := make([]Seed, 0, len(t.Seeds)+1)
	seeds = append(seeds, t.Seeds...)
	if len(t.Instructions) > 0 {
		seeds = append(seeds, Seed{
			Target:  InstructionsPath,
			Mode:    "644",
			Content: t.Instructions,
		})
	}
	return seeds
}

// Descriptor is the catalog entry published over HTTP. It carries the
// presentation metadata plus whether the fast (pre-built image) path is
// currently available on this host.
type Descriptor struct {
	Name                   string `json:"name"`
	Title                  string `json:"title"`
	Description            string `json:"description"`
	Icon                   string `json:"icon"`
	DefaultPorts           []int  `json:"defaultPorts,omitempty"`
	Default                bool   `json:"default"`
	Provisions             bool   `json:"provisions"`
	PrebuiltImage          string `json:"prebuiltImage,omitempty"`
	PrebuiltImageAvailable bool   `json:"prebuiltImageAvailable"`
	// Inputs is the template's input declaration, rendered as a form under
	// the picker card. Omitted for templates that collect nothing.
	Inputs []Input `json:"inputs,omitempty"`
}

// State is one container's template provisioning state.
type State struct {
	Template   string
	Title      string
	Status     Status
	Error      string
	LogPath    string
	StartedAt  int64
	FinishedAt int64
}

// NormalizeName lowercases and trims a requested template name.
func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
