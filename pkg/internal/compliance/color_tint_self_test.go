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

// TestTintTransformStreamsCompareAsVeraPDF pins how two Separations naming one
// colorant through two type 4 function streams fare under ISO 19005-2, 6.2.4.4
// t2. veraPDF compares the streams by content (COSStream.equals): every entry
// but /Filter, /DecodeParms and /Length, and the data as written. Measured
// with veraPDF 1.30.2 at PDF/A-2b, the six links fail the test six times or
// not at all: a copy passes, and another /Domain, another program, a /DL entry
// or the same program under another filter fail.
func TestTintTransformStreamsCompareAsVeraPDF(t *testing.T) {
	t.Parallel()

	const program = "{ pop 0.5 }"
	const fn = "/FunctionType 4 /Domain [0 1] /Range [0 1]"
	stream := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	content := "/CS1 cs 1 scn 0 0 10 10 re f /CS2 cs 1 scn 0 0 10 10 re f"
	for _, tc := range []struct {
		name   string
		second string
		want   int
	}{
		{"a copy", stream(fn, program), 0},
		{"a /Length written as a reference", "<< " + fn + " /Length 6 0 R >>\nstream\n" + program + "\nendstream", 0},
		{"another /Domain", stream("/FunctionType 4 /Domain [0 2] /Range [0 1]", program), 6},
		{"another program", stream(fn, "{ pop 0.25 }"), 6},
		{"a /DL entry", stream(fn+" /DL 11", program), 6},
		{"the program under another filter", stream(fn+" /Filter /ASCIIHexDecode", "7b20706f7020302e35207d>"), 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /ColorSpace << "+
					"/CS1 [/Separation /Spot /DeviceGray 5 0 R] /CS2 [/Separation /Spot /DeviceGray 7 0 R] >> >> /Contents 4 0 R >>",
				stream("", content),
				stream(fn, program),
				fmt.Sprint(len(program)),
				tc.second,
			)
			viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_4_4_t02); got != tc.want {
				t.Errorf("6.2.4.4 t2 fired %d times, want %d", got, tc.want)
			}
		})
	}
}
