package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// fillPDF returns a PDF whose pages are w by h points, each filled with one
// RGB colour (components 0 to 1), one page per colour.
func fillPDF(w, h float64, colours ...[3]float64) []byte {
	n := len(colours)
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>"}
	kids := make([]string, n)
	for i := range n {
		kids[i] = fmt.Sprintf("%d 0 R", 3+2*i)
	}
	objects = append(objects, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	for i, c := range colours {
		content := fmt.Sprintf("%g %g %g rg 0 0 %g %g re f", c[0], c[1], c[2], w, h)
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %g %g] /Contents %d 0 R >>", w, h, 4+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects))
	for i, obj := range objects {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

var (
	red   = [3]float64{1, 0, 0}
	blue  = [3]float64{0, 0, 1}
	white = [3]float64{1, 1, 1}
)

// colourStub converts a source whose text names colours ("red blue") to a
// page per colour of the given size, and fails on "fail".
func colourStub(w, h float64) htmlConverter {
	return func(_ context.Context, src []byte, _ htmlSettings) ([]byte, error) {
		var colours [][3]float64
		for _, word := range strings.Fields(string(src)) {
			switch word {
			case "red":
				colours = append(colours, red)
			case "blue":
				colours = append(colours, blue)
			case "white":
				colours = append(colours, white)
			case "fail":
				return nil, errors.New("stub conversion failure")
			}
		}
		return fillPDF(w, h, colours...), nil
	}
}

// unsupportedStub stands in for the converter before it lands: every call
// fails as pdf.Convert does for a format it does not read.
func unsupportedStub(context.Context, []byte, htmlSettings) ([]byte, error) {
	return nil, fmt.Errorf("%w: %q", pdf.ErrUnsupportedFormat, pdf.FormatHTML)
}

// writeFiles writes name to content pairs under dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReftestRef_RuleWidensByFuzzy(t *testing.T) {
	t.Parallel()

	if got := (reftestRef{}).rule(); got != renderPixelRule {
		t.Errorf("no fuzzy: %+v, want the render rule", got)
	}
	got := reftestRef{Fuzzy: []int{0, 100, 0, 500}}.rule()
	if got.Tolerance != 100 || got.MaxDiffPixels != 500 || got.Neighbourhood != renderNeighbourhood {
		t.Errorf("fuzzy 0-100;0-500: %+v", got)
	}
	// A fuzziness narrower than the render rule's tolerance keeps it.
	if got := (reftestRef{Fuzzy: []int{0, 2, 0, 10}}).rule(); got.Tolerance != renderChannelTolerance {
		t.Errorf("fuzzy 0-2: tolerance %d", got.Tolerance)
	}
}

func TestComparePageSets_CountsAndPages(t *testing.T) {
	t.Parallel()

	r, b := solid(8, 8, color.RGBA{0xff, 0, 0, 0xff}), solid(8, 8, color.RGBA{0, 0, 0xff, 0xff})
	tests := []struct {
		name      string
		test, ref []image.Image
		same      bool
	}{
		{"one equal page", []image.Image{r}, []image.Image{r}, true},
		{"one differing page", []image.Image{r}, []image.Image{b}, false},
		{"page counts differ", []image.Image{r, r}, []image.Image{r}, false},
		{"second page differs", []image.Image{r, r}, []image.Image{r, b}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if same, fail := comparePageSets(tc.test, tc.ref, renderPixelRule); same != tc.same {
				t.Errorf("same = %v, want %v (%s)", same, tc.same, fail.detail)
			}
		})
	}
}

// newTestReftest returns a reftest feature over a temporary WPT tree holding
// files, with tests as reftests.json would list them.
func newTestReftest(t *testing.T, conv htmlConverter, files map[string]string, tests ...*reftestEntry) (*reftestFeature, string) {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, files)
	f := newReftestFeature()
	f.convert = conv
	for _, e := range tests {
		f.tests[e.ID] = e
	}
	return f, root
}

func TestReftestFeature_Outcomes(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"a/red.html":  "red",
		"a/red2.html": "red",
		"a/blue.html": "blue",
		"a/fail.html": "fail",
		"a/two.html":  "red red",
	}
	match := func(id, ref string) *reftestEntry {
		return &reftestEntry{ID: id, Refs: []reftestRef{{Rel: "match", Path: ref}}}
	}
	mismatch := func(id, ref string) *reftestEntry {
		return &reftestEntry{ID: id, Refs: []reftestRef{{Rel: "mismatch", Path: ref}}}
	}
	tests := []struct {
		name  string
		entry *reftestEntry
		want  string
	}{
		{"match ref equal", match("a/red.html", "a/red2.html"), outcomePass},
		{"match ref differs", match("a/red.html", "a/blue.html"), outcomeFail},
		{"mismatch ref differs", mismatch("a/red.html", "a/blue.html"), outcomePass},
		{"mismatch ref equal", mismatch("a/red.html", "a/red2.html"), outcomeFail},
		{"one of two match refs", &reftestEntry{ID: "a/red.html", Refs: []reftestRef{{Rel: "match", Path: "a/blue.html"}, {Rel: "match", Path: "a/red2.html"}}}, outcomePass},
		{"test fails to convert", match("a/fail.html", "a/red.html"), outcomeError},
		{"reference fails to convert", match("a/red.html", "a/fail.html"), outcomeError},
		{"print pages compared", &reftestEntry{ID: "a/two.html", Print: true, Refs: []reftestRef{{Rel: "match", Path: "a/red.html"}}}, outcomeFail},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, root := newTestReftest(t, colourStub(60, 45), files, tc.entry)
			reports := t.TempDir()
			f.SetReportDir(reports)
			out, err := f.Run(t.Context(), filepath.Join(root, filepath.FromSlash(tc.entry.ID)), tc.entry.ID)
			if err != nil {
				t.Fatal(err)
			}
			v := f.Compare(tc.entry.ID, out)
			if v.Outcome != tc.want {
				t.Fatalf("outcome %q (%s), want %q", v.Outcome, v.Reason, tc.want)
			}
			if v.Match != (tc.want == outcomePass) {
				t.Errorf("Match = %v for outcome %s", v.Match, v.Outcome)
			}
			if tc.want == outcomeFail && tc.entry.Refs[0].Rel == "match" && !tc.entry.Print {
				if _, err := os.Stat(filepath.Join(reports, reportName(tc.entry.ID)+".png")); err != nil {
					t.Errorf("no failure report: %v", err)
				}
			}
		})
	}
}

func TestDrawFeature_ComparesStackedPagesWithTheGrid(t *testing.T) {
	t.Parallel()

	pixels := map[string]*[3]int{"R": {255, 0, 0}, "B": {0, 0, 255}, "_": {255, 255, 255}, "z": nil}
	grid := func(rows ...string) []string { return rows }
	tests := []struct {
		name     string
		html     string
		expected []string
		want     string
	}{
		{"one red page", "red", grid("RRRR", "RRRR"), outcomePass},
		{"wrong colour", "blue", grid("RRRR", "RRRR"), outcomeFail},
		{"unchecked pixels", "blue", grid("zzzz", "zzzz"), outcomePass},
		{"two pages stacked", "red blue", grid("RRRR", "RRRR", "BBBB", "BBBB"), outcomePass},
		{"wrong size", "red", grid("RRR", "RRR"), outcomeFail},
		{"conversion fails", "fail", grid("RRRR", "RRRR"), outcomeError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDrawFeature()
			// 4 by 2 pixels at 96 dpi are 3 by 1.5 points.
			f.convert = colourStub(3, 1.5)
			f.fixtures["t"] = &drawFixture{Name: "t", HTML: tc.html, Expected: tc.expected, Pixels: pixels}
			f.resources = t.TempDir()
			reports := t.TempDir()
			f.SetReportDir(reports)
			out, err := f.Run(t.Context(), "", "t")
			if err != nil {
				t.Fatal(err)
			}
			v := f.Compare("t", out)
			if v.Outcome != tc.want {
				t.Fatalf("outcome %q (%s), want %q", v.Outcome, v.Reason, tc.want)
			}
			if tc.want == outcomeFail {
				if _, err := os.Stat(filepath.Join(reports, "t.png")); err != nil {
					t.Errorf("no failure report: %v", err)
				}
			}
		})
	}
}

func TestTokenF1(t *testing.T) {
	t.Parallel()

	tests := []struct {
		golden, got string
		want        float64
	}{
		{"", "", 1},
		{"a b", "", 0},
		{"a b c d", "a b c d", 1},
		{"a  b\nc d", "a b c d", 1},
		{"a b c d", "a b", 2 * 1 * 0.5 / 1.5},
		{"a a b", "a b b", 2.0 / 3},
		{"ﬁne", "fine", 1},
		{"x y", "a b", 0},
	}
	for _, tc := range tests {
		if got := tokenF1(tc.golden, tc.got); fmt.Sprintf("%.6f", got) != fmt.Sprintf("%.6f", tc.want) {
			t.Errorf("tokenF1(%q, %q) = %.4f, want %.4f", tc.golden, tc.got, got, tc.want)
		}
	}
}

func TestLinkRecall(t *testing.T) {
	t.Parallel()

	golden := []diffLink{
		{Page: 1, URI: "https://gotenberg.dev/"},
		{Page: 1, DestPage: 2},
		{Page: 2, DestPage: 3},
		{Page: 2, DestPage: 3},
	}
	pages := []diffLithoPage{
		{links: []diffLink{{Page: 1, URI: "HTTPS://Gotenberg.dev"}, {Page: 1, DestPage: 2}}},
		{links: []diffLink{{Page: 2, DestPage: 3}, {Page: 2, URI: "https://example.com"}}},
	}
	if got := linkRecall(golden, pages); got != 0.75 {
		t.Errorf("recall = %v, want 0.75 (the duplicate dest link counts once per copy)", got)
	}
	if got := linkRecall(nil, pages); got != 1 {
		t.Errorf("no golden link: recall %v, want 1", got)
	}
}

func TestLayoutScore(t *testing.T) {
	t.Parallel()

	page := withSquare(200, 200, 40, 40, 40)
	blank := solid(200, 200, color.RGBA{0xff, 0xff, 0xff, 0xff})
	if got := layoutScore(page, page); got != 1 {
		t.Errorf("identical pages: %v", got)
	}
	if got := layoutScore(blank, page); got != 0 {
		t.Errorf("blank against a drawn page: %v", got)
	}
	if got := layoutScore(blank, blank); got != 1 {
		t.Errorf("two blank pages: %v", got)
	}
	for _, shift := range []int{1, 2, 4} {
		if got := layoutScore(withSquare(200, 200, 40+shift, 40+shift, 40), page); got < 0.9 {
			t.Errorf("a square moved %d pixels scores %v, want 0.9 or more", shift, got)
		}
	}
	if got := layoutScore(withSquare(200, 200, 140, 140, 40), page); got > 0.2 {
		t.Errorf("a square moved 100 pixels scores %v, want 0.2 or less", got)
	}
}

func TestPageCountScore(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		got, want int
		score     float64
	}{{3, 3, 1}, {2, 4, 0.5}, {4, 2, 0.5}, {0, 2, 0}} {
		if s := pageCountScore(tc.got, tc.want); s != tc.score {
			t.Errorf("pageCountScore(%d, %d) = %v, want %v", tc.got, tc.want, s, tc.score)
		}
	}
}

func TestScoreDiff_AgainstGoldens(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	page := withSquare(40, 40, 10, 10, 10)
	f, err := os.Create(filepath.Join(dir, "x.1.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, page); err != nil {
		t.Fatal(err)
	}
	f.Close()
	g := &diffGolden{
		text:   &pdfjsReference{NumPages: 1, Pages: []pdfjsRefPage{{PageNumber: 1, Text: "hello world"}}},
		render: &renderIndex{DPI: diffDPI, PageCount: 1, Pages: []renderIndexPage{{Page: 1, File: "x.1.png"}}},
		links:  &diffLinkGolden{NumPages: 1, Links: []diffLink{{Page: 1, URI: "https://a.example/"}}},
	}
	noReport := func(int, image.Image, image.Image, pixelDiff) {}

	same := scoreDiff(g, diffLithoDoc{pages: []diffLithoPage{{text: "hello  world", links: []diffLink{{Page: 1, URI: "https://a.example"}}, img: page}}}, dir, noReport)
	if same.Outcome != outcomePass || same.Scores[scoreLayout] != 1 || same.Scores[scoreStrict] != 1 || same.Scores[scoreText] != 1 {
		t.Errorf("identical conversion: %+v", same)
	}

	reported := 0
	extra := scoreDiff(g, diffLithoDoc{pages: []diffLithoPage{
		{text: "hello", img: solid(40, 40, color.RGBA{0xff, 0xff, 0xff, 0xff})},
		{text: "overflow", img: page},
	}}, dir, func(int, image.Image, image.Image, pixelDiff) { reported++ })
	if extra.Outcome != outcomeFail || extra.Scores[scorePages] != 0.5 || extra.Scores[scoreLinks] != 0 || extra.Scores[scoreLayout] != 0 {
		t.Errorf("blank page plus an extra one: %+v", extra.Scores)
	}
	if reported != 1 {
		t.Errorf("reported %d pages, want the one compared page that fails", reported)
	}

	// The page sizes and the outline: a landscape page against a portrait
	// one fails pages though the counts agree, and an outline item missing
	// fails outline_recall.
	shaped := *g
	shaped.sizes = []diffPageSize{{W: 595.92, H: 841.92}}
	shaped.outline = []diffOutlineItem{{Title: "Chapter 1", Page: 1}, {Title: "Section 1.1", Page: 1}}
	lithoPage := diffLithoPage{text: "hello world", links: []diffLink{{Page: 1, URI: "https://a.example/"}}, img: page}
	cases := []struct {
		name          string
		size          diffPageSize
		outline       []diffOutlineItem
		pages, recall float64
		outcome       string
	}{
		{"same size and outline", diffPageSize{W: 595.28, H: 841.89}, shaped.outline, 1, 1, outcomePass},
		{"landscape", diffPageSize{W: 841.92, H: 595.92}, shaped.outline, 0, 1, outcomeFail},
		{"one item missing", diffPageSize{W: 595.92, H: 841.92}, []diffOutlineItem{{Title: " Chapter\t1 ", Page: 1}}, 1, 0.5, outcomeFail},
		{"item on another page", diffPageSize{W: 595.92, H: 841.92}, []diffOutlineItem{{Title: "Chapter 1", Page: 2}, {Title: "Section 1.1", Page: 1}}, 1, 0.5, outcomeFail},
	}
	for _, tc := range cases {
		p := lithoPage
		p.size = tc.size
		got := scoreDiff(&shaped, diffLithoDoc{pages: []diffLithoPage{p}, outline: tc.outline}, dir, noReport)
		if got.Scores[scorePages] != tc.pages || got.Scores[scoreOutline] != tc.recall || got.Outcome != tc.outcome {
			t.Errorf("%s: pages %v, outline %v, %s (%s); want %v, %v, %s", tc.name, got.Scores[scorePages], got.Scores[scoreOutline], got.Outcome, got.Reason, tc.pages, tc.recall, tc.outcome)
		}
	}
}

func TestOutlineRecall(t *testing.T) {
	t.Parallel()

	golden := []diffOutlineItem{{Title: "A", Page: 1}, {Title: "A", Page: 1}, {Title: "B", Page: 2}}
	cases := []struct {
		name         string
		golden, have []diffOutlineItem
		want         float64
	}{
		{"neither has one", nil, nil, 1},
		{"only litho has one", nil, golden, 1},
		{"only Chromium has one", golden, nil, 0},
		{"the same", golden, golden, 1},
		{"a duplicate counted once", golden, []diffOutlineItem{{Title: "A", Page: 1}, {Title: "B", Page: 2}}, 2.0 / 3},
		{"NFKC and white space", []diffOutlineItem{{Title: "ﬁle  one", Page: 1}}, []diffOutlineItem{{Title: "file one", Page: 1}}, 1},
	}
	for _, tc := range cases {
		if got := outlineRecall(tc.golden, tc.have); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestConverterFeatures_ReportErrorsWithoutTheConverter runs the three
// converter features end to end, through the runner and the baseline, with
// a converter that refuses every source as pdf.Convert refuses HTML before
// the converter lands: every fixture is an error, recorded as such, and none
// crashes the run.
func TestConverterFeatures_ReportErrorsWithoutTheConverter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"wpt/a/test.html":   "<p>test</p>",
		"wpt/a/ref.html":    "<p>ref</p>",
		"html/x/index.html": "<p>x</p>",
	})

	reftest := newReftestFeature()
	reftest.convert = unsupportedStub
	reftest.tests["a/test.html"] = &reftestEntry{ID: "a/test.html", Refs: []reftestRef{{Rel: "match", Path: "a/ref.html"}}}

	draw := newDrawFeature()
	draw.convert = unsupportedStub
	draw.fixtures["d"] = &drawFixture{Name: "d", HTML: "<p>", Expected: []string{"_"}, Pixels: map[string]*[3]int{"_": {255, 255, 255}}}
	draw.resources = dir

	diff := newDiffFeature()
	diff.convert = unsupportedStub
	diff.goldens["x"] = &diffGolden{
		text:   &pdfjsReference{NumPages: 1},
		render: &renderIndex{DPI: diffDPI},
		links:  &diffLinkGolden{},
	}

	cases := []struct {
		feat    Feature
		fixture Fixture
	}{
		{reftest, Fixture{ID: "a/test.html", PDFPath: filepath.Join(dir, "wpt", "a", "test.html")}},
		{draw, Fixture{ID: "d", PDFPath: filepath.Join(dir, "d.json")}},
		{diff, Fixture{ID: "x", PDFPath: filepath.Join(dir, "html", "x", "index.html")}},
	}
	board := &Scoreboard{}
	report := &CorpusReport{Name: "smoke"}
	for _, c := range cases {
		fr := &FeatureReport{Name: c.feat.Name()}
		if _, ok := c.feat.(OutcomeFeature); ok {
			fr.RecordsOutcomes = true
		}
		if sf, ok := c.feat.(ScoreFeature); ok {
			fr.ScoreTolerance = sf.ScoreTolerance()
		}
		runFeatureAcrossFixtures(t.Context(), c.feat, []Fixture{c.fixture}, 2, fr)
		if fr.Matched != 0 || fr.Mismatched != 1 || fr.Metrics[metricConvertError] != 1 {
			t.Errorf("%s: matched %d, mismatched %d, metrics %v", fr.Name, fr.Matched, fr.Mismatched, fr.Metrics)
		}
		for _, v := range fr.Verdicts {
			if v.Outcome != outcomeError || !strings.Contains(v.Reason, "unsupported") {
				t.Errorf("%s: verdict %+v, want an unsupported-format error", fr.Name, v)
			}
		}
		report.Features = append(report.Features, fr)
	}
	board.Corpora = append(board.Corpora, report)

	bl := baselineFromScoreboard(board)
	cells := bl.Corpora["smoke"].Features
	if got := cells["reftest"].Outcomes["a/test.html"]; got != outcomeError {
		t.Errorf("reftest baseline outcome %q", got)
	}
	if got := cells["draw"].Outcomes["d"]; got != outcomeError {
		t.Errorf("draw baseline outcome %q", got)
	}
	if got := cells["diff"].Scores["x"]; len(got) != 6 || got[scoreLayout] != 0 {
		t.Errorf("diff baseline scores %v", got)
	}
	if cells["diff"].ScoreTolerance != diffScoreTolerance {
		t.Errorf("diff baseline tolerance %v", cells["diff"].ScoreTolerance)
	}
	data, err := json.Marshal(bl)
	if err != nil || !bytes.Contains(data, []byte(`"outcomes"`)) {
		t.Errorf("baseline JSON lacks the outcomes: %v", err)
	}
}
