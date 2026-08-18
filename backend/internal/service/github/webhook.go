package github

// The inbound half. Everything in this file runs on data supplied by the
// public internet, so it is written to answer one question at a time and to
// fail closed at every step.
//
// The order of the gates matters and is not an accident:
//
//  1. size — a body over the cap is dropped before it is read into memory;
//  2. signature — an unsigned or mis-signed body never reaches a JSON parser;
//  3. mapping — only three shapes of event mean anything, and each of the
//     three carries its own permission rule;
//  4. autoRun — even a correctly signed, correctly labelled event only
//     *creates a chat and notifies* until an administrator opts in.
//
// Nothing here trusts a field for authorization that the sender controls
// freely. `sender.login` is recorded but never authorizes; the label and the
// author association are what authorize, and both require repository push
// access to set.

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// SignatureHeader is the header GitHub signs a delivery with.
const SignatureHeader = "X-Hub-Signature-256"

// EventHeader names the event type of a delivery.
const EventHeader = "X-GitHub-Event"

// DeliveryHeader carries GitHub's own delivery GUID.
const DeliveryHeader = "X-GitHub-Delivery"

// signaturePrefix is the algorithm marker GitHub prepends to the digest.
const signaturePrefix = "sha256="

// Sign returns the value GitHub would put in X-Hub-Signature-256 for this
// body and secret. It exists so verification and the test suite agree by
// construction rather than by transcription.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature reports whether header is a valid signature of body under
// secret.
//
// The comparison is constant time, and the decision is made on the raw bytes
// that arrived — not on a re-encoding of the parsed JSON, which would let a
// sender smuggle a different document past a signature computed over the
// original.
func VerifySignature(secret string, body []byte, header string) error {
	if strings.TrimSpace(secret) == "" {
		return ErrWebhookDisabled
	}
	provided := strings.TrimSpace(header)
	if provided == "" {
		return ErrBadSignature
	}
	if !strings.HasPrefix(provided, signaturePrefix) {
		return ErrBadSignature
	}
	// Decoding first means a signature of the wrong length or with non-hex
	// characters is rejected without ever being compared.
	got, err := hex.DecodeString(strings.TrimPrefix(provided, signaturePrefix))
	if err != nil {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if subtle.ConstantTimeCompare(got, mac.Sum(nil)) != 1 {
		return ErrBadSignature
	}
	return nil
}

// payload is the subset of GitHub's webhook body this integration reads.
// Everything else in a delivery is deliberately ignored.
type payload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	Label struct {
		Name string `json:"name"`
	} `json:"label"`
	Issue struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"issue"`
	Comment struct {
		Body              string `json:"body"`
		HTMLURL           string `json:"html_url"`
		AuthorAssociation string `json:"author_association"`
		User              struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Review struct {
		State             string `json:"state"`
		Body              string `json:"body"`
		HTMLURL           string `json:"html_url"`
		AuthorAssociation string `json:"author_association"`
		User              struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"review"`
	PullRequest struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	} `json:"pull_request"`
}

// Decision is what an inbound delivery means. It is the whole output of the
// mapping step, so a test can assert the rules without a container, a chat, or
// a network.
type Decision struct {
	// Act is false for a delivery this integration has no rule for. Reason
	// still says why, because "nothing happened" is the answer an operator
	// stares at most often.
	Act bool
	// Number is the issue or pull request number the chat is keyed on.
	Number int
	// Title is the issue or pull request title, used for the chat title.
	Title string
	// Instruction is the text handed to the agent.
	Instruction string
	// URL is the issue, comment, or review permalink.
	URL string
	// Sender is the login that produced the delivery, for the audit trail.
	Sender string
	// Trigger names the rule that matched: "label", "command", or "review".
	Trigger string
	// Reason explains an ignored delivery.
	Reason string
}

// Trigger names.
const (
	TriggerLabel   = "label"
	TriggerCommand = "command"
	TriggerReview  = "review"
)

// privilegedAssociations are the author associations that imply write access
// to the repository. A comment from anybody else can ask for nothing: the
// whole point of the gate is that an arbitrary GitHub account must not be able
// to make this server run an agent.
// CONTRIBUTOR is deliberately absent: it means "has had a pull request merged
// here", which is a past favour, not present write access.
var privilegedAssociations = map[string]bool{
	"OWNER":        true,
	"MEMBER":       true,
	"COLLABORATOR": true,
}

// Privileged reports whether an author association implies repository write
// access. Unknown values are treated as unprivileged, so a new association
// GitHub invents later fails closed.
func Privileged(association string) bool {
	return privilegedAssociations[strings.ToUpper(strings.TrimSpace(association))]
}

// ParseCommand extracts the instruction from a comment containing the command
// verb. It returns ok=false when the verb is absent.
//
// The verb has to start a line: a comment that merely mentions `/remote` in
// the middle of a sentence ("we should use /remote for this") is prose, not a
// request. Everything from the verb to the end of the comment is the
// instruction, so a multi-line request survives intact.
func ParseCommand(body string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != CommandPrefix && !strings.HasPrefix(trimmed, CommandPrefix+" ") {
			continue
		}
		first := strings.TrimSpace(strings.TrimPrefix(trimmed, CommandPrefix))
		rest := strings.TrimSpace(strings.Join(lines[index+1:], "\n"))
		instruction := strings.TrimSpace(first + "\n" + rest)
		if instruction == "" {
			return "", false
		}
		return instruction, true
	}
	return "", false
}

// hasLabel reports whether the issue in a payload carries the trigger label.
// Comparison is case-insensitive because GitHub preserves the case an operator
// typed but treats labels case-insensitively itself.
func (p payload) hasLabel(label string) bool {
	for _, entry := range p.Issue.Labels {
		if strings.EqualFold(strings.TrimSpace(entry.Name), label) {
			return true
		}
	}
	return false
}

// MapEvent turns one verified delivery into a Decision.
//
// It is pure: the same event and settings always produce the same answer, and
// it reaches nothing outside its arguments. That is what makes the permission
// rules testable rather than merely reviewable.
func MapEvent(event string, body []byte, settings Settings) (Decision, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return Decision{Reason: "payload is not valid JSON"}, err
	}
	label := settings.LabelOrDefault()
	sender := p.Sender.Login

	switch strings.ToLower(strings.TrimSpace(event)) {
	case "ping":
		return Decision{Sender: sender, Reason: "ping acknowledged"}, nil

	case "issues":
		// Only `opened` and `labeled` are rules. `edited`, `closed` and the
		// rest are deliberately inert: an issue that has already been through
		// this gate must not be able to re-trigger by being edited.
		action := strings.ToLower(p.Action)
		if action != "opened" && action != "labeled" {
			return Decision{Sender: sender, Reason: "issues." + p.Action + " has no rule"}, nil
		}
		// The label is the authorization. GitHub drops labels supplied by an
		// account without push access, so "the trigger label is present" is
		// equivalent to "somebody with write access asked for this".
		if !p.hasLabel(label) && !strings.EqualFold(p.Label.Name, label) {
			return Decision{Sender: sender, Reason: "issue is not labelled " + label}, nil
		}
		instruction := strings.TrimSpace(p.Issue.Body)
		if instruction == "" {
			instruction = "(the issue has no description)"
		}
		return Decision{
			Act:         true,
			Number:      p.Issue.Number,
			Title:       p.Issue.Title,
			Instruction: instruction,
			URL:         p.Issue.HTMLURL,
			Sender:      sender,
			Trigger:     TriggerLabel,
		}, nil

	case "issue_comment":
		if strings.ToLower(p.Action) != "created" {
			return Decision{Sender: sender, Reason: "issue_comment." + p.Action + " has no rule"}, nil
		}
		instruction, ok := ParseCommand(p.Comment.Body)
		if !ok {
			return Decision{Sender: sender, Reason: "comment carries no " + CommandPrefix + " command"}, nil
		}
		if !Privileged(p.Comment.AuthorAssociation) {
			return Decision{
				Sender: sender,
				Reason: CommandPrefix + " from a non-collaborator (" +
					strings.ToLower(p.Comment.AuthorAssociation) + ") ignored",
			}, nil
		}
		return Decision{
			Act:         true,
			Number:      p.Issue.Number,
			Title:       p.Issue.Title,
			Instruction: instruction,
			URL:         p.Comment.HTMLURL,
			Sender:      sender,
			Trigger:     TriggerCommand,
		}, nil

	case "pull_request_review":
		if strings.ToLower(p.Action) != "submitted" {
			return Decision{Sender: sender, Reason: "pull_request_review." + p.Action + " has no rule"}, nil
		}
		if !strings.EqualFold(strings.TrimSpace(p.Review.State), "changes_requested") {
			return Decision{Sender: sender, Reason: "review state " + p.Review.State + " has no rule"}, nil
		}
		if !Privileged(p.Review.AuthorAssociation) {
			return Decision{
				Sender: sender,
				Reason: "review from a non-collaborator (" +
					strings.ToLower(p.Review.AuthorAssociation) + ") ignored",
			}, nil
		}
		instruction := strings.TrimSpace(p.Review.Body)
		if instruction == "" {
			instruction = "(the review requested changes without a summary comment)"
		}
		return Decision{
			Act:         true,
			Number:      p.PullRequest.Number,
			Title:       p.PullRequest.Title,
			Instruction: instruction,
			URL:         p.Review.HTMLURL,
			Sender:      sender,
			Trigger:     TriggerReview,
		}, nil

	default:
		return Decision{Sender: sender, Reason: "event " + event + " has no rule"}, nil
	}
}
