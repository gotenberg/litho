package image

import (
	"context"
	"image/color"
	"log/slog"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func TestResolveColorSpace_DirectName(t *testing.T) {
	doc := &document.Document{}
	dict := ast.PdfDict{
		"ColorSpace": ast.PdfName("DeviceGray"),
	}
	got := resolveColorSpace(context.Background(), doc, dict)
	if got != "DeviceGray" {
		t.Errorf("resolveColorSpace = %q, want DeviceGray", got)
	}
}

func TestResolveColorSpace_Missing(t *testing.T) {
	doc := &document.Document{}
	dict := ast.PdfDict{}
	got := resolveColorSpace(context.Background(), doc, dict)
	if got != "DeviceRGB" {
		t.Errorf("resolveColorSpace(missing) = %q, want DeviceRGB", got)
	}
}

func TestResolveColorSpace_ICCBased(t *testing.T) {
	doc := &document.Document{}
	dict := ast.PdfDict{
		"ColorSpace": ast.PdfArray{ast.PdfName("ICCBased"), ast.PdfNull{}},
	}
	got := resolveColorSpace(context.Background(), doc, dict)
	if got != "DeviceRGB" {
		t.Errorf("resolveColorSpace(ICCBased) = %q, want DeviceRGB", got)
	}
}

func TestResolveColorSpace_CalGray(t *testing.T) {
	doc := &document.Document{}
	dict := ast.PdfDict{
		"ColorSpace": ast.PdfArray{ast.PdfName("CalGray"), ast.PdfNull{}},
	}
	got := resolveColorSpace(context.Background(), doc, dict)
	if got != "DeviceGray" {
		t.Errorf("resolveColorSpace(CalGray) = %q, want DeviceGray", got)
	}
}

func TestResolveColorSpace_Indexed(t *testing.T) {
	doc := &document.Document{}
	dict := ast.PdfDict{
		"ColorSpace": ast.PdfArray{
			ast.PdfName("Indexed"),
			ast.PdfName("DeviceCMYK"),
			ast.PdfInt(255),
			ast.PdfString(""),
		},
	}
	got := resolveColorSpace(context.Background(), doc, dict)
	if got != "DeviceCMYK" {
		t.Errorf("resolveColorSpace(Indexed/DeviceCMYK) = %q, want DeviceCMYK", got)
	}
}

func TestResolveColorSpace_Separation(t *testing.T) {
	doc := &document.Document{}
	dict := ast.PdfDict{
		"ColorSpace": ast.PdfArray{ast.PdfName("Separation"), ast.PdfName("Spot1")},
	}
	got := resolveColorSpace(context.Background(), doc, dict)
	if got != "DeviceRGB" {
		t.Errorf("resolveColorSpace(Separation) = %q, want DeviceRGB", got)
	}
}

func TestResolveColorSpace_EmptyArray(t *testing.T) {
	doc := &document.Document{}
	dict := ast.PdfDict{
		"ColorSpace": ast.PdfArray{},
	}
	got := resolveColorSpace(context.Background(), doc, dict)
	if got != "DeviceRGB" {
		t.Errorf("resolveColorSpace(empty array) = %q, want DeviceRGB", got)
	}
}

func TestDecodeImage_DeviceGray(t *testing.T) {
	// 2x2 grayscale image, no filter.
	pixels := []byte{0, 128, 255, 64}
	stream := ast.NewStream(ast.PdfDict{
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(2),
		"Height":           ast.PdfInt(2),
		"ColorSpace":       ast.PdfName("DeviceGray"),
		"BitsPerComponent": ast.PdfInt(8),
	}, pixels)

	doc := &document.Document{}
	page := &document.Page{
		Resources: ast.PdfDict{
			"XObject": ast.PdfDict{
				"Im0": stream,
			},
		},
	}

	img, err := DecodeImage(context.Background(), doc, page, "Im0")
	if err != nil {
		t.Fatal(err)
	}

	bounds := img.Bounds()
	if bounds.Dx() != 2 || bounds.Dy() != 2 {
		t.Fatalf("bounds = %v, want 2x2", bounds)
	}

	// Check pixel (0,0) = gray 0.
	c := img.At(0, 0)
	gray, ok := c.(color.Gray)
	if !ok {
		t.Fatalf("pixel type = %T, want color.Gray", c)
	}
	if gray.Y != 0 {
		t.Errorf("pixel(0,0) = %d, want 0", gray.Y)
	}
}

func TestDecodeImage_DecodeArrayInverts(t *testing.T) {
	// 1x1 grayscale, sample 0. /Decode [1 0] inverts the mapping, so the
	// decoded value is white (255), not black (0). Before /Decode was
	// honored the raw sample (0) was used and the pixel rendered black.
	stream := ast.NewStream(ast.PdfDict{
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(1),
		"Height":           ast.PdfInt(1),
		"ColorSpace":       ast.PdfName("DeviceGray"),
		"BitsPerComponent": ast.PdfInt(8),
		"Decode":           ast.PdfArray{ast.PdfInt(1), ast.PdfInt(0)},
	}, []byte{0})

	doc := &document.Document{Logger: slog.New(slog.DiscardHandler)}
	page := &document.Page{
		Resources: ast.PdfDict{"XObject": ast.PdfDict{"Im0": stream}},
	}

	img, err := DecodeImage(context.Background(), doc, page, "Im0")
	if err != nil {
		t.Fatal(err)
	}
	gray, ok := img.At(0, 0).(color.Gray)
	if !ok {
		t.Fatalf("pixel type = %T, want color.Gray", img.At(0, 0))
	}
	if gray.Y != 255 {
		t.Errorf("inverted pixel(0,0) = %d, want 255", gray.Y)
	}
}

// TestDecodeImage_DecodeArrayLength keeps the leading pairs of a /Decode array
// longer than two numbers per component, as pdfjs issue20668.pdf writes one,
// and ignores one too short to give every component a range.
func TestDecodeImage_DecodeArrayLength(t *testing.T) {
	for _, tc := range []struct {
		name   string
		decode ast.PdfArray
		want   uint8
	}{
		{"trailing entry", ast.PdfArray{ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0)}, 255},
		{"one number", ast.PdfArray{ast.PdfInt(1)}, 0},
	} {
		stream := ast.NewStream(ast.PdfDict{
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(1),
			"Height":           ast.PdfInt(1),
			"ColorSpace":       ast.PdfName("DeviceGray"),
			"BitsPerComponent": ast.PdfInt(8),
			"Decode":           tc.decode,
		}, []byte{0})
		doc := &document.Document{Logger: slog.New(slog.DiscardHandler)}
		page := &document.Page{Resources: ast.PdfDict{"XObject": ast.PdfDict{"Im0": stream}}}
		img, err := DecodeImage(context.Background(), doc, page, "Im0")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := img.At(0, 0).(color.Gray).Y; got != tc.want {
			t.Errorf("%s: pixel = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestBuildDecodeLUTs_IdentityWhenAbsent(t *testing.T) {
	// No /Decode array must yield exact identity tables so output is
	// byte-for-byte unchanged for images that do not declare one.
	luts := buildDecodeLUTs(nil, 3)
	if len(luts) != 3 {
		t.Fatalf("len(luts) = %d, want 3", len(luts))
	}
	for j := range luts {
		for s := range 256 {
			if luts[j][s] != byte(s) {
				t.Fatalf("identity LUT[%d][%d] = %d, want %d", j, s, luts[j][s], s)
			}
		}
	}
}

func TestDecodeImage_DeviceRGB(t *testing.T) {
	// 1x1 RGB image.
	pixels := []byte{255, 0, 128}
	stream := ast.NewStream(ast.PdfDict{
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(1),
		"Height":           ast.PdfInt(1),
		"ColorSpace":       ast.PdfName("DeviceRGB"),
		"BitsPerComponent": ast.PdfInt(8),
	}, pixels)

	doc := &document.Document{}
	page := &document.Page{
		Resources: ast.PdfDict{
			"XObject": ast.PdfDict{
				"Im0": stream,
			},
		},
	}

	img, err := DecodeImage(context.Background(), doc, page, "Im0")
	if err != nil {
		t.Fatal(err)
	}

	r, g, b, a := img.At(0, 0).RGBA()
	// RGBA returns 16-bit values.
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 128 || a>>8 != 255 {
		t.Errorf("pixel(0,0) RGBA = (%d,%d,%d,%d), want (255,0,128,255)", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestDecodeImage_NotFound(t *testing.T) {
	doc := &document.Document{}
	page := &document.Page{
		Resources: ast.PdfDict{
			"XObject": ast.PdfDict{},
		},
	}

	_, err := DecodeImage(context.Background(), doc, page, "Missing")
	if err == nil {
		t.Error("expected error for missing image")
	}
}

func TestDecodeImage_NoXObject(t *testing.T) {
	doc := &document.Document{}
	page := &document.Page{
		Resources: ast.PdfDict{},
	}

	_, err := DecodeImage(context.Background(), doc, page, "Im0")
	if err == nil {
		t.Error("expected error for missing /XObject")
	}
}

func TestExtractImages_Basic(t *testing.T) {
	stream := ast.NewStream(ast.PdfDict{
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(100),
		"Height":           ast.PdfInt(200),
		"ColorSpace":       ast.PdfName("DeviceRGB"),
		"BitsPerComponent": ast.PdfInt(8),
	}, []byte{})

	doc := &document.Document{}
	page := &document.Page{
		Resources: ast.PdfDict{
			"XObject": ast.PdfDict{
				"Im0": stream,
			},
		},
	}

	images := ExtractImages(context.Background(), doc, page)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].Width != 100 || images[0].Height != 200 {
		t.Errorf("image size = %dx%d, want 100x200", images[0].Width, images[0].Height)
	}
	if images[0].ColorSpace != "DeviceRGB" {
		t.Errorf("colorspace = %q, want DeviceRGB", images[0].ColorSpace)
	}
}

func TestExtractImages_NoXObject(t *testing.T) {
	doc := &document.Document{}
	page := &document.Page{
		Resources: ast.PdfDict{},
	}

	images := ExtractImages(context.Background(), doc, page)
	if images != nil {
		t.Errorf("expected nil for no XObjects, got %d", len(images))
	}
}

func TestExtractImages_SkipsNonImage(t *testing.T) {
	// A Form XObject (not Image) should be skipped.
	stream := ast.NewStream(ast.PdfDict{
		"Subtype": ast.PdfName("Form"),
		"Width":   ast.PdfInt(100),
		"Height":  ast.PdfInt(100),
	}, []byte{})

	doc := &document.Document{}
	page := &document.Page{
		Resources: ast.PdfDict{
			"XObject": ast.PdfDict{
				"Fm0": stream,
			},
		},
	}

	images := ExtractImages(context.Background(), doc, page)
	if len(images) != 0 {
		t.Errorf("expected 0 images for Form XObject, got %d", len(images))
	}
}

func TestValidateImageDimensions(t *testing.T) {
	tests := []struct {
		name    string
		w, h    int
		wantErr bool
	}{
		{name: "zero width", w: 0, h: 100, wantErr: true},
		{name: "zero height", w: 100, h: 0, wantErr: true},
		{name: "negative width", w: -1, h: 100, wantErr: true},
		{name: "small image", w: 100, h: 100, wantErr: false},
		{name: "8K display", w: 7680, h: 4320, wantErr: false},
		{name: "just under cap", w: 10000, h: 9999, wantErr: false},
		{name: "100k by 100k", w: 100000, h: 100000, wantErr: true},
		{name: "32-bit overflow shape", w: 1 << 30, h: 1 << 30, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateImageDimensions(tc.w, tc.h)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateImageDimensions(%d, %d) err=%v, wantErr=%v", tc.w, tc.h, err, tc.wantErr)
			}
		})
	}
}
