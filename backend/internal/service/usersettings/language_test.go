package usersettings

import (
	"context"
	"errors"
	"testing"
)

func TestInterfaceLanguageDefaultsToAuto(t *testing.T) {
	settings, err := New(&memoryRepo{}).Get(context.Background(), "sub:user")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Appearance.Language != LanguageAuto {
		t.Fatalf("default language = %q, want auto", settings.Appearance.Language)
	}
}

// A document saved before the field existed has no language at all. It must
// read back as "auto" rather than as an invalid empty value the frontend would
// have to guess about.
func TestStoredSettingsWithoutALanguageReadBackAsAuto(t *testing.T) {
	stored := DefaultSettings()
	stored.Appearance.Language = ""
	repo := &memoryRepo{settings: stored, exists: true}

	settings, err := New(repo).Get(context.Background(), "sub:user")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Appearance.Language != LanguageAuto {
		t.Fatalf("legacy language = %q, want auto", settings.Appearance.Language)
	}
}

func TestUpdateStoresTheInterfaceLanguageWithoutTouchingTheTheme(t *testing.T) {
	repo := &memoryRepo{}
	service := New(repo)
	dark := ThemeDark
	if _, err := service.Update(context.Background(), "sub:user", UpdateInput{
		Appearance: &AppearanceUpdate{Theme: &dark},
	}); err != nil {
		t.Fatal(err)
	}

	arabic := Language(" ar ")
	settings, err := service.Update(context.Background(), "sub:user", UpdateInput{
		Appearance: &AppearanceUpdate{Language: &arabic},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Appearance.Language != LanguageArabic {
		t.Fatalf("language = %q, want ar", settings.Appearance.Language)
	}
	if settings.Appearance.Theme != ThemeDark {
		t.Fatalf("theme = %q, a language update must leave it alone", settings.Appearance.Theme)
	}
}

func TestUpdateRejectsAnUnsupportedLanguage(t *testing.T) {
	french := Language("fr")
	_, err := New(&memoryRepo{}).Update(context.Background(), "sub:user", UpdateInput{
		Appearance: &AppearanceUpdate{Language: &french},
	})
	if !errors.Is(err, ErrInvalidLanguage) {
		t.Fatalf("error = %v, want ErrInvalidLanguage", err)
	}
}
