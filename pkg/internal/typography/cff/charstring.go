package cff

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// Type 2 charstring limits (Adobe Technical Note #5177, Appendix B). A
// charstring that passes them is malformed.
const (
	// type2ArgStack is the argument stack limit.
	type2ArgStack = 48
	// type2Transient is the size of the transient array put and get index.
	type2Transient = 32
)

// Type 2 path construction operators (Adobe Technical Note #5177, section
// 4.1 and Appendix A).
const (
	csVmoveto    = 4
	csRlineto    = 5
	csHlineto    = 6
	csVlineto    = 7
	csRrcurveto  = 8
	csRmoveto    = 21
	csHmoveto    = 22
	csRcurveline = 24
	csRlinecurve = 25
	csVvcurveto  = 26
	csHhcurveto  = 27
	csVhcurveto  = 30
	csHvcurveto  = 31
)

// Type 2 escape operators besides the flex family: dotsection and the
// arithmetic, storage and conditional operators (Adobe Technical Note #5177,
// sections 4.4 and 4.5, and Appendix A).
const (
	csDotsection = 0
	csAnd        = 3
	csOr         = 4
	csNot        = 5
	csAbs        = 9
	csAdd        = 10
	csSub        = 11
	csDiv        = 12
	csNeg        = 14
	csEq         = 15
	csDrop       = 18
	csPut        = 20
	csGet        = 21
	csIfelse     = 22
	csRandom     = 23
	csMul        = 24
	csSqrt       = 26
	csDup        = 27
	csExch       = 28
	csIndex      = 29
	csRoll       = 30
)

// randomMultiplier and randomModulus drive the random operator, a
// Park-Miller generator. The Type 2 format leaves the sequence to the
// interpreter (section 4.4: a number in (0, 1]); a fixed seed per glyph keeps
// the outline, and every render built from it, deterministic.
const (
	randomMultiplier = 48271
	randomModulus    = 2147483647
	randomSeed       = 1
)

// charstringRun interprets the charstrings one glyph runs: its own and, for a
// seac endchar, its base and accent glyphs, all drawing into path and sharing
// one operator budget.
type charstringRun struct {
	font   *Font
	path   geom.Path
	budget int
	random int64
}

// glyph interprets glyph gid's charstring with its origin at origin. inSeac
// marks a base or accent glyph, which may not use seac itself (Appendix C).
// It returns the charstring's width operand, if it states one.
func (r *charstringRun) glyph(gid int, origin geom.Point, inSeac bool) (float64, bool, error) {
	if r.random == 0 {
		r.random = randomSeed
	}
	private := r.font.privateFor(gid)
	m := &type2Machine{
		run:    r,
		subrs:  private.subrs,
		pt:     origin,
		inSeac: inSeac,
	}
	if r.font.cff2 {
		m.startCFF2(private)
	}
	m.lbias = int(subrBias(len(m.subrs)))
	m.gbias = int(subrBias(len(r.font.globalSubrs)))
	if err := m.exec(r.font.charStrings[gid], 0); err != nil {
		return 0, false, err
	}
	m.closeSubpath()
	return m.width, m.hasWidth, nil
}

// type2Machine is the state of one charstring's interpretation: the argument
// stack, the transient array, the current point, the stem hints counted so
// far and the width operand (Adobe Technical Note #5177, section 3).
type type2Machine struct {
	run          *charstringRun
	subrs        [][]byte
	lbias, gbias int
	stack        []float64
	transient    [type2Transient]float64
	pt           geom.Point
	stems        int
	open         bool
	// widthDone records that the first stack-clearing operator has run, the
	// only one that may carry the width operand (section 3.1).
	widthDone, hasWidth bool
	width               float64
	ended, inSeac       bool
	// regions is k, the region count a CFF2 blend takes, and stackLimit
	// the argument stack limit: Type 2's 48, CFF2's 513.
	regions, stackLimit int
}

// errCharstringTruncated reports a charstring that ends inside an operand
// or an operator's data.
var errCharstringTruncated = fmt.Errorf("%w: cut short", ErrInvalidCharstring)

// exec interprets one charstring or subroutine body. depth counts the
// subroutine calls it is nested in.
func (m *type2Machine) exec(cs []byte, depth int) error {
	if depth > maxSubrNesting {
		return fmt.Errorf("%w: subroutines nested past %d", ErrInvalidCharstring, maxSubrNesting)
	}
	for i := 0; i < len(cs) && !m.ended; {
		if m.run.budget--; m.run.budget < 0 {
			return fmt.Errorf("%w: more than %d operators", ErrInvalidCharstring, maxCharstringOps)
		}
		b := cs[i]
		switch {
		case b == csShortInt:
			if i+2 >= len(cs) {
				return errCharstringTruncated
			}
			if err := m.push(float64(int16(binary.BigEndian.Uint16(cs[i+1:])))); err != nil { //nolint:gosec // G115: a two's-complement int16 operand.
				return err
			}
			i += 3
			continue
		case b >= 32 && b <= 246:
			if err := m.push(float64(b) - 139); err != nil {
				return err
			}
			i++
			continue
		case b >= 247 && b <= 250:
			if i+1 >= len(cs) {
				return errCharstringTruncated
			}
			if err := m.push((float64(b)-247)*256 + float64(cs[i+1]) + 108); err != nil {
				return err
			}
			i += 2
			continue
		case b >= 251 && b <= 254:
			if i+1 >= len(cs) {
				return errCharstringTruncated
			}
			if err := m.push(-(float64(b)-251)*256 - float64(cs[i+1]) - 108); err != nil {
				return err
			}
			i += 2
			continue
		case b == csFixed:
			if i+4 >= len(cs) {
				return errCharstringTruncated
			}
			if err := m.push(float64(int32(binary.BigEndian.Uint32(cs[i+1:]))) / fixed1616Scale); err != nil { //nolint:gosec // G115: a two's-complement 16.16 operand.
				return err
			}
			i += 5
			continue
		}
		i++
		if m.run.font.cff2 {
			if handled, err := m.cff2Operator(b); handled {
				if err != nil {
					return err
				}
				continue
			}
		}
		switch b {
		case csEscape:
			if i >= len(cs) {
				return errCharstringTruncated
			}
			op := cs[i]
			i++
			if err := m.escape(op); err != nil {
				return err
			}
		case csHintmask, csCntrmask:
			// Operands before a hintmask are vstem hints whose vstemhm the
			// charstring leaves out (section 4.3), and the mask holds one bit
			// per stem hint, rounded up to whole bytes.
			m.stemHints()
			n := (m.stems + 7) / 8
			if n > len(cs)-i {
				return errCharstringTruncated
			}
			i += n
		case csCallsubr, csCallgsubr:
			subrs, bias := m.subrs, m.lbias
			if b == csCallgsubr {
				subrs, bias = m.run.font.globalSubrs, m.gbias
			}
			v, err := m.pop()
			if err != nil {
				return err
			}
			idx := int(v) + bias
			if v != math.Trunc(v) || idx < 0 || idx >= len(subrs) || subrs[idx] == nil {
				return fmt.Errorf("%w: subroutine %v of %d", ErrInvalidCharstring, v, len(subrs))
			}
			if err := m.exec(subrs[idx], depth+1); err != nil {
				return err
			}
		case csReturn:
			return nil
		case csEndchar:
			return m.endchar()
		default:
			if err := m.pathOp(b); err != nil {
				return err
			}
		}
	}
	return nil
}

// push adds an operand to the argument stack.
func (m *type2Machine) push(v float64) error {
	limit := type2ArgStack
	if m.stackLimit > 0 {
		limit = m.stackLimit
	}
	if len(m.stack) >= limit {
		return fmt.Errorf("%w: more than %d operands", ErrInvalidCharstring, limit)
	}
	m.stack = append(m.stack, v)
	return nil
}

// pop removes the top operand.
func (m *type2Machine) pop() (float64, error) {
	n := len(m.stack)
	if n == 0 {
		return 0, fmt.Errorf("%w: operand stack underflow", ErrInvalidCharstring)
	}
	v := m.stack[n-1]
	m.stack = m.stack[:n-1]
	return v, nil
}

// takeWidth removes the width operand from the bottom of the stack when the
// first stack-clearing operator finds one more operand than it takes (section
// 3.1). extra reports that surplus.
func (m *type2Machine) takeWidth(extra bool) {
	if m.widthDone {
		return
	}
	m.widthDone = true
	if extra && len(m.stack) > 0 {
		m.width, m.hasWidth = m.stack[0], true
		m.stack = m.stack[1:]
	}
}

// stemHints counts the stem hints on the stack, which come in pairs, and
// clears it. An odd count carries the width.
func (m *type2Machine) stemHints() {
	m.takeWidth(len(m.stack)%2 != 0)
	m.stems += len(m.stack) / 2
	m.stack = m.stack[:0]
}

// closeSubpath ends the open subpath. Every Type 2 subpath is closed: a
// moveto or endchar closes the one before it (section 4.1).
func (m *type2Machine) closeSubpath() {
	if m.open {
		m.run.path.Close()
		m.open = false
	}
}

// moveTo starts a subpath at the current point moved by (dx, dy).
func (m *type2Machine) moveTo(dx, dy float64) {
	m.closeSubpath()
	m.pt.X += dx
	m.pt.Y += dy
	m.run.path.MoveTo(m.pt)
	m.open = true
}

// ensureOpen starts a subpath at the current point for a drawing operator no
// moveto preceded. Section 4.1 requires a moveto first; a charstring that
// omits it draws from the origin, as FreeType reads it.
func (m *type2Machine) ensureOpen() {
	if !m.open {
		m.run.path.MoveTo(m.pt)
		m.open = true
	}
}

// lineTo draws a line to the current point moved by (dx, dy).
func (m *type2Machine) lineTo(dx, dy float64) {
	m.ensureOpen()
	m.pt.X += dx
	m.pt.Y += dy
	m.run.path.LineTo(m.pt)
}

// curveTo draws a cubic Bézier whose control points and end point each move
// from the one before by a relative pair, the first from the current point.
func (m *type2Machine) curveTo(dxa, dya, dxb, dyb, dxc, dyc float64) {
	m.ensureOpen()
	c1 := geom.Point{X: m.pt.X + dxa, Y: m.pt.Y + dya}
	c2 := geom.Point{X: c1.X + dxb, Y: c1.Y + dyb}
	m.pt = geom.Point{X: c2.X + dxc, Y: c2.Y + dyc}
	m.run.path.CubicTo(c1, c2, m.pt)
}

// pathOp runs a one-byte operator other than a hintmask, a subroutine call,
// return or endchar: the stem hints and the path construction operators
// (section 4.1 and 4.3). A reserved operator clears the stack and the
// interpretation goes on, as FreeType reads one: Appendix A reserves the code
// and says nothing of a charstring that uses it, and stopping there would drop
// the rest of the glyph.
//
// # Corpus evidence
//
//   - pdfjs: issue15784.pdf embeds two CFF programs whose charstrings close
//     each subpath with byte 9, the Type 1 closepath, which Type 2 reserves.
//     Read through it, a colon draws both its dots; fontTools stops at the
//     byte and draws one. These 40 glyphs are the only outline differences
//     with fontTools over the 6834 programs of all corpora it reads.
func (m *type2Machine) pathOp(op byte) error {
	a := m.stack
	switch op {
	case csHstem, csVstem, csHstemhm, csVstemhm:
		m.stemHints()
		return nil
	case csRmoveto:
		m.takeWidth(len(a) > 2)
		a = m.stack
		if len(a) < 2 {
			return fmt.Errorf("%w: rmoveto takes 2 operands", ErrInvalidCharstring)
		}
		m.moveTo(a[0], a[1])
	case csHmoveto, csVmoveto:
		m.takeWidth(len(a) > 1)
		a = m.stack
		if len(a) < 1 {
			return fmt.Errorf("%w: hmoveto and vmoveto take 1 operand", ErrInvalidCharstring)
		}
		if op == csHmoveto {
			m.moveTo(a[0], 0)
		} else {
			m.moveTo(0, a[0])
		}
	case csRlineto:
		for ; len(a) >= 2; a = a[2:] {
			m.lineTo(a[0], a[1])
		}
	case csHlineto, csVlineto:
		horizontal := op == csHlineto
		for ; len(a) >= 1; a = a[1:] {
			if horizontal {
				m.lineTo(a[0], 0)
			} else {
				m.lineTo(0, a[0])
			}
			horizontal = !horizontal
		}
	case csRrcurveto:
		for ; len(a) >= 6; a = a[6:] {
			m.curveTo(a[0], a[1], a[2], a[3], a[4], a[5])
		}
	case csRcurveline:
		for ; len(a) >= 8; a = a[6:] {
			m.curveTo(a[0], a[1], a[2], a[3], a[4], a[5])
		}
		if len(a) >= 2 {
			m.lineTo(a[0], a[1])
		}
	case csRlinecurve:
		for ; len(a) >= 8; a = a[2:] {
			m.lineTo(a[0], a[1])
		}
		if len(a) >= 6 {
			m.curveTo(a[0], a[1], a[2], a[3], a[4], a[5])
		}
	case csHhcurveto:
		// dy1? {dxa dxb dyb dxc}+: the first curve may start off the
		// horizontal by dy1.
		dy1 := 0.0
		if len(a)%4 != 0 {
			dy1, a = a[0], a[1:]
		}
		for ; len(a) >= 4; a = a[4:] {
			m.curveTo(a[0], dy1, a[1], a[2], a[3], 0)
			dy1 = 0
		}
	case csVvcurveto:
		// dx1? {dya dxb dyb dyc}+, the vertical counterpart.
		dx1 := 0.0
		if len(a)%4 != 0 {
			dx1, a = a[0], a[1:]
		}
		for ; len(a) >= 4; a = a[4:] {
			m.curveTo(dx1, a[0], a[1], a[2], 0, a[3])
			dx1 = 0
		}
	case csHvcurveto, csVhcurveto:
		m.alternatingCurves(a, op == csHvcurveto)
	default:
		// Reserved operators (Appendix A) and the CFF2 vsindex and blend.
	}
	m.stack = m.stack[:0]
	return nil
}

// alternatingCurves draws hvcurveto and vhcurveto: curves whose tangents
// alternate between horizontal and vertical, four operands each, the last
// curve taking a fifth operand for its end point's free coordinate (section
// 4.1).
func (m *type2Machine) alternatingCurves(a []float64, horizontal bool) {
	for len(a) >= 4 {
		last := 0.0
		if len(a) == 5 {
			last = a[4]
		}
		if horizontal {
			m.curveTo(a[0], 0, a[1], a[2], last, a[3])
		} else {
			m.curveTo(0, a[0], a[1], a[2], a[3], last)
		}
		a = a[4:]
		horizontal = !horizontal
	}
}

// endchar ends the glyph: closes the open subpath and, with four operands
// beyond the width, draws the base and accent glyphs of the deprecated seac
// form (Appendix C): the accent with its origin at (adx, ady) from the base's.
func (m *type2Machine) endchar() error {
	m.takeWidth(len(m.stack) == 1 || len(m.stack) == 5)
	m.closeSubpath()
	m.ended = true
	if len(m.stack) < 4 {
		return nil
	}
	if m.inSeac {
		return fmt.Errorf("%w: a seac base or accent glyph using seac", ErrInvalidCharstring)
	}
	n := len(m.stack)
	adx, ady := m.stack[n-4], m.stack[n-3]
	base, err := m.run.font.seacGlyph(m.stack[n-2])
	if err != nil {
		return err
	}
	accent, err := m.run.font.seacGlyph(m.stack[n-1])
	if err != nil {
		return err
	}
	if _, _, err := m.run.glyph(base, geom.Point{}, true); err != nil {
		return fmt.Errorf("drawing seac base glyph %d: %w", base, err)
	}
	if _, _, err := m.run.glyph(accent, geom.Point{X: adx, Y: ady}, true); err != nil {
		return fmt.Errorf("drawing seac accent glyph %d: %w", accent, err)
	}
	return nil
}

// escape runs a two-byte operator: the flex family, which draws, and the
// arithmetic, storage and conditional operators, which leave their result on
// the stack (sections 4.1, 4.4 and 4.5). A reserved one clears the stack.
func (m *type2Machine) escape(op byte) error {
	if m.run.font.cff2 && !isFlex(op) {
		// CFF2 keeps only the flex family of the escape operators.
		m.stack = m.stack[:0]
		return nil
	}
	switch op {
	case csHflex, csFlex, csHflex1, csFlex1:
		if err := m.flex(op); err != nil {
			return err
		}
		m.stack = m.stack[:0]
		return nil
	case csAnd, csOr, csAdd, csSub, csDiv, csMul, csEq, csExch:
		return m.binary(op)
	case csNot, csAbs, csNeg, csSqrt, csDrop, csDup:
		return m.unary(op)
	case csPut, csGet, csIfelse, csRandom, csIndex, csRoll:
		return m.storage(op)
	default:
		// dotsection is a deprecated hint (Appendix A) and the rest are
		// reserved.
		m.stack = m.stack[:0]
		return nil
	}
}

// flex draws the two curves of a flex operator; the flex depth operand of
// flex and flex1 is a hinting threshold, and an outline always draws the
// curves (section 4.1).
func (m *type2Machine) flex(op byte) error {
	a := m.stack
	switch op {
	case csHflex:
		if len(a) < 7 {
			return fmt.Errorf("%w: hflex takes 7 operands", ErrInvalidCharstring)
		}
		m.curveTo(a[0], 0, a[1], a[2], a[3], 0)
		m.curveTo(a[4], 0, a[5], -a[2], a[6], 0)
	case csFlex:
		if len(a) < 13 {
			return fmt.Errorf("%w: flex takes 13 operands", ErrInvalidCharstring)
		}
		m.curveTo(a[0], a[1], a[2], a[3], a[4], a[5])
		m.curveTo(a[6], a[7], a[8], a[9], a[10], a[11])
	case csHflex1:
		if len(a) < 9 {
			return fmt.Errorf("%w: hflex1 takes 9 operands", ErrInvalidCharstring)
		}
		// The second curve ends level with the first one's start.
		m.curveTo(a[0], a[1], a[2], a[3], a[4], 0)
		m.curveTo(a[5], 0, a[6], a[7], a[8], -(a[1] + a[3] + a[7]))
	case csFlex1:
		if len(a) < 11 {
			return fmt.Errorf("%w: flex1 takes 11 operands", ErrInvalidCharstring)
		}
		// The last operand moves along the axis the five pairs travel
		// furthest on, and the other coordinate returns to the start.
		dx := a[0] + a[2] + a[4] + a[6] + a[8]
		dy := a[1] + a[3] + a[5] + a[7] + a[9]
		dx6, dy6 := a[10], -dy
		if math.Abs(dx) <= math.Abs(dy) {
			dx6, dy6 = -dx, a[10]
		}
		m.curveTo(a[0], a[1], a[2], a[3], a[4], a[5])
		m.curveTo(a[6], a[7], a[8], a[9], dx6, dy6)
	}
	return nil
}

// pushResult pushes the result of an arithmetic operator, failing on one that
// is not finite.
func (m *type2Machine) pushResult(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("%w: arithmetic result not finite", ErrInvalidCharstring)
	}
	return m.push(v)
}

// boolValue is the Type 2 truth value: 1 for true, 0 for false.
func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// binary runs an operator taking two operands (section 4.4).
func (m *type2Machine) binary(op byte) error {
	b, err := m.pop()
	if err != nil {
		return err
	}
	a, err := m.pop()
	if err != nil {
		return err
	}
	switch op {
	case csAnd:
		return m.pushResult(boolValue(a != 0 && b != 0))
	case csOr:
		return m.pushResult(boolValue(a != 0 || b != 0))
	case csAdd:
		return m.pushResult(a + b)
	case csSub:
		return m.pushResult(a - b)
	case csDiv:
		if b == 0 {
			return fmt.Errorf("%w: division by zero", ErrInvalidCharstring)
		}
		return m.pushResult(a / b)
	case csMul:
		return m.pushResult(a * b)
	case csEq:
		return m.pushResult(boolValue(a == b))
	default: // csExch
		if err := m.push(b); err != nil {
			return err
		}
		return m.push(a)
	}
}

// unary runs an operator taking one operand (sections 4.4 and 4.5).
func (m *type2Machine) unary(op byte) error {
	a, err := m.pop()
	if err != nil {
		return err
	}
	switch op {
	case csNot:
		return m.pushResult(boolValue(a == 0))
	case csAbs:
		return m.pushResult(math.Abs(a))
	case csNeg:
		return m.pushResult(-a)
	case csSqrt:
		if a < 0 {
			return fmt.Errorf("%w: square root of %v", ErrInvalidCharstring, a)
		}
		return m.pushResult(math.Sqrt(a))
	case csDup:
		if err := m.push(a); err != nil {
			return err
		}
		return m.push(a)
	default: // csDrop
		return nil
	}
}

// transientIndex returns the transient array entry an operand selects.
func transientIndex(v float64) (int, error) {
	if v < 0 || v >= type2Transient || v != math.Trunc(v) {
		return 0, fmt.Errorf("%w: transient array index %v", ErrInvalidCharstring, v)
	}
	return int(v), nil
}

// storage runs the storage and stack operators: put, get, ifelse, random,
// index and roll (sections 4.4 and 4.5).
func (m *type2Machine) storage(op byte) error {
	switch op {
	case csPut:
		i, err := m.pop()
		if err != nil {
			return err
		}
		v, err := m.pop()
		if err != nil {
			return err
		}
		idx, err := transientIndex(i)
		if err != nil {
			return err
		}
		m.transient[idx] = v
		return nil
	case csGet:
		i, err := m.pop()
		if err != nil {
			return err
		}
		idx, err := transientIndex(i)
		if err != nil {
			return err
		}
		return m.push(m.transient[idx])
	case csIfelse:
		if len(m.stack) < 4 {
			return fmt.Errorf("%w: ifelse takes 4 operands", ErrInvalidCharstring)
		}
		n := len(m.stack)
		s1, s2, v1, v2 := m.stack[n-4], m.stack[n-3], m.stack[n-2], m.stack[n-1]
		m.stack = m.stack[:n-4]
		if v1 <= v2 {
			return m.push(s1)
		}
		return m.push(s2)
	case csRandom:
		m.run.random = m.run.random * randomMultiplier % randomModulus
		return m.push(float64(m.run.random) / randomModulus)
	case csIndex:
		v, err := m.pop()
		if err != nil {
			return err
		}
		// A negative index copies the top element.
		i := max(int(v), 0)
		if v != math.Trunc(v) || i >= len(m.stack) {
			return fmt.Errorf("%w: index %v past the stack", ErrInvalidCharstring, v)
		}
		return m.push(m.stack[len(m.stack)-1-i])
	default: // csRoll
		j, err := m.pop()
		if err != nil {
			return err
		}
		nv, err := m.pop()
		if err != nil {
			return err
		}
		if nv != math.Trunc(nv) || j != math.Trunc(j) || nv < 1 || int(nv) > len(m.stack) {
			return fmt.Errorf("%w: roll of %v elements", ErrInvalidCharstring, nv)
		}
		rollTop(m.stack[len(m.stack)-int(nv):], int(math.Mod(j, nv)))
		return nil
	}
}

// rollTop rotates s toward its end by j places, the top of the stack being
// its last element: element i moves to (i+j) mod len(s), as roll does with a
// positive J (section 4.5).
func rollTop(s []float64, j int) {
	n := len(s)
	j = ((j % n) + n) % n
	if j == 0 {
		return
	}
	tmp := make([]float64, n)
	for i, v := range s {
		tmp[(i+j)%n] = v
	}
	copy(s, tmp)
}
