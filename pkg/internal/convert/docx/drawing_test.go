package docx

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/png"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
	if f := pics[1].Style; f.Float == css.FloatNone || f.Display != css.Block || f.FrameWrap != css.FrameWrapBoth || f.WordAnchor != (css.WordAnchor{Set: true, X: 468 - 100, Y: 10}) {
		t.Errorf("square wrap float %v display %v wrap %v at %+v", f.Float, f.Display, f.FrameWrap, f.WordAnchor)
	}
	if f := pics[2].Style; f.Display != css.Block || f.FrameWrap != css.FrameWrapNone || f.WordAnchor != (css.WordAnchor{Set: true, X: (468 - 100) / 2, Y: 10}) {
		t.Errorf("top and bottom wrap display %v wrap %v at %+v", f.Display, f.FrameWrap, f.WordAnchor)
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
	if len(b.d.warnings) == 0 || b.d.warnings[0].Code != warn.PictureFormatUnsupported {
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
				if !st.WordAnchor.Set || !near(css.Pts(st.WordAnchor.X), tc.marginLeft) {
					t.Errorf("offset %+v, want %v", st.WordAnchor, tc.marginLeft)
				}
				return
			}
			if !near(st.Left, tc.left) || !near(st.Top, tc.top) {
				t.Errorf("at %v, %v; want %v, %v", st.Left, st.Top, tc.left, tc.top)
			}
		})
	}
}

// TestDrawings_CutByThePage checks that a drawing of no wrapping
// positioned from its paragraph lies on its paragraph's page where its
// offset puts it, past the page's top or bottom edge too, as Word prints
// it, and adds no page.
func TestDrawings_CutByThePage(t *testing.T) {
	t.Parallel()
	const sect = `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720"/></w:sectPr>`
	for _, tc := range []struct {
		name   string
		offset int
		bottom float64
	}{
		{"past the bottom", 700, 72 + 700 + 50},
		{"past the top", -100, 72 - 100 + 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := `<w:r><w:drawing><wp:anchor behindDoc="0"><wp:positionH relativeFrom="column"><wp:posOffset>0</wp:posOffset></wp:positionH>` +
				`<wp:positionV relativeFrom="paragraph"><wp:posOffset>` + strconv.Itoa(tc.offset*12700) + `</wp:posOffset></wp:positionV>` +
				`<wp:extent cx="1270000" cy="635000"/><wp:wrapNone/><wp:docPr id="2" name="q"/>` + strings.Replace(graphic(""), ` rot="5400000" flipH="1"`, "", 1) + `</wp:anchor></w:drawing></w:r>`
			doc := convertBytes(t, docx(t, `<w:p>`+run+`<w:r><w:t>text</w:t></w:r></w:p>`+sect, map[string]string{"word/media/a.png": pngData(t, 4, 2)}), office.Options{})
			if len(doc.Pages) != 1 {
				t.Fatalf("%d pages, want 1", len(doc.Pages))
			}
			got := imageBottoms(doc.Pages[0])
			if len(got) != 1 || math.Abs(got[0]-tc.bottom) > 0.5 {
				t.Errorf("picture bottoms %v, want %v", got, tc.bottom)
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

func TestDrawings_StackOrderAndFloatOffset(t *testing.T) {
	t.Parallel()
	anchor := func(attrs, wrap string, posV int) string {
		return `<w:r><w:drawing><wp:anchor ` + attrs + `><wp:positionH relativeFrom="column"><wp:posOffset>0</wp:posOffset></wp:positionH>` +
			`<wp:positionV relativeFrom="paragraph"><wp:posOffset>` + strconv.Itoa(posV*12700) + `</wp:posOffset></wp:positionV>` +
			`<wp:extent cx="1270000" cy="635000"/>` + wrap + `<wp:docPr id="2" name="q"/>` + graphic("") + `</wp:anchor></w:drawing></w:r>`
	}
	body := `<w:p>` + anchor(`behindDoc="0" relativeHeight="200"`, `<wp:wrapNone/>`, 0) +
		anchor(`behindDoc="0" relativeHeight="300"`, `<wp:wrapSquare wrapText="bothSides"/>`, 20) +
		anchor(`behindDoc="1" relativeHeight="400"`, `<wp:wrapNone/>`, 0) + `<w:r><w:t>text</w:t></w:r></w:p>`
	b := build(t, body, map[string]string{"word/media/a.png": pngData(t, 4, 2)})
	pics := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(pics) != 3 {
		t.Fatalf("%d pictures", len(pics))
	}
	front, float, behind := pics[0].Style, pics[1].Style, pics[2].Style
	if front.ZIndex != (css.ZIndex{Level: 200}) || float.ZIndex != (css.ZIndex{Level: 300}) || behind.ZIndex.Level >= 0 {
		t.Errorf("stack levels %v %v %v", front.ZIndex, float.ZIndex, behind.ZIndex)
	}
	if float.Position != css.Relative || float.Float != css.FloatLeft || !near(css.Pts(float.WordAnchor.Y), 20) {
		t.Errorf("float position %v float %v offset %+v", float.Position, float.Float, float.WordAnchor)
	}
}

func TestDrawings_AloneKeepALine(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:drawing><wp:anchor behindDoc="0"><wp:positionH relativeFrom="column"><wp:posOffset>0</wp:posOffset></wp:positionH>` +
		`<wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV><wp:extent cx="1270000" cy="635000"/><wp:wrapNone/>` +
		`<wp:docPr id="2" name="q"/>` + graphic("") + `</wp:anchor></w:drawing></w:r></w:p>`
	b := build(t, body, map[string]string{"word/media/a.png": pngData(t, 4, 2)})
	p := role(b.root, "P")[0]
	if len(find(p, func(n *layout.Node) bool { return n.Kind == layout.LineBreak })) != 1 {
		t.Error("a paragraph holding a positioned drawing alone has no line")
	}
	if len(find(p, func(n *layout.Node) bool { return n.Kind == layout.Replaced })) != 1 {
		t.Error("the drawing is lost")
	}
}

// TestDrawings_Describe locks what a reader hears of a drawing: a
// picture's title when it has no description, nothing of a picture marked
// decorative, an artifact, and a text box's own text as the alt of its
// Figure.
func TestDrawings_Describe(t *testing.T) {
	t.Parallel()
	pic := func(docPr string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/>` + docPr +
			strings.Replace(graphic(""), ` descr="inner"`, ``, 1) + `</wp:inline></w:drawing></w:r></w:p>`
	}
	textBox := `<w:p><w:r><w:t>before</w:t></w:r><w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
		`<wps:wsp><wps:spPr><a:prstGeom prst="rect"/></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>Boxed</w:t></w:r></w:p><w:p><w:r><w:t>text</w:t></w:r></w:p></w:txbxContent></wps:txbx><wps:bodyPr/></wps:wsp>` +
		`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	body := pic(`<wp:docPr id="1" name="p" title="A title"/>`) +
		pic(`<wp:docPr id="2" name="d" descr="ignored"><a:extLst><a:ext uri="{C183D7F6-B498-43B3-948B-1728B52AA6E4}"><adec:decorative xmlns:adec="http://schemas.microsoft.com/office/drawing/2017/decorative" val="1"/></a:ext></a:extLst></wp:docPr>`) +
		textBox
	b := build(t, body, map[string]string{"word/media/a.png": pngData(t, 40, 20)})
	pics := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(pics) != 2 {
		t.Fatalf("%d pictures, want 2", len(pics))
	}
	if pics[0].Role != "Figure" || pics[0].Alt != "A title" {
		t.Errorf("titled picture: role %q alt %q", pics[0].Role, pics[0].Alt)
	}
	if pics[1].Role != "" || pics[1].Alt != "" {
		t.Errorf("decorative picture: role %q alt %q", pics[1].Role, pics[1].Alt)
	}
	figs := role(b.root, "Figure")
	if len(figs) != 2 || figs[1].Kind != layout.Element || figs[1].Alt != "Boxed text" || len(role(figs[1], "P")) != 2 {
		t.Errorf("text box figure %+v", figs)
	}
}

// TestChart checks that a chart frame draws its chart part, its title and
// category labels as text, at the frame's extent, and that a frame whose
// part is missing keeps its room with a warning.
func TestChart(t *testing.T) {
	t.Parallel()
	frame := func(id string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="3810000" cy="2540000"/><wp:docPr id="1" name="c" descr="Sales chart"/>` +
			`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart">` +
			`<c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" r:id="` + id + `"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	part := `<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><c:chart>` +
		`<c:title><c:tx><c:rich><a:p><a:r><a:t>Quarterly</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title>` +
		`<c:plotArea><c:barChart><c:barDir val="col"/><c:grouping val="clustered"/><c:ser><c:idx val="0"/><c:order val="0"/>` +
		`<c:cat><c:strLit><c:ptCount val="2"/><c:pt idx="0"><c:v>North</c:v></c:pt><c:pt idx="1"><c:v>South</c:v></c:pt></c:strLit></c:cat>` +
		`<c:val><c:numLit><c:ptCount val="2"/><c:pt idx="0"><c:v>3</c:v></c:pt><c:pt idx="1"><c:v>5</c:v></c:pt></c:numLit></c:val></c:ser>` +
		`<c:axId val="1"/><c:axId val="2"/></c:barChart><c:catAx><c:axId val="1"/><c:axPos val="b"/><c:crossAx val="2"/></c:catAx>` +
		`<c:valAx><c:axId val="2"/><c:axPos val="l"/><c:crossAx val="1"/></c:valAx></c:plotArea></c:chart></c:chartSpace>`
	b := build(t, frame("rIdchart1"), map[string]string{"word/charts/chart1.xml": part})
	charts := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(charts) != 1 {
		t.Fatalf("%d replaced boxes, want 1", len(charts))
	}
	c := charts[0]
	if c.Role != "Figure" || c.Alt != "Sales chart" || c.Style.Width != css.Pts(300) || c.Style.Height != css.Pts(200) || c.Picture.Vector == nil {
		t.Fatalf("chart role %q alt %q size %v x %v vector %v", c.Role, c.Alt, c.Style.Width, c.Style.Height, c.Picture.Vector != nil)
	}
	doc := convertBytes(t, docx(t, frame("rIdchart1"), map[string]string{"word/charts/chart1.xml": part}), office.Options{})
	for _, want := range []string{"Quarterly", "North", "South"} {
		if got := docText(doc); !strings.Contains(got, want) {
			t.Errorf("text %q lacks %q", got, want)
		}
	}
	missing := build(t, frame("rIdnone"), nil)
	if n := find(missing.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced }); len(n) != 0 {
		t.Errorf("a missing chart part drew %d boxes", len(n))
	}
	if len(missing.d.warnings) == 0 {
		t.Error("a missing chart part was not reported")
	}
}

// TestTextBox_ShapeAndAnchor checks that a text box of a preset other than
// a rectangle draws its shape behind its text, and that its text sits as
// bodyPr anchor says.
func TestTextBox_ShapeAndAnchor(t *testing.T) {
	t.Parallel()
	box := func(prst, anchor string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			`<wps:wsp><wps:spPr><a:prstGeom prst="` + prst + `"/><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>boxed</w:t></w:r></w:p></w:txbxContent></wps:txbx><wps:bodyPr anchor="` + anchor + `"/></wps:wsp>` +
			`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	figure := func(body string) *layout.Node {
		t.Helper()
		figs := role(build(t, body, nil).root, "Figure")
		if len(figs) != 1 {
			t.Fatalf("%d figures, want 1", len(figs))
		}
		return figs[0]
	}
	arrow := figure(box("rightArrow", "ctr"))
	if arrow.Style.BackgroundColor.A != 0 || len(arrow.Children) != 2 {
		t.Fatalf("arrow box background %v, %d children, want transparent and 2", arrow.Style.BackgroundColor, len(arrow.Children))
	}
	if pic := arrow.Children[0]; pic.Kind != layout.Replaced || pic.Style.Position != css.Absolute || pic.Picture.Vector == nil {
		t.Errorf("arrow's first child %v position %v, want the shape, absolute", pic.Kind, pic.Style.Position)
	}
	if area := arrow.Children[1]; area.Style.Display != css.Flex || area.Style.JustifyContent != css.BoxCenter || arrow.Style.Height != css.Pts(100) {
		t.Errorf("arrow's text area display %v justify %v, box height %v", area.Style.Display, area.Style.JustifyContent, arrow.Style.Height)
	}
	rect := figure(box("rect", "t"))
	if rect.Style.BackgroundColor.A == 0 || len(rect.Children) != 1 || rect.Children[0].Kind == layout.Replaced {
		t.Errorf("a top-anchored rectangle changed: background %v, %d children", rect.Style.BackgroundColor, len(rect.Children))
	}
	if bottom := figure(box("rect", "b")); bottom.Children[0].Style.JustifyContent != css.BoxFlexEnd {
		t.Errorf("a bottom-anchored box justifies %v", bottom.Children[0].Style.JustifyContent)
	}
}

// TestTextBox_TextRectangle checks that the text of a text box of a preset
// shape lies in the shape's text rectangle, its insets counted from it:
// an ellipse's text rectangle lies (1 - cos 45°) / 2 of each side inside
// its box, a triangle's starts at half its height, a rectangle's is its
// box.
func TestTextBox_TextRectangle(t *testing.T) {
	t.Parallel()
	box := func(prst string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			`<wps:wsp><wps:spPr><a:prstGeom prst="` + prst + `"/><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>boxed</w:t></w:r></w:p></w:txbxContent></wps:txbx><wps:bodyPr/></wps:wsp>` +
			`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	inset := (1 - math.Cos(math.Pi/4)) / 2
	for _, tc := range []struct {
		prst string
		want [4]float64
	}{
		{"ellipse", [4]float64{3.6 + 100*inset, 7.2 + 200*inset, 3.6 + 100*inset, 7.2 + 200*inset}},
		{"triangle", [4]float64{3.6 + 50, 7.2 + 50, 3.6, 7.2 + 50}},
		{"rect", [4]float64{3.6, 7.2, 3.6, 7.2}},
	} {
		figs := role(build(t, box(tc.prst), nil).root, "Figure")
		if len(figs) != 1 {
			t.Fatalf("%s: %d figures, want 1", tc.prst, len(figs))
		}
		for side, want := range tc.want {
			if got := figs[0].Style.Padding[side]; !near(got, want) {
				t.Errorf("%s: padding %d %v, want %.2f", tc.prst, side, got, want)
			}
		}
	}
}

// TestTextBox_InsetsLeaveNoRoom checks that a text box whose top and
// bottom insets take its whole height prints no text, as Word prints it,
// and warns, where a box with room keeps its text.
func TestTextBox_InsetsLeaveNoRoom(t *testing.T) {
	t.Parallel()
	box := func(tIns, bIns string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			`<wps:wsp><wps:spPr><a:prstGeom prst="rect"/><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>boxed</w:t></w:r></w:p></w:txbxContent></wps:txbx>` +
			`<wps:bodyPr vertOverflow="overflow" tIns="` + tIns + `" bIns="` + bIns + `" anchor="ctr"/></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	for _, tc := range []struct {
		name, tIns, bIns string
		text             bool
	}{
		{"insets fill the box", "508000", "762000", false},
		{"insets past the box", "762000", "762000", false},
		{"room left", "508000", "635000", true},
	} {
		b := build(t, box(tc.tIns, tc.bIns), nil)
		figs := role(b.root, "Figure")
		warned := slices.ContainsFunc(b.d.warnings, func(w typeset.Warning) bool { return strings.Contains(w.Message, "leave it no height") })
		if (len(figs) == 1) != tc.text || warned == tc.text {
			t.Errorf("%s: %d figures, warned %v; want text %v", tc.name, len(figs), warned, tc.text)
		}
	}
}

// TestTextBox_VerticalAnchor keeps a text box of vertical text at its
// height, its lines running along it, and places its lines across its
// width by its anchor: centred for ctr, at the block end for b.
func TestTextBox_VerticalAnchor(t *testing.T) {
	t.Parallel()
	box := func(anchor string) *layout.Node {
		t.Helper()
		body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="2540000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			`<wps:wsp><wps:spPr><a:prstGeom prst="rect"/></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>up</w:t></w:r></w:p></w:txbxContent></wps:txbx><wps:bodyPr vert="vert270" lIns="0" rIns="0" anchor="` + anchor + `"/></wps:wsp>` +
			`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
		figs := role(build(t, body, nil).root, "Figure")
		if len(figs) != 1 {
			t.Fatalf("%d figures, want 1", len(figs))
		}
		return figs[0]
	}
	for _, tc := range []struct {
		anchor  string
		justify css.BoxAlign
	}{{"ctr", css.BoxCenter}, {"b", css.BoxFlexEnd}} {
		n := box(tc.anchor)
		if n.Style.Height != css.Pts(200) || n.Style.WritingMode != css.SidewaysLR {
			t.Errorf("%s: height %v mode %v, want 200 points, sideways", tc.anchor, n.Style.Height, n.Style.WritingMode)
		}
		if len(n.Children) != 1 || n.Children[0].Style.JustifyContent != tc.justify || n.Children[0].Style.Height != css.Pts(100) {
			t.Errorf("%s: lines not placed across the box's 100 points", tc.anchor)
		}
	}
	if n := box("t"); n.Style.Height != css.Pts(200) || len(n.Children) != 1 || n.Children[0].Style.Display == css.Flex {
		t.Errorf("t: height %v, want 200 points and the lines at the block start", n.Style.Height)
	}
}

// TestTextBox_DashedOutline checks that a rectangular text box with a
// dashed outline (ECMA-376 Part 1, 20.1.8.48 prstDash) draws its outline
// as a path behind its text, which no border can dash, and that a solid
// outline stays the box's border.
func TestTextBox_DashedOutline(t *testing.T) {
	t.Parallel()
	box := func(dash string) *layout.Node {
		t.Helper()
		body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			`<wps:wsp><wps:spPr><a:prstGeom prst="rect"/><a:ln w="57150"><a:solidFill><a:srgbClr val="C00000"/></a:solidFill><a:prstDash val="` + dash + `"/></a:ln></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>boxed</w:t></w:r></w:p></w:txbxContent></wps:txbx><wps:bodyPr/></wps:wsp>` +
			`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
		figs := role(build(t, body, nil).root, "Figure")
		if len(figs) != 1 {
			t.Fatalf("%d figures, want 1", len(figs))
		}
		return figs[0]
	}
	dashed := box("lgDashDotDot")
	if dashed.Style.Border[css.Top].Width != 0 || len(dashed.Children) != 2 || dashed.Children[0].Kind != layout.Replaced || dashed.Children[0].Picture.Vector == nil {
		t.Errorf("dashed box border %v, %d children, want no border and the outline drawn first", dashed.Style.Border[css.Top], len(dashed.Children))
	}
	if solid := box("solid"); solid.Style.Border[css.Top].Width != 4.5 || solid.Children[0].Kind == layout.Replaced {
		t.Errorf("solid box border %v, want its 4.5-point border", solid.Style.Border[css.Top])
	}
}

// TestTextBox_AnchorInGroup checks that a centred text box inside a group,
// an absolutely positioned box, gives its text area a height in points:
// the box's height less its insets, which a percentage would leave
// unresolved there.
func TestTextBox_AnchorInGroup(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="3657600" cy="1828800"/><wp:docPr id="1" name="g"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingGroup">` +
		`<wpg:wgp xmlns:wpg="http://schemas.microsoft.com/office/word/2010/wordprocessingGroup"><wpg:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="3657600" cy="1828800"/><a:chOff x="0" y="0"/><a:chExt cx="3657600" cy="1828800"/></a:xfrm></wpg:grpSpPr>` +
		`<wps:wsp><wps:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1270000" cy="762000"/></a:xfrm><a:prstGeom prst="rect"/></wps:spPr>` +
		`<wps:txbx><w:txbxContent><w:p><w:r><w:t>centred</w:t></w:r></w:p></w:txbxContent></wps:txbx><wps:bodyPr anchor="ctr"/></wps:wsp></wpg:wgp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	figs := role(build(t, body, nil).root, "Figure")
	if len(figs) != 1 || len(figs[0].Children) != 1 {
		t.Fatalf("%d figures", len(figs))
	}
	if area := figs[0].Children[0]; area.Style.JustifyContent != css.BoxCenter || area.Style.Height != css.Pts(60-2*3.6) {
		t.Errorf("text area justify %v height %v, want centred and 52.8 points", area.Style.JustifyContent, area.Style.Height)
	}
}

// TestTextBox_TakesItsFontReferenceColour checks that the automatic text
// of a text box, in a group or alone, takes the colour of its shape's
// font reference (ECMA-376 Part 1, 20.1.4.2.10 fontRef), as Word paints
// it, and that a colour a run states wins.
func TestTextBox_TakesItsFontReferenceColour(t *testing.T) {
	t.Parallel()
	theme := `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:clrScheme name="x">` +
		`<a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:accent1><a:srgbClr val="4F81BD"/></a:accent1></a:clrScheme></a:themeElements></a:theme>`
	style := `<wps:style><a:lnRef idx="2"><a:schemeClr val="accent1"/></a:lnRef><a:fillRef idx="1"><a:schemeClr val="accent1"/></a:fillRef><a:effectRef idx="0"><a:schemeClr val="accent1"/></a:effectRef><a:fontRef idx="minor"><a:schemeClr val="lt1"/></a:fontRef></wps:style>`
	wsp := func(style, text string) string {
		return `<wps:wsp><wps:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1270000" cy="762000"/></a:xfrm><a:prstGeom prst="rect"/></wps:spPr>` + style +
			`<wps:txbx><w:txbxContent><w:p><w:r><w:t>` + text + `</w:t></w:r><w:r><w:rPr><w:color w:val="FF0000"/></w:rPr><w:t>red` + text + `</w:t></w:r></w:p></w:txbxContent></wps:txbx><wps:bodyPr/></wps:wsp>`
	}
	grouped := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="3657600" cy="1828800"/><wp:docPr id="1" name="g"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingGroup">` +
		`<wpg:wgp xmlns:wpg="http://schemas.microsoft.com/office/word/2010/wordprocessingGroup"><wpg:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="3657600" cy="1828800"/><a:chOff x="0" y="0"/><a:chExt cx="3657600" cy="1828800"/></a:xfrm></wpg:grpSpPr>` +
		wsp(style, "grouped") + `</wpg:wgp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	alone := func(style, text string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="762000"/><wp:docPr id="2" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			wsp(style, text) + `</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	body := grouped + alone(style, "alone") + alone("", "unstyled") + `<w:p><w:r><w:t>after</w:t></w:r></w:p>`
	b := build(t, body, map[string]string{"word/theme/theme1.xml": theme})
	white, black, red := css.Color{R: 1, G: 1, B: 1, A: 1}, css.Color{A: 1}, css.Color{R: 1, A: 1}
	for _, tc := range []struct {
		text string
		want css.Color
	}{
		{"grouped", white}, {"redgrouped", red}, {"alone", white}, {"redalone", red}, {"unstyled", black}, {"after", black},
	} {
		if st := runWith(t, b.root, tc.text).Style; st.Color != tc.want {
			t.Errorf("run %q: colour %+v, want %+v", tc.text, st.Color, tc.want)
		}
	}
}

// TestCanvas checks a drawing canvas: its background and outline as a
// rectangle of its size, then its shapes at their offsets.
func TestCanvas(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="1" name="c"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas">` +
		`<wpc:wpc xmlns:wpc="http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas"><wpc:bg><a:solidFill><a:srgbClr val="F2F2F2"/></a:solidFill></wpc:bg><wpc:whole><a:ln w="9525"><a:solidFill><a:srgbClr val="808080"/></a:solidFill></a:ln></wpc:whole>` +
		`<wps:wsp><wps:spPr><a:xfrm><a:off x="127000" y="254000"/><a:ext cx="1270000" cy="635000"/></a:xfrm><a:prstGeom prst="ellipse"/><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></wps:spPr><wps:bodyPr/></wps:wsp>` +
		`</wpc:wpc></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	pics := find(build(t, body, nil).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(pics) != 2 {
		t.Fatalf("%d replaced boxes, want the canvas and its shape", len(pics))
	}
	if bg := pics[0]; bg.Style.Width != css.Pts(200) || bg.Style.Left != css.Pts(0) {
		t.Errorf("background %v wide at %v", bg.Style.Width, bg.Style.Left)
	}
	if sh := pics[1]; sh.Style.Position != css.Absolute || sh.Style.Left != css.Pts(10) || sh.Style.Top != css.Pts(20) {
		t.Errorf("shape at %v, %v", sh.Style.Left, sh.Style.Top)
	}
}

// TestCanvas_Choice checks that a drawing canvas offered as the Choice of
// an mc:AlternateContent requiring its namespace is drawn, not the
// Fallback.
func TestCanvas_Choice(t *testing.T) {
	t.Parallel()
	body := `<w:p><mc:AlternateContent xmlns:wpc="http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas"><mc:Choice Requires="wpc"><w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="1" name="c"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas">` +
		`<wpc:wpc><wpc:bg><a:solidFill><a:srgbClr val="F2F2F2"/></a:solidFill></wpc:bg>` +
		`<wps:wsp><wps:spPr><a:xfrm><a:off x="127000" y="254000"/><a:ext cx="1270000" cy="635000"/></a:xfrm><a:prstGeom prst="ellipse"/><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></wps:spPr><wps:bodyPr/></wps:wsp>` +
		`</wpc:wpc></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></mc:Choice>` +
		`<mc:Fallback><w:r><w:t>fallback</w:t></w:r></mc:Fallback></mc:AlternateContent></w:p>`
	root := build(t, body, nil).root
	pics := find(root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(pics) != 2 || strings.Contains(textOf(root), "fallback") {
		t.Errorf("%d replaced boxes, text %q; want the canvas and its shape, no Fallback", len(pics), textOf(root))
	}
}

// TestDrawings_WordAnchor positions a drawing that wraps the text as Word
// does: a percentage of the margin box (MS-ODRAWXML, 2.3.2.3, 2.3.2.4),
// simplePos from the page's corner (20.4.2.13), an alignment in the
// first column of a section of columns, and a tight wrap along its wrap
// polygon (20.4.2.16) in fractions of the drawing.
func TestDrawings_WordAnchor(t *testing.T) {
	t.Parallel()
	const sect = `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720"/>%s</w:sectPr>`
	const wp14 = `xmlns:wp14="http://schemas.microsoft.com/office/word/2010/wordprocessingDrawing"`
	anchor := func(attrs, h, v, wrap string) string {
		return `<w:p><w:r><w:drawing><wp:anchor behindDoc="0" ` + attrs + `>` + h + v + `<wp:extent cx="1270000" cy="635000"/>` + wrap +
			`<wp:docPr id="2" name="q"/>` + graphic("") + `</wp:anchor></w:drawing></w:r><w:r><w:t>text</w:t></w:r></w:p>`
	}
	square := `<wp:wrapSquare wrapText="bothSides"/>`
	for _, tc := range []struct {
		name, body, cols string
		want             css.WordAnchor
		poly             []css.WrapPoint
	}{
		{"percentages of the margin", anchor(``,
			`<wp:positionH relativeFrom="margin" `+wp14+`><wp14:pctPosHOffset>40000</wp14:pctPosHOffset></wp:positionH>`,
			`<wp:positionV relativeFrom="margin" `+wp14+`><wp14:pctPosVOffset>60000</wp14:pctPosVOffset></wp:positionV>`, square),
			"", css.WordAnchor{Set: true, X: 0.4 * 468, Y: 0.6 * 648, FromPage: true}, nil},
		{"simplePos", anchor(`simplePos="1"`, `<wp:simplePos x="5486400" y="457200"/>`, ``, square),
			"", css.WordAnchor{Set: true, X: 432 - 72, Y: 36 - 72, FromPage: true}, nil},
		{"centered in a column", anchor(``, `<wp:positionH relativeFrom="column"><wp:align>center</wp:align></wp:positionH>`,
			`<wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV>`, square),
			`<w:cols w:num="2" w:space="720"/>`, css.WordAnchor{Set: true, X: (216 - 100) / 2.0}, nil},
		{"tight along its polygon", anchor(`allowOverlap="0"`, `<wp:positionH relativeFrom="column"><wp:posOffset>0</wp:posOffset></wp:positionH>`,
			`<wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV>`,
			`<wp:wrapTight wrapText="bothSides"><wp:wrapPolygon edited="0"><wp:start x="0" y="0"/><wp:lineTo x="21600" y="10800"/><wp:lineTo x="0" y="21600"/><wp:lineTo x="0" y="0"/></wp:wrapPolygon></wp:wrapTight>`),
			"", css.WordAnchor{Set: true, NoOverlap: true}, []css.WrapPoint{{X: 0, Y: 0}, {X: 1, Y: 0.5}, {X: 0, Y: 1}, {X: 0, Y: 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := build(t, tc.body+fmt.Sprintf(sect, tc.cols), map[string]string{"word/media/a.png": pngData(t, 4, 2)})
			pics := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
			if len(pics) != 1 {
				t.Fatalf("%d pictures", len(pics))
			}
			st := pics[0].Style
			got := st.WordAnchor
			if got.Set != tc.want.Set || got.FromPage != tc.want.FromPage || got.NoOverlap != tc.want.NoOverlap ||
				math.Abs(got.X-tc.want.X) > 0.01 || math.Abs(got.Y-tc.want.Y) > 0.01 {
				t.Errorf("anchor %+v, want %+v", got, tc.want)
			}
			if fmt.Sprint(st.WrapPolygon) != fmt.Sprint(tc.poly) {
				t.Errorf("polygon %v, want %v", st.WrapPolygon, tc.poly)
			}
		})
	}
}

// TestDrawings_FromAboveSpaceBefore locks the origin of a drawing
// positioned from its paragraph (ECMA-376 Part 1, 20.4.3.5 paragraph):
// above the paragraph's space before, so the offset loses that space,
// for a drawing in front of the text, one wrapping it and a VML shape;
// one positioned from its line keeps its offset.
func TestDrawings_FromAboveSpaceBefore(t *testing.T) {
	t.Parallel()
	anchor := func(rel, wrap string) string {
		return `<w:r><w:drawing><wp:anchor behindDoc="0" relativeHeight="2"><wp:positionH relativeFrom="column"><wp:posOffset>0</wp:posOffset></wp:positionH>` +
			`<wp:positionV relativeFrom="` + rel + `"><wp:posOffset>381000</wp:posOffset></wp:positionV>` +
			`<wp:extent cx="1270000" cy="635000"/>` + wrap + `<wp:docPr id="2" name="q"/>` + graphic("") + `</wp:anchor></w:drawing></w:r>`
	}
	body := `<w:p><w:pPr><w:spacing w:before="800"/></w:pPr>` + anchor("paragraph", `<wp:wrapNone/>`) +
		anchor("paragraph", `<wp:wrapSquare wrapText="bothSides"/>`) + anchor("line", `<wp:wrapNone/>`) +
		`<w:r><w:pict><v:rect style="position:absolute;margin-left:0;margin-top:30pt;width:20pt;height:20pt"/></w:pict></w:r>` +
		`<w:r><w:t>text</w:t></w:r></w:p>`
	b := build(t, body, map[string]string{"word/media/a.png": pngData(t, 4, 2)})
	shapes := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(shapes) != 4 {
		t.Fatalf("%d drawings", len(shapes))
	}
	if st := shapes[0].Style; st.Position != css.Absolute || !near(st.Top, -10) {
		t.Errorf("drawing in front of the text at %v, want -10", st.Top)
	}
	if a := shapes[1].Style.WordAnchor; !a.Set || math.Abs(a.Y+10) > 0.01 {
		t.Errorf("wrapped drawing %+v, want Y -10", a)
	}
	if st := shapes[2].Style; !near(st.Top, 30) {
		t.Errorf("drawing from its line at %v, want 30", st.Top)
	}
	if st := shapes[3].Style; !near(st.Top, -10) {
		t.Errorf("VML shape at %v, want -10", st.Top)
	}
}

// TestTextBox_Rotation locks a text box turned by its shape's rotation
// (ECMA-376 Part 1, 20.1.7.6 xfrm rot) with its text, and one whose body
// keeps the text upright (21.1.2.1.1 bodyPr upright) left unturned.
func TestTextBox_Rotation(t *testing.T) {
	t.Parallel()
	box := func(bodyPr string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			`<wps:wsp><wps:spPr><a:xfrm rot="1800000"><a:off x="0" y="0"/><a:ext cx="2540000" cy="1270000"/></a:xfrm><a:prstGeom prst="rect"/></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>Turned</w:t></w:r></w:p></w:txbxContent></wps:txbx>` + bodyPr + `</wps:wsp>` +
			`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	b := build(t, box(`<wps:bodyPr/>`)+box(`<wps:bodyPr upright="1"/>`), nil)
	figs := role(b.root, "Figure")
	if len(figs) != 2 {
		t.Fatalf("%d text boxes", len(figs))
	}
	if tr := figs[0].Style.Transform; len(tr) != 1 || tr[0].Kind != css.TransformRotate || math.Abs(tr[0].V[0]-math.Pi/6) > 1e-9 {
		t.Errorf("turned box transform %+v, want a 30 degree rotation", tr)
	}
	if tr := figs[1].Style.Transform; len(tr) != 0 {
		t.Errorf("upright box transform %+v", tr)
	}
}

// TestTextBox_InlineOnBaseline sits two inline text boxes of different
// heights on their line by their bottom edges, as Word sets every inline
// drawing (ECMA-376 Part 1, 20.4.2.8 inline): the taller box's first line
// stands the difference of their heights above the shorter one's.
func TestTextBox_InlineOnBaseline(t *testing.T) {
	t.Parallel()
	box := func(cy, text string) string {
		return `<w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="` + cy + `"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			`<wps:wsp><wps:spPr><a:prstGeom prst="rect"/></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:txbxContent></wps:txbx>` +
			`<wps:bodyPr lIns="0" tIns="0" rIns="0" bIns="0" anchor="t"/></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`
	}
	doc := convertBytes(t, docx(t, `<w:p>`+box("800000", "Short")+box("2400000", "Tall")+`</w:p>`, nil), office.Options{})
	ys := map[string]float64{}
	var walk func([]typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Text:
				ys[glyphText(v)] = v.Origin.Y
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(doc.Pages[0].Content)
	short, ok1 := ys["Short"]
	tall, ok2 := ys["Tall"]
	if !ok1 || !ok2 {
		t.Fatalf("texts %v", ys)
	}
	// 2400000 less 800000 EMU is 126 points.
	if d := short - tall; math.Abs(d-126) > 0.5 {
		t.Errorf("short box's line %.2f points below the tall one's, want 126", d)
	}
}

// TestDrawings_BreakClear starts the line after a text wrapping break
// below the square-wrapped drawings on the side its w:clear names
// (ECMA-376 Part 1, 17.3.3.1 and 17.18.3 ST_BrClear), and beside them for
// the other side and for none.
func TestDrawings_BreakClear(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		clear string
		below bool
	}{{"left", true}, {"all", true}, {"right", false}, {"none", false}} {
		t.Run(tc.clear, func(t *testing.T) {
			t.Parallel()
			pic := `<w:r><w:drawing><wp:anchor behindDoc="0" distT="0" distB="0" distL="0" distR="0"><wp:positionH relativeFrom="column"><wp:align>left</wp:align></wp:positionH>` +
				`<wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV><wp:extent cx="1270000" cy="1270000"/><wp:wrapSquare wrapText="bothSides"/>` +
				`<wp:docPr id="2" name="q"/>` + graphic("") + `</wp:anchor></w:drawing></w:r>`
			body := `<w:p>` + pic + `<w:r><w:t>Beside</w:t><w:br w:type="textWrapping" w:clear="` + tc.clear + `"/><w:t>After</w:t></w:r></w:p>`
			doc := convertBytes(t, docx(t, body, map[string]string{"word/media/a.png": pngData(t, 40, 40)}), office.Options{})
			var y float64
			found := false
			for _, r := range textRuns(doc) {
				var s strings.Builder
				for _, g := range r.Glyphs {
					s.WriteString(g.Text)
				}
				if strings.Contains(s.String(), "After") {
					y, found = r.Origin.Y, true
				}
			}
			if !found {
				t.Fatalf("no run After in %q", docText(doc))
			}
			// The picture is 100 points tall from the paragraph's top, the
			// top of the page area the runs are placed in.
			if below := y > 100; below != tc.below {
				t.Errorf("After at y %v: below the picture %v, want %v", y, below, tc.below)
			}
		})
	}
}

// TestDrawings_RelativeSize sizes an anchored shape as a share of the area
// its wp14:sizeRelH and wp14:sizeRelV name (MS-ODRAWXML, 2.3.1.2 and
// 2.3.1.3), in thousandths of a percent, a share of 0 keeping the extent.
func TestDrawings_RelativeSize(t *testing.T) {
	t.Parallel()
	const sect = `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="2880" w:left="1440" w:header="720" w:footer="720"/></w:sectPr>`
	shape := func(rel string) string {
		return `<w:p><w:r><w:drawing><wp:anchor behindDoc="0" xmlns:wp14="http://schemas.microsoft.com/office/word/2010/wordprocessingDrawing"><wp:positionH relativeFrom="column"><wp:posOffset>0</wp:posOffset></wp:positionH>` +
			`<wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV><wp:extent cx="1270000" cy="635000"/><wp:wrapNone/><wp:docPr id="3" name="s"/>` +
			`<a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"><wps:wsp><wps:spPr><a:prstGeom prst="rect"/></wps:spPr><wps:bodyPr/></wps:wsp></a:graphicData></a:graphic>` +
			rel + `</wp:anchor></w:drawing></w:r></w:p>`
	}
	body := shape(`<wp14:sizeRelH relativeFrom="margin"><wp14:pctWidth>50000</wp14:pctWidth></wp14:sizeRelH><wp14:sizeRelV relativeFrom="page"><wp14:pctHeight>25000</wp14:pctHeight></wp14:sizeRelV>`) +
		shape(`<wp14:sizeRelH relativeFrom="leftMargin"><wp14:pctWidth>100000</wp14:pctWidth></wp14:sizeRelH><wp14:sizeRelV relativeFrom="margin"><wp14:pctHeight>0</wp14:pctHeight></wp14:sizeRelV>`) +
		shape(``) + sect
	b := build(t, body, nil)
	shapes := find(b.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(shapes) != 3 {
		t.Fatalf("%d shapes, want 3", len(shapes))
	}
	// Half of the 468 points between the margins by a quarter of the 792
	// point page; the left margin's 72 points by the extent's 50; the
	// extent's 100 by 50.
	for i, want := range [][2]float64{{234, 198}, {72, 50}, {100, 50}} {
		if st := shapes[i].Style; math.Abs(st.Width.Value-want[0]) > 1e-6 || math.Abs(st.Height.Value-want[1]) > 1e-6 {
			t.Errorf("shape %d is %v by %v, want %v by %v", i, st.Width.Value, st.Height.Value, want[0], want[1])
		}
	}
}

// TestChartDerivedAltText holds the description a chart its drawing does
// not describe takes under office.Options.DerivedAltText: chart.AltText of
// its model, in English, stated on the Figure in a document of another
// language; a described chart keeps its own, and without the option an
// undescribed chart takes its title, as a sheet's or a slide's chart does
// (ISO 14289-1, 7.3), and one without a title takes none.
//
// # Corpus evidence
//
//   - litho/docx charts-bar-column, charts-chartex, charts-line-area,
//     charts-pie-doughnut, charts-scatter-bubble-radar, charts-stock-combo,
//     charts-trendlines-errorbars: every chart is titled and none is
//     described; their Figures without a description failed PDF/UA-1 and
//     PDF/UA-2, with their titles they pass.
func TestChartDerivedAltText(t *testing.T) {
	t.Parallel()
	frame := func(descr string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="3810000" cy="2540000"/><wp:docPr id="1" name="Chart 1"` + descr + `/>` +
			`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart">` +
			`<c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" r:id="rIdchart1"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	part := map[string]string{"word/charts/chart1.xml": `<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><c:chart>` +
		`<c:title><c:tx><c:rich><a:p><a:r><a:t>Quarterly</a:t></a:r></a:p></c:rich></c:tx></c:title>` +
		`<c:plotArea><c:barChart><c:barDir val="col"/><c:grouping val="clustered"/><c:ser><c:idx val="0"/><c:order val="0"/>` +
		`<c:cat><c:strLit><c:ptCount val="2"/><c:pt idx="0"><c:v>North</c:v></c:pt><c:pt idx="1"><c:v>South</c:v></c:pt></c:strLit></c:cat>` +
		`<c:val><c:numLit><c:ptCount val="2"/><c:pt idx="0"><c:v>3</c:v></c:pt><c:pt idx="1"><c:v>5</c:v></c:pt></c:numLit></c:val></c:ser>` +
		`<c:axId val="1"/><c:axId val="2"/></c:barChart><c:catAx><c:axId val="1"/><c:axPos val="b"/><c:crossAx val="2"/></c:catAx>` +
		`<c:valAx><c:axId val="2"/><c:axPos val="l"/><c:crossAx val="1"/></c:valAx></c:plotArea></c:chart></c:chartSpace>`}
	chartOf := func(body string, opts office.Options) *layout.Node {
		t.Helper()
		got := find(buildWith(t, body, part, opts).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
		if len(got) != 1 {
			t.Fatalf("%d replaced boxes, want 1", len(got))
		}
		return got[0]
	}
	c := chartOf(frame(""), office.Options{DerivedAltText: true, DefaultLang: "fr-FR"})
	if want := "Quarterly. Clustered column chart, 1 series, 2 categories (North and South)"; c.Role != "Figure" || c.Alt != want || c.Lang != "en" {
		t.Errorf("derived: role %q alt %q lang %q, want Figure %q en", c.Role, c.Alt, c.Lang, want)
	}
	if c := chartOf(frame(""), office.Options{DerivedAltText: true, DefaultLang: "en-US"}); c.Lang != "" {
		t.Errorf("in an English document the Figure states lang %q", c.Lang)
	}
	if c := chartOf(frame(` descr="Own"`), office.Options{DerivedAltText: true}); c.Alt != "Own" || c.Lang != "" {
		t.Errorf("described: alt %q lang %q", c.Alt, c.Lang)
	}
	if c := chartOf(frame(""), office.Options{}); c.Alt != "Quarterly" || c.Lang != "" {
		t.Errorf("without the option: alt %q lang %q, want the title", c.Alt, c.Lang)
	}
	untitled := map[string]string{"word/charts/chart1.xml": strings.Replace(part["word/charts/chart1.xml"], `<c:title><c:tx><c:rich><a:p><a:r><a:t>Quarterly</a:t></a:r></a:p></c:rich></c:tx></c:title>`, `<c:autoTitleDeleted val="1"/>`, 1)}
	got := find(buildWith(t, frame(""), untitled, office.Options{}).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(got) != 1 {
		t.Fatalf("an untitled chart: %d replaced boxes, want 1", len(got))
	}
	if got[0].Alt != "" {
		t.Errorf("an untitled chart: alt %q, want none", got[0].Alt)
	}
}

// TestTextBox_FitToText checks that a text box marked to resize to its
// text, in DrawingML (spAutoFit) or VML (mso-fit-shape-to-text), keeps no
// minimum height from its extent, and that an unmarked one keeps it.
func TestTextBox_FitToText(t *testing.T) {
	t.Parallel()
	dml := func(bodyPr string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="2540000" cy="1270000"/><wp:docPr id="4" name="t"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
			`<wps:wsp><wps:spPr><a:prstGeom prst="rect"/></wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>Boxed</w:t></w:r></w:p></w:txbxContent></wps:txbx>` + bodyPr + `</wps:wsp>` +
			`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	vml := func(style string) string {
		return `<w:p><w:r><w:pict><v:shape type="#_x0000_t202" style="width:200pt;height:100pt"><v:textbox style="` + style + `"><w:txbxContent><w:p><w:r><w:t>Boxed</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict></w:r></w:p>`
	}
	for _, tc := range []struct {
		name, body string
		min        float64
	}{
		{"drawingml fitted", dml(`<wps:bodyPr><a:spAutoFit/></wps:bodyPr>`), 0},
		{"drawingml", dml(`<wps:bodyPr/>`), 100},
		{"vml fitted", vml(`mso-fit-shape-to-text:t`), 0},
		{"vml", vml(``), 100},
	} {
		figs := role(build(t, tc.body, nil).root, "Figure")
		if len(figs) != 1 {
			t.Fatalf("%s: %d text boxes", tc.name, len(figs))
		}
		if got := figs[0].Style.MinHeight; got != css.Pts(tc.min) {
			t.Errorf("%s: minimum height %v, want %v", tc.name, got, tc.min)
		}
	}
}

// TestPicture_LinkPlaceholder checks that a picture that only links to its
// file prints Word's placeholder, its extent framed, and that an embedded
// picture whose part is missing keeps its room unframed.
func TestPicture_LinkPlaceholder(t *testing.T) {
	t.Parallel()
	pic := func(blip string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="1" name="p"/>` +
			strings.Replace(graphic(""), `<a:blip r:embed="rIda"/>`, blip, 1) + `</wp:inline></w:drawing></w:r></w:p>`
	}
	b := build(t, pic(`<a:blip r:link="rIdLinked"/>`)+pic(`<a:blip r:embed="rIdMissing"/>`), nil)
	boxes := find(b.root, func(n *layout.Node) bool {
		return n.Kind == layout.Element && n.Style != nil && n.Style.Display == css.InlineBlock && n.Style.Width == css.Pts(100)
	})
	if len(boxes) != 2 {
		t.Fatalf("%d picture boxes, want 2", len(boxes))
	}
	if st := boxes[0].Style; st.Border[css.Top].Width != 0.25 || !st.StrokeBorders || st.BackgroundColor.A != 1 {
		t.Errorf("linked picture box %+v, want a white box framed at 0.25 points", st.Border)
	}
	if st := boxes[1].Style; st.Border[css.Top].Width != 0 {
		t.Errorf("missing embedded picture framed: %+v", st.Border)
	}
}

// TestDrawings_LinkedPicture makes an inline picture whose wp:docPr holds
// an a:hlinkClick a link to the relationship's target (20.1.2.2.23).
func TestDrawings_LinkedPicture(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="1" name="p"><a:hlinkClick r:id="rIdlink"/></wp:docPr>` +
		graphic("") + `</wp:inline></w:drawing></w:r></w:p>`
	b := build(t, body, map[string]string{"word/media/a.png": pngData(t, 40, 20)})
	links := find(b.root, func(n *layout.Node) bool { return n.Href != "" })
	if len(links) != 1 || links[0].Href != "https://example.com/" {
		t.Fatalf("%d links, want one to https://example.com/", len(links))
	}
	if pics := find(links[0], func(n *layout.Node) bool { return n.Kind == layout.Replaced }); len(pics) != 1 {
		t.Errorf("the link holds %d pictures, want the picture", len(pics))
	}
}

// TestVMLAnchor maps the VML text anchors onto DrawingML's.
func TestVMLAnchor(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"": "t", "top": "t", "middle": "ctr", "middle-center": "ctr", "bottom-baseline": "b"} {
		if got := vmlAnchor(in); got != want {
			t.Errorf("vmlAnchor(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLockedCanvas draws the shapes of a locked canvas at their offsets
// in its child space, scaled to the canvas's extent, and those of a canvas
// of no extent at their own size, the corner of their bounds at its
// origin.
func TestLockedCanvas(t *testing.T) {
	t.Parallel()
	canvas := func(cx, cy, grp string) string {
		return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="` + cx + `" cy="` + cy + `"/><wp:docPr id="4" name="c"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/lockedCanvas">` +
			`<lc:lockedCanvas xmlns:lc="http://schemas.openxmlformats.org/drawingml/2006/lockedCanvas">` + grp +
			`<a:sp><a:spPr><a:xfrm><a:off x="1270000" y="635000"/><a:ext cx="1270000" cy="635000"/></a:xfrm><a:prstGeom prst="rect"/><a:solidFill><a:srgbClr val="4472C4"/></a:solidFill></a:spPr></a:sp>` +
			`</lc:lockedCanvas></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	shapes := func(body string) []*layout.Node {
		t.Helper()
		return find(build(t, body, nil).root, func(n *layout.Node) bool {
			return n.Kind == layout.Replaced && n.Picture != nil && n.Picture.Vector != nil
		})
	}
	// A child space of 400 by 200 points drawn in 200 by 100.
	scaled := shapes(canvas("2540000", "1270000", `<a:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="5080000" cy="2540000"/><a:chOff x="0" y="0"/><a:chExt cx="5080000" cy="2540000"/></a:xfrm></a:grpSpPr>`))
	if len(scaled) != 1 || scaled[0].Style.Left != css.Pts(50) || scaled[0].Style.Top != css.Pts(25) || scaled[0].Style.Width != css.Pts(50) {
		t.Fatalf("scaled canvas: %d shapes, want one at 50, 25, 50 points wide", len(scaled))
	}
	bare := shapes(canvas("2540000", "1270000", `<a:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></a:grpSpPr>`))
	if len(bare) != 1 || bare[0].Style.Left != css.Pts(0) || bare[0].Style.Top != css.Pts(0) || bare[0].Style.Width != css.Pts(100) {
		t.Fatalf("canvas of no extent: %d shapes, want one at its origin, 100 points wide", len(bare))
	}
}

// TestLockedCanvas_PictureAlt describes a picture of a locked canvas by
// its own non-visual properties, in the DrawingML namespace.
func TestLockedCanvas_PictureAlt(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="1270000"/><wp:docPr id="4" name="c"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/lockedCanvas">` +
		`<lc:lockedCanvas xmlns:lc="http://schemas.openxmlformats.org/drawingml/2006/lockedCanvas"><a:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1270000" cy="1270000"/><a:chOff x="0" y="0"/><a:chExt cx="1270000" cy="1270000"/></a:xfrm></a:grpSpPr>` +
		`<a:pic><a:nvPicPr><a:cNvPr id="2" name="p" descr="A statue"/><a:cNvPicPr/></a:nvPicPr><a:blipFill><a:blip r:embed="rIda"/><a:stretch><a:fillRect/></a:stretch></a:blipFill>` +
		`<a:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="635000" cy="635000"/></a:xfrm><a:prstGeom prst="rect"/></a:spPr></a:pic>` +
		`</lc:lockedCanvas></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	figs := role(build(t, body, map[string]string{"word/media/a.png": pngData(t, 4, 4)}).root, "Figure")
	if len(figs) != 1 || figs[0].Alt != "A statue" {
		t.Fatalf("%d figures, want one described %q", len(figs), "A statue")
	}
}

// TestPictureShape_Alt describes a shape that draws a picture alone, a
// rectangle filled with it (pictureShape), by the description of its
// drawing, as a picture is (ISO 14289-1, 7.3).
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/crop-roundtrip.docx: a rectangle filled
//     with a photograph, its drawing described "ramen", drew a Figure
//     without a description and failed PDF/UA-1 and PDF/UA-2.
func TestPictureShape_Alt(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="89" name="Rectangle 89" descr="ramen"/>` +
		`<a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"><wps:wsp><wps:cNvSpPr/>` +
		`<wps:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1270000" cy="635000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom>` +
		`<a:blipFill><a:blip r:embed="rIda"/><a:stretch><a:fillRect/></a:stretch></a:blipFill><a:ln><a:noFill/></a:ln></wps:spPr><wps:bodyPr/></wps:wsp>` +
		`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	figs := role(build(t, body, map[string]string{"word/media/a.png": pngData(t, 4, 4)}).root, "Figure")
	if len(figs) != 1 || figs[0].Alt != "ramen" {
		t.Fatalf("%d figures %+v, want one described %q", len(figs), figs, "ramen")
	}
}

// A group's child space scales onto its canvas by its extent over its
// child extent; nested groups multiply, so each scale is capped and the
// coordinates of a hostile canvas stay finite.
func TestCanvasSpace_ScaleIsCapped(t *testing.T) {
	xfrm := &ooxml.Element{Space: ooxml.NSA, Local: "xfrm", Children: []*ooxml.Element{
		{Space: ooxml.NSA, Local: "off", Attrs: []xml.Attr{{Name: xml.Name{Local: "x"}, Value: "0"}, {Name: xml.Name{Local: "y"}, Value: "0"}}},
		{Space: ooxml.NSA, Local: "ext", Attrs: []xml.Attr{{Name: xml.Name{Local: "cx"}, Value: "9000000000000000000"}, {Name: xml.Name{Local: "cy"}, Value: "9000000000000000000"}}},
		{Space: ooxml.NSA, Local: "chOff", Attrs: []xml.Attr{{Name: xml.Name{Local: "x"}, Value: "0"}, {Name: xml.Name{Local: "y"}, Value: "0"}}},
		{Space: ooxml.NSA, Local: "chExt", Attrs: []xml.Attr{{Name: xml.Name{Local: "cx"}, Value: "1"}, {Name: xml.Name{Local: "cy"}, Value: "1"}}},
	}}
	c := canvasSpace{scale: [2]float64{1, 1}}
	for range maxNesting {
		c = c.childSpace(xfrm)
	}
	x, y := c.point(9e18, 9e18)
	if c.scale[0] > maxCanvasScale || c.scale[1] > maxCanvasScale || math.IsInf(x, 0) || math.IsNaN(x) || math.IsInf(y, 0) || math.IsNaN(y) {
		t.Fatalf("scale %v, point %v %v: want capped scales and a finite point", c.scale, x, y)
	}
}

// TestInline_EffectExtent grows the room an inline drawing takes in its
// line by its effect extent, side by side.
func TestInline_EffectExtent(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:effectExtent l="127000" t="254000" r="0" b="381000"/><wp:docPr id="4" name="p"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
		`<wps:wsp><wps:spPr><a:prstGeom prst="ellipse"/></wps:spPr><wps:bodyPr/></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	shapes := find(build(t, body, nil).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(shapes) != 1 {
		t.Fatalf("%d drawings, want 1", len(shapes))
	}
	if m := shapes[0].Style.Margin; m != [4]css.Dimension{css.Pts(20), css.Pts(0), css.Pts(30), css.Pts(10)} {
		t.Errorf("margins %v, want 20 top, 30 bottom, 10 left", m)
	}
}
