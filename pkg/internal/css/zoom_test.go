package css

import (
	"fmt"
	"strings"
	"testing"
)

// TestZoom_MultipliesLengths locks CSS Viewport 1, 4: the effective zoom
// is the product of the zooms up the tree, 0 counts as 1, a negative
// value is invalid, and every length the element computes, inherited
// ones included, is multiplied by it, while percentages are not.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 lays out each
//     case as the test expects: width: 100px under zoom: 1.5 then 2 is
//     300px, 10rem under zoom: 2 is 320px, 10em with an inherited font
//     size of 10px is 200px, calc(50% + 10px) of 400px is 220px, and an
//     inherited text-indent of 10px is 20px.
func TestZoom_MultipliesLengths(t *testing.T) {
	c := compute(t, `<div id=n style="zoom:1.5"><div id=nn style="zoom:2;width:100px"></div></div>
<div id=zero style="zoom:0;width:100px"></div>
<div id=neg-p style="zoom:2"><div id=neg style="zoom:-1;width:100px"></div></div>
<div id=pc style="zoom:50%;width:100px"></div>
<div id=rem style="zoom:2;width:10rem"></div>
<div style="font-size:10px;line-height:20px;text-indent:10px;letter-spacing:1px;border-spacing:3px"><div id=inh style="zoom:2;width:10em;font-size:150%"></div><div id=inh2 style="zoom:2"></div></div>
<div id=calc style="zoom:2;width:calc(50% + 10px);height:50%;border:thin solid"></div>
<div id=med style="zoom:2;font-size:medium"><p id=deep style="width:1px"></p></div>`, "")
	for _, tc := range []struct {
		id   string
		got  func(*Style) float64
		want float64
	}{
		{"nn", func(s *Style) float64 { return s.Width.Value }, 300 * Pt},
		{"nn", func(s *Style) float64 { return s.UsedZoom() }, 3},
		{"zero", func(s *Style) float64 { return s.Width.Value }, 100 * Pt},
		{"neg", func(s *Style) float64 { return s.Width.Value }, 200 * Pt},
		{"pc", func(s *Style) float64 { return s.Width.Value }, 50 * Pt},
		{"rem", func(s *Style) float64 { return s.Width.Value }, 320 * Pt},
		{"inh", func(s *Style) float64 { return s.FontSize }, 30 * Pt},
		{"inh", func(s *Style) float64 { return s.Width.Value }, 300 * Pt},
		{"inh2", func(s *Style) float64 { return s.FontSize }, 20 * Pt},
		{"inh2", func(s *Style) float64 { return s.LineHeight.Value }, 40 * Pt},
		{"inh2", func(s *Style) float64 { return s.TextIndent.Value }, 20 * Pt},
		{"inh2", func(s *Style) float64 { return s.LetterSpacing.Value }, 2 * Pt},
		{"inh2", func(s *Style) float64 { return s.BorderSpacing[0] }, 6 * Pt},
		{"calc", func(s *Style) float64 { return s.Width.Value }, 20 * Pt},
		{"calc", func(s *Style) float64 { return s.Width.Fraction }, 0.5},
		{"calc", func(s *Style) float64 { return s.Height.Value }, 0.5},
		{"calc", func(s *Style) float64 { return s.Border[0].Width }, 2 * Pt},
		{"med", func(s *Style) float64 { return s.FontSize }, 32 * Pt},
		{"deep", func(s *Style) float64 { return s.FontSize }, 32 * Pt},
		{"deep", func(s *Style) float64 { return s.Width.Value }, 2 * Pt},
		{"deep", func(s *Style) float64 { return s.Margin[0].Value }, 32 * Pt},
	} {
		if got := tc.got(c.of(tc.id)); !near(got, tc.want) {
			t.Errorf("#%s: %g, want %g", tc.id, got, tc.want)
		}
	}
}

// TestZoom_Bounded locks that a huge zoom, and four hundred nested zooms of 2,
// keep every length finite: the effective zoom stops at maxZoom.
func TestZoom_Bounded(t *testing.T) {
	var b strings.Builder
	for range 400 {
		b.WriteString(`<div style="zoom:2">`)
	}
	b.WriteString(`<div id=in style="width:1px"></div>`)
	c := compute(t, fmt.Sprintf(`<div id=huge style="zoom:1e30;width:1px"></div>%s`, b.String()), "")
	for _, id := range []string{"huge", "in"} {
		s := c.of(id)
		if s.UsedZoom() != maxZoom || s.Width.Value != maxZoom*Pt {
			t.Errorf("#%s: zoom %g, width %g, want %g and %g", id, s.UsedZoom(), s.Width.Value, float64(maxZoom), maxZoom*Pt)
		}
	}
}

// TestZoom_PathData locks that the data of a path() shape is in pixels
// the element's effective zoom enlarges (CSS Viewport 1, 4: the used
// value includes values inside path() strings).
func TestZoom_PathData(t *testing.T) {
	c := compute(t, `<div id=z style="zoom:2;clip-path:path('M0,0 L100,0 L0,100 Z')"></div><div id=n style="clip-path:path('M0,0 L1,0 L0,1 Z')"></div>`, "")
	if got := c.of("z").ClipPath.Shape.PathPixel(); !near(got, 2*Pt) {
		t.Errorf("zoomed path pixel: %g points, want %g", got, 2*Pt)
	}
	if got := c.of("n").ClipPath.Shape.PathPixel(); !near(got, Pt) {
		t.Errorf("path pixel: %g points, want %g", got, Pt)
	}
}
