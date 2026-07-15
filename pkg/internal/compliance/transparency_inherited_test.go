package compliance

import "testing"

// TestInheritedResources_TransparencyDetected pins that PDF/A-1 §6.4
// transparency restrictions apply to a page whose /Resources are inherited from
// a Pages node. The inherited /ExtGState declares transparency (ca 0.5, BM
// Multiply); before resource inheritance was resolved in the transparency check,
// litho skipped the page and missed the violation. Mirrors the device-colour
// inheritance fix (see [TestInheritedResources_DeviceGrayDetected]).
func TestInheritedResources_TransparencyDetected(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources "+
			"<< /ExtGState << /GS1 << /Type /ExtGState /ca 0.5 /BM /Multiply >> >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length 24 >>\nstream\n/GS1 gs 0 0 5 5 re f\nendstream",
	)
	// §6.4 t6: a /ca constant-alpha other than 1.0 is transparency (forbidden).
	if !firesRule(t, data, "6.4", 6) {
		t.Error("transparency ExtGState in inherited resources must fire §6.4 t6")
	}
}
