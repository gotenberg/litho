package image

import (
	"encoding/binary"
	stdimage "image"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// tintedGreyProfile builds a version 2 grey ICC profile over Lab whose AToB0
// table, a lut16Type (ICC.1:2001-04, 6.5.7) over a three-point grid, reads
// sample 0 as black, L* 0, sample 255 as white, L* 100, and the samples
// between off the neutral axis, up to a* 30 at L* 50, as a sepia or toned
// scan profile reads its greys. Black and white stay neutral, so neither
// black point compensation nor Little CMS's fixing of white on white moves
// what the profile states.
func tintedGreyProfile() []byte {
	b := []byte("mft2\x00\x00\x00\x00")
	b = append(b, 1, 3, 3, 0)
	for i := range 9 {
		v := uint32(0)
		if i%4 == 0 {
			v = 1 << 16
		}
		b = binary.BigEndian.AppendUint32(b, v)
	}
	b = binary.BigEndian.AppendUint16(b, 2)
	b = binary.BigEndian.AppendUint16(b, 2)
	// Identity input table.
	b = binary.BigEndian.AppendUint16(b, 0)
	b = binary.BigEndian.AppendUint16(b, 65535)
	// Legacy 16-bit Lab: L* 100 is 0xFF00, a* 0 is 0x8000, one unit 0x100.
	for _, lab := range [][3]float64{{0, 0, 0}, {50, 30, 0}, {100, 0, 0}} {
		b = binary.BigEndian.AppendUint16(b, uint16(math.Round(lab[0]/100*65280)))
		b = binary.BigEndian.AppendUint16(b, uint16(0x8000+lab[1]*256))
		b = binary.BigEndian.AppendUint16(b, uint16(0x8000+lab[2]*256))
	}
	for range 3 {
		b = binary.BigEndian.AppendUint16(b, 0)
		b = binary.BigEndian.AppendUint16(b, 65535)
	}
	header := make([]byte, 128)
	header[8] = 2
	copy(header[12:], "scnr")
	copy(header[16:], "GRAY")
	copy(header[20:], "Lab ")
	copy(header[36:], "acsp")
	table := binary.BigEndian.AppendUint32(nil, 1)
	table = append(table, "A2B0"...)
	table = binary.BigEndian.AppendUint32(table, 128+4+12)
	table = binary.BigEndian.AppendUint32(table, uint32(len(b)))
	out := append(append(header, table...), b...)
	binary.BigEndian.PutUint32(out, uint32(len(out)))
	return out
}

// The sRGB colours of the tinted grey profile's samples 128 and 192, as
// Little CMS 2.17 (Pillow 11.3.0 ImageCms, relative colorimetric, with and
// without black point compensation) gives them.
var (
	tintedMid   = [3]int{166, 99, 121}
	tintedLight = [3]int{212, 176, 186}
	white       = [3]int{255, 255, 255}
)

// TestDecodeImage_TintedGreyProfile pins that a grey ICCBased image is read
// through its profile's whole table (ISO 32000-1, 8.6.5.5): an AToB table
// that maps a grey sample to a coloured PCS value draws it coloured, as
// poppler draws it, where reading the green channel alone drew a grey.
func TestDecodeImage_TintedGreyProfile(t *testing.T) {
	t.Parallel()

	ctx, doc, page := managedPDF(t, "", "[/ICCBased 5 0 R]", "", 4, []byte{0, 128, 192, 255},
		profileObj(1, tintedGreyProfile()))
	wantPixels(t, ctx, doc, page, [3]int{0, 0, 0}, tintedMid, tintedLight, white)

	// A /Decode array inverts the samples before the profile reads them
	// (8.9.5.2).
	ctx, doc, page = managedPDF(t, "", "[/ICCBased 5 0 R]", "/Decode [1 0]", 2, []byte{127, 63},
		profileObj(1, tintedGreyProfile()))
	wantPixels(t, ctx, doc, page, tintedMid, tintedLight)
}

// TestDecodeImage_NeutralGreyProfileStaysGrey pins that a grey profile whose
// table stays on the neutral axis keeps its one-channel reading.
func TestDecodeImage_NeutralGreyProfileStaysGrey(t *testing.T) {
	t.Parallel()

	ctx, doc, page := managedPDF(t, "", "[/ICCBased 5 0 R]", "", 2, []byte{128, 255},
		profileObj(1, flatProfile("GRAY", 25)))
	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if _, ok := img.(*stdimage.Gray); !ok {
		t.Fatalf("DecodeImage = %T, want a grey image", img)
	}
	wantPixels(t, ctx, doc, page, grey25, grey25)
}

// TestSampleColor_TintedGreyProfile pins that a colour in a grey ICCBased
// space, as a fill selects it, converts through the whole table too.
func TestSampleColor_TintedGreyProfile(t *testing.T) {
	t.Parallel()

	ctx, doc, page := managedPDF(t, "", "[/ICCBased 5 0 R]", "", 1, []byte{255},
		profileObj(1, tintedGreyProfile()))
	stream, _, err := lookupImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("lookupImage: %v", err)
	}
	sc, err := NewSampleColor(ctx, doc, stream.Dict["ColorSpace"], page.Resources, page.Dict, icc.RelativeColorimetric)
	if err != nil {
		t.Fatalf("NewSampleColor: %v", err)
	}
	for _, tc := range []struct {
		sample byte
		want   [3]int
	}{{255, white}, {192, tintedLight}, {128, tintedMid}, {0, [3]int{0, 0, 0}}} {
		c, err := sc.Convert([]byte{tc.sample})
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		got := [3]int{int(c.R), int(c.G), int(c.B)}
		for i := range 3 {
			if d := got[i] - tc.want[i]; d < -1 || d > 1 {
				t.Errorf("sample %d = %v, want %v", tc.sample, got, tc.want)
				break
			}
		}
	}
}
