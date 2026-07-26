package compliance

import (
	"fmt"
	"testing"
)

// TestNoResourcesPage_ImplicitDeviceGrayFires pins that a page with no
// /Resources anywhere in its ancestry is still scanned for device colour usage.
// The content stream strokes a path without ever selecting a colour space, so it
// paints in the initial graphics-state colour space, DeviceGray (ISO 32000-1
// §8.4.1). With no OutputIntent that is an uncalibrated colour space use, and
// ISO 19005-2 §6.2.4.3 t4 (ISO 19005-1 §6.2.3.3 t3) fires. litho previously
// skipped any page whose effective /Resources resolved to nil, missing it
// entirely (pdfcpu test.pdf, testRot.pdf, testWithText.pdf).
func TestNoResourcesPage_ImplicitDeviceGrayFires(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length 15 >>\nstream\n0 0 m 10 10 l S\nendstream",
	)
	if !firesRule2b(t, data, "6.2.4.3", 4) {
		t.Error("implicit DeviceGray on a page with no /Resources must fire §6.2.4.3 t4")
	}
}

// TestStrokeColorWithoutColorSpace_DeviceGrayFires pins that SC/SCN does not
// count as selecting a colour space. Per ISO 32000-1 §8.6.8 Table 74, SC sets
// components within the space a preceding CS selected; with no CS the stroking
// space is still the initial DeviceGray, so the S that follows paints in
// DeviceGray and §6.2.4.3 t4 fires. litho previously treated SC as proof that a
// colour space had been chosen and stayed silent (pdfcpu pike-stanford.pdf,
// where every page strokes a rule after a bare "0.3 SC").
func TestStrokeColorWithoutColorSpace_DeviceGrayFires(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R "+
			"/Resources << /ProcSet [/PDF] >> >>",
		"<< /Length 21 >>\nstream\n0.3 SC 0 0 10 10 re S\nendstream",
	)
	if !firesRule2b(t, data, "6.2.4.3", 4) {
		t.Error("a bare SC followed by S must fire §6.2.4.3 t4")
	}
}

// TestFormXObjectBareScn_NoFalsePositive pins the boundary of the SC/SCN rule.
// A Form XObject inherits the graphics state in effect at the invoking Do (ISO
// 32000-1 §8.10.2), so a bare scn inside it may legitimately name a component
// of the ICCBased space the page selected. litho does not model that inherited
// state, so it must not report DeviceGray there: veraPDF, which does propagate
// the state, reports nothing (pdf.js issue19954.pdf).
func TestFormXObjectBareScn_NoFalsePositive(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R "+
			"/Resources << /XObject << /Fm1 5 0 R >> /ColorSpace << /Cs1 [/ICCBased 6 0 R] >> >> >>",
		"<< /Length 34 >>\nstream\nq /Cs1 cs 0.5 0.5 0.5 sc /Fm1 Do Q\nendstream",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources << >> /Length 22 >>\n"+
			"stream\n0.5 scn 0 0 10 10 re f\nendstream",
		"<< /N 3 /Length 4 >>\nstream\nfake\nendstream",
	)
	if firesRule2b(t, data, "6.2.4.3", 4) {
		t.Error("a bare scn inside a Form XObject must not fire §6.2.4.3 t4")
	}
}

// TestTextRenderingModeSelectsNoColourSpace pins that text which adds no marks
// selects no colour space. Per ISO 32000-1, Table 106, rendering mode 3 neither
// fills nor strokes and mode 7 only adds the glyphs to the clipping path, so
// neither paints in the initial DeviceGray. Reporting it there flagged an
// uncalibrated colour space the file never uses; veraPDF 1.30.2 agrees, and it
// diverged from litho on exactly modes 3 and 7 and on no other mode.
func TestTextRenderingModeSelectsNoColourSpace(t *testing.T) {
	t.Parallel()

	// The content stream selects no colour space, so the only candidate is the
	// initial DeviceGray of ISO 32000-1 8.4.1.
	page := func(mode int) []byte {
		body := fmt.Sprintf("BT /F1 12 Tf %d Tr 50 700 Td (AB) Tj ET\n", mode)
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
				" /Resources << /Font << /F1 5 0 R >> >> >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(body), body),
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		)
	}

	for _, mode := range []int{0, 1, 2, 4, 5, 6} {
		if !firesRule2b(t, page(mode), "6.2.4.3", 4) {
			t.Errorf("rendering mode %d paints, so implicit DeviceGray must fire", mode)
		}
	}
	for _, mode := range []int{textRenderInvisible, textRenderClip} {
		if firesRule2b(t, page(mode), "6.2.4.3", 4) {
			t.Errorf("rendering mode %d adds no marks, so it must select no colour space", mode)
		}
	}
}
