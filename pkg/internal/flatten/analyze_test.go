package flatten

import (
	"slices"
	"testing"
)

func TestAnalyzeOpaquePageHasNoRegion(t *testing.T) {
	t.Parallel()
	a := analyze(t, fixture{content: "0 0 1 rg 0 0 100 100 re f BT /F1 12 Tf 10 10 Td (Hi) Tj ET"})
	if len(a.Units) != 2 || len(a.Regions) != 0 {
		t.Fatalf("units %q, %d regions; want path text, none", kinds(a), len(a.Regions))
	}
}

func TestAnalyzeTransparencyOverBackdrop(t *testing.T) {
	t.Parallel()
	// A backdrop under the transparent square stays: the image covers it.
	// The far square no transparent unit overlaps stays too.
	a := analyze(t, fixture{content: "0 0 1 rg 0 0 100 100 re f q /GS1 gs 1 0 0 rg 10 10 30 30 re f Q 0 g 80 80 10 10 re f"})
	if got := kinds(a); got != "path path* path" {
		t.Fatalf("units %q", got)
	}
	if len(a.Regions) != 1 {
		t.Fatalf("%d regions, want 1", len(a.Regions))
	}
	r := a.Regions[0]
	if !slices.Equal(r.Removed, []int{1}) {
		t.Errorf("removed %v, want [1]", r.Removed)
	}
	if r.Insert != a.Units[1].First {
		t.Errorf("insert at %d, want %d, the transparent square's first operator", r.Insert, a.Units[1].First)
	}
	if want := (Rect{10, 10, 40, 40}); r.Box != want {
		t.Errorf("box %v, want %v", r.Box, want)
	}
}

func TestAnalyzeTextStaysAboveTheImage(t *testing.T) {
	t.Parallel()
	// The text drawn first lies under the transparent square: the image is
	// drawn before it, so it stays above, and is counted.
	a := analyze(t, fixture{content: "BT /F1 12 Tf 10 20 Td (Hi) Tj ET /GS1 gs 1 0 0 rg 0 0 50 50 re f"})
	if got := kinds(a); got != "text path*" {
		t.Fatalf("units %q", got)
	}
	r := a.Regions[0]
	if r.Insert != 0 || !slices.Equal(r.Removed, []int{1}) || !slices.Equal(r.Text, []int{0}) {
		t.Fatalf("insert %d removed %v text %v, want 0 [1] [0]", r.Insert, r.Removed, r.Text)
	}
	if r.TextUnderTransparency != 1 {
		t.Errorf("text under transparency %d, want 1", r.TextUnderTransparency)
	}
}

func TestAnalyzeRegionClipsWhatItsTransparencyCovers(t *testing.T) {
	t.Parallel()
	// The opaque square drawn between two transparent ones that overlap
	// stays, clipped out of the region, which keeps its size.
	const content = "q /GS1 gs 10 10 20 20 re f Q 1 1 1 rg 20 20 40 40 re f q /GS1 gs 25 25 10 10 re f Q"
	a := analyze(t, fixture{content: content})
	if got := kinds(a); got != "path* path path*" {
		t.Fatalf("units %q", got)
	}
	if len(a.Regions) != 1 {
		t.Fatalf("%d regions, want 1", len(a.Regions))
	}
	r := a.Regions[0]
	if !slices.Equal(r.Removed, []int{0, 2}) || !slices.Equal(r.Clipped, []int{1}) {
		t.Errorf("removed %v clipped %v, want [0 2] [1]", r.Removed, r.Clipped)
	}
	if want := (Rect{10, 10, 35, 35}); r.Box != want {
		t.Errorf("box %v, want %v", r.Box, want)
	}
	// A form showing text there is removed instead, its text kept above
	// the image, and the region grows to hold it.
	a = analyze(t, fixture{
		resources: "/XObject << /T 7 0 R >>",
		content:   "q /GS1 gs 10 10 20 20 re f Q /T Do q /GS1 gs 25 25 10 10 re f Q",
		objs:      []string{stream("/Type /XObject /Subtype /Form /BBox [20 20 60 60] /Resources << /Font << /F 5 0 R >> >>", "BT /F 9 Tf 22 22 Td (t) Tj ET")},
	})
	if r := a.Regions[0]; !slices.Equal(r.Removed, []int{0, 1, 2}) || r.Box != (Rect{10, 10, 60, 60}) {
		t.Errorf("removed %v box %v, want [0 1 2] {10 10 60 60}", r.Removed, r.Box)
	}
}

func TestAnalyzeOpaqueAfterTransparencyStays(t *testing.T) {
	t.Parallel()
	a := analyze(t, fixture{content: "q /GS1 gs 10 10 20 20 re f Q 1 0 0 rg 20 20 20 20 re f"})
	r := a.Regions[0]
	if !slices.Equal(r.Removed, []int{0}) {
		t.Errorf("removed %v, want [0]: the opaque square after it paints over the image", r.Removed)
	}
}

func TestAnalyzeInsertionLeavesANonRectangularClip(t *testing.T) {
	t.Parallel()
	// A triangle clip is no box the image could be drawn under whole: the
	// image goes before the clip is set, after the q at operation 0.
	a := analyze(t, fixture{content: "q 0 0 m 50 0 l 25 50 l h W n /GS1 gs 1 0 0 rg 10 10 20 20 re f Q"})
	if r := a.Regions[0]; r.Insert != 1 {
		t.Errorf("insert at %d, want 1", r.Insert)
	}
	// A rectangle clip holding the region keeps the image inside it.
	a = analyze(t, fixture{content: "q 0 0 50 50 re W n /GS1 gs 1 0 0 rg 10 10 20 20 re f Q"})
	if r := a.Regions[0]; r.Insert != a.Units[0].First {
		t.Errorf("insert at %d, want %d", r.Insert, a.Units[0].First)
	}
}

func TestAnalyzeForms(t *testing.T) {
	t.Parallel()
	objs := []string{
		// 7: a transparency group of opaque content.
		stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /K true >>", "1 0 0 rg 0 0 10 10 re f"),
		// 8: a form painting with transparency, showing text.
		stream("/Type /XObject /Subtype /Form /BBox [0 0 50 50] /Resources << /ExtGState << /G 6 0 R >> /Font << /F 5 0 R >> >>",
			"/G gs 0 0 20 20 re f BT /F 10 Tf 1 1 Td (x) Tj ET"),
		// 9: an image with a soft mask.
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 10 0 R", "\x00"),
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x80"),
	}
	a := analyze(t, fixture{
		resources: "/XObject << /Opaque 7 0 R /Alpha 8 0 R /Masked 9 0 R >>",
		content:   "/Opaque Do q 1 0 0 1 60 60 cm /Alpha Do Q q 10 0 0 10 0 80 cm /Masked Do Q",
		objs:      objs,
	})
	if got := kinds(a); got != "form form* image*" {
		t.Fatalf("units %q", got)
	}
	if a.Units[0].HasText || !a.Units[1].HasText {
		t.Errorf("text %v %v, want false true", a.Units[0].HasText, a.Units[1].HasText)
	}
	if want := (Rect{60, 60, 100, 100}); a.Units[1].Box != want {
		t.Errorf("form box %v, want %v (the BBox moved, cut to the page)", a.Units[1].Box, want)
	}
}

func TestAnalyzeTransparentText(t *testing.T) {
	t.Parallel()
	a := analyze(t, fixture{content: "/GS1 gs BT /F1 12 Tf 10 20 Td (Hi) Tj ET BT 3 Tr (No) Tj ET"})
	if !slices.Equal(a.TransparentText, []int{0}) || len(a.Regions) != 0 {
		t.Errorf("transparent text %v, %d regions; want [0], none (invisible text paints nothing)", a.TransparentText, len(a.Regions))
	}
}
