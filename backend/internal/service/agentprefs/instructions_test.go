package agentprefs

import (
	"strings"
	"testing"
)

func TestInstructionsRendering(t *testing.T) {
	tests := []struct {
		name     string
		prefs    Preferences
		language string
		want     []string
		absent   []string
	}{
		{
			name:   "defaults inject nothing",
			prefs:  Defaults(),
			want:   nil,
			absent: []string{"Reply"},
		},
		{
			name:     "egyptian arabic names the dialect and pins code to english",
			prefs:    Preferences{ReplyLanguage: LanguageEgyptianArabic, Tone: ToneConcise},
			language: LanguageEgyptianArabic,
			want: []string{
				Heading,
				"Reply in Egyptian Arabic (عامية مصرية مبسطة)",
				"keep code, identifiers, commands and file paths in English",
				"be concise",
			},
		},
		{
			name:     "modern standard arabic is a distinct variant",
			prefs:    Preferences{ReplyLanguage: LanguageArabic, Tone: ToneDefault},
			language: LanguageArabic,
			want:     []string{"Modern Standard Arabic"},
			absent:   []string{"be concise", "Egyptian"},
		},
		{
			name:     "english variant",
			prefs:    Preferences{ReplyLanguage: LanguageEnglish, Tone: ToneDetailed},
			language: LanguageEnglish,
			want:     []string{"Reply in English", "be thorough"},
		},
		{
			name:     "a custom label is rendered verbatim",
			prefs:    Preferences{ReplyLanguage: "Levantine Arabic", Tone: ToneDefault},
			language: "Levantine Arabic",
			want:     []string{"Reply in Levantine Arabic"},
		},
		{
			name: "auto language still renders house rules",
			prefs: Preferences{
				ReplyLanguage:     LanguageAuto,
				Tone:              ToneDefault,
				ExtraInstructions: "Never force-push.",
			},
			language: LanguageAuto,
			want:     []string{"Never force-push."},
			absent:   []string{"Reply in"},
		},
		{
			name:     "the user override beats the platform language",
			prefs:    Preferences{ReplyLanguage: LanguageEnglish, Tone: ToneDefault},
			language: LanguageEgyptianArabic,
			want:     []string{"Egyptian Arabic"},
			absent:   []string{"Reply in English"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Instructions(Normalize(test.prefs), test.language)
			if len(test.want) == 0 && got != "" {
				t.Fatalf("Instructions() = %q, want empty", got)
			}
			for _, fragment := range test.want {
				if !strings.Contains(got, fragment) {
					t.Errorf("Instructions() = %q, missing %q", got, fragment)
				}
			}
			for _, fragment := range test.absent {
				if strings.Contains(got, fragment) {
					t.Errorf("Instructions() = %q, unexpectedly contains %q", got, fragment)
				}
			}
		})
	}
}

func TestPreambleIsOneParagraphAndBounded(t *testing.T) {
	prefs := Normalize(Preferences{
		ReplyLanguage:     LanguageEgyptianArabic,
		Tone:              ToneConcise,
		ExtraInstructions: strings.Repeat("و", preambleExtraBudget+200),
	})

	got := Preamble(prefs, prefs.ReplyLanguage)
	if strings.Contains(got, "\n") {
		t.Errorf("Preamble() spans multiple lines: %q", got)
	}
	if !strings.HasSuffix(got, "[…]") {
		t.Errorf("Preamble() = %q, want the over-budget extra instructions elided", got)
	}
	if !strings.Contains(got, "Egyptian Arabic") {
		t.Errorf("Preamble() = %q, missing the language clause", got)
	}
}

func TestNormalizeMapsLanguageSpellings(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", LanguageAuto},
		{"  ", LanguageAuto},
		{"AUTO", LanguageAuto},
		{"english", LanguageEnglish},
		{"en-US", LanguageEnglish},
		{"Arabic", LanguageArabic},
		{"ar-eg", LanguageEgyptianArabic},
		{"AR_EG", LanguageEgyptianArabic},
		{"Levantine Arabic", "Levantine Arabic"},
	}
	for _, test := range tests {
		if got := Normalize(Preferences{ReplyLanguage: test.in}).ReplyLanguage; got != test.want {
			t.Errorf("Normalize(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestValidateRejectsOversizedDocuments(t *testing.T) {
	tests := []struct {
		name    string
		prefs   Preferences
		wantErr bool
	}{
		{name: "defaults are valid", prefs: Defaults()},
		{
			name:    "extra instructions above the cap",
			prefs:   Preferences{ExtraInstructions: strings.Repeat("a", MaxExtraInstructionsLength+1)},
			wantErr: true,
		},
		{
			name:    "language label above the cap",
			prefs:   Preferences{ReplyLanguage: strings.Repeat("a", MaxReplyLanguageLength+1)},
			wantErr: true,
		},
		{name: "unknown tone", prefs: Preferences{Tone: "shouty"}, wantErr: true},
		{name: "unknown applyTo", prefs: Preferences{ApplyTo: "someProjects"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(Normalize(test.prefs))
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
