package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
)

// pngChunk appends a PNG chunk (ISO/IEC 15948, 5.3) to b.
func pngChunk(b *bytes.Buffer, typ string, data []byte) {
	_ = binary.Write(b, binary.BigEndian, uint32(len(data))) //nolint:errcheck,gosec // bytes.Buffer writes never fail; a short chunk
	h := crc32.NewIEEE()
	h.Write([]byte(typ))
	h.Write(data)
	b.WriteString(typ)
	b.Write(data)
	_ = binary.Write(b, binary.BigEndian, h.Sum32()) //nolint:errcheck // bytes.Buffer writes never fail
}

// pngImage returns a PNG of w by h RGBA pixels whose image data is rows, as
// many as given, of zero bytes: a header that promises far more pixels than
// the data holds when rows is small.
func pngImage(w, h uint32, rows int) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6
	pngChunk(&b, "IHDR", ihdr)
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	row := make([]byte, 1+4*int(min(w, 4096)))
	for range rows {
		_, _ = zw.Write(row) //nolint:errcheck // bytes.Buffer writes never fail
	}
	_ = zw.Close() //nolint:errcheck // bytes.Buffer writes never fail
	pngChunk(&b, "IDAT", z.Bytes())
	pngChunk(&b, "IEND", nil)
	return b.Bytes()
}

// pixelPNG is a valid 1 by 1 PNG.
func pixelPNG() []byte { return pngImage(1, 1, 1) }

// jpegImage returns a baseline JPEG (ITU-T T.81) whose frame header claims
// w by h pixels in three components and whose scan holds a few blocks.
func jpegImage(w, h uint16) []byte {
	var b bytes.Buffer
	seg := func(marker byte, data []byte) {
		b.Write([]byte{0xFF, marker})
		_ = binary.Write(&b, binary.BigEndian, uint16(len(data)+2)) //nolint:errcheck,gosec // bytes.Buffer writes never fail; a short segment
		b.Write(data)
	}
	b.Write([]byte{0xFF, 0xD8})
	seg(0xDB, append([]byte{0}, bytes.Repeat([]byte{1}, 64)...))
	sof := []byte{8, 0, 0, 0, 0, 3, 1, 0x11, 0, 2, 0x11, 0, 3, 0x11, 0}
	binary.BigEndian.PutUint16(sof[1:], h)
	binary.BigEndian.PutUint16(sof[3:], w)
	seg(0xC0, sof)
	// One code of length 1 for symbol 0 in each table: every DC difference
	// is 0 and every block ends at once.
	table := append([]byte{1}, make([]byte, 15)...)
	seg(0xC4, append(append([]byte{0x00}, table...), 0))
	seg(0xC4, append(append([]byte{0x10}, table...), 0))
	seg(0xDA, []byte{3, 1, 0x00, 2, 0x00, 3, 0x00, 0, 63, 0})
	b.Write(make([]byte, 64))
	b.Write([]byte{0xFF, 0xD9})
	return b.Bytes()
}

// gifImage returns a GIF89a whose screen and image claim w by h pixels and
// whose LZW data holds a clear code and the end code alone.
func gifImage(w, h uint16) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	b.WriteString("GIF89a")
	_ = binary.Write(&b, le, w) //nolint:errcheck // bytes.Buffer writes never fail
	_ = binary.Write(&b, le, h) //nolint:errcheck // bytes.Buffer writes never fail
	b.Write([]byte{0x80, 0, 0, 0, 0, 0, 0xFF, 0xFF, 0xFF})
	b.WriteByte(0x2C)
	_ = binary.Write(&b, le, uint32(0)) //nolint:errcheck // bytes.Buffer writes never fail
	_ = binary.Write(&b, le, w)         //nolint:errcheck // bytes.Buffer writes never fail
	_ = binary.Write(&b, le, h)         //nolint:errcheck // bytes.Buffer writes never fail
	b.Write([]byte{0, 2, 1, 0x44, 0, 0x3B})
	return b.Bytes()
}
