package type1

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// csOp is a charstring operator, one byte or the escape and a second byte.
type csOp []byte

// Charstring operators by name (Adobe Type 1 Font Format, Appendix 1).
var (
	tHstem          = csOp{1}
	tVmoveto        = csOp{4}
	tRlineto        = csOp{5}
	tHlineto        = csOp{6}
	tVlineto        = csOp{7}
	tRrcurveto      = csOp{8}
	tClosepath      = csOp{9}
	tCallsubr       = csOp{10}
	tReturn         = csOp{11}
	tHsbw           = csOp{13}
	tEndchar        = csOp{14}
	tRmoveto        = csOp{21}
	tHmoveto        = csOp{22}
	tVhcurveto      = csOp{30}
	tHvcurveto      = csOp{31}
	tDotsection     = csOp{12, 0}
	tSeac           = csOp{12, 6}
	tSbw            = csOp{12, 7}
	tDiv            = csOp{12, 12}
	tCallothersubr  = csOp{12, 16}
	tPop            = csOp{12, 17}
	tSetcurrentpoin = csOp{12, 33}
)

// cs assembles a plaintext charstring from integers and operators.
func cs(items ...any) []byte {
	var out []byte
	for _, it := range items {
		switch v := it.(type) {
		case int:
			out = append(out, csNum(v)...)
		case csOp:
			out = append(out, v...)
		default:
			panic(fmt.Sprintf("cs: %T", it))
		}
	}
	return out
}

// csNum encodes v as a Type 1 charstring number (Adobe Type 1 Font Format,
// 6.2).
func csNum(v int) []byte {
	switch {
	case v >= -107 && v <= 107:
		return []byte{byte(v + 139)}
	case v >= 108 && v <= 1131:
		v -= 108
		return []byte{byte(v/256 + 247), byte(v % 256)}
	case v >= -1131 && v <= -108:
		v = -v - 108
		return []byte{byte(v/256 + 251), byte(v % 256)}
	}
	out := []byte{255, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(out[1:], uint32(int32(v))) //nolint:gosec // G115: test values fit an int32.
	return out
}

type testGlyph struct {
	name string
	cs   []byte
}

// testProgram describes a Type 1 program for the outline tests.
type testProgram struct {
	matrix string
	// lenIV is written to the private dictionary unless it is 4; -1 leaves
	// the charstrings unencrypted.
	lenIV  int
	subrs  [][]byte
	glyphs []testGlyph
	hex    bool
	salt   []byte
}

func (p testProgram) build() []byte {
	var clear bytes.Buffer
	clear.WriteString("%!PS-AdobeFont-1.0: Test 001.000\n12 dict begin\n/FontName /Test def\n")
	matrix := p.matrix
	if matrix == "" {
		matrix = "[0.001 0 0 0.001 0 0]"
	}
	fmt.Fprintf(&clear, "/FontMatrix %s readonly def\n", matrix)
	clear.WriteString("/Encoding StandardEncoding def\ncurrentdict end\ncurrentfile eexec\n")

	salt := p.salt
	if salt == nil {
		salt = []byte{1, 2, 3, 4}
	}
	lenIV := p.lenIV
	enc := func(c []byte) []byte {
		if lenIV < 0 {
			return c
		}
		return crypt(append(make([]byte, lenIV), c...), charStringKey, true)
	}
	var priv bytes.Buffer
	priv.Write(salt)
	priv.WriteString("dup /Private 8 dict dup begin\n/RD{string currentfile exch readstring pop}executeonly def\n")
	priv.WriteString("/ND{noaccess def}executeonly def\n/NP{noaccess put}executeonly def\n")
	if lenIV != defaultLenIV {
		fmt.Fprintf(&priv, "/lenIV %d def\n", lenIV)
	}
	if p.subrs != nil {
		fmt.Fprintf(&priv, "/Subrs %d array\n", len(p.subrs))
		for i, s := range p.subrs {
			e := enc(s)
			fmt.Fprintf(&priv, "dup %d %d RD ", i, len(e))
			priv.Write(e)
			priv.WriteString(" NP\n")
		}
		priv.WriteString("ND\n")
	}
	fmt.Fprintf(&priv, "2 index /CharStrings %d dict dup begin\n", len(p.glyphs))
	for _, g := range p.glyphs {
		e := enc(g.cs)
		fmt.Fprintf(&priv, "/%s %d RD ", g.name, len(e))
		priv.Write(e)
		priv.WriteString(" ND\n")
	}
	priv.WriteString("end\nend\nmark currentfile closefile\n")
	encrypted := crypt(priv.Bytes(), eexecKey, true)
	if p.hex {
		encrypted = hexEncode(encrypted)
	}
	out := append(clear.Bytes(), encrypted...)
	out = append(out, "\n0000000000000000000000000000000000000000000000000000000000000000\ncleartomark\n"...)
	return out
}

// square is the glyph A of most tests: a 500-unit square from the origin.
var square = cs(0, 500, tHsbw, 0, 0, tRmoveto, 500, 0, tRlineto, 0, 500, tRlineto, -500, 0, tRlineto, tClosepath, tEndchar)

func parseTest(t *testing.T, p testProgram) *Font {
	t.Helper()
	f, err := ParseFont(context.Background(), p.build())
	if err != nil {
		t.Fatalf("ParseFont: %v", err)
	}
	return f
}

func outline(t *testing.T, f *Font, name string) Glyph {
	t.Helper()
	g, err := f.GlyphOutline(name)
	if err != nil {
		t.Fatalf("GlyphOutline(%s): %v", name, err)
	}
	return g
}

// pathOf builds the expected path from MoveTo/LineTo/CubicTo/Close steps
// written as an op followed by its points.
func pathOf(steps ...any) geom.Path {
	var p geom.Path
	for i := 0; i < len(steps); {
		switch steps[i].(geom.Op) {
		case geom.MoveTo:
			p.MoveTo(steps[i+1].(geom.Point))
			i += 2
		case geom.LineTo:
			p.LineTo(steps[i+1].(geom.Point))
			i += 2
		case geom.CubicTo:
			p.CubicTo(steps[i+1].(geom.Point), steps[i+2].(geom.Point), steps[i+3].(geom.Point))
			i += 4
		case geom.Close:
			p.Close()
			i++
		}
	}
	return p
}

func pt(x, y float64) geom.Point { return geom.Point{X: x, Y: y} }

func assertPath(t *testing.T, got, want geom.Path) {
	t.Helper()
	if len(got.Ops) != len(want.Ops) || len(got.Pts) != len(want.Pts) {
		t.Fatalf("path:\n got %v %v\nwant %v %v", got.Ops, got.Pts, want.Ops, want.Pts)
	}
	for i := range got.Ops {
		if got.Ops[i] != want.Ops[i] {
			t.Fatalf("op %d = %v, want %v\n got %v\nwant %v", i, got.Ops[i], want.Ops[i], got.Ops, want.Ops)
		}
	}
	for i := range got.Pts {
		if math.Abs(got.Pts[i].X-want.Pts[i].X) > 1e-9 || math.Abs(got.Pts[i].Y-want.Pts[i].Y) > 1e-9 {
			t.Fatalf("point %d = %v, want %v\n got %v\nwant %v", i, got.Pts[i], want.Pts[i], got.Pts, want.Pts)
		}
	}
}

func TestGlyphOutlineSquare(t *testing.T) {
	t.Parallel()
	f := parseTest(t, testProgram{lenIV: 4, glyphs: []testGlyph{{".notdef", cs(0, 250, tHsbw, tEndchar)}, {"A", square}}})
	g := outline(t, f, "A")
	assertPath(t, g.Path, pathOf(geom.MoveTo, pt(0, 0), geom.LineTo, pt(500, 0), geom.LineTo, pt(500, 500), geom.LineTo, pt(0, 500), geom.Close))
	if g.Advance != pt(500, 0) || g.SideBearing != pt(0, 0) {
		t.Errorf("metrics = %v %v, want (500,0) (0,0)", g.Advance, g.SideBearing)
	}
	notdef := outline(t, f, ".notdef")
	if !notdef.Path.Empty() || notdef.Advance != pt(250, 0) {
		t.Errorf(".notdef = %+v, want an empty path of advance 250", notdef)
	}
	if f.FontMatrix() != defaultFontMatrix {
		t.Errorf("FontMatrix = %v", f.FontMatrix())
	}
	if !f.HasGlyph("A") || f.HasGlyph("B") {
		t.Error("HasGlyph does not follow /CharStrings")
	}
}

// TestGlyphOutlineEveryPathOperator draws one glyph through every path
// operator, with the points worked out by hand from Adobe Type 1 Font Format,
// 6.4.
func TestGlyphOutlineEveryPathOperator(t *testing.T) {
	t.Parallel()
	glyph := cs(50, 600, tHsbw,
		100, 10, tHstem, tDotsection,
		10, 20, tRmoveto, // (60,20)
		100, tHlineto, // (160,20)
		50, tVlineto, // (160,70)
		-10, 5, tRlineto, // (150,75)
		1, 2, 3, 4, 5, 6, tRrcurveto, // (151,77) (154,81) (159,87)
		10, 20, 30, 40, tVhcurveto, // (159,97) (179,127) (219,127)
		1, 2, 3, 4, tHvcurveto, // (220,127) (222,130) (222,134)
		tClosepath,
		5, tHmoveto, // (227,134)
		6, tVmoveto, // (227,140), replacing the move before
		1, 1, tRlineto,
		tEndchar)
	f := parseTest(t, testProgram{lenIV: 4, glyphs: []testGlyph{{"g", glyph}}})
	g := outline(t, f, "g")
	assertPath(t, g.Path, pathOf(
		geom.MoveTo, pt(60, 20),
		geom.LineTo, pt(160, 20),
		geom.LineTo, pt(160, 70),
		geom.LineTo, pt(150, 75),
		geom.CubicTo, pt(151, 77), pt(154, 81), pt(159, 87),
		geom.CubicTo, pt(159, 97), pt(179, 127), pt(219, 127),
		geom.CubicTo, pt(220, 127), pt(222, 130), pt(222, 134),
		geom.Close,
		geom.MoveTo, pt(227, 140),
		geom.LineTo, pt(228, 141),
	))
	if g.SideBearing != pt(50, 0) || g.Advance != pt(600, 0) {
		t.Errorf("metrics = %v %v", g.SideBearing, g.Advance)
	}
}

func TestGlyphOutlineSbw(t *testing.T) {
	t.Parallel()
	f := parseTest(t, testProgram{lenIV: 4, glyphs: []testGlyph{{"g", cs(10, 20, 700, 30, tSbw, 5, 5, tRlineto, tEndchar)}}})
	g := outline(t, f, "g")
	assertPath(t, g.Path, pathOf(geom.MoveTo, pt(10, 20), geom.LineTo, pt(15, 25)))
	if g.SideBearing != pt(10, 20) || g.Advance != pt(700, 30) {
		t.Errorf("metrics = %v %v", g.SideBearing, g.Advance)
	}
}

// standardSubrs are the subroutines 0 to 4 Adobe Type 1 Font Format, 8.3 and
// 8.2 give every font using flex and hint replacement.
var standardSubrs = [][]byte{
	cs(3, 0, tCallothersubr, tPop, tPop, tSetcurrentpoin, tReturn),
	cs(0, 1, tCallothersubr, tReturn),
	cs(0, 2, tCallothersubr, tReturn),
	cs(tReturn),
	cs(1, 3, tCallothersubr, tPop, tCallsubr, tReturn),
}

func TestGlyphOutlineSubroutinesDivAndHints(t *testing.T) {
	t.Parallel()
	subrs := append(append([][]byte{}, standardSubrs...),
		cs(100, 0, tRlineto, 6, tCallsubr, tReturn), // 5: nests into 6
		cs(0, 100, tRlineto, tReturn),               // 6
		cs(50, 50, tHstem, tReturn),                 // 7: a hint set
	)
	glyph := cs(0, 1000, tHsbw, 0, 0, tRmoveto, 5, tCallsubr, 7, 4, tCallsubr, 10, 3, tDiv, tHlineto, tClosepath, tEndchar)
	f := parseTest(t, testProgram{lenIV: 4, subrs: subrs, glyphs: []testGlyph{{"g", glyph}}})
	g := outline(t, f, "g")
	assertPath(t, g.Path, pathOf(geom.MoveTo, pt(0, 0), geom.LineTo, pt(100, 0), geom.LineTo, pt(100, 100), geom.LineTo, pt(100+10.0/3, 100), geom.Close))
}

// TestGlyphOutlineFlex draws a flex through the standard subroutines: the
// reference point is dropped and the six points that follow make two curves
// (Adobe Type 1 Font Format, 8.3).
func TestGlyphOutlineFlex(t *testing.T) {
	t.Parallel()
	glyph := cs(0, 1000, tHsbw, 0, 0, tRmoveto, 100, 0, tRlineto,
		1, tCallsubr,
		50, 0, tRmoveto, 2, tCallsubr, // reference (150,0)
		-40, 10, tRmoveto, 2, tCallsubr, // (110,10)
		20, 10, tRmoveto, 2, tCallsubr, // (130,20)
		20, 0, tRmoveto, 2, tCallsubr, // (150,20)
		20, 0, tRmoveto, 2, tCallsubr, // (170,20)
		20, -10, tRmoveto, 2, tCallsubr, // (190,10)
		10, -10, tRmoveto, 2, tCallsubr, // (200,0)
		50, 200, 0, 0, tCallsubr,
		0, 100, tRlineto, tClosepath, tEndchar)
	f := parseTest(t, testProgram{lenIV: 4, subrs: standardSubrs, glyphs: []testGlyph{{"g", glyph}}})
	g := outline(t, f, "g")
	assertPath(t, g.Path, pathOf(
		geom.MoveTo, pt(0, 0),
		geom.LineTo, pt(100, 0),
		geom.CubicTo, pt(110, 10), pt(130, 20), pt(150, 20),
		geom.CubicTo, pt(170, 20), pt(190, 10), pt(200, 0),
		geom.LineTo, pt(200, 100),
		geom.Close,
	))
}

// TestGlyphOutlineSeac composes Aacute from A and acute, the Standard
// encoding's codes 65 and 194. The accent's origin lands at adx - asb past
// the composite's side bearing.
func TestGlyphOutlineSeac(t *testing.T) {
	t.Parallel()
	acute := cs(100, 333, tHsbw, 0, 600, tRmoveto, 50, 50, tRlineto, tClosepath, tEndchar)
	aacute := cs(20, 600, tHsbw, 100, 150, 10, 65, 194, tSeac)
	f := parseTest(t, testProgram{lenIV: 4, glyphs: []testGlyph{{"A", square}, {"acute", acute}, {"Aacute", aacute}}})
	g := outline(t, f, "Aacute")
	assertPath(t, g.Path, pathOf(
		geom.MoveTo, pt(0, 0), geom.LineTo, pt(500, 0), geom.LineTo, pt(500, 500), geom.LineTo, pt(0, 500), geom.Close,
		geom.MoveTo, pt(170, 610), geom.LineTo, pt(220, 660), geom.Close,
	))
	if g.SideBearing != pt(20, 0) || g.Advance != pt(600, 0) {
		t.Errorf("metrics = %v %v, want the composite's", g.SideBearing, g.Advance)
	}
}

func TestParseFontForms(t *testing.T) {
	t.Parallel()
	glyphs := []testGlyph{{"A", square}}
	want := pathOf(geom.MoveTo, pt(0, 0), geom.LineTo, pt(500, 0), geom.LineTo, pt(500, 500), geom.LineTo, pt(0, 500), geom.Close)
	plain := testProgram{lenIV: 4, glyphs: glyphs}.build()
	cut := bytes.Index(plain, eexecMarker) + len(eexecMarker) + 1
	pfb := pfbSegment(pfbASCII, plain[:cut])
	pfb = append(pfb, pfbSegment(pfbBinary, plain[cut:])...)
	pfb = append(pfb, pfbSegmentMarker, 3)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"binary", plain},
		{"ASCII-hex", testProgram{lenIV: 4, glyphs: glyphs, hex: true}.build()},
		{"unencrypted charstrings", testProgram{lenIV: -1, glyphs: glyphs}.build()},
		{"no salt in charstrings", testProgram{lenIV: 0, glyphs: glyphs}.build()},
		{"salt opening with a form feed", testProgram{lenIV: 4, glyphs: glyphs, salt: []byte{0x0C, 0, 0, 0}}.build()},
		{"PFB segments", pfb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := ParseFont(context.Background(), tc.data)
			if err != nil {
				t.Fatalf("ParseFont: %v", err)
			}
			assertPath(t, outline(t, f, "A").Path, want)
		})
	}
}

func pfbSegment(kind byte, payload []byte) []byte {
	out := []byte{pfbSegmentMarker, kind, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(out[2:], uint32(len(payload))) //nolint:gosec // G115: test payloads are small.
	return append(out, payload...)
}

func TestParseFontMatrix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		decl string
		want geom.Matrix
	}{
		{"[0.002 0 0.1 0.002 5 6]", geom.Matrix{0.002, 0, 0.1, 0.002, 5, 6}},
		{"{0.0005 0 0 0.0005 0 0}", geom.Matrix{0.0005, 0, 0, 0.0005, 0, 0}},
		{"[0 0 0 0 0 0]", defaultFontMatrix},
		{"[0.001 0 0]", defaultFontMatrix},
		{"[0.001 0 0 x 0 0]", defaultFontMatrix},
	} {
		f := parseTest(t, testProgram{lenIV: 4, matrix: tc.decl, glyphs: []testGlyph{{"A", square}}})
		if got := f.FontMatrix(); got != tc.want {
			t.Errorf("FontMatrix %s = %v, want %v", tc.decl, got, tc.want)
		}
	}
}

// TestParseFontLaterDefinitionWins pins the PostScript reading of a name the
// /CharStrings dictionary defines twice.
func TestParseFontLaterDefinitionWins(t *testing.T) {
	t.Parallel()
	later := cs(0, 300, tHsbw, 0, 0, tRmoveto, 1, 1, tRlineto, tEndchar)
	f := parseTest(t, testProgram{lenIV: 4, glyphs: []testGlyph{{"A", square}, {"A", later}}})
	if g := outline(t, f, "A"); g.Advance != pt(300, 0) {
		t.Errorf("advance = %v, want the later definition's 300", g.Advance)
	}
}

func TestGlyphOutlineErrors(t *testing.T) {
	t.Parallel()
	selfCall := cs(0, tCallsubr, tReturn)
	// Each subroutine calls the next eight times, so the chain's work grows
	// as 8 to the power of its depth while its nesting stays within bounds.
	fanOut := make([][]byte, 10)
	for i := range 9 {
		var items []any
		for range 8 {
			items = append(items, i+1, tCallsubr)
		}
		fanOut[i] = cs(append(items, tReturn)...)
	}
	fanOut[9] = cs(1, 1, tRlineto, tReturn)
	var overflow []any
	for range maxCharStringStack + 1 {
		overflow = append(overflow, 1)
	}
	for _, tc := range []struct {
		name, want string
		subrs      [][]byte
		glyph      []byte
	}{
		{"missing operand", "operands", nil, cs(0, 500, tHsbw, 1, tRlineto, tEndchar)},
		{"stack overflow", "overflows", nil, cs(overflow...)},
		{"missing subroutine", "not defined", nil, cs(0, 500, tHsbw, 3, tCallsubr, tEndchar)},
		{"subroutines nest too deep", "nest past", [][]byte{selfCall}, cs(0, 500, tHsbw, 0, tCallsubr, tEndchar)},
		{"operation budget", "operations", fanOut, cs(0, 500, tHsbw, 0, tCallsubr, tEndchar)},
		{"division by zero", "division", nil, cs(0, 500, tHsbw, 1, 0, tDiv, tHlineto, tEndchar)},
		{"pop with nothing", "pop", nil, cs(0, 500, tHsbw, tPop, tEndchar)},
		{"flex point outside a flex", "outside a flex", standardSubrs, cs(0, 500, tHsbw, 2, tCallsubr, tEndchar)},
		{"flex short of points", "does not collect", standardSubrs, cs(0, 500, tHsbw, 1, tCallsubr, 50, 0, 0, 0, tCallsubr, tEndchar)},
		{"seac of a missing glyph", "glyph not found", nil, cs(0, 500, tHsbw, 0, 0, 0, 66, 194, tSeac)},
		{"seac of a code no glyph has", "names no", nil, cs(0, 500, tHsbw, 0, 0, 0, 0, 194, tSeac)},
		{"nested seac", "itself a seac", nil, cs(0, 500, tHsbw, 0, 0, 0, 65, 202, tSeac)},
		{"number cut short", "cut short", nil, append(cs(0, 500, tHsbw), 255, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := parseTest(t, testProgram{lenIV: 4, subrs: tc.subrs, glyphs: []testGlyph{
				{"A", square},
				{"g", tc.glyph},
				{"ring", cs(0, 500, tHsbw, 0, 0, 0, 65, 194, tSeac)},
				{"acute", cs(0, 333, tHsbw, tEndchar)},
			}})
			_, err := f.GlyphOutline("g")
			if !errors.Is(err, ErrMalformedCharString) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("GlyphOutline = %v, want ErrMalformedCharString for %q", err, tc.want)
			}
		})
	}
}

func TestGlyphOutlineNotFound(t *testing.T) {
	t.Parallel()
	f := parseTest(t, testProgram{lenIV: 4, glyphs: []testGlyph{{"A", square}}})
	if _, err := f.GlyphOutline("B"); !errors.Is(err, ErrGlyphNotFound) {
		t.Errorf("GlyphOutline(B) = %v, want ErrGlyphNotFound", err)
	}
}

func TestParseFontRejects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"no eexec", []byte("%!PS-AdobeFont-1.0: X\n/FontName /X def\n")},
		{"salt only", []byte("currentfile eexec\n\x01\x02\x03")},
		{"no charstrings", append([]byte("currentfile eexec\n"), crypt([]byte("\x00\x00\x00\x00/Private 1 dict begin end"), eexecKey, true)...)},
	} {
		if _, err := ParseFont(context.Background(), tc.data); !errors.Is(err, ErrNoCharStrings) {
			t.Errorf("%s: ParseFont = %v, want ErrNoCharStrings", tc.name, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseFont(ctx, testProgram{lenIV: 4, glyphs: []testGlyph{{"A", square}}}.build()); !errors.Is(err, context.Canceled) {
		t.Errorf("ParseFont on a cancelled context = %v", err)
	}
}

// TestGlyphOutlineConcurrent draws from one Font on many goroutines, for
// -race to check the immutability [Font] documents.
func TestGlyphOutlineConcurrent(t *testing.T) {
	t.Parallel()
	f := parseTest(t, testProgram{lenIV: 4, subrs: standardSubrs, glyphs: []testGlyph{{"A", square}, {"acute", square}, {"Aacute", cs(0, 500, tHsbw, 0, 10, 10, 65, 194, tSeac)}}})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				for _, n := range []string{"A", "Aacute"} {
					if _, err := f.GlyphOutline(n); err != nil {
						t.Error(err)
						return
					}
				}
			}
		})
	}
	wg.Wait()
}

// BenchmarkGlyphOutline draws a glyph through flex and hint replacement. The
// interpreter is linear in the operators it runs.
func BenchmarkGlyphOutline(b *testing.B) {
	glyph := cs(0, 1000, tHsbw, 0, 0, tRmoveto, 100, 0, tRlineto, 7, 4, tCallsubr,
		1, tCallsubr,
		50, 0, tRmoveto, 2, tCallsubr, -40, 10, tRmoveto, 2, tCallsubr, 20, 10, tRmoveto, 2, tCallsubr,
		20, 0, tRmoveto, 2, tCallsubr, 20, 0, tRmoveto, 2, tCallsubr, 20, -10, tRmoveto, 2, tCallsubr,
		10, -10, tRmoveto, 2, tCallsubr, 50, 200, 0, 0, tCallsubr,
		1, 2, 3, 4, 5, 6, tRrcurveto, 10, 20, 30, 40, tVhcurveto, 0, 100, tRlineto, tClosepath, tEndchar)
	subrs := append(append([][]byte{}, standardSubrs...), cs(tReturn), cs(tReturn), cs(50, 50, tHstem, tReturn))
	f, err := ParseFont(context.Background(), testProgram{lenIV: 4, subrs: subrs, glyphs: []testGlyph{{"g", glyph}}}.build())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := f.GlyphOutline("g"); err != nil {
			b.Fatal(err)
		}
	}
}
