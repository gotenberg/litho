package html

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// redCentre returns the centre, in CSS pixels on the page, of the first
// red rectangle under frags, each group's transform applied.
func redCentre(frags []typeset.Fragment, m typeset.Matrix) (float64, float64, bool) {
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Group:
			gm := v.Transform
			if gm == (typeset.Matrix{}) {
				gm = typeset.Identity
			}
			// The group's matrix maps its content into m's frame.
			c := typeset.Matrix{
				A: gm.A*m.A + gm.B*m.C, B: gm.A*m.B + gm.B*m.D,
				C: gm.C*m.A + gm.D*m.C, D: gm.C*m.B + gm.D*m.D,
				E: gm.E*m.A + gm.F*m.C + m.E, F: gm.E*m.B + gm.F*m.D + m.F,
			}
			if x, y, ok := redCentre(v.Content, c); ok {
				return x, y, true
			}
		case *typeset.Path:
			if v.Fill == nil || v.Fill.Color != (typeset.Color{R: 1, A: 1}) || len(v.Ops) < 3 {
				continue
			}
			var sx, sy float64
			n := 0
			for _, op := range v.Ops {
				if op.Kind == typeset.MoveTo || op.Kind == typeset.LineTo {
					p := op.P[0]
					sx += m.A*p.X + m.C*p.Y + m.E
					sy += m.B*p.X + m.D*p.Y + m.F
					n++
				}
			}
			return sx / float64(n) / css.Pt, sy / float64(n) / css.Pt, true
		}
	}
	return 0, 0, false
}

// An offset-path url() names an SVG shape element of the document: the
// box moves along the shape's own geometry, its transform left out, laid
// from the box's top left as a path() is (CSS Motion Path 1, 2.1); a url
// that names no shape is path("m 0 0"), with no warning.
func TestOffsetPath_URLNamesAShape(t *testing.T) {
	const svg = `<svg width="0" height="0" style="position:absolute"><path id="p" d="M 20 20 L 220 20"/>` +
		`<circle id="c" cx="100" cy="100" r="50" transform="translate(30 0)"/><g transform="translate(50 50)"><rect id="r" x="10" y="10" width="100" height="60"/></g></svg>`
	for name, tc := range map[string]struct {
		path string
		x, y float64
	}{
		"path":            {"url(#p) 50%", 120, 20},
		"circle":          {"url(#c) 25%", 100, 150},
		"rect in a group": {"url(#r) 40%", 110, 38},
		"no such shape":   {"url(#nope) 50%", 0, 0},
		"not a shape":     {"url(#t) 50%", 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			src := `<!doctype html><style>@page{margin:0}body{margin:0}#t{width:20px;height:20px;background:red;offset:` + tc.path + `}</style><body>` + svg + `<div id="t"></div>`
			doc := convertDoc(t, src, Options{})
			x, y, ok := redCentre(doc.Pages[0].Content, typeset.Identity)
			if !ok {
				t.Fatal("no red box")
			}
			if math.Abs(x-tc.x) > 0.01 || math.Abs(y-tc.y) > 0.01 {
				t.Errorf("box centre (%.2f, %.2f), want (%v, %v)", x, y, tc.x, tc.y)
			}
			for _, w := range doc.Warnings {
				if w.Code == warn.ContentUnsupported {
					t.Errorf("warning %v", w)
				}
			}
		})
	}
}
