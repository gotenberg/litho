package plaintext_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/plaintext"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// opts asks for the test face and every other default.
func opts() plaintext.Options {
	m := plaintext.DefaultMargin
	return plaintext.Options{FontFamilies: []string{"Test Serif"}, MarginTop: m, MarginRight: m, MarginBottom: m, MarginLeft: m}
}

// linesPerPage is how many lines fit a default page of the test face: the
// first baseline at the top margin plus the ascent, one line height apart,
// the last descent above the bottom margin.
func linesPerPage(t *testing.T, f *text.Fonts) int {
	t.Helper()
	m := f.Match(text.Style{Families: []string{"Test Serif"}, Size: plaintext.DefaultFontSize}).Metrics(plaintext.DefaultFontSize)
	room := plaintext.DefaultPageHeight - 2*plaintext.DefaultMargin - m.Ascent - m.Descent
	return int(math.Floor(room/(plaintext.DefaultFontSize*plaintext.DefaultLineHeight))) + 1
}

func TestConvert_PageCountFollowsTheLineHeight(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	per := linesPerPage(t, f)
	// 0.8 em ascent, 0.2 em descent, 13.8 points a line: 52 lines.
	if per != 52 {
		t.Fatalf("lines per page %d, want 52", per)
	}
	for _, n := range []int{1, per, per + 1, 2*per + 7} {
		src := strings.Repeat("AB\n", n)
		doc := convert(t, f, src, opts())
		want := (n + per - 1) / per
		if len(doc.Pages) != want {
			t.Errorf("%d lines: %d pages, want %d", n, len(doc.Pages), want)
		}
		if got := len(pageLines(doc.Pages[0])); got != min(n, per) {
			t.Errorf("%d lines: %d on the first page, want %d", n, got, min(n, per))
		}
	}
}

func TestConvert_FirstBaselineAndLineAdvance(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	doc := convert(t, f, "A\nB\n", opts())
	c := doc.Pages[0].Content
	if len(c) != 2 {
		t.Fatalf("%d fragments, want 2", len(c))
	}
	a, b := c[0].(*compose.Text), c[1].(*compose.Text)
	if want := plaintext.DefaultMargin + 0.8*12; math.Abs(a.Origin.Y-want) > 1e-3 || a.Origin.X != plaintext.DefaultMargin {
		t.Errorf("first origin %v, want (%v, %v)", a.Origin, plaintext.DefaultMargin, want)
	}
	if d := b.Origin.Y - a.Origin.Y; math.Abs(d-12*1.15) > 1e-9 {
		t.Errorf("line advance %v, want 13.8", d)
	}
}

func TestConvert_RoundTripsTheText(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	var src strings.Builder
	var want []string
	for i := range 130 {
		line := strings.Repeat(string(rune('a'+i%26)), 1+i%7) + " line " + strings.Repeat("Z", i%5)
		if i%9 == 0 {
			line = ""
		}
		src.WriteString(line + "\n")
		want = append(want, line)
	}
	doc := convert(t, f, src.String(), opts())
	r := open(t, write(t, doc))
	per := linesPerPage(t, f)
	if n, err := r.PageCount(context.Background()); err != nil || n != (len(want)+per-1)/per {
		t.Fatalf("page count %d, %v", n, err)
	}
	var got []string
	for p := 1; p <= len(doc.Pages); p++ {
		s, err := r.ExtractText(context.Background(), p)
		if err != nil {
			t.Fatalf("ExtractText(%d): %v", p, err)
		}
		got = append(got, nonEmptyLines(s)...)
	}
	if !slices.Equal(got, nonEmpty(want)) {
		t.Errorf("extracted %q,\nwant %q", got, nonEmpty(want))
	}
}

// nonEmptyLines splits s into lines, trims their trailing white space and
// drops the empty ones.
func nonEmptyLines(s string) []string {
	return nonEmpty(strings.Split(s, "\n"))
}

func nonEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if l = strings.TrimRight(l, " \t"); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestConvert_FormFeedStartsAPage(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	for _, tc := range []struct {
		src   string
		pages [][]string
	}{
		{"A\fB", [][]string{{"A"}, {"B"}}},
		{"A\f\nB\n", [][]string{{"A"}, {"B"}}},
		{"A\n\f\fB", [][]string{{"A"}, nil, {"B"}}},
		{"\fA", [][]string{nil, {"A"}}},
		{"A\f", [][]string{{"A"}}},
		{"A\f\n\f\n", [][]string{{"A"}}},
		{"", [][]string{nil}},
	} {
		doc := convert(t, f, tc.src, opts())
		var got [][]string
		for _, p := range doc.Pages {
			got = append(got, pageLines(p))
		}
		if !slices.EqualFunc(got, tc.pages, slices.Equal) {
			t.Errorf("%q: pages %q, want %q", tc.src, got, tc.pages)
		}
	}
}

func TestConvert_ExpandsTabsToTabStops(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	for _, tc := range []struct {
		tab  int
		src  string
		want string
	}{
		{0, "A\tB", "A       B"},
		{0, "ABCDEFGH\tB", "ABCDEFGH        B"},
		{4, "A\tBC\tD", "A   BC  D"},
		{2, "\t\tX", "    X"},
	} {
		o := opts()
		o.TabWidth = tc.tab
		doc := convert(t, f, tc.src, o)
		if got := pageLines(doc.Pages[0]); len(got) != 1 || got[0] != tc.want {
			t.Errorf("tab %d, %q: %q, want %q", tc.tab, tc.src, got, tc.want)
		}
	}
	// The last glyph sits at its column: 8 characters of 6 points.
	doc := convert(t, f, "A\tB", opts())
	run := doc.Pages[0].Content[0].(*compose.Text)
	if g := run.Glyphs[len(run.Glyphs)-1]; math.Abs(g.X-48) > 1e-3 || g.Text != "B" {
		t.Errorf("B at %v (%q), want 48", g.X, g.Text)
	}
}

func TestConvert_WrapsALongLine(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	// 80 characters of 6 points fill the 481.89-point line.
	word := "ABCDEFGHI "
	src := strings.Repeat(word, 20)
	doc := convert(t, f, src, opts())
	lines := pageLines(doc.Pages[0])
	if len(lines) != 3 {
		t.Fatalf("lines %q, want 3", lines)
	}
	if lines[0] != strings.Repeat(word, 8) || strings.Join(lines, "") != src {
		t.Errorf("lines %q", lines)
	}
	if n := len(paragraphs(t, doc)); n != 1 {
		t.Errorf("%d paragraphs, want 1 for one source line", n)
	}
}

func TestConvert_ParagraphContinuesOnTheNextPage(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	per := linesPerPage(t, f)
	src := strings.Repeat("A\n", per-1) + strings.Repeat("ABCDEFGHI ", 16)
	doc := convert(t, f, src, opts())
	if len(doc.Pages) != 2 || len(pageLines(doc.Pages[1])) != 1 {
		t.Fatalf("%d pages", len(doc.Pages))
	}
	last := paragraphs(t, doc)[per-1]
	for _, p := range doc.Pages {
		if run := p.Content[len(p.Content)-1].(*compose.Text); run.Parent != last {
			t.Errorf("the split paragraph's runs point at %p, want %p", run.Parent, last)
		}
	}
}

func TestConvert_DecodesBOMAndLineEnds(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	doc := convert(t, f, "\xEF\xBB\xBFAB\r\nCD\rEF\n\r\nGH", opts())
	if got, want := pageLines(doc.Pages[0]), []string{"AB", "CD", "EF", "GH"}; !slices.Equal(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
	if n := len(paragraphs(t, doc)); n != 4 {
		t.Errorf("%d paragraphs, want 4", n)
	}
	// The blank line between EF and GH takes one line height.
	c := doc.Pages[0].Content
	if d := c[3].(*compose.Text).Origin.Y - c[2].(*compose.Text).Origin.Y; math.Abs(d-2*13.8) > 1e-9 {
		t.Errorf("EF to GH %v, want two line heights", d)
	}
}

func TestConvert_ReplacesInvalidUTF8(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	doc := convert(t, f, "A\xffB\xed\xa0\x80C\n\xff", opts())
	lines := pageLines(doc.Pages[0])
	if len(lines) != 2 || lines[0] != "A�B���C" || lines[1] != "�" {
		t.Errorf("lines %q", lines)
	}
	if len(doc.Warnings) != 1 {
		t.Fatalf("warnings %+v, want one for U+FFFD", doc.Warnings)
	}
	w := doc.Warnings[0]
	if w.Code != plaintext.CodeMissingGlyph || !strings.Contains(w.Message, "U+FFFD") || !strings.Contains(w.Message, "Test Serif") || w.Page != 1 {
		t.Errorf("warning %+v", w)
	}
	write(t, doc)
}

func TestConvert_WarnsOncePerMissingCharacter(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	doc := convert(t, f, "é\nAé\fé ü\n", opts())
	var got []string
	for _, w := range doc.Warnings {
		got = append(got, w.Message[:len("no font covers U+00E9")])
		if w.Code != plaintext.CodeMissingGlyph {
			t.Errorf("code %q", w.Code)
		}
	}
	if want := []string{"no font covers U+00E9", "no font covers U+00FC"}; !slices.Equal(got, want) {
		t.Errorf("warnings %q, want %q", got, want)
	}
	if doc.Warnings[1].Page != 2 {
		t.Errorf("U+00FC reported on page %d, want 2", doc.Warnings[1].Page)
	}
}

func TestConvert_RefusesASourcePastTheByteCap(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	o := opts()
	o.MaxInputBytes = 4
	if _, err := plaintext.Convert(context.Background(), strings.NewReader("ABCD"), f, o); err != nil {
		t.Errorf("4 bytes under a 4-byte cap: %v", err)
	}
	if _, err := plaintext.Convert(context.Background(), strings.NewReader("ABCDE"), f, o); !errors.Is(err, plaintext.ErrInputTooLarge) {
		t.Errorf("5 bytes under a 4-byte cap: %v, want ErrInputTooLarge", err)
	}
}

func TestConvert_RefusesPastThePageCap(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	o := opts()
	o.MaxPages = 2
	if doc := convert(t, f, "A\fB", o); len(doc.Pages) != 2 {
		t.Errorf("%d pages, want 2", len(doc.Pages))
	}
	if _, err := plaintext.Convert(context.Background(), strings.NewReader("A\fB\fC"), f, o); !errors.Is(err, plaintext.ErrTooManyPages) {
		t.Errorf("three pages under a two-page cap: %v, want ErrTooManyPages", err)
	}
	if _, err := plaintext.Convert(context.Background(), strings.NewReader(strings.Repeat("A\n", 200)), f, o); !errors.Is(err, plaintext.ErrTooManyPages) {
		t.Errorf("200 lines under a two-page cap: %v, want ErrTooManyPages", err)
	}
}

func TestConvert_TagsEachLineThatDrawsText(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	doc := convert(t, f, "AB\n\nCD EF\n   \n", opts())
	paras := paragraphs(t, doc)
	if len(paras) != 3 {
		t.Fatalf("%d paragraphs, want 3: AB, CD EF and the spaces", len(paras))
	}
	for i, frag := range doc.Pages[0].Content {
		if run := frag.(*compose.Text); run.Parent != paras[i] {
			t.Errorf("run %d points at %p, want %p", i, run.Parent, paras[i])
		}
	}
	if doc.Metadata.Lang != "" {
		t.Errorf("Lang %q, want unknown", doc.Metadata.Lang)
	}
	r := open(t, write(t, doc))
	if tagged, err := r.IsTagged(context.Background()); err != nil || !tagged {
		t.Errorf("IsTagged = %v, %v", tagged, err)
	}
}

func TestConvert_IsDeterministic(t *testing.T) {
	t.Parallel()
	src := "Hello\tworld\nsecond line é\fthird page " + strings.Repeat("WORD ", 40)
	a := write(t, convert(t, testFonts(t), src, opts()))
	b := write(t, convert(t, testFonts(t), src, opts()))
	if !bytes.Equal(a, b) {
		t.Error("two conversions of the same source wrote different bytes")
	}
}

func TestConvert_SplitsAHugeLine(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	o := opts()
	o.MaxPages = 1000
	doc := convert(t, f, strings.Repeat("A", 1<<16+10), o)
	n := 0
	for _, w := range doc.Warnings {
		if w.Code == plaintext.CodeLineSplit {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d line-split warnings, want 1", n)
	}
}

func TestConvert_ValidatesOptions(t *testing.T) {
	t.Parallel()
	f := testFonts(t)
	for name, o := range map[string]plaintext.Options{
		"one page side":        {PageWidth: 100},
		"negative width":       {PageWidth: -1, PageHeight: 100},
		"NaN height":           {PageWidth: 100, PageHeight: math.NaN()},
		"past the PDF limit":   {PageWidth: 14401, PageHeight: 100},
		"negative font size":   {FontSize: -1},
		"infinite font size":   {FontSize: math.Inf(1)},
		"huge font size":       {FontSize: 1001},
		"NaN line height":      {LineHeight: math.NaN()},
		"negative line height": {LineHeight: -1},
		"negative margin":      {MarginTop: -1},
		"infinite margin":      {MarginLeft: math.Inf(1)},
		"no room across":       {PageWidth: 100, PageHeight: 100, MarginLeft: 50, MarginRight: 50},
		"no room down":         {MarginTop: 500, MarginBottom: 400},
		"negative tab":         {TabWidth: -1},
		"huge tab":             {TabWidth: 65},
		"negative input cap":   {MaxInputBytes: -1},
		"input cap past ceil":  {MaxInputBytes: 1<<30 + 1},
		"negative page cap":    {MaxPages: -1},
		"page cap past ceil":   {MaxPages: 100_001},
	} {
		if _, err := plaintext.Convert(context.Background(), strings.NewReader("A"), f, o); !errors.Is(err, plaintext.ErrInvalidOptions) {
			t.Errorf("%s: %v, want ErrInvalidOptions", name, err)
		}
	}
	if _, err := plaintext.Convert(context.Background(), strings.NewReader("A"), nil, opts()); !errors.Is(err, plaintext.ErrInvalidOptions) {
		t.Errorf("nil fonts: %v, want ErrInvalidOptions", err)
	}
	empty, err := text.NewFonts(context.Background(), nil, []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plaintext.Convert(context.Background(), strings.NewReader("A"), empty, opts()); !errors.Is(err, text.ErrNoFonts) {
		t.Errorf("empty index: %v, want ErrNoFonts", err)
	}
}

func TestConvert_HonoursTheContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := plaintext.Convert(ctx, strings.NewReader("A"), testFonts(t), opts()); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestConvert_DefaultsToSerif(t *testing.T) {
	t.Parallel()
	// No family names the test face, so Match falls back to it; the page
	// takes A4, and the zero margins are kept as none.
	doc := convert(t, testFonts(t), "A", plaintext.Options{})
	p := doc.Pages[0]
	if p.Width != 595.276 || p.Height != 841.890 {
		t.Errorf("page %vx%v, want A4", p.Width, p.Height)
	}
	if run := p.Content[0].(*compose.Text); run.Size != 12 || run.Origin.X != 0 {
		t.Errorf("run size %v at %v", run.Size, run.Origin)
	}
}
