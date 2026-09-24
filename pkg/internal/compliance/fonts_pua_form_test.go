package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPUAActualText_PerGlyphAndInForms pins that ISO 14289-2, 8.4.3 is judged
// per shown glyph, wherever the glyph is drawn.
//
// veraPDF builds a Glyph for every code a text operator shows, in the page
// content and in every Form XObject it invokes, and fails one whose Unicode
// value lies in a Private Use Area unless the marked-content sequence around
// it, or the structure element its MCID names, carries /ActualText
// (GFGlyph.getactualTextPresent). litho checked page fonts only, and exempted
// a whole composite font once any /ActualText appeared. Measured against
// veraPDF on fixtures of each shape: marked content around the Do operator
// does not cover the form's glyphs, and one covered glyph does not cover its
// neighbour.
//
// # Corpus evidence
//
//   - pdfjs issue14864.pdf: draws 114 PUA glyphs of an Identity-H Type 0 font
//     inside a Form XObject.
func TestPUAActualText_PerGlyphAndInForms(t *testing.T) {
	t.Parallel()

	// The /ToUnicode maps codes 1 to 4 into the PUA through an array-form
	// bfrange (ISO 32000-1, 9.10.3).
	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <0000> <ffff> endcodespacerange\n" +
		"1 beginbfrange <0001> <0004> [<E12D> <E19E> <E1D7> <E19C>] endbfrange\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	const (
		shown   = "BT /F1 12 Tf 10 50 Td <00010002> Tj ET"
		partial = "/Span << /ActualText (x) >> BDC BT /F1 12 Tf 10 50 Td <0001> Tj ET EMC " +
			"BT /F1 12 Tf 10 30 Td <0002> Tj ET"
	)

	// doc draws formBody inside a Form XObject that pageBody invokes; an empty
	// formBody puts the font on the page and draws pageBody there instead.
	doc := func(pageBody, formBody string) []byte {
		font := "<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H " +
			"/DescendantFonts [8 0 R] /ToUnicode 6 0 R >>"
		pageRes := "/Resources << /Font << /F1 4 0 R >> >>"
		if formBody != "" {
			pageRes = "/Resources << /XObject << /Fm1 10 0 R >> >>"
		}
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] " + pageRes + " /Contents 5 0 R >>",
			font,
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageBody), pageBody),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
			"<< /Type /StructTreeRoot >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /DW 1000 " +
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 9 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 /Ascent 700 " +
				"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
		}
		if formBody != "" {
			objs = append(objs, fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(formBody), formBody))
		}
		return cyclePDF(objs...)
	}

	fires := func(t *testing.T, data []byte) bool {
		t.Helper()
		d, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), d, models.PDFUA2)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "8.4.3" {
				return true
			}
		}
		return false
	}

	tests := []struct {
		name     string
		pageBody string
		formBody string
		want     bool
	}{
		{"PUA glyphs shown on the page", shown, "", true},
		{"one of two PUA glyphs covered on the page", partial, "", true},
		{"PUA glyphs shown in a form", "q /Fm1 Do Q", shown, true},
		{"every PUA glyph in a form covered", "q /Fm1 Do Q", "/Span << /ActualText (xx) >> BDC " + shown + " EMC", false},
		{"one of two PUA glyphs covered in a form", "q /Fm1 Do Q", partial, true},
		{"ActualText around the Do does not reach the form", "/Span << /ActualText (xx) >> BDC q /Fm1 Do Q EMC", shown, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, doc(tt.pageBody, tt.formBody)); got != tt.want {
				t.Errorf("8.4.3 fired = %v, want %v", got, tt.want)
			}
		})
	}
}
