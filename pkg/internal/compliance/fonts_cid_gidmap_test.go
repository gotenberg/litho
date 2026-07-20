package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCheckCIDFontWidths_HonorsCIDToGIDMap pins that the CIDFontType2 glyph-width
// check resolves the embedded program width by the mapped GID, not the raw CID.
// A non-Identity /CIDToGIDMap maps CID 15 to GID 2; /W declares CID 15's width as
// GID 2's width. Reading GID 15 instead (the Identity assumption) reports a
// spurious mismatch, a false positive veraPDF does not raise because it follows
// the map (ISO 32000-1 §9.7.4.2 / ISO 14289-1 §7.21.5).
func TestCheckCIDFontWidths_HonorsCIDToGIDMap(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	rule := Rule_14289_1_7_21_5_t01

	binaryWidths := map[uint16]uint16{
		0:  1000, // .notdef
		2:  700,  // the glyph CID 15 actually maps to
		15: 600,  // a different glyph at the raw-CID slot
	}
	var unitsPerEm uint16 = 1000

	// CIDToGIDMap: two bytes per CID; CID 15 → GID 2, everything else → GID 0.
	gidMap := make([]byte, 32)
	gidMap[30], gidMap[31] = 0x00, 0x02 // CID 15 (offset 30) → GID 2
	mapStream := ast.NewStream(ast.PdfDict{}, gidMap)

	// /W [15 [700]] declares CID 15's width as 700 = GID 2's width.
	wArr := ast.PdfArray{ast.PdfInt(15), ast.PdfArray{ast.PdfReal(700.0)}}

	t.Run("mapped GID width matches: no violation", func(t *testing.T) {
		t.Parallel()
		dict := ast.PdfDict{"W": wArr, "CIDToGIDMap": mapStream}
		results := checkCIDFontWidths(ctx, nopDoc(), dict, binaryWidths, unitsPerEm, "TestCID", 1, rule)
		if len(results) != 0 {
			t.Errorf("expected no violation (CID 15 → GID 2 width 700 == /W 700), got %d: %v", len(results), results)
		}
	})

	t.Run("identity assumption would misfire", func(t *testing.T) {
		t.Parallel()
		// Same /W but no map: CID 15 → GID 15 (width 600) ≠ 700 → real mismatch.
		dict := ast.PdfDict{"W": wArr}
		results := checkCIDFontWidths(ctx, nopDoc(), dict, binaryWidths, unitsPerEm, "TestCID", 1, rule)
		if len(results) == 0 {
			t.Error("expected a violation under Identity mapping (GID 15 width 600 != /W 700), got none")
		}
	})

	t.Run("CID mapping to notdef is skipped", func(t *testing.T) {
		t.Parallel()
		// /W [7 [123]]: CID 7 maps to GID 0 (.notdef, width 1000). A mismatch here
		// is a coverage concern, not a width one, so the width check skips it.
		dict := ast.PdfDict{
			"W":           ast.PdfArray{ast.PdfInt(7), ast.PdfArray{ast.PdfReal(123.0)}},
			"CIDToGIDMap": mapStream,
		}
		results := checkCIDFontWidths(ctx, nopDoc(), dict, binaryWidths, unitsPerEm, "TestCID", 1, rule)
		if len(results) != 0 {
			t.Errorf("expected no violation for CID 7 → GID 0 (.notdef), got %d: %v", len(results), results)
		}
	})
}
