package templates

// Template inputs are the one place an operator's typing reaches a provision
// script. The rules here are therefore deliberately narrow:
//
//   - only declared keys are accepted (an unknown key is a client bug, and
//     silently dropping it would hide it);
//   - every value is coerced to the declared type, so a script never has to
//     parse "maybe true, maybe 1, maybe on";
//   - secret values never enter the returned Values map, so nothing upstream
//     can persist them into project metadata by accident;
//   - the resulting environment is a map of argv-safe key/value pairs, passed
//     to `lxc exec --env`, never interpolated into a shell string.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidInput is the sentinel every input rejection wraps, so the project
// service can map the whole class to one HTTP status without string matching.
var ErrInvalidInput = errors.New("invalid template input")

// generatedPasswordLength is the length of a server-minted secret. 24
// characters out of the 70-glyph alphabet below is ~147 bits, far past
// anything a login form needs, and still copy-pasteable.
const generatedPasswordLength = 24

// passwordAlphabet excludes quotes, backslashes and shell metacharacters. The
// value is never interpolated into a shell string, but an admin password is
// also typed, pasted into config files and put in URLs by humans, and a
// generated one has no reason to be hostile to any of that.
const passwordAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789-_.@#%+="

// inputKeyPattern keeps keys camelCase-ish so EnvName produces a valid
// environment variable name for every declared input.
var inputKeyPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,39}$`)

// Values are the resolved, non-secret inputs of one project. They are what
// gets persisted in project metadata and re-read on every later convergence.
type Values map[string]string

// Resolution is the outcome of validating one create request's raw inputs.
type Resolution struct {
	// Values are the non-secret inputs, safe to persist in project metadata.
	Values Values
	// Secrets maps a project-secret key (e.g. WP_ADMIN_PASSWORD) to its
	// value. The caller stores these in the project secrets repository.
	Secrets map[string]string
}

// Context carries the server-side defaults a template can ask for.
type Context struct {
	ProjectName string
	UserEmail   string
}

// ValidateInputDeclaration checks one template's inputs at catalog load time.
// Template data is compiled into the binary, so a failure here is a build
// defect, caught by the catalog test rather than by a user.
func ValidateInputDeclaration(inputs []Input, admin *AdminAccess) error {
	seenKey := make(map[string]bool, len(inputs))
	seenEnv := make(map[string]bool, len(inputs))
	seenSecret := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		if !inputKeyPattern.MatchString(input.Key) {
			return fmt.Errorf("input key %q must match %s", input.Key, inputKeyPattern)
		}
		if seenKey[input.Key] {
			return fmt.Errorf("input %q is declared twice", input.Key)
		}
		seenKey[input.Key] = true
		env := EnvName(input.Key)
		if seenEnv[env] {
			return fmt.Errorf("input %q collides with another input on %s", input.Key, env)
		}
		seenEnv[env] = true
		if strings.TrimSpace(input.Label) == "" {
			return fmt.Errorf("input %q: label is required", input.Key)
		}
		switch input.Type {
		case InputText, InputEmail, InputPassword, InputCheckbox:
			if len(input.Options) > 0 {
				return fmt.Errorf("input %q: options are only valid for a select", input.Key)
			}
		case InputSelect:
			if len(input.Options) == 0 {
				return fmt.Errorf("input %q: a select needs at least one option", input.Key)
			}
			if input.Default != "" && !hasOption(input.Options, input.Default) {
				return fmt.Errorf("input %q: default %q is not one of its options", input.Key, input.Default)
			}
		default:
			return fmt.Errorf("input %q: unknown type %q", input.Key, input.Type)
		}
		if input.Type == InputCheckbox {
			if input.Default != "" && input.Default != "true" && input.Default != "false" {
				return fmt.Errorf("input %q: checkbox default must be \"true\" or \"false\"", input.Key)
			}
			if input.Required {
				return fmt.Errorf("input %q: a checkbox cannot be required", input.Key)
			}
		}
		if input.Secret {
			if input.Type != InputPassword {
				return fmt.Errorf("input %q: only a password input can be a secret", input.Key)
			}
			if !validSecretName(input.SecretName) {
				return fmt.Errorf("input %q: secretName %q is not a valid env var name", input.Key, input.SecretName)
			}
			if seenSecret[input.SecretName] {
				return fmt.Errorf("input %q: secretName %q is declared twice", input.Key, input.SecretName)
			}
			seenSecret[input.SecretName] = true
			if input.Default != "" {
				return fmt.Errorf("input %q: a secret must not ship a default", input.Key)
			}
		} else if input.SecretName != "" || input.Generate {
			return fmt.Errorf("input %q: secretName/generate need secret: true", input.Key)
		}
		switch input.DefaultFrom {
		case "", DefaultFromProjectName, DefaultFromUserEmail:
		default:
			return fmt.Errorf("input %q: unknown defaultFrom %q", input.Key, input.DefaultFrom)
		}
	}
	if admin == nil {
		return nil
	}
	if strings.TrimSpace(admin.Label) == "" {
		return errors.New("adminAccess: label is required")
	}
	if admin.Port < 1 || admin.Port > 65535 {
		return fmt.Errorf("adminAccess: port %d is out of range", admin.Port)
	}
	if admin.Path != "" && !strings.HasPrefix(admin.Path, "/") {
		return fmt.Errorf("adminAccess: path %q must start with /", admin.Path)
	}
	if admin.UserInput != "" && !seenKey[admin.UserInput] {
		return fmt.Errorf("adminAccess: userInput %q is not a declared input", admin.UserInput)
	}
	if admin.PasswordSecret != "" && !seenSecret[admin.PasswordSecret] {
		return fmt.Errorf("adminAccess: passwordSecret %q is not produced by any input", admin.PasswordSecret)
	}
	return nil
}

// ResolveInputs validates a create request's raw inputs against a template's
// declaration and splits the result into persistable values and secrets.
//
// raw comes straight off the JSON body, so values arrive as string, bool or
// number. Anything else (object, array, null) is rejected rather than
// stringified: a script must never receive "map[]".
func (t Template) ResolveInputs(raw map[string]any, context Context) (Resolution, error) {
	resolution := Resolution{Values: Values{}, Secrets: map[string]string{}}
	if len(t.Inputs) == 0 {
		if len(raw) > 0 {
			return Resolution{}, fmt.Errorf(
				"%w: template %q takes no inputs", ErrInvalidInput, t.Name,
			)
		}
		return resolution, nil
	}
	declared := make(map[string]Input, len(t.Inputs))
	for _, input := range t.Inputs {
		declared[input.Key] = input
	}
	for key := range raw {
		if _, ok := declared[key]; !ok {
			return Resolution{}, fmt.Errorf(
				"%w: %q is not an input of template %q", ErrInvalidInput, key, t.Name,
			)
		}
	}
	for _, input := range t.Inputs {
		value, err := resolveOne(input, raw[input.Key], raw, context)
		if err != nil {
			return Resolution{}, err
		}
		if input.Secret {
			if value != "" {
				resolution.Secrets[input.SecretName] = value
			}
			continue
		}
		resolution.Values[input.Key] = value
	}
	return resolution, nil
}

func resolveOne(input Input, supplied any, raw map[string]any, context Context) (string, error) {
	_, present := raw[input.Key]
	value := ""
	if present {
		coerced, err := coerce(input, supplied)
		if err != nil {
			return "", err
		}
		value = coerced
	}
	if value == "" {
		value = defaultFor(input, context)
	}
	if value == "" {
		if input.Secret && input.Generate {
			generated, err := GeneratePassword()
			if err != nil {
				return "", err
			}
			return generated, nil
		}
		if input.Required {
			return "", fmt.Errorf("%w: %s is required", ErrInvalidInput, input.Label)
		}
		if input.Type == InputCheckbox {
			return "false", nil
		}
		return "", nil
	}
	return value, validate(input, value)
}

// coerce turns one JSON value into the string form the script receives.
func coerce(input Input, supplied any) (string, error) {
	switch value := supplied.(type) {
	case nil:
		return "", nil
	case string:
		if input.Type == InputCheckbox {
			return coerceCheckbox(input, value)
		}
		if input.Type == InputPassword {
			// A password is taken verbatim: leading or trailing spaces are
			// legitimate characters in one, unlike in a title or a login.
			return value, nil
		}
		return strings.TrimSpace(value), nil
	case bool:
		if input.Type != InputCheckbox {
			return "", fmt.Errorf("%w: %s does not take a boolean", ErrInvalidInput, input.Label)
		}
		return strconv.FormatBool(value), nil
	case float64:
		if input.Type == InputCheckbox {
			return coerceCheckbox(input, strconv.FormatFloat(value, 'f', -1, 64))
		}
		return strconv.FormatFloat(value, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("%w: %s must be a string, boolean or number", ErrInvalidInput, input.Label)
	}
}

func coerceCheckbox(input Input, value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return "true", nil
	case "", "0", "false", "no", "off":
		return "false", nil
	default:
		return "", fmt.Errorf("%w: %s must be true or false", ErrInvalidInput, input.Label)
	}
}

func defaultFor(input Input, context Context) string {
	if input.Default != "" {
		return input.Default
	}
	switch input.DefaultFrom {
	case DefaultFromProjectName:
		return strings.TrimSpace(context.ProjectName)
	case DefaultFromUserEmail:
		return strings.TrimSpace(context.UserEmail)
	default:
		return ""
	}
}

func validate(input Input, value string) error {
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%w: %s must be a single line", ErrInvalidInput, input.Label)
	}
	if len(value) > 512 {
		return fmt.Errorf("%w: %s is too long (max 512 characters)", ErrInvalidInput, input.Label)
	}
	switch input.Type {
	case InputEmail:
		if _, err := mail.ParseAddress(value); err != nil {
			return fmt.Errorf("%w: %s must be an email address", ErrInvalidInput, input.Label)
		}
	case InputSelect:
		if !hasOption(input.Options, value) {
			return fmt.Errorf("%w: %s must be one of the offered options", ErrInvalidInput, input.Label)
		}
	case InputCheckbox:
		if value != "true" && value != "false" {
			return fmt.Errorf("%w: %s must be true or false", ErrInvalidInput, input.Label)
		}
	}
	return nil
}

func hasOption(options []InputOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

// validSecretName mirrors the project secrets store's key rule, so a template
// can never declare a secret the store would refuse.
func validSecretName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for index, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r == '_':
		case index > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// Environment renders the TPL_* variables a provisioning run receives.
//
// Every declared input is present, so a script can rely on `set -u` and read
// "$TPL_LANGUAGE" without a guard. Values come from the stored project
// metadata; secrets are supplied separately by the caller (they live in the
// secrets store, never in metadata). previewURL is optional.
func (t Template) Environment(values Values, secrets map[string]string, previewURL string) map[string]string {
	env := make(map[string]string, len(t.Inputs)+1)
	for _, input := range t.Inputs {
		value := ""
		switch {
		case input.Secret:
			value = secrets[input.SecretName]
		default:
			if stored, ok := values[input.Key]; ok {
				value = stored
			} else {
				// Metadata written before this input existed: fall back to
				// the declared default so old projects keep provisioning.
				value = input.Default
				if value == "" && input.Type == InputCheckbox {
					value = "false"
				}
			}
		}
		env[input.EnvName()] = value
	}
	if previewURL != "" {
		env[PreviewURLEnv] = previewURL
	}
	return env
}

// SecretNames lists the project-secret keys this template's inputs produce.
func (t Template) SecretNames() []string {
	var names []string
	for _, input := range t.Inputs {
		if input.Secret && input.SecretName != "" {
			names = append(names, input.SecretName)
		}
	}
	return names
}

// GeneratePassword mints a strong random admin password. It fails loudly
// rather than falling back to a weaker source: a silently predictable admin
// password is worse than a failed project creation.
func GeneratePassword() (string, error) {
	alphabet := []rune(passwordAlphabet)
	limit := big.NewInt(int64(len(alphabet)))
	out := make([]rune, generatedPasswordLength)
	for index := range out {
		pick, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		out[index] = alphabet[pick.Int64()]
	}
	return string(out), nil
}
