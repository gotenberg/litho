package compliance

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestHeadlessTrueTypeScalesAt2048 pins that a TrueType program with no
// 'head' table has its advances weighed at 2048 units per em, as veraPDF
// weighs them. The litho fixture fontnohead_2b.pdf plants H1, a simple font
// whose 1229-unit advances /Widths states as 600, and H2, a CIDFontType2 whose
// 600-unit advances /W states as 293. Both are consistent at 2048 and failed
// ISO 19005-2, 6.2.11.5 t1 at 1000; veraPDF 1.30.2 passes both. HO, an OpenType
// program with CFF outlines and no 'head', keeps its own scale.
func TestHeadlessTrueTypeScalesAt2048(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		profile string
		rule    Rule
	}{
		{"PDF/A-1b", Rule_19005_1_6_3_6_t01},
		{"PDF/A-2b", Rule_19005_2_6_2_11_5_t01},
		{"PDF/A-4", Rule_19005_4_6_2_10_5_t01},
		{"PDF/UA-2", Rule_14289_2_8_4_5_6_t01},
	} {
		t.Run(tt.profile, func(t *testing.T) {
			t.Parallel()
			if got := fixtureFontsFailing(t, "fontnohead_2b.pdf", tt.profile, tt.rule); len(got) != 0 {
				t.Errorf("width rule fired for %v, want none", got)
			}
		})
	}
}

// TestHeadlessTrueTypeExpectedWidth pins the converse of
// [TestHeadlessTrueTypeScalesAt2048] through the repair helper: a headless
// program advancing 600 units is drawn 293 wide, so remediation writes that,
// and veraPDF 1.30.2 fails /Widths 600 for it at PDF/A-2b.
func TestHeadlessTrueTypeExpectedWidth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prog := ttfWithoutTable("head")
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /Litho /FirstChar 65 /LastChar 65 /Widths [722]"+
			" /Encoding /WinAnsiEncoding /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /Litho /Flags 32 /FontFile2 6 0 R >>",
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(prog), len(prog), prog),
	)
	fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving the font dict: %v", err)
	}
	got, ok := ExpectedSimpleFontWidths(ctx, doc, fontDict)
	if !ok {
		t.Fatal("no metrics for a program whose 'hhea' and 'hmtx' read")
	}
	// The fixture's GID 1 advances 722 units: 722 * 1000 / 2048.
	if want := 722.0 * 1000 / 2048; math.Abs(got[65]-want) > 0.01 {
		t.Errorf("expected width of code 65 = %v, want %v", got[65], want)
	}
}

// ttfWithoutTable rebuilds [nonSymbolicTTF] with the table tag dropped from
// its directory, the table's bytes left where they were.
func ttfWithoutTable(tag string) string {
	prog := []byte(nonSymbolicTTF())
	numTables := int(binary.BigEndian.Uint16(prog[4:6]))
	out := append([]byte{}, prog[:12]...)
	kept := 0
	for i := range numTables {
		rec := prog[12+16*i : 28+16*i]
		if string(rec[:4]) == tag {
			continue
		}
		out = append(out, rec...)
		kept++
	}
	// The dropped record's 16 bytes are padded back so every table offset
	// still points at its table.
	out = append(out, make([]byte, 16*(numTables-kept))...)
	out = append(out, prog[12+16*numTables:]...)
	binary.BigEndian.PutUint16(out[4:6], uint16(kept)) //nolint:gosec // G115: kept is below the uint16 table count it came from.
	return string(out)
}
