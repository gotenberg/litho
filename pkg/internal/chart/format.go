package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numfmt"
)

// formatLabel formats v with the number format code code, the
// SpreadsheetML syntax Calc also uses for its own formats (ECMA-376 Part 1,
// 18.8.31 numFmt), as Calc shows the value: "" is General, and a code
// numfmt refuses falls back to General. A label is never "###": a chart
// has no column to fit.
func formatLabel(code string, v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}
	f := numfmt.General
	if code != "" {
		if parsed, err := numfmt.Parse(code); err == nil {
			f = parsed
		}
	}
	return f.Apply(numfmt.Value{Kind: numfmt.Number, Number: v}).Text
}

// formatGeneral formats v in the General format.
func formatGeneral(v float64) string {
	return formatLabel("", v)
}
