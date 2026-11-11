package docx

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
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
	if frags := rect.Picture.Vector(t.Context(), compose.Rect{Width: 100, Height: 50}); len(frags) != 1 {
		t.Errorf("rectangle paints %d fragments", len(frags))
	}
	if !near(box.Style.Left, 0) || !near(box.Style.Width, 50) || box.Style.Display != css.Block || textOf(box) != "grouped" || box.Style.Padding[css.Left] != css.Pts(0) {
		t.Errorf("text box at %v, %v wide, display %v, text %q", box.Style.Left, box.Style.Width, box.Style.Display, textOf(box))
	}
	oval := paras[1].Children[0]
	if oval.Style.Position != css.Absolute || !near(oval.Style.Left, 10) || !near(oval.Style.Top, 20) || oval.Style.ZIndex.Level != -1 || paras[1].Style.Position != css.Relative {
		t.Errorf("oval position %v at %v, %v z %v", oval.Style.Position, oval.Style.Left, oval.Style.Top, oval.Style.ZIndex)
	}
	if rr := paras[2].Children[0]; rr.Style.Float != css.FloatRight {
		t.Errorf("square-wrapped shape floats %v", rr.Style.Float)
	}
	if kids := paras[4].Children; len(kids) != 2 || kids[0].Style.Position != css.Static || !near(kids[0].Style.Height, 40) {
		t.Errorf("custom shapes: %d boxes, want the room of the one in the line", len(kids)-1)
	}
	if contains(textOf(b.root), "HIDDEN") {
		t.Error("a hidden shape is drawn")
	}
}

func TestVMLShapeStyle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		xml          string
		fill, stroke bool
		width        float64
	}{
		{"defaults", `<v:rect/>`, true, true, 0.75},
		{"none", `<v:rect filled="f" stroked="false"/>`, false, false, 0},
		{"children off", `<v:rect><v:fill on="f"/><v:stroke on="f"/></v:rect>`, false, false, 0.75},
		{"weight", `<v:rect strokeweight="2pt" strokecolor="red"/>`, true, true, 2},
	} {
		root, err := ooxml.Parse(t.Context(), []byte(`<w:pict `+nsDecl+`>`+tc.xml+`</w:pict>`), nil)
		if err != nil {
			t.Fatal(err)
		}
		ss := vmlShapeStyle(root.Kids()[0])
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
