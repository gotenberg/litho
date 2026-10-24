package image

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// yccProfile builds an RGB input profile whose lut16 A2B0 reads its three
// components as Y, Cb and Cr: L* follows the first, b* the second and a* the
// third, each over its full range, so zero samples are L* 0, a* and b* -128,
// a saturated blue, and its black is 0, 128, 128.
func yccProfile() []byte {
	b := []byte("mft2\x00\x00\x00\x00")
	b = append(b, 3, 3, 2, 0)
	for i := range 9 {
		v := uint32(0)
		if i%4 == 0 {
			v = 1 << 16
		}
		b = binary.BigEndian.AppendUint32(b, v)
	}
	b = binary.BigEndian.AppendUint16(b, 2)
	b = binary.BigEndian.AppendUint16(b, 2)
	for range 3 {
		b = binary.BigEndian.AppendUint16(b, 0)
		b = binary.BigEndian.AppendUint16(b, 65535)
	}
	for y := range 2 {
		for cb := range 2 {
			for cr := range 2 {
				// Legacy 16-bit Lab: L* 100 is 0xFF00, a* and b* run 0 to 0xFFFF.
				b = binary.BigEndian.AppendUint16(b, uint16(0xFF00*y))
				b = binary.BigEndian.AppendUint16(b, uint16(0xFFFF*cr))
				b = binary.BigEndian.AppendUint16(b, uint16(0xFFFF*cb))
			}
		}
	}
	for range 3 {
		b = binary.BigEndian.AppendUint16(b, 0)
		b = binary.BigEndian.AppendUint16(b, 65535)
	}

	tags := [][2]any{{"A2B0", b}}
	header := make([]byte, 128)
	header[8] = 2
	copy(header[12:], "scnr")
	copy(header[16:], "RGB ")
	copy(header[20:], "Lab ")
	copy(header[36:], "acsp")
	table := binary.BigEndian.AppendUint32(nil, uint32(len(tags)))
	offset := 128 + 4 + 12*len(tags)
	var body []byte
	for _, tag := range tags {
		data := tag[1].([]byte)
		table = append(table, tag[0].(string)...)
		table = binary.BigEndian.AppendUint32(table, uint32(offset+len(body)))
		table = binary.BigEndian.AppendUint32(table, uint32(len(data)))
		body = append(body, data...)
	}
	out := append(append(header, table...), body...)
	binary.BigEndian.PutUint32(out, uint32(len(out)))
	return out
}

// TestNativeBlack pins the black redaction blanks an image to: the
// conventional black of a space where it shows black, the darkest sample of an
// ICC space where zero shows a colour, and no ink for a space over None.
func TestNativeBlack(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		cs         string
		components int
		extra      []string
		want       []byte // nil: any sample the space shows black
	}{
		{"gray", "/DeviceGray", 1, nil, []byte{0}},
		{"rgb", "/DeviceRGB", 3, nil, []byte{0, 0, 0}},
		{"cmyk", "/DeviceCMYK", 4, nil, []byte{0, 0, 0, 255}},
		{"calrgb", "[/CalRGB << /WhitePoint [0.9505 1 1.089] >>]", 3, nil, []byte{0, 0, 0}},
		{"icc rgb", "[/ICCBased 5 0 R]", 3, []string{profileObj(3, icc.SRGBProfile())}, []byte{0, 0, 0}},
		{"icc ycc", "[/ICCBased 5 0 R]", 3, []string{profileObj(3, yccProfile())}, nil},
		{"none", "[/Separation /None /DeviceGray 5 0 R]", 1, []string{noneTint}, []byte{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			samples := bytes.Repeat([]byte{128}, tc.components)
			ctx, doc, page := contextPDF(t, pdfContext{}, tc.cs, "", 1, samples, tc.extra...)
			stream, _, err := lookupImage(ctx, doc, page, ast.PdfName("Im0"))
			if err != nil {
				t.Fatalf("lookupImage: %v", err)
			}
			got, err := NativeBlack(ctx, doc, page, stream.Dict, tc.components)
			if err != nil {
				t.Fatalf("NativeBlack: %v", err)
			}
			if tc.want != nil {
				if !bytes.Equal(got, tc.want) {
					t.Fatalf("NativeBlack = %v, want %v", got, tc.want)
				}
				return
			}
			if bytes.Equal(got, make([]byte, tc.components)) {
				t.Fatal("NativeBlack returned zero samples, which the space shows blue")
			}
			sc, err := NewSampleColor(ctx, doc, stream.Dict["ColorSpace"], page.Resources, page.Dict, icc.RelativeColorimetric)
			if err != nil {
				t.Fatalf("NewSampleColor: %v", err)
			}
			c, err := sc.Convert(got)
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if peak := max(c.R, c.G, c.B); peak > nativeBlackLevel {
				t.Fatalf("NativeBlack = %v shows %v, brighter than black", got, c)
			}
			zero, err := sc.Convert(make([]byte, tc.components))
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if max(zero.R, zero.G, zero.B) <= nativeBlackLevel {
				t.Fatalf("zero samples show %v: the profile does not plant a non-black zero", zero)
			}
		})
	}
}

// TestNativeBlack_RefusesAComponentMismatch pins that a count the space does
// not hold is an error rather than a black of the wrong length.
func TestNativeBlack_RefusesAComponentMismatch(t *testing.T) {
	t.Parallel()

	ctx, doc, page := contextPDF(t, pdfContext{}, "/DeviceRGB", "", 1, []byte{0, 0, 0})
	stream, _, err := lookupImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("lookupImage: %v", err)
	}
	if got, err := NativeBlack(ctx, doc, page, stream.Dict, 4); err == nil {
		t.Fatalf("NativeBlack = %v, want an error for 4 components of an RGB space", got)
	}
}
