package image

import (
	"encoding/binary"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// labColr is an enumerated CIELab Colour Specification box carrying the EP
// fields of ISO/IEC 15444-2, M.11.7.4.1: RL, OL, RA, OA, RB, OB and IL.
func labColr(ep [7]uint32) []byte {
	body := []byte{1, 0, 0, 0, 0, 0, jpx.ColourCIELab}
	for _, v := range ep {
		body = binary.BigEndian.AppendUint32(body, v)
	}
	return jp2Box("colr", body)
}

// decodeConstantJPX decodes a JPXDecode image without /ColorSpace whose every
// pixel is entry, through a palette under the given colour specification, and
// returns the first pixel.
func decodeConstantJPX(t *testing.T, colr []byte, entry [3]byte) [3]int {
	t.Helper()
	data := j10JP2(t, colr, palette3(func(int) [3]byte { return entry }))
	ctx, doc, page := imagePDF(t, jpxPDF(1, 9, "", data))
	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}

// nearRGB reports whether every component of got is within tol of want.
func nearRGB(got, want [3]int, tol int) bool {
	for c := range 3 {
		if d := got[c] - want[c]; d < -tol || d > tol {
			return false
		}
	}
	return true
}

// TestDecodeImage_JPXCIELab reads enumerated CIELab JPEG 2000 data (ISO/IEC
// 15444-2, M.11.7.4.1) as colour, where it used to be read as DeviceRGB by its
// channel count.
//
// The default box states no EP fields and takes the T.42 ranges: over 8-bit
// channels a byte b is L* 100 b/255, a* 170 (b - 128)/255 and b* 200
// (b - 96)/255, so (255, 128, 96) is the white of L* 100 and (0, 128, 96)
// black. The explicit box sets a* and b* to b - 128, so (128, 128, 128) is
// the neutral of L* 50.2, whose Y of 0.1859 is sRGB 119, and (138, 209, 198)
// is L* 54.1, a* 81, b* 70 under D50, the Lab of sRGB red.
func TestDecodeImage_JPXCIELab(t *testing.T) {
	t.Parallel()

	def := jp2Box("colr", []byte{1, 0, 0, 0, 0, 0, jpx.ColourCIELab})
	explicit := labColr([7]uint32{100, 0, 255, 128, 255, 128, jpx.IlluminantD50})
	for _, tc := range []struct {
		name  string
		colr  []byte
		entry [3]byte
		want  [3]int
		tol   int
	}{
		{"default white", def, [3]byte{255, 128, 96}, [3]int{255, 255, 255}, 1},
		{"default black", def, [3]byte{0, 128, 96}, [3]int{0, 0, 0}, 1},
		{"explicit grey", explicit, [3]byte{128, 128, 128}, [3]int{119, 119, 119}, 1},
		{"explicit red", explicit, [3]byte{138, 209, 198}, [3]int{255, 0, 0}, 12},
		{"D65 white", labColr([7]uint32{100, 0, 255, 128, 255, 128, jpx.IlluminantD65}), [3]byte{255, 128, 128}, [3]int{255, 255, 255}, 1},
	} {
		if got := decodeConstantJPX(t, tc.colr, tc.entry); !nearRGB(got, tc.want, tc.tol) {
			t.Errorf("%s: pixel %v, want %v within %d", tc.name, got, tc.want, tc.tol)
		}
	}
}

// TestDecodeImage_JPXROMM reads enumerated ROMM-RGB JPEG 2000 data through
// the ISO 22028-2 encoding: white stays white, and the grey byte 128, linear
// 0.2892 under the 1.8 power, is sRGB 146 where reading it as DeviceRGB left
// it at 128.
func TestDecodeImage_JPXROMM(t *testing.T) {
	t.Parallel()

	colr := enumeratedColr(jpx.ColourROMMRGB, 0, 0)
	for _, tc := range []struct {
		name  string
		entry [3]byte
		want  [3]int
		tol   int
	}{
		{"white", [3]byte{255, 255, 255}, [3]int{255, 255, 255}, 1},
		{"grey", [3]byte{128, 128, 128}, [3]int{146, 146, 146}, 1},
		{"black", [3]byte{0, 0, 0}, [3]int{0, 0, 0}, 0},
	} {
		if got := decodeConstantJPX(t, colr, tc.entry); !nearRGB(got, tc.want, tc.tol) {
			t.Errorf("%s: pixel %v, want %v within %d", tc.name, got, tc.want, tc.tol)
		}
	}
}

// TestDecodeImage_JPXUnreadEnumeratedSpaces pins the specifications litho
// does not read, which fall to the device space of the channel count as
// ISO 32000-1, 7.4.9 says: CIELab under illuminant A, whose white point the
// box names but litho does not hold, and e-sRGB and e-sYCC, whose integer
// encoding ISO/IEC 15444-2 leaves to PIMA 7667. Each pixel is the entry as it
// stands.
func TestDecodeImage_JPXUnreadEnumeratedSpaces(t *testing.T) {
	t.Parallel()

	entry := [3]byte{200, 100, 50}
	for _, tc := range []struct {
		name string
		colr []byte
	}{
		{"CIELab under illuminant A", labColr([7]uint32{100, 0, 255, 128, 255, 128, 0x00005341})},
		{"e-sRGB", enumeratedColr(jpx.ColourESRGB, 0, 0)},
		{"e-sYCC", enumeratedColr(jpx.ColourESYCC, 0, 0)},
	} {
		if got := decodeConstantJPX(t, tc.colr, entry); got != [3]int{200, 100, 50} {
			t.Errorf("%s: pixel %v, want the entry %v read as DeviceRGB", tc.name, got, entry)
		}
	}
}
