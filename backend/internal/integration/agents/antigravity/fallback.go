package antigravity

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// maxCapacityFallbacks bounds how many other models one turn tries after
// Google reports that a model has no capacity. Each attempt is a real run, so
// the chain stays short.
const maxCapacityFallbacks = 3

// capacityContinuePrompt resumes a conversation whose model ran out of
// capacity part-way. The conversation already holds the user's request and any
// work done, so the agent continues rather than starting over.
const capacityContinuePrompt = "The previous model stopped because it had no capacity. " +
	"Continue the same task from exactly where you stopped; do not redo finished steps."

var (
	// Transient, provider-side unavailability — not the account's quota and not
	// a bad request, which another model would not fix.
	capacityErrorPattern = regexp.MustCompile(
		`(?i)no capacity available|model is overloaded|\bUNAVAILABLE\b.{0,20}\b503\b|\b503\b.{0,40}\bUNAVAILABLE\b`)
	capacityModelPattern = regexp.MustCompile(`(?i)for model\s+([A-Za-z0-9][A-Za-z0-9._-]*)`)
	geminiModelPattern   = regexp.MustCompile(`^gemini-(\d+(?:\.\d+)?)-([a-z]+)-([a-z]+)$`)
)

// capacityFailure reports whether text is a no-capacity error, and the model it
// names when it names one.
func capacityFailure(text string) (bool, string) {
	if !capacityErrorPattern.MatchString(text) {
		return false, ""
	}
	if match := capacityModelPattern.FindStringSubmatch(text); match != nil {
		return true, match[1]
	}
	return true, ""
}

var effortRank = map[string]int{"high": 0, "medium": 1, "low": 2}

// nextCapacityFallback picks the model to try after current had no capacity:
// the same tier and effort on older versions, nearest first (3.8 Flash High →
// 3.7 Flash High → 3.6 Flash High), then one effort lower across the same
// versions (3.8 Medium → 3.7 Medium → …), and so on down.
// Only models the CLI listed and not yet tried are candidates; a model outside
// the gemini-<version>-<tier>-<effort> family has no fallback.
func nextCapacityFallback(current string, available []string, tried map[string]bool) string {
	match := geminiModelPattern.FindStringSubmatch(current)
	if match == nil {
		return ""
	}
	version, _ := strconv.ParseFloat(match[1], 64)
	tier, effort := match[2], match[3]

	type candidate struct {
		id            string
		sameEffort    bool
		versionBehind float64
		effortDrop    int
	}
	var candidates []candidate
	for _, id := range available {
		if id == current || tried[id] {
			continue
		}
		other := geminiModelPattern.FindStringSubmatch(id)
		if other == nil || other[2] != tier {
			continue
		}
		otherVersion, _ := strconv.ParseFloat(other[1], 64)
		if otherVersion > version {
			continue
		}
		rank, known := effortRank[other[3]]
		if !known || rank < effortRank[effort] {
			continue
		}
		candidates = append(candidates, candidate{
			id:            id,
			sameEffort:    other[3] == effort,
			versionBehind: version - otherVersion,
			effortDrop:    rank - effortRank[effort],
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.sameEffort != b.sameEffort {
			return a.sameEffort
		}
		if a.effortDrop != b.effortDrop {
			return a.effortDrop < b.effortDrop
		}
		return a.versionBehind < b.versionBehind
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].id
}

// modelIDs lists the ids from an `agy models` catalog.
func modelIDs(output string) []string {
	entries := parseCLIModels(output)
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, strings.TrimSpace(entry.id))
	}
	return ids
}
