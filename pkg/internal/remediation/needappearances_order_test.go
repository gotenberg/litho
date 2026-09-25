package remediation

import (
	"bytes"
	"fmt"
	"testing"
)

// splitValueAppearancePDF builds a form whose stored appearance paints the
// field value "HelloWorld" in two halves, one per Form XObject. The stream
// invokes /B, which shows "Hello", before /A, which shows "World", so paint
// order and name order disagree.
func splitValueAppearancePDF() []byte {
	body := "/Tx BMC\nq /B Do Q\nq /A Do Q\nEMC\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /NeedAppearances true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (t) /V (HelloWorld) /Rect [10 700 300 720] " +
			"/AP << /N 5 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 290 20] "+
			"/Resources << /XObject << /A 6 0 R /B 7 0 R >> >> /Length %d >>\nstream\n%sendstream", len(body), body),
		"<< /Type /XObject /Subtype /Form /BBox [0 0 290 20] /Length 22 >>\nstream\nBT (World) Tj ET\nendstream",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 290 20] /Length 22 >>\nstream\nBT (Hello) Tj ET\nendstream",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// TestNeedAppearancesReadsTheAppearanceInPaintOrder pins that the text an
// appearance shows is read in the order its stream paints the XObjects, on
// every run.
//
// ISO 19005-2, 6.9 forbids /NeedAppearances, and the flag is removed only
// when the stored appearance already shows the value (ISO 32000-1,
// 12.7.3.3). The halves were joined by ranging the /XObject resource map, so
// the same bytes read "HelloWorld" on some runs and "WorldHello" on others,
// and the flag was dropped or kept at random (CONTRIBUTING "Deterministic
// output").
func TestNeedAppearancesReadsTheAppearanceInPaintOrder(t *testing.T) {
	t.Parallel()

	data := splitValueAppearancePDF()
	// Go randomizes map iteration per range statement, so a two-entry map
	// ranged 16 times comes out in both orders with probability 1 - 2^-15.
	for run := range 16 {
		if fixes := runNeedAppearances(t, data); len(fixes) == 0 {
			t.Fatalf("run %d: kept the flag, want it removed: the appearance paints \"Hello\" then \"World\"", run)
		}
	}
}
