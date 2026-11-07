package numfmt

import (
	"math"
	"testing"
	"unicode/utf8"
)

func FuzzParse(f *testing.F) {
	f.Add(`#,##0.00;[Red]-#,##0.00;"zero";@`, 1234.5)
	f.Add(`[h]:mm:ss.00 AM/PM`, 1.5)
	f.Add(`# ??/??`, -3.3)
	f.Add(`"unclosed`, 0.0)
	f.Fuzz(func(t *testing.T, code string, v float64) {
		fm, err := Parse(code)
		if err != nil {
			return
		}
		measure := func(s string) float64 { return float64(utf8.RuneCountInString(s)) * 5 }
		for _, val := range []Value{
			{Kind: Number, Number: v},
			{Kind: Number, Number: -v},
			{Kind: Number, Number: math.Inf(1)},
			{Kind: Text, Text: code},
			{Kind: Bool, Number: 1},
			{Kind: Error, Text: "#N/A"},
		} {
			r := fm.Apply(val)
			if r.FillAt < 0 || r.FillAt > len(r.Text) {
				t.Fatalf("fill offset %d outside %q", r.FillAt, r.Text)
			}
			fm.Fit(val, 30, measure)
		}
	})
}
