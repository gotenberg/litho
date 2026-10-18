package remediation

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// probeTrueType builds the smallest TrueType program the width readers accept:
// a table directory over head, hhea, hmtx and maxp. unitsPerEm is 1000, so an
// advance width is already in glyph space units, and GID 1 advances by advance.
func probeTrueType(advance uint16) []byte {
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:20], 1000)

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[34:36], 2) // numberOfHMetrics

	hmtx := make([]byte, 8)
	binary.BigEndian.PutUint16(hmtx[4:6], advance) // GID 1 advance

	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:6], 2) // numGlyphs

	tables := []struct {
		tag  string
		data []byte
	}{{"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp}}

	out := make([]byte, 12+len(tables)*16)
	binary.BigEndian.PutUint32(out[0:4], 0x00010000)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(tables)))
	for i, t := range tables {
		rec := 12 + i*16
		copy(out[rec:rec+4], t.tag)
		binary.BigEndian.PutUint32(out[rec+8:rec+12], uint32(len(out)))
		binary.BigEndian.PutUint32(out[rec+12:rec+16], uint32(len(t.data)))
		out = append(out, t.data...)
	}
	return out
}

// directCIDFontPDF builds a Type0 font whose /W declares 999 for CID 1 while
// the font program advances by 500. When direct is set the descendant CIDFont
// is inlined in the /DescendantFonts array (object 5), which is what producers
// emit; otherwise object 5 is the CIDFont dictionary itself.
func directCIDFontPDF(direct bool) []byte {
	cidFont := "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Probe " +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> " +
		"/FontDescriptor 6 0 R /W [1 [999]] >>"
	descendants := cidFont
	if direct {
		descendants = "[" + cidFont + "]"
	}

	program := probeTrueType(500)
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F0 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Probe /Encoding /Identity-H "+
			"/DescendantFonts "+descendantsRef(direct)+" >>",
		descendants,
		"<< /Type /FontDescriptor /FontName /Probe /Flags 4 /FontFile2 7 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program),
	)
}

// descendantsRef names object 5 as the array itself when the CIDFont is inlined
// there, and wraps it in a one-element array when object 5 is the CIDFont.
func descendantsRef(direct bool) string {
	if direct {
		return "5 0 R"
	}
	return "[5 0 R]"
}

// TestFixType0Font_PersistsWidthsThroughADirectCIDFont pins that a corrected
// /W array reaches the output when the descendant CIDFont is a direct
// dictionary.
//
// Per ISO 32000-1, Table 121 /DescendantFonts shall hold a reference, so the
// fixer wrote the corrected widths at the CIDFont's own object number. A direct
// dictionary has none, so the widths were computed, dropped, and still reported
// as repaired on every pass while the ISO 19005-2, 6.2.11.5 width mismatch
// stayed in the output forever.
//
// # Corpus evidence
//
//   - pdfjs issue18298.pdf: remediating it reported "corrected /W widths" for
//     three CIDFonts on every pass while the width mismatch stayed in the
//     output.
func TestFixType0Font_PersistsWidthsThroughADirectCIDFont(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		direct bool
		// objNum is where the corrected widths must land. wantArray says the
		// written object is the /DescendantFonts array rather than the CIDFont.
		objNum    int
		wantArray bool
	}{
		{"control: /DescendantFonts holds a reference", false, 5, false},
		{"descendant CIDFont is a direct dictionary", true, 5, true},
	} {
		doc, alloc := newTestDoc(t, directCIDFontPDF(tc.direct))
		fontRef := ast.PdfIndirectRef{ObjectNum: 4}
		fontDict, err := document.ResolveDict(context.Background(), doc, fontRef)
		if err != nil || fontDict == nil {
			t.Fatalf("%s: resolving the font dictionary: %v", tc.name, err)
		}

		fixes := fixType0Font(context.Background(), doc, alloc, fontDict, fontRef, true, nil, nil)
		if len(fixes) == 0 {
			t.Fatalf("%s: no fix reported for a font whose /W disagrees with its program", tc.name)
		}

		pending, ok := alloc.Pending()[tc.objNum]
		if !ok {
			t.Fatalf("%s: object %d not written; the repair was reported but dropped", tc.name, tc.objNum)
		}
		cid, ok := pending.(ast.PdfDict)
		if tc.wantArray {
			arr, isArr := pending.(ast.PdfArray)
			if !isArr || len(arr) == 0 {
				t.Fatalf("%s: object %d is %T, want the /DescendantFonts array", tc.name, tc.objNum, pending)
			}
			cid, ok = arr[0].(ast.PdfDict)
		}
		if !ok {
			t.Fatalf("%s: written object does not hold a CIDFont dictionary", tc.name)
		}

		w, isArr := cid["W"].(ast.PdfArray)
		if !isArr || len(w) != 2 {
			t.Fatalf("%s: /W = %v, want a two-element array", tc.name, cid["W"])
		}
		widths, isArr := w[1].(ast.PdfArray)
		if !isArr || len(widths) != 1 {
			t.Fatalf("%s: /W widths = %v, want one entry", tc.name, w[1])
		}
		if got := document.Float(widths[0]); got != 500 {
			t.Errorf("%s: corrected width = %v, want 500 from the font program", tc.name, got)
		}
	}
}
