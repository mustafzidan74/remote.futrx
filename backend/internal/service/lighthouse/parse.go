package lighthouse

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
)

// rawReport is the sliver of Lighthouse's JSON this platform reads.
//
// The real document is a few hundred kilobytes and over a hundred audits.
// Decoding into a narrow struct rather than a map is what keeps a new
// Lighthouse release from changing the shape of what gets stored: fields this
// does not name are ignored, and fields it names that disappear come through
// as zero and are handled below.
type rawReport struct {
	LighthouseVersion string `json:"lighthouseVersion"`
	FetchTime         string `json:"fetchTime"`
	RuntimeError      *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"runtimeError"`
	Categories map[string]struct {
		Title  string   `json:"title"`
		Score  *float64 `json:"score"`
		Audits []struct {
			ID string `json:"id"`
		} `json:"auditRefs"`
	} `json:"categories"`
	Audits map[string]rawAudit `json:"audits"`
}

type rawAudit struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Score            *float64 `json:"score"`
	ScoreDisplayMode string   `json:"scoreDisplayMode"`
	DisplayValue     string   `json:"displayValue"`
	NumericValue     float64  `json:"numericValue"`
	NumericUnit      string   `json:"numericUnit"`
	Details          *struct {
		OverallSavingsMs float64 `json:"overallSavingsMs"`
	} `json:"details"`
}

// Parse turns one Lighthouse JSON report into the summary this platform keeps.
func Parse(path string, data []byte, now int64) (Report, error) {
	if len(data) == 0 || len(data) > MaxReportBytes {
		return Report{}, ErrBadReport
	}
	var raw rawReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return Report{}, ErrBadReport
	}
	// A runtime error is Lighthouse saying it could not audit the page at all
	// — a redirect loop, a page that never loaded. It exits zero and produces
	// a report full of nulls, which without this check would be stored as a
	// page that scored nothing.
	if raw.RuntimeError != nil && raw.RuntimeError.Code != "" {
		message := strings.TrimSpace(raw.RuntimeError.Message)
		if message == "" {
			message = raw.RuntimeError.Code
		}
		return Report{Path: path, Error: message, FetchedAt: now}, nil
	}
	if len(raw.Categories) == 0 {
		return Report{}, ErrBadReport
	}

	report := Report{
		Path:          path,
		Performance:   percent(raw.Categories["performance"].Score),
		Accessibility: percent(raw.Categories["accessibility"].Score),
		BestPractices: percent(raw.Categories["best-practices"].Score),
		SEO:           percent(raw.Categories["seo"].Score),
		Version:       raw.LighthouseVersion,
		FetchedAt:     now,
	}

	for _, tracked := range TrackedMetrics {
		audit, ok := raw.Audits[tracked.ID]
		if !ok || audit.ScoreDisplayMode == "notApplicable" {
			continue
		}
		report.Metrics = append(report.Metrics, Metric{
			ID:      tracked.ID,
			Label:   tracked.Label,
			Display: audit.DisplayValue,
			Value:   audit.NumericValue,
			Unit:    audit.NumericUnit,
			Score:   audit.Score,
		})
	}
	report.Opportunities = opportunities(raw)
	return report, nil
}

// opportunities picks the failing audits worth showing.
//
// "Worth showing" is doing real work here. Lighthouse marks over a hundred
// audits per page, and most failures are either informational, not applicable,
// or a single missing attribute nobody is going to act on this afternoon. The
// list is cut to the ones that failed outright or nearly, ordered by the time
// Lighthouse thinks fixing them would return, so the top of the list is the
// thing to do first rather than the thing that happens to sort first
// alphabetically.
func opportunities(raw rawReport) []Finding {
	// Which category an audit belongs to, so a finding can say where it came
	// from. An audit can appear under more than one; the first wins, which is
	// the same order Lighthouse's own report lists them in.
	category := make(map[string]string, len(raw.Audits))
	for id, group := range raw.Categories {
		for _, ref := range group.Audits {
			if _, taken := category[ref.ID]; !taken {
				category[ref.ID] = id
			}
		}
	}

	findings := make([]Finding, 0, MaxOpportunities)
	for id, audit := range raw.Audits {
		if audit.Score == nil || *audit.Score >= 0.9 {
			continue
		}
		// binary and numeric are the modes that carry a verdict. informative
		// and notApplicable do not, and manual is a checklist item for a human.
		if audit.ScoreDisplayMode != "" &&
			audit.ScoreDisplayMode != "binary" &&
			audit.ScoreDisplayMode != "numeric" &&
			audit.ScoreDisplayMode != "metricSavings" {
			continue
		}
		// The metrics are already reported on their own; repeating them as
		// failures would list "Largest Contentful Paint" twice on a slow page.
		if trackedMetric(id) {
			continue
		}
		finding := Finding{
			ID:       id,
			Title:    strings.TrimSpace(audit.Title),
			Category: category[id],
			Display:  audit.DisplayValue,
			Score:    audit.Score,
		}
		if audit.Details != nil {
			finding.SavingsMs = audit.Details.OverallSavingsMs
		}
		findings = append(findings, finding)
	}

	sort.SliceStable(findings, func(left, right int) bool {
		if findings[left].SavingsMs != findings[right].SavingsMs {
			return findings[left].SavingsMs > findings[right].SavingsMs
		}
		// With nothing to weigh them by, the worse score goes first, and a tie
		// falls back to the id so the list is stable between runs — a panel
		// that reshuffles on every audit is one an operator cannot compare.
		leftScore, rightScore := scoreOf(findings[left]), scoreOf(findings[right])
		if leftScore != rightScore {
			return leftScore < rightScore
		}
		return findings[left].ID < findings[right].ID
	})
	if len(findings) > MaxOpportunities {
		findings = findings[:MaxOpportunities]
	}
	return findings
}

func trackedMetric(id string) bool {
	for _, tracked := range TrackedMetrics {
		if tracked.ID == id {
			return true
		}
	}
	return false
}

func scoreOf(finding Finding) float64 {
	if finding.Score == nil {
		return 0
	}
	return *finding.Score
}

// percent turns Lighthouse's 0..1 score into the 0..100 everyone quotes, and
// keeps "not measured" distinct from zero.
func percent(score *float64) *int {
	if score == nil {
		return nil
	}
	value := int(math.Round(*score * 100))
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return &value
}
