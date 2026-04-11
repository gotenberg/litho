package pdf

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// --- TextStamp: font selection, opacity, rotation, color ---

func TestWriter_StampText_StandardFont(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	w.StampText(TextStamp{
		Text:     "DRAFT",
		X:        100,
		Y:        500,
		FontSize: 48,
		Font:     FontTimesBold,
	}, 1)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/BaseFont /Times-Bold")) {
		t.Error("expected /BaseFont /Times-Bold in output")
	}
}

func TestWriter_StampText_Opacity_EmitsExtGState(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	w.StampText(TextStamp{
		Text:     "CONFIDENTIAL",
		X:        100,
		Y:        400,
		FontSize: 24,
		Opacity:  0.3,
	}, 1)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	// ExtGState dict with /ca and /CA entries must be present in the output.
	if !bytes.Contains(out.Bytes(), []byte("/ExtGState")) {
		t.Error("expected /ExtGState in output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/ca 0.3")) && !bytes.Contains(out.Bytes(), []byte("/ca 0.30")) {
		t.Error("expected /ca 0.3 in ExtGState")
	}
}

func TestWriter_StampText_Rotation(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	w.StampText(TextStamp{
		Text:     "ROTATED",
		X:        300,
		Y:        400,
		FontSize: 24,
		Rotation: 45,
	}, 1)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	// The content stream should contain a "cm" transform operator.
	if !bytes.Contains(out.Bytes(), []byte("cm")) {
		t.Error("expected cm operator in rotated stamp content stream")
	}
}

// --- Custom TTF font ---

func TestNewTTFFont_RejectsShort(t *testing.T) {
	if _, err := NewTTFFont([]byte{0, 1, 2}); err == nil {
		t.Error("expected error for short TTF data")
	}
}

func TestWriter_StampText_CustomTTF(t *testing.T) {
	ttf := minimalTTF(t)
	font, err := NewTTFFont(ttf)
	if err != nil {
		t.Fatalf("NewTTFFont: %v", err)
	}

	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	w.StampText(TextStamp{
		Text:     "ABC",
		X:        100,
		Y:        500,
		FontSize: 24,
		Font:     font,
	}, 1)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	// Output should reference a Type0 composite font and embed the TTF.
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /Type0")) {
		t.Error("expected /Subtype /Type0 in output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /CIDFontType2")) {
		t.Error("expected /Subtype /CIDFontType2 in output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/FontFile2")) {
		t.Error("expected /FontFile2 in output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/ToUnicode")) {
		t.Error("expected /ToUnicode in output")
	}
}

// --- ImageStamp: JPEG + PNG (with and without alpha) ---

func TestWriter_StampImage_JPEG(t *testing.T) {
	jpeg := minimalJPEG(t)

	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	err = w.StampImage(ImageStamp{
		Data:     jpeg,
		MimeType: "image/jpeg",
		X:        100,
		Y:        500,
		Width:    64,
		Height:   64,
	}, 1)
	if err != nil {
		t.Fatalf("StampImage: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/DCTDecode")) {
		t.Error("expected /DCTDecode in JPEG stamp output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /Image")) {
		t.Error("expected /Subtype /Image in output")
	}
}

func TestWriter_StampImage_PNGNoAlpha(t *testing.T) {
	pngData := buildPNG(t, false)

	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	err = w.StampImage(ImageStamp{
		Data:     pngData,
		MimeType: "image/png",
		X:        50,
		Y:        50,
	}, 1)
	if err != nil {
		t.Fatalf("StampImage: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/FlateDecode")) {
		t.Error("expected /FlateDecode in PNG stamp output")
	}
	if bytes.Contains(out.Bytes(), []byte("/SMask")) {
		t.Error("opaque PNG should not emit /SMask")
	}
}

func TestWriter_StampImage_PNGWithAlpha(t *testing.T) {
	pngData := buildPNG(t, true)

	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	err = w.StampImage(ImageStamp{
		Data:     pngData,
		MimeType: "image/png",
		X:        50,
		Y:        50,
	}, 1)
	if err != nil {
		t.Fatalf("StampImage: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/SMask")) {
		t.Error("expected /SMask for PNG with alpha")
	}
}

func TestWriter_StampImage_UnsupportedMime(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	defer r.Close()
	w := NewWriter(r)
	err := w.StampImage(ImageStamp{Data: []byte("x"), MimeType: "image/gif"}, 1)
	if err == nil {
		t.Error("expected error for unsupported mime type")
	}
}

// --- PDFStamp ---

func TestWriter_StampPDF(t *testing.T) {
	// Use a copy of threePagePDF as both the target and the stamp source.
	targetData := threePagePDF()
	stampData := threePagePDF()

	ctx := context.Background()
	target, err := Open(ctx, bytes.NewReader(targetData), int64(len(targetData)))
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	defer target.Close()

	stamp, err := Open(ctx, bytes.NewReader(stampData), int64(len(stampData)))
	if err != nil {
		t.Fatalf("Open stamp: %v", err)
	}
	defer stamp.Close()

	w := NewWriter(target)
	err = w.StampPDF(PDFStamp{
		Source: stamp,
		Page:   2,
		X:      0,
		Y:      0,
		Scale:  0.5,
	}, 1)
	if err != nil {
		t.Fatalf("StampPDF: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /Form")) {
		t.Error("expected /Subtype /Form for PDF stamp")
	}
}

func TestWriter_StampPDF_InvalidPage(t *testing.T) {
	targetData := threePagePDF()
	stampData := threePagePDF()
	ctx := context.Background()
	target, _ := Open(ctx, bytes.NewReader(targetData), int64(len(targetData)))
	defer target.Close()
	stamp, _ := Open(ctx, bytes.NewReader(stampData), int64(len(stampData)))
	defer stamp.Close()

	w := NewWriter(target)
	if err := w.StampPDF(PDFStamp{Source: stamp, Page: 99}); err != nil {
		// Accept if the error surfaces at queue time.
		if strings.Contains(err.Error(), "out of range") {
			return
		}
	}
	// Otherwise the error must surface at WriteTo time.
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err == nil {
		t.Error("expected error for out-of-range PDF stamp page")
	}
}

// --- Helpers ---

// minimalJPEG returns a very small valid JPEG for stamp tests. Generated
// by encoding a 2x2 solid-red image.
func minimalJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	// Use PNG then convert to a synthetic minimal JPEG? Simpler: write a
	// hand-crafted minimal JPEG that only exercises the SOF marker path.
	return minimalSyntheticJPEG()
}

// minimalSyntheticJPEG returns the raw bytes of a tiny 1x1 black JPEG.
// The goal is to exercise the SOF marker parsing, not render a real image.
// This is an 8x8 grayscale JFIF.
func minimalSyntheticJPEG() []byte {
	return []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, // APP0
		0xFF, 0xDB, 0x00, 0x43, 0x00, // DQT (quantization table, 64 bytes)
		0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09, 0x09, 0x08, 0x0A, 0x0C, 0x14,
		0x0D, 0x0C, 0x0B, 0x0B, 0x0C, 0x19, 0x12, 0x13, 0x0F, 0x14, 0x1D, 0x1A, 0x1F, 0x1E, 0x1D, 0x1A,
		0x1C, 0x1C, 0x20, 0x24, 0x2E, 0x27, 0x20, 0x22, 0x2C, 0x23, 0x1C, 0x1C, 0x28, 0x37, 0x29, 0x2C,
		0x30, 0x31, 0x34, 0x34, 0x34, 0x1F, 0x27, 0x39, 0x3D, 0x38, 0x32, 0x3C, 0x2E, 0x33, 0x34, 0x32,
		0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x08, 0x00, 0x08, 0x01, 0x01, 0x11, 0x00, // SOF0: 8x8 grayscale
		0xFF, 0xC4, 0x00, 0x1F, 0x00, // DHT (DC luma)
		0x00, 0x01, 0x05, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B,
		0xFF, 0xC4, 0x00, 0xB5, 0x10, // DHT (AC luma)
		0x00, 0x02, 0x01, 0x03, 0x03, 0x02, 0x04, 0x03, 0x05, 0x05, 0x04, 0x04, 0x00, 0x00, 0x01, 0x7D,
		0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12, 0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
		0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xA1, 0x08, 0x23, 0x42, 0xB1, 0xC1, 0x15, 0x52, 0xD1, 0xF0,
		0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0A, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x25, 0x26, 0x27, 0x28,
		0x29, 0x2A, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3A, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
		0x4A, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
		0x6A, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
		0x8A, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9A, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7,
		0xA8, 0xA9, 0xAA, 0xB2, 0xB3, 0xB4, 0xB5, 0xB6, 0xB7, 0xB8, 0xB9, 0xBA, 0xC2, 0xC3, 0xC4, 0xC5,
		0xC6, 0xC7, 0xC8, 0xC9, 0xCA, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0xDA, 0xE1, 0xE2,
		0xE3, 0xE4, 0xE5, 0xE6, 0xE7, 0xE8, 0xE9, 0xEA, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8,
		0xF9, 0xFA,
		0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, // SOS
		0xFC, 0xFF, 0xD9, // EOI (minimal compressed data)
	}
}

// buildPNG creates a small 4x4 PNG, optionally with per-pixel alpha.
func buildPNG(t *testing.T, withAlpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			a := uint8(255)
			if withAlpha {
				a = uint8((x + y) * 32)
			}
			img.Set(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

// minimalTTF builds a minimal valid TrueType font with the tables required
// by the stamp font embedder (head, hhea, maxp, hmtx, cmap, name, post).
// It defines two glyphs: notdef and the letter "A" (GID 1, U+0041).
func minimalTTF(t *testing.T) []byte {
	t.Helper()

	// head table: 54 bytes, must set unitsPerEm at offset 18 and FontBBox
	// at offsets 36-44. indexToLocFormat at offset 50.
	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000) // version
	binary.BigEndian.PutUint16(head[18:], 1000)      // unitsPerEm
	binary.BigEndian.PutUint16(head[36:], 0)         // xMin
	binary.BigEndian.PutUint16(head[38:], 0)         // yMin
	binary.BigEndian.PutUint16(head[40:], 1000)      // xMax
	binary.BigEndian.PutUint16(head[42:], 1000)      // yMax
	binary.BigEndian.PutUint16(head[50:], 0)         // indexToLocFormat (short)

	// hhea table: ascent at offset 4, descent at offset 6,
	// numberOfHMetrics at offset 34. Minimum 36 bytes.
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[4:], 800) // ascent
	binary.BigEndian.PutUint16(hhea[6:], 200) // descent (as uint16; int16 logically negative)
	binary.BigEndian.PutUint16(hhea[34:], 2)  // numberOfHMetrics

	// maxp: 6 bytes header, numGlyphs at offset 4.
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:], 2)

	// hmtx: 2 glyphs × 4 bytes each.
	hmtx := make([]byte, 8)
	binary.BigEndian.PutUint16(hmtx[0:], 500) // gid 0 advance
	binary.BigEndian.PutUint16(hmtx[4:], 600) // gid 1 advance

	// cmap: format 4 subtable mapping U+0041 → GID 1.
	cmapSub := buildCmapFormat4TestOnly(map[uint16]uint16{0x0041: 1})
	cmap := buildCmapTestOnly(3, 1, cmapSub)

	// post: format 3 (no glyph names), minimum 32 bytes.
	post := make([]byte, 32)
	binary.BigEndian.PutUint32(post[0:], 0x00030000) // format 3.0

	// name: minimal table with no records.
	name := make([]byte, 6)
	binary.BigEndian.PutUint16(name[0:], 0) // format
	binary.BigEndian.PutUint16(name[2:], 0) // count
	binary.BigEndian.PutUint16(name[4:], 6) // stringOffset

	// loca: two entries (gid 0 and gid 1), both pointing to offset 0.
	loca := make([]byte, 6)

	// glyf: empty.
	glyf := []byte{}

	tables := map[string][]byte{
		"head": head,
		"hhea": hhea,
		"maxp": maxp,
		"hmtx": hmtx,
		"cmap": cmap,
		"post": post,
		"name": name,
		"loca": loca,
		"glyf": glyf,
	}
	return buildTableDirectoryTestOnly(tables)
}

func buildTableDirectoryTestOnly(tables map[string][]byte) []byte {
	numTables := len(tables)
	headerSize := 12 + numTables*16
	totalSize := headerSize
	for _, data := range tables {
		totalSize += len(data)
	}
	buf := make([]byte, totalSize)
	binary.BigEndian.PutUint32(buf[0:4], 0x00010000)
	binary.BigEndian.PutUint16(buf[4:6], uint16(numTables))

	offset := uint32(headerSize)
	i := 0
	for tag, data := range tables {
		recOff := 12 + i*16
		copy(buf[recOff:recOff+4], tag)
		binary.BigEndian.PutUint32(buf[recOff+8:recOff+12], offset)
		binary.BigEndian.PutUint32(buf[recOff+12:recOff+16], uint32(len(data)))
		copy(buf[offset:], data)
		offset += uint32(len(data))
		i++
	}
	return buf
}

func buildCmapFormat4TestOnly(mappings map[uint16]uint16) []byte {
	type segment struct {
		start, end uint16
		delta      int16
	}
	var segs []segment
	for charCode, gid := range mappings {
		segs = append(segs, segment{charCode, charCode, int16(gid) - int16(charCode)})
	}
	segs = append(segs, segment{0xFFFF, 0xFFFF, 1})

	segCount := len(segs)
	length := 14 + segCount*8 + 2
	data := make([]byte, length)
	binary.BigEndian.PutUint16(data[0:], 4)
	binary.BigEndian.PutUint16(data[2:], uint16(length))
	binary.BigEndian.PutUint16(data[6:], uint16(segCount*2))

	endOff := 14
	startOff := endOff + segCount*2 + 2
	deltaOff := startOff + segCount*2
	rangeOff := deltaOff + segCount*2
	for i, seg := range segs {
		binary.BigEndian.PutUint16(data[endOff+i*2:], seg.end)
		binary.BigEndian.PutUint16(data[startOff+i*2:], seg.start)
		binary.BigEndian.PutUint16(data[deltaOff+i*2:], uint16(seg.delta))
		binary.BigEndian.PutUint16(data[rangeOff+i*2:], 0)
	}
	return data
}

func buildCmapTestOnly(platformID, encodingID uint16, subtable []byte) []byte {
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:], 0)
	binary.BigEndian.PutUint16(header[2:], 1)
	binary.BigEndian.PutUint16(header[4:], platformID)
	binary.BigEndian.PutUint16(header[6:], encodingID)
	binary.BigEndian.PutUint32(header[8:], uint32(len(header)))
	return append(header, subtable...)
}
