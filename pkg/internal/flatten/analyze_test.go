package flatten

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
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
	// drawn before it, so it stays above, before the region's last
	// transparent unit.
	a := analyze(t, fixture{content: "BT /F1 12 Tf 10 20 Td (Hi) Tj ET /GS1 gs 1 0 0 rg 0 0 50 50 re f"})
	if got := kinds(a); got != "text path*" {
		t.Fatalf("units %q", got)
	}
	r := a.Regions[0]
	if r.Insert != 0 || !slices.Equal(r.Removed, []int{1}) || !slices.Equal(r.Text, []int{0}) {
		t.Fatalf("insert %d removed %v text %v, want 0 [1] [0]", r.Insert, r.Removed, r.Text)
	}
	if r.Last != 1 {
		t.Errorf("last %d, want 1", r.Last)
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

func TestAnalyzeOnlyWhatTransparencyCoversIsTakenOver(t *testing.T) {
	t.Parallel()
	// The black strip lies over the first square and under nothing
	// transparent: it stays as it is, drawn over the image as before.
	a := analyze(t, fixture{content: "q /GS1 gs 1 0 0 rg 5 5 20 20 re f Q 0 g 0 0 40 8 re f q /GS1 gs 0 0 1 rg 20 20 10 10 re f Q"})
	r := a.Regions[0]
	if !slices.Equal(r.Removed, []int{0, 2}) || len(r.Clipped) != 0 {
		t.Errorf("removed %v clipped %v, want [0 2] and none", r.Removed, r.Clipped)
	}
	// The black square covers the text and the transparent square covers
	// it: the square is clipped out of the region.
	a = analyze(t, fixture{content: "BT /F1 12 Tf 10 20 Td (Hi) Tj ET 0 g 0 0 40 40 re f q /GS1 gs 1 0 0 rg 5 5 40 40 re f Q"})
	r = a.Regions[0]
	if !slices.Equal(r.Clipped, []int{1}) || !slices.Equal(r.Text, []int{0}) {
		t.Errorf("clipped %v text %v, want [1] and [0]", r.Clipped, r.Text)
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

// TestAnalyzeTransparentTextInUnusableICC checks that text in an ICCBased
// space whose stream holds no profile composites over the paper of the space
// it is drawn in, its /Alternate (ISO 32000-1, 8.6.5.5, Table 66): a spot
// colour's tint, where the paper is tint 0 (8.6.6.4), not the grey of its
// /N, where it is 1; and a Lab alternate, which no formula composites,
// drops the text's transparency instead.
func TestAnalyzeTransparentTextInUnusableICC(t *testing.T) {
	t.Parallel()
	const noProfile = "litho: not an ICC profile"
	a := analyze(t, fixture{
		resources: "/ColorSpace << /CS0 [/ICCBased 7 0 R] >>",
		content:   "/GS1 gs /CS0 cs 0.6 sc BT /F1 12 Tf 10 20 Td (Hi) Tj ET",
		objs: []string{stream("/N 1 /Alternate [/Separation /Spot /DeviceCMYK << /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 0 0] /N 1 >>]",
			noProfile)},
	})
	if e := a.TextEdits[a.Units[0].First+3]; e.Prefix != "0.3 scn" || e.Suffix != "0.6 scn" {
		t.Errorf("edit %+v, want tint 0.3 drawn and 0.6 put back", e)
	}
	a = analyze(t, fixture{
		resources: "/ColorSpace << /CS0 [/ICCBased 7 0 R] >>",
		content:   "/GS1 gs /CS0 cs 50 20 -30 sc BT /F1 12 Tf 10 20 Td (Hi) Tj ET",
		objs:      []string{stream("/N 3 /Alternate [/Lab << /WhitePoint [0.9505 1 1.089] >>]", noProfile)},
	})
	if len(a.TextEdits) != 0 || !slices.Equal(a.TransparentText, []int{0}) {
		t.Errorf("transparent text %v edits %v, want [0] and none", a.TransparentText, a.TextEdits)
	}
}

func TestAnalyzeTransparentText(t *testing.T) {
	t.Parallel()
	// On the paper, half-transparent black is drawn exactly as opaque grey;
	// invisible text paints nothing.
	a := analyze(t, fixture{content: "/GS1 gs BT /F1 12 Tf 10 20 Td (Hi) Tj ET BT 3 Tr (No) Tj ET"})
	if len(a.TransparentText) != 0 || len(a.Regions) != 0 {
		t.Errorf("transparent text %v, %d regions; want none and none", a.TransparentText, len(a.Regions))
	}
	if e := a.TextEdits[a.Units[0].First+3]; e.Prefix != "0.5 sc" || e.Suffix != "0 sc" {
		t.Errorf("edit %+v, want grey 0.5 drawn and black put back", e)
	}
	// Over a square drawn before it, the grey only approximates it.
	a = analyze(t, fixture{content: "0 0 1 rg 0 0 50 50 re f /GS1 gs 0 g BT /F1 12 Tf 10 20 Td (Hi) Tj ET"})
	if !slices.Equal(a.TransparentText, []int{1}) {
		t.Errorf("transparent text %v, want [1]", a.TransparentText)
	}
	// A soft mask has no opaque reading at all.
	a = analyze(t, fixture{
		resources: "/ExtGState << /SM << /SMask << /S /Luminosity /G 7 0 R >> >> >>",
		content:   "/SM gs BT /F1 12 Tf 10 20 Td (Hi) Tj ET",
		objs:      []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency >>", "1 g 0 0 100 100 re f")},
	})
	if !slices.Equal(a.TransparentText, []int{0}) || len(a.TextEdits) != 0 {
		t.Errorf("transparent text %v edits %v, want [0] and none", a.TransparentText, a.TextEdits)
	}
}

// analyzeSeeds runs [analyzeSeeded] over the fixture's content, the units
// that begin at the operations seeds the region seeds.
func analyzeSeeds(t *testing.T, f fixture, seeds ...int) *Analysis {
	t.Helper()
	doc, pg := open(t, f.bytes())
	ops, _, err := content.ParseStreamDataWithOffsets(context.Background(), []byte(f.content), content.SplitGluedOperators())
	if err != nil {
		t.Fatalf("parsing content: %v", err)
	}
	set := make(map[int]bool, len(seeds))
	for _, s := range seeds {
		set[s] = true
	}
	a, err := analyzeSeeded(context.Background(), doc, pg, ops, set)
	if err != nil {
		t.Fatalf("analyzeSeeded: %v", err)
	}
	return a
}

// seededForms are two opaque forms, objects 7 and 8: /A over the box 0 0 50
// 50, /B over bx 0 100 50.
func seededForms(bx float64) fixture {
	return fixture{
		resources: "/XObject << /A 7 0 R /B 8 0 R >>",
		objs: []string{
			stream("/Type /XObject /Subtype /Form /BBox [0 0 50 50]", "0 0 1 rg 0 0 50 50 re f"),
			stream(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [%g 0 100 50]", bx), fmt.Sprintf("0 1 0 rg %g 0 %g 50 re f", bx, 100-bx)),
		},
	}
}

func TestAnalyzeSeededClipsATranslucentUnitBetweenSeeds(t *testing.T) {
	t.Parallel()
	// The half-transparent square drawn between the two seeds is no seed,
	// and the seed after it does not overlap it. The image holds it
	// composited: drawn again over the image, it would composite twice, so
	// it is clipped out of the region.
	f := seededForms(45)
	f.content = "/A Do q /GS1 gs 1 0 0 rg 20 20 20 20 re f Q /B Do"
	a := analyzeSeeds(t, f, 0, 7)
	if got := kinds(a); got != "form* path form*" {
		t.Fatalf("units %q", got)
	}
	if len(a.Regions) != 1 {
		t.Fatalf("%d regions, want 1", len(a.Regions))
	}
	if r := a.Regions[0]; !slices.Equal(r.Removed, []int{0, 2}) || !slices.Equal(r.Clipped, []int{1}) {
		t.Errorf("removed %v clipped %v, want [0 2] [1]", r.Removed, r.Clipped)
	}
	// Drawn after the last seed, it extends the region's units the same way.
	f.content = "/A Do /B Do q /GS1 gs 1 0 0 rg 20 20 20 20 re f Q"
	a = analyzeSeeds(t, f, 0, 1)
	if r := a.Regions[0]; !slices.Equal(r.Clipped, []int{2}) || r.Last != 2 {
		t.Errorf("clipped %v last %d, want [2] 2", r.Clipped, r.Last)
	}
}

func TestAnalyzeSeededKeepsThePaintingOrder(t *testing.T) {
	t.Parallel()
	// The second square is clipped out of the region, since the seed drawn
	// after it covers it. The first square, which it overlaps, cannot stay
	// either: drawn again over the image, it would paint over what the image
	// holds of the second where they overlap (ISO 32000-2, 8.1).
	f := seededForms(35)
	f.content = "/A Do 0 g 10 10 20 20 re f 1 0 0 rg 20 20 20 20 re f /B Do"
	a := analyzeSeeds(t, f, 0, 7)
	if got := kinds(a); got != "form* path path form*" {
		t.Fatalf("units %q", got)
	}
	if r := a.Regions[0]; !slices.Equal(r.Removed, []int{0, 3}) || !slices.Equal(r.Clipped, []int{1, 2}) {
		t.Errorf("removed %v clipped %v, want [0 3] [1 2]", r.Removed, r.Clipped)
	}
	// A square nothing taken over overlaps stays where it is.
	f.content = "/A Do 0 g 2 2 5 5 re f 1 0 0 rg 20 20 20 20 re f /B Do"
	a = analyzeSeeds(t, f, 0, 7)
	if r := a.Regions[0]; !slices.Equal(r.Clipped, []int{2}) {
		t.Errorf("clipped %v, want [2]", r.Clipped)
	}
}
