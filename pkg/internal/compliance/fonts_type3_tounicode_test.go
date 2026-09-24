package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestToUnicode_Type3JudgedPerShownCode pins that a Type 3 font with no
// /ToUnicode is judged under PDF/UA by the glyph names of the codes a page
// shows (ISO 14289-1, 7.21.7 t1), not by every name its /Differences lists.
//
// On one-glyph fixtures veraPDF fails a shown code whose name is empty or that
// /Differences leaves out, and takes a missing name from a /BaseEncoding when
// one is given.
//
// # Corpus evidence
//
//   - pdfjs issue5896.pdf: names its letters A, G, P and so on but leaves two
//     empty names at codes 11 and 12 it never shows. litho reported the whole
//     font while veraPDF passed every shown glyph.
func TestToUnicode_Type3JudgedPerShownCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
		code     byte
		fires    bool
	}{
		{"a shown glyph list name beside unshown empty names", "/Differences [11 / / 65 /A]", 65, false},
		{"a shown empty name", "/Differences [11 / / 65 /A]", 11, true},
		{"a shown code the array leaves out", "/Differences [11 / / 65 /A]", 66, true},
		{"a missing name the base encoding supplies", "/BaseEncoding /WinAnsiEncoding /Differences [11 /]", 66, false},
		{"a name no glyph list defines", "/Differences [65 /xyzzy]", 65, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]"+
					" /CharProcs << /A 3 0 R /B 3 0 R >> /Encoding << /Type /Encoding "+tt.encoding+" >>"+
					" /FirstChar 0 /LastChar 255 /Widths [] /Resources << >> >>",
				"<< /Length 8 >>\nstream\n500 0 d0\nendstream",
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{tt.code: true}, rawStrings: []ast.PdfString{ast.PdfString([]byte{tt.code})}}
			got := checkToUnicode(ctx, doc, fontDict, "F1", 1, "PDF/UA-1", usage)
			if fired := len(got) != 0; fired != tt.fires {
				t.Errorf("fired = %v (%v), want %v", fired, got, tt.fires)
			}
		})
	}
}
