package odf

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// geometryXML wraps an enhanced geometry's attributes and equations.
func geometryXML(attrs, kids string) string {
	return `<draw:enhanced-geometry ` + ns + ` xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" xmlns:drawooo="http://openoffice.org/2010/draw" ` + attrs + `>` + kids + `</draw:enhanced-geometry>`
}

// parseGeometryXML parses s and reads its geometry.
func parseGeometryXML(t testing.TB, s string) *shape.Geometry {
	t.Helper()
	el, err := ParseXML(context.Background(), []byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return ParseGeometry(el)
}

// ends returns the end points of a path's operations.
func ends(ops []compose.PathOp) []compose.Point {
	var out []compose.Point
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo, compose.LineTo:
			out = append(out, op.P[0])
		case compose.CurveTo:
			out = append(out, op.P[2])
		case compose.ClosePath:
		}
	}
	return out
}

func TestParseGeometry_Enhanced(t *testing.T) {
	box := compose.Rect{X: 0, Y: 0, Width: 100, Height: 50}
	tests := []struct {
		name   string
		xml    string
		preset string
		want   [][]compose.Point
		fill   []bool
		stroke []bool
	}{
		{
			name: "lines in the default view",
			xml:  geometryXML(`draw:enhanced-path="M 0 0 L 21600 0 21600 21600 Z N"`, ""),
			want: [][]compose.Point{{{0, 0}, {100, 0}, {100, 50}}},
		},
		{
			name: "equations, modifiers and a view",
			xml: geometryXML(`svg:viewBox="0 0 200 100" draw:modifiers="50" draw:enhanced-path="M ?f0 0 L ?f1 ?f2"`,
				`<draw:equation draw:name="f0" draw:formula="$0 * 2"/><draw:equation draw:name="f1" draw:formula="right - ?f0"/><draw:equation draw:name="f2" draw:formula="if(-1, 0, height/2) + max(1, 2) - abs(-2)"/>`),
			want: [][]compose.Point{{{50, 0}, {50, 25}}},
		},
		{
			name:   "paths cut at N with F and S",
			xml:    geometryXML(`svg:viewBox="0 0 10 10" draw:enhanced-path="M 0 0 L 10 10 Z F N M 0 10 L 10 0 Z S N"`, ""),
			want:   [][]compose.Point{{{0, 0}, {100, 50}}, {{0, 50}, {100, 0}}},
			fill:   []bool{false, true},
			stroke: []bool{true, false},
		},
		{
			name: "a full angle ellipse",
			xml:  geometryXML(`draw:type="ellipse" draw:enhanced-path="U 10800 10800 10800 10800 0 360 Z N"`, ""),
			// From angle 0 counter-clockwise: right, top, left, bottom.
			want:   [][]compose.Point{{{100, 25}, {50, 0}, {0, 25}, {50, 50}, {100, 25}}},
			preset: "ellipse",
		},
		{
			name: "an arc counter-clockwise from right to left",
			xml:  geometryXML(`svg:viewBox="0 0 20 20" draw:enhanced-path="B 0 0 20 20 20 10 0 10 N"`, ""),
			want: [][]compose.Point{{{100, 25}, {50, 0}, {0, 25}}},
		},
		{
			name: "a quadrant pair",
			xml:  geometryXML(`svg:viewBox="0 0 20 20" draw:enhanced-path="M 10 0 X 0 10 Y 10 20 N"`, ""),
			want: [][]compose.Point{{{50, 0}, {0, 25}, {50, 50}}},
		},
		{
			name: "LibreOffice's extension path with G, in an empty view",
			xml: geometryXML(`svg:viewBox="0 0 0 0" draw:type="ooxml-arc" draw:enhanced-path="M 0 0 Z N" drawooo:enhanced-path="M 0 ?f0 G ?f1 ?f0 180 180 N"`,
				`<draw:equation draw:name="f0" draw:formula="logheight"/><draw:equation draw:name="f1" draw:formula="logwidth/2"/>`),
			want:   [][]compose.Point{{{0, 50}, {50, 0}, {100, 50}}},
			preset: "arc",
		},
		{
			name: "commands against numbers and sub-view sizes",
			xml:  geometryXML(`drawooo:sub-view-size="10 10" draw:enhanced-path="M0 0L10 5N"`, ""),
			want: [][]compose.Point{{{0, 0}, {100, 25}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := parseGeometryXML(t, tt.xml)
			if g == nil || g.Custom == nil {
				t.Fatalf("no custom geometry: %+v", g)
			}
			if err := shape.Check(g); err != nil {
				t.Fatal(err)
			}
			if g.Preset != tt.preset {
				t.Errorf("preset %q, want %q", g.Preset, tt.preset)
			}
			paths := shape.Paths(g, box)
			if len(paths) != len(tt.want) {
				t.Fatalf("%d paths, want %d", len(paths), len(tt.want))
			}
			for i, p := range paths {
				got := ends(p.Ops)
				if len(got) != len(tt.want[i]) {
					t.Fatalf("path %d: %v, want %v", i, got, tt.want[i])
				}
				for j := range got {
					if math.Abs(got[j].X-tt.want[i][j].X) > 1e-6 || math.Abs(got[j].Y-tt.want[i][j].Y) > 1e-6 {
						t.Errorf("path %d point %d: %v, want %v", i, j, got[j], tt.want[i][j])
					}
				}
				if tt.fill != nil && (p.Fill != tt.fill[i] || p.Stroke != tt.stroke[i]) {
					t.Errorf("path %d: fill %v stroke %v", i, p.Fill, p.Stroke)
				}
			}
		})
	}
}

func TestParseGeometry_Types(t *testing.T) {
	tests := []struct {
		xml     string
		preset  string
		mirrorH bool
	}{
		{geometryXML(`draw:type="round-rectangle"`, ""), "roundRect", false},
		{geometryXML(`draw:type="ooxml-star5" draw:mirror-horizontal="true"`, ""), "star5", true},
		{geometryXML(`draw:type="mso-spt100"`, ""), "pie", false},
		{geometryXML(`draw:type="col-123"`, ""), "col-123", false},
	}
	for _, tt := range tests {
		g := parseGeometryXML(t, tt.xml)
		if g.Preset != tt.preset || g.Custom != nil || g.MirrorH != tt.mirrorH {
			t.Errorf("%s: %+v", tt.xml, g)
		}
	}
	if ParseGeometry(nil) != nil {
		t.Error("a nil element has a geometry")
	}
}

func TestParseGeometry_Problems(t *testing.T) {
	for _, s := range []string{
		geometryXML(`draw:enhanced-path="M ?nope 0 L $3 0"`, ""),
		geometryXML(`draw:enhanced-path="M ?f0 0"`, `<draw:equation draw:name="f0" draw:formula="*sqrt(2)"/>`),
		geometryXML(`draw:enhanced-path="M ?f0 0"`, `<draw:equation draw:name="f0" draw:formula="?f1"/><draw:equation draw:name="f1" draw:formula="?f0"/>`),
	} {
		if err := shape.Check(parseGeometryXML(t, s)); err == nil {
			t.Errorf("%s: no problem reported", s)
		}
	}
}

// TestParseGeometry_OpenSubpaths checks a LibreOffice shape's open
// subpath is stroked, not filled, and a DrawingML one's filled.
func TestParseGeometry_OpenSubpaths(t *testing.T) {
	box := compose.Rect{Width: 10, Height: 10}
	own := shape.Paths(parseGeometryXML(t, geometryXML(`draw:type="bracket-pair" draw:enhanced-path="M 0 0 L 21600 0 21600 21600 N"`, "")), box)
	if len(own) != 1 || own[0].Fill || !own[0].Stroke {
		t.Errorf("LibreOffice shape: %+v", own)
	}
	dml := shape.Paths(parseGeometryXML(t, geometryXML(`draw:type="ooxml-rect" draw:enhanced-path="M 0 0 L 21600 0 21600 21600 N"`, "")), box)
	if len(dml) != 1 || !dml[0].Fill {
		t.Errorf("DrawingML shape: %+v", dml)
	}
}
