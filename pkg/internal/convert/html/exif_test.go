package html

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// exifSegment returns an APP1 segment whose IFD0 holds the orientation o,
// in the byte order bo.
func exifSegment(bo binary.ByteOrder, o uint16) []byte {
	var tiff bytes.Buffer
	if bo == binary.BigEndian {
		tiff.WriteString("MM\x00*")
	} else {
		tiff.WriteString("II*\x00")
	}
	_ = binary.Write(&tiff, bo, uint32(8))
	_ = binary.Write(&tiff, bo, uint16(2))
	// An unrelated tag first, ImageDescription, then the orientation.
	for _, e := range [][4]uint32{{0x010E, 2, 1, 0}, {tagOrientation, 3, 1, uint32(o)}} {
		_ = binary.Write(&tiff, bo, uint16(e[0]))
		_ = binary.Write(&tiff, bo, uint16(e[1]))
		_ = binary.Write(&tiff, bo, e[2])
		_ = binary.Write(&tiff, bo, uint16(e[3]))
		_ = binary.Write(&tiff, bo, uint16(0))
	}
	_ = binary.Write(&tiff, bo, uint32(0))
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	return append(seg, payload...)
}

// withEXIF inserts seg right after the SOI marker of the JPEG data.
func withEXIF(data, seg []byte) []byte {
	return append(append(append([]byte{}, data[:2]...), seg...), data[2:]...)
}

func TestJPEGOrientation(t *testing.T) {
	t.Parallel()
	plain := jpegBytes(t, 4, 2)
	for _, tc := range []struct {
		name string
		data []byte
		want int
	}{
		{"none", plain, 1},
		{"big endian 6", withEXIF(plain, exifSegment(binary.BigEndian, 6)), 6},
		{"little endian 3", withEXIF(plain, exifSegment(binary.LittleEndian, 3)), 3},
		{"out of range", withEXIF(plain, exifSegment(binary.BigEndian, 9)), 1},
		{"zero", withEXIF(plain, exifSegment(binary.BigEndian, 0)), 1},
		{"truncated", withEXIF(plain, exifSegment(binary.BigEndian, 6))[:30], 1},
		{"not a JPEG", []byte("\x89PNG\r\n\x1a\n"), 1},
		{"empty", nil, 1},
	} {
		if got := jpegOrientation(tc.data); got != tc.want {
			t.Errorf("%s: orientation %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestOrientImage(t *testing.T) {
	t.Parallel()
	// A 3 by 2 picture whose pixel (x, y) is grey 10*(1+x+3y).
	src := image.NewGray(image.Rect(0, 0, 3, 2))
	for y := range 2 {
		for x := range 3 {
			src.SetGray(x, y, color.Gray{Y: uint8(10 * (1 + x + 3*y))})
		}
	}
	// Each orientation's rows, top to bottom (EXIF 2.32, 4.6.4).
	for o, want := range map[int][][]uint8{
		1: {{10, 20, 30}, {40, 50, 60}},
		2: {{30, 20, 10}, {60, 50, 40}},
		3: {{60, 50, 40}, {30, 20, 10}},
		4: {{40, 50, 60}, {10, 20, 30}},
		5: {{10, 40}, {20, 50}, {30, 60}},
		6: {{40, 10}, {50, 20}, {60, 30}},
		7: {{60, 30}, {50, 20}, {40, 10}},
		8: {{30, 60}, {20, 50}, {10, 40}},
	} {
		got := orientImage(src, o)
		b := got.Bounds()
		if b.Dy() != len(want) || b.Dx() != len(want[0]) {
			t.Errorf("orientation %d: bounds %v", o, b)
			continue
		}
		for y, row := range want {
			for x, v := range row {
				if g := color.GrayModel.Convert(got.At(x, y)).(color.Gray).Y; g != v {
					t.Errorf("orientation %d: pixel (%d, %d) = %d, want %d", o, x, y, g, v)
				}
			}
		}
	}
	// A colour picture comes out as RGBA.
	rgb := image.NewRGBA(image.Rect(0, 0, 2, 1))
	if _, ok := orientImage(rgb, 6).(*image.RGBA); !ok {
		t.Errorf("colour picture not RGBA")
	}
}

func TestPrepare_AppliesEXIFOrientation(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewYCbCr(image.Rect(0, 0, 4, 2), image.YCbCrSubsampleRatio420), nil); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{
		"turned.jpg":  {Data: withEXIF(buf.Bytes(), exifSegment(binary.BigEndian, 6))},
		"flipped.jpg": {Data: withEXIF(buf.Bytes(), exifSegment(binary.LittleEndian, 3))},
		"plain.jpg":   {Data: withEXIF(buf.Bytes(), exifSegment(binary.LittleEndian, 1))},
	}
	p := prep(t, `<img id=t src=turned.jpg><img id=f src=flipped.jpg><img id=p src=plain.jpg>`, Options{Resources: fsys})
	for id, want := range map[string]struct {
		w, h float64
		jpeg bool
	}{"t": {2, 4, false}, "f": {4, 2, false}, "p": {4, 2, true}} {
		pic := byID(t, p.root, id).Picture
		if pic == nil {
			t.Fatalf("%s: no picture: %+v", id, p.warnings)
		}
		if pic.Width != want.w*css.Pt || pic.Height != want.h*css.Pt {
			t.Errorf("%s: size %v by %v", id, pic.Width, pic.Height)
		}
		if (pic.Source.JPEG != nil) != want.jpeg {
			t.Errorf("%s: kept as JPEG %v", id, pic.Source.JPEG != nil)
		}
		if !want.jpeg {
			checkSize(t, id, pic.Source, int(want.w), int(want.h))
		}
	}
}

// checkSize fails the test when the decoded picture of s is not w by h.
func checkSize(t *testing.T, id string, s *compose.ImageSource, w, h int) {
	t.Helper()
	if s.Image == nil || s.Image.Bounds().Dx() != w || s.Image.Bounds().Dy() != h {
		t.Errorf("%s: decoded picture %v", id, s.Image)
	}
}

func FuzzJPEGOrientation(f *testing.F) {
	f.Add(withEXIF([]byte{0xFF, 0xD8, 0xFF, 0xD9}, exifSegment(binary.BigEndian, 6)))
	f.Add(withEXIF([]byte{0xFF, 0xD8, 0xFF, 0xD9}, exifSegment(binary.LittleEndian, 8)))
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xFF, 0xE1, 0x00, 0x02})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if o := jpegOrientation(data); o < 1 || o > 8 {
			t.Fatalf("orientation %d", o)
		}
		_ = unsupportedFormat(data, "")
	})
}
