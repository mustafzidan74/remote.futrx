package lighthouse

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// realReport is a Lighthouse 13 report from an actual run, trimmed to the
// fields this package reads. It is here so a Lighthouse upgrade that moves
// something shows up as a failing test rather than as an empty panel.
func realReport(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/report.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseReadsARealLighthouseReport(t *testing.T) {
	report, err := Parse("/", realReport(t), 1_780_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Measured() {
		t.Fatalf("a good report was not treated as measured: %+v", report)
	}
	if report.Version == "" {
		t.Fatal("the Lighthouse version was not carried through")
	}

	for name, score := range map[string]*int{
		"performance":    report.Performance,
		"accessibility":  report.Accessibility,
		"best practices": report.BestPractices,
		"seo":            report.SEO,
	} {
		if score == nil {
			t.Fatalf("%s has no score", name)
		}
		if *score < 0 || *score > 100 {
			t.Fatalf("%s score %d is outside 0-100", name, *score)
		}
	}

	// Every tracked metric that the page actually has must come through with
	// Lighthouse's own rendering: the platform never restates a number in its
	// own words and risks disagreeing with the tool.
	byID := map[string]Metric{}
	for _, metric := range report.Metrics {
		byID[metric.ID] = metric
	}
	for _, required := range []string{
		"largest-contentful-paint",
		"cumulative-layout-shift",
		"total-blocking-time",
		"first-contentful-paint",
	} {
		metric, ok := byID[required]
		if !ok {
			t.Fatalf("%s is missing from the parsed metrics", required)
		}
		if metric.Label == "" || metric.Display == "" {
			t.Fatalf("%s came through unlabelled: %+v", required, metric)
		}
	}
}

func TestOpportunitiesAreTheFailuresWorthActingOn(t *testing.T) {
	report, err := Parse("/", realReport(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Opportunities) == 0 {
		t.Fatal("a report with failing audits produced no opportunities")
	}
	if len(report.Opportunities) > MaxOpportunities {
		t.Fatalf("the list was not capped: %d entries", len(report.Opportunities))
	}
	for _, finding := range report.Opportunities {
		if finding.Title == "" {
			t.Fatalf("a finding has no title: %+v", finding)
		}
		if finding.Score != nil && *finding.Score >= 0.9 {
			t.Fatalf("a passing audit was reported as an opportunity: %+v", finding)
		}
		// The metrics have their own section; listing them twice would tell an
		// operator to go fix "Largest Contentful Paint".
		if trackedMetric(finding.ID) {
			t.Fatalf("a tracked metric was repeated as a finding: %s", finding.ID)
		}
	}
}

// The same report must parse to the same list twice, or a weekly panel is
// impossible to read against last week's.
func TestOpportunityOrderIsStable(t *testing.T) {
	data := realReport(t)
	first, err := Parse("/", data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		next, err := Parse("/", data, 0)
		if err != nil {
			t.Fatal(err)
		}
		for index := range first.Opportunities {
			if first.Opportunities[index].ID != next.Opportunities[index].ID {
				t.Fatalf("order moved between parses at %d: %s then %s",
					index, first.Opportunities[index].ID, next.Opportunities[index].ID)
			}
		}
	}
}

// Lighthouse exits zero on a page it could not load and hands back a report
// full of nulls. Stored as-is that reads as a page which scored nothing.
func TestARuntimeErrorIsAFailedPageNotAZeroScore(t *testing.T) {
	payload := []byte(`{
		"lighthouseVersion":"13.4.1",
		"runtimeError":{"code":"ERRORED_DOCUMENT_REQUEST","message":"the page did not load"},
		"categories":{"performance":{"score":null}},
		"audits":{}
	}`)
	report, err := Parse("/broken", payload, 42)
	if err != nil {
		t.Fatal(err)
	}
	if report.Error != "the page did not load" {
		t.Fatalf("the runtime error was not surfaced: %+v", report)
	}
	if report.Measured() {
		t.Fatal("a page that never loaded was reported as measured")
	}
	if report.Performance != nil {
		t.Fatalf("a failed page carries a score: %v", *report.Performance)
	}
}

// A category Lighthouse could not score must stay absent. Rendered as 0 it
// would tell the operator their page failed completely.
func TestAnUnscoredCategoryStaysAbsentRatherThanZero(t *testing.T) {
	payload := []byte(`{
		"categories":{"performance":{"score":0.42},"seo":{"score":null}},
		"audits":{}
	}`)
	report, err := Parse("/", payload, 0)
	if err != nil {
		t.Fatal(err)
	}
	if report.Performance == nil || *report.Performance != 42 {
		t.Fatalf("performance did not round to 42: %v", report.Performance)
	}
	if report.SEO != nil {
		t.Fatalf("an unscored category became %v", *report.SEO)
	}
}

func TestParseRefusesSomethingThatIsNotAReport(t *testing.T) {
	for _, payload := range [][]byte{
		nil,
		[]byte(""),
		[]byte("lighthouse: command not found"),
		[]byte(`{"categories":{}}`),
	} {
		if _, err := Parse("/", payload, 0); !errors.Is(err, ErrBadReport) {
			t.Fatalf("expected ErrBadReport for %q, got %v", string(payload), err)
		}
	}
}

func TestNormalizeRejectsWhatTheContainerShouldNeverBeAsked(t *testing.T) {
	tests := []struct {
		name string
		in   RunInput
		want error
	}{
		{"no port", RunInput{Paths: []string{"/"}}, ErrInvalidPort},
		{"no paths", RunInput{Port: 3000}, ErrNoPaths},
		{"relative path", RunInput{Port: 3000, Paths: []string{"about"}}, ErrInvalidPath},
		{"traversal", RunInput{Port: 3000, Paths: []string{"/../etc"}}, ErrInvalidPath},
		{"shell characters", RunInput{Port: 3000, Paths: []string{"/a b"}}, ErrInvalidPath},
		{"too many pages", RunInput{Port: 3000, Paths: []string{"/a", "/b", "/c", "/d", "/e", "/f", "/g"}}, ErrTooManyPaths},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.in.Normalize(); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestNormalizeDefaultsToMobileAndCollapsesDuplicates(t *testing.T) {
	out, err := RunInput{Port: 3000, Paths: []string{"/", " / ", "/pricing"}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Paths) != 2 {
		t.Fatalf("duplicates survived: %#v", out.Paths)
	}
	// Mobile is what Google ranks on, so it is what an unspecified run measures.
	if out.FormFactor != string(FormFactorMobile) {
		t.Fatalf("expected mobile by default, got %q", out.FormFactor)
	}
	desktop, _ := RunInput{Port: 3000, Paths: []string{"/"}, FormFactor: "DESKTOP"}.Normalize()
	if desktop.FormFactor != string(FormFactorDesktop) {
		t.Fatalf("desktop was not recognised: %q", desktop.FormFactor)
	}
	// A typo does not lose the operator their audit.
	odd, _ := RunInput{Port: 3000, Paths: []string{"/"}, FormFactor: "tablet"}.Normalize()
	if odd.FormFactor != string(FormFactorMobile) {
		t.Fatalf("an unknown form factor did not fall back to mobile: %q", odd.FormFactor)
	}
}

// The stored summary is what makes keeping twenty runs affordable, so its size
// is a property worth asserting rather than assuming.
func TestTheStoredSummaryIsSmall(t *testing.T) {
	raw := realReport(t)
	report, err := Parse("/", raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) > 8<<10 {
		t.Fatalf("one page's summary is %d bytes; twenty runs of six pages would not be cheap", len(stored))
	}
}
