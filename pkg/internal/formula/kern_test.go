package formula

import (
	"encoding/binary"
	"testing"

	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
)

// kernTable returns a MATH table that holds only a MathKernInfo: glyph
// base's top-right corner kerns by baseKern font units at every height
// and glyph script's bottom-left by scriptKern.
func kernTable(t *testing.T, base, script uint16, baseKern, scriptKern int16) *mt.Table {
	t.Helper()
	be := func(b []byte, vs ...int) []byte {
		for _, v := range vs {
			b = binary.BigEndian.AppendUint16(b, uint16(v)) //nolint:gosec // test values in range
		}
		return b
	}
	first, second := base, script
	if first > second {
		first, second = second, first
	}
	// MathKernInfo: coverage offset, count 2, two records of four
	// offsets, the coverage, then a MathKern of no height for each kern.
	kerns := be(nil, 4+16, 2)
	coverage := be(nil, 1, 2, int(first), int(second))
	tables := 4 + 16 + len(coverage)
	rec := func(g uint16) []byte {
		switch g {
		case base:
			return be(nil, tables, 0, 0, 0)
		}
		return be(nil, 0, 0, 0, tables+6)
	}
	kerns = append(kerns, rec(first)...)
	kerns = append(kerns, rec(second)...)
	kerns = append(kerns, coverage...)
	kerns = be(kerns, 0, int(baseKern), 0)
	kerns = be(kerns, 0, int(scriptKern), 0)
	info := be(nil, 0, 0, 0, 8)
	info = append(info, kerns...)
	data := be(nil, 1, 0, 0, 10, 0)
	tab, err := mt.ParseTable(append(data, info...))
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

// TestScriptKerning_MovesScriptsByTheirCorners moves a superscript and a
// subscript toward a base by the sums of their corners' math kerning
// under ScriptKerning, and not without it (OpenType, MATH, MathKernInfo).
func TestScriptKerning_MovesScriptsByTheirCorners(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	l := &layouter{ctx: t.Context(), s: s}
	f := l.faceOf(&st)
	baseGID, _ := f.glyphs.NominalGlyph('𝑓')
	supGID, _ := f.glyphs.NominalGlyph('2')
	f.math = kernTable(t, uint16(baseGID), uint16(supGID), -100, -50) //nolint:gosec // glyph ids of the font
	root := el("math", el("msup", tok("mi", "𝑓"), tok("mn", "2")))
	plain, _, _ := childAt(lay(t, s, root, st), 0, 1)
	st.ScriptKerning = true
	kerned, _, sup := childAt(lay(t, s, root, st), 0, 1)
	if sup.glyph == nil {
		t.Fatal("the superscript keeps no glyph")
	}
	// Each corner's kern scales with its own glyph's size.
	if want := plain - float64(100*10)/f.upem - float64(50*sup.glyph.size)/f.upem; !near(kerned, want) {
		t.Fatalf("superscript at %g kerned, %g plain; want %g", kerned, plain, want)
	}
}
