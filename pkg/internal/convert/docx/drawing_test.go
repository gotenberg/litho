package docx

import (
	"bytes"
	"image"
	"image/png"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// pngData returns a w by h PNG.
func pngData(t testing.TB, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// graphic returns a picture graphic of the image relationship.
func graphic(crop string) string {
	return `<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>` +
		`<pic:nvPicPr><pic:cNvPr id="1" name="a.png" descr="inner"/><pic:cNvPicPr/></pic:nvPicPr>` +
		`<pic:blipFill><a:blip r:embed="rIda"/>` + crop + `</pic:blipFill>` +
		`<pic:spPr><a:xfrm rot="5400000" flipH="1"><a:off x="0" y="0"/><a:ext cx="1270000" cy="635000"/></a:xfrm></pic:spPr></pic:pic></a:graphicData></a:graphic>`
}

func TestDrawings(t *testing.T) {
	t.Parallel()
	inline := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="1" name="p" descr="A chart"/>` + graphic(`<a:srcRect l="25000" r="25000"/>`) + `</wp:inline></w:drawing></w:r></w:p>`
	anchor := func(wrap, pos string) string {
		return `<w:p><w:r><w:drawing><wp:anchor behindDoc="0"><wp:positionH relativeFrom="column">` + pos + `</wp:positionH>` +
			`<wp:positionV relativeFrom="paragraph"><wp:posOffset>127000</wp:posOffset></wp:positionV><wp:extent cx="1270000" cy="1270000"/>` + wrap +
			`<wp:docPr id="2" name="q"/>` + graphic("") + `</wp:anchor></w:drawing></w:r><w:r><w:t>text</w:t></w:r></w:p>`
	}
	shape := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="3" name="s"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
		`<wps:wsp><wps:spPr><a:prstGeom prst="ellipse"/><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></wps:spPr><wps:bodyPr/></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	textBox := `<w:p><w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
		`<wps:wsp><wps:spPr><a:prstGeom prst="rect"/><a:ln w="12700"><a:solidFill><a:srgbClr val="0000FF"/></a:solidFill></a:ln></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>boxed</w:t></w:r></w:p></w:txbxContent></wps:txbx><wps:bodyPr/></wps:wsp>` +
		`</a:graphicData></a:graphic></wp:inline></w:drawing></mc:Choice><mc:Fallback><w:pict><w:r><w:t>FALLBACK</w:t></w:r></w:pict></mc:Fallback></mc:AlternateContent></w:r></w:p>`
	body := inline + anchor(`<wp:wrapSquare wrapText="bothSides"/>`, `<wp:align>right</wp:align>`) +
		anchor(`<wp:wrapTopAndBottom/>`, `<wp:align>center</wp:align>`) + anchor(`<wp:wrapNone/>`, `<wp:posOffset>254000</wp:posOffset>`) + shape + textBox +
		anchor(`<wp:wrapNone/>`, ``)
	b := build(t, body, map[string]string{"word/media/a.png": pngData(t, 40, 20)})
	pics := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(pics) != 6 {
		t.Fatalf("%d replaced boxes, want 6", len(pics))
	}
	p := pics[0]
	if p.Role != "Figure" || p.Alt != "A chart" || p.Style.Width != css.Pts(100) || p.Style.Height != css.Pts(50) || p.Picture.Vector == nil || len(p.Style.Transform) != 2 {
		t.Errorf("inline picture role %q alt %q size %v x %v vector %v transform %v", p.Role, p.Alt, p.Style.Width, p.Style.Height, p.Picture.Vector != nil, p.Style.Transform)
	}
	if f := pics[1].Style; f.Float != css.FloatRight || f.Display != css.Block {
		t.Errorf("square wrap float %v display %v", f.Float, f.Display)
	}
	if f := pics[2].Style; f.Display != css.Block || f.Margin[css.Left].Kind != css.Auto || f.Margin[css.Right].Kind != css.Auto {
		t.Errorf("top and bottom wrap display %v margins %+v", f.Display, f.Margin)
	}
	if f := pics[3].Style; f.Position != css.Absolute || f.Left != css.Pts(20) || f.Top != css.Pts(10) {
		t.Errorf("no wrap position %v left %v top %v", f.Position, f.Left, f.Top)
	}
	paras := role(b.root, "P")
	if paras[3].Style.Position != css.Relative {
		t.Error("the paragraph of an absolutely positioned drawing is not its containing block")
	}
	if pics[4].Picture.Vector == nil || pics[4].Style.Width != css.Pts(100) {
		t.Error("ellipse shape not drawn as a vector")
	}
	if got := textOf(b.root); !contains(got, "boxed") || contains(got, "FALLBACK") {
		t.Errorf("text box content %q", got)
	}
	if f := pics[5].Style; f.Position != css.Absolute || f.Left != css.Pts(0) {
		t.Errorf("a positionH without align or posOffset: position %v left %v, want absolute at 0", f.Position, f.Left)
	}
}

func TestDrawings_UnsupportedPicture(t *testing.T) {
	t.Parallel()
	emf := make([]byte, 64)
	emf[0] = 1
	copy(emf[40:], " EMF")
	body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="1" name="p"/>` + graphic("") + `</wp:inline></w:drawing></w:r></w:p>`
	b := build(t, body, map[string]string{"word/media/a.png": string(emf)})
	if len(find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })) != 0 {
		t.Error("an EMF picture was drawn")
	}
	if len(b.d.warnings) == 0 || b.d.warnings[0].Code != "unsupported-picture" {
		t.Errorf("warnings %+v", b.d.warnings)
	}
}

func TestDrawings_FromThePage(t *testing.T) {
	t.Parallel()
	const sect = `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="2880" w:left="1440" w:header="720" w:footer="720"/></w:sectPr>`
	anchor := func(wrap, h, v string) string {
		return `<w:r><w:drawing><wp:anchor behindDoc="0">` + h + v +
			`<wp:extent cx="1270000" cy="635000"/>` + wrap + `<wp:docPr id="2" name="q"/>` + graphic("") + `</wp:anchor></w:drawing></w:r>`
	}
	pos := func(axis, rel, inner string) string {
		return `<wp:position` + axis + ` relativeFrom="` + rel + `">` + inner + `</wp:position` + axis + `>`
	}
	off := func(pt int) string { return `<wp:posOffset>` + strconv.Itoa(pt*12700) + `</wp:posOffset>` }
	align := func(a string) string { return `<wp:align>` + a + `</wp:align>` }
	for _, tc := range []struct {
		name       string
		run        string
		cell       bool
		page       bool
		left, top  float64
		marginLeft float64
		float      css.Float
	}{
		{"page offsets", anchor(`<wp:wrapNone/>`, pos("H", "page", off(30)), pos("V", "page", off(40))), false, true, 30, 40, 0, css.FloatNone},
		{"margin alignments", anchor(`<wp:wrapNone/>`, pos("H", "margin", align("center")), pos("V", "margin", align("bottom"))), false, true, 72 + (468-100)/2.0, 792 - 144 - 50, 0, css.FloatNone},
		{"bottom margin", anchor(`<wp:wrapNone/>`, pos("H", "column", off(10)), pos("V", "bottomMargin", off(5))), false, true, 82, 792 - 144 + 5, 0, css.FloatNone},
		{"page right from a paragraph", anchor(`<wp:wrapNone/>`, pos("H", "page", align("right")), pos("V", "paragraph", off(0))), false, false, 612 - 100 - 72, 0, 0, css.FloatNone},
		{"square from the page", anchor(`<wp:wrapSquare wrapText="bothSides"/>`, pos("H", "page", off(200)), pos("V", "paragraph", off(0))), false, false, 0, 0, 128, css.FloatLeft},
		{"in a table cell", anchor(`<wp:wrapNone/>`, pos("H", "column", off(10)), pos("V", "page", off(40))), true, false, 10, 0, 0, css.FloatNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `<w:p>` + tc.run + `<w:r><w:t>text</w:t></w:r></w:p>`
			if tc.cell {
				body = `<w:tbl><w:tr><w:tc>` + body + `</w:tc></w:tr></w:tbl>`
			}
			b := build(t, body+sect, map[string]string{"word/media/a.png": pngData(t, 4, 2)})
			pics := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
			if len(pics) != 1 {
				t.Fatalf("%d pictures", len(pics))
			}
			st := pics[0].Style
			if st.PageAnchor != tc.page || st.Float != tc.float {
				t.Fatalf("page anchor %v float %v, want %v %v", st.PageAnchor, st.Float, tc.page, tc.float)
			}
			if tc.float != css.FloatNone {
				if !near(st.Margin[css.Left], tc.marginLeft) {
					t.Errorf("left margin %v, want %v", st.Margin[css.Left], tc.marginLeft)
				}
				return
			}
			if !near(st.Left, tc.left) || !near(st.Top, tc.top) {
				t.Errorf("at %v, %v; want %v, %v", st.Left, st.Top, tc.left, tc.top)
			}
		})
	}
}

func TestDrawings_PageAnchoredGroup(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:pict><v:group style="position:absolute;margin-left:100pt;margin-top:50pt;width:200pt;height:100pt;mso-position-horizontal-relative:page;mso-position-vertical-relative:page" coordsize="200,100">` +
		`<v:rect style="position:absolute;left:20;top:10;width:50;height:50"/><v:oval style="position:absolute;left:100;top:0;width:50;height:50"/></v:group></w:pict></w:r></w:p>`
	b := build(t, body, nil)
	shapes := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(shapes) != 2 {
		t.Fatalf("%d shapes", len(shapes))
	}
	for i, want := range [][2]float64{{120, 60}, {200, 50}} {
		st := shapes[i].Style
		if !st.PageAnchor || !near(st.Left, want[0]) || !near(st.Top, want[1]) {
			t.Errorf("shape %d anchored %v at %v, %v; want %v", i, st.PageAnchor, st.Left, st.Top, want)
		}
	}
}
