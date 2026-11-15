package imagefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
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
		if got := JPEGOrientation(tc.data); got != tc.want {
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
		got, err := OrientImage(context.Background(), src, o)
		if err != nil {
			t.Fatalf("orientation %d: %v", o, err)
		}
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
	if got, err := OrientImage(context.Background(), rgb, 6); err != nil {
		t.Errorf("colour picture: %v", err)
	} else if _, ok := got.(*image.RGBA); !ok {
		t.Errorf("colour picture not RGBA")
	}
	// A conversion that ends stops the copy.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OrientImage(ctx, rgb, 6); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled copy: %v, want context.Canceled", err)
	}
}

func FuzzJPEGOrientation(f *testing.F) {
	f.Add(withEXIF([]byte{0xFF, 0xD8, 0xFF, 0xD9}, exifSegment(binary.BigEndian, 6)))
	f.Add(withEXIF([]byte{0xFF, 0xD8, 0xFF, 0xD9}, exifSegment(binary.LittleEndian, 8)))
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xFF, 0xE1, 0x00, 0x02})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if o := JPEGOrientation(data); o < 1 || o > 8 {
			t.Fatalf("orientation %d", o)
		}
	})
}

// pngWithEXIF returns a 4 by 2 PNG whose eXIf chunk, before its IDAT,
// holds the orientation o.
func pngWithEXIF(t *testing.T, o uint16) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	tiff := exifSegment(binary.BigEndian, o)[10:]
	chunk := make([]byte, 8, 12+len(tiff))
	binary.BigEndian.PutUint32(chunk, uint32(len(tiff)))
	copy(chunk[4:], "eXIf")
	chunk = append(chunk, tiff...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	// After the signature and the IHDR chunk, 8 + 25 bytes.
	return append(append(append([]byte{}, data[:33]...), chunk...), data[33:]...)
}

func TestPNGOrientation(t *testing.T) {
	t.Parallel()
	six := pngWithEXIF(t, 6)
	for _, tc := range []struct {
		name string
		data []byte
		want int
	}{
		{"eXIf 6", six, 6},
		{"eXIf out of range", pngWithEXIF(t, 9), 1},
		{"truncated", six[:40], 1},
		{"none", pngWithEXIF(t, 1), 1},
		{"not a PNG", []byte{0xFF, 0xD8}, 1},
	} {
		if got := PNGOrientation(tc.data); got != tc.want {
			t.Errorf("%s: orientation %d, want %d", tc.name, got, tc.want)
		}
	}
	if _, err := png.Decode(bytes.NewReader(six)); err != nil {
		t.Fatalf("the PNG with eXIf does not decode: %v", err)
	}
}

func FuzzPNGOrientation(f *testing.F) {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, 1, 1))) // a bytes.Buffer takes every write
	f.Add(b.Bytes())
	f.Add(b.Bytes()[:20])
	f.Fuzz(func(t *testing.T, data []byte) {
		if o := PNGOrientation(data); o < 1 || o > 8 {
			t.Fatalf("orientation %d", o)
		}
	})
}

// jpegBytes encodes a w by h JPEG.
func jpegBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
