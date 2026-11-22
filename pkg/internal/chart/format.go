package chart

import (
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/models"
)

// formatLabel formats v with the number format code code, the
// SpreadsheetML syntax Calc also uses for its own formats (ECMA-376 Part 1,
// 18.8.31 numFmt), as Calc shows the value: "" is General, and a code
// numfmt refuses falls back to General. The separators are loc's, unless
// the code names a locale of its own ([$-407]). A label is never "###": a
// chart has no column to fit.
func formatLabel(code string, v float64, loc models.Locale) string {
	if !numeric.Finite(v) {
		return ""
	}
	f := numfmt.General
	if code != "" {
		if parsed, err := numfmt.Parse(code); err == nil {
			f = parsed
		}
	}
	if !strings.Contains(code, "[$-") {
		f = f.WithLocale(loc)
	}
	return f.Apply(numfmt.Value{Kind: numfmt.Number, Number: v}).Text
}

// formatGeneral formats the index v in the General format.
func formatGeneral(v float64) string {
	return formatLabel("", v, "")
}

// valueText formats the value v of a data label with code. Under
// [OfficeRules] a General value takes at most officeGeneralChars
// characters, fewer decimals first, then scientific notation, as Excel
// narrows a General number in a cell ([numfmt.Format.Fit] under
// [numfmt.Format.Excel]).
func (rd *renderer) valueText(code string, v float64) string {
	if !rd.office() || (code != "" && !strings.EqualFold(code, "General")) || !numeric.Finite(v) {
		return formatLabel(code, v, rd.c.Locale)
	}
	f := numfmt.General.Excel().WithLocale(rd.c.Locale)
	r := f.Fit(numfmt.Value{Kind: numfmt.Number, Number: v}, officeGeneralChars, func(s string) float64 {
		return float64(utf8.RuneCountInString(s))
	})
	return r.Text
}

// officeGeneralChars is the most characters Office writes a data label's
// General value in, a point and a sign counted as characters.
//
// # Corpus evidence
//
//   - openxmlsdk/xlsx: O14ISOStrict/Excel/xl8galry.xlsx's first chart
//     labels 1189.6790093003899, 573.65446810428102 and 1594.36831022005
//     as 1189.679009, 573.6544681 and 1594.36831 in Excel, eleven
//     characters at most, the last a trailing zero short.
const officeGeneralChars = 11
