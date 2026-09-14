package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestBlendModeCompatibleIsTransparency pins that an ExtGState naming
// /Compatible makes a page transparent for ISO 19005-2 §6.2.10 t2, while the
// neighbouring rule on which values /BM may take still admits it.
//
// The pair reads oddly and is the whole point: ISO 32000-1, 11.3.5 calls
// /Compatible "the same as Normal" and §6.2.10 t1 lists it beside Normal among
// the permitted values, so litho read it as no transparency at all. Whether the
// page CONTAINS transparency is a different question, and veraPDF's
// PDPage.containsTransparency answers yes.
//
// Measured: three fixtures identical but for the blend mode, none with a page
// /Group or a PDF/A OutputIntent. veraPDF passes /Normal and reports both
// /Compatible and /Darken under §6.2.10 t2. govdocs thread_300_300345.pdf is the
// corpus case, a page whose /GS3 names /Compatible.
func TestBlendModeCompatibleIsTransparency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	page := func(t *testing.T, blendMode string) []Violation {
		t.Helper()

		const content = "/GS0 gs 0 0 100 100 re f"
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
				" /Resources << /ExtGState << /GS0 5 0 R >> >> >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /ExtGState /BM /"+blendMode+" /CA 1 /ca 1 /SMask /None >>",
		)
		return checkTransparencyParts2Plus(ctx, doc, cat, collectPages(ctx, doc, cat), "PDF/A-2b", 2)
	}

	t.Run("Compatible contains transparency", func(t *testing.T) {
		t.Parallel()
		if vs := page(t, "Compatible"); !hasRule(vs, Rule_19005_2_6_2_10_t02) {
			t.Errorf("a /Compatible blend mode must report §6.2.10 t2, got %v", vs)
		}
	})

	t.Run("Darken contains transparency", func(t *testing.T) {
		t.Parallel()
		if vs := page(t, "Darken"); !hasRule(vs, Rule_19005_2_6_2_10_t02) {
			t.Errorf("a /Darken blend mode must report §6.2.10 t2, got %v", vs)
		}
	})

	t.Run("Normal does not", func(t *testing.T) {
		t.Parallel()
		if vs := page(t, "Normal"); hasRule(vs, Rule_19005_2_6_2_10_t02) {
			t.Errorf("a /Normal blend mode must report nothing, got %v", vs)
		}
	})

	t.Run("the permitted-values rule still admits Compatible", func(t *testing.T) {
		t.Parallel()
		// ISO 19005-2 §6.2.10 t1 and ISO 19005-1 §6.4 t4 both list Compatible
		// beside Normal, so neither may report it.
		if vs := page(t, "Compatible"); hasRule(vs, Rule_19005_2_6_2_10_t01) {
			t.Errorf("/Compatible is a permitted blend mode, got %v", vs)
		}
	})
}
