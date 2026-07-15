package compliance

import (
	"testing"
)

// TestFieldOnlyWidgetNotChecked pins that a widget present only in the /AcroForm
// /Fields tree, never referenced by a page /Annots array, does not fire the
// §6.3.3 appearance rule: veraPDF scopes it to PDAnnot (rendered annotations),
// and such a widget renders nothing (qpdf shared-unnamed-field obj 30).
func TestFieldOnlyWidgetNotChecked(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>", // no /Annots
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [10 10 100 30] >>", // no /AP
	)
	if firesRule2b(t, data, "6.3.3", 1) {
		t.Error("a /Fields-only widget (not in any page /Annots) must not fire §6.3.3 t1")
	}
}

// TestPageAnnotWidgetChecked pins the other side: the same widget referenced by
// a page /Annots array is a rendered annotation and its missing appearance fires
// §6.3.3 t1, so the scope fix does not under-report.
func TestPageAnnotWidgetChecked(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [10 10 100 30] >>", // no /AP
	)
	if !firesRule2b(t, data, "6.3.3", 1) {
		t.Error("a page /Annots widget missing /AP must fire §6.3.3 t1")
	}
}

// TestAppearanceMissingNIsT2 pins that a widget whose /AP is present but carries
// no /N appearance fires §6.3.3 t2, not t1: veraPDF's t1 is satisfied once an
// /AP exists (AP != null), and t2 covers an /AP that is not the lone /N
// appearance (qpdf need-appearances-more2 annot 10).
func TestAppearanceMissingNIsT2(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		// /AP present with a /D (down) appearance but no /N.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [10 10 100 30] /AP << /D 6 0 R >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 90 20] /Length 0 >>\nstream\n\nendstream",
	)
	if firesRule2b(t, data, "6.3.3", 1) {
		t.Error("/AP present without /N must not fire §6.3.3 t1 (AP != null)")
	}
	if !firesRule2b(t, data, "6.3.3", 2) {
		t.Error("/AP present without /N must fire §6.3.3 t2")
	}
}
