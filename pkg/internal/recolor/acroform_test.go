package recolor

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// daText returns the bytes of a /DA string, or the object spelled out when it
// is no string.
func daText(obj ast.PdfObject) string {
	if s, ok := obj.(ast.PdfString); ok {
		return string(s)
	}
	return fmt.Sprint(obj)
}

// formPDF is a one-page document whose catalog names the interactive form
// acroForm: objects 1 to 4 are the catalog, the page tree, the page with
// pageDict's entries and its empty content, and objs follow from 5.
func formPDF(acroForm, pageDict string, objs ...string) []byte {
	return buildPDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm " + acroForm + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 40 40] /Contents 4 0 R " + pageDict + " >>",
		stream("", ""),
	}, objs...))
}

// TestDefaultAppearanceColour checks that the colour operators of a /DA (ISO
// 32000-1, 12.7.3.3) convert to the target, on a widget the page reaches, on
// a field above it, and on the form, while the font operator keeps its
// bytes, and that a colour already in the target stays.
func TestDefaultAppearanceColour(t *testing.T) {
	t.Parallel()
	src := formPDF("<< /Fields [5 0 R 7 0 R] /DA (/Helv 0 Tf 1 0 0 rg) >>", "/Annots [6 0 R 7 0 R]",
		"<< /T (parent) /FT /Tx /DA (0 g /Helv 12 Tf) /Kids [6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [0 0 9 9] /DA (/Helv 9 Tf 0 0 1 rg 0 0 1 RG) >>",
		"<< /Type /Annot /Subtype /Widget /T (kept) /FT /Tx /Rect [0 0 9 9] /DA (/Helv 9 Tf 0 0 0 1 k) >>")
	r := recolorBytes(t, src, cmyk, naive(Options{}))

	for num, want := range map[int]string{
		5: "0 0 0 1 k /Helv 12 Tf",
		6: "/Helv 9 Tf 1 1 0 0 k 1 1 0 0 K",
		7: "/Helv 9 Tf 0 0 0 1 k",
	} {
		if got := daText(r.dict(ast.PdfIndirectRef{ObjectNum: num})["DA"]); got != want {
			t.Errorf("object %d /DA: %q, want %q", num, got, want)
		}
	}
	catalog := r.dict(r.doc.Index.Trailer["Root"])
	if got := daText(r.dict(catalog["AcroForm"])["DA"]); got != "/Helv 0 Tf 0 1 1 0 k" {
		t.Errorf("form /DA: %q", got)
	}
	if len(r.report.Skipped) > 0 {
		t.Errorf("skipped: %+v", r.report.Skipped)
	}
}

// TestDefaultAppearanceGrayTarget checks that a grey target writes a /DA's
// colour with g, and that an indirect /DA string is rewritten in place.
func TestDefaultAppearanceGrayTarget(t *testing.T) {
	t.Parallel()
	src := formPDF("6 0 R", "",
		"(/Helv 0 Tf 1 1 1 rg)",
		"<< /Fields [] /DA 5 0 R >>")
	r := recolorBytes(t, src, Target{Family: "DeviceGray"}, naive(Options{}))
	if got := daText(r.object(5)); got != "/Helv 0 Tf 1 g" {
		t.Errorf("indirect /DA: %q, want %q", got, "/Helv 0 Tf 1 g")
	}
}

// TestDefaultAppearanceUnchanged checks that a /DA with no device colour
// operator, or one whose operands are not numbers, stays as it was.
func TestDefaultAppearanceUnchanged(t *testing.T) {
	t.Parallel()
	src := formPDF("<< /Fields [5 0 R] >>", "",
		"<< /T (a) /FT /Tx /DA (/Helv 9 Tf /X rg) /Kids [6 0 R] >>",
		"<< /T (b) /Parent 5 0 R /DA (/Helv 9 Tf) >>")
	r := recolorBytes(t, src, cmyk, naive(Options{}))
	for num, want := range map[int]string{5: "/Helv 9 Tf /X rg", 6: "/Helv 9 Tf"} {
		if got := daText(r.dict(ast.PdfIndirectRef{ObjectNum: num})["DA"]); got != want {
			t.Errorf("object %d /DA: %q, want %q", num, got, want)
		}
	}
}
