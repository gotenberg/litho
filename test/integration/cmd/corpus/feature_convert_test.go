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
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/test/integration/fixture"
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
		for word := range strings.FieldsSeq(string(src)) {
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
// fails as a format litho does not read fails.
func unsupportedStub(context.Context, []byte, htmlSettings) ([]byte, error) {
	return nil, fmt.Errorf("%w: %s", fixture.ErrUnsupportedFormat, fixture.FormatHTML)
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

func TestReftestRef_EqualFollowsWPTFuzzyRule(t *testing.T) {
	t.Parallel()

	diff := func(pixels, delta int) pixelDiff { return pixelDiff{DiffPixels: pixels, MaxDelta: delta} }
	tests := []struct {
		name  string
		fuzzy []int
		d     pixelDiff
		want  bool
	}{
		{"no fuzzy, identical", nil, diff(0, 0), true},
		{"no fuzzy, noise", nil, diff(reftestNoisePixels, reftestNoiseDelta), true},
		{"no fuzzy, one level past the noise", nil, diff(1, reftestNoiseDelta+1), false},
		{"no fuzzy, one pixel past the noise", nil, diff(reftestNoisePixels+1, 1), false},
		{"no fuzzy, a small glyph", nil, diff(20, 255), false},
		{"fuzzy 0;0 is none", []int{0, 0, 0, 0}, diff(20, 255), false},
		{"inside both ranges", []int{0, 100, 0, 500}, diff(500, 100), true},
		{"past the pixel range", []int{0, 100, 0, 500}, diff(501, 100), false},
		{"past the difference range", []int{0, 100, 0, 500}, diff(20, 101), false},
		{"below a difference lower bound", []int{50, 100, 0, 500}, diff(400, 20), false},
		{"below a pixel lower bound", []int{0, 100, 450, 500}, diff(400, 20), false},
		{"nothing differs by a range from 0", []int{20, 100, 0, 500}, diff(0, 0), true},
		{"size mismatch", []int{0, 255, 0, 1 << 30}, pixelDiff{SizeMismatch: true}, false},
	}
	for _, tc := range tests {
		if got := (reftestRef{Fuzzy: tc.fuzzy}).equal(tc.d); got != tc.want {
			t.Errorf("%s: equal = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestComparePageSets_CountsAndPages(t *testing.T) {
	t.Parallel()

	r, b := solid(8, 8, color.RGBA{0xff, 0, 0, 0xff}), solid(8, 8, color.RGBA{0, 0, 0xff, 0xff})
	// noisy differs from r by one level at one pixel, dotted by a black one.
	noisy := solid(8, 8, color.RGBA{0xff, 0, 0, 0xff})
	noisy.SetRGBA(3, 3, color.RGBA{0xfe, 0, 0, 0xff})
	dotted := solid(8, 8, color.RGBA{0xff, 0, 0, 0xff})
	dotted.SetRGBA(3, 3, color.RGBA{0, 0, 0, 0xff})
	tests := []struct {
		name      string
		test, ref []image.Image
		fuzzy     []int
		same      bool
	}{
		{"one equal page", []image.Image{r}, []image.Image{r}, nil, true},
		{"one differing page", []image.Image{r}, []image.Image{b}, nil, false},
		{"one pixel off by one level", []image.Image{noisy}, []image.Image{r}, nil, true},
		{"one black pixel", []image.Image{dotted}, []image.Image{r}, nil, false},
		{"one black pixel inside the fuzziness", []image.Image{dotted}, []image.Image{r}, []int{0, 255, 0, 1}, true},
		{"page counts differ", []image.Image{r, r}, []image.Image{r}, nil, false},
		{"second page differs", []image.Image{r, r}, []image.Image{r, b}, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			same, fail := comparePageSets(tc.test, tc.ref, reftestRef{Rel: "match", Fuzzy: tc.fuzzy})
			if same != tc.same {
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

// TestReftestFeature_ScreenPagesAreNotShrunk pins that a screen reftest is
// converted without the shrinking a browser prints with, and a print
// reftest with it: a box wider than the page keeps a square beside it at
// its size in the first and scales it down in the second.
func TestReftestFeature_ScreenPagesAreNotShrunk(t *testing.T) {
	t.Parallel()

	const wide = `<body style="margin:0"><div style="width:2000px;height:10px"></div><div style="width:100px;height:100px;background:#00f"></div></body>`
	var mu sync.Mutex
	shrunk := map[bool]bool{}
	record := func(ctx context.Context, src []byte, s htmlSettings) ([]byte, error) {
		opts := append([]pdf.HTMLOption{pdf.WithPaper(pdf.Paper{Width: 600, Height: 450}), pdf.WithMargins(0, 0, 0, 0)}, s.Print...)
		var out bytes.Buffer
		if _, err := pdf.FromHTML(bytes.NewReader(src), opts...).WriteTo(ctx, &out); err != nil {
			return nil, err
		}
		r, err := pdf.Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			return nil, err
		}
		img, err := r.RenderPage(ctx, 1, pdf.WithRenderDPI(96))
		if err != nil {
			return nil, err
		}
		// Unshrunk, the square spans rows 10 to 110: its last rows are
		// blue. Shrunk to fit 2000 pixels in 800, it ends well above.
		red, _, blue, _ := img.At(50, 100).RGBA()
		mu.Lock()
		shrunk[s.Media == "print"] = blue != 0xffff || red != 0
		mu.Unlock()
		return colourStub(60, 45)(ctx, []byte("red"), s)
	}
	f, root := newTestReftest(t, record, map[string]string{"a/wide.html": wide, "a/ref.html": wide},
		&reftestEntry{ID: "a/wide.html", Refs: []reftestRef{{Rel: "match", Path: "a/ref.html"}}},
		&reftestEntry{ID: "a/wide-print.html", Print: true, Refs: []reftestRef{{Rel: "match", Path: "a/ref.html"}}},
	)
	writeFiles(t, root, map[string]string{"a/wide-print.html": wide})
	for _, id := range []string{"a/wide.html", "a/wide-print.html"} {
		if _, err := f.Run(t.Context(), filepath.Join(root, filepath.FromSlash(id)), id); err != nil {
			t.Fatal(err)
		}
	}
	if shrunk[false] || !shrunk[true] {
		t.Errorf("shrunk: screen %v, print %v; want false, true", shrunk[false], shrunk[true])
	}
}

// TestReftestFeature_ScreenDrawsTheViewport pins how a screen reftest is
// drawn, with litho's converter: content that reaches past the viewport is
// not fragmented at its edge, the @page rules of the document do not apply,
// and the viewport units measure the 600 pixel viewport of a document that
// fits it.
func TestReftestFeature_ScreenDrawsTheViewport(t *testing.T) {
	t.Parallel()

	const body = `<body style="margin:0">`
	files := map[string]string{
		// On pages of the viewport the blue box, which avoids a break
		// inside, moves to the second page; on a screen it starts at 550.
		"a/avoid.html":     body + `<div style="height:550px"></div><div style="height:100px;break-inside:avoid;background:#00f"></div>`,
		"a/avoid-ref.html": body + `<div style="height:550px"></div><div style="height:50px;background:#00f"></div>`,
		"a/page.html":      `<style>@page { size: 100px 100px; margin: 20px }</style>` + body + `<div style="width:50px;height:50px;background:#00f"></div>`,
		"a/page-ref.html":  body + `<div style="width:50px;height:50px;background:#00f"></div>`,
		"a/vh.html":        body + `<div style="height:50vh;background:#00f"></div>`,
		"a/vh-ref.html":    body + `<div style="height:300px;background:#00f"></div>`,
	}
	match := func(id, ref string) *reftestEntry {
		return &reftestEntry{ID: id, Refs: []reftestRef{{Rel: "match", Path: ref}}}
	}
	entries := []*reftestEntry{match("a/avoid.html", "a/avoid-ref.html"), match("a/page.html", "a/page-ref.html"), match("a/vh.html", "a/vh-ref.html")}
	f, root := newTestReftest(t, convertHTML, files, entries...)
	for _, e := range entries {
		t.Run(e.ID, func(t *testing.T) {
			t.Parallel()
			out, err := f.Run(t.Context(), filepath.Join(root, filepath.FromSlash(e.ID)), e.ID)
			if err != nil {
				t.Fatal(err)
			}
			if v := f.Compare(e.ID, out); v.Outcome != outcomePass {
				t.Errorf("outcome %q (%s), want pass", v.Outcome, v.Reason)
			}
		})
	}
}

func TestTopViewport_CropsToTheViewport(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ w, h, wantW, wantH int }{{800, 1200, 800, 600}, {800, 600, 800, 600}, {900, 500, 800, 500}} {
		img := image.NewRGBA(image.Rect(0, 0, tt.w, tt.h))
		img.Set(0, 0, color.RGBA{R: 255, A: 255})
		got := topViewport(img)
		if b := got.Bounds(); b.Dx() != tt.wantW || b.Dy() != tt.wantH {
			t.Errorf("%dx%d: viewport %v, want %dx%d", tt.w, tt.h, b, tt.wantW, tt.wantH)
		}
		if r, _, _, _ := got.At(got.Bounds().Min.X, got.Bounds().Min.Y).RGBA(); r != 0xffff {
			t.Errorf("%dx%d: the top left pixel moved", tt.w, tt.h)
		}
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

	same := scoreDiff(g, diffLithoDoc{pages: []diffLithoPage{{text: "hello  world", links: []diffLink{{Page: 1, URI: "https://a.example"}}, img: page}}}, dir, false, "", noReport)
	if same.Outcome != outcomePass || same.Scores[scoreLayout] != 1 || same.Scores[scoreStrict] != 1 || same.Scores[scoreText] != 1 {
		t.Errorf("identical conversion: %+v", same)
	}

	reported := 0
	extra := scoreDiff(g, diffLithoDoc{pages: []diffLithoPage{
		{text: "hello", img: solid(40, 40, color.RGBA{0xff, 0xff, 0xff, 0xff})},
		{text: "overflow", img: page},
	}}, dir, false, "", func(int, image.Image, image.Image, pixelDiff) { reported++ })
	if extra.Outcome != outcomeFail || extra.Scores[scorePages] != 0.5 || extra.Scores[scoreLinks] != 0 || extra.Scores[scoreLayout] != 0 {
		t.Errorf("blank page plus an extra one: %+v", extra.Scores)
	}
	if reported != 1 {
		t.Errorf("reported %d pages, want the one compared page that fails", reported)
	}

	// An expected text stands for the reference's: litho's text, its pages
	// in order and line breaks as spaces, is scored against it whole, and
	// the reference's text is not read.
	wrong := *g
	wrong.text = &pdfjsReference{NumPages: 1, Pages: []pdfjsRefPage{{PageNumber: 1, Text: "dlrow olleh"}}}
	broken := diffLithoPage{text: "hello\nworld", links: []diffLink{{Page: 1, URI: "https://a.example"}}, img: page}
	if got := scoreDiff(&wrong, diffLithoDoc{pages: []diffLithoPage{broken}}, dir, false, "", noReport); got.Scores[scoreText] != 0 || got.ExpectedText {
		t.Errorf("against a reference whose text is wrong: text %v, expected text %v", got.Scores[scoreText], got.ExpectedText)
	}
	judged := scoreDiff(&wrong, diffLithoDoc{pages: []diffLithoPage{broken}}, dir, false, "hello\n world\n", noReport)
	if judged.Outcome != outcomePass || judged.Scores[scoreText] != 1 || !judged.ExpectedText || judged.PageResults[0].TextF1 != 0 {
		t.Errorf("against the expected text: %+v", judged)
	}
	half := scoreDiff(&wrong, diffLithoDoc{pages: []diffLithoPage{broken}}, dir, false, "hello there", noReport)
	if half.Scores[scoreText] != 0.5 {
		t.Errorf("one of two tokens against the expected text: text %v, want 0.5", half.Scores[scoreText])
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
		got := scoreDiff(&shaped, diffLithoDoc{pages: []diffLithoPage{p}, outline: tc.outline}, dir, false, "", noReport)
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
		byPage       bool
		want         float64
	}{
		{"neither has one", nil, nil, false, 1},
		{"only litho has one", nil, golden, false, 1},
		{"only Chromium has one", golden, nil, false, 0},
		{"the same", golden, golden, false, 1},
		{"a duplicate counted once", golden, []diffOutlineItem{{Title: "A", Page: 1}, {Title: "B", Page: 2}}, false, 2.0 / 3},
		{"NFKC and white space", []diffOutlineItem{{Title: "ﬁle  one", Page: 1}}, []diffOutlineItem{{Title: "file one", Page: 1}}, false, 1},
		{"another title on the page", []diffOutlineItem{{Title: "3f2a-uuid", Page: 1}}, []diffOutlineItem{{Title: "Sheet1", Page: 1}}, false, 0},
		{"by page, another title on the page", []diffOutlineItem{{Title: "3f2a-uuid", Page: 1}}, []diffOutlineItem{{Title: "Sheet1", Page: 1}}, true, 1},
		{"by page, the wrong page", []diffOutlineItem{{Title: "3f2a-uuid", Page: 1}}, []diffOutlineItem{{Title: "Sheet1", Page: 2}}, true, 0},
	}
	for _, tc := range cases {
		if got := outlineRecall(tc.golden, tc.have, tc.byPage); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestConverterFeatures_ReportErrorsWithoutTheConverter runs the three
// converter features end to end, through the runner and the baseline, with
// a converter that refuses every source as a format litho does not read is
// refused: every fixture is an error, recorded as such, and none
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
