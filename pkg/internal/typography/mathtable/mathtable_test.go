package mathtable

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/woff"
)

// be appends big-endian uint16 values to b.
func be(b []byte, vs ...int) []byte {
	for _, v := range vs {
		b = binary.BigEndian.AppendUint16(b, uint16(v))
	}
	return b
}

// fixtureDejaVuMath is the math font of the reference image, provisioned
// with the corpus fonts.
const fixtureDejaVuMath = "../../../../test/integration/testdata/fonts/gotenberg/truetype/dejavu/DejaVuMathTeXGyre.ttf"

// buildTable returns a MATH table whose constants hold their own index
// plus 100, whose glyph 5 has an italics correction of 40, glyph 7 a top
// accent attachment of 250 and a top right kern of 10 under height 300
// and 20 from it, glyph 9 is an extended shape, and glyph 3 grows
// vertically through variants 30 and 31 and an assembly of three parts.
func buildTable() []byte {
	consts := be(nil, 70, 50, 1300, 1500)
	for i := range numRecords {
		consts = be(consts, 100+i+int(MathLeading), 0)
	}
	consts = be(consts, 60)

	// MathGlyphInfo: four offsets, then the four subtables.
	italics := be(nil, 8, 1, 40, 0)
	italics = append(italics, be(nil, 1, 1, 5)...) // coverage format 1
	accents := be(nil, 8, 1, 250, 0)
	accents = append(accents, be(nil, 2, 1, 7, 7, 0)...) // coverage format 2
	extended := be(nil, 1, 1, 9)
	// MathKernInfo: coverage offset, count, one record of 4 offsets, the
	// coverage, then a MathKern of one height.
	kernTable := be(nil, 1, 300, 0, 10, 0, 20, 0)
	kerns := be(nil, 12, 1, 18, 0, 0, 0)
	kerns = append(kerns, be(nil, 1, 1, 7)...)
	kerns = append(kerns, kernTable...)
	info := be(nil, 8, 8+len(italics), 8+len(italics)+len(accents), 8+len(italics)+len(accents)+len(extended))
	info = append(info, italics...)
	info = append(info, accents...)
	info = append(info, extended...)
	info = append(info, kerns...)

	// MathVariants: overlap 20, vertical coverage, no horizontal one, one
	// vertical construction.
	cov := be(nil, 1, 1, 3)
	construction := be(nil, 12, 2, 30, 1200, 31, 1800)
	assembly := be(nil, 15, 0, 3, 40, 0, 100, 600, 0, 41, 100, 100, 300, 1, 42, 100, 0, 600, 0)
	construction = append(construction, assembly...)
	variants := be(nil, 20, 12, 0, 1, 0, 12+len(cov))
	variants = append(variants, cov...)
	variants = append(variants, construction...)

	head := be(nil, 1, 0, 10, 10+len(consts), 10+len(consts)+len(info))
	out := append(head, consts...)
	out = append(out, info...)
	return append(out, variants...)
}

// TestParseTable_ReadsEverySubtable reads each value of a built table
// back (OpenType 1.9.1, MATH).
func TestParseTable_ReadsEverySubtable(t *testing.T) {
	tab, err := ParseTable(buildTable())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		c    Constant
		want int
	}{
		{ScriptPercentScaleDown, 70}, {ScriptScriptPercentScaleDown, 50},
		{DelimitedSubFormulaMinHeight, 1300}, {DisplayOperatorMinHeight, 1500},
		{MathLeading, 104}, {AxisHeight, 105}, {RadicalKernAfterDegree, 100 + int(RadicalKernAfterDegree)},
		{RadicalDegreeBottomRaisePercent, 60},
	} {
		if got, ok := tab.Constant(c.c); !ok || got != c.want {
			t.Errorf("Constant(%d) = %d, %v; want %d", c.c, got, ok, c.want)
		}
	}
	if v, ok := tab.ItalicsCorrection(5); !ok || v != 40 {
		t.Errorf("ItalicsCorrection(5) = %d, %v", v, ok)
	}
	if _, ok := tab.ItalicsCorrection(6); ok {
		t.Error("glyph 6 has an italics correction")
	}
	if v, ok := tab.TopAccentAttachment(7); !ok || v != 250 {
		t.Errorf("TopAccentAttachment(7) = %d, %v", v, ok)
	}
	if !tab.IsExtendedShape(9) || tab.IsExtendedShape(8) {
		t.Error("extended shapes misread")
	}
	if k := tab.Kern(7, TopRight, 299); k != 10 {
		t.Errorf("kern under the height = %d, want 10", k)
	}
	if k := tab.Kern(7, TopRight, 300); k != 20 {
		t.Errorf("kern at the height = %d, want 20", k)
	}
	if k := tab.Kern(7, TopLeft, 0); k != 0 {
		t.Errorf("kern of a corner without a table = %d", k)
	}
	if tab.MinConnectorOverlap() != 20 {
		t.Errorf("MinConnectorOverlap = %d", tab.MinConnectorOverlap())
	}
	vs, asm, ok := tab.Construction(3, true)
	if !ok || len(vs) != 2 || vs[1] != (Variant{GID: 31, Advance: 1800}) {
		t.Fatalf("Construction(3) = %v, %v", vs, ok)
	}
	if asm == nil || asm.ItalicsCorrection != 15 || len(asm.Parts) != 3 ||
		asm.Parts[1] != (Part{GID: 41, StartConnector: 100, EndConnector: 100, FullAdvance: 300, Extender: true}) {
		t.Fatalf("assembly = %+v", asm)
	}
	if _, _, ok := tab.Construction(3, false); ok {
		t.Error("glyph 3 grows horizontally")
	}
}

// TestParseTable_TruncatedReadsAsAbsent cuts the table at every length:
// the header alone must read for a table, and a cut subtable reads as
// absent, never past the bytes.
func TestParseTable_TruncatedReadsAsAbsent(t *testing.T) {
	full := buildTable()
	for n := range len(full) {
		tab, err := ParseTable(full[:n])
		if n < 10 {
			if !errors.Is(err, ErrInvalidTable) {
				t.Fatalf("a %d-byte table: err = %v", n, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("a %d-byte table: %v", n, err)
		}
		tab.Constant(AxisHeight)
		tab.ItalicsCorrection(5)
		tab.TopAccentAttachment(7)
		tab.Kern(7, TopRight, 400)
		tab.Construction(3, true)
	}
	bad := be(nil, 2, 0, 0, 0, 0)
	if _, err := ParseTable(bad); !errors.Is(err, ErrInvalidTable) {
		t.Fatalf("major version 2: err = %v", err)
	}
}

// TestParse_DejaVuMathTeXGyre reads the math font of the reference image:
// its constants and every vertical construction.
//
// # Corpus evidence
//
//   - probe, ttf: a reader of the MathConstants table written apart, in
//     Python, gives these constants for DejaVuMathTeXGyre.ttf of
//     gotenberg/gotenberg:9.4.1.
func TestParse_DejaVuMathTeXGyre(t *testing.T) {
	program, err := os.ReadFile(fixtureDejaVuMath)
	if err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	tab, err := Parse(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		c    Constant
		want int
	}{
		{ScriptPercentScaleDown, 80}, {ScriptScriptPercentScaleDown, 65}, {DisplayOperatorMinHeight, 1333},
		{AxisHeight, 275}, {SuperscriptShiftUpCramped, 312}, {FractionRuleThickness, 64}, {RadicalDegreeBottomRaisePercent, 55},
	} {
		if got, ok := tab.Constant(c.c); !ok || got != c.want {
			t.Errorf("Constant(%d) = %d, %v; want %d", c.c, got, ok, c.want)
		}
	}
	// Glyph 3 of no font is guaranteed; a parenthesis is looked up by the
	// layout through cmap. Here every vertical construction must read.
	for gid := range uint16(4000) {
		vs, asm, ok := tab.Construction(gid, true)
		if !ok {
			continue
		}
		for _, v := range vs {
			if v.Advance <= 0 {
				t.Fatalf("glyph %d: variant %+v", gid, v)
			}
		}
		if asm != nil && len(asm.Parts) == 0 {
			t.Fatalf("glyph %d: empty assembly", gid)
		}
	}
	if _, err := Parse([]byte("not a font at all")); !errors.Is(err, ErrInvalidTable) {
		t.Fatalf("garbage: err = %v", err)
	}
}

// wptFont returns the program of a math test font of web-platform-tests
// (mathml/tools in its repository builds each to isolate one constant).
func wptFont(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/wpt/" + name + ".woff")
	if err != nil {
		t.Fatal(err)
	}
	program, err := woff.Decode(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

// TestParse_WebPlatformTestFonts reads the constant each test font sets
// apart, in units of an em of 1000.
func TestParse_WebPlatformTestFonts(t *testing.T) {
	for _, c := range []struct {
		font string
		c    Constant
		want int
	}{
		{"fraction-axisheight7000-rulethickness1000", AxisHeight, 7000},
		{"fraction-axisheight7000-rulethickness1000", FractionRuleThickness, 1000},
		{"fraction-rulethickness10000", FractionRuleThickness, 10000},
		{"scripts-superscriptshiftup7000", SuperscriptShiftUp, 7000},
		{"scripts-subscriptshiftdown6000", SubscriptShiftDown, 6000},
		{"largeop-displayoperatorminheight5000", DisplayOperatorMinHeight, 5000},
		{"radical-verticalgap6000-rulethickness1000", RadicalVerticalGap, 6000},
		{"scriptpercentscaledown80-scriptscriptpercentscaledown40", ScriptPercentScaleDown, 80},
		{"scriptpercentscaledown80-scriptscriptpercentscaledown40", ScriptScriptPercentScaleDown, 40},
		{"limits-upperlimitgapmin7000", UpperLimitGapMin, 7000},
	} {
		tab, err := Parse(wptFont(t, c.font))
		if err != nil {
			t.Fatalf("%s: %v", c.font, err)
		}
		if got, ok := tab.Constant(c.c); !ok || got != c.want {
			t.Errorf("%s: Constant(%d) = %d, %v; want %d", c.font, c.c, got, ok, c.want)
		}
	}
	// stretchy.woff gives its vertical arrow and its parenthesis variants
	// and an assembly; at least one glyph grows each way.
	tab, err := Parse(wptFont(t, "stretchy"))
	if err != nil {
		t.Fatal(err)
	}
	vertical, horizontal := 0, 0
	for gid := range uint16(64) {
		if _, _, ok := tab.Construction(gid, true); ok {
			vertical++
		}
		if _, _, ok := tab.Construction(gid, false); ok {
			horizontal++
		}
	}
	if vertical == 0 || horizontal == 0 {
		t.Fatalf("stretchy.woff: %d vertical and %d horizontal constructions", vertical, horizontal)
	}
}
