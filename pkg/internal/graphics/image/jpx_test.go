package image

import (
	"errors"
	"fmt"
	stdimage "image"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// jpxPDF builds a one-page document drawing /Im0, a JPXDecode image of the
// given size carrying entries and data, plus any extra objects numbered from 5.
func jpxPDF(width, height int, entries string, data []byte, extra ...string) []string {
	return append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /Filter /JPXDecode %s /Length %d >>\nstream\n%s\nendstream",
			width, height, entries, len(data), data),
	}, extra...)
}

// readTestJPX reads a JPEG 2000 file from the filter package's test data.
func readTestJPX(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../filter/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// greyAt returns the grey level of a pixel.
func greyAt(img stdimage.Image, x, y int) uint32 {
	r, _, _, _ := img.At(x, y).RGBA()
	return r >> 8
}

// TestDecodeImage_JPXColourFromData decodes JPXDecode images whose
// dictionaries give no /ColorSpace, which ISO 32000-1, 7.4.9 then takes from
// the JPEG 2000 data: a bare codestream of one channel reads as DeviceGray,
// and a JP2 file in enumerated sRGB, with an opacity channel beside its three
// colour channels, as DeviceRGB.
func TestDecodeImage_JPXColourFromData(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, jpxPDF(1, 9, "", readTestJPX(t, "testdata/annex_j10.j2k")))
	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("J.10: %v", err)
	}
	var grey []uint32
	for y := range 9 {
		grey = append(grey, greyAt(img, 0, y))
	}
	if want := []uint32{101, 103, 104, 105, 96, 97, 96, 102, 109}; !slices.Equal(grey, want) {
		t.Errorf("J.10 grey levels %v, want %v", grey, want)
	}

	ctx, doc, page = imagePDF(t, jpxPDF(9, 7, "", readTestJPX(t, "testdata/rgba.jp2")))
	img, err = DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("RGBA: %v", err)
	}
	for y := range 7 {
		for x := range 9 {
			r, g, b, _ := img.At(x, y).RGBA()
			if got, want := [3]uint32{r >> 8, g >> 8, b >> 8}, [3]uint32{uint32(x * 28), uint32(y * 36), uint32(x * y * 7 % 256)}; got != want {
				t.Fatalf("RGBA pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

// TestDecodeImage_JPXIndexed reads the samples of a palettized JP2 file as
// indices into the image's own /Indexed colour table, not the JPEG 2000
// palette (ISO 32000-1, 7.4.9): the table here maps index i to grey level
// 255 - i, where the JPEG 2000 palette would give other colours.
func TestDecodeImage_JPXIndexed(t *testing.T) {
	t.Parallel()

	var table strings.Builder
	for i := range 256 {
		fmt.Fprintf(&table, "%02X", 255-i)
	}
	cs := "/ColorSpace [/Indexed /DeviceGray 255 <" + table.String() + ">]"
	// The colour space is given directly, and through an indirect reference,
	// which the document resolves before the filter looks for Indexed.
	for _, objs := range [][]string{
		jpxPDF(1, 9, cs, palettedJ10Bytes(t)),
		jpxPDF(1, 9, "/ColorSpace 5 0 R", palettedJ10Bytes(t), strings.TrimPrefix(cs, "/ColorSpace ")),
	} {
		ctx, doc, page := imagePDF(t, objs)
		img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
		if err != nil {
			t.Fatalf("DecodeImage: %v", err)
		}
		for y, v := range []uint32{101, 103, 104, 105, 96, 97, 96, 102, 109} {
			if got := greyAt(img, 0, y); got != 255-v {
				t.Errorf("row %d grey %d, want %d", y, got, 255-v)
			}
		}
	}
}

// palettedJ10Bytes wraps the J.10 codestream in a JP2 file mapping its
// component through a 256-entry sRGB palette, entry j being (255 - j, j/2, 0).
// No column maps an index to itself, so a palette lookup never passes for the
// index.
func palettedJ10Bytes(t *testing.T) []byte {
	t.Helper()
	box := func(kind string, body ...[]byte) []byte {
		b := slices.Concat(body...)
		n := len(b) + 8
		return slices.Concat([]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, []byte(kind), b)
	}
	pclr := []byte{1, 0, 3, 7, 7, 7}
	for j := range 256 {
		pclr = append(pclr, byte(255-j), byte(j/2), 0)
	}
	return slices.Concat(
		box("jP  ", []byte{0x0D, 0x0A, 0x87, 0x0A}),
		box("ftyp", []byte("jp2 \x00\x00\x00\x00jp2 ")),
		box("jp2h",
			box("ihdr", []byte{0, 0, 0, 9, 0, 0, 0, 1, 0, 1, 7, 7, 0, 0}),
			box("colr", []byte{1, 0, 0, 0, 0, 0, 16}),
			box("pclr", pclr),
			box("cmap", []byte{0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 2})),
		box("jp2c", readTestJPX(t, "testdata/annex_j10.j2k")),
	)
}

// TestDecodeImage_JPXUnsupportedColour refuses a JPXDecode image without
// /ColorSpace whose data names CIEJab, which would need a conversion litho
// does not make, or names sRGB over one colour channel, rather than reading
// its samples in a space they do not fill.
func TestDecodeImage_JPXUnsupportedColour(t *testing.T) {
	t.Parallel()

	jab := palettedJ10Bytes(t)
	i := strings.Index(string(jab), "colr")
	jab[i+10] = 19
	// Without its Component Mapping box the palette is not used, and the one
	// component is the image's only colour channel.
	grey := palettedJ10Bytes(t)
	i = strings.Index(string(grey), "cmap")
	copy(grey[i:], "skip")
	for name, data := range map[string][]byte{"CIEJab": jab, "sRGB over one channel": grey} {
		ctx, doc, page := imagePDF(t, jpxPDF(1, 9, "", data))
		if _, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0")); !errors.Is(err, jpx.ErrUnsupported) {
			t.Errorf("%s: DecodeImage = %v, want %v", name, err, jpx.ErrUnsupported)
		}
	}
}

// TestDecodeImage_JPXStatedDepth decodes a JPXDecode image whose dictionary
// states /BitsPerComponent 4. ISO 32000-1, Table 89 says the entry is ignored
// for a JPXDecode image, and the filter hands on 8 bits per sample, so reading
// the output as packed 4-bit samples would scramble every pixel.
func TestDecodeImage_JPXStatedDepth(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, jpxPDF(1, 9, "/BitsPerComponent 4", readTestJPX(t, "testdata/annex_j10.j2k")))
	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	for y, v := range []uint32{101, 103, 104, 105, 96, 97, 96, 102, 109} {
		if got := greyAt(img, 0, y); got != v {
			t.Errorf("row %d grey %d, want %d", y, got, v)
		}
	}
}
