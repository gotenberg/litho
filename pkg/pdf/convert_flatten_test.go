package pdf

import (
	"bytes"
	"context"
	"testing"
)

// transparentRGBPDF draws in DeviceRGB a page whose transparency the tests
// flatten: a half-transparent red square over a blue backdrop, and a
// multiply-blended green square. group is the page's /Group entry, empty for
// none.
func transparentRGBPDF(group string) []byte {
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R " + group +
			" /Resources << /ExtGState << /Half 5 0 R /Mul 6 0 R >> >> >>",
		colorStream("", "0 0 1 rg 0 0 200 100 re f q /Half gs 1 0 0 rg 20 40 80 80 re f Q q /Mul gs 0 1 0 rg 120 60 60 60 re f Q"),
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 >>",
		"<< /Type /ExtGState /BM /Multiply >>",
	})
}

// writeOut writes w and returns the bytes.
func writeOut(t *testing.T, w *Writer) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// convertCMYK converts data to the built-in CMYK target with opts.
func convertCMYK(t *testing.T, data []byte, opts ...ColorOption) []byte {
	t.Helper()
	w := NewWriter(openColorPDF(t, data))
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.ConvertColors(context.Background(), CMYKColorTarget(), opts...); err != nil {
		t.Fatalf("ConvertColors: %v", err)
	}
	return writeOut(t, w)
}

// requireCMYKOnly fails unless page 1 of data paints in DeviceCMYK alone and
// draws at least one composite image, in DeviceCMYK.
func requireCMYKOnly(t *testing.T, data []byte) {
	t.Helper()
	ctx := context.Background()
	r := openColorPDF(t, data)
	uses, err := r.ExtractColorSpaces(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractColorSpaces: %v", err)
	}
	if len(uses) == 0 {
		t.Fatal("no colour space use")
	}
	for _, u := range uses {
		if u.Family != "DeviceCMYK" {
			t.Errorf("%s: outside the CMYK target", describeUse(u))
		}
	}
	images, err := r.ExtractImages(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractImages: %v", err)
	}
	if len(images) == 0 {
		t.Fatal("no composite image")
	}
	for _, im := range images {
		if im.ColorSpace != "DeviceCMYK" {
			t.Errorf("image %s in %s, want DeviceCMYK", im.Filename, im.ColorSpace)
		}
	}
}

// TestConvertColorsThenFlattenStaysCMYK checks that flattening a document
// converted to CMYK composites its page group in CMYK, so the conversion's
// output stays in its target: the conversion makes the CMYK target the output
// intent, and a page group that names no /CS composites in the intent's
// device space (ISO 32000-1, 11.4.7, Table 147, 14.11.5).
func TestConvertColorsThenFlattenStaysCMYK(t *testing.T) {
	t.Parallel()
	for _, group := range []string{"", "/Group << /S /Transparency >>"} {
		converted := convertCMYK(t, transparentRGBPDF(group))
		_, out := flattenPDF(t, converted)
		requireCMYKOnly(t, out)
	}
}

// TestConvertColorsNamesThePageGroupSpace checks that a CMYK conversion that
// leaves the output intents alone gives a page group that names no space
// the target, so a flattening composites in it, and that an RGB conversion
// leaves the group unnamed.
func TestConvertColorsNamesThePageGroupSpace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	converted := convertCMYK(t, transparentRGBPDF("/Group << /S /Transparency >>"), WithoutOutputIntent())
	r := openColorPDF(t, converted)
	uses, err := r.ExtractColorSpaces(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractColorSpaces: %v", err)
	}
	named := false
	for _, u := range uses {
		named = named || u.Source == "group" && u.Family == "DeviceCMYK"
	}
	if !named {
		t.Errorf("page group does not blend in DeviceCMYK: %+v", uses)
	}
	_, out := flattenPDF(t, converted)
	requireCMYKOnly(t, out)

	w := NewWriter(openColorPDF(t, transparentRGBPDF("/Group << /S /Transparency >>")))
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.ConvertColors(ctx, RGBColorTarget()); err != nil {
		t.Fatalf("ConvertColors: %v", err)
	}
	if bytes.Contains(writeOut(t, w), []byte("/CS")) {
		t.Error("an RGB conversion named the page group's space")
	}
}

// TestFlattenCompositesInTheOutputIntentSpace checks that one Writer
// converting to CMYK and then flattening writes CMYK composites, and that a
// document with no grey or CMYK output intent still composites in sRGB.
func TestFlattenCompositesInTheOutputIntentSpace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, transparentRGBPDF("")))
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.ConvertColors(ctx, CMYKColorTarget()); err != nil {
		t.Fatalf("ConvertColors: %v", err)
	}
	if _, err := w.FlattenTransparency(ctx); err != nil {
		t.Fatalf("FlattenTransparency: %v", err)
	}
	requireCMYKOnly(t, writeOut(t, w))

	_, out := flattenPDF(t, transparentRGBPDF(""))
	images, err := openColorPDF(t, out).ExtractImages(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractImages: %v", err)
	}
	if len(images) == 0 {
		t.Fatal("no composite image")
	}
	for _, im := range images {
		if im.ColorSpace != "DeviceRGB" {
			t.Errorf("image %s in %s, want sRGB", im.Filename, im.ColorSpace)
		}
	}
}
