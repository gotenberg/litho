package imagefile

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"runtime"
	"testing"
)

// bombTIFF returns a 100 by 100 gray TIFF whose one Deflate strip declares
// rows rows a strip and inflates to n zero bytes.
func bombTIFF(t *testing.T, rows uint32, n int) []byte {
	t.Helper()
	var z bytes.Buffer
	zw, err := zlib.NewWriterLevel(&z, zlib.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	zero := make([]byte, 1<<16)
	for written := 0; written < n; written += len(zero) {
		if _, err := zw.Write(zero); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	entries := [][3]uint32{{256, 4, 100}, {257, 4, 100}, {258, 3, 8}, {259, 3, 8}, {262, 3, 1}, {273, 4, 0}, {277, 3, 1}, {278, 4, rows}, {279, 4, uint32(z.Len())}}
	ifd := 8
	data := le.AppendUint32([]byte("II*\x00"), uint32(ifd))
	data = le.AppendUint16(data, uint16(len(entries)))
	strip := ifd + 2 + 12*len(entries) + 4
	for _, e := range entries {
		v := e[2]
		if e[0] == 273 {
			v = uint32(strip)
		}
		data = le.AppendUint16(data, uint16(e[0]))
		data = le.AppendUint16(data, uint16(e[1]))
		data = le.AppendUint32(data, 1)
		if e[1] == 3 {
			data = le.AppendUint32(data, v&0xFFFF)
		} else {
			data = le.AppendUint32(data, v)
		}
	}
	data = append(data, 0, 0, 0, 0)
	return append(data, z.Bytes()...)
}

// TestDecode_BoundsATIFFStripByItsPage checks that a strip whose
// RowsPerStrip passes the page's rows inflates no further than the page:
// golang.org/x/image/tiff bounds a strip's inflated bytes by RowsPerStrip,
// so a strip of 2^31 rows over a page of 100 would inflate its 64 MiB.
func TestDecode_BoundsATIFFStripByItsPage(t *testing.T) {
	data := bombTIFF(t, 1<<31-1, 64<<20)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	img, _, err := Decode(data)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b != image.Rect(0, 0, 100, 100) {
		t.Fatalf("decoded %v", b)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 8<<20 {
		t.Errorf("decoding allocated %d bytes", grew)
	}
}
