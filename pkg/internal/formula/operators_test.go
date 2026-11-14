package formula

import "testing"

// TestCategoryOf_Figure25 checks the dictionary against the mapping of
// MathML Core, B.1, Figure 25, an encoding of the same table apart from
// Figure 27, which the lookup reads.
func TestCategoryOf_Figure25(t *testing.T) {
	for _, c := range []struct {
		content string
		form    form
		want    category
	}{
		{"→", infix, catA}, {"⟶", infix, catA}, {"+", infix, catB}, {"−", infix, catB},
		{"±", infix, catB}, {"&&", infix, catB}, {"*", infix, catC}, {"×", infix, catC},
		{"⋅", infix, catC}, {"!", prefix, catD}, {"¬", prefix, catD}, {"∀", prefix, catD},
		{"!", postfix, catE}, {"′", postfix, catE}, {"(", prefix, catF}, {"[", prefix, catF},
		{"⟨", prefix, catF}, {")", postfix, catG}, {"}", postfix, catG}, {"∫", prefix, catH},
		{"∮", prefix, catH}, {"^", postfix, catI}, {"‾", postfix, catI}, {"⏞", postfix, catI},
		{"∑", prefix, catJ}, {"∏", prefix, catJ}, {"⋃", prefix, catJ}, {"/", infix, catK},
		{"⁡", infix, catK}, {"∂", prefix, catL}, {"√", prefix, catL}, {",", infix, catM},
		{";", infix, catM}, {":", infix, catM}, {"|", infix, catForceDefault},
		{"∼", infix, catForceDefault}, {"=", infix, catDefault}, {"x", infix, catDefault},
		{"sin", infix, catDefault}, {"≠", infix, catDefault}, {"≠", infix, catDefault},
		{"↛", infix, catA}, {"̠", infix, catDefault}, {"|", prefix, catF},
		{"|", postfix, catG}, {"\U0001EEF0", postfix, catI}, {"\U0001EEF0", infix, catDefault},
	} {
		if got := categoryOf(c.content, c.form); got != c.want {
			t.Errorf("categoryOf(%q, %d) = %d, want %d", c.content, c.form, got, c.want)
		}
	}
}

// TestCategoryOf_EveryEntryCounted reads the 716 entries of Figure 27.
func TestCategoryOf_EveryEntryCounted(t *testing.T) {
	n := 0
	for _, r := range operatorEntries {
		n += int(r[1]-r[0]) + 1
	}
	if n != 716 || len(operatorEntries) != 236 {
		t.Fatalf("%d entries in %d ranges, Figure 27 holds 716 in 236", n, len(operatorEntries))
	}
	if len(inlineStretchAxis) != 246 {
		t.Fatalf("%d inline stretch axis entries, Figure 28 holds 246", len(inlineStretchAxis))
	}
}

// TestDictionaryCategory_TriesOtherForms falls back to the infix, postfix
// and prefix forms only when no form attribute was given (MathML Core,
// 3.2.4.2).
func TestDictionaryCategory_TriesOtherForms(t *testing.T) {
	if got := dictionaryCategory("∑", infix, false); got != catJ {
		t.Errorf("an infix ∑ without a form attribute = %d, want J", got)
	}
	if got := dictionaryCategory("∑", infix, true); got != catDefault {
		t.Errorf("form=\"infix\" ∑ = %d, want Default", got)
	}
}

// TestStretchAxis_AndItalics reads Figure 28 and Appendix C.1.
func TestStretchAxis_AndItalics(t *testing.T) {
	if !stretchesInline('→') || !stretchesInline('=') || stretchesInline('(') {
		t.Error("intrinsic stretch axis misread")
	}
	if r, ok := ItalicForm('h'); !ok || r != 0x210E {
		t.Errorf("italic h = %U, %v; want U+210E", r, ok)
	}
	if r, ok := ItalicForm('x'); !ok || r != 0x1D465 {
		t.Errorf("italic x = %U, %v", r, ok)
	}
	if _, ok := ItalicForm('∞'); ok {
		t.Error("∞ has an italic form")
	}
}
