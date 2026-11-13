package css

// IsLength reports whether d resolves to a length against a reference: a
// Points, a Percent or a Calc.
func (d Dimension) IsLength() bool {
	return d.Kind == Points || d.Kind == Percent || d.Kind == Calc
}

// HasPercent reports whether d depends on its reference: a Percent or a
// Calc. A percentage of an indefinite reference makes either behave as
// auto where the property says so (CSS 2.1, 10.5; CSS Values 4, 10.8).
func (d Dimension) HasPercent() bool {
	return d.Kind == Percent || d.Kind == Calc
}

// Resolve returns the length d stands for against ref, the size its
// percentage is of: Value for Points, Value times ref for Percent, and
// the calculation of a Calc, clamped to the largest length litho lays
// out. It returns 0 for the other kinds.
func (d Dimension) Resolve(ref float64) float64 {
	switch d.Kind { //nolint:exhaustive // the keywords resolve to no length
	case Points:
		return clampLen(d.Value)
	case Percent:
		return clampLen(d.Value * clampLen(ref))
	case Calc:
		if d.expr != nil {
			return clampLen(censor(d.expr.eval(&calcScope{basis: clampLen(ref)})))
		}
		return clampLen(clampLen(d.Value) + clampLen(d.Fraction*clampLen(ref)))
	}
	return 0
}
