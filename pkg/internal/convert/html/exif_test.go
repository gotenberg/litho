package html

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
	for _, e := range [][4]uint32{{0x010E, 2, 1, 0}, {0x0112, 3, 1, uint32(o)}} {
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
func checkSize(t *testing.T, id string, s *typeset.ImageSource, w, h int) {
	t.Helper()
	if len(s.JPEG) > 0 || s.Width != w || s.Height != h {
		t.Errorf("%s: picture of %d by %d, JPEG %v", id, s.Width, s.Height, len(s.JPEG) > 0)
	}
}

func FuzzUnsupportedFormat(f *testing.F) {
	f.Add([]byte("\x00\x00\x00\x18ftypavif"), "")
	f.Add([]byte{0, 0, 1, 0, 1, 0}, "image/x-icon")
	f.Add([]byte{}, "")
	f.Fuzz(func(_ *testing.T, data []byte, mediaType string) {
		_ = unsupportedFormat(data, mediaType)
	})
}
