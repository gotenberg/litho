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
