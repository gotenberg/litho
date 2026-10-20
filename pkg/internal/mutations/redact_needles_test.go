package mutations

import (
	"slices"
	"testing"
)

func TestRedactionNeedles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		runs []string
		want []string
	}{
		{"a run that names something is kept whole", []string{"the secret plan"}, []string{"the secret plan"}},
		{"three letters are enough", []string{"Bob"}, []string{"Bob"}},
		{"three digits are enough", []string{"450"}, []string{"450"}},
		{"a lone letter is not looked for", []string{"A"}, nil},
		{"whitespace and punctuation are not looked for", []string{" ", ".", ") ", "--"}, nil},
		{"two characters are not looked for", []string{"12", " ", "v."}, nil},
		{"a word drawn glyph by glyph is joined", []string{"S", "E", "C", "R", "E", "T"}, []string{"SECRET"}},
		{"a joined line is split into words", []string{"J", "o", "h", "n", " ", "S", "m", "i", "t", "h", " ", "J", "r"},
			[]string{"John", "Smith"}},
		{"punctuation is trimmed from a joined word", []string{"(", "S", "E", "C", "R", "E", "T", ".", ")"}, []string{"SECRET"}},
		{"an identifying run ends a join", []string{"S", "E", "keep", "C", "R", "E"}, []string{"keep", "CRE"}},
		{"each needle once, in the order first met", []string{"plan", "x", "plan", "y"}, []string{"plan"}},
		{"one ideograph names something", []string{"王"}, []string{"王"}},
		{"two kana do not", []string{"の", "に"}, nil},
		{"nothing in, nothing out", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RedactionNeedles(tc.runs); !slices.Equal(got, tc.want) {
				t.Errorf("RedactionNeedles(%q) = %q, want %q", tc.runs, got, tc.want)
			}
		})
	}
}
