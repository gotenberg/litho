package symbolfont

import (
	"strings"
	"testing"
)

func TestRecode(t *testing.T) {
	for _, tc := range []struct {
		char       string
		fams       []string
		draw, text string
		face       string
	}{
		{"•", []string{"Arial"}, "•", "•", "Arial"},
		{"q", []string{"Wingdings"}, "\ue43c", "\uf071", "OpenSymbol"},
		{"\uf06e", []string{"Wingdings"}, "\ue439", "\uf06e", "OpenSymbol"},
		{"\u00a7", []string{"Wingdings"}, "\ue46f", "\uf0a7", "OpenSymbol"},
		{"\uf0b7", []string{"Symbol"}, "\ue12c", "\uf0b7", "OpenSymbol"},
		{"\uf095", []string{"StarBats"}, "\u2022", "\uf095", "OpenSymbol"},
		{"n", []string{"Wingdings 2"}, string(wingDings2Tab['n'-0x20]), "\uf06e", "OpenSymbol"},
		{"\uf0b7", []string{"StarSymbol"}, "\uf0b7", "\uf0b7", "OpenSymbol"},
		{"➢", []string{"Wingdings"}, "➢", "➢", "OpenSymbol"},
		{"\x01", []string{"Symbol"}, "•", "•", "OpenSymbol"},
	} {
		draw, txt, fams := Recode(tc.char, tc.fams)
		if draw != tc.draw || txt != tc.text || len(fams) == 0 || fams[0] != tc.face {
			t.Errorf("Recode(%q, %q) = %q, %q, %q", tc.char, tc.fams, draw, txt, fams)
		}
	}
}

func TestRecodes(t *testing.T) {
	for fam, want := range map[string]bool{"Symbol": true, " Wingdings 2 ": true, "OpenSymbol": false, "Arial": false, "": false} {
		if got := Recodes(fam); got != want {
			t.Errorf("Recodes(%q) = %v, want %v", fam, got, want)
		}
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		family string
		code   rune
		want   rune
		ok     bool
	}{
		{"StarBats", 0x22, 0x25cf, true},
		{"StarBats", 0xf022, 0x25cf, true},
		{"Star Bats", 0xf095, 0x2022, true},
		{"StarBats", 0x5b, 0, false},
		{"StarBats", 0x10, 0, false},
		{"StarBats", 0x2022, 0, false},
		{"Arial", 0x22, 0, false},
	} {
		if got, ok := Lookup(tc.family, tc.code); got != tc.want || ok != tc.ok {
			t.Errorf("Lookup(%q, %#x) = %#x, %t, want %#x, %t", tc.family, tc.code, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDraw(t *testing.T) {
	t.Parallel()
	// clone covers what Standard Symbols PS covers: the Symbol codes at
	// U+0020 to U+00FE and nothing of their private-use form.
	clone := func(fam string, aliases bool, r rune) bool {
		return fam == "OpenSymbol" || aliases && r >= 0x20 && r <= 0xfe
	}
	// own covers what Symbol itself covers through its symbol cmap.
	own := func(fam string, _ bool, r rune) bool { return fam == "Symbol" && r >= 0xf020 && r <= 0xf0ff }
	// opensymbol covers what OpenSymbol covers, nothing of the symbol
	// fonts; bare covers nothing at all.
	opensymbol := func(fam string, _ bool, _ rune) bool { return fam == "OpenSymbol" }
	bare := func(string, bool, rune) bool { return false }
	for _, tc := range []struct {
		name, s, family string
		rule            Rule
		covers          func(string, bool, rune) bool
		want            string
		fams            []string
		changed         bool
	}{
		{"Word, Symbol through its clone", "\uf061\uf0b7", "Symbol", RuleWord, clone, "a\u00b7", nil, true},
		{"Word, Symbol itself", "\uf061\uf0b7", "Symbol", RuleWord, own, "\uf061\uf0b7", nil, false},
		{"Word, Wingdings without a clone", "\uf0fc", "Wingdings", RuleWord, opensymbol, "\ue4c2", []string{"OpenSymbol"}, true},
		{"Word, Symbol without a clone", "\uf061", "Symbol", RuleWord, opensymbol, "\u03b1", []string{"OpenSymbol"}, true},
		{"LibreOffice, Symbol through its clone", "\uf061 a", "Symbol", RuleLibreOffice, clone, "\u03b1 \u03b1", []string{"OpenSymbol"}, true},
		{"LibreOffice, Symbol itself", "\uf061", "Symbol", RuleLibreOffice, own, "\uf061", nil, false},
		{"a text font", "\uf061", "Arial", RuleLibreOffice, opensymbol, "\uf061", nil, false},
		{"OpenSymbol lacking the recoding", "\uf061", "Symbol", RuleLibreOffice, bare, "\uf061", nil, false},
	} {
		got, fams, changed := Draw(tc.s, tc.family, tc.rule, tc.covers)
		if got != tc.want || changed != tc.changed || strings.Join(fams, ",") != strings.Join(tc.fams, ",") {
			t.Errorf("%s: Draw = %q, %v, %t, want %q, %v, %t", tc.name, got, fams, changed, tc.want, tc.fams, tc.changed)
		}
	}
}
