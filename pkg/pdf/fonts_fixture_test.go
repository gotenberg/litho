package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/models"
)

// fontEmbedFixture is the litho corpus fixture whose unembedded fonts
// [fixturePrograms] covers.
var fontEmbedFixture = filepath.Join("..", "..", "test", "integration", "corpus", "litho", "pdfs", "fontembed_2b.pdf")

// fixturePrograms returns the synthetic programs [TestFontEmbedFixture]
// supplies, by /BaseFont: a glyph for each character the fixture shows with
// the font, at the width the font declares, but for LithoWide, whose program
// advances 667 where the font declares 900. LithoMissing gets none.
func fixturePrograms() map[string][]byte {
	glyph := func(name string, r rune, adv uint16) fontbuild.Glyph {
		return fontbuild.Glyph{Name: name, Unicode: r, Advance: adv}
	}
	sans := func(ps string, a, b uint16) []byte {
		return fontbuild.TrueType{PostScriptName: ps, Family: ps, Subfamily: "Regular", NotdefAdvance: 500,
			Glyphs: []fontbuild.Glyph{glyph("A", 'A', a), glyph("B", 'B', b)}}.Build()
	}
	return map[string][]byte{
		"Helvetica": fontbuild.TrueType{PostScriptName: "FixtureSans", Family: "Fixture Sans", Subfamily: "Regular", NotdefAdvance: 500,
			Glyphs: []fontbuild.Glyph{glyph("H", 'H', 722), glyph("e", 'e', 556), glyph("l", 'l', 222), glyph("o", 'o', 556)}}.Build(),
		"Helvetica-Bold":    sans("FixtureSans-Bold", 722, 722),
		"Helvetica-Oblique": sans("FixtureSans-Oblique", 667, 667),
		"LithoSans":         sans("FixtureSans", 667, 667),
		"LithoWide":         sans("FixtureWide", 667, 667),
		"LithoSerif": fontbuild.CFF{FontName: "FixtureSerif", Glyphs: []fontbuild.Glyph{
			{Name: "A", Advance: 722}, {Name: "B", Advance: 667},
		}}.Build(),
		"LithoMincho": fontbuild.TrueType{PostScriptName: "FixtureMincho", Family: "Fixture Mincho", Subfamily: "Regular",
			NotdefAdvance: 1000, Glyphs: []fontbuild.Glyph{glyph("uni3042", 0x3042, 1000)}}.Build(),
	}
}

// remediatedFixture remediates data at PDF/A-2b with opts, and returns the
// report, the output, and the rule classes the output fails with the count of
// each.
func remediatedFixture(t *testing.T, data []byte, opts ...RemediateOption) (*RemediationReport, []byte, map[string]int) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	report, err := w.RemediatePDFA(ctx, models.PDFA2b, opts...)
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r2.Close()
	vs, err := r2.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	rules := make(map[string]int)
	for _, v := range toViolations(vs) {
		rules[fmt.Sprintf("%s/%d", v.Clause, v.TestNumber)]++
	}
	return report, out.Bytes(), rules
}

// textPositions returns every text element of every page of data, keyed by
// its page, its content-stream chunk and its text.
func textPositions(t *testing.T, data []byte) map[string]models.TextElement {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	pages, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	out := make(map[string]models.TextElement)
	for page := 1; page <= pages; page++ {
		els, err := r.ExtractTextElements(ctx, page)
		if err != nil {
			t.Fatalf("ExtractTextElements(%d): %v", page, err)
		}
		for _, el := range els {
			out[fmt.Sprintf("%d/%d/%s", page, el.Chunk, el.Text)] = el
		}
	}
	return out
}

// TestFontEmbedFixture remediates the litho corpus fixture fontembed_2b.pdf
// with synthetic font programs built here, so the corpus commits none. It is
// the coverage the corpus gate cannot give: the gate remediates without a
// font provider, where every font of the fixture stays unembedded, and its
// runner sits outside pkg/, so it cannot build these programs.
//
// With the programs, seven of the nine fonts are embedded, the ones written
// inline among them, and two are refused with their reasons. No rule class
// the remediation without programs leaves is added, and every text element
// keeps its place.
//
// # Corpus evidence
//
//   - litho fontembed_2b.pdf: veraPDF 1.30.2 at PDF/A-2b reports 6.2.11.4.1
//     t1 nine times on the source and twice on this output, LithoMissing and
//     LithoWide, as litho does, and no rule the source did not fail.
func TestFontEmbedFixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(fontEmbedFixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	programs := fixturePrograms()
	provider := FontProviderFunc(func(_ context.Context, req FontRequest) ([]byte, error) {
		return programs[req.BaseFont], nil
	})

	_, _, without := remediatedFixture(t, data)
	if without["6.2.11.4.1/1"] != 9 {
		t.Fatalf("precondition: the output without programs fails 6.2.11.4.1 t1 %d times, want 9", without["6.2.11.4.1/1"])
	}
	report, out, with := remediatedFixture(t, data, WithFontProvider(provider))
	if with["6.2.11.4.1/1"] != 2 {
		t.Errorf("6.2.11.4.1 t1 fails %d times, want LithoMissing and LithoWide alone", with["6.2.11.4.1/1"])
	}
	for rule, n := range with {
		if n > without[rule] {
			t.Errorf("rule %s fails %d times, %d without programs", rule, n, without[rule])
		}
	}

	want := map[string]error{"LithoMissing": ErrFontNotSupplied, "LithoWide": ErrFontWidths}
	if len(report.FontRefusals) != len(want) {
		t.Errorf("refusals %v, want %d", report.FontRefusals, len(want))
	}
	for _, r := range report.FontRefusals {
		if !errors.Is(r.Err, want[r.BaseFont]) {
			t.Errorf("refusal %v, want %v", r, want[r.BaseFont])
		}
	}

	// LithoMincho names a predefined CMap of a known Adobe collection, so the
	// source extracts U+3042 unembedded (ISO 32000-1, 9.10.2) and the output
	// keeps it in place once the program is embedded.
	src, got := textPositions(t, data), textPositions(t, out)
	if len(src) != 10 {
		t.Fatalf("the source extracts %d text elements, want 10", len(src))
	}
	for key, was := range src {
		now, ok := got[key]
		if !ok {
			t.Errorf("element %s is gone", key)
			continue
		}
		if math.Abs(was.X-now.X) > 0.01 || math.Abs(was.Y-now.Y) > 0.01 || math.Abs(was.Width-now.Width) > 0.01 {
			t.Errorf("element %s: at (%.2f, %.2f) width %.2f, was at (%.2f, %.2f) width %.2f",
				key, now.X, now.Y, now.Width, was.X, was.Y, was.Width)
		}
	}
}
