package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestType1WidthsWithoutArray pins ISO 19005-2, 6.2.11.5 t1 on a classic Type 1
// font whose /Widths cannot be indexed for the code shown: no array, no
// /FirstChar, an empty array, or a code past /LastChar. The code then declares
// the /MissingWidth (ISO 32000-1, 9.6.2.1, Table 111), default 0, which veraPDF
// weighs the charstring advance against: pdfjs issue11150_reduced.pdf embeds a
// Symbol program with no /Widths. Each case mirrors one measured against
// veraPDF.
func TestType1WidthsWithoutArray(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	program := type1NamedGlyphProgram(type1EncodingVector, type1EncodingGlyphs...)

	for _, tt := range []struct {
		name   string
		widths string
		want   int
	}{
		{"no /Widths", "", 1},
		{"a /Widths array covering the code", " /FirstChar 65 /LastChar 65 /Widths [722]", 0},
		{"a /Widths array with no /FirstChar", " /LastChar 65 /Widths [722]", 1},
		{"an empty /Widths array", " /FirstChar 65 /LastChar 65 /Widths []", 1},
		{"a code past /LastChar", " /FirstChar 64 /LastChar 64 /Widths [0 722]", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			const body = "BT /F1 12 Tf 10 50 Td (A) Tj ET"
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
				"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Test"+tt.widths+" /Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>",
				"<< /Type /FontDescriptor /FontName /ABCDEF+Test /Flags 32 /FontBBox [0 0 1000 1000] /ItalicAngle 0"+
					" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile 7 0 R >>",
				fmt.Sprintf("<< /Length %d /Length1 %d /Length2 %d /Length3 %d >>\nstream\n%s\nendstream",
					len(program.data), program.clear, program.eexec, len(program.data)-program.clear-program.eexec, program.data),
			)
			viols, err := ValidatePDFA(ctx, doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_11_5_t01); got != tt.want {
				t.Errorf("6.2.11.5 t1 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
