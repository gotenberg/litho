package ods

import "testing"

// TestHyperlinkFormula locks the targets read from HYPERLINK formulas
// (see [hyperlinkFormula]).
func TestHyperlinkFormula(t *testing.T) {
	for _, tc := range []struct {
		f, want string
		ok      bool
	}{
		{`of:=HYPERLINK("http://www.example.com"; "Hyperlink")`, "http://www.example.com", true},
		{`of:=hyperlink("https://a.example/""q""")`, `https://a.example/"q"`, true},
		{`=HYPERLINK("#Sheet2.A1")`, "#Sheet2.A1", true},
		{`of:=HYPERLINK([.A1]; "x")`, "", false},
		{`of:=HYPERLINK("http://a" & "b")`, "", false},
		{`of:=SUM([.A1:.A2])`, "", false},
		{`of:=HYPERLINK("unterminated`, "", false},
	} {
		got, ok := hyperlinkFormula(tc.f)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: %q %v, want %q %v", tc.f, got, ok, tc.want, tc.ok)
		}
	}
}

// TestSoleHyperlinkArgument locks the HYPERLINK calls of one argument
// (see [soleHyperlinkArgument]).
func TestSoleHyperlinkArgument(t *testing.T) {
	for f, want := range map[string]bool{
		`of:=HYPERLINK([.O10])`:                true,
		`of:=HYPERLINK([.O10]&"x;y")`:          true,
		`of:=HYPERLINK([.O10]; "text")`:        false,
		`of:=HYPERLINK(IF([.A1];[.B1];[.C1]))`: true,
		`of:=SUM([.A1])`:                       false,
		`of:=HYPERLINK([.A1]`:                  false,
	} {
		if got := soleHyperlinkArgument(f); got != want {
			t.Errorf("%s: %v, want %v", f, got, want)
		}
	}
}
