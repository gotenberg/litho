package docx

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// near reports whether a length is in points and within a hundredth of
// a point of want.
func near(d css.Dimension, want float64) bool {
	return d.Kind == css.Points && math.Abs(d.Value-want) < 0.01
}

func TestVML_Shapes(t *testing.T) {
	t.Parallel()
	pict := func(s string) string {
		return `<w:p><w:r><w:pict>` + s + `</w:pict></w:r><w:r><w:t>text</w:t></w:r></w:p>`
	}
	group := `<v:group style="width:200pt;height:100pt" coordorigin="100,0" coordsize="2000,1000">` +
		`<v:rect style="position:absolute;left:600;top:500;width:1000;height:500" fillcolor="#396" stroked="f"><v:fill opacity="32768f"/></v:rect>` +
		`<v:shape style="position:absolute;left:100;top:0;width:500;height:500" type="#_x0000_t202" filled="f"><v:textbox inset="0,0,0,0"><w:txbxContent><w:p><w:r><w:t>grouped</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape>` +
		`</v:group>`
	shapetype := `<v:shapetype id="_x0000_t202" coordsize="21600,21600" o:spt="202" path="m,l,21600r21600,l21600,xe"/>`
	floating := `<v:oval style="position:absolute;margin-left:10pt;margin-top:20pt;width:30pt;height:40pt;z-index:-1"/>`
	square := `<v:roundrect style="position:absolute;margin-left:0;margin-top:0;width:30pt;height:40pt;mso-position-horizontal:right"><w10:wrap type="square"/></v:roundrect>`
	hidden := `<v:rect style="width:30pt;height:40pt;visibility:hidden"><v:textbox><w:txbxContent><w:p><w:r><w:t>HIDDEN</w:t></w:r></w:p></w:txbxContent></v:textbox></v:rect>`
	custom := `<v:shape style="position:absolute;margin-left:0;margin-top:300pt;width:30pt;height:40pt" path="m,l,21600r21600,l21600,xe"/>` +
		`<v:shape style="width:30pt;height:40pt" path="m,l,21600r21600,l21600,xe"/>`
	b := build(t, pict(shapetype+group)+pict(floating)+pict(square)+pict(hidden)+pict(custom), nil)
	paras := b.root.Children[0].Children
	g := paras[0].Children[0]
	if !near(g.Style.Width, 200) || !near(g.Style.Height, 100) || len(g.Children) != 2 {
		t.Fatalf("group %v x %v with %d shapes", g.Style.Width, g.Style.Height, len(g.Children))
	}
	rect, box := g.Children[0], g.Children[1]
	if rect.Kind != layout.Replaced || !near(rect.Style.Left, 50) || !near(rect.Style.Top, 50) || !near(rect.Style.Width, 100) || !near(rect.Style.Height, 50) || rect.Style.Position != css.Absolute {
		t.Errorf("rectangle at %v, %v, %v x %v", rect.Style.Left, rect.Style.Top, rect.Style.Width, rect.Style.Height)
	}
	if frags := rect.Picture.Vector(t.Context(), typeset.Rect{Width: 100, Height: 50}); len(frags) != 1 {
		t.Errorf("rectangle paints %d fragments", len(frags))
	}
	if !near(box.Style.Left, 0) || !near(box.Style.Width, 50) || box.Style.Display != css.Block || textOf(box) != "grouped" || box.Style.Padding[css.Left] != css.Pts(0) {
		t.Errorf("text box at %v, %v wide, display %v, text %q", box.Style.Left, box.Style.Width, box.Style.Display, textOf(box))
	}
	oval := paras[1].Children[0]
	if oval.Style.Position != css.Absolute || !near(oval.Style.Left, 10) || !near(oval.Style.Top, 20) || oval.Style.ZIndex.Level != -1 || paras[1].Style.Position != css.Relative {
		t.Errorf("oval position %v at %v, %v z %v", oval.Style.Position, oval.Style.Left, oval.Style.Top, oval.Style.ZIndex)
	}
	if rr := paras[2].Children[0].Style; rr.Float == css.FloatNone || !rr.WordAnchor.Set || !near(css.Pts(rr.WordAnchor.X), 468-30) {
		t.Errorf("square-wrapped shape floats %v at %+v", rr.Float, rr.WordAnchor)
	}
	// Both shapes of their own path (ECMA-376 Part 4, 14.1.2.19 path) are
	// drawn: the floating one placed, the other in the line.
	if kids := paras[4].Children; len(kids) != 3 || kids[0].Style.Position != css.Absolute || kids[1].Kind != layout.Replaced || !near(kids[1].Style.Height, 40) {
		t.Errorf("custom shapes: %d boxes", len(kids)-1)
	} else if frags := kids[1].Picture.Vector(t.Context(), typeset.Rect{Width: 30, Height: 40}); len(frags) == 0 {
		t.Error("a shape of its own path paints nothing")
	}
	if len(b.d.warnings) > 0 {
		t.Errorf("warnings %+v", b.d.warnings)
	}
	if contains(textOf(b.root), "HIDDEN") {
		t.Error("a hidden shape is drawn")
	}
}

// TestVMLShapeStyle locks the fill and stroke of a VML shape, which takes
// what it leaves unsaid from its shape type (ECMA-376 Part 4, 14.1.2.20).
func TestVMLShapeStyle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		xml, typ     string
		fill, stroke bool
		width        float64
	}{
		{"defaults", `<v:rect/>`, "", true, true, 0.75},
		{"none", `<v:rect filled="f" stroked="false"/>`, "", false, false, 0},
		{"children off", `<v:rect><v:fill on="f"/><v:stroke on="f"/></v:rect>`, "", false, false, 0.75},
		{"weight", `<v:rect strokeweight="2pt" strokecolor="red"/>`, "", true, true, 2},
		{"type off", `<v:shape/>`, `<v:shapetype filled="f" stroked="f"/>`, false, false, 0},
		{"shape over type", `<v:shape filled="t"/>`, `<v:shapetype filled="f" strokeweight="3pt"/>`, true, true, 3},
		{"type child", `<v:shape/>`, `<v:shapetype><v:stroke on="f"/></v:shapetype>`, true, false, 0},
	} {
		root, err := ooxml.Parse(t.Context(), []byte(`<w:pict `+nsDecl+`>`+tc.xml+tc.typ+`</w:pict>`), nil)
		if err != nil {
			t.Fatal(err)
		}
		var typ *ooxml.Element
		if tc.typ != "" {
			typ = root.Kids()[1]
		}
		ss := vmlShapeStyle(root.Kids()[0], typ)
		if (ss.Fill != nil) != tc.fill || (ss.Line != nil) != tc.stroke || (tc.stroke && ss.LineWidth != tc.width) {
			t.Errorf("%s: fill %v stroke %v width %v", tc.name, ss.Fill, ss.Line, ss.LineWidth)
		}
	}
}

func TestVMLFlow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ style, want string }{
		{"", ""},
		{"layout-flow:vertical", "tbRl"},
		{"layout-flow:vertical;mso-layout-flow-alt:bottom-to-top", "btLr"},
		{"layout-flow:vertical-ideographic", "tbRl"},
	} {
		if got := vmlFlow(ooxml.ParseVMLStyle(tc.style)); got != tc.want {
			t.Errorf("vmlFlow(%q) = %q, want %q", tc.style, got, tc.want)
		}
	}
}

// TestVML_PictureAlt locks what a reader hears of a VML picture: its
// shape's alt, else its image's o:title.
//
// # Corpus evidence
//
//   - litho/docx: shapes-vml's picture titled "vml picture" in o:title was
//     a Figure without a description.
func TestVML_PictureAlt(t *testing.T) {
	t.Parallel()
	pic := func(shapeAttrs, title string) string {
		return `<w:p><w:r><w:pict><v:shape style="width:30pt;height:20pt"` + shapeAttrs + `><v:imagedata r:id="rIda" o:title="` + title + `"/></v:shape></w:pict></w:r></w:p>`
	}
	b := build(t, pic(` alt="The alt"`, "The title")+pic("", "The title"), map[string]string{"word/media/a.png": pngData(t, 4, 4)})
	var got []string
	for _, n := range find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced }) {
		got = append(got, n.Role+":"+n.Alt)
	}
	if len(got) != 2 || got[0] != "Figure:The alt" || got[1] != "Figure:The title" {
		t.Errorf("pictures %q", got)
	}
}

// TestVML_RotatedTextBox locks that a turned VML text box turns with its
// text, laid out left to right whatever its layout-flow, and that an
// unturned one keeps its flow.
//
// # Corpus evidence
//
//   - libreoffice/docx: ooxmlexport/TextFrameRotation.docx prints the text
//     of its text box turned by rotation:-90 and of layout-flow vertical
//     bottom-to-top reading from the bottom up, as a left-to-right line
//     turned a quarter counter-clockwise.
func TestVML_RotatedTextBox(t *testing.T) {
	t.Parallel()
	box := func(rot string) string {
		return `<w:p><w:r><w:pict><v:rect style="position:absolute;margin-left:0;margin-top:0;width:200pt;height:40pt` + rot + `">` +
			`<v:textbox style="layout-flow:vertical;mso-layout-flow-alt:bottom-to-top"><w:txbxContent><w:p><w:r><w:t>turned</w:t></w:r></w:p></w:txbxContent></v:textbox></v:rect></w:pict></w:r></w:p>`
	}
	b := build(t, box(";rotation:-90")+box(""), nil)
	paras := b.root.Children[0].Children
	turned, flat := paras[0].Children[0], paras[1].Children[0]
	if tr := turned.Style.Transform; len(tr) != 1 || tr[0].Kind != css.TransformRotate || !near(css.Pts(tr[0].V[0]*180/math.Pi), -90) {
		t.Errorf("turned box transform %+v, want one rotation of -90 degrees", tr)
	}
	if turned.Style.WritingMode != (css.Style{}).WritingMode {
		t.Errorf("turned box writing mode %v, want horizontal", turned.Style.WritingMode)
	}
	if flat.Style.WritingMode == (css.Style{}).WritingMode || len(flat.Style.Transform) != 0 {
		t.Errorf("unturned box writing mode %v, transform %+v, want its vertical flow and no turn", flat.Style.WritingMode, flat.Style.Transform)
	}
}

// TestVML_LineFromItsEndPoints places a line positioned in the text at
// the nearer of its end points (ECMA-376 Part 4, 14.1.2.12 from and to),
// its box as large as the distance between them.
func TestVML_LineFromItsEndPoints(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:pict><v:line style="position:absolute;margin-left:10pt" from="330pt,56pt" to="420pt,6pt" strokecolor="red"/></w:pict></w:r><w:r><w:t>x</w:t></w:r></w:p>`
	lines := find(build(t, body, nil).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(lines) != 1 {
		t.Fatalf("%d lines", len(lines))
	}
	st := lines[0].Style
	if !near(st.Left, 340) || !near(st.Top, 6) || !near(st.Width, 90) || !near(st.Height, 50) {
		t.Errorf("line at %v, %v, %v by %v; want 340, 6, 90 by 50", st.Left, st.Top, st.Width, st.Height)
	}
}

// TestVMLGeometry_OwnPathOverType draws a v:shape of a path of its own by
// that path whatever preset its o:spt maps to, and one of no path of its
// own by its type's preset.
func TestVMLGeometry_OwnPathOverType(t *testing.T) {
	t.Parallel()
	d := &document{vmlTypes: map[string]int{}}
	for _, tc := range []struct{ xml, want string }{
		{`<v:shape o:spt="100" path="ar,0,10,10,0,0,10,10e"/>`, ""},
		{`<v:shape o:spt="100"><v:path v="m,l10,10e"/></v:shape>`, ""},
		{`<v:shape o:spt="100"/>`, "pie"},
	} {
		root, err := ooxml.Parse(t.Context(), []byte(`<w:pict `+nsDecl+`>`+tc.xml+`</w:pict>`), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := d.vmlGeometry(root.Kids()[0]); got != tc.want {
			t.Errorf("%s: geometry %q, want %q", tc.xml, got, tc.want)
		}
	}
}

// TestVML_ArcSize rounds a v:roundrect's corners by its arcsize, a
// fraction of its shorter side (Part 4, 14.1.2.17), 0.2 when absent, as
// the roundRect preset's adj in 1/100,000 of that side.
func TestVML_ArcSize(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		attr string
		want float64
	}{{``, 20000}, {` arcsize="2637f"`, 2637.0 / 65536 * 100000}, {` arcsize="10%"`, 10000}, {` arcsize="0.1"`, 10000}, {` arcsize="50%"`, 50000}, {` arcsize="16384f"`, 25000}, {` arcsize="0.9"`, 50000}} {
		root, err := ooxml.Parse(t.Context(), []byte(`<w:pict `+nsDecl+`><v:roundrect`+tc.attr+`/></w:pict>`), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := arcSize(root.Kids()[0]); math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("%q: adj %v, want %v", tc.attr, got, tc.want)
		}
	}
}

// TestVML_RelativeSize sizes a VML shape whose style gives a percentage of
// an area, in tenths of a percent, as that share of the area: the page
// when it names none (MS-OI29500, 2.1.1394), in a header as in the body.
func TestVML_RelativeSize(t *testing.T) {
	t.Parallel()
	rect := `<w:p><w:r><w:pict><v:rect style="position:absolute;width:100pt;height:50pt;mso-width-percent:500;mso-height-percent:250;mso-height-relative:margin"/></w:pict></w:r></w:p>`
	// The default page is Letter, 612 by 792 points, with 72-point margins.
	b := build(t, rect, nil)
	shapes := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(shapes) != 1 || !near(shapes[0].Style.Width, 306) || !near(shapes[0].Style.Height, 162) {
		t.Fatalf("body shape %d, want 306 by 162 points", len(shapes))
	}
	b = build(t, `<w:p/><w:sectPr><w:headerReference w:type="default" r:id="rIdheader1"/></w:sectPr>`, map[string]string{
		"word/header1.xml": `<w:hdr ` + nsDecl + `>` + rect + `</w:hdr>`,
	})
	p := b.d.newPages(b.secs)
	n, _ := p.content(b.secs[0], b.secs[0].headers["default"], &fieldValues{})
	shapes = find(n, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(shapes) != 1 || !near(shapes[0].Style.Width, 306) || !near(shapes[0].Style.Height, 162) {
		t.Errorf("header shape %d, want 306 by 162 points", len(shapes))
	}
}
