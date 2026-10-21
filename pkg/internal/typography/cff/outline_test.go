package cff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// outlineGolden is fontTools' reading of a CFF font's glyphs: the path its
// Type 2 interpreter draws (seac glyphs decomposed) and the advance width.
type outlineGolden struct {
	Glyphs []struct {
		GID     int             `json:"gid"`
		Name    string          `json:"name"`
		Ops     [][]interface{} `json:"ops"`
		Advance float64         `json:"advance"`
	} `json:"glyphs"`
}

// outlineFontPath returns the file a golden was recorded from.
func outlineFontPath(name string) string {
	if name == "lato_bold_subset.cff" {
		return "../../compliance/testdata/" + name
	}
	return "testdata/" + name
}

// loadOutlineGoldens reads testdata/outlines_golden.json, recorded by
// fontTools 4.60 from the package's test fonts, compliance's Lato subset, and
// testdata/type2_ops.cff, a font crafted to run every Type 2 path operator,
// the flex family, local and global subroutines (one pushing the width),
// hintmask and cntrmask over implicit vstems and multi-byte masks, vstemhm,
// a seac endchar and glyphs with and without a width operand, and
// testdata/cff2_ops.cff2, a CFF2 table build_cff2_ops.py crafts to run blend
// and vsindex in charstrings and Private DICTs, two Font DICTs and 121
// operands at once.
func loadOutlineGoldens(t *testing.T) map[string]outlineGolden {
	t.Helper()
	raw, err := os.ReadFile("testdata/outlines_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var goldens map[string]outlineGolden
	if err := json.Unmarshal(raw, &goldens); err != nil {
		t.Fatal(err)
	}
	return goldens
}

// pathOps renders a path as rows of an op code and its coordinates.
func pathOps(p geom.Path) [][]float64 {
	var out [][]float64
	k := 0
	for _, op := range p.Ops {
		switch op {
		case geom.MoveTo, geom.LineTo:
			out = append(out, []float64{float64(op), p.Pts[k].X, p.Pts[k].Y})
			k++
		case geom.CubicTo:
			out = append(out, []float64{float64(op), p.Pts[k].X, p.Pts[k].Y, p.Pts[k+1].X, p.Pts[k+1].Y, p.Pts[k+2].X, p.Pts[k+2].Y})
			k += 3
		case geom.Close:
			out = append(out, []float64{float64(op)})
		}
	}
	return out
}

// goldenOps converts fontTools' recorded pen calls to [pathOps] rows.
func goldenOps(t *testing.T, ops [][]interface{}) [][]float64 {
	t.Helper()
	codes := map[string]geom.Op{"M": geom.MoveTo, "L": geom.LineTo, "C": geom.CubicTo, "Z": geom.Close}
	var out [][]float64
	for _, op := range ops {
		code, ok := codes[op[0].(string)]
		if !ok {
			t.Fatalf("golden op %v", op)
		}
		row := []float64{float64(code)}
		for _, v := range op[1:] {
			row = append(row, v.(float64))
		}
		out = append(out, row)
	}
	return out
}

// samePath compares two paths in [pathOps] rows.
func samePath(got, want [][]float64) error {
	if len(got) != len(want) {
		return fmt.Errorf("%d ops, want %d:\n got %v\nwant %v", len(got), len(want), got, want)
	}
	for i := range got {
		if len(got[i]) != len(want[i]) {
			return fmt.Errorf("op %d: %v, want %v", i, got[i], want[i])
		}
		for j := range got[i] {
			if math.Abs(got[i][j]-want[i][j]) > 1e-9*math.Max(1, math.Abs(want[i][j])) {
				return fmt.Errorf("op %d: %v, want %v", i, got[i], want[i])
			}
		}
	}
	return nil
}

func TestGlyphMatchesFontTools(t *testing.T) {
	t.Parallel()
	for name, golden := range loadOutlineGoldens(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(outlineFontPath(name))
			if err != nil {
				t.Fatal(err)
			}
			parse := ParseFont
			if strings.HasSuffix(name, ".cff2") {
				parse = ParseCFF2
			}
			f, err := parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if f.NumGlyphs() != len(golden.Glyphs) {
				t.Fatalf("NumGlyphs = %d, want %d", f.NumGlyphs(), len(golden.Glyphs))
			}
			for _, g := range golden.Glyphs {
				glyph, err := f.Glyph(t.Context(), g.GID)
				if err != nil {
					t.Fatalf("glyph %s: %v", g.Name, err)
				}
				if err := samePath(pathOps(glyph.Path), goldenOps(t, g.Ops)); err != nil {
					t.Errorf("glyph %s: %v", g.Name, err)
				}
				if glyph.Advance != g.Advance {
					t.Errorf("glyph %s: Advance = %v, want %v", g.Name, glyph.Advance, g.Advance)
				}
			}
		})
	}
}

func TestFontMatrixAndLookups(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file string
		cid  bool
		want geom.Matrix
	}{
		{"testdata/type2_ops.cff", false, defaultFontMatrix},
		{"../../compliance/testdata/lato_bold_subset.cff", false, geom.Matrix{0.0005, 0, 0, 0.0005, 0, 0}},
		// The Top DICT states no FontMatrix and the Font DICT one of 2048
		// units per em.
		{"testdata/cid_fdarray_matrix.cff", true, geom.Matrix{0.00048828099, 0, 0, 0.00048828099, 0, 0}},
	}
	for _, tt := range tests {
		data, err := os.ReadFile(tt.file)
		if err != nil {
			t.Fatal(err)
		}
		f, err := ParseFont(data)
		if err != nil {
			t.Fatal(err)
		}
		if f.CIDKeyed() != tt.cid {
			t.Errorf("%s: CIDKeyed = %v", tt.file, f.CIDKeyed())
		}
		for i, v := range f.FontMatrix(1) {
			if math.Abs(v-tt.want[i]) > 1e-12 {
				t.Errorf("%s: FontMatrix = %v, want %v", tt.file, f.FontMatrix(1), tt.want)
				break
			}
		}
	}
	data, err := os.ReadFile("testdata/type2_ops.cff")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFont(data)
	if err != nil {
		t.Fatal(err)
	}
	if gid, ok := f.GlyphForName("Aacute"); !ok || gid != 4 {
		t.Errorf("GlyphForName(Aacute) = %d, %v", gid, ok)
	}
	if _, ok := f.GlyphForCID(4); ok {
		t.Error("GlyphForCID on a name-keyed font: ok")
	}
}

func TestCIDMatrixComposition(t *testing.T) {
	t.Parallel()
	top := geom.Matrix{2, 0, 0, 2, 0, 0}
	fd := geom.Matrix{0.001, 0, 0, 0.001, 0, 0}
	if got := cidFontMatrix(top, true, fd, true); got != (geom.Matrix{0.002, 0, 0, 0.002, 0, 0}) {
		t.Errorf("both: %v", got)
	}
	if got := cidFontMatrix(top, false, fd, true); got != fd {
		t.Errorf("Font DICT only: %v", got)
	}
	if got := cidFontMatrix(top, true, fd, false); got != top {
		t.Errorf("Top DICT only: %v", got)
	}
	if got := cidFontMatrix(top, false, fd, false); got != defaultFontMatrix {
		t.Errorf("neither: %v", got)
	}
}

// charstringFont is a name-keyed font of the given charstrings and local
// subroutines, with no width defaults.
func charstringFont(subrs [][]byte, charStrings ...[]byte) *Font {
	return &Font{
		charStrings: charStrings,
		privates:    []outlinePrivate{{subrs: subrs, matrix: defaultFontMatrix}},
		byName:      map[string]int{},
	}
}

// num encodes small integers as one-byte Type 2 operands.
func num(vs ...int) []byte {
	out := make([]byte, 0, len(vs))
	for _, v := range vs {
		out = append(out, byte(v+139)) //nolint:gosec // G115: test values lie in -107..107.
	}
	return out
}

// cs joins charstring pieces.
func cs(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// op is a one-byte operator; esc a two-byte one.
func op(b byte) []byte  { return []byte{b} }
func esc(b byte) []byte { return []byte{csEscape, b} }

func TestArithmeticOperators(t *testing.T) {
	t.Parallel()
	// Each glyph computes the operands of one rlineto from the origin; the
	// line's end is what the operators left on the stack.
	tests := []struct {
		name   string
		cs     []byte
		wx, wy float64
	}{
		{"add sub", cs(num(3, 4), esc(csAdd), num(10, 4), esc(csSub)), 7, 6},
		{"mul div", cs(num(6, 7), esc(csMul), num(9, 2), esc(csDiv)), 42, 4.5},
		{"neg abs", cs(num(5), esc(csNeg), num(-8), esc(csAbs)), -5, 8},
		{"sqrt", cs(num(9), esc(csSqrt), num(16), esc(csSqrt)), 3, 4},
		{"and or not eq", cs(num(1, 0), esc(csAnd), num(0, 3), esc(csOr), esc(csAdd), num(0), esc(csNot), num(2, 2), esc(csEq), esc(csAdd)), 1, 2},
		{"dup exch drop", cs(num(1, 2), esc(csExch), num(9), esc(csDrop), esc(csDup), esc(csAdd)), 2, 2},
		{"put get", cs(num(11, 3), esc(csPut), num(22, 31), esc(csPut), num(3), esc(csGet), num(31), esc(csGet)), 11, 22},
		{"ifelse", cs(num(1, 2, 5, 5), esc(csIfelse), num(1, 2, 6, 5), esc(csIfelse)), 1, 2},
		// 5 7 1 index leaves 5 7 5, and 4 3 -1 index copies the top.
		{"index", cs(num(5, 7, 1), esc(csIndex), esc(csAdd)), 5, 12},
		{"negative index", cs(num(4, 3, -1), esc(csIndex), esc(csAdd)), 4, 6},
		// 1 2 3 3 1 roll leaves 3 1 2.
		{"roll", cs(num(1, 2, 3, 3, 1), esc(csRoll), esc(csDrop)), 3, 1},
		{"roll back", cs(num(1, 2, 3, 3, -1), esc(csRoll), esc(csDrop)), 2, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := charstringFont(nil, cs(num(0, 0), op(csRmoveto), tt.cs, op(csRlineto), op(csEndchar)))
			g, err := f.Glyph(t.Context(), 0)
			if err != nil {
				t.Fatal(err)
			}
			want := [][]float64{
				{float64(geom.MoveTo), 0, 0},
				{float64(geom.LineTo), tt.wx, tt.wy},
				{float64(geom.Close)},
			}
			if err := samePath(pathOps(g.Path), want); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRandomIsDeterministic(t *testing.T) {
	t.Parallel()
	f := charstringFont(nil, cs(num(0, 0), op(csRmoveto), esc(csRandom), esc(csRandom), op(csRlineto), op(csEndchar)))
	a, err := f.Glyph(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Glyph(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := samePath(pathOps(a.Path), pathOps(b.Path)); err != nil {
		t.Fatal(err)
	}
	for _, v := range a.Path.Pts[1:] {
		if v.X <= 0 || v.X > 1 || v.Y <= 0 || v.Y > 1 || v.X == v.Y {
			t.Fatalf("random values %v, want two distinct numbers in (0, 1]", v)
		}
	}
}

func TestFixedOperandAndWidth(t *testing.T) {
	t.Parallel()
	// 255 carries a 16.16 operand: 1.5 then an rmoveto with a width.
	fixed := []byte{csFixed, 0, 1, 0x80, 0}
	f := charstringFont(nil, cs(num(40), fixed, num(2), op(csRmoveto), op(csEndchar)))
	f.privates[0].nominalWidthX = 100
	g, err := f.Glyph(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if g.Advance != 140 {
		t.Errorf("Advance = %v, want 140", g.Advance)
	}
	if g.Path.Pts[0] != (geom.Point{X: 1.5, Y: 2}) {
		t.Errorf("moveto %v, want (1.5, 2)", g.Path.Pts[0])
	}
}

func TestCharstringMalformed(t *testing.T) {
	t.Parallel()
	recursive := cs(num(-107), op(csCallsubr), op(csReturn))
	many := make([]byte, 0, type2ArgStack+1)
	for range type2ArgStack + 1 {
		many = append(many, num(1)...)
	}
	// Each subroutine calls the next twice: 2^10 bodies of 62 operators
	// each, past the operator budget.
	fan := make([][]byte, 11)
	fan[10] = cs(leafBody(), leafBody(), op(csReturn))
	for i := 9; i >= 0; i-- {
		fan[i] = cs(num(i+1-107), op(csCallsubr), num(i+1-107), op(csCallsubr), op(csReturn))
	}
	tests := []struct {
		name  string
		subrs [][]byte
		cs    []byte
	}{
		{"truncated short int", nil, []byte{csShortInt, 1}},
		{"truncated fixed", nil, []byte{csFixed, 0, 1}},
		{"truncated escape", nil, []byte{csEscape}},
		{"truncated hintmask", nil, cs(num(1, 2, 3, 4), op(csHstemhm), op(csHintmask))},
		{"stack overflow", nil, many},
		{"underflow", nil, esc(csAdd)},
		{"division by zero", nil, cs(num(1, 0), esc(csDiv))},
		{"sqrt of negative", nil, cs(num(-1), esc(csSqrt))},
		{"transient past 32", nil, cs(num(1, 32), esc(csPut))},
		{"index past the stack", nil, cs(num(1, 5), esc(csIndex))},
		{"roll past the stack", nil, cs(num(1, 3, 1), esc(csRoll))},
		{"missing subroutine", nil, cs(num(0), op(csCallsubr))},
		{"recursion", [][]byte{recursive}, recursive},
		{"operator budget", fan, cs(num(-107), op(csCallsubr), op(csEndchar))},
		{"seac without names", nil, cs(num(0, 0, 65, 66), op(csEndchar))},
		{"rmoveto without operands", nil, op(csRmoveto)},
		{"flex without operands", nil, esc(csFlex)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := charstringFont(tt.subrs, tt.cs)
			if _, err := f.Glyph(t.Context(), 0); !errors.Is(err, ErrInvalidCharstring) {
				t.Fatalf("err = %v, want ErrInvalidCharstring", err)
			}
		})
	}
	f := charstringFont(nil, op(csEndchar))
	if _, err := f.Glyph(t.Context(), 1); !errors.Is(err, ErrInvalidCharstring) {
		t.Errorf("glyph past the font: err = %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.Glyph(cancelled, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: err = %v", err)
	}
}

// leafBody is 30 operands and a reserved operator, which clears the stack.
func leafBody() []byte {
	out := make([]byte, 0, 31)
	for range 30 {
		out = append(out, num(1)...)
	}
	return append(out, 2)
}

func TestSeacInsideSeacFails(t *testing.T) {
	t.Parallel()
	seac := cs(num(0, 0, 65, 65), op(csEndchar))
	f := charstringFont(nil, op(csEndchar), seac)
	f.byName["A"] = 1
	if _, err := f.Glyph(t.Context(), 1); !errors.Is(err, ErrInvalidCharstring) {
		t.Fatalf("err = %v, want ErrInvalidCharstring", err)
	}
}

func TestParseFontRejects(t *testing.T) {
	t.Parallel()
	if _, err := ParseFont([]byte{2, 0, 4, 1}); !errors.Is(err, ErrInvalidCFF) {
		t.Errorf("CFF2 header: err = %v", err)
	}
	if _, err := ParseFont([]byte{1, 0, 4, 1, 0, 0}); err == nil {
		t.Error("no Top DICT: want an error")
	}
}

func BenchmarkGlyph(b *testing.B) {
	data, err := os.ReadFile("testdata/frutiger_subr_width.cff")
	if err != nil {
		b.Fatal(err)
	}
	f, err := ParseFont(data)
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
