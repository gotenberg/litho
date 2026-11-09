package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// The page a screen reftest is converted on: 800 by 600 CSS pixels, the
// viewport WPT takes reftest screenshots in, which at 96 dpi is 600 by 450
// points, with no margin, so the page is the viewport.
const (
	reftestScreenWidth  = 600
	reftestScreenHeight = 450
)

// The page a print reftest is converted on: 12.7 by 7.62 cm (5 by 3 inches)
// with a 12.7 mm (half inch) margin on every side, the defaults WPT's
// print-reftests document sets for them.
const (
	reftestPrintWidth  = 360
	reftestPrintHeight = 216
	reftestPrintMargin = 36
)

// reftestDPI is the resolution reftests are drawn at: 96 dpi makes one CSS
// pixel one device pixel, the scale WPT compares screenshots at.
const reftestDPI = 96

// reftestFontsTarget is the make target that fetches what the reftest
// feature reads.
const reftestFontsTarget = "make corpus-fetch CORPUS=wpt"

// metricConvertError counts, in a converter cell, the fixtures litho failed
// to convert or to read back: an error before there was anything to compare.
// A cost metric.
const metricConvertError = "convert_error"

// reftestEntry is one test of corpus/wpt/reftests.json, written by
// `corpus update --corpus=wpt` (select_tests.go).
type reftestEntry struct {
	ID        string       `json:"id"`
	Print     bool         `json:"print"`
	Resources []string     `json:"resources"`
	Refs      []reftestRef `json:"refs"`
	// Pages is WPT's <meta name="reftest-pages"> of a print reftest: the
	// test's pages compared, as "2" or "1,3-5". Empty compares every page.
	Pages string `json:"pages,omitempty"`
}

// reftestRef is one reference of a test: the file, whether the test must
// match it or differ from it, and the fuzziness WPT allows for it (see
// [reftestRef.rule]).
type reftestRef struct {
	Rel       string   `json:"rel"`
	Path      string   `json:"path"`
	Resources []string `json:"resources"`
	// Fuzzy is WPT's <meta name="fuzzy"> for this reference: the lowest and
	// highest maximum channel difference, then the lowest and highest count
	// of differing pixels. Empty when the test declares none.
	Fuzzy []int `json:"fuzzy,omitempty"`
}

// rule returns the render cell's pixel rule widened by the reference's
// fuzziness: a pixel counts as equal up to the larger of the two channel
// tolerances, and the page matches with up to the larger of 1% of its pixels
// and the fuzzy pixel count differing. WPT's own rule passes a comparison
// whose maximum difference and pixel count fall in the fuzzy ranges; this
// rule is never stricter than that one's upper bounds, and keeps the render
// cell's tolerance of anti-aliasing for the tests that declare none.
func (r reftestRef) rule() pixelRule {
	rule := renderPixelRule
	if len(r.Fuzzy) == 4 {
		rule.Tolerance = max(rule.Tolerance, r.Fuzzy[1])
		rule.MaxDiffPixels = max(rule.MaxDiffPixels, r.Fuzzy[3])
	}
	return rule
}

// reftestFeature scores litho's HTML converter on web-platform-tests
// reftests. Each test and each of its references is converted to PDF,
// drawn by litho at 96 dpi, and the drawings compared under the render
// cell's pixel rule widened by the test's fuzziness (see
// [reftestRef.rule]). A screen reftest is converted for the screen media on
// an 800 by 600 pixel page with no margin, and its first page compared; a
// print reftest for print on WPT's 5 by 3 inch page, every page compared, a
// differing page count failing it.
//
// A test passes when it matches one of its match references, if it has any,
// and differs from every mismatch reference (WPT's multiple-reference rule).
// A conversion or drawing failure is an error, not a failure. Each test's
// outcome lands in the baseline, so a pass that turns into anything else is
// a regression whatever the cell's count does.
//
// Each document is converted with the file system rooted at its own
// directory, the one its relative URLs resolve in (the selector keeps
// only tests whose resources lie under it), and with the fixture fonts plus
// Ahem, the font most reftests draw with.
type reftestFeature struct {
	convert htmlConverter

	mu        sync.RWMutex
	tests     map[string]*reftestEntry
	fontDirs  []string
	reportDir string
}

func newReftestFeature() *reftestFeature {
	return &reftestFeature{convert: convertHTML, tests: make(map[string]*reftestEntry)}
}

func (f *reftestFeature) Name() string         { return "reftest" }
func (f *reftestFeature) GoldenSource() string { return "wpt" }

// RecordsOutcomes records each test's outcome in the baseline.
func (f *reftestFeature) RecordsOutcomes() {}

// SetReportDir sets where failing tests are drawn.
func (f *reftestFeature) SetReportDir(dir string) {
	f.mu.Lock()
	f.reportDir = dir
	f.mu.Unlock()
}

// LoadGoldens reads reftests.json from dir, the corpus source directory, and
// resolves the fonts the tests are converted with.
func (f *reftestFeature) LoadGoldens(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "reftests.json")) //nolint:gosec // committed corpus list
	if err != nil {
		return ErrNoGoldens
	}
	var entries []*reftestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("decode reftests.json: %w", err)
	}
	tests := make(map[string]*reftestEntry, len(entries))
	for _, e := range entries {
		if !validInputID(e.ID) || len(e.Refs) == 0 {
			return fmt.Errorf("reftests.json: entry %q has no valid id or no reference", e.ID)
		}
		if _, err := parseReftestPages(e.Pages); err != nil {
			return fmt.Errorf("reftests.json: %s: %w", e.ID, err)
		}
		for _, r := range e.Refs {
			if !validInputID(r.Path) || (r.Rel != "match" && r.Rel != "mismatch") || (len(r.Fuzzy) != 0 && len(r.Fuzzy) != 4) {
				return fmt.Errorf("reftests.json: %s has an invalid reference %+v", e.ID, r)
			}
		}
		tests[e.ID] = e
	}
	if len(tests) == 0 {
		return ErrNoGoldens
	}
	fonts, err := fixtureFontDirs(fixtureFontsRoot(), reftestFontsTarget, "ahem")
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.tests, f.fontDirs = tests, fonts
	f.mu.Unlock()
	return nil
}

func (f *reftestFeature) HasGolden(fixtureID string) bool { return f.test(fixtureID) != nil }

func (f *reftestFeature) test(id string) *reftestEntry {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.tests[id]
}

// reftestOutput is Run's output: the outcome and, per reference, how the
// comparison went.
type reftestOutput struct {
	Outcome string            `json:"outcome"`
	Reason  string            `json:"reason,omitempty"`
	Refs    []reftestRefCheck `json:"refs,omitempty"`
}

// reftestRefCheck is the comparison of a test with one reference.
type reftestRefCheck struct {
	Path string `json:"path"`
	Rel  string `json:"rel"`
	// Same reports whether the drawings matched under the reference's rule.
	Same   bool   `json:"same"`
	Detail string `json:"detail"`
}

// Run converts and draws the test and its references and compares them.
// srcPath is the test file under the fetched WPT tree, whose root is the
// path with the test ID removed.
func (f *reftestFeature) Run(ctx context.Context, srcPath, fixtureID string) ([]byte, error) {
	t := f.test(fixtureID)
	if t == nil {
		return json.Marshal(reftestOutput{Outcome: outcomeError, Reason: "not in reftests.json"})
	}
	root := strings.TrimSuffix(filepath.ToSlash(srcPath), t.ID)
	testPages, err := f.draw(ctx, root, t.ID, t.Print)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return json.Marshal(reftestOutput{Outcome: outcomeError, Reason: "test: " + err.Error()})
	}
	if t.Print && t.Pages != "" {
		ranges, err := parseReftestPages(t.Pages)
		if err != nil {
			return json.Marshal(reftestOutput{Outcome: outcomeError, Reason: "test: " + err.Error()})
		}
		testPages = selectReftestPages(testPages, ranges)
	}
	out := reftestOutput{}
	hasMatch, matched, mismatchOK := false, false, true
	var failure *reftestFailure
	for _, ref := range t.Refs {
		refPages, err := f.draw(ctx, root, ref.Path, t.Print)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return json.Marshal(reftestOutput{Outcome: outcomeError, Reason: "reference " + ref.Path + ": " + err.Error()})
		}
		same, worst := comparePageSets(testPages, refPages, ref.rule())
		out.Refs = append(out.Refs, reftestRefCheck{Path: ref.Path, Rel: ref.Rel, Same: same, Detail: worst.detail})
		switch ref.Rel {
		case "match":
			hasMatch = true
			matched = matched || same
			if !same && failure == nil {
				failure = &worst
			}
		case "mismatch":
			if same {
				mismatchOK = false
				failure = &worst
			}
		}
	}
	pass := (!hasMatch || matched) && mismatchOK
	if pass {
		out.Outcome = outcomePass
		return json.Marshal(out)
	}
	out.Outcome = outcomeFail
	out.Reason = "differs from its match reference"
	if !mismatchOK {
		out.Reason = "matches its mismatch reference"
	}
	if failure != nil {
		out.Reason += ": " + failure.detail
		f.report(fixtureID, failure)
	}
	return json.Marshal(out)
}

// maxReftestPage bounds a page number of a reftest-pages range. Anti-DoS
// guard, not spec-mandated: a print reftest runs to a few pages, and
// renderPDF draws at most maxConvertedPages.
const maxReftestPage = maxConvertedPages

// reftestPageRange is one range of a reftest-pages value, first to last
// page inclusive, one-based.
type reftestPageRange struct{ first, last int }

// parseReftestPages parses WPT's reftest-pages syntax: ranges separated by
// commas, each "N", "N-M", "N-" (to the last page) or "-M" (from the first),
// with white space allowed around each number. Empty selects every page.
// A page number below 1, a range whose start exceeds its end, and anything
// else is an error.
func parseReftestPages(spec string) ([]reftestPageRange, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	var out []reftestPageRange
	for part := range strings.SplitSeq(spec, ",") {
		lo, hi, isRange := strings.Cut(part, "-")
		num := func(s string, def int) (int, error) {
			s = strings.TrimSpace(s)
			if s == "" {
				return def, nil
			}
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > maxReftestPage {
				return 0, fmt.Errorf("reftest-pages %q: page %q is not a number in [1, %d]", spec, s, maxReftestPage)
			}
			return n, nil
		}
		first, err := num(lo, 1)
		if err != nil {
			return nil, err
		}
		last := first
		if isRange {
			if last, err = num(hi, maxReftestPage); err != nil {
				return nil, err
			}
		} else if strings.TrimSpace(lo) == "" {
			return nil, fmt.Errorf("reftest-pages %q: empty range", spec)
		}
		if first > last {
			return nil, fmt.Errorf("reftest-pages %q: range %q starts after it ends", spec, part)
		}
		out = append(out, reftestPageRange{first, last})
	}
	return out, nil
}

// selectReftestPages keeps the pages the ranges name, in page order, each
// once, as WPT's runner does; a page past the last is ignored. No range
// keeps every page.
func selectReftestPages(pages []image.Image, ranges []reftestPageRange) []image.Image {
	if len(ranges) == 0 {
		return pages
	}
	var out []image.Image
	for i, p := range pages {
		n := i + 1
		for _, r := range ranges {
			if n >= r.first && n <= r.last {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// reftestFailure is the page pair a failing comparison is drawn from.
type reftestFailure struct {
	test, ref image.Image
	diff      pixelDiff
	detail    string
}

// report draws a failure into the report directory, when one is set. A
// report that cannot be written is said on stderr: it is a reading aid, not
// a result.
func (f *reftestFeature) report(id string, fail *reftestFailure) {
	f.mu.RLock()
	dir := f.reportDir
	f.mu.RUnlock()
	if dir == "" || fail.test == nil || fail.ref == nil {
		return
	}
	if err := writeDiffReport(filepath.Join(dir, reportName(id)+".png"), fail.test, fail.ref, fail.diff, 1); err != nil {
		fmt.Fprintf(os.Stderr, "reftest: report for %s: %v\n", id, err)
	}
}

// draw converts the document at rel under root and draws its pages: the
// first for a screen reftest, every one for a print reftest.
func (f *reftestFeature) draw(ctx context.Context, root, rel string, paged bool) ([]image.Image, error) {
	file := filepath.Join(filepath.FromSlash(root), filepath.FromSlash(rel))
	src, err := os.ReadFile(file) //nolint:gosec // file of the fetched WPT tree, listed in reftests.json
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	f.mu.RLock()
	fonts := f.fontDirs
	f.mu.RUnlock()
	s := htmlSettings{
		Media:     "screen",
		Width:     reftestScreenWidth,
		Height:    reftestScreenHeight,
		Top:       0,
		Right:     0,
		Bottom:    0,
		Left:      0,
		Resources: os.DirFS(filepath.Join(filepath.FromSlash(root), filepath.FromSlash(path.Dir(rel)))),
		FontDirs:  fonts,
	}
	maxPages := 1
	if paged {
		s.Media = "print"
		s.Width, s.Height = reftestPrintWidth, reftestPrintHeight
		s.Top, s.Right, s.Bottom, s.Left = reftestPrintMargin, reftestPrintMargin, reftestPrintMargin, reftestPrintMargin
		maxPages = 0
	}
	data, err := f.convert(ctx, src, s)
	if err != nil {
		return nil, fmt.Errorf("convert: %w", err)
	}
	pages, err := renderPDF(ctx, data, reftestDPI, maxPages)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("the conversion has no page")
	}
	return pages, nil
}

// comparePageSets reports whether two drawings match under rule: the same
// number of pages, each pair within the rule. It returns the first page pair
// that does not match, or the last compared when all do, to draw.
func comparePageSets(test, ref []image.Image, rule pixelRule) (bool, reftestFailure) {
	var last reftestFailure
	if len(test) != len(ref) {
		last = reftestFailure{detail: fmt.Sprintf("%d pages against %d", len(test), len(ref))}
		if len(test) > 0 && len(ref) > 0 {
			last.test, last.ref = test[0], ref[0]
			last.diff = comparePixels(test[0], ref[0], rule, 0)
		}
		return false, last
	}
	for i := range test {
		d := comparePixels(test[i], ref[i], rule, 0)
		last = reftestFailure{test: test[i], ref: ref[i], diff: d, detail: fmt.Sprintf("page %d: %s", i+1, d)}
		if !d.within(rule) {
			return false, last
		}
	}
	return true, last
}

// Compare turns Run's outcome into a verdict.
func (f *reftestFeature) Compare(fixtureID string, output []byte) Verdict {
	return outcomeVerdict(fixtureID, output)
}

// outcomeVerdict decodes an output carrying an outcome and a reason, the
// shape the reftest and draw features share, into a verdict: a pass
// matches, and an error counts in convert_error.
func outcomeVerdict(fixtureID string, output []byte) Verdict {
	var got struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{
			FixtureID: fixtureID, Outcome: outcomeError, Reason: fmt.Sprintf("decode output: %v", err),
			Metrics: map[string]int{metricConvertError: 1},
		}
	}
	v := Verdict{FixtureID: fixtureID, Outcome: got.Outcome, Reason: got.Reason, Metrics: map[string]int{metricConvertError: 0}}
	switch got.Outcome {
	case outcomePass:
		v.Match, v.Score = true, 1
	case outcomeFail:
	default:
		v.Outcome = outcomeError
		v.Metrics[metricConvertError] = 1
	}
	return v
}
