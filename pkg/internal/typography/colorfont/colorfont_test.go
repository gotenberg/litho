package colorfont

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"slices"
	"testing"
)

// sfnt assembles a program from tables, sorted by tag, with a head of
// unitsPerEm upem and a maxp of numGlyphs glyphs.
func sfnt(t testing.TB, upem, numGlyphs int, tables map[string][]byte) []byte {
	t.Helper()
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:], uint16(upem))
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:], uint16(numGlyphs))
	all := map[string][]byte{"head": head, "maxp": maxp}
	for k, v := range tables {
		all[k] = v
	}
	var tags []string
	for k := range all {
		tags = append(tags, k)
	}
	slices.Sort(tags)
	out := make([]byte, 12+16*len(tags))
	binary.BigEndian.PutUint32(out, 0x00010000)
	binary.BigEndian.PutUint16(out[4:], uint16(len(tags)))
	for i, tag := range tags {
		rec := out[12+16*i:]
		copy(rec, tag)
		binary.BigEndian.PutUint32(rec[8:], uint32(len(out)))
		binary.BigEndian.PutUint32(rec[12:], uint32(len(all[tag])))
		out = append(out, all[tag]...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	return out
}

// pngOf encodes a w by h picture of one colour.
func pngOf(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xC0
	}
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type be struct{ b []byte }

func (w *be) u8(v int)  { w.b = append(w.b, byte(v)) }
func (w *be) u16(v int) { w.b = binary.BigEndian.AppendUint16(w.b, uint16(v)) }
func (w *be) u32(v int) { w.b = binary.BigEndian.AppendUint32(w.b, uint32(v)) }

// cbTables builds a CBLC and CBDT pair of one strike at ppem holding glyph
// 1 in index format 1, image format 17, and glyph 3 in index format 4,
// image format 18.
func cbTables(t testing.TB, ppem int) (cblc, cbdt []byte) {
	t.Helper()
	img := pngOf(t, 4, 3)
	var d be
	d.u32(0x00030000)
	g1 := len(d.b)
	d.u8(3) // height
	d.u8(4) // width
	d.u8(1) // bearing x
	d.u8(2) // bearing y
	d.u8(5) // advance
	d.u32(len(img))
	d.b = append(d.b, img...)
	g1end := len(d.b)
	g3 := len(d.b)
	d.u8(3)
	d.u8(4)
	d.u8(0xFF) // bearing x -1
	d.u8(7)
	d.u8(5)
	d.u8(0)
	d.u8(0)
	d.u8(0)
	d.u32(len(img))
	d.b = append(d.b, img...)
	g3end := len(d.b)

	var c be
	c.u16(3)
	c.u16(0)
	c.u32(1)
	arrayOff := 8 + 48
	c.u32(arrayOff)
	c.u32(0)
	c.u32(2) // two index subtables
	c.u32(0)
	c.b = append(c.b, make([]byte, 24)...)
	c.u16(1)
	c.u16(3)
	c.u8(ppem)
	c.u8(ppem)
	c.u8(32)
	c.u8(1)
	// IndexSubTableArray: two records, offsets from the array.
	c.u16(1)
	c.u16(1)
	c.u32(16)
	c.u16(3)
	c.u16(3)
	c.u32(16 + 16)
	// Index format 1, image format 17: offsets from the image data offset.
	c.u16(1)
	c.u16(17)
	c.u32(g1)
	c.u32(0)
	c.u32(g1end - g1)
	// Index format 4, image format 18.
	c.u16(4)
	c.u16(18)
	c.u32(0)
	c.u32(1)
	c.u16(3)
	c.u16(g3)
	c.u16(0)
	c.u16(g3end)
	return c.b, d.b
}

// sbixTable builds an sbix table of one strike at ppem for 4 glyphs: glyph
// 2 a PNG, glyph 3 a dupe of glyph 2.
func sbixTable(t testing.TB, ppem int) []byte {
	t.Helper()
	img := pngOf(t, 6, 5)
	var s be
	s.u16(1)
	s.u16(1)
	s.u32(1)
	s.u32(12)
	strike := len(s.b)
	s.u16(ppem)
	s.u16(72)
	offs := len(s.b)
	s.b = append(s.b, make([]byte, 4*5)...)
	set := func(i, v int) { binary.BigEndian.PutUint32(s.b[offs+4*i:], uint32(v-strike)) }
	set(0, len(s.b))
	set(1, len(s.b))
	set(2, len(s.b))
	s.u16(2)
	s.u16(0xFFFF) // y -1
	s.b = append(s.b, "png "...)
	s.b = append(s.b, img...)
	set(3, len(s.b))
	s.u16(0)
	s.u16(0)
	s.b = append(s.b, "dupe"...)
	s.u16(2)
	set(4, len(s.b))
	return s.b
}

// colrTables builds COLR version 0 with glyph 5 drawn by layers 1 (palette
// entry 0) and 2 (foreground), and a CPAL version 1 of two palettes of two
// entries, the second marked for dark backgrounds.
func colrTables() (colrData, cpalData []byte) {
	var c be
	c.u16(0)
	c.u16(1)
	c.u32(14)
	c.u32(20)
	c.u16(2)
	c.u16(5)
	c.u16(0)
	c.u16(2)
	c.u16(1)
	c.u16(0)
	c.u16(2)
	c.u16(Foreground)

	var p be
	p.u16(1)
	p.u16(2) // entries
	p.u16(2) // palettes
	p.u16(4) // records
	p.u32(12 + 4 + 12)
	p.u16(0)
	p.u16(2)
	p.u32(12 + 4 + 12 + 16) // types
	p.u32(0)
	p.u32(0)
	for _, rgba := range [][4]int{{255, 0, 0, 255}, {0, 0, 255, 128}, {1, 2, 3, 4}, {5, 6, 7, 8}} {
		p.u8(rgba[2])
		p.u8(rgba[1])
		p.u8(rgba[0])
		p.u8(rgba[3])
	}
	p.u32(0)
	p.u32(paletteDark)
	return c.b, p.b
}

func TestParse_NoColourTable(t *testing.T) {
	_, err := Parse(sfnt(t, 1000, 4, nil))
	if !errors.Is(err, ErrNoColor) {
		t.Fatalf("Parse = %v, want ErrNoColor", err)
	}
}

func TestBitmap_CBDT(t *testing.T) {
	cblc, cbdt := cbTables(t, 20)
	f, err := Parse(sfnt(t, 1000, 4, map[string][]byte{"CBLC": cblc, "CBDT": cbdt}))
	if err != nil {
		t.Fatal(err)
	}
	if !f.HasBitmaps() || f.HasLayers() {
		t.Fatalf("HasBitmaps %v HasLayers %v", f.HasBitmaps(), f.HasLayers())
	}
	bm, ok, err := f.Bitmap(1, 12)
	if err != nil || !ok {
		t.Fatalf("Bitmap(1) = %v, %v", ok, err)
	}
	if bm.Format != PNG || bm.PPEM != 20 || bm.Width != 4 || bm.Height != 3 || bm.Left != 1 || bm.Top != 2 {
		t.Errorf("glyph 1 = %+v", bm)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(bm.Data)); err != nil {
		t.Errorf("glyph 1 image: %v", err)
	}
	bm, ok, err = f.Bitmap(3, 12)
	if err != nil || !ok || bm.Left != -1 || bm.Top != 7 {
		t.Errorf("Bitmap(3) = %+v, %v, %v", bm, ok, err)
	}
	if _, ok, err := f.Bitmap(2, 12); ok || err != nil {
		t.Errorf("Bitmap(2) = %v, %v, want none", ok, err)
	}
}

func TestBitmap_Sbix(t *testing.T) {
	f, err := Parse(sfnt(t, 1000, 4, map[string][]byte{"sbix": sbixTable(t, 40)}))
	if err != nil {
		t.Fatal(err)
	}
	bm, ok, err := f.Bitmap(2, 40)
	if err != nil || !ok {
		t.Fatalf("Bitmap(2) = %v, %v", ok, err)
	}
	// The image's bottom sits at the y offset, -1: its top at 4.
	if bm.Width != 6 || bm.Height != 5 || bm.Left != 2 || bm.Top != 4 || bm.PPEM != 40 {
		t.Errorf("glyph 2 = %+v", bm)
	}
	dupe, ok, err := f.Bitmap(3, 40)
	if err != nil || !ok || !bytes.Equal(dupe.Data, bm.Data) {
		t.Errorf("dupe = %v, %v", ok, err)
	}
	if _, ok, _ := f.Bitmap(1, 40); ok {
		t.Error("glyph 1 has no data, got a bitmap")
	}
}

func TestPreference_NearestStrikeAtOrAboveTheSize(t *testing.T) {
	got := preference([]int{20, 160, 40, 96}, 30)
	if want := []int{2, 3, 1, 0}; !slices.Equal(got, want) {
		t.Errorf("preference = %v, want %v", got, want)
	}
	got = preference([]int{20, 40}, 300)
	if want := []int{1, 0}; !slices.Equal(got, want) {
		t.Errorf("preference past the largest = %v, want %v", got, want)
	}
}

func TestLayersAndPalettes(t *testing.T) {
	colrData, cpalData := colrTables()
	f, err := Parse(sfnt(t, 1000, 8, map[string][]byte{"COLR": colrData, "CPAL": cpalData}))
	if err != nil {
		t.Fatal(err)
	}
	layers, ok, err := f.Layers(5)
	if err != nil || !ok {
		t.Fatalf("Layers(5) = %v, %v", ok, err)
	}
	if want := []Layer{{1, 0}, {2, Foreground}}; !slices.Equal(layers, want) {
		t.Errorf("layers = %v, want %v", layers, want)
	}
	if _, ok, _ := f.Layers(4); ok {
		t.Error("glyph 4 has no layers")
	}
	if f.NumPalettes() != 2 {
		t.Fatalf("NumPalettes = %d", f.NumPalettes())
	}
	if got := f.Palette(0); !slices.Equal(got, []Color{{255, 0, 0, 255}, {0, 0, 255, 128}}) {
		t.Errorf("palette 0 = %v", got)
	}
	if got := f.Palette(1); !slices.Equal(got, []Color{{1, 2, 3, 4}, {5, 6, 7, 8}}) {
		t.Errorf("palette 1 = %v", got)
	}
	if f.Palette(2) != nil {
		t.Error("palette 2 exists")
	}
	if i, ok := f.PaletteForDark(); !ok || i != 1 {
		t.Errorf("PaletteForDark = %d, %v", i, ok)
	}
	if _, ok := f.PaletteForLight(); ok {
		t.Error("no palette is marked for light backgrounds")
	}
}

func TestParse_RefusesTablesPastTheirLength(t *testing.T) {
	cblc, cbdt := cbTables(t, 20)
	colrData, cpalData := colrTables()
	cases := map[string]map[string][]byte{
		"CBLC": {"CBLC": cblc[:60], "CBDT": cbdt},
		"sbix": {"sbix": sbixTable(t, 40)[:20]},
		"COLR": {"COLR": colrData[:16], "CPAL": cpalData},
		"CPAL": {"COLR": colrData, "CPAL": cpalData[:20]},
	}
	for name, tables := range cases {
		if _, err := Parse(sfnt(t, 1000, 8, tables)); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("%s: Parse = %v, want ErrInvalidFont", name, err)
		}
	}
}

func TestBitmap_GlyphDataPastCBDT(t *testing.T) {
	cblc, cbdt := cbTables(t, 20)
	f, err := Parse(sfnt(t, 1000, 4, map[string][]byte{"CBLC": cblc, "CBDT": cbdt[:40]}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Bitmap(1, 20); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("Bitmap = %v, want ErrInvalidFont", err)
	}
}
