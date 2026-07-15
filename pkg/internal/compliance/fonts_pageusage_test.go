package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// pageFontUsagePDF builds a one-page PDF whose /Resources/Font declares a single
// non-embedded TrueType font. content is the page content stream: pass a stream
// that selects the font (a rendering Tf plus a show operator) or one that never
// selects it to toggle whether the font is used for visible rendering.
func pageFontUsagePDF(content string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /TrueType /BaseFont /Arial "+
			"/FirstChar 65 /LastChar 65 /Widths [500] /FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /Arial /Flags 32 "+
			"/Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 "+
			"/FontBBox [0 0 500 700] /ItalicAngle 0 >>",
	)
}

func firesRule2b(t *testing.T, data []byte, clause string, test int) bool {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			return true
		}
	}
	return false
}

// TestPageFont_RenderedNonEmbeddedFires pins that a non-embedded, non-symbolic
// TrueType page font selected and shown by a content operator is checked: it has
// no /Encoding, so it fires the TrueType encoding rule (ISO 19005-2 §6.2.11.6
// t2), and being non-embedded it fires the embedding rule (§6.2.11.4.1 t1). This
// matches veraPDF, which builds a PDTrueTypeFont only for fonts a content
// operator renders.
func TestPageFont_RenderedNonEmbeddedFires(t *testing.T) {
	t.Parallel()
	data := pageFontUsagePDF("BT /F1 12 Tf (A) Tj ET")
	if !firesRule2b(t, data, "6.2.11.6", 2) {
		t.Error("rendered non-symbolic TrueType font with no /Encoding must fire §6.2.11.6 t2")
	}
	if !firesRule2b(t, data, "6.2.11.4.1", 1) {
		t.Error("rendered non-embedded page font must fire §6.2.11.4.1 t1 (embedding)")
	}
}

// TestPageFont_DeclaredButUnusedSkipped pins that a font declared in the page
// /Resources/Font but never selected by a content operator renders no glyph and
// is out of scope for the page pass: veraPDF never builds it, so litho must not
// check it. The embedding rule was already scoped to visible fonts, but the
// TrueType encoding rule ran on every declared font and raised a §6.2.11.6 t2
// false positive veraPDF never reports (25 files across the qpdf corpus).
func TestPageFont_DeclaredButUnusedSkipped(t *testing.T) {
	t.Parallel()
	data := pageFontUsagePDF("BT ET")
	if firesRule2b(t, data, "6.2.11.6", 2) {
		t.Error("declared-but-unrendered TrueType page font must not fire §6.2.11.6 t2")
	}
	if firesRule2b(t, data, "6.2.11.4.1", 1) {
		t.Error("declared-but-unrendered page font must not fire §6.2.11.4.1 t1")
	}
}
