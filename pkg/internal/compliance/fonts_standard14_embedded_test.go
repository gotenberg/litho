package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestStandard14EmbeddedRequiresMetrics pins that embedding a font program
// forfeits the standard 14 exemption from /FirstChar, /LastChar and /Widths.
//
// ISO 32000-1, 9.6.2.2 makes those three and /FontDescriptor all present or all
// absent: the standard 14 names are a shorthand for metrics a reader already
// holds. A font that embeds its own glyphs has left the shorthand behind and
// owes its own metrics, which is why veraPDF's isStandard is false for it.
// litho exempted on the /BaseFont name alone.
//
// # Corpus evidence
//
//   - pdfjs issue11150_reduced.pdf: names /Symbol and embeds a /FontFile
//     while declaring none of the three, and veraPDF reports ISO 19005-4,
//     6.2.10.2 t4, t5 and t6 on it.
func TestStandard14EmbeddedRequiresMetrics(t *testing.T) {
	t.Parallel()

	// pdf builds a page showing one code through a /Symbol font that carries a
	// descriptor, embedding a program only when embed is set. The font declares
	// no /FirstChar, /LastChar or /Widths either way.
	pdf := func(embed bool) []byte {
		const stream = "BT /F1 12 Tf 10 50 Td (A) Tj ET"
		const prog = "%!PS-AdobeFont-1.0: Symbol 001.001\n/FontName /Symbol def\n" +
			"currentdict end\ncurrentfile eexec\n"
		fontFile := ""
		if embed {
			fontFile = "/FontFile 6 0 R "
		}
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Symbol /FontDescriptor 7 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
			fmt.Sprintf("<< /Length %d /Length1 %d /Length2 0 /Length3 0 >>\nstream\n%s\nendstream",
				len(prog), len(prog), prog),
			"<< /Type /FontDescriptor /FontName /Symbol /Flags 4 /ItalicAngle 0 "+
				"/Ascent 1010 /Descent -293 /CapHeight 0 /StemV 85 "+
				"/FontBBox [-180 -293 1090 1010] "+fontFile+">>",
		)
	}

	// firesRequiredKey reports how many of the three required-key tests fail.
	firesRequiredKey := func(t *testing.T, data []byte) int {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFA(context.Background(), doc, models.PDFA4)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		n := 0
		for _, e := range viols {
			v, ok := e.(Violation)
			if !ok || v.Rule.Clause != "6.2.10.2" {
				continue
			}
			if v.Rule.TestNumber >= 4 && v.Rule.TestNumber <= 6 {
				n++
			}
		}
		return n
	}

	t.Run("an embedded standard 14 font owes its metrics", func(t *testing.T) {
		t.Parallel()
		if got := firesRequiredKey(t, pdf(true)); got != 3 {
			t.Errorf("required-key violations = %d, want 3 (/FirstChar, /LastChar, /Widths)", got)
		}
	})

	t.Run("a non-embedded one keeps the exemption", func(t *testing.T) {
		t.Parallel()
		if got := firesRequiredKey(t, pdf(false)); got != 0 {
			t.Errorf("required-key violations = %d, want 0: the standard 14 metrics are implied", got)
		}
	})
}
