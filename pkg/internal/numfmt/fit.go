package numfmt

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// hashText is what Calc shows for a number too wide for its cell.
const hashText = "###"

// Fit formats v for a cell whose text may be width points wide, measure
// returning the width of a string in the cell's font, as Calc draws a
// value cell (sc/source/ui/view/output2.cxx, SetTextToWidthOrHash): text
// is returned as Apply formats it, whatever its width; a number, boolean
// or error that fits is too; a number in the General format is written
// with fewer decimals, then in scientific notation, to fit; any other
// value that does not fit is "###", with [Result.Hash] set. A fill is
// left out of the measure. Under Excel's rules ([Format.Excel]) a General
// number keeps as many characters as digits of the widest width fit the
// cell, whether its text fits or not, a point or an exponent counted as a
// digit and a minus sign at its own width (see excelGeneral).
func (f *Format) Fit(v Value, width float64, measure func(string) float64) Result {
	r := f.Apply(v)
	if v.Kind == Text || measure == nil {
		return r
	}
	x := v.Number
	general := v.Kind == Number && f.standard && numeric.Finite(x)
	// Excel narrows a General number by its characters, the text that
	// fits by its width included ([Format.excelGeneral]).
	if (!general || !f.excel) && measure(r.Text) <= width {
		return r
	}
	if !general {
		return f.hash(r)
	}
	maxDigit := 0.0
	for _, d := range digitTexts {
		maxDigit = max(maxDigit, measure(d))
	}
	if maxDigit <= 0 {
		return f.hash(r)
	}
	w := width
	if f.excel {
		room := w
		if math.Signbit(x) {
			room -= measure("-")
		}
		n := min(digitCount(room, maxDigit), excelGeneralChars)
		// The text Apply wrote holds at most excelGeneralChars; one that
		// already keeps within n characters is what narrowing would write.
		if utf8.RuneCountInString(r.Text)-strings.Count(r.Text, "-") <= n && measure(r.Text) <= w {
			return r
		}
		return f.excelGeneral(r, excelNarrow(x, n), w, measure)
	}
	s := narrowGeneral(x, digitCount(w, maxDigit))
	signs, dots, exps := strings.Count(s, "-"), strings.Count(s, "."), strings.Count(s, "E")
	// A small value narrowed to "0" may still fit with the point counted
	// at its own width (output2.cxx, #i112250).
	if s == "0" && x != 0 {
		dots = 1
	}
	w += float64(float64(dots) * (maxDigit - measure(".")))
	w += float64(float64(signs) * (maxDigit - measure("-")))
	w += float64(float64(exps) * (maxDigit - measure("E")))
	if signs+dots+exps > 0 {
		s = narrowGeneral(x, digitCount(w, maxDigit))
	}
	if loc := f.sections[0].loc; loc != nil {
		s = strings.Replace(s, ".", loc.decimal, 1)
	}
	if measure(s) > w {
		return f.hash(r)
	}
	r.Text = s
	return r
}

// digitTexts are the ten digits, each a string of its own written once.
var digitTexts = [10]string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

// excelGeneral returns r showing s, a General number narrowed to as many
// characters as digits fit the cell ([excelNarrow]), a minus sign's room
// taken first, "###" when s does not fit width: Excel counts each
// character of a narrowed General number but the sign as a digit, where
// Calc counts a point, a sign and an exponent at their own widths.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/escape-unicode prints 1161014163 as 1.16E+09 in
//     a column of the default width, where Calc fits 1.161E+09.
//   - litho/xlsx: large_numbers_general, in a column of 8.43 characters,
//     prints 12345678901 as 1.2E+10 and 0.000123456789 as 0.00012, seven
//     characters, and -1234567.891 as -1234568, seven and the sign, where
//     1.23E+10 is narrower than -1234568.
//   - libreoffice/xlsx: sc/Sparklines.xlsx prints 2.684713 whole and
//     -6.105228 as -6.10523 in one column: eight characters, or seven
//     and the sign.
func (f *Format) excelGeneral(r Result, s string, w float64, measure func(string) float64) Result {
	if loc := f.sections[0].loc; loc != nil {
		s = strings.Replace(s, ".", loc.decimal, 1)
	}
	if measure(s) > w {
		return f.hash(r)
	}
	r.Text = s
	return r
}

// maxFitDigits caps the digits a General number is narrowed to. Anti-DoS
// guard, not spec-mandated: a double carries 17 significant digits, and
// formatFloat writes at most 15.
const maxFitDigits = 64

// digitCount returns how many digits of width digit fit in w.
func digitCount(w, digit float64) int {
	n := math.Floor(w / digit)
	if !numeric.NonNegative(n) {
		return 0
	}
	return int(min(n, maxFitDigits))
}

// hash returns r showing "###", or under Excel's rules a cell filled
// with "#".
func (f *Format) hash(r Result) Result {
	r.Text, r.Fill, r.FillAt, r.Hash = hashText, 0, 0, true
	if f.excel {
		r.Text, r.Fill = "", '#'
	}
	return r
}

// excelGeneralChars is how many characters Excel's General format shows a
// number in at most, a minus sign aside: a number that needs more is
// rounded, or written in scientific notation (ECMA-376 Part 1, 18.8.30
// numFmt leaves General to the application).
//
// # Corpus evidence
//
//   - litho/xlsx: large_numbers_general, in a column 30 characters wide,
//     prints 12345678901 whole, 123456789012345 as 1.23457E+14,
//     0.000123456789 as 0.000123457, 0.3333333333333333 as 0.333333333,
//     2.2250738585072014E-308 as 2.2251E-308, -1234567.891 whole,
//     100000000000 and 99999999999.5 as 1E+11, 1E+16 as 1E+16.
const excelGeneralChars = 11

// excelLowerBound is the magnitude below which Excel's General format
// prefers scientific notation, where Calc's is [expLowerBound].
//
// # Corpus evidence
//
//   - litho/xlsx: large_numbers_general prints 0.00001 as 0.00001 in
//     columns of 8.43, 14 and 30 characters, where Calc writes 1E-05.
const excelLowerBound = 1e-5

// excelNarrow returns x in Excel's General format in at most n
// characters, a minus sign aside: the characters of [narrowGeneral] with
// fixed point down to [excelLowerBound].
func excelNarrow(x float64, n int) string {
	if x == 0 || !numeric.Finite(x) {
		return generalString(x)
	}
	a := math.Abs(x)
	var s string
	// A number Calc's standard format writes in fixed point within n
	// characters is written alike: no digit it keeps rounds away.
	if g := generalString(a); a >= excelLowerBound && len(g) <= n && !strings.ContainsAny(g, "Ee") {
		s = g
	} else if a < excelLowerBound || a >= expLowerBound {
		s = narrowGeneral(a, n)
	} else {
		s = formatFloat(a, fmtF, max(n-2, 0), true)
		if len(s) > n || strings.Trim(s, "0.") == "" {
			s = narrowScientific(a, n)
		}
	}
	if math.Signbit(x) && strings.Trim(s, "0.") != "" {
		s = "-" + s
	}
	return s
}

// narrowGeneral returns x in the General format in at most n characters
// where it can (SvNumberformat::GetOutputString with a character count).
func narrowGeneral(x float64, n int) string {
	a := math.Abs(x)
	neg := math.Signbit(x)
	if a < expLowerBound {
		return narrowScientific(x, n)
	}
	e := detmath.Log10(a)
	pre := 1
	if e >= 0 {
		pre = int(math.Ceil(e))
	}
	if pre > 15 {
		return narrowScientific(x, n)
	}
	prec := 0
	if n >= pre {
		prec = n - pre
	}
	if prec > 0 && neg {
		prec--
	}
	if prec > 0 {
		prec--
	}
	s := formatFloat(x, fmtF, min(prec, 15), true)
	if s[0] == '-' && strings.Trim(s[1:], "0") == "" {
		s = s[1:]
	}
	if len(s) > n {
		return narrowScientific(x, n)
	}
	return s
}

// narrowScientific returns x in scientific notation in about n
// characters (lcl_GetOutputStringScientific).
func narrowScientific(x float64, n int) string {
	e := detmath.Log10(math.Abs(x))
	if e < 0 {
		e = 1 - e
	}
	format := 6
	if e >= 100 {
		format = 7
	}
	prec := 0
	if n > format {
		prec = n - format
	}
	if prec > 0 && math.Signbit(x) {
		prec--
	}
	return formatFloat(x, fmtE2, min(prec, 14), true)
}
