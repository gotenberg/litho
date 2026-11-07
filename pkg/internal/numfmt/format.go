package numfmt

import (
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// Kind is the kind of a [Value].
type Kind uint8

// The value kinds.
const (
	// Number is a number, a date or a time as its serial number.
	Number Kind = iota
	// Text is a string.
	Text
	// Bool is a boolean, Number 0 or 1.
	Bool
	// Error is an error value, its code in Text.
	Error
)

// Value is a cell value to format.
type Value struct {
	Kind   Kind
	Number float64
	Text   string
}

// Result is a formatted value.
type Result struct {
	// Text is what the cell shows, the fill aside. A "?" placeholder with
	// no digit shows U+2007 FIGURE SPACE, as Calc draws it.
	Text string
	// Fill, when not 0, is the character "*x" repeats: the caller inserts
	// as many copies at the byte offset FillAt of Text as fill the cell,
	// as Calc does.
	Fill   rune
	FillAt int
	// Color is the colour the section names, HasColor reporting one.
	Color    css.Color
	HasColor bool
	// Numeric reports a value a number section showed: Calc aligns it
	// right under the general alignment, as it does a number. A text
	// value, or a number in a text-only section, is not numeric.
	Numeric bool
	// Hash reports a value [Format.Fit] replaced by "###".
	Hash bool
}

// Format is a parsed number format code. A Format is immutable and safe
// for concurrent use.
type Format struct {
	sections []*section
	// standard reports Calc's standard format, General alone: [Format.Fit]
	// narrows it to a column.
	standard bool
	// explicitThird reports a code with three or more sections; with a
	// condition and two sections, the third is General.
}

// General is the General format.
var General = &Format{sections: []*section{{kind: secGeneral, toks: []token{{kind: tGeneral}}}}, standard: true}

// Parse reads a format code. "" and "General" (in any case) give
// [General]. It returns [ErrSyntax] for a code Calc refuses.
func Parse(code string) (*Format, error) {
	if len(code) > maxCodeBytes {
		return nil, fmt.Errorf("%w: %d bytes, at most %d", ErrSyntax, len(code), maxCodeBytes)
	}
	if code == "" || strings.EqualFold(code, "General") {
		return General, nil
	}
	parts, err := splitSections(code)
	if err != nil {
		return nil, err
	}
	f := &Format{}
	for _, p := range parts {
		s, err := scanSection(p)
		if err != nil {
			return nil, err
		}
		f.sections = append(f.sections, s)
	}
	f.defaultConditions()
	// A code of a locale tag alone formats as General.
	if len(f.sections) == 1 && len(f.sections[0].toks) == 0 && !f.sections[0].empty {
		f.sections[0].kind = secGeneral
		f.sections[0].toks = []token{{kind: tGeneral}}
		f.standard = f.sections[0].color == nil && !f.sections[0].explicitCond
	}
	return f, nil
}

// defaultConditions gives the sections the conditions LibreOffice
// implies (svl/source/numbers/zformat.cxx, the SvNumberformat
// constructor): the first section > 0 and the second < 0 when a later
// one follows, the first >= 0 when two sections without conditions are
// all; a missing third section after two with conditions is General.
func (f *Format) defaultConditions() {
	n := f.numberSections()
	if n >= 2 && f.sections[0].op == opNone {
		f.sections[0].op = opGT
	}
	if n >= 3 && f.sections[1].op == opNone {
		f.sections[1].op = opLT
	}
	if n == 2 && f.sections[1].op != opNone {
		f.sections = append(f.sections[:2], append([]*section{{kind: secGeneral, toks: []token{{kind: tGeneral}}}}, f.sections[2:]...)...)
	}
	if n == 2 && f.sections[0].op == opGT && f.sections[0].limit == 0 && f.sections[1].op == opNone {
		f.sections[0].op = opGE
	}
}

// numberSections returns how many sections format numbers: all but a
// fourth, and but a last text section after the first.
func (f *Format) numberSections() int {
	n := len(f.sections)
	if n == 4 {
		return 3
	}
	if n > 1 && f.sections[n-1].kind == secText {
		return n - 1
	}
	return n
}

// check evaluates a section condition, -1 for none.
func check(v float64, op condOp, limit float64) int {
	b := false
	switch op {
	case opNone:
		return -1
	case opEQ:
		b = v == limit
	case opNE:
		b = v != limit
	case opLT:
		b = v < limit
	case opLE:
		b = v <= limit
	case opGT:
		b = v > limit
	case opGE:
		b = v >= limit
	}
	if b {
		return 1
	}
	return 0
}

// pick returns the index of the section that formats the number v
// (GetSubformatIndex).
func (f *Format) pick(v float64) int {
	n := f.numberSections()
	if c := check(v, f.sections[0].op, f.sections[0].limit); c != 0 || n == 1 {
		return 0
	}
	if n < 2 {
		return 0
	}
	if c := check(v, f.sections[1].op, f.sections[1].limit); c != 0 || n == 2 {
		return 1
	}
	return 2
}

// realNegative reports whether the section i formats negative numbers
// without their sign (IsFirstSubformatRealNegative,
// IsSecondSubformatRealNegative).
func (f *Format) realNegative(i int) bool {
	s0, s1 := f.sections[0], (*section)(nil)
	if len(f.sections) > 1 {
		s1 = f.sections[1]
	}
	op2, l2 := opNone, 0.0
	if s1 != nil {
		op2, l2 = s1.op, s1.limit
	}
	if s0.limit != 0 || l2 != 0 {
		return false
	}
	switch i {
	case 0:
		return (s0.op == opLT && (op2 == opGT || op2 == opEQ || op2 == opGE || op2 == opNone)) ||
			(s0.op == opLE && (op2 == opNone || op2 == opGT))
	case 1:
		return (s0.op == opGE && op2 == opNone) || (s0.op == opGT && op2 == opLT) || (s0.op == opNone && op2 == opNone)
	}
	return false
}

// textSection returns the section that formats text, nil for none: the
// fourth, or a last text section.
func (f *Format) textSection() *section {
	n := len(f.sections)
	if n == 4 {
		return f.sections[3]
	}
	if s := f.sections[n-1]; s.kind == secText {
		return s
	}
	return nil
}

// Apply formats v.
func (f *Format) Apply(v Value) Result {
	switch v.Kind {
	case Text:
		s := f.textSection()
		if s == nil {
			return Result{Text: v.Text}
		}
		r := s.text(v.Text)
		return r
	case Error:
		return Result{Text: v.Text}
	case Bool:
		if f.standard || (len(f.sections) == 1 && f.sections[0].kind == secGeneral) {
			t := "FALSE"
			if v.Number != 0 {
				t = "TRUE"
			}
			r := Result{Text: t}
			if c := f.sections[0].color; c != nil {
				r.Color, r.HasColor = *c, true
			}
			return r
		}
	case Number:
	}
	x := v.Number
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return Result{Text: "#NUM!"}
	}
	i := f.pick(x)
	s := f.sections[i]
	if x < 0 && f.realNegative(i) {
		x = -x
	}
	r := s.number(x, i == 0)
	if s.color != nil {
		r.Color, r.HasColor = *s.color, true
	}
	return r
}

// text formats the text t by the text section s.
func (s *section) text(t string) Result {
	var b strings.Builder
	r := Result{}
	for _, tk := range s.toks {
		switch tk.kind {
		case tAt, tGeneral:
			b.WriteString(t)
		case tFill:
			r.setFill(tk, b.Len())
		case tLit:
			b.WriteString(tk.text)
		case tDigit, tDot, tComma, tPercent, tExp, tSlash, tDate, tAMPM, tAP, tSecFrac:
		}
	}
	r.Text = b.String()
	if s.color != nil {
		r.Color, r.HasColor = *s.color, true
	}
	return r
}

// setFill records the first fill of a section.
func (r *Result) setFill(tk token, at int) {
	if r.Fill != 0 {
		return
	}
	for _, c := range tk.text {
		r.Fill, r.FillAt = c, at
		break
	}
}

// number formats the number x by s; first reports the first section, the
// only one that writes a negative number's sign (ImpGetNumberOutput).
func (s *section) number(x float64, first bool) Result {
	if s.empty {
		return Result{Numeric: true}
	}
	switch s.kind {
	case secGeneral:
		return s.general(x, first)
	case secNumber:
		return s.fixed(x, first)
	case secFraction:
		return s.fraction(x, first)
	case secDate:
		return s.date(x, first)
	case secText:
		// A number in a text section shows in the standard format.
		return s.text(generalString(x))
	case secLiteral:
	}
	r := s.text("")
	r.Numeric = true
	return r
}

// general formats x as the General keyword, with the section's literals.
func (s *section) general(x float64, first bool) Result {
	neg := x < 0
	if neg {
		x = -x
	}
	g := generalString(x)
	if neg && first {
		g = "-" + g
	}
	r := s.text(g)
	r.Numeric = true
	return r
}

// generalString returns x in Calc's standard format at unlimited
// precision (SvNumberformat::GetOutputString, the UNLIMITED_PRECISION
// branch): fixed point for magnitudes in [1e-4, 1e15), scientific beyond,
// at most 15 significant digits, an integer below 2^53 whole.
func generalString(x float64) string {
	if x == 0 {
		return "0"
	}
	if x > -1 && x < 1 {
		fix := x < -expLowerBound || expLowerBound < x
		if !fix {
			// LibreOffice writes small values in fixed point down to
			// 1e-9 when they have few significant digits.
			e := int(math.Ceil(-math.Log10(math.Abs(x))))
			if e <= 9 && approxEqual(roundTo(x, 16), roundTo(x, e+16)) {
				fix = true
			}
		}
		if fix {
			return formatFloat(x, fmtF, maxDecimals, true)
		}
		return formatFloat(x, fmtE2, maxDecimals, true)
	}
	return formatFloat(x, fmtAuto, maxDecimals, true)
}

// expLowerBound is the magnitude below which Calc prefers scientific
// notation (zformat.cxx, EXP_LOWER_BOUND).
const expLowerBound = 1e-4

// roundTo rounds x half away from zero at dec decimals, through its
// shortest decimal representation.
func roundTo(x float64, dec int) float64 {
	if x == 0 || math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	if dec >= 0 && (math.Abs(x) >= 0x1p52 || x == math.Trunc(x)) {
		return x
	}
	p := math.Pow(10, float64(dec))
	if math.IsInf(p, 0) || p == 0 {
		return x
	}
	return math.Copysign(math.Floor(math.Abs(x)*p+0.5)/p, x)
}

// approxEqual reports a and b equal to about 15 significant digits
// (rtl::math::approxEqual).
func approxEqual(a, b float64) bool {
	if a == b {
		return true
	}
	if a == 0 || b == 0 {
		return false
	}
	d := math.Abs(a - b)
	return d < math.Abs(a)*(1.0/(1<<48)) && d < math.Abs(b)*(1.0/(1<<48))
}
