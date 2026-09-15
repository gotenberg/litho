package compliance

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ttfWithUnitsPerEm rebuilds [nonSymbolicTTF] with a chosen head.unitsPerEm, so
// a test can hand the width maths a scale the OpenType spec does not allow.
func ttfWithUnitsPerEm(upem uint16) string {
	prog := []byte(nonSymbolicTTF())
	// The head table is the fixture's second table; find it through the
	// directory rather than assuming an offset.
	numTables := int(binary.BigEndian.Uint16(prog[4:6]))
	for i := range numTables {
		rec := 12 + 16*i
		if string(prog[rec:rec+4]) != "head" {
			continue
		}
		off := binary.BigEndian.Uint32(prog[rec+8 : rec+12])
		binary.BigEndian.PutUint16(prog[int(off)+18:], upem)
		break
	}
	return string(prog)
}

// TestExpectedWidthsRejectAnImpossibleUnitsPerEm pins that a corrupt
// head.unitsPerEm cannot produce a nonsense width.
//
// The OpenType head table states "Valid range is from 16 to 16384", so a value
// outside it is a corrupt head, not a scale, and every width divided by it is
// meaningless. pdfjs issue9462.pdf carries such a font: litho scaled by it,
// wrote /Widths near 1.5 million into the remediated file, and the output then
// broke the PDF/A-1 real-number ceiling of 32767 (ISO 19005-1, 6.1.12 test 2) --
// a repair that introduced a violation the input never had.
func TestExpectedWidthsRejectAnImpossibleUnitsPerEm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// The fixture's GID 1 is 722 units wide; at 1000 upm that is 722.
	const wantAt1000 = 722.0

	cases := []struct {
		name string
		upem uint16
	}{
		{"a sane scale is used as given", 1000},
		{"below the legal minimum falls back", 7},
		{"zero falls back", 0},
		{"above the legal maximum falls back", 40000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog := ttfWithUnitsPerEm(tc.upem)
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]"+
					" /Resources << /Font << /F1 4 0 R >> >> >>",
				"<< /Type /Font /Subtype /TrueType /BaseFont /ABCDEF+Litho"+
					" /FirstChar 65 /LastChar 65 /Widths [722]"+
					" /Encoding /WinAnsiEncoding /FontDescriptor 5 0 R >>",
				"<< /Type /FontDescriptor /FontName /ABCDEF+Litho /Flags 32 /FontFile2 6 0 R >>",
				fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream",
					len(prog), len(prog), prog),
			)

			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
			if err != nil || fontDict == nil {
				t.Fatalf("resolving the font dict: %v", err)
			}
			got, ok := ExpectedSimpleFontWidths(ctx, doc, fontDict)
			if !ok {
				t.Skip("metrics unavailable for this fixture")
			}
			w, has := got[65]
			if !has {
				t.Fatalf("no expected width for code 65: %v", got)
			}
			// Every case must land on the same width: a legal scale because it
			// is 1000, an illegal one because it falls back to 1000.
			if w < wantAt1000-1 || w > wantAt1000+1 {
				t.Errorf("upem %d gave width %g, want about %g", tc.upem, w, wantAt1000)
			}
			// The concrete failure being guarded: a width that cannot be written
			// into a PDF/A-1 file at all.
			if w > maxRealAbs1 {
				t.Errorf("upem %d gave width %g, past the PDF/A-1 real ceiling %g",
					tc.upem, w, maxRealAbs1)
			}
		})
	}
}

// TestUnitsPerEmBoundsMatchTheSpec pins the bounds themselves against the
// OpenType head table, so a later edit cannot quietly widen them.
func TestUnitsPerEmBoundsMatchTheSpec(t *testing.T) {
	t.Parallel()
	if minUnitsPerEm != 16 || maxUnitsPerEm != 16384 {
		t.Errorf("unitsPerEm bounds are %d..%d, want 16..16384 (OpenType head)",
			minUnitsPerEm, maxUnitsPerEm)
	}
	if defaultUnitsPerEm != 1000 {
		t.Errorf("fallback upem is %d, want the 1000 the unreadable-head paths use",
			defaultUnitsPerEm)
	}
}
