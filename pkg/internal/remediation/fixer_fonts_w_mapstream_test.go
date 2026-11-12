package remediation

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// mappedGlyphScaledWidth is the advance of glyph 37 of [notdefTTFPath], 1336
// at 2048 units per em, in the 1000-unit space a PDF width uses.
const mappedGlyphScaledWidth = 652.34

// cidFontMapStreamPDF builds a Type0 font over [notdefTTFPath] whose
// descendant CIDFont leads CID 1 to glyph 37 through a /CIDToGIDMap stream
// and declares w as CID 1's width. Glyph 1, which CID 1 would select under
// Identity, advances 0. /DW agrees with the program, so only /W can change.
func cidFontMapStreamPDF(t *testing.T, w string) []byte {
	t.Helper()

	ttf, err := os.ReadFile(notdefTTFPath)
	if err != nil {
		t.Fatalf("reading the font fixture: %v", err)
	}
	cidToGID := "\x00\x00\x00\x25"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /X /Encoding /Identity-H /DescendantFonts [5 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /X /CIDToGIDMap 8 0 R /DW 443.36" +
			" /W [1 [" + w + "]] /FontDescriptor 6 0 R" +
			" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		"<< /Type /FontDescriptor /FontName /X /Flags 4 /FontFile2 7 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ttf), string(ttf)),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cidToGID), cidToGID),
	})
}

// TestFixType0FontReadsWidthsThroughTheMapStream pins that the /W repair
// weighs a CID against the glyph its /CIDToGIDMap stream selects (ISO
// 32000-1, 9.7.4.2, Table 117), as the width check does. Read by glyph index
// instead, a width that agreed with the program was replaced by another
// glyph's, and the repair created the mismatches it reports fixing.
//
// # Corpus evidence
//
//   - litho/docx: fields-logic-formula converts with two Carlito subsets
//     under map streams beside a font whose .notdef width disagrees. The
//     repair of that one width rewrote every Carlito width, and the PDF/A-2b
//     report listed 324 residual violations where it now lists one.
func TestFixType0FontReadsWidthsThroughTheMapStream(t *testing.T) {
	t.Parallel()

	t.Run("an agreeing width stays", func(t *testing.T) {
		t.Parallel()

		cid, fixes := repairedCIDFont(t, cidFontMapStreamPDF(t, "652"))
		if cid != nil {
			t.Errorf("the CIDFont was rewritten: %v", fixes)
		}
	})

	t.Run("a disagreeing width takes the mapped glyph's", func(t *testing.T) {
		t.Parallel()

		cid, fixes := repairedCIDFont(t, cidFontMapStreamPDF(t, "500"))
		if cid == nil {
			t.Fatalf("the CIDFont was not rewritten, fixes=%v", fixes)
		}
		w, err := document.ResolveArray(t.Context(), nil, cid["W"])
		if err != nil || len(w) != 2 {
			t.Fatalf("/W = %v, %v", cid["W"], err)
		}
		run, err := document.ResolveArray(t.Context(), nil, w[1])
		if err != nil || len(run) != 1 {
			t.Fatalf("/W run = %v, %v", w[1], err)
		}
		if got := document.Float(run[0]); math.Abs(got-mappedGlyphScaledWidth) > 0.01 {
			t.Errorf("CID 1 width = %v, want %v, the advance of the glyph the map selects", got, mappedGlyphScaledWidth)
		}
	})
}
