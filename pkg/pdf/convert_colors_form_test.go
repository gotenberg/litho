package pdf

import (
	"context"
	"strings"
	"testing"
)

// rgbTextFieldPDF holds a text field with a value and no appearance stream,
// whose /DA, and the form's, state an RGB text colour (ISO 32000-1,
// 12.7.3.3), so a flattening generates its appearance from the /DA.
func rgbTextFieldPDF() []byte {
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /DA (/Helv 0 Tf 1 0 0 rg) /DR << /Font << /Helv 6 0 R >> >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Annots [5 0 R] >>",
		colorStream("", "0 0 1 rg 0 0 200 20 re f"),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (litho) /Rect [20 100 180 130] /F 4 /P 3 0 R /DA (/Helv 12 Tf 0 0 1 rg) >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	})
}

// TestConvertColorsThenFlattenFormStaysCMYK checks that a CMYK conversion
// converts a text field's /DA colour, so the appearance a form flattening
// generates from it paints in the target: a /DA's colour operator is left in
// DeviceRGB otherwise.
func TestConvertColorsThenFlattenFormStaysCMYK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	converted := convertCMYK(t, rgbTextFieldPDF())

	w := NewWriter(openColorPDF(t, converted))
	t.Cleanup(func() { _ = w.Close() })
	w.FlattenForm()
	out := writeOut(t, w)

	r := openColorPDF(t, out)
	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "litho") {
		t.Fatalf("the flattened field drew no value: %q", text)
	}
	uses, err := r.ExtractColorSpaces(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractColorSpaces: %v", err)
	}
	if len(uses) < 2 {
		t.Fatalf("%d colour space uses, want the backdrop and the field's text", len(uses))
	}
	for _, u := range uses {
		if u.Family != "DeviceCMYK" {
			t.Errorf("%s: outside the CMYK target", describeUse(u))
		}
	}
}
