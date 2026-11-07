package numfmt

import (
	"math"
	"strings"
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
// left out of the measure.
func (f *Format) Fit(v Value, width float64, measure func(string) float64) Result {
	r := f.Apply(v)
	if v.Kind == Text || measure == nil {
		return r
	}
	if measure(r.Text) <= width {
		return r
	}
	x := v.Number
	if v.Kind != Number || !f.standard || math.IsNaN(x) || math.IsInf(x, 0) {
		return hash(r)
	}
	maxDigit := 0.0
	for c := '0'; c <= '9'; c++ {
		maxDigit = max(maxDigit, measure(string(c)))
	}
	if maxDigit <= 0 {
		return hash(r)
	}
	w := width
	s := narrowGeneral(x, digitCount(w, maxDigit))
	signs, dots, exps := strings.Count(s, "-"), strings.Count(s, "."), strings.Count(s, "E")
	// A small value narrowed to "0" may still fit with the point counted
	// at its own width (output2.cxx, #i112250).
	if s == "0" && x != 0 {
		dots = 1
	}
	w += float64(dots) * (maxDigit - measure("."))
	w += float64(signs) * (maxDigit - measure("-"))
	w += float64(exps) * (maxDigit - measure("E"))
	if signs+dots+exps > 0 {
		s = narrowGeneral(x, digitCount(w, maxDigit))
	}
	if measure(s) > w {
		return hash(r)
	}
	r.Text = s
	return r
}

// digitCount returns how many digits of width digit fit in w.
func digitCount(w, digit float64) int {
	n := math.Floor(w / digit)
	if n < 0 || math.IsNaN(n) {
		return 0
	}
	return int(min(n, 64))
}

// hash returns r showing "###".
func hash(r Result) Result {
	r.Text, r.Fill, r.FillAt, r.Hash = hashText, 0, 0, true
	return r
}

// narrowGeneral returns x in the General format in at most n characters
// where it can (SvNumberformat::GetOutputString with a character count).
func narrowGeneral(x float64, n int) string {
	a := math.Abs(x)
	neg := math.Signbit(x)
	if a < expLowerBound {
		return narrowScientific(x, n)
	}
	e := math.Log10(a)
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
	e := math.Log10(math.Abs(x))
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
