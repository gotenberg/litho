package compliance

import (
	"testing"
)

// TestInheritedResources_DeviceGrayDetected pins that DeviceGray usage is
// checked on a page whose /Resources are inherited from a Pages node rather than
// declared on the page itself (ISO 32000-1 §7.7.3.4). The page paints text with
// the default fill colour space (DeviceGray) and the document has no
// OutputIntent, so ISO 19005-2 §6.2.4.3 t4 fires. Before inheritance was
// resolved here, litho skipped the page and missed the violation (qpdf
// shared-images, form-xobjects-*).
func TestInheritedResources_DeviceGrayDetected(t *testing.T) {
	t.Parallel()
	// The leaf Page has no /Resources; the Pages node carries them. Page content
	// shows text with no explicit colour operator (implicit DeviceGray fill).
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /Font << /F1 5 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length 39 >>\nstream\nBT /F1 24 Tf 72 720 Td (hi) Tj ET\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
	if !firesRule2b(t, data, "6.2.4.3", 4) {
		t.Error("DeviceGray on a page with inherited resources must fire §6.2.4.3 t4")
	}
}

// TestInheritedResources_DefaultGrayCovers pins the other side: when the
// inherited /Resources carry a device-independent /DefaultGray, the same page
// does not fire §6.2.4.3 t4. This confirms the inheritance fix also inherits the
// covering DefaultGray, so it does not over-report.
func TestInheritedResources_DefaultGrayCovers(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /Font << /F1 5 0 R >> "+
			"/ColorSpace << /DefaultGray [/CalGray << /WhitePoint [0.9505 1.0 1.089] >>] >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length 39 >>\nstream\nBT /F1 24 Tf 72 720 Td (hi) Tj ET\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
	if firesRule2b(t, data, "6.2.4.3", 4) {
		t.Error("DeviceGray covered by an inherited DefaultGray must not fire §6.2.4.3 t4")
	}
}
