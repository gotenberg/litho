package pdf

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// convertBuiltin runs ConvertColors with the built-in engine and writes the
// output.
func convertBuiltin(t *testing.T, src []byte, target ColorTarget, opts ...ColorOption) (*ColorReport, []byte) {
	t.Helper()
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, src))
	t.Cleanup(func() { _ = w.Close() })
	report, err := w.ConvertColors(ctx, target, opts...)
	if err != nil {
		t.Fatalf("ConvertColors: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return report, out.Bytes()
}

// firstPageOps returns the operations of data's first page.
func firstPageOps(t *testing.T, data []byte) []content.Operation {
	t.Helper()
	ctx := context.Background()
	r := openColorPDF(t, data)
	pages, err := r.resolvePages(ctx)
	if err != nil || len(pages) == 0 {
		t.Fatalf("pages: %d, %v", len(pages), err)
	}
	ops, err := content.ParsePageContent(ctx, r.doc, pages[0].Dict, content.WholeStream())
	if err != nil {
		t.Fatal(err)
	}
	return ops
}

// operands returns the numeric operands of the first op operator.
func operands(t *testing.T, ops []content.Operation, op string) []float64 {
	t.Helper()
	for _, o := range ops {
		if o.Operator != op {
			continue
		}
		out := make([]float64, len(o.Operands))
		for i, v := range o.Operands {
			switch n := v.(type) {
			case ast.PdfInt:
				out[i] = float64(n)
			case ast.PdfReal:
				out[i] = float64(n)
			}
		}
		return out
	}
	t.Fatalf("no %s operator in %v", op, ops)
	return nil
}

// linked converts in from src to dst relative colorimetrically.
func linked(t *testing.T, src, dst []byte, in []float64) []float64 {
	t.Helper()
	sp, err := icc.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	dp, err := icc.Parse(dst)
	if err != nil {
		t.Fatal(err)
	}
	l, err := icc.NewLink(sp, dp, icc.TransformOptions{Intent: icc.RelativeColorimetric})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float64, l.OutputChannels())
	l.Convert(in, out)
	return out
}

func near4(got, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-3 {
			return false
		}
	}
	return true
}

// TestConvertColorsBuiltin_ToCMYK converts DeviceRGB colour to the built-in
// CMYK target through the ICC engine, relative colorimetrically, and the
// output paints in DeviceCMYK alone.
func TestConvertColorsBuiltin_ToCMYK(t *testing.T) {
	t.Parallel()
	report, out := convertBuiltin(t, recolorPDF(""), CMYKColorTarget())
	if report.ConvertedOperators != 1 || report.Target != "DeviceCMYK" {
		t.Errorf("report = %+v", report)
	}
	got := operands(t, firstPageOps(t, out), "k")
	if want := linked(t, icc.SRGBProfile(), icc.CMYKPrinterProfile(), []float64{1, 0, 0}); !near4(got, want) {
		t.Errorf("red = %v k, want %v", got, want)
	}
	uses, err := openColorPDF(t, out).ExtractColorSpaces(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range uses {
		if u.Family != "DeviceCMYK" {
			t.Errorf("the output paints in %s", u.Family)
		}
	}
}

// TestConvertColorsBuiltin_OutputIntentMeansTheDeviceSpace reads DeviceRGB
// through the document's RGB output intent, as the renderer does, not as
// sRGB.
func TestConvertColorsBuiltin_OutputIntentMeansTheDeviceSpace(t *testing.T) {
	t.Parallel()
	profile, err := os.ReadFile("../internal/graphics/icc/testdata/v4-rgb-display.icc")
	if err != nil {
		t.Fatal(err)
	}
	src := recolorPDF("/OutputIntents [<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (Custom) /DestOutputProfile 5 0 R >>]",
		colorStream("/N 3", string(profile)))
	_, out := convertBuiltin(t, src, CMYKColorTarget(), WithoutOutputIntent())
	got := operands(t, firstPageOps(t, out), "k")
	want := linked(t, profile, icc.CMYKPrinterProfile(), []float64{1, 0, 0})
	srgb := linked(t, icc.SRGBProfile(), icc.CMYKPrinterProfile(), []float64{1, 0, 0})
	if !near4(got, want) || near4(got, srgb) {
		t.Errorf("red = %v k, want the intent's reading %v, not sRGB's %v", got, want, srgb)
	}
}

// TestConvertColorsBuiltin_ICCTarget writes the converted colour in an
// ICCBased space over the caller's profile, named in the page's resources.
func TestConvertColorsBuiltin_ICCTarget(t *testing.T) {
	t.Parallel()
	_, out := convertBuiltin(t, recolorPDF(""), ICCColorTarget(icc.CMYKPrinterProfile()), WithoutOutputIntent())
	ops := firstPageOps(t, out)
	if len(ops) < 2 || ops[0].Operator != "cs" || ops[1].Operator != "sc" || len(ops[1].Operands) != 4 {
		t.Fatalf("operators %v, want cs and sc of four components", ops)
	}
	uses, err := openColorPDF(t, out).ExtractColorSpaces(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) == 0 || uses[0].Family != "ICCBased" || uses[0].Components != 4 {
		t.Errorf("uses = %+v, want ICCBased of 4 components", uses)
	}
}

// TestConvertColorsBuiltin_KeepsSpotsAndConvertsImages keeps a Separation,
// naming its colorant, and re-encodes an RGB image in the target.
func TestConvertColorsBuiltin_KeepsSpotsAndConvertsImages(t *testing.T) {
	t.Parallel()
	src := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R " +
			"/Resources << /ColorSpace << /S1 [/Separation /Gold /DeviceRGB << /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [1 0.8 0] /N 1 >>] >> /XObject << /Im0 5 0 R >> >> >>",
		colorStream("", "/S1 cs 1 sc 0 0 50 50 re f q 50 0 0 50 0 0 cm /Im0 Do Q"),
		colorStream("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\xff\x00\x00\x00\x00\xff"),
	})
	report, out := convertBuiltin(t, src, CMYKColorTarget(), WithoutOutputIntent())
	if !slices.Equal(report.KeptSpotColorants, []string{"Gold"}) || report.ConvertedImages != 1 {
		t.Errorf("report = %+v, want Gold kept and one image converted", report)
	}
	uses, err := openColorPDF(t, out).ExtractColorSpaces(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range uses {
		ok := u.Family == "DeviceCMYK" || u.Family == "Separation" && u.Alternate != nil && u.Alternate.Family == "DeviceCMYK"
		if !ok {
			t.Errorf("the output paints in %+v", u)
		}
	}
}

// TestConvertColorsBuiltin_RefusesWhatItLeaves refuses a document whose image
// samples do not decode: converting the rest would leave the image in another
// space.
func TestConvertColorsBuiltin_RefusesWhatItLeaves(t *testing.T) {
	t.Parallel()
	src := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>",
		colorStream("", "1 0 0 rg /Im0 Do"),
		colorStream("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode", "not flate"),
	})
	w := NewWriter(openColorPDF(t, src))
	t.Cleanup(func() { _ = w.Close() })
	_, err := w.ConvertColors(context.Background(), CMYKColorTarget())
	if !errors.Is(err, ErrColorConversionRefused) || !errors.Is(err, errColorsLeft) {
		t.Fatalf("err = %v, want ErrColorConversionRefused for the image left", err)
	}
	if w.forceRewrite || w.rewriteOverrides != nil {
		t.Error("a refused conversion changed the Writer")
	}
}
