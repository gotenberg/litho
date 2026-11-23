package layout

import "testing"

// TestStatedLang locks the language a document whose source states none
// takes from its text: the one whose text counts the most characters, a
// node's Lang reaching the text below it, a tie going to the first stated,
// case aside; text in no language counts for none.
func TestStatedLang(t *testing.T) {
	in := inlineSty()
	run := func(lang, s string) *Node { n := el(in, txt(in, s)); n.Lang = lang; return n }
	for _, tc := range []struct {
		name string
		root *Node
		want string
	}{
		{"most characters", el(sty(), run("fr", "un"), run("en-US", "longer text"), run("fr", "deux")), "en-US"},
		{"inherited", el(sty(), func() *Node { p := el(sty(), txt(in, "a long text in German")); p.Lang = "de"; return p }(), run("fr", "court")), "de"},
		{"tie to the first", el(sty(), run("pl-PL", "abc"), run("EN", "abc")), "pl-PL"},
		{"case folded", el(sty(), run("en-us", "ab"), run("EN-US", "cd"), run("fr", "xyz")), "en-us"},
		{"none stated", el(sty(), txt(in, "plain")), ""},
	} {
		if got := StatedLang(tc.root); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
