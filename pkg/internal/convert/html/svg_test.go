package html

import (
	"context"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/svg"
)

// svgPage converts src on a 400 by 400 point page without margins.
func svgPage(t *testing.T, src string, fsys fstest.MapFS) *compose.Document {
	t.Helper()
	doc, err := Convert(context.Background(), strings.NewReader(src), testFonts(t), Options{
		PageWidth: 400, PageHeight: 400, Resources: fsys,
	})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return doc
}

// filled returns the page bounding boxes of the paths of page 1 filled
// with c, in paint order, group transforms applied.
func filled(doc *compose.Document, c compose.Color) []compose.Rect {
	var out []compose.Rect
	var walk func([]compose.Fragment, compose.Matrix)
	walk = func(frags []compose.Fragment, m compose.Matrix) {
		for _, f := range frags {
			switch v := f.(type) {
			case *compose.Group:
				t := v.Transform
				if t == (compose.Matrix{}) {
					t = compose.Identity
				}
				walk(v.Content, compose.Matrix{
					A: m.A*t.A + m.C*t.B, B: m.B*t.A + m.D*t.B,
					C: m.A*t.C + m.C*t.D, D: m.B*t.C + m.D*t.D,
					E: m.A*t.E + m.C*t.F + m.E, F: m.B*t.E + m.D*t.F + m.F,
				})
			case *compose.Path:
				if v.Fill == nil || v.Fill.Color != c {
					continue
				}
				x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
				for _, op := range v.Ops {
					if op.Kind == compose.ClosePath {
						continue
					}
					p := op.P[0]
					if op.Kind == compose.CurveTo {
						p = op.P[2]
					}
					x, y := m.A*p.X+m.C*p.Y+m.E, m.B*p.X+m.D*p.Y+m.F
					x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
				}
				out = append(out, compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0})
			}
		}
	}
	walk(doc.Pages[0].Content, compose.Identity)
	return out
}

var svgRed = compose.Color{R: 1, A: 1}

func rectNear(a, b compose.Rect) bool {
	const eps = 0.01
	return math.Abs(a.X-b.X) < eps && math.Abs(a.Y-b.Y) < eps && math.Abs(a.Width-b.Width) < eps && math.Abs(a.Height-b.Height) < eps
}

func TestSVG_Placement(t *testing.T) {
	full := `<rect width="100%" height="100%" fill="red"/>`
	fsys := fstest.MapFS{
		"fixed.svg":   {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">` + full + `</svg>`)},
		"ratio.svg":   {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 10">` + full + `</svg>`)},
		"nosize.svg":  {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg">` + full + `</svg>`)},
		"scaled.svg":  {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="5" height="10" fill="red"/></svg>`)},
		"sub/img.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><image href="../px.svg" width="10" height="10"/></svg>`)},
	}
	style := `<style>body{margin:0} img,svg{display:block}</style>`
	tests := []struct {
		name string
		body string
		want compose.Rect
	}{
		{"img natural size", `<img src="fixed.svg">`, compose.Rect{Width: 75, Height: 37.5}},
		{"img ratio fills the width", `<img src="ratio.svg">`, compose.Rect{Width: 400, Height: 100}},
		{"img ratio with a width", `<img src="ratio.svg" style="width:80px">`, compose.Rect{Width: 60, Height: 15}},
		{"img default size", `<img src="nosize.svg">`, compose.Rect{Width: 225, Height: 112.5}},
		{"img scaled content", `<img src="scaled.svg" style="width:20px;height:20px">`, compose.Rect{Width: 7.5, Height: 15}},
		{"img stretched without a viewBox", `<img src="scaled.svg" style="width:60px;height:40px">`, compose.Rect{Width: 22.5, Height: 30}},
		{"img data URL", `<img src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='8' height='8'%3E%3Crect width='8' height='8' fill='red'/%3E%3C/svg%3E">`, compose.Rect{Width: 6, Height: 6}},
		{"inline natural size", `<svg width="40" height="20">` + full + `</svg>`, compose.Rect{Width: 30, Height: 15}},
		{"inline percent width", `<svg width="50%" height="20">` + full + `</svg>`, compose.Rect{Width: 200, Height: 15}},
		{"inline viewBox fills the width", `<svg viewBox="0 0 4 1">` + full + `</svg>`, compose.Rect{Width: 400, Height: 100}},
		{"inline css size", `<svg width="40" height="20" style="width:20px;height:20px">` + full + `</svg>`, compose.Rect{Width: 15, Height: 15}},
		{"background", `<div style="width:40px;height:40px;background:url(fixed.svg) no-repeat"></div>`, compose.Rect{Width: 75, Height: 37.5}},
		{"background size", `<div style="width:40px;height:40px;background:url(ratio.svg) no-repeat;background-size:contain"></div>`, compose.Rect{Width: 30, Height: 7.5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := svgPage(t, style+tt.body, fsys)
			got := filled(doc, svgRed)
			if len(got) != 1 || !rectNear(got[0], tt.want) {
				t.Errorf("red paths %v, want one at %v", got, tt.want)
			}
		})
	}
}

func TestSVG_InlineInheritsTheDocument(t *testing.T) {
	src := `<style>body{margin:0;color:red} .icon .a{fill:currentColor} svg{display:block}</style>` +
		`<div class="icon"><svg width="10" height="10"><rect class="a" width="10" height="10" fill="blue"/></svg></div>`
	got := filled(svgPage(t, src, nil), svgRed)
	if len(got) != 1 || !rectNear(got[0], compose.Rect{Width: 7.5, Height: 7.5}) {
		t.Errorf("red paths %v, want the rect in the document's colour", got)
	}
}

func TestSVG_NestedSVGImageRefused(t *testing.T) {
	fsys := fstest.MapFS{
		"a.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><image href="a.svg" width="10" height="10"/><rect width="5" height="5" fill="red"/></svg>`)},
	}
	doc := svgPage(t, `<img src="a.svg">`, fsys)
	if got := filled(doc, svgRed); len(got) != 1 {
		t.Errorf("red paths %v, want the rect", got)
	}
	if !hasWarning(doc.Warnings, "svg-image", "inside an SVG image") {
		t.Errorf("warnings %+v, want the nested image refused", doc.Warnings)
	}
}

func TestSVG_UnsupportedFeaturesWarn(t *testing.T) {
	src := `<svg width="10" height="10"><filter id="f"/><rect width="10" height="10" fill="red" filter="url(#f)"/></svg>`
	doc := svgPage(t, src, nil)
	if !hasWarning(doc.Warnings, "unsupported-svg-property", "filter") {
		t.Errorf("warnings %+v", doc.Warnings)
	}
	if got := filled(doc, svgRed); len(got) != 1 {
		t.Errorf("red paths %v, want the unfiltered rect", got)
	}
}

func TestSVG_DepthFitsTheLayout(t *testing.T) {
	// The layout keeps room for the deepest drawing the svg package
	// returns.
	if svg.MaxGroupDepth > layout.MaxVectorDepth {
		t.Errorf("svg.MaxGroupDepth %d > layout.MaxVectorDepth %d", svg.MaxGroupDepth, layout.MaxVectorDepth)
	}
}
