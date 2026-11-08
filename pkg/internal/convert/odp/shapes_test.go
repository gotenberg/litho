package odp

import (
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

func TestRead_Placement(t *testing.T) {
	tests := []struct {
		name, shape  string
		box          compose.Rect
		rot          float64
		flipH, flipV bool
	}{
		{"plain", `<draw:rect svg:x="1cm" svg:y="2cm" svg:width="4cm" svg:height="2cm"/>`,
			compose.Rect{X: cm(1), Y: cm(2), Width: cm(4), Height: cm(2)}, 0, false, false},
		// Measured on the pinned LibreOffice: the box's corner at the
		// origin turns 30 degrees counterclockwise, then moves to 10 cm,
		// 5 cm; its centre lands at 346.73, 137.94 points.
		{"rotated", `<draw:rect draw:transform="rotate (0.5235987756) translate (10cm 5cm)" svg:width="4cm" svg:height="2cm"/>`,
			compose.Rect{X: 346.73 - cm(2), Y: 137.94 - cm(1), Width: cm(4), Height: cm(2)}, 330, false, false},
		{"line backwards", `<draw:line svg:x1="5cm" svg:y1="4cm" svg:x2="1cm" svg:y2="2cm"/>`,
			compose.Rect{X: cm(1), Y: cm(2), Width: cm(4), Height: cm(2)}, 0, true, true},
		// The geometry carries the mirroring (shape.Geometry.MirrorH), so the
		// shape itself is not flipped.
		{"mirrored custom shape", `<draw:custom-shape svg:x="0cm" svg:y="0cm" svg:width="1cm" svg:height="1cm"><draw:enhanced-geometry draw:type="rectangle" draw:mirror-horizontal="true"/></draw:custom-shape>`,
			compose.Rect{Width: cm(1), Height: cm(1)}, 0, false, false},
		{"scaled down", `<draw:rect draw:transform="scale (1 -1) translate (1cm 3cm)" svg:width="2cm" svg:height="2cm"/>`,
			compose.Rect{X: cm(1), Y: cm(1), Width: cm(2), Height: cm(2)}, 0, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := firstShape(t, flat{body: page(tt.shape)})
			b := s.Box
			if !near(b.X, tt.box.X) || !near(b.Y, tt.box.Y) || !near(b.Width, tt.box.Width) || !near(b.Height, tt.box.Height) {
				t.Errorf("box = %+v, want %+v", b, tt.box)
			}
			if !near(s.Rotation, tt.rot) || s.FlipH != tt.flipH || s.FlipV != tt.flipV {
				t.Errorf("rotation %v flips %v %v, want %v %v %v", s.Rotation, s.FlipH, s.FlipV, tt.rot, tt.flipH, tt.flipV)
			}
			if tt.name == "mirrored custom shape" && (s.Geometry == nil || !s.Geometry.MirrorH) {
				t.Errorf("geometry %+v, want it mirrored horizontally", s.Geometry)
			}
		})
	}
}

func TestRead_StackingAndGroups(t *testing.T) {
	body := page(`<draw:rect draw:name="top" draw:z-index="2" svg:width="1cm" svg:height="1cm"/>` +
		`<draw:rect draw:name="bottom" draw:z-index="0" svg:width="1cm" svg:height="1cm"/>` +
		`<draw:g draw:name="group" draw:z-index="1"><draw:rect svg:x="1cm" svg:y="1cm" svg:width="1cm" svg:height="1cm"/>` +
		`<draw:a xlink:href="https://example.com/"><draw:ellipse svg:x="3cm" svg:y="2cm" svg:width="1cm" svg:height="2cm"/></draw:a></draw:g>`)
	doc, _ := read(t, flat{body: body}.bytes())
	var names []string
	for _, s := range doc.Slides[0].Shapes {
		names = append(names, s.Name)
	}
	if want := []string{"bottom", "group", "top"}; !slices.Equal(names, want) {
		t.Fatalf("order = %q, want %q", names, want)
	}
	g := doc.Slides[0].Shapes[1]
	want := compose.Rect{X: cm(1), Y: cm(1), Width: cm(3), Height: cm(3)}
	if g.Kind != slides.KindGroup || len(g.Kids) != 2 || !nearRect(g.Box, want) || !nearRect(g.Child, want) {
		t.Errorf("group = %+v", g)
	}
	if h := g.Kids[1].Href; h != "https://example.com/" {
		t.Errorf("href = %q", h)
	}
}

func TestRead_Geometry(t *testing.T) {
	tests := []struct {
		name, shape string
		want        *shape.Geometry
	}{
		// A type without a path maps onto the DrawingML preset at its default
		// adjustments, as the shared geometry reader reads it.
		{"custom shape", `<draw:custom-shape svg:width="2cm" svg:height="1cm"><draw:enhanced-geometry draw:type="round-rectangle" draw:modifiers="3600 12.5"/></draw:custom-shape>`,
			&shape.Geometry{Preset: "roundRect"}},
		{"rectangle", `<draw:rect svg:width="2cm" svg:height="1cm"/>`, nil},
		{"rounded rectangle", `<draw:rect draw:corner-radius="0.25cm" svg:width="2cm" svg:height="1cm"/>`,
			&shape.Geometry{Preset: "roundRect", Adjust: []shape.Adjust{{Name: "adj", Value: 25000}}}},
		{"circle", `<draw:circle svg:width="1cm" svg:height="1cm"/>`, &shape.Geometry{Preset: "ellipse"}},
		{"section", `<draw:ellipse draw:kind="section" draw:start-angle="0" draw:end-angle="90" svg:width="1cm" svg:height="1cm"/>`,
			&shape.Geometry{Preset: "pie", Adjust: []shape.Adjust{{Name: "adj1", Value: 270 * 60000}, {Name: "adj2", Value: 0}}}},
		{"connector", `<draw:connector svg:x1="0cm" svg:y1="0cm" svg:x2="1cm" svg:y2="1cm"/>`, &shape.Geometry{Preset: "bentConnector3"}},
		{"curved connector", `<draw:connector draw:type="curve" svg:x1="0cm" svg:y1="0cm" svg:x2="1cm" svg:y2="1cm"/>`, &shape.Geometry{Preset: "curvedConnector3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := firstShape(t, flat{body: page(tt.shape)})
			switch {
			case tt.want == nil && s.Geometry != nil:
				t.Errorf("geometry = %+v, want a rectangle", s.Geometry)
			case tt.want != nil && (s.Geometry == nil || s.Geometry.Preset != tt.want.Preset || !slices.Equal(s.Geometry.Adjust, tt.want.Adjust)):
				t.Errorf("geometry = %+v, want %+v", s.Geometry, tt.want)
			}
		})
	}
}

func TestRead_Outlines(t *testing.T) {
	// The outline of each shape in a 1 cm square, at the box's corner.
	tests := []struct {
		name, shape string
		box         compose.Rect
		want        [][2]float64
		fill        bool
	}{
		{"path", `<draw:path svg:x="1cm" svg:y="1cm" svg:width="1cm" svg:height="1cm" svg:viewBox="0 0 10 10" svg:d="M0 0h10v10z"/>`,
			compose.Rect{X: cm(1), Y: cm(1), Width: cm(1), Height: cm(1)}, [][2]float64{{0, 0}, {1, 0}, {1, 1}}, true},
		{"polygon", `<draw:polygon svg:width="1cm" svg:height="1cm" svg:viewBox="0 0 100 100" draw:points="0,100 100,100 50,0"/>`,
			compose.Rect{Width: cm(1), Height: cm(1)}, [][2]float64{{0, 1}, {1, 1}, {0.5, 0}}, true},
		{"polyline", `<draw:polyline svg:width="1cm" svg:height="1cm" svg:viewBox="0 0 100 100" draw:points="0,0 100,100"/>`,
			compose.Rect{Width: cm(1), Height: cm(1)}, [][2]float64{{0, 0}, {1, 1}}, false},
		{"triangle", `<draw:regular-polygon svg:width="1cm" svg:height="1cm" draw:corners="3"/>`,
			compose.Rect{Width: cm(1), Height: cm(1)}, [][2]float64{{0.5, 0}, {1, 1}, {0, 1}}, true},
		// The route of a connector is in page coordinates in 1/100 mm.
		{"connector route", `<draw:connector svg:x1="1cm" svg:y1="1cm" svg:x2="2cm" svg:y2="2cm" svg:viewBox="0 0 1000 1500" svg:d="M1000 1000v-500h1000v1000"/>`,
			compose.Rect{X: cm(1), Y: cm(0.5), Width: cm(1), Height: cm(1)}, [][2]float64{{0, 0.5}, {0, 0}, {1, 0}, {1, 1}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := firstShape(t, flat{body: page(tt.shape)})
			if s.Geometry == nil || s.Geometry.Custom == nil {
				t.Fatalf("geometry = %+v, want a custom one", s.Geometry)
			}
			if !nearRect(s.Box, tt.box) {
				t.Errorf("box = %+v, want %+v", s.Box, tt.box)
			}
			paths := shape.Paths(s.Geometry, compose.Rect{Width: 1, Height: 1})
			if len(paths) != 1 || paths[0].Fill != tt.fill || !paths[0].Stroke {
				t.Fatalf("paths = %+v", paths)
			}
			var got [][2]float64
			for _, op := range paths[0].Ops {
				if op.Kind == compose.MoveTo || op.Kind == compose.LineTo {
					got = append(got, [2]float64{op.P[0].X, op.P[0].Y})
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("points = %v, want %v", got, tt.want)
			}
			for i := range got {
				if math.Abs(got[i][0]-tt.want[i][0]) > 1e-6 || math.Abs(got[i][1]-tt.want[i][1]) > 1e-6 {
					t.Errorf("point %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRead_Frames(t *testing.T) {
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	t.Run("picture over a metafile", func(t *testing.T) {
		s := firstShape(t, flat{
			auto: `<style:style style:name="m" style:family="graphic"><style:graphic-properties style:mirror="horizontal"/></style:style>`,
			body: page(`<draw:frame draw:style-name="m" svg:width="2cm" svg:height="1cm"><draw:image><office:binary-data>VkNMTVRG</office:binary-data></draw:image>` +
				`<draw:image><office:binary-data>` + png + `</office:binary-data></draw:image></draw:frame>`),
		})
		if s.Kind != slides.KindPicture || s.Picture == nil || !s.FlipH {
			t.Errorf("shape = %+v", s)
		}
	})
	t.Run("picture text", func(t *testing.T) {
		s := firstShape(t, flat{body: page(`<draw:frame svg:width="2cm" svg:height="1cm"><draw:image><office:binary-data>` + png + `</office:binary-data><text:p>x</text:p></draw:image></draw:frame>`)})
		if s.Kind != slides.KindGroup || len(s.Kids) != 2 || s.Kids[0].Kind != slides.KindPicture || !slices.Equal(texts(s.Kids[1].Text.Paragraphs), []string{"x"}) {
			t.Errorf("shape = %+v", s)
		}
	})
	t.Run("placeholder size from the master", func(t *testing.T) {
		f := flat{
			master: `<style:master-page style:name="Default"><draw:frame presentation:class="title" presentation:placeholder="true" svg:x="1cm" svg:y="2cm" svg:width="20cm" svg:height="3cm"><draw:text-box/></draw:frame></style:master-page>`,
			body:   page(`<draw:frame presentation:class="title"><draw:text-box><text:p>T</text:p></draw:text-box></draw:frame>`),
		}
		s := firstShape(t, f)
		if want := (compose.Rect{X: cm(1), Y: cm(2), Width: cm(20), Height: cm(3)}); !nearRect(s.Box, want) {
			t.Errorf("box = %+v, want %+v", s.Box, want)
		}
	})
	t.Run("table", func(t *testing.T) {
		s := firstShape(t, flat{
			auto: `<style:style style:name="co1" style:family="table-column"><style:table-column-properties style:column-width="3cm"/></style:style>` +
				`<style:style style:name="ro1" style:family="table-row"><style:table-row-properties style:row-height="1cm"/></style:style>` +
				`<style:style style:name="ce1" style:family="table-cell"><loext:graphic-properties draw:fill="solid" draw:fill-color="#00ff00"/><style:paragraph-properties fo:border="0.1cm solid #ff0000"/></style:style>`,
			body: page(`<draw:frame svg:width="8cm" svg:height="3cm"><table:table><table:table-column table:style-name="co1" table:number-columns-repeated="2"/><table:table-column/>` +
				`<table:table-row table:style-name="ro1"><table:table-cell table:style-name="ce1" table:number-columns-spanned="2"><text:p>A</text:p></table:table-cell><table:covered-table-cell/><table:table-cell><text:p>B</text:p></table:table-cell></table:table-row>` +
				`<table:table-row><table:table-cell table:number-columns-repeated="3"/></table:table-row></table:table></draw:frame>`),
		})
		tb := s.Table
		if s.Kind != slides.KindTable || tb == nil {
			t.Fatalf("shape = %+v", s)
		}
		if !nearAll(tb.ColumnWidths, []float64{cm(3), cm(3), cm(2)}) {
			t.Errorf("columns = %v", tb.ColumnWidths)
		}
		if len(tb.Rows) != 2 || !near(tb.Rows[0].Height, cm(1)) || !near(tb.Rows[1].Height, cm(2)) {
			t.Fatalf("rows = %+v", tb.Rows)
		}
		c := tb.Rows[0].Cells
		if c[0].ColSpan != 2 || !c[1].Covered || c[0].Fill.Kind != slides.FillSolid || !near(c[0].Borders[0].Width, cm(0.1)) {
			t.Errorf("cells = %+v", c)
		}
		if got := texts(c[2].Text.Paragraphs); !slices.Equal(got, []string{"B"}) {
			t.Errorf("text = %q", got)
		}
	})
}
