package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestStitchingTintTransformEqualsItself pins that a direct Separation whose
// tint transform is a stitching function over a type 4 one is consistent with
// itself at every link, ISO 19005-2, 6.2.4.4 t2. veraPDF 1.30.2 fails nothing
// at PDF/A-2b where litho failed it three times, once per link.
func TestStitchingTintTransformEqualsItself(t *testing.T) {
	t.Parallel()

	const program = "{ pop 0.5 }"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /ColorSpace << /CS1 [/Separation /Spot /DeviceGray " +
			"<< /FunctionType 3 /Domain [0 1] /Functions [5 0 R] /Bounds [] /Encode [0 1] >>] >> >> /Contents 4 0 R >>",
		"<< /Length 28 >>\nstream\n/CS1 cs 1 scn 0 0 10 10 re f\nendstream",
		fmt.Sprintf("<< /FunctionType 4 /Domain [0 1] /Range [0 1] /Length %d >>\nstream\n%s\nendstream", len(program), program),
	}
	doc, _ := objsDocPDF(t, objs...)
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	if got := countRuleErrs(t, viols, Rule_19005_2_6_2_4_4_t02); got != 0 {
		t.Errorf("6.2.4.4 t2 fired %d times, want 0", got)
	}
}
