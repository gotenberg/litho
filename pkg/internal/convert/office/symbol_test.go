package office

import "testing"

func TestSymbolText(t *testing.T) {
	for _, tc := range []struct{ text, family, want string }{
		{"", "Symbol", "•"},
		{"a\tb", "Symbol", "α\tβ"},
		{"", "Arial", ""},
		{"x", "OpenSymbol", ""},
		{"\t", "Wingdings", ""},
	} {
		if got := SymbolText(tc.text, tc.family); got != tc.want {
			t.Errorf("SymbolText(%q, %q) = %q, want %q", tc.text, tc.family, got, tc.want)
		}
	}
}
