package css

import "testing"

// TestInitial_FontFamilyIsNoList locks the initial font-family as a nil
// list, a value of its own: a style that takes it and one whose author
// wrote serif are not shared, since Chromium draws the first with its
// standard font and the second with its serif family, which differ in
// some languages.
func TestInitial_FontFamilyIsNoList(t *testing.T) {
	t.Parallel()
	initial := Initial()
	if initial.FontFamilies != nil {
		t.Fatalf("the initial font-family is %q, want no list", initial.FontFamilies)
	}
	serif := Initial()
	serif.FontFamilies = []string{"serif"}
	table := NewStyles()
	if a, b := table.Share(initial), table.Share(serif); a == b {
		t.Error("the initial font-family and an author's serif share one style")
	}
}
