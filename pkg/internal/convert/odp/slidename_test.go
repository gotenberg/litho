package odp

import "testing"

// TestSlideName locks the name a slide shows to the bookmarks the pinned
// LibreOffice writes for pages named on purpose: a stored default name
// reads "Slide" and the page's position, any other name stays.
//
// # Corpus evidence
//
//   - probe, odp: a deck of sixteen pages, fifteen of them named as below
//     at the positions below.
func TestSlideName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		position      int
		stored, shown string
	}{
		{1, "page7", "page7"},
		{2, "Slide 9", "Slide 2"},
		{3, "Overview", "Overview"},
		{4, "page4", "Slide 4"},
		{5, "Slide x", "Slide 5"},
		{6, "Slides", "Slides"},
		{7, "Slide 07", "Slide 7"},
		{8, "page08", "Slide 8"},
		{9, "Slide 8", "Slide 9"},
		{11, "slide 2", "slide 2"},
		{12, "Page 12", "Page 12"},
		{13, "page12x", "page12x"},
		{14, "Slide 10 end", "Slide 14"},
		{15, "page13", "page13"},
		{16, "Slide", "Slide"},
	}
	for _, c := range cases {
		if got := slideName(c.stored, c.position); got != c.shown {
			t.Errorf("page %d named %q shows %q, want %q", c.position, c.stored, got, c.shown)
		}
	}
	// Not measured: a page without a name, and names a number parser
	// would take for the position.
	if got := slideName("", 3); got != "Slide 3" {
		t.Errorf("a page without a name shows %q", got)
	}
	for _, name := range []string{"page", "page+3", "page-3", "page 3", "page99999999999999999999"} {
		if got := slideName(name, 3); got != name {
			t.Errorf("page 3 named %q shows %q", name, got)
		}
	}
}
