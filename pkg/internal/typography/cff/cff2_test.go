package cff

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// cff2Font is a CFF2 font of one Private DICT whose blends take k regions,
// with charStrings as its glyphs. regionCounts are the ItemVariationData of
// its VariationStore, nil for a font without one.
func cff2Font(k int, regionCounts []int, subrs [][]byte, charStrings ...[]byte) *Font {
	return &Font{
		charStrings:  charStrings,
		privates:     []outlinePrivate{{subrs: subrs, matrix: defaultFontMatrix, regions: k}},
		cff2:         true,
		regionCounts: regionCounts,
	}
}

// cff2Index encodes a CFF2 INDEX with 4-byte offsets.
func cff2Index(entries ...[]byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(entries))) //nolint:gosec // G115: test INDEXes hold a few entries.
	if len(entries) == 0 {
		return out
	}
	out = append(out, 4)
	off := 1
	out = binary.BigEndian.AppendUint32(out, uint32(off)) //nolint:gosec // G115: small test offsets.
	for _, e := range entries {
		off += len(e)
		out = binary.BigEndian.AppendUint32(out, uint32(off)) //nolint:gosec // G115: small test offsets.
	}
	for _, e := range entries {
		out = append(out, e...)
	}
	return out
}

// dictInt is a DICT integer in the 5-byte form.
func dictInt(v int) []byte {
	return binary.BigEndian.AppendUint32([]byte{29}, uint32(int32(v))) //nolint:gosec // G115: small test offsets.
}

// cff2Table assembles a CFF2 table: a Top DICT naming the CharStrings INDEX,
// an FDArray of one Font DICT whose Private DICT is private, and fdSelect when
// it is not nil.
func cff2Table(private []byte, fdSelect []byte, charStrings ...[]byte) []byte {
	topOf := func(cs, fdArray, fdSel int) []byte {
		t := cs2(dictInt(cs), []byte{opCharStrings}, dictInt(fdArray), []byte{12, 36})
		if fdSelect != nil {
			t = cs2(t, dictInt(fdSel), []byte{12, 37})
		}
		return t
	}
	topLen := len(topOf(0, 0, 0))
	gsubrs := cff2Index()
	pos := cff2HeaderSize + topLen + len(gsubrs)
	csIndex := cff2Index(charStrings...)
	csOff := pos
	pos += len(csIndex)
	fdSelOff := pos
	pos += len(fdSelect)
	fdArrayLen := len(cff2Index(make([]byte, 11)))
	fdArrayOff := pos
	pos += fdArrayLen
	fontDict := cs2(dictInt(len(private)), dictInt(pos), []byte{opPrivate})
	top := topOf(csOff, fdArrayOff, fdSelOff)
	header := []byte{2, 0, cff2HeaderSize, 0, byte(len(top))}
	return cs2(header, top, gsubrs, csIndex, fdSelect, cff2Index(fontDict), private)
}

// cs2 joins byte slices.
func cs2(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestParseCFF2Font(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/cff2_ops.cff2")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseCFF2(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.NumGlyphs() != 7 || f.CIDKeyed() {
		t.Errorf("NumGlyphs %d, CIDKeyed %v, want 7 and false", f.NumGlyphs(), f.CIDKeyed())
	}
	if _, ok := f.GlyphForName(".notdef"); ok {
		t.Error("a CFF2 font named a glyph: it has no charset")
	}
	want := geom.Matrix{0.0005, 0, 0, 0.0005, 0, 0}
	for gid := range f.NumGlyphs() {
		if got := f.FontMatrix(gid); got != want {
			t.Errorf("FontMatrix(%d) = %v, want %v", gid, got, want)
		}
	}
	if got := f.privateFor(3).regions; got != 1 {
		t.Errorf("glyph 3 blends over %d regions, want the 1 its Private DICT's vsindex selects", got)
	}
	if got := f.privateFor(1).regions; got != 2 {
		t.Errorf("glyph 1 blends over %d regions, want 2", got)
	}
}

func TestParseCFF2DefaultMatrixAndNoFDSelect(t *testing.T) {
	t.Parallel()
	data := cff2Table(nil, nil, cs(num(0, 0), op(csRmoveto), num(10, 10), op(csRlineto)))
	f, err := ParseCFF2(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.FontMatrix(0); got != defaultFontMatrix {
		t.Errorf("FontMatrix = %v, want the default", got)
	}
	g, err := f.Glyph(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if g.Advance != 0 || len(g.Path.Ops) != 3 {
		t.Errorf("glyph: advance %v, %d ops, want 0 and moveto, lineto, close", g.Advance, len(g.Path.Ops))
	}
}

func TestParseCFF2Rejects(t *testing.T) {
	t.Parallel()
	cff1, err := os.ReadFile("testdata/type2_ops.cff")
	if err != nil {
		t.Fatal(err)
	}
	good := cff2Table(nil, nil, cs(num(0, 0), op(csRmoveto)))
	badTop := append([]byte(nil), good...)
	badTop[4] = 0xFF
	badVSIndex := cff2Table(cs2(dictInt(3), []byte{opCFF2VSIndex}), nil, cs(num(0, 0), op(csRmoveto)))
	badSubrs := cff2Table(cs2(dictInt(1<<20), []byte{opSubrs}), nil, cs(num(0, 0), op(csRmoveto)))
	for name, data := range map[string][]byte{
		"CFF1":                  cff1,
		"short":                 {2, 0, 5},
		"Top DICT past the end": badTop,
		"no glyphs":             cff2Table(nil, nil),
		"vsindex without store": badVSIndex,
		"Subrs past the end":    badSubrs,
	} {
		if _, err := ParseCFF2(data); !errors.Is(err, ErrInvalidCFF) && !errors.Is(err, ErrTruncated) {
			t.Errorf("%s: err = %v, want ErrInvalidCFF or ErrTruncated", name, err)
		}
	}
	if _, err := ParseCFF2(good); err != nil {
		t.Errorf("control: %v", err)
	}
}

func TestCFF2FDSelectFormat4(t *testing.T) {
	t.Parallel()
	// Two ranges: glyph 0 in Font DICT 0, glyphs 1-2 in Font DICT 300, past
	// the byte formats 0 and 3 hold.
	sel := []byte{fdSelectFormat4}
	sel = binary.BigEndian.AppendUint32(sel, 2)
	sel = binary.BigEndian.AppendUint32(sel, 0)
	sel = binary.BigEndian.AppendUint16(sel, 0)
	sel = binary.BigEndian.AppendUint32(sel, 1)
	sel = binary.BigEndian.AppendUint16(sel, 300)
	sel = binary.BigEndian.AppendUint32(sel, 3)
	got := parseCFF2FDSelect(sel, 0, 3)
	if want := []uint16{0, 300, 300}; got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("format 4 = %v, want %v", got, want)
	}
	// A range that steps back ends the read.
	back := append([]byte(nil), sel[:5]...)
	back = binary.BigEndian.AppendUint32(back, 2)
	back = binary.BigEndian.AppendUint16(back, 7)
	back = binary.BigEndian.AppendUint32(back, 0)
	back = binary.BigEndian.AppendUint16(back, 9)
	back = binary.BigEndian.AppendUint32(back, 3)
	if got := parseCFF2FDSelect(back, 0, 3); got[0] != 0 || got[2] != 0 {
		t.Errorf("unsorted ranges = %v, want the read to stop", got)
	}
	// Format 3 through the CFF reader, which takes no FDSelect at offset 0.
	f3 := []byte{0xFF, fdSelectFormat3, 0, 2, 0, 0, 0, 0, 1, 1, 0, 3}
	if got := parseCFF2FDSelect(f3, 1, 3); got[0] != 0 || got[1] != 1 || got[2] != 1 {
		t.Errorf("format 3 = %v, want [0 1 1]", got)
	}
}

func TestCFF2CharstringOperators(t *testing.T) {
	t.Parallel()
	rect := cs(num(0, 0), op(csRmoveto), num(10, 0), op(csRlineto))
	tests := []struct {
		name    string
		k       int
		regions []int
		cs      []byte
		ops     int
		wantErr bool
	}{
		{"blend keeps the defaults", 2, []int{2}, cs(num(5, 6, 1, 2, 3, 4, 2), op(csBlend), op(csRmoveto)), 2, false},
		{"blend without a store", noVariationStore, nil, cs(num(5, 1, 1), op(csBlend)), 0, true},
		{"blend short of operands", 2, []int{2}, cs(num(5, 1, 2), op(csBlend)), 0, true},
		{"blend of a negative count", 2, []int{2}, cs(num(-1), op(csBlend)), 0, true},
		{"vsindex selects k", 2, []int{2, 1}, cs(num(1), op(csVsindex), num(5, 6, 1, 2, 2), op(csBlend), op(csRmoveto)), 2, false},
		{"vsindex past the store", 2, []int{2}, cs(num(1), op(csVsindex)), 0, true},
		// CFF2 drops endchar and return: each clears the stack and the
		// charstring runs on.
		{"endchar does not end", 0, []int{0}, cs(rect, op(csEndchar), num(0, 10), op(csRlineto)), 4, false},
		{"return is reserved", 0, []int{0}, cs(rect, num(1), op(csReturn), num(0, 10), op(csRlineto)), 4, false},
		// CFF2 drops the arithmetic operators: add clears the stack, so the
		// rlineto after it has no operand.
		{"add is reserved", 0, []int{0}, cs(num(0, 0), op(csRmoveto), num(1, 2), esc(csAdd), op(csRlineto)), 2, false},
		{"flex stays", 0, []int{0}, cs(num(0, 0), op(csRmoveto), num(1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 50), esc(csFlex)), 4, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cff2Font(tt.k, tt.regions, nil, tt.cs)
			g, err := f.Glyph(t.Context(), 0)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if err != nil {
				if !errors.Is(err, ErrInvalidCharstring) {
					t.Errorf("err = %v, want ErrInvalidCharstring", err)
				}
				return
			}
			if len(g.Path.Ops) != tt.ops {
				t.Errorf("%d ops %v, want %d", len(g.Path.Ops), g.Path.Ops, tt.ops)
			}
			if g.Advance != 0 {
				t.Errorf("Advance = %v, want 0", g.Advance)
			}
		})
	}
}

func TestCFF2StackLimit(t *testing.T) {
	t.Parallel()
	under := cs(num(0, 0), op(csRmoveto))
	for range maxCharstringStack {
		under = append(under, num(0)...)
	}
	under = append(under, csRlineto)
	if _, err := cff2Font(0, []int{0}, nil, under).Glyph(t.Context(), 0); err != nil {
		t.Errorf("513 operands: %v", err)
	}
	over := cs(under[:len(under)-1], num(0), op(csRlineto))
	if _, err := cff2Font(0, []int{0}, nil, over).Glyph(t.Context(), 0); !errors.Is(err, ErrInvalidCharstring) {
		t.Errorf("514 operands: err = %v, want ErrInvalidCharstring", err)
	}
	// A CFF program keeps the Type 2 limit of 48.
	if _, err := charstringFont(nil, under).Glyph(t.Context(), 0); !errors.Is(err, ErrInvalidCharstring) {
		t.Errorf("CFF, 513 operands: err = %v, want ErrInvalidCharstring", err)
	}
}

func TestScanCFF2DictBlend(t *testing.T) {
	t.Parallel()
	// 55 -29 19 1 blend StdHW, then 7 Subrs, with k = 2.
	dict := cs2(num(55, -29, 19, 1), []byte{opCFF2Blend, 10}, num(7), []byte{opSubrs})
	var got [][]float64
	scanCFF2Dict(dict, func() int { return 2 }, func(_ int, operands []float64) {
		got = append(got, append([]float64(nil), operands...))
	})
	if len(got) != 2 || len(got[0]) != 1 || got[0][0] != 55 || got[1][0] != 7 {
		t.Errorf("operands = %v, want [[55] [7]]", got)
	}
	// Without a VariationStore the blend clears its operands.
	got = nil
	scanCFF2Dict(dict, func() int { return noVariationStore }, func(_ int, operands []float64) {
		got = append(got, append([]float64(nil), operands...))
	})
	if len(got) != 2 || len(got[0]) != 0 {
		t.Errorf("no store: operands = %v, want StdHW without operands", got)
	}
}

func BenchmarkCFF2Glyph(b *testing.B) {
	data, err := os.ReadFile("testdata/cff2_ops.cff2")
	if err != nil {
		b.Fatal(err)
	}
	f, err := ParseCFF2(data)
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		for gid := range f.NumGlyphs() {
			if _, err := f.Glyph(ctx, gid); err != nil {
				b.Fatal(err)
			}
		}
	}
}
