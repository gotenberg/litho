package formula

import "testing"

// tok returns a token element of name holding s.
func tok(name, s string) *Element { return &Element{Name: name, Text: s} }

// el returns an element of name with children.
func el(name string, kids ...*Element) *Element { return &Element{Name: name, Children: kids} }

// TestAltText_PrefersTheAuthorsText reads the alttext attribute of the
// root before the tokens (MathML Core, 2.1.1).
func TestAltText_PrefersTheAuthorsText(t *testing.T) {
	root := el("math", tok("mi", "x"))
	root.Attrs = []Attr{{Name: "alttext", Value: "  x  squared "}}
	if got := AltText(root); got != "x squared" {
		t.Fatalf("AltText = %q, want %q", got, "x squared")
	}
}

// TestAltText_ReadsTheTokensInOrder joins the drawn tokens, the
// annotations of semantics and the other actions of maction left out.
func TestAltText_ReadsTheTokensInOrder(t *testing.T) {
	root := el("math",
		el("msup", tok("mi", "x"), tok("mn", "2")),
		tok("mo", "+"),
		el("semantics", tok("mn", "1"), &Element{Name: "annotation", Text: "one"}),
		el("maction", tok("mi", "a"), tok("mi", "b")),
	)
	if got := AltText(root); got != "x 2 + 1 a" {
		t.Fatalf("AltText = %q, want %q", got, "x 2 + 1 a")
	}
	blank := el("math", tok("mi", "y"))
	blank.Attrs = []Attr{{Name: "alttext", Value: "   "}}
	if got := AltText(blank); got != "y" {
		t.Fatalf("AltText of a blank alttext = %q, want the tokens", got)
	}
}

// TestElement_AttrIsCaseInsensitive compares keyword values without case
// (MathML Core, 2.1.2).
func TestElement_AttrIsCaseInsensitive(t *testing.T) {
	e := &Element{Name: "mfrac", Attrs: []Attr{{Name: "displaystyle", Value: " TRUE"}}}
	if !e.attrIs("displaystyle", "true") {
		t.Fatal("displaystyle=\" TRUE\" does not read as true")
	}
	if e.attrIs("scriptlevel", "true") {
		t.Fatal("an absent attribute reads as set")
	}
}
