package pdf

import "testing"

// overprintFlattenPDF paints cyan, magenta under overprint mode 1 over it,
// and a half-transparent black square over both.
func overprintFlattenPDF() []byte {
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R " +
			"/Resources << /ExtGState << /Half 5 0 R /OP 6 0 R >> >> >>",
		colorStream("", "1 0 0 0 k 0 0 100 100 re f q /OP gs 0 1 0 0 k 0 0 100 100 re f Q q /Half gs 0 0 0 1 k 40 40 20 20 re f Q"),
		"<< /Type /ExtGState /ca 0.5 >>",
		"<< /Type /ExtGState /OP true /OPM 1 >>",
	})
}

func TestFlattenTransparency_Overprint(t *testing.T) {
	t.Parallel()
	report, _ := flattenPDF(t, overprintFlattenPDF(), WithFlattenDPI(72))
	if len(report.Pages) != 1 || report.Pages[0].Overprinted != 1 {
		t.Fatalf("report %+v", report)
	}
	report, out := flattenPDF(t, overprintFlattenPDF(), WithFlattenDPI(72), WithFlattenOverprintSimulation())
	if len(report.Pages) != 1 || report.Pages[0].Overprinted != 1 {
		t.Fatalf("simulated report %+v", report)
	}
	// The simulated composite keeps the cyan the magenta overprinted: drawn
	// on screen the square shows blue and black, not magenta and black.
	img := renderRGBA(t, openColorPDF(t, out), 72)
	if c := nrgbaAt(t, img, 50, 50); c.R > c.B {
		t.Errorf("square centre %v, want blue under the black", c)
	}
}
