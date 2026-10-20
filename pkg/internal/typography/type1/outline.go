package type1

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// Font is a Type 1 program parsed for its glyph outlines. It keeps the
// decrypted private section and the offsets of every charstring and
// subroutine in it, and decrypts a charstring only when [Font.GlyphOutline]
// draws it.
//
// A Font is immutable once [ParseFont] returns it and is safe for concurrent
// use by multiple goroutines.
type Font struct {
	matrix geom.Matrix
	// plain is the decrypted eexec section, salt removed.
	plain  []byte
	glyphs map[string]span
	subrs  map[int]span
	// lenIV is the count of salt bytes before each charstring, or -1 when
	// the charstrings are not encrypted (Adobe Type 1 Font Format, 5.3).
	lenIV int
}

// span bounds a charstring in [Font.plain], still encrypted.
type span struct {
	start, end int
}

// Glyph is the outline of one glyph in glyph space, the font's design units,
// which [Font.FontMatrix] maps to text space (ISO 32000-1, 9.2.4).
type Glyph struct {
	// Path is the outline the charstring draws, to be filled with the
	// nonzero winding rule (Adobe Type 1 Font Format, 3.4). Every subpath
	// starts with a MoveTo; one the charstring leaves open is not closed.
	Path geom.Path
	// SideBearing is the left side bearing point the hsbw or sbw operator
	// sets, where the charstring's first rmoveto starts from.
	SideBearing geom.Point
	// Advance is the advance width vector the hsbw or sbw operator sets.
	Advance geom.Point
}

// defaultFontMatrix maps the 1000-unit glyph space most Type 1 programs use
// to text space (Adobe Type 1 Font Format, 5.1).
var defaultFontMatrix = geom.Matrix{0.001, 0, 0, 0.001, 0, 0}

// fontMatrixArrayRe captures the six elements of a /FontMatrix declaration,
// written as an array or, by some producers, as a procedure.
var fontMatrixArrayRe = regexp.MustCompile(`/FontMatrix\s*[\[{]([^\]}]*)[\]}]`)

// Type 1 charstring operators the outline interpreter adds to those the width
// scan and the subset read (Adobe Type 1 Font Format, 6.4 to 6.6, and Table
// 6.1 of its Appendix 1).
const (
	csOpEscapeDotSection      byte = 0
	csOpEscapeVstem3          byte = 1
	csOpEscapeHstem3          byte = 2
	csOpEscapeSetCurrentPoint byte = 33
)

// Standard OtherSubrs entries the interpreter knows (Adobe Type 1 Font
// Format, 8.1 to 8.3): flex is ended by entry 0, started by entry 1, and fed
// one point by entry 2, and entry 3 replaces hints.
const (
	otherSubrFlexEnd   = 0
	otherSubrFlexStart = 1
	otherSubrFlexPoint = 2
)

// flexVectors is how many points a flex collects between its start and its
// end: the reference point, then the control points and end points of the
// two curves it stands for (Adobe Type 1 Font Format, 8.3).
const flexVectors = 7

// maxSubrNesting is the depth of nested callsubr a charstring may reach.
// Adobe Type 1 Font Format, 6.5, sets the limit on subroutine nesting to 10.
const maxSubrNesting = 10

// maxGlyphOperations caps the operators interpreted for one glyph, its seac
// components and every subroutine they call included. Anti-DoS guard, not
// spec-mandated: subroutine nesting is bounded, but a subroutine may call
// others many times, so the work of one glyph grows as a power of the
// nesting. Real glyphs run a few hundred operators, flex and hint
// replacement included, so 65,536 leaves two orders of magnitude.
const maxGlyphOperations = 1 << 16

// ParseFont parses a Type 1 program, the bytes of a /FontFile stream (ISO
// 32000-1, 9.9), for glyph outlines. The program may be in the binary or the
// ASCII-hex eexec form (Adobe Type 1 Font Format, 7.2), or in PFB segments,
// which some producers embed. It fails with [ErrNoCharStrings] when the
// program holds no /CharStrings dictionary it can read. A /Subrs array that
// stops reading part way keeps the subroutines read before: a glyph calling
// one past them fails when drawn.
//
// # Corpus evidence
//
//   - govdocs thread_100_100478.pdf: a program defines /bar twice, a 26-byte
//     and a 45-byte charstring; fontTools draws the later, as ParseFont does.
func ParseFont(ctx context.Context, data []byte) (*Font, error) {
	data = joinPFBSegments(data)
	clearText, _, _ := bytes.Cut(data, eexecMarker)
	plain := decryptEexec(eexecRegion(data))
	if plain == nil {
		return nil, fmt.Errorf("%w: no eexec section", ErrNoCharStrings)
	}
	entries, ok := charStringEntries(plain)
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("%w: no /CharStrings entry", ErrNoCharStrings)
	}
	f := &Font{
		matrix: parseFontMatrix(clearText, plain),
		plain:  plain,
		glyphs: make(map[string]span, len(entries)),
		subrs:  make(map[int]span),
		lenIV:  outlineLenIV(plain),
	}
	for i, e := range entries {
		if i%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("parsing a Type 1 program: %w", err)
			}
		}
		// A later definition of a name replaces an earlier one, as def does
		// in the PostScript dictionary /CharStrings is (PostScript Language
		// Reference, 3.7.1). The width scan keeps the first, the definition
		// PDF.js reads, for text extraction to agree with it.
		f.glyphs[e.name] = span{e.bodyStart, e.bodyEnd}
	}
	if err := f.readSubrs(ctx); err != nil {
		return nil, err
	}
	return f, nil
}

// readSubrs indexes the /Subrs array: "/Subrs COUNT array", then one "dup
// INDEX LEN RD <LEN bytes> NP" per subroutine (Adobe Type 1 Font Format,
// 5.2). Every entry consumes bytes of the private section, so the map grows
// with the program, never with the COUNT it declares.
func (f *Font) readSubrs(ctx context.Context) error {
	loc := subrsRe.FindSubmatchIndex(f.plain)
	if loc == nil {
		return nil
	}
	count, err := strconv.Atoi(string(f.plain[loc[2]:loc[3]]))
	if err != nil {
		// The pattern admits only digits, so the count overflows an int:
		// read entries until the definition closes.
		count = math.MaxInt
	}
	pos := loc[1]
	for read := 0; read < count; read++ {
		if read%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("parsing a Type 1 program: %w", err)
			}
		}
		index, start, end, status := nextSubr(f.plain, pos)
		if status != subrEntry {
			return nil
		}
		pos = end
		if index >= count {
			continue
		}
		// A later put of an index replaces an earlier one, as in PostScript.
		f.subrs[index] = span{start, end}
	}
	return nil
}

// FontMatrix returns the transform from glyph space to text space the
// program's /FontMatrix declares (ISO 32000-1, 9.2.4), or [0.001 0 0 0.001 0
// 0] when it declares none it can read or one that is singular.
func (f *Font) FontMatrix() geom.Matrix {
	return f.matrix
}

// HasGlyph reports whether the program's /CharStrings dictionary defines
// name.
func (f *Font) HasGlyph(name string) bool {
	_, ok := f.glyphs[name]
	return ok
}

// GlyphOutline interprets the charstring of the glyph name and returns its
// outline in glyph space. It runs every Type 1 charstring command (Adobe Type
// 1 Font Format, 6.4 to 6.6), subroutines, flex and hint replacement through
// the standard OtherSubrs (8.1 to 8.3), and seac, which draws a base and an
// accent glyph the Standard encoding names (Appendix 3). Hints are read and
// ignored. The work is bounded by [maxGlyphOperations], so the call takes no
// context.
//
// It fails with [ErrGlyphNotFound] when no charstring defines name and with
// [ErrMalformedCharString] when the charstring breaks a rule the interpreter
// needs: an operand missing or overflowing the 24-entry stack (6.1), a
// subroutine missing or nested past 10 (6.5), a flex that does not collect
// its seven points (8.3), a seac whose component is missing or itself a seac,
// a division by zero, or a charstring past the operation budget.
//
// # Corpus evidence
//
//   - All corpora: 522 distinct /FontFile programs. fontTools' t1Lib reads
//     515, and 32,137 of their 32,258 glyphs draw the same points here. Of
//     the other 7, 4 are bare CFF or CIDFontType 0 data that ParseFont
//     rejects too, and pdfjs issue7769.pdf (/lenIV -1) and
//     issue14462_reduced.pdf hold the 3 fontTools cannot read.
//   - govdocs thread_100_100478.pdf and five other programs: 6 glyphs named
//     by the empty name "/", which the /CharStrings walk skips.
//   - govdocs thread_300_300035.pdf, thread_300_300083.pdf,
//     thread_300_300084.pdf and thread_400_400574.pdf: 105 glyphs open with
//     sbw, which fontTools' outline reader leaves unimplemented, so its
//     outlines lose the side bearing point and its advances read 0.
//   - pdfjs issue10004.pdf and nine other programs: a .notdef opening with
//     the undefined operator 0, then "1000 hsbw" one operand short, fails
//     with ErrMalformedCharString where fontTools draws nothing.
func (f *Font) GlyphOutline(name string) (Glyph, error) {
	cs, err := f.charString(name)
	if err != nil {
		return Glyph{}, err
	}
	in := &interpreter{font: f}
	if err := in.run(cs, 0); err != nil {
		return Glyph{}, fmt.Errorf("drawing glyph /%s: %w", name, err)
	}
	in.dropDanglingMove()
	return Glyph{Path: in.path, SideBearing: in.sideBearing, Advance: in.advance}, nil
}

// charString returns the decrypted charstring of the glyph name.
func (f *Font) charString(name string) ([]byte, error) {
	s, ok := f.glyphs[name]
	if !ok {
		return nil, fmt.Errorf("%w: /%s", ErrGlyphNotFound, name)
	}
	return f.decrypt(s), nil
}

// decrypt returns the plaintext of the charstring at s.
func (f *Font) decrypt(s span) []byte {
	enc := f.plain[s.start:s.end]
	if f.lenIV < 0 {
		return enc
	}
	if len(enc) <= f.lenIV {
		return nil
	}
	return crypt(enc, charStringKey, false)[f.lenIV:]
}

// interpreter runs Type 1 charstrings for one glyph (Adobe Type 1 Font
// Format, 6). Coordinates are relative to the glyph's origin; offset moves
// the points it emits, which places a seac accent.
type interpreter struct {
	font  *Font
	path  geom.Path
	stack [maxCharStringStack]float64
	top   int
	// ps is the PostScript operand stack callothersubr fills and pop
	// drains, its top at the end.
	ps []float64
	// x and y are the current point.
	x, y float64
	// started reports that the current subpath has its MoveTo.
	started     bool
	flex        bool
	flexPts     []geom.Point
	offset      geom.Point
	component   bool
	sideBearing geom.Point
	advance     geom.Point
	ops         int
}

// errEndChar ends the interpretation of a glyph at endchar or seac, out of
// any subroutine it is in.
var errEndChar = errors.New("type1: endchar")

// run interprets cs, a charstring or a subroutine at nesting depth. It
// returns nil at return or at the end of the bytes, and stops the whole glyph
// at endchar or seac.
func (in *interpreter) run(cs []byte, depth int) error {
	err := in.exec(cs, depth)
	if depth == 0 && errors.Is(err, errEndChar) {
		return nil
	}
	return err
}

func (in *interpreter) exec(cs []byte, depth int) error {
	for i := 0; i < len(cs); {
		in.ops++
		if in.ops > maxGlyphOperations {
			return fmt.Errorf("%w: more than %d operations", ErrMalformedCharString, maxGlyphOperations)
		}
		b := cs[i]
		if b > csLastOperator {
			v, n := charStringNumber(cs[i:])
			if n == 0 {
				return fmt.Errorf("%w: a number cut short", ErrMalformedCharString)
			}
			if err := in.push(v); err != nil {
				return err
			}
			i += n
			continue
		}
		i++
		if b == csOpEscape {
			if i >= len(cs) {
				return fmt.Errorf("%w: an escape cut short", ErrMalformedCharString)
			}
			op := cs[i]
			i++
			if err := in.escaped(op); err != nil {
				return err
			}
			continue
		}
		switch b {
		case csOpReturn:
			return nil
		case csOpCallSubr:
			if err := in.callSubr(depth); err != nil {
				return err
			}
		default:
			if err := in.command(b); err != nil {
				return err
			}
		}
	}
	return nil
}

// args returns the n operands on top of the stack, bottom first.
func (in *interpreter) args(n int) ([]float64, error) {
	if in.top < n {
		return nil, fmt.Errorf("%w: an operator takes %d operands, the stack holds %d", ErrMalformedCharString, n, in.top)
	}
	return in.stack[in.top-n : in.top], nil
}

func (in *interpreter) push(v float64) error {
	if in.top == maxCharStringStack {
		return fmt.Errorf("%w: the operand stack overflows %d entries", ErrMalformedCharString, maxCharStringStack)
	}
	in.stack[in.top] = v
	in.top++
	return nil
}

func (in *interpreter) pop() (float64, error) {
	if in.top == 0 {
		return 0, fmt.Errorf("%w: an operand is missing", ErrMalformedCharString)
	}
	in.top--
	return in.stack[in.top], nil
}

// command runs a one-byte operator other than callsubr and return.
func (in *interpreter) command(op byte) error {
	var n int
	switch op {
	case csOpHstem, csOpVstem, csOpRlineTo, csOpRmoveTo, csOpHsbw:
		n = 2
	case csOpHlineTo, csOpVlineTo, csOpHmoveTo, csOpVmoveTo:
		n = 1
	case csOpRrcurveTo:
		n = 6
	case csOpVhcurveTo, csOpHvcurveTo:
		n = 4
	case csOpClosePath, csOpEndChar:
		n = 0
	default:
		// Leniency: Adobe Type 1 Font Format, Appendix 1 leaves the other
		// one-byte codes undefined. PDF.js warns and skips them, keeping the
		// stack, and so does this interpreter, so a glyph an old producer
		// wrote with an undocumented code still draws.
		return nil
	}
	a, err := in.args(n)
	if err != nil {
		return err
	}
	switch op {
	case csOpHsbw:
		in.setMetrics(geom.Point{X: a[0]}, geom.Point{X: a[1]})
	case csOpRmoveTo:
		in.moveTo(in.x+a[0], in.y+a[1])
	case csOpHmoveTo:
		in.moveTo(in.x+a[0], in.y)
	case csOpVmoveTo:
		in.moveTo(in.x, in.y+a[0])
	case csOpRlineTo:
		in.lineTo(in.x+a[0], in.y+a[1])
	case csOpHlineTo:
		in.lineTo(in.x+a[0], in.y)
	case csOpVlineTo:
		in.lineTo(in.x, in.y+a[0])
	case csOpRrcurveTo:
		in.curveTo(a[0], a[1], a[2], a[3], a[4], a[5])
	case csOpVhcurveTo:
		in.curveTo(0, a[0], a[1], a[2], a[3], 0)
	case csOpHvcurveTo:
		in.curveTo(a[0], 0, a[1], a[2], 0, a[3])
	case csOpClosePath:
		// The current point stays where the subpath ended, as FreeType and
		// PDF.js keep it: a charstring closes a subpath at its start point
		// (Adobe Type 1 Font Format, 6.4), so both readings agree on every
		// well-formed glyph.
		if in.started {
			in.path.Close()
			in.started = false
		}
	case csOpEndChar:
		return errEndChar
	}
	in.top = 0
	return nil
}

// escaped runs the two-byte operator 12 op.
func (in *interpreter) escaped(op byte) error {
	switch op {
	case csOpEscapeDotSection:
		in.top = 0
	case csOpEscapeVstem3, csOpEscapeHstem3:
		if _, err := in.args(6); err != nil {
			return err
		}
		in.top = 0
	case csOpEscapeSbw:
		a, err := in.args(4)
		if err != nil {
			return err
		}
		in.setMetrics(geom.Point{X: a[0], Y: a[1]}, geom.Point{X: a[2], Y: a[3]})
		in.top = 0
	case csOpEscapeDiv:
		a, err := in.args(2)
		if err != nil {
			return err
		}
		if a[1] == 0 {
			return fmt.Errorf("%w: a division by zero", ErrMalformedCharString)
		}
		in.stack[in.top-2] = a[0] / a[1]
		in.top--
	case csOpEscapeCallOtherSubr:
		return in.callOtherSubr()
	case csOpEscapePop:
		if len(in.ps) == 0 {
			return fmt.Errorf("%w: pop with no OtherSubrs result", ErrMalformedCharString)
		}
		v := in.ps[len(in.ps)-1]
		in.ps = in.ps[:len(in.ps)-1]
		return in.push(v)
	case csOpEscapeSetCurrentPoint:
		a, err := in.args(2)
		if err != nil {
			return err
		}
		in.x, in.y = a[0], a[1]
		in.top = 0
	case csOpEscapeSeac:
		a, err := in.args(5)
		if err != nil {
			return err
		}
		if err := in.seac(a[0], a[1], a[2], a[3], a[4]); err != nil {
			return err
		}
		return errEndChar
	default:
		// Leniency: as for an undefined one-byte operator (see command).
	}
	return nil
}

// callSubr runs the subroutine whose number is on top of the stack (Adobe
// Type 1 Font Format, 6.5).
func (in *interpreter) callSubr(depth int) error {
	v, err := in.pop()
	if err != nil {
		return err
	}
	if depth >= maxSubrNesting {
		return fmt.Errorf("%w: subroutines nest past %d", ErrMalformedCharString, maxSubrNesting)
	}
	if v != math.Trunc(v) || v < 0 || v > math.MaxInt32 {
		return fmt.Errorf("%w: subroutine number %v", ErrMalformedCharString, v)
	}
	s, ok := in.font.subrs[int(v)]
	if !ok {
		return fmt.Errorf("%w: subroutine %d is not defined", ErrMalformedCharString, int(v))
	}
	return in.exec(in.font.decrypt(s), depth+1)
}

// callOtherSubr runs "arg1 ... argn n othersubr# callothersubr" (Adobe Type 1
// Font Format, 6.5). The flex entries 0 to 2 draw as Section 8.3 describes;
// every other entry, hint replacement (8.2) among them, leaves its arguments
// for pop to return in their order, as the standard entry 3 does with its
// subroutine number.
func (in *interpreter) callOtherSubr() error {
	a, err := in.args(2)
	if err != nil {
		return err
	}
	n, entry := a[0], a[1]
	in.top -= 2
	if n != math.Trunc(n) || n < 0 || int(n) > in.top {
		return fmt.Errorf("%w: callothersubr takes %v arguments, the stack holds %d", ErrMalformedCharString, n, in.top)
	}
	args := in.stack[in.top-int(n) : in.top]
	in.ps = in.ps[:0]
	for k := len(args) - 1; k >= 0; k-- {
		in.ps = append(in.ps, args[k])
	}
	in.top -= int(n)
	switch entry {
	case otherSubrFlexStart:
		in.ensureStarted()
		in.flex = true
		in.flexPts = in.flexPts[:0]
	case otherSubrFlexPoint:
		if !in.flex {
			return fmt.Errorf("%w: a flex point outside a flex", ErrMalformedCharString)
		}
		if len(in.flexPts) == flexVectors {
			return fmt.Errorf("%w: a flex of more than %d points", ErrMalformedCharString, flexVectors)
		}
		in.flexPts = append(in.flexPts, geom.Point{X: in.x, Y: in.y})
	case otherSubrFlexEnd:
		if !in.flex || len(in.flexPts) != flexVectors || len(args) != 3 {
			return fmt.Errorf("%w: a flex that does not collect %d points", ErrMalformedCharString, flexVectors)
		}
		// The first point is the reference point, which only hinted
		// rendering of a shallow flex uses. The two curves follow it.
		p := in.flexPts
		in.emitCurve(p[1], p[2], p[3])
		in.emitCurve(p[4], p[5], p[6])
		in.flex = false
		// The entry leaves the end point for "pop pop setcurrentpoint".
		in.ps = append(in.ps[:0], in.y, in.x)
	}
	return nil
}

// seac draws the accented glyph made of the glyphs the Standard encoding
// names at codes bchar and achar (Adobe Type 1 Font Format, 6.4 and Appendix
// 3). The base is drawn at the origin. The accent is drawn with its origin at
// (adx - asb, ady) plus the composite's left side bearing, which is where
// FreeType and PDF.js place it: the composite's side bearing equals the
// base's, and adx is measured from the base's side bearing point.
func (in *interpreter) seac(asb, adx, ady, bchar, achar float64) error {
	if in.component {
		return fmt.Errorf("%w: a seac component is itself a seac", ErrMalformedCharString)
	}
	base, err := in.component1(bchar)
	if err != nil {
		return err
	}
	accent, err := in.component1(achar)
	if err != nil {
		return err
	}
	if err := in.drawComponent(base, geom.Point{}); err != nil {
		return err
	}
	return in.drawComponent(accent, geom.Point{X: in.sideBearing.X + adx - asb, Y: ady})
}

// component1 returns the charstring of the glyph the Standard encoding names
// at code.
func (in *interpreter) component1(code float64) ([]byte, error) {
	if code != math.Trunc(code) || code < 0 || code > math.MaxUint8 {
		return nil, fmt.Errorf("%w: seac code %v", ErrMalformedCharString, code)
	}
	name := encodings.StandardEncodingNames[int(code)]
	if name == "" {
		return nil, fmt.Errorf("%w: seac code %d names no Standard encoding glyph", ErrMalformedCharString, int(code))
	}
	cs, err := in.font.charString(name)
	if err != nil {
		return nil, fmt.Errorf("%w: seac component: %w", ErrMalformedCharString, err)
	}
	return cs, nil
}

// drawComponent interprets the seac component cs with its origin at offset,
// sharing this interpreter's path and operation budget.
func (in *interpreter) drawComponent(cs []byte, offset geom.Point) error {
	in.dropDanglingMove()
	sub := &interpreter{font: in.font, path: in.path, offset: offset, component: true, ops: in.ops}
	if err := sub.run(cs, 0); err != nil {
		return err
	}
	sub.dropDanglingMove()
	in.path = sub.path
	in.ops = sub.ops
	in.started = false
	return nil
}

// setMetrics records the side bearing and advance of hsbw or sbw and moves
// the current point to the side bearing point (Adobe Type 1 Font Format,
// 6.4). A seac component keeps the composite's metrics.
func (in *interpreter) setMetrics(sb, adv geom.Point) {
	in.x, in.y = sb.X, sb.Y
	if in.component {
		return
	}
	in.sideBearing, in.advance = sb, adv
}

// moveTo starts a subpath at (x, y). Inside a flex the moves only carry the
// current point to the next flex point (Adobe Type 1 Font Format, 8.3).
func (in *interpreter) moveTo(x, y float64) {
	in.x, in.y = x, y
	if in.flex {
		return
	}
	pt := in.point(x, y)
	if n := len(in.path.Ops); in.started && n > 0 && in.path.Ops[n-1] == geom.MoveTo {
		in.path.Pts[len(in.path.Pts)-1] = pt
		return
	}
	in.path.MoveTo(pt)
	in.started = true
}

func (in *interpreter) lineTo(x, y float64) {
	in.ensureStarted()
	in.x, in.y = x, y
	in.path.LineTo(in.point(x, y))
}

// curveTo draws a curve whose control points and end point are given as
// successive offsets from the current point.
func (in *interpreter) curveTo(dx1, dy1, dx2, dy2, dx3, dy3 float64) {
	x1, y1 := in.x+dx1, in.y+dy1
	x2, y2 := x1+dx2, y1+dy2
	x3, y3 := x2+dx3, y2+dy3
	in.emitCurve(geom.Point{X: x1, Y: y1}, geom.Point{X: x2, Y: y2}, geom.Point{X: x3, Y: y3})
}

// emitCurve adds a curve with control points and end point in glyph
// coordinates and leaves the current point at its end.
func (in *interpreter) emitCurve(c1, c2, end geom.Point) {
	in.ensureStarted()
	in.path.CubicTo(in.point(c1.X, c1.Y), in.point(c2.X, c2.Y), in.point(end.X, end.Y))
	in.x, in.y = end.X, end.Y
}

// ensureStarted opens a subpath at the current point when a drawing command
// follows a closepath or the metrics with no rmoveto, which PostScript
// would reject but rasterisers accept.
func (in *interpreter) ensureStarted() {
	if in.started {
		return
	}
	in.path.MoveTo(in.point(in.x, in.y))
	in.started = true
}

// dropDanglingMove removes a MoveTo no segment follows.
func (in *interpreter) dropDanglingMove() {
	if n := len(in.path.Ops); n > 0 && in.path.Ops[n-1] == geom.MoveTo {
		in.path.Ops = in.path.Ops[:n-1]
		in.path.Pts = in.path.Pts[:len(in.path.Pts)-1]
	}
	in.started = false
}

func (in *interpreter) point(x, y float64) geom.Point {
	return geom.Point{X: x + in.offset.X, Y: y + in.offset.Y}
}

// outlineLenIV returns the /lenIV of the private section, -1 when it is
// negative, which leaves the charstrings unencrypted (Adobe Type 1 Font
// Format, 5.3), and [defaultLenIV] when it declares none. [parseLenIV] keeps
// the width scan's reading, which decrypts every charstring.
func outlineLenIV(plain []byte) int {
	m := lenIVRe.FindSubmatch(plain)
	if m == nil {
		return defaultLenIV
	}
	v, err := strconv.Atoi(string(m[1]))
	switch {
	case err != nil:
		return defaultLenIV
	case v < 0:
		return -1
	}
	return v
}

// parseFontMatrix returns the /FontMatrix the clear-text portion declares,
// or the private section when the clear text holds none, and the default
// matrix when neither reads as six finite numbers of an invertible
// transform.
func parseFontMatrix(clearText, plain []byte) geom.Matrix {
	for _, src := range [][]byte{clearText, plain} {
		m := fontMatrixArrayRe.FindSubmatch(src)
		if m == nil {
			continue
		}
		fields := bytes.Fields(m[1])
		if len(fields) != len(geom.Matrix{}) {
			return defaultFontMatrix
		}
		var out geom.Matrix
		for i, f := range fields {
			v, err := strconv.ParseFloat(string(f), 64)
			if err != nil {
				return defaultFontMatrix
			}
			out[i] = v
		}
		if _, ok := out.Invert(); !ok {
			return defaultFontMatrix
		}
		return out
	}
	return defaultFontMatrix
}

// PFB segment types that carry the program: ASCII text and binary data. Any
// other type, the end-of-file segment among them, ends it.
const (
	pfbASCII  = 1
	pfbBinary = 2
)

// pfbHeaderLen is the size of a PFB segment header: the marker, the type,
// and a four-byte little-endian length.
const pfbHeaderLen = 6

// joinPFBSegments returns the program a font in PFB segments holds, the
// payloads of its segments joined, or data unchanged when it is not in PFB
// form. A segment whose length runs past the data ends the program there.
func joinPFBSegments(data []byte) []byte {
	if len(data) == 0 || data[0] != pfbSegmentMarker {
		return data
	}
	var out []byte
	for pos := 0; pos+pfbHeaderLen <= len(data) && data[pos] == pfbSegmentMarker; {
		kind := data[pos+1]
		if kind != pfbASCII && kind != pfbBinary {
			break
		}
		n := binary.LittleEndian.Uint32(data[pos+2 : pos+pfbHeaderLen])
		pos += pfbHeaderLen
		if int64(n) > int64(len(data)-pos) {
			out = append(out, data[pos:]...)
			break
		}
		out = append(out, data[pos:pos+int(n)]...)
		pos += int(n)
	}
	return out
}
