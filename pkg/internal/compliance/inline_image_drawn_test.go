package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// countRulePDFA returns how many violations of the clause/test rule validating
// data under profile raises.
func countRulePDFA(t *testing.T, data []byte, profile models.PDFAProfile, clause string, test int) int {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	n := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			n++
		}
	}
	return n
}

// TestInlineImageFiltersInEveryDrawnStream pins the reach of the inline image
// filter rule, ISO 19005-1, 6.1.10 t2 and ISO 19005-2, 6.1.10 t1 (see
// [checkInlineImageFilters]). Every case was measured with veraPDF 1.30 at
// PDF/A-1b and PDF/A-2b: an LZW inline image fails the rule once in a page's
// content and once in every form XObject, tiling pattern, Type3 glyph and
// annotation appearance the page draws, a form painted twice, or by two pages,
// once, and a form no Do paints never. Two images in a row in a form fail it
// once, as they do in a page's content, and two with an operator between them
// twice.
func TestInlineImageFiltersInEveryDrawnStream(t *testing.T) {
	t.Parallel()

	var f drawnFixture
	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		lzw     = "BI /W 1 /H 1 /BPC 8 /CS /G /F /LZW ID xx EI\n"
		lzw2    = "BI /W 1 /H 1 /BPC 8 /CS /G /F /LZW ID yy EI\n"
	)
	x := f.xobjects
	for _, tc := range []struct {
		name string
		want int
		objs []string
	}{
		{"a form the page paints", 1, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "q /Fm0 Do Q"), f.form("<< >>", lzw),
		}},
		{"the page's content and a form", 2, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", lzw+"q /Fm0 Do Q"), f.form("<< >>", lzw),
		}},
		{"a form painted twice", 1, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do /Fm0 Do"), f.form("<< >>", lzw),
		}},
		{"a form two pages paint", 1, []string{
			f.pages(3, 6), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"), f.form("<< >>", lzw),
			f.page(x("Fm0", "5"), 4, ""),
		}},
		{"a form no Do paints", 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", drawnPlain), f.form("<< >>", lzw),
		}},
		{"a form a form paints", 1, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form(x("Fm1", "6"), "/Fm1 Do"), f.form("<< >>", lzw),
		}},
		{"a tiling pattern", 1, []string{
			f.pages(3), f.page("<< /Pattern << /P0 5 0 R >> >>", 4, ""),
			exponentStream("", "/Pattern cs /P0 scn 0 0 100 100 re f"), f.pattern("<< >>", lzw),
		}},
		{"a Type3 glyph", 1, []string{
			f.pages(3), f.page("<< /Font << /T0 5 0 R >> >>", 4, ""),
			exponentStream("", "BT /T0 12 Tf 10 10 Td (a) Tj ET"), f.type3("<< >>", 6),
			exponentStream("", "1000 0 d0 "+lzw),
		}},
		{"an annotation appearance", 1, []string{
			f.pages(3), f.page("<< >>", 4, " /Annots [5 0 R]"), exponentStream("", drawnPlain), f.annot(6),
			f.form("<< >>", lzw),
		}},
		{"two in a row in a form", 1, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"), f.form("<< >>", lzw+lzw2),
		}},
		{"two apart in a form", 2, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"), f.form("<< >>", lzw+"q Q\n"+lzw2),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := cyclePDF(append([]string{catalog}, tc.objs...)...)
			if got := countRulePDFA(t, data, models.PDFA1b, "6.1.10", 2); got != tc.want {
				t.Errorf("PDF/A-1b: 6.1.10 t2 fails %d times, want %d", got, tc.want)
			}
			if got := countRulePDFA(t, data, models.PDFA2b, "6.1.10", 1); got != tc.want {
				t.Errorf("PDF/A-2b: 6.1.10 t1 fails %d times, want %d", got, tc.want)
			}
		})
	}
}
