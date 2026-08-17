package project

import (
	"strings"
	"unicode"
)

const (
	MaxSlugLen = 32
	// MinSlugLen keeps "<slug>--<port>" a valid IDNA label (see Slugify).
	MinSlugLen = 3
)

// Slugify produces an LXC- and DNS-safe identifier from a display name.
func Slugify(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	lastDash := true
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || r == '.' || r == '/' || unicode.IsSpace(r):
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		s = "project"
	} else if !(s[0] >= 'a' && s[0] <= 'z') {
		s = "p-" + s
	}
	if len(s) > MaxSlugLen {
		s = strings.TrimRight(s[:MaxSlugLen], "-")
	}
	if len(s) < MinSlugLen {
		// Preview hosts are "<slug>--<port>": a two-character slug puts the
		// "--" at label positions 3-4, which IDNA reserves for punycode
		// ("xn--"), so Go's TLS stack rejects the host name and no
		// certificate can ever be issued for the project's previews.
		s += "-project"
	}
	return s
}

func ValidSlug(s string) bool {
	if len(s) < MinSlugLen || len(s) > MaxSlugLen {
		return false
	}
	// "--" at positions 3-4 is reserved by IDNA; the preview host would be
	// unresolvable for TLS purposes.
	if strings.HasPrefix(s[2:], "--") {
		return false
	}
	if !(s[0] >= 'a' && s[0] <= 'z') {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}
