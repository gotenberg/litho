package pptx

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// pngBytes returns a 4 by 2 red PNG.
func pngBytes(t testing.TB) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// sp returns an autoshape named name of geometry and properties spPr
// content, with an optional p:style.
func sp(name, spPr, style string) string {
	return `<p:sp><p:nvSpPr><p:cNvPr id="3" name="` + name + `"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm rot="5400000" flipH="1"><a:off x="12700" y="25400"/><a:ext cx="127000" cy="254000"/></a:xfrm>` +
		spPr + `</p:spPr>` + style + `</p:sp>`
}

func TestShapes_FillsAndLines(t *testing.T) {
	t.Parallel()
	style := `<p:style><a:lnRef idx="2"><a:schemeClr val="accent2"/></a:lnRef><a:fillRef idx="3"><a:schemeClr val="accent1"/></a:fillRef><a:effectRef idx="0"><a:schemeClr val="accent1"/></a:effectRef><a:fontRef idx="minor"/></p:style>`
	for _, tc := range []struct {
		name, spPr, style string
		check             func(t *testing.T, s *slides.Shape)
	}{
		{"a style reference's colour stands for phClr without its alpha", "", `<p:style><a:lnRef idx="0"/><a:fillRef idx="1"><a:srgbClr val="FF0000"><a:alpha val="50000"/></a:srgbClr></a:fillRef><a:effectRef idx="0"/><a:fontRef idx="minor"/></p:style>`, func(t *testing.T, s *slides.Shape) {
			if s.Fill.Kind != slides.FillSolid || s.Fill.Color != (css.Color{R: 1, A: 1}) {
				t.Errorf("fill %+v", s.Fill)
			}
		}},
		{"a line of zero width is a hairline", `<a:ln w="0"><a:solidFill><a:srgbClr val="000000"/></a:solidFill></a:ln>`, "", func(t *testing.T, s *slides.Shape) {
			if !s.Line.Hairline || s.Line.Width <= 0 {
				t.Errorf("line %+v", s.Line)
			}
		}},
		{"solid fill and dashed line with arrows", `<a:prstGeom prst="roundRect"><a:avLst><a:gd name="adj" fmla="val 25000"/></a:avLst></a:prstGeom><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill>` +
			`<a:ln w="25400" cap="rnd"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:prstDash val="dash"/><a:bevel/><a:headEnd type="triangle" w="lg" len="sm"/><a:tailEnd type="oval"/></a:ln>`, "", func(t *testing.T, s *slides.Shape) {
			if s.Geometry == nil || s.Geometry.Preset != "roundRect" || len(s.Geometry.Adjust) != 1 || s.Geometry.Adjust[0].Value != 25000 {
				t.Errorf("geometry %+v", s.Geometry)
			}
			if s.Fill.Kind != slides.FillSolid || !sameColor(s.Fill.Color, rgb(0x00FF00)) {
				t.Errorf("fill %+v", s.Fill)
			}
			l := s.Line
			if l.Width != 2 || !slices.Equal(l.Dash, []float64{8, 6}) || l.Cap != slides.CapRound || l.Join != slides.JoinBevel {
				t.Errorf("line %+v", l)
			}
			if l.Head != (slides.Arrow{Kind: slides.ArrowTriangle, Width: 2, Length: 0.5}) || l.Tail.Kind != slides.ArrowOval {
				t.Errorf("arrows %+v %+v", l.Head, l.Tail)
			}
			if s.Box != (compose.Rect{X: 1, Y: 2, Width: 10, Height: 20}) || s.Rotation != 90 || !s.FlipH {
				t.Errorf("placement %+v %v %v", s.Box, s.Rotation, s.FlipH)
			}
		}},
		{"style matrix", `<a:prstGeom prst="ellipse"/>`, style, func(t *testing.T, s *slides.Shape) {
			// fillRef 3 is the theme's shaded solid fill of accent1.
			if s.Fill.Kind != slides.FillSolid || sameColor(s.Fill.Color, rgb(0x4F81BD)) {
				t.Errorf("fill %+v", s.Fill)
			}
			if s.Line.Width != 2 || !sameColor(s.Line.Color, rgb(0xC0504D)) {
				t.Errorf("line %+v", s.Line)
			}
		}},
		{"own properties over the style", `<a:prstGeom prst="rect"/><a:noFill/><a:ln><a:noFill/></a:ln>`, style, func(t *testing.T, s *slides.Shape) {
			if s.Fill.Kind != slides.FillNone || s.Line.Width != 0 || s.Geometry != nil {
				t.Errorf("shape %+v", s)
			}
		}},
		{"gradient", `<a:gradFill><a:gsLst><a:gs pos="100000"><a:srgbClr val="0000FF"/></a:gs><a:gs pos="0"><a:srgbClr val="FF0000"/></a:gs></a:gsLst><a:lin ang="5400000"/></a:gradFill>`, "", func(t *testing.T, s *slides.Shape) {
			g := s.Fill.Gradient
			if s.Fill.Kind != slides.FillGradient || g.Angle != 90 || g.Radial || len(g.Stops) != 2 || !sameColor(g.Stops[0].Color, rgb(0xFF0000)) {
				t.Errorf("fill %+v %+v", s.Fill, g)
			}
		}},
		{"radial gradient", `<a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="FF0000"/></a:gs></a:gsLst><a:path path="circle"/></a:gradFill>`, "", func(t *testing.T, s *slides.Shape) {
			if g := s.Fill.Gradient; !g.Radial || len(g.Stops) != 2 || g.CenterX != 0.5 || g.CenterY != 0.5 {
				t.Errorf("gradient %+v", g)
			}
		}},
		{"radial gradient from a corner", `<a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="FF0000"/></a:gs></a:gsLst><a:path path="circle"><a:fillToRect l="100000" t="100000"/></a:path></a:gradFill>`, "", func(t *testing.T, s *slides.Shape) {
			if g := s.Fill.Gradient; !g.Radial || g.CenterX != 1 || g.CenterY != 1 {
				t.Errorf("gradient %+v", g)
			}
		}},
		{"pattern", `<a:pattFill prst="dkUpDiag"><a:fgClr><a:srgbClr val="FF0000"/></a:fgClr><a:bgClr><a:srgbClr val="00FF00"/></a:bgClr></a:pattFill>`, "", func(t *testing.T, s *slides.Shape) {
			f := s.Fill
			if f.Kind != slides.FillPattern || f.Pattern != "dkUpDiag" || !sameColor(f.Color, rgb(0xFF0000)) || !sameColor(f.Background, rgb(0x00FF00)) {
				t.Errorf("fill %+v", f)
			}
		}},
		{"picture fill tiled", `<a:blipFill><a:blip r:embed="rIdP"/><a:tile/></a:blipFill>`, "", func(t *testing.T, s *slides.Shape) {
			if s.Fill.Kind != slides.FillPicture || s.Fill.Picture == nil || !s.Fill.Tile {
				t.Errorf("fill %+v", s.Fill)
			}
		}},
		{"custom geometry", `<a:custGeom><a:pathLst/></a:custGeom>`, "", func(t *testing.T, s *slides.Shape) {
			// The shared geometry reader parses the custom geometry.
			if s.Geometry == nil || s.Geometry.Preset != "" || s.Geometry.Custom == nil {
				t.Errorf("geometry %+v", s.Geometry)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := readDeck(t, testDeck{
				slides: []testSlide{{tree: sp("S", tc.spPr, tc.style), rels: `<Relationship Id="rIdP" Type="` + tRelImage + `" Target="../media/image1.png"/>`}},
				parts:  map[string]string{"ppt/media/image1.png": pngBytes(t)},
			})
			tc.check(t, doc.Slides[0].Shapes[0])
		})
	}
}

func TestShapes_BackgroundGroupPictureConnector(t *testing.T) {
	t.Parallel()
	pic := `<p:pic><p:nvPicPr><p:cNvPr id="4" name="Pic"><a:hlinkClick r:id="rIdH"/></p:cNvPr><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="rIdP"/><a:srcRect l="25000"/><a:stretch/></p:blipFill>` +
		`<p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="38100" cy="25400"/></a:xfrm><a:prstGeom prst="rect"/></p:spPr></p:pic>`
	round := `<p:pic><p:nvPicPr><p:cNvPr id="5" name="Round"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="rIdP"/></p:blipFill>` +
		`<p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="38100" cy="25400"/></a:xfrm><a:prstGeom prst="ellipse"/></p:spPr></p:pic>`
	group := `<p:grpSp><p:nvGrpSpPr><p:cNvPr id="6" name="G"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="127000" y="127000"/><a:ext cx="254000" cy="254000"/><a:chOff x="0" y="0"/><a:chExt cx="127000" cy="127000"/></a:xfrm><a:solidFill><a:srgbClr val="FFFF00"/></a:solidFill></p:grpSpPr>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="7" name="Member"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="12700" y="0"/><a:ext cx="12700" cy="12700"/></a:xfrm><a:prstGeom prst="rect"/><a:grpFill/></p:spPr></p:sp></p:grpSp>`
	cxn := `<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="8" name="Conn"/><p:cNvCxnSpPr/><p:nvPr/></p:nvCxnSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="12700" cy="0"/></a:xfrm><a:prstGeom prst="straightConnector1"/><a:ln w="12700"><a:solidFill><a:srgbClr val="000000"/></a:solidFill><a:tailEnd type="arrow"/></a:ln></p:spPr></p:cxnSp>`
	doc, _ := readDeck(t, testDeck{
		masterBg: `<p:bg><p:bgRef idx="1001"><a:schemeClr val="bg2"/></p:bgRef></p:bg>`,
		slides: []testSlide{
			{tree: pic + round + group + cxn, rels: `<Relationship Id="rIdP" Type="` + tRelImage + `" Target="../media/image1.png"/>` +
				`<Relationship Id="rIdH" Type="` + tRelLink + `" Target="https://example.com/pic" TargetMode="External"/>`},
			{tree: title("x"), bg: `<p:bg><p:bgPr><a:solidFill><a:srgbClr val="123456"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>`},
		},
		parts: map[string]string{"ppt/media/image1.png": pngBytes(t)},
	})
	s := doc.Slides[0]
	if f := s.Background; f.Kind != slides.FillSolid || !sameColor(f.Color, rgb(0xEEECE1)) {
		t.Errorf("master background %+v", f)
	}
	if f := doc.Slides[1].Background; !sameColor(f.Color, rgb(0x123456)) {
		t.Errorf("slide background %+v", f)
	}
	p := shapeNamed(s.Shapes, "Pic")
	if p == nil || p.Kind != slides.KindPicture || p.Picture == nil || p.Href != "https://example.com/pic" {
		t.Fatalf("picture %+v", p)
	}
	// A quarter cut off the left of a 3-point-wide picture.
	if w := p.Picture.Width; w < 2.24 || w > 2.26 {
		t.Errorf("cropped width %v", w)
	}
	if r := shapeNamed(s.Shapes, "Round"); r == nil || r.Kind != slides.KindGeometry || r.Fill.Kind != slides.FillPicture || r.Geometry.Preset != "ellipse" {
		t.Errorf("round picture %+v", r)
	}
	g := shapeNamed(s.Shapes, "G")
	if g == nil || g.Kind != slides.KindGroup || g.Box != (compose.Rect{X: 10, Y: 10, Width: 20, Height: 20}) || g.Child != (compose.Rect{Width: 10, Height: 10}) {
		t.Fatalf("group %+v", g)
	}
	if m := g.Kids[0]; m.Box != (compose.Rect{X: 1, Width: 1, Height: 1}) || !sameColor(m.Fill.Color, rgb(0xFFFF00)) {
		t.Errorf("member %+v", m)
	}
	c := shapeNamed(s.Shapes, "Conn")
	if c == nil || c.Geometry.Preset != "straightConnector1" || c.Line.Width != 1 || c.Line.Tail.Kind != slides.ArrowOpen {
		t.Errorf("connector %+v", c)
	}
}

func TestShapes_Table(t *testing.T) {
	t.Parallel()
	cell := func(text, attrs, pr string) string {
		return `<a:tc ` + attrs + `><a:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></a:txBody><a:tcPr ` + pr + `</a:tc>`
	}
	frame := func(tblPr string) string {
		return `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="3" name="T"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="0" y="0"/><a:ext cx="254000" cy="254000"/></p:xfrm>` +
			`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl>` + tblPr + `<a:tblGrid><a:gridCol w="127000"/><a:gridCol w="127000"/></a:tblGrid>` +
			`<a:tr h="127000">` + cell("A", `gridSpan="2"`, `marL="0" anchor="ctr"/>`) + cell("", `hMerge="1"`, `/>`) + `</a:tr>` +
			`<a:tr h="127000">` + cell("C", "", `><a:lnL w="25400"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></a:lnL><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></a:tcPr>`) + cell("D", "", "/>") + `</a:tr>` +
			`<a:tr h="127000">` + cell("E", "", "/>") + cell("F", "", "/>") + `</a:tr>` +
			`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`
	}
	// The default Medium Style 2 - Accent 1 as Impress draws it.
	s := oneSlide(t, frame(`<a:tblPr firstRow="1" bandRow="1"><a:tableStyleId>{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}</a:tableStyleId></a:tblPr>`))
	tb := s.Shapes[0].Table
	if s.Shapes[0].Kind != slides.KindTable || tb == nil || !slices.Equal(tb.ColumnWidths, []float64{10, 10}) || len(tb.Rows) != 3 || tb.Rows[0].Height != 10 {
		t.Fatalf("table %+v", tb)
	}
	a, b := tb.Rows[0].Cells[0], tb.Rows[0].Cells[1]
	if a.ColSpan != 2 || !b.Covered || a.Text.Insets[3] != 0 || a.Text.Anchor != slides.AnchorMiddle {
		t.Errorf("spanning cell %+v, covered %+v", a, b)
	}
	if !sameColor(a.Fill.Color, rgb(0x4F81BD)) || !sameColor(a.Text.Paragraphs[0].Runs[0].Style.Color, rgb(0xFFFFFF)) {
		t.Errorf("header %+v %+v", a.Fill, a.Text.Paragraphs[0].Runs[0].Style)
	}
	c, d, e := tb.Rows[1].Cells[0], tb.Rows[1].Cells[1], tb.Rows[2].Cells[0]
	if !sameColor(c.Fill.Color, rgb(0x00FF00)) || c.Borders[3].Width != 2 || !sameColor(c.Borders[3].Color, rgb(0xFF0000)) {
		t.Errorf("own cell properties %+v", c)
	}
	if !sameColor(d.Fill.Color, rgb(0xD0D8E7)) || !sameColor(e.Fill.Color, rgb(0xE9ECF3)) {
		t.Errorf("bands %+v %+v", d.Fill, e.Fill)
	}
	if d.Borders[0].Width != 1 || !sameColor(d.Borders[0].Color, rgb(0xFFFFFF)) {
		t.Errorf("inside border %+v", d.Borders[0])
	}
	if r := d.Text.Paragraphs[0].Runs[0].Style; !sameColor(r.Color, rgb(0)) || r.Size != 18 {
		t.Errorf("body text %+v", r)
	}
	// A table naming no style takes the presentation's default; one naming
	// an unknown style prints without one.
	s = oneSlide(t, frame(`<a:tblPr><a:tableStyleId>{00000000-0000-0000-0000-000000000000}</a:tableStyleId></a:tblPr>`))
	if f := s.Shapes[0].Table.Rows[2].Cells[1].Fill; f.Kind != slides.FillNone {
		t.Errorf("unknown style fills %+v", f)
	}
	doc, _ := readDeck(t, testDeck{
		slides: []testSlide{{tree: frame(`<a:tblPr/>`)}},
		parts: map[string]string{"ppt/tableStyles.xml": `<a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" def="{AB}">` +
			`<a:tblStyle styleId="{AB}" styleName="x"><a:wholeTbl><a:tcTxStyle b="on"><a:schemeClr val="accent2"/></a:tcTxStyle><a:tcStyle><a:fill><a:solidFill><a:srgbClr val="ABCDEF"/></a:solidFill></a:fill></a:tcStyle></a:wholeTbl></a:tblStyle></a:tblStyleLst>`},
	})
	f := doc.Slides[0].Shapes[0].Table.Rows[2].Cells[1]
	if !sameColor(f.Fill.Color, rgb(0xABCDEF)) || !f.Text.Paragraphs[0].Runs[0].Style.Bold || !sameColor(f.Text.Paragraphs[0].Runs[0].Style.Color, rgb(0xC0504D)) {
		t.Errorf("default style cell %+v %+v", f.Fill, f.Text.Paragraphs[0].Runs[0].Style)
	}
}

func TestShapes_NamesAndSymbolBullets(t *testing.T) {
	t.Parallel()
	doc, _ := readDeck(t, testDeck{slides: []testSlide{
		{tree: title("Hello")}, {tree: title("Hello")}, {tree: textBox("TB", 0, 0, 1, 1, `<a:bodyPr/><a:p><a:pPr marL="0"><a:buFont typeface="Wingdings"/><a:buChar char="q"/></a:pPr><a:r><a:t>x</a:t></a:r></a:p>`)},
		{tree: title("Hello")},
	}})
	var names []string
	for _, s := range doc.Slides {
		names = append(names, s.Name)
	}
	if want := []string{"Hello", "Hello (2)", "Slide 3", "Hello (3)"}; !slices.Equal(names, want) {
		t.Errorf("names %q, want %q", names, want)
	}
	if b := doc.Slides[2].Shapes[0].Text.Paragraphs[0].Bullet; b == nil || b.Char != "\uf071" || !slices.Equal(b.Families, []string{"OpenSymbol"}) {
		t.Errorf("bullet %+v", b)
	}
}

func TestShapes_LayoutColorMapOverride(t *testing.T) {
	t.Parallel()
	files := deckFiles(testDeck{slides: []testSlide{{tree: textBox("TB", 0, 0, 1, 1, para("x"))}}})
	files["ppt/slideLayouts/slideLayout1.xml"] = `<p:sldLayout ` + nsDecl + `><p:cSld><p:spTree>` + spGroup + defaultLayoutTree + `</p:spTree></p:cSld>` +
		`<p:clrMapOvr><a:overrideClrMapping bg1="dk1" tx1="lt1" bg2="dk2" tx2="lt2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/></p:clrMapOvr></p:sldLayout>`
	d := loadFiles(t, files)
	if c := d.Slides[0].Shapes[0].Text.Paragraphs[0].Runs[0].Style.Color; !sameColor(c, rgb(0xFFFFFF)) {
		t.Errorf("a slide takes its layout's colour map: %+v", c)
	}
}

func TestFillRectCrop(t *testing.T) {
	cases := []struct {
		name, fillRect string
		want           [4]float64
	}{
		{"no fill rectangle", ``, [4]float64{}},
		{"a picture wider than the box is cut", `<a:fillRect l="-25000" r="-25000"/>`, [4]float64{css.Left: 0.25 / 1.5, css.Right: 0.25 / 1.5}},
		{"an inset leaves blank room", `<a:fillRect t="10000" b="10000"/>`, [4]float64{css.Top: -0.1 / 0.8, css.Bottom: -0.1 / 0.8}},
		{"an empty rectangle draws the picture whole", `<a:fillRect l="60000" r="60000"/>`, [4]float64{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var fr *ooxml.Element
			if c.fillRect != "" {
				root, err := ooxml.Parse(context.Background(), []byte(`<a:stretch xmlns:a="`+ooxml.NSA+`">`+c.fillRect+`</a:stretch>`), nil)
				if err != nil {
					t.Fatal(err)
				}
				fr = root.Child(ooxml.NSA, "fillRect")
			}
			got := fillRectCrop(fr)
			for i := range got {
				if d := got[i] - c.want[i]; d > 1e-9 || d < -1e-9 {
					t.Fatalf("crop %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestShapes_Jumps(t *testing.T) {
	link := func(name, action, id string) string {
		h := `<a:hlinkClick r:id="` + id + `" action="` + action + `"/>`
		return `<p:sp><p:nvSpPr><p:cNvPr id="3" name="` + name + `">` + h + `</p:cNvPr><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="12700" cy="12700"/></a:xfrm></p:spPr></p:sp>`
	}
	tree := link("next", "ppaction://hlinkshowjump?jump=nextslide", "") + link("previous", "ppaction://hlinkshowjump?jump=previousslide", "") +
		link("first", "ppaction://hlinkshowjump?jump=firstslide", "") + link("last", "ppaction://hlinkshowjump?jump=lastslide", "") +
		link("third", "ppaction://hlinksldjump", "rIdJ") + link("none", "ppaction://hlinkshowjump?jump=endshow", "")
	doc, _ := readDeck(t, testDeck{slides: []testSlide{
		{tree: link("before first", "ppaction://hlinkshowjump?jump=previousslide", "")},
		{tree: tree, rels: `<Relationship Id="rIdJ" Type="` + tRelSlide + `" Target="slide3.xml"/>`},
		{},
	}})
	for name, want := range map[string]int{"next": 3, "previous": 1, "first": 1, "last": 3, "third": 3, "none": 0} {
		if s := shapeNamed(doc.Slides[1].Shapes, name); s == nil || s.Jump != want {
			t.Errorf("%s: shape %+v, want a jump to %d", name, s, want)
		}
	}
	if s := shapeNamed(doc.Slides[0].Shapes, "before first"); s == nil || s.Jump != 0 {
		t.Errorf("a jump before the first slide: %+v", s)
	}
}

func TestShapes_CameraTurn(t *testing.T) {
	t.Parallel()
	scene := func(prst, rot string) string {
		return `<a:scene3d><a:camera prst="` + prst + `">` + rot + `</a:camera><a:lightRig rig="threePt" dir="t"/></a:scene3d>`
	}
	for _, tc := range []struct {
		name, spPr string
		want       float64
	}{
		{"a front camera's revolution turns the shape back", scene("orthographicFront", `<a:rot lat="0" lon="0" rev="5400000"/>`), 0},
		{"a front camera without a rotation", scene("orthographicFront", ""), 90},
		{"a tilted camera is drawn flat", scene("orthographicFront", `<a:rot lat="1200000" lon="0" rev="5400000"/>`), 90},
		{"an isometric camera is drawn flat", scene("isometricRightUp", ""), 90},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := oneSlide(t, sp("S", tc.spPr, ""))
			if got := s.Shapes[0].Rotation; got != tc.want {
				t.Errorf("rotation %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShapes_GradientTurn(t *testing.T) {
	t.Parallel()
	grad := func(attrs string) string {
		return `<a:gradFill ` + attrs + `><a:gsLst><a:gs pos="0"><a:srgbClr val="FFFF00"/></a:gs><a:gs pos="100000"><a:srgbClr val="00B0F0"/></a:gs></a:gsLst><a:lin ang="0"/></a:gradFill>`
	}
	camera := `<a:scene3d><a:camera prst="orthographicFront"><a:rot lat="0" lon="0" rev="5400000"/></a:camera><a:lightRig rig="threePt" dir="t"/></a:scene3d>`
	for _, tc := range []struct {
		name, spPr string
		want       float64
	}{
		{"a gradient turns with its shape", grad(""), 0},
		{"a gradient that does not rotate with the shape keeps the page's direction", grad(`rotWithShape="0"`), 270},
		{"a camera's turn does not turn the gradient", grad("") + camera, 90},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := oneSlide(t, sp("S", tc.spPr, "")).Shapes[0]
			if s.Fill.Gradient == nil || s.Fill.Gradient.Angle != tc.want {
				t.Errorf("gradient %+v, want an angle of %v", s.Fill.Gradient, tc.want)
			}
		})
	}
}

func TestShapes_BackgroundFill(t *testing.T) {
	t.Parallel()
	shape := `<p:sp useBgFill="1"><p:nvSpPr><p:cNvPr id="3" name="Bg"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="12700" cy="12700"/></a:xfrm></p:spPr>` +
		`<p:style><a:lnRef idx="0"/><a:fillRef idx="1"><a:schemeClr val="accent1"/></a:fillRef><a:effectRef idx="0"/><a:fontRef idx="minor"/></p:style></p:sp>`
	doc, _ := readDeck(t, testDeck{slides: []testSlide{{tree: shape, bg: `<p:bg><p:bgPr><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>`}}})
	if f := doc.Slides[0].Shapes[0].Fill; f.Kind != slides.FillSolid || f.Color != (css.Color{G: 1, A: 1}) {
		t.Errorf("fill %+v, want the slide's green background", f)
	}
	doc, _ = readDeck(t, testDeck{slides: []testSlide{{tree: shape}}})
	if f := doc.Slides[0].Shapes[0].Fill; f.Kind != slides.FillSolid || f.Color != (css.Color{R: 1, G: 1, B: 1, A: 1}) {
		t.Errorf("fill %+v, want white for a slide without a background", f)
	}
}
