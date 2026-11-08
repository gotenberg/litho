package odp

import (
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
		{"mirrored custom shape", `<draw:custom-shape svg:x="0cm" svg:y="0cm" svg:width="1cm" svg:height="1cm"><draw:enhanced-geometry draw:type="rectangle" draw:mirror-horizontal="true"/></draw:custom-shape>`,
			compose.Rect{Width: cm(1), Height: cm(1)}, 0, true, false},
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
		{"custom shape", `<draw:custom-shape svg:width="2cm" svg:height="1cm"><draw:enhanced-geometry draw:type="round-rectangle" draw:modifiers="3600 12.5"/></draw:custom-shape>`,
			&shape.Geometry{Preset: "round-rectangle", Adjust: []shape.Adjust{{Name: "$0", Value: 3600}, {Name: "$1", Value: 12.5}}}},
		{"rectangle", `<draw:rect svg:width="2cm" svg:height="1cm"/>`, nil},
		{"rounded rectangle", `<draw:rect draw:corner-radius="0.25cm" svg:width="2cm" svg:height="1cm"/>`,
			&shape.Geometry{Preset: "roundRect", Adjust: []shape.Adjust{{Name: "adj", Value: 25000}}}},
		{"circle", `<draw:circle svg:width="1cm" svg:height="1cm"/>`, &shape.Geometry{Preset: "ellipse"}},
		{"connector", `<draw:connector svg:x1="0cm" svg:y1="0cm" svg:x2="1cm" svg:y2="1cm"/>`, &shape.Geometry{Preset: "bentConnector3"}},
		{"curved connector", `<draw:connector draw:type="curve" svg:x1="0cm" svg:y1="0cm" svg:x2="1cm" svg:y2="1cm"/>`, &shape.Geometry{Preset: "curvedConnector3"}},
		{"custom path", `<draw:path svg:width="1cm" svg:height="1cm" svg:viewBox="0 0 10 10" svg:d="M0 0L10 10"/>`, &shape.Geometry{}},
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
