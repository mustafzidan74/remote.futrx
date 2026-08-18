package templates

import (
	"errors"
	"strings"
	"testing"
)

// inputTemplate is one template exercising every input type, including the
// two the shipped WordPress template relies on most: a generated secret and a
// server-supplied default.
func inputTemplate() Template {
	return Template{Definition: Definition{
		Name:  "stack",
		Title: "Stack",
		Inputs: []Input{
			{Key: "siteTitle", Label: "Site title", Type: InputText, Required: true, DefaultFrom: DefaultFromProjectName},
			{Key: "adminEmail", Label: "Admin email", Type: InputEmail, Required: true, DefaultFrom: DefaultFromUserEmail},
			{Key: "adminUser", Label: "Admin user", Type: InputText, Required: true, Default: "admin"},
			{
				Key: "adminPassword", Label: "Admin password", Type: InputPassword,
				Secret: true, SecretName: "WP_ADMIN_PASSWORD", Generate: true,
			},
			{
				Key: "language", Label: "Language", Type: InputSelect, Default: "ar",
				Options: []InputOption{{Value: "ar", Label: "العربية"}, {Value: "en_US", Label: "English"}},
			},
			{Key: "installWoocommerce", Label: "Install WooCommerce", Type: InputCheckbox},
		},
	}}
}

func TestEnvName(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{key: "siteTitle", want: "TPL_SITE_TITLE"},
		{key: "adminEmail", want: "TPL_ADMIN_EMAIL"},
		{key: "adminUser", want: "TPL_ADMIN_USER"},
		{key: "adminPassword", want: "TPL_ADMIN_PASSWORD"},
		{key: "language", want: "TPL_LANGUAGE"},
		{key: "installWoocommerce", want: "TPL_INSTALL_WOOCOMMERCE"},
		{key: "installWooCommerce", want: "TPL_INSTALL_WOO_COMMERCE"},
		{key: "demoContent", want: "TPL_DEMO_CONTENT"},
		{key: "php8Version", want: "TPL_PHP8_VERSION"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := EnvName(tt.key); got != tt.want {
				t.Fatalf("EnvName(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestResolveInputsCoercesAndDefaults(t *testing.T) {
	context := Context{ProjectName: "My Shop", UserEmail: "owner@example.com"}

	tests := []struct {
		name        string
		raw         map[string]any
		wantValues  map[string]string
		wantSecret  string
		wantErr     bool
		errContains string
	}{
		{
			name: "an empty request falls back to every declared default",
			raw:  map[string]any{},
			wantValues: map[string]string{
				"siteTitle": "My Shop", "adminEmail": "owner@example.com",
				"adminUser": "admin", "language": "ar", "installWoocommerce": "false",
			},
		},
		{
			name: "supplied values win over defaults",
			raw: map[string]any{
				"siteTitle": "  Metro  ", "adminEmail": "a@b.co",
				"adminUser": "root", "language": "en_US", "installWoocommerce": true,
			},
			wantValues: map[string]string{
				"siteTitle": "Metro", "adminEmail": "a@b.co",
				"adminUser": "root", "language": "en_US", "installWoocommerce": "true",
			},
		},
		{
			name: "a checkbox accepts the string and number spellings of true",
			raw:  map[string]any{"installWoocommerce": "on"},
			wantValues: map[string]string{
				"siteTitle": "My Shop", "adminEmail": "owner@example.com",
				"adminUser": "admin", "language": "ar", "installWoocommerce": "true",
			},
		},
		{
			name:       "an explicit password is kept verbatim and stays out of the values",
			raw:        map[string]any{"adminPassword": " pa ss'\"word "},
			wantSecret: " pa ss'\"word ",
			wantValues: map[string]string{
				"siteTitle": "My Shop", "adminEmail": "owner@example.com",
				"adminUser": "admin", "language": "ar", "installWoocommerce": "false",
			},
		},
		{
			name:        "an unknown key is rejected rather than dropped",
			raw:         map[string]any{"dbPassword": "x"},
			wantErr:     true,
			errContains: `"dbPassword" is not an input`,
		},
		{
			name:        "a select only accepts its own options",
			raw:         map[string]any{"language": "fr_FR"},
			wantErr:     true,
			errContains: "one of the offered options",
		},
		{
			name:        "an email input rejects a non-address",
			raw:         map[string]any{"adminEmail": "not-an-email"},
			wantErr:     true,
			errContains: "must be an email address",
		},
		{
			name: "blanking a required input falls back to its default",
			raw:  map[string]any{"adminUser": "   "},
			wantValues: map[string]string{
				"siteTitle": "My Shop", "adminEmail": "owner@example.com",
				"adminUser": "admin", "language": "ar", "installWoocommerce": "false",
			},
		},
		{
			name:        "a structured value is refused instead of stringified",
			raw:         map[string]any{"siteTitle": []any{"a"}},
			wantErr:     true,
			errContains: "must be a string, boolean or number",
		},
		{
			name:        "a newline cannot be smuggled into an env var",
			raw:         map[string]any{"siteTitle": "Shop\nexport EVIL=1"},
			wantErr:     true,
			errContains: "must be a single line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolution, err := inputTemplate().ResolveInputs(tt.raw, context)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveInputs() = %+v, want an error", resolution)
				}
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("error %v does not wrap ErrInvalidInput", err)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %q, want it to mention %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveInputs() = %v", err)
			}
			for key, want := range tt.wantValues {
				if resolution.Values[key] != want {
					t.Fatalf("Values[%q] = %q, want %q", key, resolution.Values[key], want)
				}
			}
			if _, leaked := resolution.Values["adminPassword"]; leaked {
				t.Fatal("a secret input leaked into the persisted values")
			}
			password := resolution.Secrets["WP_ADMIN_PASSWORD"]
			if tt.wantSecret != "" && password != tt.wantSecret {
				t.Fatalf("secret = %q, want %q", password, tt.wantSecret)
			}
			if tt.wantSecret == "" && len(password) != generatedPasswordLength {
				t.Fatalf("generated secret = %q, want %d characters", password, generatedPasswordLength)
			}
		})
	}
}

func TestResolveInputsRequiresAValueThatHasNoDefault(t *testing.T) {
	// The declaration is what makes an input required, and a required input
	// with nothing to fall back on is the only case that can actually fail.
	template := Template{Definition: Definition{
		Name:   "stack",
		Inputs: []Input{{Key: "domain", Label: "Domain", Type: InputText, Required: true}},
	}}

	if _, err := template.ResolveInputs(nil, Context{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ResolveInputs() error = %v, want ErrInvalidInput", err)
	}
	resolution, err := template.ResolveInputs(map[string]any{"domain": "example.com"}, Context{})
	if err != nil {
		t.Fatalf("ResolveInputs() = %v", err)
	}
	if resolution.Values["domain"] != "example.com" {
		t.Fatalf("Values = %v", resolution.Values)
	}
}

func TestResolveInputsRejectsAnythingForATemplateWithoutInputs(t *testing.T) {
	blank := Template{Definition: Definition{Name: "blank"}}

	if _, err := blank.ResolveInputs(map[string]any{"siteTitle": "x"}, Context{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ResolveInputs() error = %v, want ErrInvalidInput", err)
	}
	if _, err := blank.ResolveInputs(nil, Context{}); err != nil {
		t.Fatalf("ResolveInputs(nil) = %v, want no error", err)
	}
}

func TestEnvironmentMapsEveryDeclaredInput(t *testing.T) {
	template := inputTemplate()
	values := Values{
		"siteTitle": "My Shop", "adminEmail": "owner@example.com",
		"adminUser": "admin", "language": "ar", "installWoocommerce": "true",
	}
	secrets := map[string]string{"WP_ADMIN_PASSWORD": "s3cr3t"}

	env := template.Environment(values, secrets, "https://shop--8080.dev.example.com")

	want := map[string]string{
		"TPL_SITE_TITLE":          "My Shop",
		"TPL_ADMIN_EMAIL":         "owner@example.com",
		"TPL_ADMIN_USER":          "admin",
		"TPL_ADMIN_PASSWORD":      "s3cr3t",
		"TPL_LANGUAGE":            "ar",
		"TPL_INSTALL_WOOCOMMERCE": "true",
		"TPL_PREVIEW_URL":         "https://shop--8080.dev.example.com",
	}
	if len(env) != len(want) {
		t.Fatalf("Environment() = %v, want %d entries", env, len(want))
	}
	for key, value := range want {
		if env[key] != value {
			t.Fatalf("Environment()[%q] = %q, want %q", key, env[key], value)
		}
	}
}

func TestEnvironmentFillsInputsMissingFromOlderMetadata(t *testing.T) {
	// A project created before an input existed has no value for it. The
	// declared default keeps its provisioning reproducible instead of handing
	// the script an empty string it never expected.
	env := inputTemplate().Environment(Values{"siteTitle": "Old"}, nil, "")

	if env["TPL_ADMIN_USER"] != "admin" {
		t.Fatalf("TPL_ADMIN_USER = %q, want the declared default", env["TPL_ADMIN_USER"])
	}
	if env["TPL_LANGUAGE"] != "ar" {
		t.Fatalf("TPL_LANGUAGE = %q, want the declared default", env["TPL_LANGUAGE"])
	}
	if env["TPL_INSTALL_WOOCOMMERCE"] != "false" {
		t.Fatalf("TPL_INSTALL_WOOCOMMERCE = %q, want %q", env["TPL_INSTALL_WOOCOMMERCE"], "false")
	}
	if _, present := env[PreviewURLEnv]; present {
		t.Fatal("an empty preview URL must not be exported")
	}
}

func TestGeneratePasswordIsStrongAndDistinct(t *testing.T) {
	first, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword() = %v", err)
	}
	second, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword() = %v", err)
	}
	if first == second {
		t.Fatal("two generated passwords are identical")
	}
	if len(first) != generatedPasswordLength {
		t.Fatalf("length = %d, want %d", len(first), generatedPasswordLength)
	}
	for _, r := range first {
		if !strings.ContainsRune(passwordAlphabet, r) {
			t.Fatalf("password %q contains %q, which is outside the alphabet", first, r)
		}
	}
}

func TestValidateInputDeclaration(t *testing.T) {
	valid := inputTemplate().Inputs

	tests := []struct {
		name    string
		inputs  []Input
		admin   *AdminAccess
		wantErr string
	}{
		{name: "the shipped shape is accepted", inputs: valid},
		{
			name:    "an unknown type is a build defect",
			inputs:  []Input{{Key: "a", Label: "A", Type: "textarea"}},
			wantErr: "unknown type",
		},
		{
			name:    "a select without options cannot be rendered",
			inputs:  []Input{{Key: "a", Label: "A", Type: InputSelect}},
			wantErr: "at least one option",
		},
		{
			name: "a select default must be one of its options",
			inputs: []Input{{
				Key: "a", Label: "A", Type: InputSelect, Default: "z",
				Options: []InputOption{{Value: "y", Label: "Y"}},
			}},
			wantErr: "not one of its options",
		},
		{
			name:    "only a password may be a secret",
			inputs:  []Input{{Key: "a", Label: "A", Type: InputText, Secret: true, SecretName: "A"}},
			wantErr: "only a password input can be a secret",
		},
		{
			name:    "a secret needs a valid store key",
			inputs:  []Input{{Key: "a", Label: "A", Type: InputPassword, Secret: true, SecretName: "not a key"}},
			wantErr: "not a valid env var name",
		},
		{
			name:    "a secret must not ship a default",
			inputs:  []Input{{Key: "a", Label: "A", Type: InputPassword, Secret: true, SecretName: "A", Default: "hunter2"}},
			wantErr: "must not ship a default",
		},
		{
			name:    "two keys must not collide on one env var",
			inputs:  []Input{{Key: "siteTitle", Label: "A", Type: InputText}, {Key: "site_title", Label: "B", Type: InputText}},
			wantErr: "must match",
		},
		{
			name:    "a checkbox cannot be required",
			inputs:  []Input{{Key: "a", Label: "A", Type: InputCheckbox, Required: true}},
			wantErr: "cannot be required",
		},
		{
			name:    "adminAccess must point at a declared input",
			inputs:  valid,
			admin:   &AdminAccess{Label: "Admin", Port: 8080, UserInput: "nobody"},
			wantErr: "not a declared input",
		},
		{
			name:    "adminAccess must point at a secret some input produces",
			inputs:  valid,
			admin:   &AdminAccess{Label: "Admin", Port: 8080, PasswordSecret: "NOPE"},
			wantErr: "not produced by any input",
		},
		{
			name:   "a complete adminAccess is accepted",
			admin:  &AdminAccess{Label: "Admin", Port: 8080, Path: "/wp-admin", UserInput: "adminUser", PasswordSecret: "WP_ADMIN_PASSWORD"},
			inputs: valid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInputDeclaration(tt.inputs, tt.admin)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateInputDeclaration() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateInputDeclaration() = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}
