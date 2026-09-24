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

// jp2Box wraps body in a JP2 box of the given type (T.800, I.4).
func jp2Box(kind string, body ...[]byte) []byte {
	b := slices.Concat(body...)
	n := len(b) + 8
	return slices.Concat([]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, []byte(kind), b)
}

// j10JP2 wraps the J.10 codestream, one 8-bit component 1 wide and 9 high,
// in a JP2 file whose header holds boxes after its Image Header box.
func j10JP2(t *testing.T, boxes ...[]byte) []byte {
	t.Helper()
	return slices.Concat(
		jp2Box("jP  ", []byte{0x0D, 0x0A, 0x87, 0x0A}),
		jp2Box("ftyp", []byte("jp2 \x00\x00\x00\x00jp2 ")),
		jp2Box("jp2h", append([][]byte{jp2Box("ihdr", []byte{0, 0, 0, 9, 0, 0, 0, 1, 0, 1, 7, 7, 0, 0})}, boxes...)...),
		jp2Box("jp2c", readTestJPX(t, "testdata/annex_j10.j2k")),
	)
}

// enumeratedColr is a Colour Specification box of the enumerated method with
// the given precedence and approximation.
func enumeratedColr(space byte, prec int8, approx byte) []byte {
	return jp2Box("colr", []byte{1, byte(prec), approx, 0, 0, 0, space})
}

// palette3 is a Palette box of 256 entries, entry j being column(j), with its
// Component Mapping box sending the one component through all three columns.
func palette3(column func(j int) [3]byte) []byte {
	pclr := []byte{1, 0, 3, 7, 7, 7}
	for j := range 256 {
		c := column(j)
		pclr = append(pclr, c[0], c[1], c[2])
	}
	return slices.Concat(jp2Box("pclr", pclr), jp2Box("cmap", []byte{0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 2}))
}

// palettedJ10Bytes wraps the J.10 codestream in a JP2 file mapping its
// component through a 256-entry sRGB palette, entry j being (255 - j, j/2, 0).
// No column maps an index to itself, so a palette lookup never passes for the
// index.
func palettedJ10Bytes(t *testing.T) []byte {
	t.Helper()
	return j10JP2(t, enumeratedColr(16, 0, 0), palette3(func(j int) [3]byte { return [3]byte{byte(255 - j), byte(j / 2), 0} }))
}

// TestDecodeImage_JPXUnsupportedColour reads a JPXDecode image without
// /ColorSpace whose data names no colour space litho supports in the device
// space of its colour channel count, as ISO 32000-1, 7.4.9 says to: CIEJab
// over three channels as DeviceRGB, and sRGB over one as DeviceGray. Two
// channels have no device space and are refused.
func TestDecodeImage_JPXUnsupportedColour(t *testing.T) {
	t.Parallel()

	var two []byte
	two = append(two, 1, 0, 2, 7, 7)
	for j := range 256 {
		two = append(two, byte(j), byte(j))
	}
	for _, tc := range []struct {
		name string
		data []byte
		want [3]uint32
	}{
		{"CIEJab over three channels", j10JP2(t, enumeratedColr(jpx.ColourCIEJab, 0, 0), palette3(func(j int) [3]byte { return [3]byte{byte(255 - j), byte(j / 2), 0} })), [3]uint32{154, 50, 0}},
		{"sRGB over one channel", j10JP2(t, enumeratedColr(jpx.ColourSRGB, 0, 0)), [3]uint32{101, 101, 101}},
	} {
		ctx, doc, page := imagePDF(t, jpxPDF(1, 9, "", tc.data))
		img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
		if err != nil {
			t.Errorf("%s: DecodeImage: %v", tc.name, err)
			continue
		}
		r, g, b, _ := img.At(0, 0).RGBA()
		if got := [3]uint32{r >> 8, g >> 8, b >> 8}; got != tc.want {
			t.Errorf("%s: pixel %v, want %v", tc.name, got, tc.want)
		}
	}

	data := j10JP2(t, enumeratedColr(jpx.ColourSRGB, 0, 0), jp2Box("pclr", two), jp2Box("cmap", []byte{0, 0, 1, 0, 0, 0, 1, 1}))
	ctx, doc, page := imagePDF(t, jpxPDF(1, 9, "", data))
	if _, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0")); !errors.Is(err, jpx.ErrUnsupported) {
		t.Errorf("two channels: DecodeImage = %v, want %v", err, jpx.ErrUnsupported)
	}
}

// TestDecodeImage_JPXColourSpecificationOrder reads a JPXDecode image without
// /ColorSpace through the colour specification ISO 32000-1, 7.4.9 selects:
// the one of highest precedence, then best approximation (ISO/IEC 15444-2,
// M.11.7.2), then first in the file, passing over one litho cannot read. The
// greyscale specification reads the first sample as grey 101, the ICC profile
// as L* 25, grey 59.
func TestDecodeImage_JPXColourSpecificationOrder(t *testing.T) {
	t.Parallel()

	icc := func(prec int8, approx byte, profile []byte) []byte {
		return jp2Box("colr", []byte{2, byte(prec), approx}, profile)
	}
	flat := flatProfile("GRAY", 25)
	for _, tc := range []struct {
		name  string
		boxes [][]byte
		want  uint32
	}{
		{"first in the file", [][]byte{enumeratedColr(jpx.ColourGreyscale, 0, 0), icc(0, 0, flat)}, 101},
		{"first in the file, the profile", [][]byte{icc(0, 0, flat), enumeratedColr(jpx.ColourGreyscale, 0, 0)}, 59},
		{"an unreadable profile passed over", [][]byte{icc(0, 0, []byte("not a profile")), icc(0, 0, flat)}, 59},
		{"CIEJab passed over", [][]byte{enumeratedColr(jpx.ColourCIEJab, 0, 0), icc(0, 0, flat)}, 59},
		{"higher precedence", [][]byte{enumeratedColr(jpx.ColourGreyscale, 0, 1), icc(1, 1, flat)}, 59},
		{"negative precedence", [][]byte{icc(-1, 1, flat), enumeratedColr(jpx.ColourGreyscale, 0, 1)}, 101},
		{"better approximation", [][]byte{icc(0, 4, flat), enumeratedColr(jpx.ColourGreyscale, 0, 1)}, 101},
	} {
		ctx, doc, page := imagePDF(t, jpxPDF(1, 9, "", j10JP2(t, tc.boxes...)))
		img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
		if err != nil {
			t.Errorf("%s: DecodeImage: %v", tc.name, err)
			continue
		}
		if got := greyAt(img, 0, 0); got < tc.want-1 || got > tc.want+1 {
			t.Errorf("%s: grey %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestDecodeImage_JPXSYCC reads JPXDecode images without /ColorSpace whose
// data is in enumerated sYCC (ISO/IEC 15444-2, Table M.25) as sRGB.
//
// ycc420.jp2 is what OpenJPEG 2.5 codes from eight 16x16 patches of known
// colour taken to 8-bit sYCC, its chroma subsampled 2x2, and it names sYCC
// itself. Every patch comes back within 2 levels of its colour, the rounding
// of the 8-bit sYCC samples; pdf.js reads each within 1 level of litho.
//
// The palette case pins the transform: entry j is Y j, Cb 192, Cr 100.
func TestDecodeImage_JPXSYCC(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, jpxPDF(64, 32, "", readTestJPX(t, "testdata/ycc420.jp2")))
	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("ycc420.jp2: DecodeImage: %v", err)
	}
	for i, want := range [][3]int{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 255, 0}, {0, 255, 255}, {255, 0, 255}, {128, 128, 128}, {200, 120, 40}} {
		x, y := i%4*16+8, i/4*16+8
		r, g, b, _ := img.At(x, y).RGBA()
		got := [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
		for c := range 3 {
			if d := got[c] - want[c]; d < -2 || d > 2 {
				t.Errorf("ycc420.jp2 patch %d: %v, want %v", i, got, want)
				break
			}
		}
	}

	data := j10JP2(t, enumeratedColr(jpx.ColourSYCC, 0, 0), palette3(func(j int) [3]byte { return [3]byte{byte(j), 192, 100} }))
	ctx, doc, page = imagePDF(t, jpxPDF(1, 9, "", data))
	img, err = DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("palette: DecodeImage: %v", err)
	}
	// Y 101: R = 101 + 1.402 x -28, G = 101 - 0.34414 x 64 - 0.71414 x -28,
	// B = 101 + 1.772 x 64.
	r, g, b, _ := img.At(0, 0).RGBA()
	if got, want := [3]uint32{r >> 8, g >> 8, b >> 8}, [3]uint32{62, 99, 214}; got != want {
		t.Errorf("palette: pixel %v, want %v", got, want)
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
