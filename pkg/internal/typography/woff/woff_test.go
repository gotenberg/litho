package woff

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// ahemTTF is the sfnt program the testdata files wrap.
const ahemTTF = "../../layout/testdata/Ahem.ttf"

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// tablesOf returns the tables of the sfnt data by tag.
func tablesOf(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	records, err := truetype.ParseTableDirectory(data)
	if err != nil {
		t.Fatalf("table directory: %v", err)
	}
	out := make(map[string][]byte, len(records))
	for _, rec := range records {
		b, err := truetype.TableData(data, records, rec.Tag)
		if err != nil {
			t.Fatalf("table %q: %v", rec.Tag, err)
		}
		out[rec.Tag] = b
	}
	return out
}

// headFields returns head without checkSumAdjustment, the modification
// date and flags bit 11.
func headFields(head []byte) []byte {
	h := bytes.Clone(head)
	clear(h[8:12])
	clear(h[28:36])
	h[16] &^= 0x08
	return h
}

func TestDecode_RebuildsAhem(t *testing.T) {
	orig := readFile(t, ahemTTF)
	want := tablesOf(t, orig)
	wantFont, err := truetype.ParseFont(orig)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Ahem.woff", "Ahem.woff2", "Ahem-hmtx.woff2"} {
		t.Run(name, func(t *testing.T) {
			got, err := Decode(readFile(t, "testdata/"+name))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !truetype.IsSfnt(got) {
				t.Fatal("the result is not an sfnt program")
			}
			tables := tablesOf(t, got)
			if len(tables) != len(want) {
				t.Errorf("%d tables, want %d", len(tables), len(want))
			}
			for tag, w := range want {
				g, ok := tables[tag]
				switch {
				case !ok:
					t.Errorf("table %q missing", tag)
				case tag == "glyf" || tag == "loca":
					// Re-encoded: compared through the outlines below.
				case tag == "head":
					// checkSumAdjustment is recomputed, the encoder stamps
					// its modification date, and a WOFF2 encoder sets
					// flags bit 11 (WOFF 2.0, 5.1).
					if !bytes.Equal(headFields(g), headFields(w)) {
						t.Errorf("head differs")
					}
				case !bytes.Equal(g, w):
					t.Errorf("table %q differs", tag)
				}
			}
			gotFont, err := truetype.ParseFont(got)
			if err != nil {
				t.Fatalf("ParseFont: %v", err)
			}
			if gotFont.NumGlyphs() != wantFont.NumGlyphs() {
				t.Fatalf("%d glyphs, want %d", gotFont.NumGlyphs(), wantFont.NumGlyphs())
			}
			for gid := range wantFont.NumGlyphs() {
				wp, werr := wantFont.GlyphOutline(context.Background(), gid)
				gp, gerr := gotFont.GlyphOutline(context.Background(), gid)
				if (werr == nil) != (gerr == nil) || !reflect.DeepEqual(wp, gp) {
					t.Fatalf("glyph %d: outline differs", gid)
				}
				if gotFont.Advance(gid) != wantFont.Advance(gid) {
					t.Fatalf("glyph %d: advance differs", gid)
				}
			}
		})
	}
}

func TestDecode_Deterministic(t *testing.T) {
	data := readFile(t, "testdata/Ahem.woff2")
	a, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two decodes of one file differ")
	}
}

func TestDecode_RefusesOtherFormats(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("wOF"), readFile(t, ahemTTF), []byte("<html>")} {
		if _, err := Decode(data); !errors.Is(err, ErrNotWOFF) {
			t.Errorf("Decode(%.8q): %v, want ErrNotWOFF", data, err)
		}
	}
}

func TestSniff_Formats(t *testing.T) {
	tests := []struct {
		data []byte
		want Kind
	}{
		{readFile(t, "testdata/Ahem.woff"), WOFF},
		{readFile(t, "testdata/Ahem.woff2"), WOFF2},
		{readFile(t, ahemTTF), Other},
		{[]byte("wO"), Other},
	}
	for _, tt := range tests {
		if got := Sniff(tt.data); got != tt.want {
			t.Errorf("Sniff(%.4q) = %d, want %d", tt.data, got, tt.want)
		}
	}
}

func TestDecode_TruncatedFails(t *testing.T) {
	for _, name := range []string{"Ahem.woff", "Ahem.woff2", "Ahem-hmtx.woff2"} {
		data := readFile(t, "testdata/"+name)
		for _, n := range []int{5, 43, 47, 60, len(data) / 2, len(data) - 1} {
			cut := data[:n]
			if _, err := Decode(cut); err == nil {
				t.Errorf("%s cut to %d bytes decodes", name, n)
			}
		}
	}
}

// patched returns a copy of data with v written big-endian at off.
func patched(data []byte, off int, v uint32, size int) []byte {
	out := bytes.Clone(data)
	switch size {
	case 2:
		binary.BigEndian.PutUint16(out[off:], uint16(v)) //nolint:gosec // test values fit
	case 4:
		binary.BigEndian.PutUint32(out[off:], v)
	}
	return out
}

func TestDecode_RefusesMalformedHeaders(t *testing.T) {
	woff := readFile(t, "testdata/Ahem.woff")
	woff2 := readFile(t, "testdata/Ahem.woff2")
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"woff length", patched(woff, 8, 1, 4), ErrInvalid},
		{"woff no table", patched(woff, 12, 0, 2), ErrInvalid},
		{"woff reserved", patched(woff, 14, 1, 2), ErrInvalid},
		{"woff too many tables", patched(woff, 12, maxTables+1, 2), ErrTooLarge},
		{"woff table outside", patched(woff, woffHeaderSize+4, 1<<30, 4), ErrInvalid},
		{"woff huge table", patched(woff, woffHeaderSize+12, MaxSfntBytes, 4), ErrTooLarge},
		{"woff2 length", patched(woff2, 8, 1, 4), ErrInvalid},
		{"woff2 no table", patched(woff2, 12, 0, 2), ErrInvalid},
		{"woff2 too many tables", patched(woff2, 12, maxTables+1, 2), ErrTooLarge},
		{"woff2 compressed size", patched(woff2, 20, 1<<30, 4), ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode(tt.data); !errors.Is(err, tt.want) {
				t.Errorf("Decode: %v, want %v", err, tt.want)
			}
		})
	}
}

func TestReader_Base128(t *testing.T) {
	tests := []struct {
		in   []byte
		want uint32
		ok   bool
	}{
		{[]byte{0x3F}, 63, true},
		{[]byte{0x81, 0x00}, 128, true},
		{[]byte{0x8F, 0xFF, 0xFF, 0xFF, 0x7F}, 0xFFFFFFFF, true},
		{[]byte{0x80, 0x01}, 0, false},
		{[]byte{0x90, 0x80, 0x80, 0x80, 0x00}, 0, false},
		{[]byte{0x81, 0x81, 0x81, 0x81, 0x81, 0x01}, 0, false},
		{[]byte{0x81}, 0, false},
	}
	for _, tt := range tests {
		r := &reader{b: tt.in}
		got := r.base128()
		if (r.err == nil) != tt.ok || got != tt.want {
			t.Errorf("base128(% x) = %d, %v", tt.in, got, r.err)
		}
	}
}

func TestReader_U255(t *testing.T) {
	tests := []struct {
		in   []byte
		want uint16
	}{
		{[]byte{252}, 252},
		{[]byte{255, 0}, 253},
		{[]byte{254, 0}, 506},
		{[]byte{253, 0x01, 0x00}, 256},
	}
	for _, tt := range tests {
		r := &reader{b: tt.in}
		if got := r.u255(); got != tt.want || r.err != nil {
			t.Errorf("u255(% x) = %d, %v; want %d", tt.in, got, r.err, tt.want)
		}
	}
}

// transformedGlyf builds a transformed glyf table (WOFF 2.0, 5.1) from its
// streams.
func transformedGlyf(numGlyphs, indexFormat uint16, streams [7][]byte, options uint16, overlap []byte) []byte {
	var b []byte
	b = binary.BigEndian.AppendUint16(b, 0)
	b = binary.BigEndian.AppendUint16(b, options)
	b = binary.BigEndian.AppendUint16(b, numGlyphs)
	b = binary.BigEndian.AppendUint16(b, indexFormat)
	for _, s := range streams {
		b = binary.BigEndian.AppendUint32(b, uint32(len(s))) //nolint:gosec // test streams are small
	}
	for _, s := range streams {
		b = append(b, s...)
	}
	return append(b, overlap...)
}

func TestReconstructGlyf_SimpleAndComposite(t *testing.T) {
	// Glyph 0: empty. Glyph 1: a 100-unit square from (10, 0), one contour
	// of four on-curve points. Glyph 2: glyph 1 offset by (5, 6), with an
	// explicit box and two bytes of instructions.
	nContour := []byte{0, 0, 0, 1, 0xFF, 0xFF}
	nPoints := []byte{4}
	flags := []byte{
		10 + 1, // dx = +10, dy = 0 (flag 10..19, positive: 1 byte)
		0 + 1,  // dy = +100
		10 + 1, // dx = +100
		0,      // dy = -100
	}
	glyph := []byte{
		10, 100, 100, 100, // triplet bytes of the four points
		0, // glyph 1: no instructions
		2, // glyph 2: two instruction bytes
	}
	var composite []byte
	composite = binary.BigEndian.AppendUint16(composite, compHaveInstruction|0x0002)
	composite = binary.BigEndian.AppendUint16(composite, 1)
	composite = append(composite, 5, 6)
	bitmap := []byte{0x20, 0, 0, 0} // glyph 2 has an explicit box
	var bbox []byte
	bbox = append(bbox, bitmap...)
	for _, v := range []int16{15, 6, 115, 106} {
		bbox = binary.BigEndian.AppendUint16(bbox, uint16(v)) //nolint:gosec // test values fit
	}
	instr := []byte{0xB0, 0x01}
	data := transformedGlyf(3, 0, [7][]byte{nContour, nPoints, flags, glyph, composite, bbox, instr}, 1, []byte{0x40, 0, 0, 0})
	res, err := reconstructGlyf(data)
	if err != nil {
		t.Fatalf("reconstructGlyf: %v", err)
	}
	loca := res.loca
	off := func(i int) int { return 2 * int(binary.BigEndian.Uint16(loca[2*i:])) }
	if off(0) != 0 || off(1) != 0 {
		t.Fatalf("glyph 0 not empty: loca % x", loca)
	}
	g1 := res.glyf[off(1):off(2)]
	wantBox := []int16{10, 0, 110, 100}
	for k, w := range wantBox {
		if got := int16(binary.BigEndian.Uint16(g1[2+2*k:])); got != w { //nolint:gosec // test
			t.Errorf("glyph 1 box[%d] = %d, want %d", k, got, w)
		}
	}
	if res.xMin[1] != 10 || res.xMin[2] != 15 || res.xMin[0] != 0 {
		t.Errorf("xMin = %v", res.xMin)
	}
	// Glyph 1 holds endPts (1 contour), instruction length 0, then flags:
	// the first carries OVERLAP_SIMPLE.
	if g1[10+2+2]&flagOverlapSimpl == 0 {
		t.Errorf("glyph 1 lacks OVERLAP_SIMPLE: % x", g1)
	}
	g2 := res.glyf[off(2):off(3)]
	if int16(binary.BigEndian.Uint16(g2)) != -1 { //nolint:gosec // test
		t.Fatalf("glyph 2 is not composite: % x", g2)
	}
	if !bytes.Contains(g2, append(composite, 0, 2, 0xB0, 0x01)) {
		t.Errorf("glyph 2 lacks its component or instructions: % x", g2)
	}
	// The rebuilt pair parses: glyph 2 draws glyph 1 moved by (5, 6).
	font := minimalSfnt(t, res)
	p1, err := font.GlyphOutline(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := font.GlyphOutline(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Pts) == 0 || len(p1.Pts) != len(p2.Pts) {
		t.Fatalf("outlines: %v and %v", p1, p2)
	}
	dx, dy := p2.Pts[0].X-p1.Pts[0].X, p2.Pts[0].Y-p1.Pts[0].Y
	for i := range p1.Pts {
		if gx, gy := p2.Pts[i].X-p1.Pts[i].X, p2.Pts[i].Y-p1.Pts[i].Y; gx != dx || gy != dy || dx == 0 || dy == 0 {
			t.Fatalf("point %d moved by (%v, %v), the first by (%v, %v)", i, gx, gy, dx, dy)
		}
	}
}

// minimalSfnt wraps a rebuilt glyf and loca into a program ParseFont reads.
func minimalSfnt(t *testing.T, res *glyfResult) *truetype.Font {
	t.Helper()
	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[12:], 0x5F0F3CF5)
	binary.BigEndian.PutUint16(head[18:], 1000)
	binary.BigEndian.PutUint16(head[50:], res.indexFormat)
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint32(maxp, 0x00005000)
	binary.BigEndian.PutUint16(maxp[4:], uint16(res.numGlyphs)) //nolint:gosec // test
	tag := func(s string) uint32 { return binary.BigEndian.Uint32([]byte(s)) }
	program, err := assemble(0x00010000, []table{
		{tag: tag("head"), data: head},
		{tag: tag("maxp"), data: maxp},
		{tag: tag("glyf"), data: res.glyf},
		{tag: tag("loca"), data: res.loca},
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := truetype.ParseFont(program)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestReconstructGlyf_RefusesBadGlyphs(t *testing.T) {
	emptyBitmap := []byte{0, 0, 0, 0}
	boxBit := []byte{0x80, 0, 0, 0}
	box := append(bytes.Clone(boxBit), 0, 0, 0, 0, 0, 1, 0, 1)
	tests := []struct {
		name    string
		streams [7][]byte
		format  uint16
	}{
		{"contour stream short", [7][]byte{{0}, nil, nil, nil, nil, emptyBitmap, nil}, 0},
		{"empty glyph with a box", [7][]byte{{0, 0}, nil, nil, nil, nil, box, nil}, 0},
		{"composite without a box", [7][]byte{{0xFF, 0xFF}, nil, nil, nil, {0, 0, 0, 0, 0, 0}, emptyBitmap, nil}, 0},
		{"composite stream short", [7][]byte{{0xFF, 0xFF}, nil, nil, nil, {0, 0x20, 0, 0}, box, nil}, 0},
		{"contour count -2", [7][]byte{{0xFF, 0xFE}, nil, nil, nil, nil, emptyBitmap, nil}, 0},
		{"no points", [7][]byte{{0, 1}, {0}, nil, nil, nil, emptyBitmap, nil}, 0},
		{"flag stream short", [7][]byte{{0, 1}, {3}, {0}, nil, nil, emptyBitmap, nil}, 0},
		{"glyph stream short", [7][]byte{{0, 1}, {1}, {0}, nil, nil, emptyBitmap, nil}, 0},
		{"instructions short", [7][]byte{{0, 1}, {1}, {0}, {0, 9}, nil, emptyBitmap, nil}, 0},
		{"index format 2", [7][]byte{{0, 0}, nil, nil, nil, nil, emptyBitmap, nil}, 2},
		{"bitmap missing", [7][]byte{{0, 0}, nil, nil, nil, nil, nil, nil}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := reconstructGlyf(transformedGlyf(1, tt.format, tt.streams, 0, nil)); !errors.Is(err, ErrInvalid) {
				t.Errorf("reconstructGlyf: %v, want ErrInvalid", err)
			}
		})
	}
}

func TestTriplet_Table4(t *testing.T) {
	tests := []struct {
		flag   int
		in     []byte
		dx, dy int
	}{
		{0, []byte{5}, 0, -5},
		{1, []byte{5}, 0, 5},
		{3, []byte{5}, 0, 256 + 5},
		{10, []byte{7}, -7, 0},
		{11, []byte{7}, 7, 0},
		{20, []byte{0x12}, -2, -3},
		{23, []byte{0x12}, 2, 3},
		{84, []byte{1, 2}, -2, -3},
		{87, []byte{1, 2}, 2, 3},
		{120, []byte{0x12, 0x34, 0x56}, -0x123, -0x456},
		{123, []byte{0x12, 0x34, 0x56}, 0x123, 0x456},
		{124, []byte{0x01, 0x02, 0x03, 0x04}, -0x102, -0x304},
		{127, []byte{0x01, 0x02, 0x03, 0x04}, 0x102, 0x304},
	}
	for _, tt := range tests {
		s := &glyfStreams{glyph: reader{b: tt.in}}
		dx, dy, ok := s.triplet(tt.flag)
		if !ok || dx != tt.dx || dy != tt.dy {
			t.Errorf("triplet(%d, % x) = %d, %d, %v; want %d, %d", tt.flag, tt.in, dx, dy, ok, tt.dx, tt.dy)
		}
	}
}

func TestReconstructHmtx_Flags(t *testing.T) {
	xMin := []int16{0, 10, 20}
	tests := []struct {
		name string
		data []byte
		want []byte
		ok   bool
	}{
		{"both derived", []byte{3, 0, 100, 0, 200}, []byte{0, 100, 0, 0, 0, 200, 0, 10, 0, 20}, true},
		{"monospaced stored", []byte{1, 0, 100, 0, 200, 0, 7}, []byte{0, 100, 0, 0, 0, 200, 0, 10, 0, 7}, true},
		{"no flag", []byte{0, 0, 100, 0, 200}, nil, false},
		{"reserved bit", []byte{7, 0, 100, 0, 200}, nil, false},
		{"short", []byte{3, 0}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reconstructHmtx(tt.data, 2, xMin)
			if (err == nil) != tt.ok || !bytes.Equal(got, tt.want) {
				t.Errorf("reconstructHmtx = % x, %v; want % x", got, err, tt.want)
			}
		})
	}
	if _, err := reconstructHmtx([]byte{3, 0, 1}, 4, xMin); err == nil {
		t.Error("more metrics than glyphs accepted")
	}
}
