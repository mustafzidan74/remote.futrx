package auxmodel

import (
	"context"
	"strings"
	"unicode/utf8"
)

// The prompts below are the whole of this feature's "intelligence". They are
// written for a 3B model: short, imperative, one instruction per line, and
// each one repeats the output shape at the end, because a small model that is
// told the format twice obeys it far more often than one told it once.
//
// Every job also post-processes the answer. A small model will occasionally
// answer a request for six words with a paragraph, and the caller must get
// something usable or nothing at all — never a paragraph where a title goes.

// Input caps per job. They exist so a 300-message chat or a 4000-file diff
// cannot be sent to a model with an 8k context, and so the cost of a
// nice-to-have stays proportional to its value.
const (
	// TitleInputLimit is how much of the first prompt shapes a title.
	TitleInputLimit = 1200
	// SummaryInputLimit is how much of an assistant's answer is summarized.
	// The end of an answer is the useful part, so callers pass the tail.
	SummaryInputLimit = 4000
	// CommitInputLimit bounds the diff stat plus file names. It is small on
	// purpose: this job sees names and counts, never file contents.
	CommitInputLimit = 3000
	// TranslateInputLimit bounds one client message. It matches the snippet
	// body limit, so any template a user can save can also be translated.
	TranslateInputLimit = 8000
)

/* ------------------------------------------------------------------ *
 * Chat titles
 * ------------------------------------------------------------------ */

const titleSystemPrompt = `You name conversations.
Read the user's first message and write a title for it.
Rules:
- 3 to 6 words.
- Write the title in the SAME LANGUAGE as the user's message. Arabic message means an Arabic title; never translate it to English.
- No quotes, no punctuation at the end, no prefix like "Title:".
- Describe the task, not the person asking.
Answer with the title only.`

// TitleMaxRunes bounds the title that reaches the chat store. It is well
// under the sixty characters the truncating fallback produces, because a
// generated title that is longer than the thing it replaced has failed.
const TitleMaxRunes = 60

// Title asks for a short title for a chat, in the language the prompt was
// written in. An error means the caller keeps whatever title it has.
func (s *Service) Title(ctx context.Context, firstPrompt string) (string, error) {
	answer, err := s.Complete(
		ctx,
		JobChatTitle,
		titleSystemPrompt,
		Truncate(strings.TrimSpace(firstPrompt), TitleInputLimit),
	)
	if err != nil {
		return "", err
	}
	return CleanTitle(answer), nil
}

// CleanTitle shapes a model's answer into something that fits a sidebar row.
// It is exported so the fallback path and the tests agree on what "a title"
// means.
func CleanTitle(answer string) string {
	title := OneLine(answer)
	title = strings.TrimRight(title, ".。！!？?،,;: ")
	if utf8.RuneCountInString(title) > TitleMaxRunes {
		title = Truncate(title, TitleMaxRunes)
	}
	return strings.TrimSpace(title)
}

/* ------------------------------------------------------------------ *
 * Notification and chat summaries
 * ------------------------------------------------------------------ */

const runSummarySystemPrompt = `You write one-sentence status updates for a phone notification.
You are given the end of an AI coding agent's reply.
Write ONE sentence saying what was done or what is blocked.
Rules:
- One sentence, at most 25 words.
- Write in the SAME LANGUAGE as the agent's reply. Arabic reply means an Arabic sentence; never translate it.
- Concrete: name the thing that changed, not "the task".
- No preamble, no markdown, no code fences.
Answer with the sentence only.`

const chatSummarySystemPrompt = `You write one-line summaries used as search subtitles.
You are given the end of an AI coding agent's reply in a chat.
Write ONE short line describing what this conversation is about.
Rules:
- At most 12 words.
- Write in the SAME LANGUAGE as the reply. Arabic reply means an Arabic line; never translate it.
- No preamble, no markdown, no trailing punctuation.
Answer with the line only.`

// SummaryMaxRunes bounds a summary. A notification body and a sidebar
// subtitle are both single lines on a phone; anything longer is truncated by
// the sink anyway, and truncating here keeps the stored value honest.
const SummaryMaxRunes = 220

// RunSummary condenses an agent's closing words into the one sentence a
// notification should carry. On error the caller falls back to the raw tail
// it sends today.
func (s *Service) RunSummary(ctx context.Context, output string) (string, error) {
	answer, err := s.Complete(ctx, JobRunSummary, runSummarySystemPrompt, summaryInput(output))
	if err != nil {
		return "", err
	}
	return CleanSummary(answer), nil
}

// ChatSummary condenses the same text into the shorter line stored on chat
// meta for the sidebar and the dashboard.
func (s *Service) ChatSummary(ctx context.Context, output string) (string, error) {
	answer, err := s.Complete(ctx, JobChatSummary, chatSummarySystemPrompt, summaryInput(output))
	if err != nil {
		return "", err
	}
	return CleanSummary(answer), nil
}

// CleanSummary shapes a model's answer into one storable line.
func CleanSummary(answer string) string {
	summary := OneLine(answer)
	return strings.TrimSpace(Truncate(summary, SummaryMaxRunes))
}

// summaryInput takes the *end* of an agent's answer. An agent's closing
// paragraph says what happened; its opening paragraph restates the question.
func summaryInput(output string) string {
	output = strings.TrimSpace(output)
	runes := []rune(output)
	if len(runes) <= SummaryInputLimit {
		return output
	}
	return "…" + string(runes[len(runes)-SummaryInputLimit:])
}

/* ------------------------------------------------------------------ *
 * Commit messages
 * ------------------------------------------------------------------ */

const commitSystemPrompt = `You write git commit subject lines in Conventional Commits style.
You are given the output of "git diff --stat" and the list of changed paths.
Write ONE subject line.
Rules:
- Format: type(scope): summary — types are feat, fix, refactor, docs, test, chore, style, perf, build, ci.
- Lower case after the colon, imperative mood, no trailing full stop.
- At most 72 characters.
- Infer the scope from the changed paths; omit "(scope)" if nothing fits.
- You cannot see file contents, so describe the change at the level the paths support.
Answer with the subject line only.`

// CommitSubjectMaxRunes is git's own comfortable subject length.
const CommitSubjectMaxRunes = 72

// CommitMessage turns a diff stat and the changed paths into a conventional
// commit subject. The caller falls back to the deterministic dated message.
func (s *Service) CommitMessage(ctx context.Context, diffStat string) (string, error) {
	answer, err := s.Complete(
		ctx,
		JobCommitMessage,
		commitSystemPrompt,
		Truncate(strings.TrimSpace(diffStat), CommitInputLimit),
	)
	if err != nil {
		return "", err
	}
	return CleanCommitSubject(answer), nil
}

// CleanCommitSubject shapes a model's answer into a single subject line. An
// answer that came back with a body is cut down to its first line: the commit
// dialog offers one input, and a newline in it would be silently dropped by
// git anyway.
func CleanCommitSubject(answer string) string {
	subject := OneLine(answer)
	subject = strings.TrimRight(subject, ". ")
	if utf8.RuneCountInString(subject) > CommitSubjectMaxRunes {
		subject = Truncate(subject, CommitSubjectMaxRunes)
	}
	return strings.TrimSpace(subject)
}

/* ------------------------------------------------------------------ *
 * Client message translation
 * ------------------------------------------------------------------ */

// TranslationTarget is the language a client message is translated into. Only
// the two languages the client templates are written in are supported,
// because those are the two fields the editor has.
type TranslationTarget string

const (
	TargetArabic  TranslationTarget = "ar"
	TargetEnglish TranslationTarget = "en"
)

// NormalizeTarget maps anything a client sends onto one of the two targets.
// Anything unrecognized becomes English, which is the fallback variant every
// template already carries.
func NormalizeTarget(target string) TranslationTarget {
	if strings.EqualFold(strings.TrimSpace(target), string(TargetArabic)) {
		return TargetArabic
	}
	return TargetEnglish
}

const translateSystemPromptAR = `You translate short business messages into Modern Standard Arabic.
Rules:
- Translate the meaning, not word by word. Keep the tone professional and warm.
- Keep every {{placeholder}} token exactly as written, in the same form.
- Keep URLs, code, file paths, and product names unchanged.
- Preserve line breaks and list structure.
Answer with the translation only, with no preamble and no explanation.`

const translateSystemPromptEN = `You translate short business messages into English.
Rules:
- Translate the meaning, not word by word. Keep the tone professional and warm.
- Keep every {{placeholder}} token exactly as written, in the same form.
- Keep URLs, code, file paths, and product names unchanged.
- Preserve line breaks and list structure.
Answer with the translation only, with no preamble and no explanation.`

// Translate renders one client message in the other language. Unlike the
// other jobs this one has no silent fallback: it runs because a person
// pressed a button, so the caller reports the failure to them instead of
// quietly doing nothing.
func (s *Service) Translate(
	ctx context.Context,
	target TranslationTarget,
	text string,
) (string, error) {
	system := translateSystemPromptEN
	if target == TargetArabic {
		system = translateSystemPromptAR
	}
	answer, err := s.Complete(
		ctx,
		JobTranslate,
		system,
		Truncate(strings.TrimSpace(text), TranslateInputLimit),
	)
	if err != nil {
		return "", err
	}
	return CleanTranslation(answer), nil
}

// CleanTranslation keeps the paragraph structure a message needs — this is
// the one job whose answer is multi-line by design — and strips only the
// wrappers a chatty model adds around it.
func CleanTranslation(answer string) string {
	text := strings.TrimSpace(answer)
	if _, after, found := strings.Cut(text, "</think>"); found {
		text = strings.TrimSpace(after)
	}
	// A fenced block around the whole answer is a formatting habit, not part
	// of the message.
	if strings.HasPrefix(text, "```") {
		if _, rest, found := strings.Cut(text, "\n"); found {
			text = rest
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 1 && isPreamble(strings.TrimSpace(lines[0])) {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
