package image

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// tintedImagePDF builds a one-page document drawing a 4x1 image in a Separation
// or DeviceN colour space. cs is the colour space array, written inline.
func tintedImagePDF(cs string, samples []byte) []string {
	data := string(samples)
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 4 /Height 1"+
			" /ColorSpace %s /BitsPerComponent 8 /Length %d >>\nstream\n%s\nendstream",
			cs, len(data), data),
	}
}

// TestDecodeImage_DeviceNTintArityIsChecked pins that a tint transform whose
// input count does not match the colorant count is refused BY NAME.
//
// Per ISO 32000-1, 8.6.6.5 the transform takes one input per colorant. A file
// declaring seven colorants and a one-input transform is malformed, and letting
// it reach [function.Function.Eval] reports "wrong number of inputs: got 7,
// want 1", which names neither the colour space nor the image. The refusal here
// names both counts.
func TestDecodeImage_DeviceNTintArityIsChecked(t *testing.T) {
	t.Parallel()

	const cs = "[/DeviceN [/Cyan /Magenta /Yellow /Black /Spot1 /Spot2 /Spot3]" +
		" /DeviceRGB 5 0 R]"
	samples := make([]byte, 4*7)
	for i := range samples {
		samples[i] = byte(i * 9)
	}

	// A type 2 function takes exactly one input, so this is the mismatch.
	objs := append(tintedImagePDF(cs, samples),
		"<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>")
	ctx, doc, page := imagePDF(t, objs)

	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil {
		t.Fatal("DecodeImage accepted a 7-colorant space with a one-input tint transform")
	}
	if !strings.Contains(err.Error(), "devicen") {
		t.Errorf("refusal does not name the colour space family: %v", err)
	}
	if !strings.Contains(err.Error(), "7") {
		t.Errorf("refusal does not name the colorant count: %v", err)
	}
}

// TestDecodeImage_DeviceNDecodesThroughItsTintTransform pins the decode this
// replaces the old refusal with.
//
// A DeviceN image carries one sample per colorant and the transform converts
// the whole tuple at once (ISO 32000-1, 8.6.6.5), so it cannot be reduced to a
// 256-entry table the way a Separation can. Before this it was refused
// outright, and before THAT it was read as three-component RGB, which strides
// a seven-colorant image across pixel boundaries.
//
// The table is a two-input, one-output sampled function over a DeviceGray
// alternate, and the expected greys are computed from the algorithm rather than
// from this decoder: with the first input varying fastest, cells are
// (0,0)=0 (1,0)=85 (0,1)=170 (1,1)=255.
func TestDecodeImage_DeviceNDecodesThroughItsTintTransform(t *testing.T) {
	t.Parallel()

	const cs = "[/DeviceN [/A /B] /DeviceGray 5 0 R]"
	// Four pixels, two colorants each: (0,0) (255,0) (0,255) (255,255).
	samples := []byte{0, 0, 255, 0, 0, 255, 255, 255}
	table := []byte{0, 85, 170, 255}

	objs := append(tintedImagePDF(cs, samples),
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1 0 1] /Range [0 1]"+
			" /Size [2 2] /BitsPerSample 8 /Length %d >>\nstream\n%s\nendstream",
			len(table), table))
	ctx, doc, page := imagePDF(t, objs)

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage refused a DeviceN image: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 4 || got.Dy() != 1 {
		t.Fatalf("bounds = %v, want 4x1", got)
	}

	for _, tc := range []struct {
		x    int
		want uint8
		note string
	}{
		{0, 0, "tuple (0,0)"},
		{1, 85, "tuple (255,0): the FIRST colorant varies fastest"},
		{2, 170, "tuple (0,255)"},
		{3, 255, "tuple (255,255)"},
	} {
		r, g, b, _ := img.At(tc.x, 0).RGBA()
		if uint8(r>>8) != tc.want || uint8(g>>8) != tc.want || uint8(b>>8) != tc.want {
			t.Errorf("pixel %d [%s] = (%d,%d,%d), want grey %d",
				tc.x, tc.note, r>>8, g>>8, b>>8, tc.want)
		}
	}
}

// TestDecodeImage_DeviceNRepeatedTuplesUseTheCache pins that a repeated tuple
// decodes to the same colour, which is what the tuple cache trades memory for.
//
// The cache exists because the transform cannot be tabulated over 256^N: the
// largest corpus image is 540x534 over seven colorants and holds 31,400
// distinct tuples, so evaluating per distinct tuple rather than per pixel turns
// 36.9 million corner visits into 4.0 million.
func TestDecodeImage_DeviceNRepeatedTuplesUseTheCache(t *testing.T) {
	t.Parallel()

	const cs = "[/DeviceN [/A /B] /DeviceGray 5 0 R]"
	// The same two tuples alternating, so every pixel after the first two is a
	// cache hit.
	samples := []byte{0, 0, 255, 255, 0, 0, 255, 255}
	table := []byte{0, 85, 170, 255}

	objs := append(tintedImagePDF(cs, samples),
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1 0 1] /Range [0 1]"+
			" /Size [2 2] /BitsPerSample 8 /Length %d >>\nstream\n%s\nendstream",
			len(table), table))
	ctx, doc, page := imagePDF(t, objs)

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	for _, p := range [][2]int{{0, 2}, {1, 3}} {
		ra, _, _, _ := img.At(p[0], 0).RGBA()
		rb, _, _, _ := img.At(p[1], 0).RGBA()
		if ra != rb {
			t.Errorf("pixels %d and %d hold the same tuple but decoded to %d and %d",
				p[0], p[1], ra>>8, rb>>8)
		}
	}
}

// TestDecodeImage_SeparationDecodesThroughItsTintTransform pins that a
// Separation image is decoded through its tint transform.
//
// A Separation space has exactly one component whose value is ink coverage
// (ISO 32000-1, 8.6.6.4). Read as the substituted three-component "DeviceRGB",
// complete data came back as "decoded data too short for RGB image" at a ratio
// of exactly 3.0000, blaming the file; where a JPEG's shape was measured
// instead, the tint was emitted as a grey LEVEL and the image rendered
// INVERTED, because every tint transform measured maps tint 0 to paper white
// and full tint to near black.
//
// The expected colours are computed from the transform, not from this decoder:
// C0 white to C1 black with N=1 is rgb = (1-t) per channel.
func TestDecodeImage_SeparationDecodesThroughItsTintTransform(t *testing.T) {
	t.Parallel()

	const cs = "[/Separation /Spot1 /DeviceRGB 5 0 R]"
	objs := append(tintedImagePDF(cs, []byte{0, 64, 128, 255}),
		"<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1]"+
			" /C0 [1 1 1] /C1 [0 0 0] /N 1 >>")
	ctx, doc, page := imagePDF(t, objs)

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage refused a Separation image: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 4 || got.Dy() != 1 {
		t.Fatalf("bounds = %v, want 4x1", got)
	}

	// Tint 0 is NO ink, so it must render as paper white, not black. That sign
	// is the whole defect: measured on thread_200_200192.pdf, three images and
	// 687,592 pixels came back with not one pixel above luminance 192, against
	// poppler's MEAN_LUM=221.91 for the same page.
	for _, tc := range []struct {
		x    int
		want color.RGBA
	}{
		{0, color.RGBA{R: 255, G: 255, B: 255, A: 255}},
		{1, color.RGBA{R: 191, G: 191, B: 191, A: 255}},
		{2, color.RGBA{R: 127, G: 127, B: 127, A: 255}},
		{3, color.RGBA{R: 0, G: 0, B: 0, A: 255}},
	} {
		r, g, b, a := img.At(tc.x, 0).RGBA()
		got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
		if got != tc.want {
			t.Errorf("pixel %d = %v, want %v", tc.x, got, tc.want)
		}
	}
}

// TestDecodeImage_SeparationOverCMYKAlternate pins the alternate-space
// conversion, where the tint yields four values rather than three.
func TestDecodeImage_SeparationOverCMYKAlternate(t *testing.T) {
	t.Parallel()

	const cs = "[/Separation /Black /DeviceCMYK 5 0 R]"
	objs := append(tintedImagePDF(cs, []byte{0, 128, 255, 255}),
		"<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1 0 1]"+
			" /C0 [0 0 0 0] /C1 [0 0 0 1] /N 1 >>")
	ctx, doc, page := imagePDF(t, objs)

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	for _, tc := range []struct {
		x    int
		want uint8
	}{{0, 255}, {1, 127}, {2, 0}} {
		r, g, b, _ := img.At(tc.x, 0).RGBA()
		if uint8(r>>8) != tc.want || uint8(g>>8) != tc.want || uint8(b>>8) != tc.want {
			t.Errorf("pixel %d = (%d,%d,%d), want grey %d", tc.x, r>>8, g>>8, b>>8, tc.want)
		}
	}
}

// TestDecodeImage_SeparationWithSampledTint pins a type 0 tint transform, the
// commonest kind: 74 of the 112 Separation images in govdocs use one.
func TestDecodeImage_SeparationWithSampledTint(t *testing.T) {
	t.Parallel()

	samples := []byte{255, 255, 255, 128, 128, 128, 0, 0, 0}
	const cs = "[/Separation /Spot1 /DeviceRGB 5 0 R]"
	objs := append(tintedImagePDF(cs, []byte{0, 128, 255, 255}),
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1] /Range [0 1 0 1 0 1]"+
			" /Size [3] /BitsPerSample 8 /Encode [0 2] /Length %d >>\nstream\n%s\nendstream",
			len(samples), samples))
	ctx, doc, page := imagePDF(t, objs)

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	for _, tc := range []struct {
		x    int
		want uint8
	}{{0, 255}, {1, 127}, {2, 0}} {
		r, _, _, _ := img.At(tc.x, 0).RGBA()
		if uint8(r>>8) != tc.want {
			t.Errorf("pixel %d = %d, want %d", tc.x, r>>8, tc.want)
		}
	}
}

// TestDecodeImage_SeparationRefusalsSurvive pins the cases the tint path still
// refuses, so decoding one Separation image does not become decoding anything
// shaped vaguely like one.
func TestDecodeImage_SeparationRefusalsSurvive(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		cs   string
		fn   string
		want string
	}{
		{
			"an alternate space with no component count",
			"[/Separation /Spot1 [/Lab << /WhitePoint [0.9505 1 1.089] >>] 5 0 R]",
			"<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>",
			"unsupported alternate",
		},
		{
			"a stitching tint transform, which this decoder does not evaluate",
			"[/Separation /Spot1 /DeviceRGB 5 0 R]",
			"<< /FunctionType 3 /Domain [0 1] /Range [0 1 0 1 0 1] /Bounds [] /Encode [0 1] /Functions [] >>",
			"tint transform",
		},
		{
			"a tint transform whose outputs do not fit the alternate space",
			"[/Separation /Spot1 /DeviceCMYK 5 0 R]",
			"<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>",
			"yields 3 values",
		},
	} {
		objs := append(tintedImagePDF(tc.cs, []byte{0, 64, 128, 255}), tc.fn)
		ctx, doc, page := imagePDF(t, objs)

		_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
		if err == nil {
			t.Errorf("%s: DecodeImage accepted it", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: refusal %q does not mention %q", tc.name, err, tc.want)
		}
	}
}

// TestDecodeImage_DeviceNThreeColorantsGoThroughTheTransform pins that a
// three-colorant DeviceN is transformed rather than passed through.
//
// This case used to decode by accident: resolveColorSpace substitutes
// "DeviceRGB", which assumes three components, so the stride happened to be
// right and the tints were emitted AS IF they were RGB. The pdfjs fixtures
// colorspace_atan.pdf, colorspace_cos.pdf and colorspace_sin.pdf are exactly
// that shape, and all three rendered at MEAN_LUM 112.47 with a black corner
// against poppler's ~226: litho produced the same picture for three DIFFERENT
// tint programs, which is the tell that the transform was ignored.
//
// Here the transform maps every tuple to full red, so a passthrough would show
// the raw samples instead and the assertion fails.
func TestDecodeImage_DeviceNThreeColorantsGoThroughTheTransform(t *testing.T) {
	t.Parallel()

	const cs = "[/DeviceN [/X /Y /Z] /DeviceRGB 5 0 R]"
	samples := []byte{
		255, 0, 0,
		0, 255, 0,
		0, 0, 255,
		255, 255, 255,
	}
	// Three inputs, three outputs, constant red whatever the tuple.
	program := "{ pop pop pop 1 0 0 }"
	objs := append(tintedImagePDF(cs, samples),
		fmt.Sprintf("<< /FunctionType 4 /Domain [0 1 0 1 0 1] /Range [0 1 0 1 0 1]"+
			" /Length %d >>\nstream\n%s\nendstream", len(program), program))
	ctx, doc, page := imagePDF(t, objs)

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage refused a 3-colorant DeviceN image: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 4 || got.Dy() != 1 {
		t.Fatalf("bounds = %v, want 4x1", got)
	}
	for x := range 4 {
		r, g, b, _ := img.At(x, 0).RGBA()
		if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
			t.Errorf("pixel %d = (%d,%d,%d), want the TRANSFORM's red, not the raw samples",
				x, r>>8, g>>8, b>>8)
		}
	}
}

// TestTintedColorSpace pins the colorant count the guard reads.
func TestTintedColorSpace(t *testing.T) {
	t.Parallel()

	ctx, doc, _ := imagePDF(t, jpxImagePDF("/FlateDecode", "/BitsPerComponent 8"))

	for _, tc := range []struct {
		name       string
		dict       ast.PdfDict
		wantFamily string
		wantCount  int
		wantOK     bool
	}{
		{
			"a Separation has exactly one colorant",
			ast.PdfDict{"ColorSpace": ast.PdfArray{
				ast.PdfName("Separation"), ast.PdfName("Spot1"), ast.PdfName("DeviceRGB"),
			}},
			"Separation", 1, true,
		},
		{
			"a DeviceN has one per name",
			ast.PdfDict{"ColorSpace": ast.PdfArray{
				ast.PdfName("DeviceN"),
				ast.PdfArray{ast.PdfName("C"), ast.PdfName("M"), ast.PdfName("Y"), ast.PdfName("K")},
				ast.PdfName("DeviceRGB"),
			}},
			"DeviceN", 4, true,
		},
		{
			"a device space is not tinted",
			ast.PdfDict{"ColorSpace": ast.PdfName("DeviceRGB")},
			"", 0, false,
		},
		{
			"an indexed space is not tinted",
			ast.PdfDict{"ColorSpace": ast.PdfArray{
				ast.PdfName("Indexed"), ast.PdfName("DeviceRGB"), ast.PdfInt(1), ast.PdfString("\xff\x00\x00\x00\xff\x00"),
			}},
			"", 0, false,
		},
	} {
		_, family, count, ok := tintedColorSpace(ctx, doc, tc.dict)
		if ok != tc.wantOK || family != tc.wantFamily || count != tc.wantCount {
			t.Errorf("%s: got (%q, %d, %v), want (%q, %d, %v)",
				tc.name, family, count, ok, tc.wantFamily, tc.wantCount, tc.wantOK)
		}
	}
}
