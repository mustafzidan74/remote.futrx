package auxmodel

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanTitleTamesAChattySmallModel(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		want   string
	}{
		{
			name:   "a plain answer passes through",
			answer: "Fix the login redirect loop",
			want:   "Fix the login redirect loop",
		},
		{
			name:   "quotes and a trailing stop are wrappers, not the title",
			answer: "\"Fix the login redirect loop.\"",
			want:   "Fix the login redirect loop",
		},
		{
			name:   "a preamble line is dropped",
			answer: "Sure, here is a title:\nFix the login redirect loop",
			want:   "Fix the login redirect loop",
		},
		{
			name:   "a Title: prefix is dropped",
			answer: "Title: Fix the login redirect loop",
			want:   "Fix the login redirect loop",
		},
		{
			name:   "reasoning tags are not part of the answer",
			answer: "<think>the user wants a name</think>\nFix the login redirect loop",
			want:   "Fix the login redirect loop",
		},
		{
			name:   "Arabic comes back untouched",
			answer: "إصلاح حلقة إعادة التوجيه",
			want:   "إصلاح حلقة إعادة التوجيه",
		},
		{
			name:   "an Arabic full stop is trimmed too",
			answer: "إصلاح حلقة إعادة التوجيه.",
			want:   "إصلاح حلقة إعادة التوجيه",
		},
		{
			name:   "nothing in, nothing out",
			answer: "   ",
			want:   "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CleanTitle(test.answer); got != test.want {
				t.Fatalf("CleanTitle(%q) = %q, want %q", test.answer, got, test.want)
			}
		})
	}
}

func TestCleanTitleNeverOutgrowsTheThingItReplaces(t *testing.T) {
	long := strings.Repeat("a very wordy title ", 20)
	got := CleanTitle(long)
	if utf8.RuneCountInString(got) > TitleMaxRunes+1 {
		t.Fatalf("CleanTitle() produced %d runes, want at most %d", utf8.RuneCountInString(got), TitleMaxRunes+1)
	}
}

func TestCleanCommitSubjectFitsOnOneLine(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		want   string
	}{
		{
			name:   "a conventional subject passes through",
			answer: "feat(auth): add device login",
			want:   "feat(auth): add device login",
		},
		{
			name:   "a body after the subject is dropped",
			answer: "fix(chat): stop the double send\n\nThe composer fired twice on Enter.",
			want:   "fix(chat): stop the double send",
		},
		{
			name:   "a bullet marker is not part of the subject",
			answer: "- refactor(store): split the writer",
			want:   "refactor(store): split the writer",
		},
		{
			name:   "a trailing full stop is removed",
			answer: "docs: describe the auxiliary model.",
			want:   "docs: describe the auxiliary model",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CleanCommitSubject(test.answer)
			if got != test.want {
				t.Fatalf("CleanCommitSubject(%q) = %q, want %q", test.answer, got, test.want)
			}
			if strings.Contains(got, "\n") {
				t.Fatal("a commit subject must be one line")
			}
		})
	}
}

func TestCleanTranslationKeepsParagraphsButDropsWrappers(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		want   string
	}{
		{
			name:   "two paragraphs survive",
			answer: "مرحبا،\n\nتم إنجاز العمل.",
			want:   "مرحبا،\n\nتم إنجاز العمل.",
		},
		{
			name:   "a preamble line is dropped",
			answer: "Here is the translation:\nمرحبا",
			want:   "مرحبا",
		},
		{
			name:   "a fenced block is unwrapped",
			answer: "```\nHello there\n```",
			want:   "Hello there",
		},
		{
			name:   "placeholders are left exactly as written",
			answer: "Hello {{client}}, your site {{url}} is ready.",
			want:   "Hello {{client}}, your site {{url}} is ready.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CleanTranslation(test.answer); got != test.want {
				t.Fatalf("CleanTranslation(%q) = %q, want %q", test.answer, got, test.want)
			}
		})
	}
}

func TestSummaryInputTakesTheEndOfTheAnswer(t *testing.T) {
	// An agent's closing paragraph says what happened; its opening paragraph
	// restates the question, so the tail is the useful half.
	head := strings.Repeat("x", SummaryInputLimit)
	body := head + "AND HERE IS THE CONCLUSION"
	got := summaryInput(body)

	if !strings.HasSuffix(got, "AND HERE IS THE CONCLUSION") {
		t.Fatal("summaryInput() dropped the conclusion")
	}
	if !strings.HasPrefix(got, "…") {
		t.Fatal("summaryInput() did not mark that it cut the head off")
	}
	if utf8.RuneCountInString(got) > SummaryInputLimit+1 {
		t.Fatalf("summaryInput() kept %d runes, want at most %d", utf8.RuneCountInString(got), SummaryInputLimit+1)
	}
}

func TestNormalizeTargetOnlyEverAnswersWithTheTwoVariants(t *testing.T) {
	tests := []struct {
		in   string
		want TranslationTarget
	}{
		{in: "ar", want: TargetArabic},
		{in: " AR ", want: TargetArabic},
		{in: "en", want: TargetEnglish},
		{in: "", want: TargetEnglish},
		{in: "fr", want: TargetEnglish},
	}

	for _, test := range tests {
		if got := NormalizeTarget(test.in); got != test.want {
			t.Fatalf("NormalizeTarget(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestJobHelpersCapTheirInputAndAskTheRightJob(t *testing.T) {
	tests := []struct {
		name    string
		job     Job
		call    func(*Service) (string, error)
		wantSub string
	}{
		{
			name: "Title asks the chat-title job",
			job:  JobChatTitle,
			call: func(s *Service) (string, error) {
				return s.Title(context.Background(), strings.Repeat("word ", 2000))
			},
			wantSub: "name conversations",
		},
		{
			name: "RunSummary asks the notification job",
			job:  JobRunSummary,
			call: func(s *Service) (string, error) {
				return s.RunSummary(context.Background(), "the deploy finished")
			},
			wantSub: "phone notification",
		},
		{
			name: "CommitMessage asks the commit job",
			job:  JobCommitMessage,
			call: func(s *Service) (string, error) {
				return s.CommitMessage(context.Background(), " auth.go | 3 +++ ")
			},
			wantSub: "Conventional Commits",
		},
		{
			name: "Translate to Arabic asks the translate job with the Arabic prompt",
			job:  JobTranslate,
			call: func(s *Service) (string, error) {
				return s.Translate(context.Background(), TargetArabic, "your site is ready")
			},
			wantSub: "Modern Standard Arabic",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			completer := &stubCompleter{answer: "an answer"}
			service := newTestService(t, activeConfig(), completer)

			if _, err := test.call(service); err != nil {
				t.Fatalf("call = %v", err)
			}
			if !strings.Contains(completer.lastReq.SystemPrompt, test.wantSub) {
				t.Fatalf("system prompt = %q, want it to mention %q",
					completer.lastReq.SystemPrompt, test.wantSub)
			}
			want := tokenCap(service.Config(), test.job)
			if completer.lastReq.MaxTokens != want {
				t.Fatalf("MaxTokens = %d, want the %s cap of %d",
					completer.lastReq.MaxTokens, test.job, want)
			}
			if utf8.RuneCountInString(completer.lastReq.UserText) > maxInputRunes+1 {
				t.Fatalf("the job sent %d runes, past the %d backstop",
					utf8.RuneCountInString(completer.lastReq.UserText), maxInputRunes)
			}
		})
	}
}
