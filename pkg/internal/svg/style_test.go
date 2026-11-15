package svg

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestStyle_PropertiesTheCascadeKnows locks that an HTML style sheet
// holding a property this package reads warns of nothing: the cascade of
// HTML elements either reads it too or lists it as an SVG property.
func TestStyle_PropertiesTheCascadeKnows(t *testing.T) {
	t.Parallel()
	names := []string{"marker", "mix-blend-mode", "isolation", "background-color"}
	for n := range presentationAttrs {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		sheet, err := css.Parse(context.Background(), []byte("rect { "+n+": inherit }"), css.ParseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range sheet.Warnings() {
			if w.Code == warn.CSSUnsupported {
				t.Errorf("%s: %s", n, w.Message)
			}
		}
	}
}

// strokes returns the strokes of the paths in frags, at any depth.
func strokes(frags []typeset.Fragment) []*typeset.Stroke {
	var out []*typeset.Stroke
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Group:
			out = append(out, strokes(v.Content)...)
		case *typeset.Path:
			if v.Stroke != nil {
				out = append(out, v.Stroke)
			}
		}
	}
	return out
}

// TestShapeRendering_KeepsTheStatedStroke reads shape-rendering as the
// hint SVG 2, 13.10.1 makes it: every value draws a stroke at its stated
// width and opacity, a line of width 0 being what ISO 32000-1, 8.4.3.2
// advises against, and an unknown value is refused.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 writes a stroke of 1, 1.3 and 1.33 CSS
//     pixels with butt caps and miter joins under crispEdges or
//     optimizeSpeed as 0 w, its alpha multiplied by its width in points
//     (0.75, 0.975, 0.9975), and every other stroke as it is; its screen
//     draws each at its stated width, without antialiasing.
func TestShapeRendering_KeepsTheStatedStroke(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"auto", "crispEdges", "optimizeSpeed", "geometricPrecision"} {
		d := parse(t, `<svg width="100" height="100"><line x1="0" y1="10" x2="100" y2="10" stroke="blue" stroke-width="1" stroke-opacity="0.5" shape-rendering="`+v+`"/></svg>`)
		s := strokes(d.Draw(context.Background(), typeset.Rect{Width: 75, Height: 75}))
		if len(s) != 1 {
			t.Fatalf("%s: %d strokes", v, len(s))
		}
		if math.Abs(s[0].Width-1) > 1e-9 || math.Abs(s[0].Color.A-0.5) > 1e-9 {
			t.Errorf("%s: width %v alpha %v, want the stated 1 and 0.5", v, s[0].Width, s[0].Color.A)
		}
		if len(d.Warnings()) != 0 {
			t.Errorf("%s: warnings %+v, want none", v, d.Warnings())
		}
	}
	var st style
	if st.apply(declaration{prop: "shape-rendering", value: "sharp"}, &style{}) {
		t.Errorf("an unknown shape-rendering value was accepted")
	}
}
