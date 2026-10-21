package pdf

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// transparentAnnotationPDF draws a blue page under a square annotation whose
// appearance paints red under a constant alpha of 0.5, with /CA 0.5 on the
// annotation too, and an opaque printed circle annotation apart from it.
// widget adds a text field widget whose appearance paints with the same
// transparency.
func transparentAnnotationPDF(widget bool) []byte {
	annots := "/Annots [6 0 R 7 0 R]"
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if widget {
		annots = "/Annots [6 0 R 7 0 R 9 0 R]"
		catalog = "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [9 0 R] >> >>"
	}
	return colorFixturePDF([]string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R " + annots + " >>",
		colorStream("", "0 0 1 rg 0 0 100 200 re f"),
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 >>",
		"<< /Type /Annot /Subtype /Square /Rect [50 50 150 150] /F 4 /CA 0.5 /Contents (note) /AP << /N 8 0 R >> >>",
		"<< /Type /Annot /Subtype /Circle /Rect [160 160 190 190] /F 4 /AP << /N 10 0 R >> >>",
		colorStream("/Type /XObject /Subtype /Form /BBox [50 50 150 150] /Resources << /ExtGState << /G 5 0 R >> >>",
			"/G gs 1 0 0 rg 50 50 100 100 re f"),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /V (x) /Rect [10 160 40 190] /F 4 /AP << /N 8 0 R >> >>",
		colorStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "0 1 0 rg 0 0 10 10 re f"),
	})
}

// clause64 counts the ISO 19005-1, 6.4 violations PDF/A-1b validation finds
// in data.
func clause64(t *testing.T, data []byte) int {
	t.Helper()
	r := openColorPDF(t, data)
	violations, err := r.ValidatePDFAReport(context.Background(), models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}
	n := 0
	for _, v := range violations {
		if v.Clause == "6.4" || strings.HasPrefix(v.Clause, "6.4.") {
			n++
		}
	}
	return n
}

// TestFlattenTransparencyBakesAnnotations pins that an annotation painting
// with transparency is drawn into the page and flattened with it, stays as
// an annotation that paints nothing, and leaves no transparency ISO 19005-1,
// 6.4 reads, while the page looks as it did.
func TestFlattenTransparencyBakesAnnotations(t *testing.T) {
	t.Parallel()
	in := transparentAnnotationPDF(false)
	if clause64(t, in) == 0 {
		t.Fatal("the source shows no transparency to PDF/A-1b: the check cannot see it")
	}
	report, out := flattenPDF(t, in)
	if len(report.Pages) != 1 || report.Pages[0].Annotations != 1 || report.Pages[0].Regions == 0 {
		t.Fatalf("report %+v, want one annotation drawn and composited", report)
	}
	if report.TransparentAnnotations != 0 {
		t.Errorf("TransparentAnnotations = %d, want 0", report.TransparentAnnotations)
	}
	if n := clause64(t, out); n != 0 {
		t.Errorf("the output fails ISO 19005-1, 6.4 %d times", n)
	}
	before, after := openColorPDF(t, in), openColorPDF(t, out)
	if d := pixelsApart(renderRGBA(t, before, 72), renderRGBA(t, after, 72), 8); d > 0.01 {
		t.Errorf("%.2f%% of the pixels differ", 100*d)
	}
	annots, err := after.ExtractAnnotations(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(annots) != 2 {
		t.Errorf("the output holds %d annotations, want both kept", len(annots))
	}
	again := NewWriter(after)
	defer again.Close()
	if r, err := again.FlattenTransparency(context.Background()); err != nil || len(r.Pages) != 0 {
		t.Errorf("a second flattening changes %+v (%v), want nothing", r, err)
	}
}

// TestFlattenTransparencyLeavesWidgets pins that a form field's widget is
// left as it is and counted: baking it is flattening the form.
func TestFlattenTransparencyLeavesWidgets(t *testing.T) {
	t.Parallel()
	report, _ := flattenPDF(t, transparentAnnotationPDF(true))
	if report.TransparentAnnotations != 1 {
		t.Errorf("TransparentAnnotations = %d, want the widget", report.TransparentAnnotations)
	}
	if len(report.Pages) != 1 || report.Pages[0].Annotations != 1 {
		t.Errorf("report %+v, want the square drawn alone", report)
	}
}

// TestFlattenTransparencyPrintedAnnotations pins
// [WithFlattenPrintedAnnotations]: the opaque printed circle is drawn too.
func TestFlattenTransparencyPrintedAnnotations(t *testing.T) {
	t.Parallel()
	report, _ := flattenPDF(t, transparentAnnotationPDF(true), WithFlattenPrintedAnnotations())
	if len(report.Pages) != 1 || report.Pages[0].Annotations != 2 {
		t.Errorf("report %+v, want the square and the circle drawn", report)
	}
	if report.TransparentAnnotations != 1 {
		t.Errorf("TransparentAnnotations = %d, want the widget left", report.TransparentAnnotations)
	}
}

// transparentSignatureObjs returns [certifiedObjs] with the certifying
// signature's appearance painting under a constant alpha of 0.5, and, with
// page, page 1 painting with that transparency too.
func transparentSignatureObjs(page bool) []string {
	objs := certifiedObjs()
	objs[7] = colorStream("/Type /XObject /Subtype /Form /BBox [0 0 200 50] "+
		"/Resources << /Font << /Helv 9 0 R >> /ExtGState << /G << /ca 0.5 >> >> >>",
		"q /G gs 1 0 0 rg 0 0 200 50 re f Q BT /Helv 10 Tf 5 20 Td (Signed by Jane) Tj ET")
	if page {
		objs[2] = strings.Replace(objs[2], "/Annots", "/Contents 18 0 R /Resources << /ExtGState << /G 19 0 R >> >> /Annots", 1)
		objs = append(objs,
			colorStream("", "0 0 1 rg 0 0 300 300 re f q /G gs 0 1 0 rg 100 100 300 300 re f Q"),
			"<< /Type /ExtGState /ca 0.5 >>")
	}
	return objs
}

// TestFlattenTransparencyDrawsTransparentSignatures pins that a signed
// signature whose appearance paints with transparency is drawn into its page
// before the flattening when the flattening rewrites the document, so that
// transparency is flattened too: the rewrite leaves the signature out and
// draws its appearance all the same (see [Writer.DroppedSignatures]).
func TestFlattenTransparencyDrawsTransparentSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := buildPDF(transparentSignatureObjs(true))
	out, dropped := writeSigned(ctx, t, in, func(w *Writer) []WriteOption {
		report, err := w.FlattenTransparency(ctx)
		if err != nil {
			t.Fatalf("FlattenTransparency: %v", err)
		}
		if report.TransparentAnnotations != 0 {
			t.Errorf("report %+v, want the signature's transparency flattened", report)
		}
		return nil
	})
	if !slices.Equal(dropped, certifiedDropped) {
		t.Errorf("DroppedSignatures = %v, want %v", dropped, certifiedDropped)
	}
	if n := clause64(t, out); n != 0 {
		t.Errorf("the output fails ISO 19005-1, 6.4 %d times", n)
	}
	if text := pageText(ctx, t, out, 1); !strings.Contains(text, "Signed by Jane") {
		t.Errorf("page 1 text = %q, want the signature's", text)
	}
}

// TestFlattenTransparencyKeepsAValidSignature pins that a signed signature
// whose appearance is the only transparency of the document is left as it
// is, and counted: dropping it to flatten its look would break a signature
// an incremental write keeps valid.
func TestFlattenTransparencyKeepsAValidSignature(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := buildPDF(transparentSignatureObjs(false))
	if clause64(t, in) == 0 {
		t.Fatal("the source shows no transparency to PDF/A-1b: the check cannot see it")
	}
	_, dropped := writeSigned(ctx, t, in, func(w *Writer) []WriteOption {
		report, err := w.FlattenTransparency(ctx)
		if err != nil {
			t.Fatalf("FlattenTransparency: %v", err)
		}
		if len(report.Pages) != 0 || report.TransparentAnnotations != 1 {
			t.Errorf("report %+v, want nothing flattened and the signature counted", report)
		}
		return nil
	})
	if len(dropped) != 0 {
		t.Errorf("DroppedSignatures = %v, want none", dropped)
	}
}
