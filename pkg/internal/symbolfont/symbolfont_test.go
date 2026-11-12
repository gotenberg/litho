package symbolfont

import "testing"

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
