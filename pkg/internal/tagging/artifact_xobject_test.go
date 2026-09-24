package tagging

import "testing"

// TestPageSpans_LeavesTextXObjectUnmarked pins that a region painting a
// text-bearing Form XObject is never swept into an /Artifact.
//
// An artifact is content with no meaning for a reader (ISO 32000-1, 14.8.2.2),
// so marking one over an XObject that paints TEXT hides those words from
// assistive technology.
//
// # Corpus evidence
//
//   - pdfjs TAMReview.pdf: keeps most of its text in such XObjects, and
//     tagging cut it from 11914 extractable characters to 2266.
func TestPageSpans_LeavesTextXObjectUnmarked(t *testing.T) {
	// No structure leaves, so the whole stream is one gap.
	data := []byte("q 1 0 0 1 0 0 cm /ARRS Do Q\n")

	// With no text-bearing XObjects the gap is ordinary graphics: artifact it.
	spans, _ := pageSpans(data, nil, nil)
	if len(spans) != 1 || !spans[0].artifact {
		t.Fatalf("graphics-only gap produced %d span(s) %+v, want one artifact", len(spans), spans)
	}

	// Once /ARRS is known to paint text, the same gap must be left alone.
	spans, _ = pageSpans(data, nil, map[string]bool{"ARRS": true})
	for _, s := range spans {
		if s.artifact {
			t.Errorf("a region painting text-bearing /ARRS was marked as an artifact: %+v", s)
		}
	}
}

// TestShowsText covers the text-operator probe behind that decision.
func TestShowsText(t *testing.T) {
	cases := []struct {
		data string
		want bool
	}{
		{"BT /F1 12 Tf (hi) Tj ET", true},
		{"BT [(a) -20 (b)] TJ ET", true},
		{"BT (line) ' ET", true},
		{`BT 1 2 (x) " ET`, true},
		{"q 1 0 0 1 0 0 cm /Im1 Do Q", false},
		{"0 0 100 100 re f", false},
		{"", false},
	}
	for _, c := range cases {
		if got := showsText([]byte(c.data)); got != c.want {
			t.Errorf("showsText(%q) = %v, want %v", c.data, got, c.want)
		}
	}
}
