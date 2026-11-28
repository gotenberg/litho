package docx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// The graphic data URIs (ECMA-376 Part 1, 20.1.2.2.17) the converter
// tells apart.
const (
	uriPicture = "http://schemas.openxmlformats.org/drawingml/2006/picture"
	uriShape   = "http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
	uriGroup   = "http://schemas.microsoft.com/office/word/2010/wordprocessingGroup"
	uriChart   = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	uriDiagram = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
	// uriChartEx is an Office 2016 chart (MS-ODRAWXML, 2.24 cx:chart).
	uriChartEx = ooxml.NSChartEx
)

// chart builds the chart a graphic frame's c:chart or cx:chart element
// names through its r:id (ECMA-376 Part 1, 21.2.2.27; MS-ODRAWXML, 2.24.3):
// a Figure replaced box of w by h points that draws the chart part's
// model, read by [ooxml.ParseChart], or [ooxml.ParseChartEx] when ex is
// set. It returns nil with a warning when the part is missing or does not
// parse, the frame then keeping its room empty.
//
// The Figure carries alt, the description of its drawing. Without one it
// carries the chart's title, as the chart of a sheet or a slide does: the
// title is what the source states of the chart for a reader, and a Figure
// needs a description (ISO 14289-1, 7.3; ISO 14289-2, 8.2.5.28.2). Word's
// PDF tags such a chart as a Figure without one. With
// office.Options.DerivedAltText, an undescribed chart carries the
// description [chart.AltText] derives from the chart's model instead, its
// title first, in English, which the Figure states when the document's
// language is another ([chart.AltTextLang]).
//
// An Office 2016 chart sits in an mc:AlternateContent whose Choice
// requires the cx1 or cx2 namespace; the converter takes the Choice, as
// Word draws the chart and not the Fallback picture ([understood]).
//
// # Corpus evidence
//
//   - litho/docx charts-bar-column, charts-line-area,
//     charts-scatter-bubble-radar, charts-stock-combo: Word draws each
//     chart part; drawn, they pass, their layout from 0 to 0.76 to 0.87.
//   - litho/docx charts-chartex: Word prints the titles, legends and axis
//     labels of the Office 2016 charts and not their fallback pictures;
//     the Choice raises text F1 from 0.11 to 0.50.
//   - litho/docx charts-bar-column: Word's PDF tags its seven titled
//     charts as Figures without Alt.
func (d *document) chart(s *story, ref *ooxml.Element, st *css.Style, w, h float64, alt string, ex bool) *layout.Node {
	id, _ := ref.AttrNS(ooxml.NSR, "id")
	rel, ok := d.pkg.RelationshipByID(d.ctx, s.part, id)
	if !ok || rel.External || !d.pkg.Has(rel.Target) {
		d.warn(warn.PartMalformed, warn.Omitted, "a chart whose part is missing is left out")
		return nil
	}
	if w <= 0 || h <= 0 {
		return nil
	}
	parse := ooxml.ParseChart
	if ex {
		parse = ooxml.ParseChartEx
	}
	c, err := parse(d.ctx, d.pkg, rel.Target)
	if err != nil {
		if d.stop(err) {
			return nil
		}
		d.warn(warn.PartMalformed, warn.Omitted, fmt.Sprintf("chart %s does not parse and is left out: %v", rel.Target, err))
		return nil
	}
	for _, cw := range c.Warnings {
		d.warn(cw.Code, cw.Effect, cw.Message)
	}
	// Word draws some parts of a chart otherwise than Excel and
	// PowerPoint (chart.Chart.Word).
	c.Word = true
	ps := inlineStyle(st)
	ps.Display = css.Inline
	ps.Width, ps.Height = css.Pts(w), css.Pts(h)
	ps.VerticalAlign = css.VerticalAlign{}
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	ps.MinLineHeight = 0
	fonts := d.fonts
	pic := &layout.Picture{Width: w, Height: h}
	pic.Vector = func(ctx context.Context, r typeset.Rect) []typeset.Fragment {
		return chart.Render(ctx, fonts, c, r)
	}
	n := &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic, Role: "Figure", Alt: alt}
	switch {
	case alt != "":
	case d.opts.DerivedAltText:
		// The node holds no text: its language is its description's.
		n.Alt, n.Lang = chart.AltText(c), chart.AltTextLang(d.lang())
	case c.Title != nil:
		n.Alt = strings.Join(strings.Fields(c.Title.Text), " ")
	}
	return n
}

// drawing builds a w:drawing (17.3.3.9): each wp:inline as an inline box,
// each wp:anchor placed by its wrapping (20.4.2.3).
func (d *document) drawing(s *story, c *ooxml.Element, st *css.Style, sink *inlineSink, depth int) {
	for _, k := range c.Kids() {
		inline := k.Is(ooxml.NSWP, "inline")
		if !inline && !k.Is(ooxml.NSWP, "anchor") {
			continue
		}
		var wpt, hpt float64
		if ext := k.Child(ooxml.NSWP, "extent"); ext != nil {
			wpt, _ = ooxml.EMU(attrOr(ext, "cx"))
			hpt, _ = ooxml.EMU(attrOr(ext, "cy"))
		}
		if !inline {
			wpt, hpt = d.relativeSize(k, wpt, hpt)
		}
		alt, decorative := ooxml.Describe(k.Child(ooxml.NSWP, "docPr"))
		data := k.Child(ooxml.NSA, "graphic").Child(ooxml.NSA, "graphicData")
		n := d.graphic(s, data, st, max(wpt, 0), max(hpt, 0), alt, depth)
		if n == nil {
			continue
		}
		if decorative && n.Role == "Figure" {
			// A drawing its author marked decorative is an artifact
			// (ISO 32000-1, 14.8.2.2.2; ISO 14289-1, 7.1).
			n.Role, n.Alt = "", ""
		}
		if caption, ok := d.captions[k]; ok && n.Role == "Figure" && n.Alt == "" && n.Kind == layout.Replaced {
			n.Alt = caption
		}
		if !inline {
			d.anchor(k, n, wpt, hpt, sink)
			sink.add(n)
			continue
		}
		onBaseline(n)
		effectExtent(n, k.Child(ooxml.NSWP, "effectExtent"))
		if href := d.drawingLink(s, k.Child(ooxml.NSWP, "docPr")); href != "" {
			sink.openLink(href)
			sink.add(n)
			sink.closeLink()
			continue
		}
		sink.add(n)
	}
}

// onBaseline sits the inline drawing n on its line by its bottom edge,
// as Word sets every inline drawing (ECMA-376 Part 1, 20.4.2.8 inline),
// a text box included, whose box would else align by its top or its
// last line.
//
// # Corpus evidence
//
//   - libreoffice/docx layout/inline-textbox-position.docx: Word sets an
//     inline text box 63 points tall beside one 189 points tall with
//     both bottom edges on the line's baseline, each box's one line at
//     its top.
func onBaseline(n *layout.Node) {
	if n.Style == nil || n.Style.Display != css.InlineBlock {
		return
	}
	n.Style.VerticalAlign = css.VerticalAlign{}
	n.Style.BaselineAtBottom = true
}

// maxEffectExtent bounds each side of an inline drawing's effect extent,
// in points. Anti-DoS guard, not spec-mandated: Word writes a few dozen
// points for a turned picture or a shadow, and a page is at most 1,584
// points wide.
const maxEffectExtent = 1584

// effectExtent grows the room the inline drawing n takes in its line by
// its wp:effectExtent e (ECMA-376 Part 1, 20.4.2.6): the extent its
// effects and its rotation add on each side, which Word lays out as part
// of the drawing, the drawing set that far inside it.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/effect-extent-inline.docx and
//     fdo70457.docx: Word sets a turned inline picture's box its effect
//     extent's left side right of the line's start, and the line its top
//     and bottom sides taller.
func effectExtent(n *layout.Node, e *ooxml.Element) {
	if e == nil || n.Style == nil {
		return
	}
	for side, name := range map[css.Side]string{css.Top: "t", css.Right: "r", css.Bottom: "b", css.Left: "l"} {
		if v, ok := ooxml.EMU(attrOr(e, name)); ok && v != 0 {
			n.Style.Margin[side] = css.Pts(max(-maxEffectExtent, min(v, maxEffectExtent)))
		}
	}
}

// drawingLink returns the target of the hyperlink an inline drawing's
// wp:docPr carries (20.4.2.5 docPr; 20.1.2.2.23 hlinkClick, its r:id
// naming an external relationship), "" for none.
//
// # Corpus evidence
//
//   - libreoffice/docx: ooxmlimport/image-hyperlink.docx: Word writes a
//     link annotation over an inline picture whose docPr holds an
//     a:hlinkClick.
func (d *document) drawingLink(s *story, pr *ooxml.Element) string {
	h := pr.Child(ooxml.NSA, "hlinkClick")
	if h == nil {
		return ""
	}
	id, ok := h.AttrNS(ooxml.NSR, "id")
	if !ok {
		return ""
	}
	if rel, ok := d.pkg.RelationshipByID(d.ctx, s.part, id); ok && rel.External {
		return rel.Target
	}
	return ""
}

// relativeSize returns the size of the anchored drawing k whose extent is
// w by h points: a wp14:sizeRelH or wp14:sizeRelV whose wp14:pctWidth or
// wp14:pctHeight is positive sizes that side as that share, in
// thousandths of a percent, of the area its relativeFrom names: the page,
// the area inside the margins, or a margin (MS-ODRAWXML, 2.3.1.2 and
// 2.3.1.3; ST_SizeRelFromH and ST_SizeRelFromV). A share of 0 keeps the
// extent, as does a drawing whose page is not known when it is built,
// outside the body and its headers and footers.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/dml-shape-relsize.docx: Word draws a
//     shape of extent 88 by 63 points, pctWidth 40000 and pctHeight 20000
//     of the margin, 189 by 130 points, its outline included, on a Letter
//     page of 1417-twip margins: 40 and 20 per cent of 470 by 650.
//   - libreoffice/docx ooxmlexport/dml-groupshape-relsize.docx: a group
//     sized so scales its shapes with it.
//   - libreoffice/docx ooxmlexport/dml-rectangle-relsize.docx: a shape of
//     pctHeight 0 keeps its extent's height; one of pctHeight 70000 of the
//     page is 70 per cent of the page tall.
//   - libreoffice/docx ooxmlexport/effectextent-margin.docx departs: its
//     text box of extent 159.5 points, pctWidth 40000 of a 415.3-point
//     margin, prints 147 points wide, neither size; not explained.
func (d *document) relativeSize(k *ooxml.Element, w, h float64) (float64, float64) {
	if d.page == nil {
		return w, h
	}
	if rel := k.Child(ooxml.NSWP14, "sizeRelH"); rel != nil {
		if pct, ok := pctPosition(rel, "pctWidth"); ok && pct > 0 {
			_, aw := hArea(d.page, attrOr(rel, "relativeFrom"))
			w = float64(pct * aw)
		}
	}
	if rel := k.Child(ooxml.NSWP14, "sizeRelV"); rel != nil {
		if pct, ok := pctPosition(rel, "pctHeight"); ok && pct > 0 {
			_, ah := vArea(d.page, attrOr(rel, "relativeFrom"))
			h = float64(pct * ah)
		}
	}
	return w, h
}

// graphic builds the content of a graphic frame of size w by h points.
func (d *document) graphic(s *story, data *ooxml.Element, st *css.Style, w, h float64, alt string, depth int) *layout.Node {
	if data == nil {
		return nil
	}
	switch ooxml.GraphicURI(data) {
	case uriPicture:
		if pic := data.Child(ooxml.NSPic, "pic"); pic != nil {
			n := d.picture(s, ooxml.ParsePicture(pic), st, w, h, alt)
			if n != nil {
				n.Picture = d.withEffects(n.Picture, d.effectList(pic.Child(ooxml.NSPic, "spPr"), nil))
			}
			return n
		}
	case uriShape:
		if sp := data.Child(ooxml.NSWPS, "wsp"); sp != nil {
			return d.shape(s, sp, st, w, h, alt, depth)
		}
	case uriGroup:
		if g := data.Child(ooxml.NSWPG, "wgp"); g != nil {
			return d.group(s, g, st, w, h, depth)
		}
	case uriCanvas:
		if c := data.Child(nsWPC, "wpc"); c != nil {
			return d.canvas(s, c, st, w, h, depth)
		}
	case uriLockedCanvas:
		if c := data.Child(uriLockedCanvas, "lockedCanvas"); c != nil {
			return d.lockedCanvas(s, c, st, w, h, depth)
		}
	case uriChart:
		if n := d.chart(s, data.Child(ooxml.NSChart, "chart"), st, w, h, alt, false); n != nil {
			return n
		}
	case uriChartEx:
		if n := d.chart(s, data.Child(ooxml.NSChartEx, "chart"), st, w, h, alt, true); n != nil {
			return n
		}
	case uriDiagram:
		return d.diagram(s, data, st, w, h, depth)
	default:
		d.warn(warn.ContentUnsupported, warn.Omitted, "a drawing of an unknown kind is left out")
	}
	return box(st, w, h)
}

// box returns an empty inline box of w by h points, which keeps the room a
// drawing takes.
func box(st *css.Style, w, h float64) *layout.Node {
	bs := inlineStyle(st)
	bs.Display = css.InlineBlock
	bs.Width, bs.Height = css.Pts(w), css.Pts(h)
	return &layout.Node{Kind: layout.Element, Style: bs}
}

// linkPlaceholder returns the box Word prints for a picture that only
// links to its file (ECMA-376 Part 1, 20.1.8.13 blip link with no embed):
// the extent filled white inside a black frame a quarter of a point wide.
// Word adds an icon and a notice that the linked image cannot be
// displayed, drawn as glyph outlines a few points tall, which the box
// leaves out.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlimport/linked-picture.docx: Word prints the
//     150 by 75 point extent of its linked picture filled white, framed by
//     a 0.25 point black line inside its edge.
//   - libreoffice/docx ooxmlexport/FileWithInvalidImageLink.docx and
//     litho/docx images-svg-linked: Word prints the same frame for a
//     linked picture whose file cannot be found.
func linkPlaceholder(st *css.Style, w, h float64) *layout.Node {
	n := box(st, w, h)
	n.Style.BoxSizing = css.BorderBox
	n.Style.BackgroundColor = css.Color{R: 1, G: 1, B: 1, A: 1}
	for i := range n.Style.Border {
		n.Style.Border[i] = css.Border{Width: 0.25, Style: css.Solid, Color: css.Color{A: 1}}
	}
	// Word strokes the frame as a line, not a filled band.
	strokeBorders(n.Style)
	return n
}

// picture builds a picture of size w by h points (0 for its natural size)
// from its reference: a Figure replaced box, cropped, rotated and flipped
// as the reference says.
//
// A picture is a Figure, its alt the description its drawing or its own
// properties carry; one its properties mark decorative is an artifact
// (ISO 32000-1, 14.8.2.2.2). A picture described nowhere stays a Figure
// without one: the source holds nothing to say about it.
func (d *document) picture(s *story, ref ooxml.PictureRef, st *css.Style, w, h float64, alt string) *layout.Node {
	if alt == "" {
		alt = ref.Descr
	}
	var pic *layout.Picture
	if ref.SVG != "" {
		pic = d.loadPicture(s.part, ref.SVG)
	}
	if pic == nil && ref.Embed != "" {
		pic = d.loadPicture(s.part, ref.Embed)
	}
	if pic == nil && ref.Link != "" {
		d.warn(warn.PictureMissing, warn.Omitted, "a linked picture is left out: linked files are not read")
	}
	if pic == nil {
		if w > 0 && h > 0 {
			if ref.Link != "" && ref.Embed == "" && ref.SVG == "" {
				return linkPlaceholder(st, w, h)
			}
			return box(st, w, h)
		}
		return nil
	}
	pic = ooxml.Cropped(d.adjustLum(pic, ref.Bright, ref.Contrast), ref.Crop)
	if pic == nil {
		return nil
	}
	ps := inlineStyle(st)
	ps.Display = css.Inline
	if w <= 0 || h <= 0 {
		w, h = pic.Width, pic.Height
	}
	ps.Width, ps.Height = css.Pts(w), css.Pts(h)
	ps.VerticalAlign = css.VerticalAlign{}
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	ps.MinLineHeight = 0
	setTransform(ps, ref.Rotation, ref.FlipH, ref.FlipV)
	n := &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic, Role: "Figure", Alt: alt}
	if ref.Decorative {
		n.Role, n.Alt = "", ""
	}
	return n
}

// setTransform rotates and mirrors the box of st about its centre.
func setTransform(st *css.Style, rot float64, flipH, flipV bool) {
	if rot != 0 {
		st.Transform = append(st.Transform, css.TransformFunc{Kind: css.TransformRotate, V: [6]float64{rot * math.Pi / 180}})
	}
	if flipH || flipV {
		sx, sy := 1.0, 1.0
		if flipH {
			sx = -1
		}
		if flipV {
			sy = -1
		}
		st.Transform = append(st.Transform, css.TransformFunc{Kind: css.TransformScale, V: [6]float64{sx, sy}})
	}
}

// loadPicture decodes the picture the relationship id of part names,
// once per target, nil with a warning when it cannot be drawn.
func (d *document) loadPicture(part, id string) *layout.Picture {
	rel, ok := d.pkg.RelationshipByID(d.ctx, part, id)
	if !ok || rel.External {
		if ok {
			d.warn(warn.PictureMissing, warn.Omitted, "a linked picture is left out: linked files are not read")
		}
		return nil
	}
	if pic, ok := d.pictures[rel.Target]; ok {
		return pic
	}
	if len(d.pictures) >= limits.MaxConvertPictures {
		d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("pictures past %d are not drawn", limits.MaxConvertPictures))
		return nil
	}
	d.pictures[rel.Target] = nil
	data, err := d.pkg.Read(rel.Target)
	if err != nil {
		if d.stop(err) {
			return nil
		}
		d.warn(warn.PictureMissing, warn.Omitted, fmt.Sprintf("picture %s cannot be read: %v", rel.Target, err))
		return nil
	}
	pic, err := ooxml.DecodePicture(d.ctx, data, d.fonts, rel.Target)
	if err != nil {
		switch {
		case d.stop(err):
		case errors.Is(err, ooxml.ErrUnsupportedPicture):
			d.warn(warn.PictureFormatUnsupported, warn.Omitted, fmt.Sprintf("picture %s is left out: %v", rel.Target, err))
		default:
			d.warn(warn.PictureUnreadable, warn.Omitted, fmt.Sprintf("picture %s does not decode: %v", rel.Target, err))
		}
		return nil
	}
	d.pictures[rel.Target] = pic
	return pic
}

// boxTextColor returns the colour of the automatic text of a wps:wsp
// text box whose shape style is style: the colour of its font reference
// (ECMA-376 Part 1, 20.1.4.2.10 fontRef), nil when it states none. A
// colour a run or its styles state wins over it.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/dml-groupshape-capitalization.docx,
//     dml-groupshape-paraspacing.docx, dml-groupshape-runfonts.docx,
//     ooxmlimport/dml-groupshape-paraadjust.docx: Word paints the text of
//     grouped text boxes, which states no colour, white, the lt1 of their
//     font reference; ooxmlexport/fdo74401.docx,
//     dml-textframe-vertadjust.docx, LineStyle_DashType.docx: white too
//     in text boxes outside a group.
//   - libreoffice/docx ooxmlexport/ColorOverwritten.docx: the text of a
//     text box whose font reference is lt1 takes the red and the green of
//     its paragraph styles.
func (d *document) boxTextColor(style *ooxml.Element) *css.Color {
	ref := style.Child(ooxml.NSA, "fontRef")
	if ref == nil {
		return nil
	}
	c, ok := ooxml.SchemeColor(ref, d.theme)
	if !ok {
		return nil
	}
	return &c
}

// shape builds a wps:wsp (MS-ODRAWXML, 2.3.4): a text box as a block of
// its content over the shape's fill, any other shape as a vector picture
// of its preset or custom geometry. alt describes the shape, "" to read
// its own non-visual properties.
func (d *document) shape(s *story, sp *ooxml.Element, st *css.Style, w, h float64, alt string, depth int) *layout.Node {
	if alt == "" {
		alt, _ = ooxml.Describe(sp.Child(ooxml.NSWPS, "cNvPr"))
	}
	spPr := sp.Child(ooxml.NSWPS, "spPr")
	ss := ooxml.ParseShapeStyle(spPr, sp.Child(ooxml.NSWPS, "style"), d.theme)
	fx := d.effectList(spPr, sp.Child(ooxml.NSWPS, "style"))
	if ref := ooxml.ParsePicture(sp); ref.Embed != "" && spPr.Child(ooxml.NSA, "blipFill") != nil && pictureShape(spPr, ss) {
		// The picture is the shape: the shape's description is the
		// picture's, its Figure's Alt (ISO 14289-1, 7.3).
		n := d.picture(s, ref, st, w, h, alt)
		if n != nil {
			n.Picture = d.withEffects(n.Picture, fx)
		}
		return n
	}
	d.resolveBlip(s, &ss)
	if tb := sp.Child(ooxml.NSWPS, "txbx"); tb != nil {
		bodyPr := sp.Child(ooxml.NSWPS, "bodyPr")
		boxText := d.boxText
		d.boxText = d.boxTextColor(sp.Child(ooxml.NSWPS, "style"))
		insets := bodyInsets(bodyPr)
		content := tb.Child(ooxml.NSW, "txbxContent")
		if noTextRoom(insets, h, tb, bodyPr) {
			d.warn(warn.ContentUnsupported, warn.Omitted, "the text of a text box whose insets leave it no height is left out, as Word prints it")
			content = nil
		}
		n := d.textBox(s, content, w, h, ss, insets, alt, depth)
		if bodyPr.Child(ooxml.NSA, "spAutoFit") != nil {
			fitToText(n)
		}
		d.boxText = boxText
		id := attrOr(tb, "id")
		if id != "" {
			setChain(n.Style, id, 0, h)
			d.textBoxEffectsOmitted(fx)
		} else if vert := bodyVert(attrOr(bodyPr, "vert")); vert == "" {
			g := ooxml.ParseGeometry(spPr)
			d.checkGeometry(g)
			d.shapeTextBox(n, g, ss, fx, w, h, attrOr(bodyPr, "anchor"), bodyPr.Child(ooxml.NSA, "spAutoFit") != nil)
		} else {
			d.textBoxEffectsOmitted(fx)
		}
		d.turn(n, bodyVert(attrOr(bodyPr, "vert")))
		if id == "" && bodyPr.Child(ooxml.NSA, "spAutoFit") == nil {
			d.anchorVertical(n, bodyVert(attrOr(bodyPr, "vert")), attrOr(bodyPr, "anchor"), h)
		}
		turnTextBox(n, spPr, bodyPr, id == "")
		return n
	}
	if l := sp.Child(ooxml.NSWPS, "linkedTxbx"); l != nil && attrOr(l, "id") != "" {
		d.textBoxEffectsOmitted(fx)
		// A later link of a chain of text boxes holds none of the
		// chain's content, which flows into it from the link before.
		bodyPr := sp.Child(ooxml.NSWPS, "bodyPr")
		n := d.textBox(s, nil, w, h, ss, bodyInsets(bodyPr), "", depth)
		seq, _ := ooxml.Int(attrOr(l, "seq"))
		setChain(n.Style, attrOr(l, "id"), seq, h)
		d.turn(n, bodyVert(attrOr(bodyPr, "vert")))
		return n
	}
	g := ooxml.ParseGeometry(spPr)
	d.checkGeometry(g)
	n := vectorShape(st, g, ss, w, h)
	n.Picture = d.withEffects(n.Picture, fx)
	if xfrm := spPr.Child(ooxml.NSA, "xfrm"); xfrm != nil {
		rot, fh, fv := ooxml.Transform2D(xfrm)
		setTransform(n.Style, rot, fh, fv)
	}
	return n
}

// pathOutline reports whether the outline of ss is one a box's border
// cannot draw: a dashed line (20.1.8.48 prstDash, 20.1.8.21 custDash) or a
// gradient line (20.1.8.33 gradFill in 20.1.2.2.24 ln).
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/LineStyle_DashType.docx: Word strokes
//     the outlines of its seven text boxes with their dashes, 4.5 points
//     in lgDashDotDot as 36 13.5 4.5 13.5 4.5 13.5.
func pathOutline(ss ooxml.ShapeStyle) bool {
	return ss.Line != nil && (ss.Dash != "" || len(ss.CustDash) > 0 || ss.LineGradient != nil)
}

// turnTextBox turns the text box n by its shape's rotation (ECMA-376
// Part 1, 20.1.7.6 xfrm rot), its text with it, unless its body keeps the
// text upright (21.1.2.1.1 bodyPr upright); the flips of xfrm leave the
// text as it reads. A box of a chain of linked boxes stays unturned, its
// text flowing from box to box.
//
// # Corpus evidence
//
//   - libreoffice/docx: ooxmlexport/fdo73247.docx prints its text box of
//     rot 1969698 (32.8 degrees) turned clockwise with its text.
func turnTextBox(n *layout.Node, spPr, bodyPr *ooxml.Element, single bool) {
	xfrm := spPr.Child(ooxml.NSA, "xfrm")
	if n == nil || xfrm == nil || !single || hasAttr(bodyPr, "upright") && ooxml.OnOff(attrOr(bodyPr, "upright"), true) {
		return
	}
	if rot, _, _ := ooxml.Transform2D(xfrm); rot != 0 {
		setTransform(n.Style, rot, false, false)
	}
}

// pictureShape reports whether a shape filled with a picture draws as the
// picture itself: a rectangle without an outline whose picture stretches
// over it. Any other shape draws its geometry filled with the picture
// (20.1.8.14 blipFill), stretched or tiled, under its outline.
//
// # Corpus evidence
//
//   - litho/docx shapes-fills-lines: Word tiles a 16-pixel texture at 12
//     points from the top left of a rectangle with a dotted outline, and
//     stretches a picture over a rounded rectangle clipped to its corners,
//     both under their outlines.
func pictureShape(spPr *ooxml.Element, ss ooxml.ShapeStyle) bool {
	if ss.Line != nil || (ss.Blip != nil && ss.Blip.Tile) {
		return false
	}
	g := ooxml.ParseGeometry(spPr)
	return g == nil || (g.Custom == nil && (g.Preset == "" || g.Preset == "rect"))
}

// resolveBlip loads the picture of a shape's picture fill, when it has
// one, into ss.
func (d *document) resolveBlip(s *story, ss *ooxml.ShapeStyle) {
	if ss.Blip == nil || ss.Blip.Embed == "" {
		return
	}
	if pic := d.loadPicture(s.part, ss.Blip.Embed); pic != nil && pic.Source != nil {
		b := *ss.Blip
		b.Picture, b.Width, b.Height = pic.Source, pic.Width, pic.Height
		ss.Blip = &b
	}
}

// checkGeometry warns of a geometry the engine draws as its bounding box
// or whose guides do not all resolve.
func (d *document) checkGeometry(g *shape.Geometry) {
	if !shape.Known(g) {
		d.warn(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a %s shape is drawn as its bounding box", g.Preset))
	} else if err := shape.Check(g); err != nil {
		d.warn(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a shape's geometry is drawn in part: %v", err))
	}
}

// vectorShape returns the replaced box of w by h points drawing the
// geometry g with the fill and outline ss.
func vectorShape(st *css.Style, g *shape.Geometry, ss ooxml.ShapeStyle, w, h float64) *layout.Node {
	ps := inlineStyle(st)
	ps.Width, ps.Height = css.Pts(max(w, 0.1)), css.Pts(max(h, 0.1))
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	pic := &layout.Picture{Width: max(w, 0.1), Height: max(h, 0.1)}
	pic.Vector = func(_ context.Context, r typeset.Rect) []typeset.Fragment {
		// The box's least size is the layout's: a line of no height or
		// no width is drawn level or upright.
		if w < 0.1 {
			r.Width = max(w, 0)
		}
		if h < 0.1 {
			r.Height = max(h, 0)
		}
		return ss.Paint(g, r)
	}
	return &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic}
}

// bodyVert maps a text box's bodyPr vert (20.1.10.83 ST_TextVerticalType)
// onto a text direction: vert270 reads bottom to top, mongolianVert top
// to bottom with its lines flowing left to right, the other vertical
// types top to bottom with their lines flowing right to left, horz none.
func bodyVert(v string) string {
	switch v {
	case "", "horz":
		return ""
	case "vert270":
		return "btLr"
	case "mongolianVert":
		return "tbLrV"
	}
	return "tbRl"
}

// textDirection maps a w:textDirection value (ECMA-376 Part 1, 17.18.93
// ST_TextDirection, and Part 4, 14.11.7 for the transitional names) onto
// the directions [setWritingMode] reads, "" for horizontal lines: lr and
// btLr read bottom to top; rl, rlV, tbRl and tbRlV top to bottom, lines
// flowing right to left; lrV and tbLrV top to bottom, lines flowing left
// to right; tb, tbV, lrTb and lrTbV are horizontal, tbV and lrTbV only
// turning East Asian characters.
//
// # Corpus evidence
//
//   - litho/docx: tables-text-direction-valign prints in Word its lrTbV
//     cell horizontal and its tbLrV cell top to bottom at the cell's left
//     edge, its tbRl cell's line at the right edge.
func textDirection(v string) string {
	switch v {
	case "btLr", "lr":
		return "btLr"
	case "tbLrV", "lrV":
		return "tbLrV"
	case "", "lrTb", "tb", "lrTbV", "tbV":
		return ""
	}
	return "tbRl"
}

// warnTextDirection warns of what a text direction of 17.18.93 draws
// approximately: the turned East Asian characters of tbV and lrTbV are
// drawn upright, and the lines of lrV and tbLrV flow right to left as
// the layout stacks every vertical flow.
func (d *document) warnTextDirection(v string) {
	switch v {
	case "tbV", "lrTbV":
		d.warn(warn.ContentUnsupported, warn.Approximated, "East Asian characters of text direction "+v+" are drawn upright")
	case "lrV", "tbLrV":
		d.warn(warn.ContentUnsupported, warn.Approximated, "vertical text with lines from left to right is laid out as from right to left")
	}
}

// turn sets the writing mode of a text box's block n for the text
// direction dir, "" for horizontal text.
func (d *document) turn(n *layout.Node, dir string) {
	d.warnTextDirection(dir)
	if dir == "" || n == nil {
		return
	}
	setWritingMode(n.Style, dir)
}

// noTextRoom reports a text box of height h that prints no text, as Word
// prints it: a box of horizontal text, neither linked nor fitted to its
// text, whose top and bottom insets take its whole height, whatever its
// vertOverflow says (ECMA-376 Part 1, 20.1.2.1.1 bodyPr).
//
// # Corpus evidence
//
//   - libreoffice/docx: layout/tdf137025.docx, a 198.45 point box whose
//     insets of 30 and 40 mm add up to its height, prints its fill and no
//     text, its bodyPr saying vertOverflow overflow.
func noTextRoom(insets [4]float64, h float64, tb, bodyPr *ooxml.Element) bool {
	if attrOr(tb, "id") != "" || bodyPr.Child(ooxml.NSA, "spAutoFit") != nil || bodyVert(attrOr(bodyPr, "vert")) != "" {
		return false
	}
	return h > 0 && insets[css.Top]+insets[css.Bottom] >= h-roomEpsilon
}

// roomEpsilon absorbs the rounding of EMUs to points when insets are
// compared with the height they fill.
const roomEpsilon = 1e-6

// bodyInsets returns the insets of a text box's body properties, top,
// right, bottom, left (20.1.2.1.1 bodyPr: 0.1 inch left and right, 0.05
// inch top and bottom by default).
func bodyInsets(bodyPr *ooxml.Element) [4]float64 {
	insets := [4]float64{3.6, 7.2, 3.6, 7.2}
	for side, name := range map[css.Side]string{css.Top: "tIns", css.Right: "rIns", css.Bottom: "bIns", css.Left: "lIns"} {
		if v, ok := ooxml.EMU(attrOr(bodyPr, name)); ok {
			insets[side] = max(0, v)
		}
	}
	return insets
}

// textBoxEffectsOmitted warns that the effects fx of a text box are not
// drawn: those of a box of a chain of linked boxes or of vertical text,
// whose layout leaves no shape behind the text to draw them around.
func (d *document) textBoxEffectsOmitted(fx *css.OfficeEffects) {
	if fx != nil {
		d.warn(warn.ContentUnsupported, warn.Omitted, "the effects of a linked or vertical text box are not drawn")
	}
}

// shapeTextBox gives the text box n, built by [document.textBox] at w by
// h points, the geometry g of its shape and the vertical anchor of its
// text (ECMA-376 Part 1, 20.1.2.1.1 bodyPr anchor; 20.1.10.59
// ST_TextAnchoringType t, ctr, b): a shape other than a rectangle, or one
// with the effects fx (20.1.8.26 effectLst) or an outline no border draws
// ([pathOutline]), draws behind the text with the fill and outline ss and
// its effects around it, which the box then leaves unpainted, its text
// laid out in the geometry's text rectangle (20.1.9.22 rect; the presets
// of Annex D), the insets counted from it; and the text sits at the top,
// the middle or the bottom of the inset area, as Word places it. A box that grows to
// fit its text (a:spAutoFit) keeps its text at the top; the shape behind
// it keeps the size the file gives.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/ColorOverwritten: Word draws a text box
//     of preset rightArrow as the arrow, its text centred in it; drawn so,
//     layout rises from 0.55 to 0.78.
//   - libreoffice/docx ooxmlexport/fdo77719: layout rises from 0.26 to 0.75.
//   - libreoffice/docx ooxmlexport/WPGbodyPr.docx: Word sets the first line
//     of an ellipse 386.25 points tall, inset 3.6 at the top, 63 points
//     below its top, where its text rectangle starts 56.6 points down.
func (d *document) shapeTextBox(n *layout.Node, g *shape.Geometry, ss ooxml.ShapeStyle, fx *css.OfficeEffects, w, h float64, anchor string, autoFit bool) {
	rect := g == nil || (g.Custom == nil && (g.Preset == "" || g.Preset == "rect"))
	if autoFit || (anchor != "ctr" && anchor != "b") {
		anchor = ""
	}
	if (fx != nil || pathOutline(ss)) && (rect || !shape.Known(g)) {
		// The box's own fill and outline give way to a rectangle drawn
		// behind the text, which carries the effects and the outline.
		g, rect = &shape.Geometry{Preset: "rect"}, false
	}
	if (rect || !shape.Known(g)) && anchor == "" {
		return
	}
	bs := n.Style
	bs.Position = css.Relative
	kids := n.Children
	if !rect && shape.Known(g) {
		// The insets count from the geometry's text rectangle, not from
		// the shape's box.
		tr := shape.TextRect(g, typeset.Rect{Width: w, Height: h})
		for side, v := range [4]float64{tr.Y, w - tr.X - tr.Width, h - tr.Y - tr.Height, tr.X} {
			bs.Padding[side] = css.Pts(bs.Padding[side].Value + max(v, 0))
		}
	}
	if anchor != "" {
		bs.MinHeight = css.Pts(0)
		bs.Height = css.Pts(max(h, 1))
		area := d.blockStyle()
		area.Display = css.Flex
		area.FlexDirection = css.FlexColumn
		// The area's height in points rather than a percentage, which an
		// absolutely positioned box, a group's shape, leaves unresolved.
		inner := h - bs.Padding[css.Top].Value - bs.Padding[css.Bottom].Value
		if rect || !shape.Known(g) {
			inner -= bs.Border[css.Top].Width + bs.Border[css.Bottom].Width
		}
		area.Height = css.Pts(max(inner, 0))
		// Positioned, the text paints over the shape before it (CSS 2.1,
		// Appendix E).
		area.Position = css.Relative
		area.JustifyContent = css.BoxCenter
		if anchor == "b" {
			area.JustifyContent = css.BoxFlexEnd
		}
		kids = []*layout.Node{{Kind: layout.Element, Style: area, Children: []*layout.Node{{Kind: layout.Element, Style: d.blockStyle(), Children: kids}}}}
	}
	if !rect && shape.Known(g) {
		bs.BackgroundColor = css.Transparent
		for i := range bs.Border {
			bs.Border[i] = css.Border{}
		}
		bs.StrokeBorders = false
		pic := vectorShape(bs, g, ss, w, h)
		pic.Picture = d.withEffects(pic.Picture, fx)
		ps := pic.Style
		ps.Position = css.Absolute
		ps.Display = css.Block
		ps.MinHeight = css.Pts(0)
		ps.Left, ps.Top = css.Pts(0), css.Pts(0)
		ps.Right, ps.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		if anchor == "" {
			// Positioned, the text paints over the shape before it.
			inner := d.blockStyle()
			inner.Position = css.Relative
			kids = []*layout.Node{{Kind: layout.Element, Style: inner, Children: kids}}
		}
		kids = append([]*layout.Node{pic}, kids...)
	}
	n.Children = kids
}

// anchorVertical keeps the text box n of vertical text, of direction dir
// ("" for horizontal text, which it leaves alone), h points tall, and
// places its lines across the box by the anchor of its text (ECMA-376
// Part 1, 20.1.2.1.1 bodyPr anchor, 20.1.10.59 ST_TextAnchoringType; VML
// v-text-anchor): t at the box's block start, the side its first line
// takes, ctr in its middle, b at its block end. The lines of a vertical
// box run along its height, which they do not grow.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/fdo69636.docx and
//     ooxmlimport/btlr-frame-vml.docx: Word sets the one line of a text
//     box 57 points wide reading bottom to top, anchored ctr (v-text-anchor
//     middle), in the middle of the box's width.
//   - libreoffice/docx ooxmlexport/btlr-textbox.docx: Word starts the
//     lines of a box reading bottom to top at the bottom of its 90-point
//     height, where its text's length alone would set them at its top.
func (d *document) anchorVertical(n *layout.Node, dir, anchor string, h float64) {
	if dir == "" || n == nil || n.Style == nil {
		return
	}
	bs := n.Style
	bs.MinHeight = css.Pts(0)
	bs.Height = css.Pts(max(h, 1))
	if anchor != "ctr" && anchor != "b" || bs.Width.Kind != css.Points {
		return
	}
	area := d.blockStyle()
	area.Display = css.Flex
	area.FlexDirection = css.FlexColumn
	// The flow is laid out unturned, then turned: the area's height is
	// the box's width less its insets and side borders.
	inner := bs.Width.Value - bs.Padding[css.Left].Value - bs.Padding[css.Right].Value - bs.Border[css.Left].Width - bs.Border[css.Right].Width
	area.Height = css.Pts(max(inner, 0))
	area.JustifyContent = css.BoxCenter
	if anchor == "b" {
		area.JustifyContent = css.BoxFlexEnd
	}
	n.Children = []*layout.Node{{Kind: layout.Element, Style: area, Children: []*layout.Node{{Kind: layout.Element, Style: d.blockStyle(), Children: n.Children}}}}
}

// fitToText makes the text box n, built by [document.textBox], as tall as
// its content and its insets, smaller than the extent the file gives too:
// the box resizes to its text (ECMA-376 Part 1, 21.1.2.1.4 spAutoFit; VML
// mso-fit-shape-to-text, MS-OI29500, 2.1.1394), as Word prints it.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/autofit.docx: Word prints the text box
//     marked spAutoFit, 139 points tall in the file, one line tall, beside
//     an unmarked one at its 128 points.
//   - libreoffice/docx ooxmlimport/n757890.docx and
//     ooxmlexport/docxopenhyperlinkbox.docx: a VML text box of 110.6
//     points marked mso-fit-shape-to-text prints one line tall.
func fitToText(n *layout.Node) {
	n.Style.MinHeight = css.Pts(0)
}

// textBox builds a text box of w by h points: an inline block holding the
// content's blocks, with the shape's fill and outline and the insets,
// top, right, bottom and left.
//
// A text box with content is a Figure: it is anchored in a paragraph and
// lies inside that paragraph's P, which ISO 32005, Table 5 lets hold a
// Figure but not the P and Div elements of the box's content, while a
// Figure holds them. Its alt is alt, the description of its drawing,
// else the text it shows, so that a reader hears the box either way
// (ISO 14289-1, 7.3).
func (d *document) textBox(s *story, content *ooxml.Element, w, h float64, ss ooxml.ShapeStyle, insets [4]float64, alt string, depth int) *layout.Node {
	bs := d.blockStyle()
	bs.Display = css.InlineBlock
	bs.Width = css.Pts(max(w, 1))
	bs.MinHeight = css.Pts(max(h, 0))
	bs.BoxSizing = css.BorderBox
	bs.LineHeight = css.Dimension{Kind: css.Normal}
	bs.VerticalAlign = css.VerticalAlign{Kind: css.AlignTop}
	for i := range insets {
		bs.Padding[i] = css.Pts(insets[i])
	}
	if ss.Fill != nil {
		bs.BackgroundColor = *ss.Fill
	}
	if ss.Line != nil && ss.LineWidth > 0 {
		for i := range bs.Border {
			bs.Border[i] = css.Border{Width: ss.LineWidth, Style: css.Solid, Color: *ss.Line}
		}
		strokeBorders(bs)
	}
	var kids []*layout.Node
	if content != nil {
		ts := &story{part: s.part, fields: s.fields, note: s.note}
		d.nested++
		// The box's text lies on the box, not on the shading of the
		// paragraph that anchors it.
		under := d.under
		d.under = nil
		restore := d.comments.apart()
		kids = d.finish(d.blocks(ts, nil, content, depth+1), false)
		restore()
		d.under = under
		d.nested--
	}
	n := &layout.Node{Kind: layout.Element, Style: bs, Children: kids}
	if len(kids) > 0 {
		n.Role, n.Alt = "Figure", alt
		if alt == "" {
			n.Alt = office.ShownText(kids)
		}
	}
	return n
}
