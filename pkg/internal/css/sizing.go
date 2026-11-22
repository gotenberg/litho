package css

// This file defines the sizing keywords of width, height and their
// minimums and maximums (CSS Sizing 3, 3.1 and 3.2; CSS Sizing 4, 3.1):
// min-content, max-content, fit-content and stretch compute to the
// [MinContent], [MaxContent], [FitContent] and [Stretch] kinds of a
// [Dimension], which the layout resolves against the box's content and
// containing block. fit-content() with an argument (CSS Sizing 4, 3.2)
// computes to [FitContentOf], its argument a length-percentage, on width,
// height and their minimums and maximums; flex-basis, whose <'width'>
// takes it too, keeps it invalid. Chromium parses no fit-content() on
// these properties, so the standard decides.

import "maps"

// fitSpec is a specified fit-content(<length-percentage>).
type fitSpec struct{ l length }

// parseFitContent parses fit-content(<length-percentage [0,∞]>) (CSS
// Sizing 4, 3.2), false for any other value.
func parseFitContent(v []token) (fitSpec, bool) {
	t, ok := single(v)
	if !ok || t.typ != tFunction || lower(t.value) != "fit-content" {
		return fitSpec{}, false
	}
	arg := significant(t.children)
	if len(arg) != 1 {
		return fitSpec{}, false
	}
	l, ok := parseLength(arg[0], allowPct|nonNeg|allowMixed)
	return fitSpec{l}, ok
}

// withFitContent returns def with fit-content(<length-percentage>) parsed
// and computed into the dimension at f; autoF, when not nil, is the flag
// of a minimum's auto keyword, which the value clears.
func withFitContent(def propDef, f func(*Style) *Dimension, autoF func(*Style) *bool) propDef {
	parse, compute := def.parse, def.compute
	def.parse = func(v []token, c *declCtx) (any, bool) {
		if fs, ok := parseFitContent(v); ok {
			return fs, true
		}
		return parse(v, c)
	}
	def.compute = func(s *styler, st *Style, v any) {
		fs, ok := v.(fitSpec)
		if !ok {
			compute(s, st, v)
			return
		}
		d := fs.l.dim(s.lc, true)
		out := Dimension{Kind: FitContentOf, expr: d.expr}
		switch d.Kind { //nolint:exhaustive // a length-percentage computes to one of these three
		case Points:
			out.Value = d.Value
		case Percent:
			out.Fraction = d.Value
		case Calc:
			out.Value, out.Fraction = d.Value, d.Fraction
		}
		*f(st) = out
		if autoF != nil {
			*autoF(st) = false
		}
	}
	return def
}

// FitLimit returns the argument of a fit-content(<length-percentage>), a
// Points, Percent or Calc dimension; the zero length for any other kind.
func (d Dimension) FitLimit() Dimension {
	switch {
	case d.Kind != FitContentOf:
		return Pts(0)
	case d.expr == nil && d.Fraction == 0:
		return Pts(d.Value)
	case d.expr == nil && d.Value == 0:
		return Dimension{Kind: Percent, Value: d.Fraction}
	}
	return Dimension{Kind: Calc, Value: d.Value, Fraction: d.Fraction, expr: d.expr}
}

// sizingKeywords are the sizing keywords and their kinds, with the
// prefixed spellings Chromium parses on width and height. The -moz-
// spellings are in no specification and Chromium refuses them, so litho
// does too.
var sizingKeywords = map[string]DimKind{
	"min-content": MinContent, "-webkit-min-content": MinContent,
	"max-content": MaxContent, "-webkit-max-content": MaxContent,
	"fit-content": FitContent, "-webkit-fit-content": FitContent,
	"stretch": Stretch, "-webkit-fill-available": Stretch,
}

// withSizing returns kws with the sizing keywords added.
func withSizing(kws map[string]DimKind) map[string]DimKind {
	maps.Copy(kws, sizingKeywords)
	return kws
}

// IsSizingKeyword reports a dimension that holds a sizing keyword:
// min-content, max-content, fit-content, fit-content() or stretch.
func (d Dimension) IsSizingKeyword() bool {
	return d.Kind == MinContent || d.Kind == MaxContent || d.Kind == FitContent || d.Kind == FitContentOf || d.Kind == Stretch
}
