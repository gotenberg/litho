package odt

import (
	"encoding/xml"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// paths returns the paths of a page, groups' translations applied to
// their points.
func paths(pg *typeset.Page) []*typeset.Path {
	var out []*typeset.Path
	var walk func(frags []typeset.Fragment, dx, dy float64)
	walk = func(frags []typeset.Fragment, dx, dy float64) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Path:
				p := *v
				p.Ops = make([]typeset.PathOp, len(v.Ops))
				for i, op := range v.Ops {
					p.Ops[i] = op
					for j := range op.P {
						p.Ops[i].P[j].X += dx
						p.Ops[i].P[j].Y += dy
					}
				}
				out = append(out, &p)
			case *typeset.Group:
				walk(v.Content, dx+v.Transform.E, dy+v.Transform.F)
			}
		}
	}
	walk(pg.Content, 0, 0)
	return out
}

const arrowMarker = `<draw:marker draw:name="Arrow" svg:viewBox="0 0 20 30" svg:d="M10 0l-10 30h20z"/>`

// TestLineMarkers locks how Writer draws a line's stroke and markers (see
// [builder.lineDrawing], which holds the measurements): the marker's tip
// on the line's end, its base a marker's length back, the stroke ending
// there at its own width.
func TestLineMarkers(t *testing.T) {
	auto := `<style:style style:name="gr" style:family="graphic"><style:graphic-properties draw:stroke="solid" svg:stroke-color="#008000" svg:stroke-width="0.3cm" draw:marker-end="Arrow" draw:marker-end-width="1cm" style:wrap="none" style:vertical-pos="from-top" style:vertical-rel="page" style:horizontal-pos="from-left" style:horizontal-rel="page"/></style:style>`
	body := `<draw:line text:anchor-type="page" text:anchor-page-number="1" draw:style-name="gr" svg:x1="7cm" svg:y1="13cm" svg:x2="12cm" svg:y2="13cm"/><text:p/>`
	pg := convertDoc(t, doc{styles: arrowMarker, contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0]
	green := typeset.Color{R: 0, G: 128 / 255.0, B: 0, A: 1}
	var stroke, head *typeset.Path
	for _, p := range paths(pg) {
		switch {
		case p.Stroke != nil && p.Stroke.Color == green:
			stroke = p
		case p.Fill != nil && p.Fill.Color == green:
			head = p
		}
	}
	if stroke == nil || head == nil {
		t.Fatalf("stroke %v, marker %v: want both", stroke != nil, head != nil)
	}
	tip, base := 12/2.54*72, (12-1.5)/2.54*72
	if !near(head.Ops[0].P[0].X, tip, 0.05) {
		t.Errorf("marker tip at x %.2f, want %.2f", head.Ops[0].P[0].X, tip)
	}
	if end := stroke.Ops[1].P[0].X; !near(end, base, 0.05) {
		t.Errorf("stroke ends at x %.2f, want the marker's base %.2f", end, base)
	}
	if !near(stroke.Stroke.Width, 0.3/2.54*72, 0.01) || stroke.Stroke.Cap != typeset.ButtCap {
		t.Errorf("stroke %.2f wide, cap %v; want 8.50, butt", stroke.Stroke.Width, stroke.Stroke.Cap)
	}
}

// TestLineWrap locks how Writer sets text around a line of none wrap (see
// [builder.lineOf]): the line before
// the anchor paragraph stays though the marker reaches above it, and the
// anchor paragraph's text goes below the marker.
func TestLineWrap(t *testing.T) {
	auto := `<style:style style:name="gr" style:family="graphic"><style:graphic-properties draw:stroke="solid" svg:stroke-width="1cm" draw:marker-end="Arrow" draw:marker-end-width="2cm" style:wrap="none" style:vertical-pos="from-top" style:vertical-rel="paragraph" style:horizontal-pos="from-left" style:horizontal-rel="paragraph"/></style:style>`
	line := `<draw:line text:anchor-type="paragraph" draw:style-name="gr" svg:x1="2cm" svg:y1="0.9cm" svg:x2="8cm" svg:y2="0.9cm"/>`
	for _, link := range []bool{false} {
		l := line
		if link {
			l = `<draw:a xlink:href="http://example.com/" xlink:type="simple">` + line + `</draw:a>`
		}
		body := `<text:p>Before</text:p><text:p>` + l + `</text:p><text:p>After</text:p>`
		pg := convertDoc(t, doc{styles: arrowMarker, contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0]
		y := map[string]float64{}
		for _, r := range runs(pg) {
			y[strings.TrimSpace(r.text)] = r.y
		}
		// LibreOffice: Before at 67.90, After at 149.45.
		if !near(y["Before"], 67.9, 0.3) || !near(y["After"]-y["Before"], 149.45-67.9, 0.5) {
			t.Errorf("link %v: Before at %.2f, After %.2f below it; want 67.90 and 81.55", link, y["Before"], y["After"]-y["Before"])
		}
	}
}

// TestEllipseParts locks the sections, cuts and arcs of an ellipse (see
// [ellipsePart]): angles on the circle the ellipse scales, bounds of the
// part alone.
func TestEllipseParts(t *testing.T) {
	el := func(kind, start, end string) *odf.Element {
		return &odf.Element{Attr: []xml.Attr{
			{Name: xml.Name{Space: odf.NSDraw, Local: "kind"}, Value: kind},
			{Name: xml.Name{Space: odf.NSDraw, Local: "start-angle"}, Value: start},
			{Name: xml.Name{Space: odf.NSDraw, Local: "end-angle"}, Value: end},
		}}
	}
	if d, _, _ := ellipsePart(el("full", "30", "300"), 100, 60); d != "" {
		t.Errorf("a full ellipse draws %q, want the ellipse", d)
	}
	d, open, r := ellipsePart(el("section", "30", "300"), 100, 60)
	// From the centre (50, 30) to the point at 30 degrees, (50+50 cos 30,
	// 30-30 sin 30), round through the top, the left and the bottom to
	// 300 degrees: the bounds stop at the 30 degree point on the right.
	if !strings.HasPrefix(d, "M50 30L93.30127") || open {
		t.Errorf("section %q, open %v", d, open)
	}
	want := typeset.Rect{X: 0, Y: 0, Width: 50 + 50*math.Cos(30*math.Pi/180), Height: 60}
	if !near(r.X, want.X, 1e-9) || !near(r.Y, want.Y, 1e-9) || !near(r.Width, want.Width, 1e-9) || !near(r.Height, want.Height, 1e-9) {
		t.Errorf("section bounds %+v, want %+v", r, want)
	}
	if _, open, _ := ellipsePart(el("arc", "30", "300"), 100, 60); !open {
		t.Error("an arc is not open")
	}
}
