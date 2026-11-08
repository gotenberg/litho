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
	for i, p := range parts {
		p = systemFormat(p)
		parts[i] = p
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

// The en-US system date and time formats a section tagged [$-F800] or
// [$-F400] stands for, whatever else it says (ECMA-376 Part 1, 18.8.31;
// LibreOffice's SvNumberFormatter maps the tags to NF_DATE_SYSTEM_LONG,
// "NNNNMMMM D, YYYY", and NF_TIME_HHMMSSAMPM).
const (
	systemLongDate = "dddd, mmmm d, yyyy"
	systemLongTime = "h:mm:ss AM/PM"
)

// systemFormat returns the section p, or the system format its tag names
// when it holds more than the tag; a tag alone formats as General.
//
// # Corpus evidence
//
//   - xlsx: sc/tdf165180_date1904.xlsx, "[$-F800]dddd\,\ mmmm\ dd\,\
//     yyyy", prints "Tuesday, March 1, 1904", the day unpadded; the
//     tag alone prints 45000 as 45000 (TestApply_Calc).
func systemFormat(p string) string {
	u := strings.ToUpper(p)
	if strings.HasPrefix(u, "[") && strings.Index(u, "]") == len(u)-1 {
		return p
	}
	switch {
	case strings.Contains(u, "[$-F800]"), strings.Contains(u, "X-SYSDATE]"):
		return systemLongDate
	case strings.Contains(u, "[$-F400]"), strings.Contains(u, "X-SYSTIME]"):
		return systemLongTime
	}
	return p
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
	if s.loc != nil && s.loc.Decimal != "" {
		g = strings.Replace(g, ".", s.loc.Decimal, 1)
	}
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

// Locale is the separators a format writes numbers with, those of the
// locale a document or a format names. The zero Locale is en-US's: "."
// and ",".
type Locale struct {
	// Decimal is the decimal separator, Group the thousands separator.
	Decimal, Group string
}

// localeSeparators are the decimal and thousands separators LibreOffice
// writes numbers with, by language, or by language and country where the
// country changes them (i18npool's locale data); a language not listed
// writes en-US's.
//
// # Corpus evidence
//
//   - LibreOffice 26.8.2.2: 1234567.5 in #,##0.00 of each locale prints
//     1.234.567,50 in de-DE, es-ES, it-IT, nl-NL, pt-BR, da-DK, tr-TR,
//     el-GR, ro-RO, id-ID, vi-VN, ca-ES, hr-HR, sl-SI and lt-LT;
//     1 234 567,50 with a no-break space in fr-FR, fr-CA, hu-HU, ru-RU,
//     pl-PL, sv-SE, cs-CZ, fi-FI, nb-NO, uk-UA, sk-SK, bg-BG, lv-LV,
//     et-EE and pt-PT; 1'234'567.50 in de-CH; 1,234,567.50 in en-US,
//     en-GB, ja-JP, zh-CN, ko-KR, he-IL, th-TH and es-MX.
var localeSeparators = map[string]Locale{
	"de": {",", "."}, "es": {",", "."}, "it": {",", "."}, "nl": {",", "."}, "pt": {",", "."},
	"da": {",", "."}, "tr": {",", "."}, "el": {",", "."}, "ro": {",", "."}, "id": {",", "."},
	"vi": {",", "."}, "ca": {",", "."}, "hr": {",", "."}, "sl": {",", "."}, "lt": {",", "."},
	"fr": {",", "\u00a0"}, "hu": {",", "\u00a0"}, "ru": {",", "\u00a0"}, "pl": {",", "\u00a0"},
	"sv": {",", "\u00a0"}, "cs": {",", "\u00a0"}, "fi": {",", "\u00a0"}, "nb": {",", "\u00a0"},
	"uk": {",", "\u00a0"}, "sk": {",", "\u00a0"}, "bg": {",", "\u00a0"}, "lv": {",", "\u00a0"},
	"et": {",", "\u00a0"}, "pt-pt": {",", "\u00a0"}, "de-ch": {".", "'"}, "es-mx": {}, "en": {},
}

// LocaleOf returns the separators of the language tag lang ("de-DE",
// "es_ES", "hu"), en-US's for a language LibreOffice writes so or does
// not list, and for an empty tag.
func LocaleOf(lang string) Locale {
	l := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(lang)), "_", "-")
	if loc, ok := localeSeparators[l]; ok {
		return loc
	}
	if i := strings.IndexByte(l, '-'); i >= 0 {
		if loc, ok := localeSeparators[l[:i]]; ok {
			return loc
		}
	}
	return Locale{}
}

// WithLocale returns f writing its separators in loc: a copy, f itself
// for the en-US separators.
func (f *Format) WithLocale(loc Locale) *Format {
	if loc == (Locale{}) || loc == (Locale{Decimal: ".", Group: ","}) {
		return f
	}
	out := *f
	out.sections = make([]*section, len(f.sections))
	for i, s := range f.sections {
		c := *s
		c.loc = &loc
		out.sections[i] = &c
	}
	return &out
}

// lcidTags names the Windows locale identifiers (MS-LCID) of the locales
// whose separators [localeSeparators] lists.
var lcidTags = map[uint64]string{
	0x407: "de-DE", 0x807: "de-CH", 0xC07: "de-AT", 0x40A: "es-ES", 0xC0A: "es-ES", 0x80A: "es-MX",
	0x410: "it-IT", 0x413: "nl-NL", 0x813: "nl-BE", 0x416: "pt-BR", 0x816: "pt-PT", 0x406: "da-DK",
	0x41F: "tr-TR", 0x408: "el-GR", 0x418: "ro-RO", 0x421: "id-ID", 0x42A: "vi-VN", 0x403: "ca-ES",
	0x41A: "hr-HR", 0x424: "sl-SI", 0x427: "lt-LT", 0x40C: "fr-FR", 0xC0C: "fr-CA", 0x80C: "fr-BE",
	0x40E: "hu-HU", 0x419: "ru-RU", 0x415: "pl-PL", 0x41D: "sv-SE", 0x405: "cs-CZ", 0x40B: "fi-FI",
	0x414: "nb-NO", 0x422: "uk-UA", 0x41B: "sk-SK", 0x402: "bg-BG", 0x426: "lv-LV", 0x425: "et-EE",
	0x409: "en-US", 0x809: "en-GB",
}

// LCIDOf returns the Windows locale identifier of the language tag lang
// ("hu-HU"), for a reader that writes a locale into a format code as a
// [$-LCID] tag; false for a locale whose separators are en-US's or that
// [lcidTags] lacks.
func LCIDOf(lang string) (uint64, bool) {
	l := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(lang)), "_", "-")
	loc := LocaleOf(l)
	if loc == (Locale{}) {
		return 0, false
	}
	prefix, _, _ := strings.Cut(l, "-")
	best, found := uint64(0), false
	for id, tag := range lcidTags {
		t := strings.ToLower(tag)
		if t == l {
			return id, true
		}
		if strings.HasPrefix(t, prefix+"-") && LocaleOf(t) == loc && (!found || id < best) {
			best, found = id, true
		}
	}
	return best, found
}
