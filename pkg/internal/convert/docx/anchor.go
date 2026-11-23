package docx

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// placement is where an anchored drawing goes (ECMA-376 Part 1, 20.4.2.3
// anchor; for VML, Part 4, 14.1.2.19 style and 14.3.4 w10:wrap): its
// wrapping, its offsets or alignments and what they are relative to, in
// DrawingML's names (20.4.3.4 ST_RelFromH, 20.4.3.5 ST_RelFromV).
type placement struct {
	// wrap is wrapSquare, wrapTight, wrapThrough, wrapTopAndBottom or
	// wrapNone.
	wrap string
	// wrapText is the sides of a square, tight or through wrapping the
	// text takes (20.4.3.7 ST_WrapText), "" for both.
	wrapText       string
	behind         bool
	alignH, alignV string
	posH, posV     float64
	// pctH and pctV, when hasPctH or hasPctV, are an offset in fractions
	// of the area the position is relative to (MS-ODRAWXML, 2.3.2.3
	// pctPosHOffset, 2.3.2.4 pctPosVOffset), in place of posH or posV.
	pctH, pctV       float64
	hasPctH, hasPctV bool
	relH, relV       string
	// dist is the distance kept from the text, top, right, bottom, left.
	dist [4]float64
	// w and h are the drawing's size in points.
	w, h float64
	// z orders the drawings in front of the text (20.4.2.3
	// relativeHeight), 0 for none.
	z int
	// poly is the wrap polygon of a tight or through wrapping (20.4.2.16),
	// in fractions of the drawing's size, nil for none.
	poly []css.WrapPoint
	// noOverlap keeps the drawing off the other drawings (20.4.2.3
	// allowOverlap off).
	noOverlap bool
	// simple places the drawing at simpleX, simpleY from the page's
	// top-left corner (20.4.2.13 simplePos, the anchor's simplePos on).
	simple           bool
	simpleX, simpleY float64
}

// maxZ bounds the stack level read from a drawing. Anti-DoS guard, not
// spec-mandated: relativeHeight is an unsigned int, Word writes values
// near 2^28.
const maxZ = 1 << 30

// pendingAnchor is a drawing whose position needs its section's page,
// which the section's properties, at its end, give.
type pendingAnchor struct {
	n  *layout.Node
	st *css.Style
	p  placement
	// page anchors the box to the page box; else only its horizontal
	// offset is measured from the page, the box positioned from its
	// paragraph or floated.
	page bool
	// float sets the offset as the box's left margin.
	float bool
	// kids are the shapes of a group anchored to the page, laid out
	// beside it at their offsets from it.
	kids []*layout.Node
	// word sets the offsets of a float of css.Office.WordAnchor.
	word bool
}

// anchor places the anchored drawing n by its wp:anchor k (20.4.2.3).
func (d *document) anchor(k *ooxml.Element, n *layout.Node, w, h float64, sink *inlineSink) {
	dist := func(name string) float64 {
		v, _ := ooxml.EMU(attrOr(k, name))
		return max(0, v)
	}
	p := placement{w: w, h: h}
	ph, pv := k.Child(ooxml.NSWP, "positionH"), k.Child(ooxml.NSWP, "positionV")
	p.posH, p.alignH, p.relH = position(ph)
	p.posV, p.alignV, p.relV = position(pv)
	p.pctH, p.hasPctH = pctPosition(ph, "pctPosHOffset")
	p.pctV, p.hasPctV = pctPosition(pv, "pctPosVOffset")
	p.behind = hasAttr(k, "behindDoc") && ooxml.OnOff(attrOr(k, "behindDoc"), true)
	if v, ok := ooxml.Int(attrOr(k, "relativeHeight")); ok {
		p.z = max(0, min(v, maxZ))
	}
	p.noOverlap = hasAttr(k, "allowOverlap") && !ooxml.OnOff(attrOr(k, "allowOverlap"), true)
	p.dist = [4]float64{dist("distT"), dist("distR"), dist("distB"), dist("distL")}
	if hasAttr(k, "simplePos") && ooxml.OnOff(attrOr(k, "simplePos"), true) {
		if sp := k.Child(ooxml.NSWP, "simplePos"); sp != nil {
			p.simple = true
			p.simpleX, _ = ooxml.EMU(attrOr(sp, "x"))
			p.simpleY, _ = ooxml.EMU(attrOr(sp, "y"))
		}
	}
	p.wrap = "wrapNone"
	for _, c := range k.Kids() {
		if c.Space == ooxml.NSWP && strings.HasPrefix(c.Local, "wrap") {
			p.wrap = c.Local
			p.wrapText = attrOr(c, "wrapText")
			p.poly = wrapPolygon(c.Child(ooxml.NSWP, "wrapPolygon"))
			break
		}
	}
	d.place(p, n, sink)
}

// fromPageH reports a horizontal position relative to the page or one of
// its margins (20.4.3.4).
func fromPageH(rel string) bool {
	switch rel {
	case "page", "margin", "leftMargin", "rightMargin", "insideMargin", "outsideMargin":
		return true
	}
	return false
}

// fromPageV reports a vertical position relative to the page or one of
// its margins (20.4.3.5).
func fromPageV(rel string) bool {
	switch rel {
	case "page", "margin", "topMargin", "bottomMargin", "insideMargin", "outsideMargin":
		return true
	}
	return false
}

// place lays the anchored drawing n out by its placement p: square, tight
// and through wrapping float it on the side its horizontal position says;
// top-and-bottom makes it a block of its own line; no wrapping positions
// it absolutely, in front of or behind the text. A drawing positioned
// vertically from the page or its margins in the body is anchored to the
// page box of its anchor's page (css.Style.PageAnchor), its offsets
// measured from its section's page; any other is positioned from its
// paragraph, a horizontal offset from the page measured from the
// paragraph's start at the margin.
func (d *document) place(p placement, n *layout.Node, sink *inlineSink) {
	st := n.Style
	if d.pageAnchors() && wordWraps(p.wrap) {
		d.placeWord(p, n)
		return
	}
	pageH := fromPageH(p.relH) && d.pageAnchors()
	if !p.behind && p.z > 0 {
		// The drawings in front of the text stack in the order of their
		// relative heights, floats among them, as Word and Writer layer
		// them.
		st.ZIndex = css.ZIndex{Level: p.z}
		if st.Position == css.Static {
			st.Position = css.Relative
		}
	}
	switch p.wrap {
	case "wrapSquare", "wrapTight", "wrapThrough":
		st.Float = css.FloatLeft
		if p.alignH == "right" || p.alignH == "outside" {
			st.Float = css.FloatRight
		}
		st.Margin = [4]css.Dimension{css.Pts(p.dist[0]), css.Pts(p.dist[1]), css.Pts(p.dist[2]), css.Pts(p.dist[3])}
		// LibreOffice keeps a drawing anchored in the body inside the
		// page its paragraph is on.
		st.PageClamp = d.pageAnchors()
		if !fromPageV(p.relV) && p.alignV == "" && (p.posV > 0 || st.PageClamp) {
			// A float lower than its paragraph's top starts that much
			// lower, an approximation of its vertical offset; one higher
			// starts that much higher, inside its page.
			st.Margin[css.Top] = css.Pts(p.posV)
		}
		if st.Float == css.FloatLeft && p.alignH == "" {
			switch {
			case pageH:
				d.pending = append(d.pending, pendingAnchor{n: n, st: st, p: p, float: true})
			case p.posH > 0 && !fromPageH(p.relH):
				st.Margin[css.Left] = css.Pts(p.posH)
			}
		}
		if st.Display == css.Inline || st.Display == css.InlineBlock {
			st.Display = css.Block
		}
	case "wrapTopAndBottom":
		st.Display = css.Block
		st.Margin = [4]css.Dimension{css.Pts(p.dist[0]), css.Pts(0), css.Pts(p.dist[2]), css.Pts(0)}
		switch p.alignH {
		case "center":
			st.Margin[css.Left], st.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		case "right", "outside":
			st.Margin[css.Left] = css.Dimension{Kind: css.Auto}
		default:
			switch {
			case pageH:
				d.pending = append(d.pending, pendingAnchor{n: n, st: st, p: p, float: true})
			case p.posH > 0:
				st.Margin[css.Left] = css.Pts(p.posH)
			}
		}
	default:
		// An absolutely positioned box is blockified (CSS 2.1, 9.7).
		st.Position = css.Absolute
		setFrameWrap(st, p.wrap, p.wrapText)
		setWrapGap(st, p.dist)
		if st.Display == css.Inline || st.Display == css.InlineBlock {
			st.Display = css.Block
		}
		if p.behind {
			st.ZIndex = css.ZIndex{Level: -1}
		}
		if (fromPageV(p.relV) || p.simple) && d.pageAnchors() {
			st.PageAnchor = true
			kids := detachKids(n, 0, 0, 0)
			for _, k := range kids {
				k.Style.PageAnchor = true
				sink.add(k)
			}
			d.pending = append(d.pending, pendingAnchor{n: n, st: st, p: p, page: true, kids: kids})
			return
		}
		if fromPageV(p.relV) && d.inMargin && d.nested == 0 {
			// A drawing of a header or footer positioned from the page
			// is placed when its header or footer is, on its page.
			d.marginAnchors = append(d.marginAnchors, pendingAnchor{n: n, st: st, p: p, page: true})
			sink.positioned = true
			return
		}
		st.Left, st.Top = css.Pts(p.posH), css.Pts(p.posV)
		if p.relV == "paragraph" {
			d.fromPara = append(d.fromPara, n)
		}
		// LibreOffice keeps a drawing positioned from its paragraph
		// inside the page its paragraph is on.
		st.PageClamp = d.pageAnchors()
		if fromPageV(p.relV) {
			d.warn(warn.ContentUnsupported, warn.Approximated, "a drawing positioned from the page in a note, text box or table is positioned from its paragraph")
			st.Top = css.Pts(0)
		}
		switch {
		case pageH || (p.alignH != "" && d.pageAnchors()):
			// An alignment in the column needs the column's width, which
			// the section's properties give.
			d.pending = append(d.pending, pendingAnchor{n: n, st: st, p: p})
		case p.alignH == "center":
			st.Left = css.Dimension{Kind: css.Percent, Value: 0.5}
		case p.alignH == "right" || p.alignH == "outside":
			st.Left, st.Right = css.Dimension{Kind: css.Auto}, css.Pts(0)
		}
		sink.positioned = true
	}
}

// wordWraps reports a wrapping that takes room in the text (20.4.2.15 to
// 20.4.2.19): every one but wrapNone.
func wordWraps(wrap string) bool {
	switch wrap {
	case "wrapSquare", "wrapTight", "wrapThrough", "wrapTopAndBottom":
		return true
	}
	return false
}

// placeWord lays the anchored drawing n of the body out by its placement
// p as Word places a drawing that wraps the text: a float of
// css.Office.WordAnchor at the offsets its positions give (20.4.2.10,
// 20.4.2.11), set when its section's page is known, that overlaps what
// is there. The text keeps its distances from it (20.4.2.3 distT, distB,
// distL, distR), along its wrap polygon for a tight or through wrapping
// (20.4.2.16), and takes the sides its wrapText says (20.4.3.7):
// bothSides both, left and right the one named, largest the one with
// more room; wrapTopAndBottom takes none.
//
// # Corpus evidence
//
//   - litho/docx images-wrap: Word sets a left-aligned 93.6 pt picture
//     with distL and distR of 9 pt at the margin, 72 pt, and the text
//     beside it at 174.6 pt; the lines of the heading above its
//     paragraph that its distT of 3.6 pt reaches wrap too. The text of a
//     centered picture of wrapTight bothSides runs on both sides of its
//     diamond contour, each piece justified, 9 pt from the contour over
//     the line grown by about 3 pt.
//   - litho/docx images-positioning: two pictures of wrapSquare offset
//     28.8 pt apart overlap, the text beside the one each line meets; of
//     two pictures of allowOverlap 0, Word moves the second right of the
//     first, and a line beside both takes "measure" in the 48.6 pt left
//     of the second.
func (d *document) placeWord(p placement, n *layout.Node) {
	st := n.Style
	if !p.behind && p.z > 0 {
		st.ZIndex = css.ZIndex{Level: p.z}
		if st.Position == css.Static {
			st.Position = css.Relative
		}
	}
	st.Float = css.FloatLeft
	st.Margin = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	st.WordAnchor = css.WordAnchor{Set: true}
	setWrapGap(st, p.dist)
	switch {
	case p.wrap == "wrapTopAndBottom":
		st.FrameWrap = css.FrameWrapNone
	case p.wrapText == "left":
		st.FrameWrap = css.FrameWrapLeft
	case p.wrapText == "right":
		st.FrameWrap = css.FrameWrapRight
	case p.wrapText == "largest":
		st.FrameWrap = css.FrameWrapLargest
	default:
		st.FrameWrap = css.FrameWrapBoth
	}
	if p.wrap == "wrapTight" || p.wrap == "wrapThrough" {
		st.WrapPolygon = p.poly
	}
	// Word keeps a drawing anchored in the body on its paragraph's page.
	st.PageClamp = true
	if st.Display == css.Inline || st.Display == css.InlineBlock {
		st.Display = css.Block
	}
	d.pending = append(d.pending, pendingAnchor{n: n, st: st, p: p, word: true})
	if p.relV == "paragraph" {
		d.fromPara = append(d.fromPara, n)
	}
}

// maxPolygonPoints bounds the points read from a wrap polygon.
// Anti-DoS guard, not spec-mandated: Word writes a few dozen, and the
// layout follows at most as many.
const maxPolygonPoints = 1024

// wrapPolygonSpace is the size of the space Word writes a wrap polygon's
// coordinates in, whatever the drawing's size: 21600 by 21600.
const wrapPolygonSpace = 21600.0

// wrapPolygon reads a wp:wrapPolygon (20.4.2.16): its wp:start and
// wp:lineTo points, in fractions of the drawing's size, nil for fewer
// than three.
func wrapPolygon(e *ooxml.Element) []css.WrapPoint {
	if e == nil {
		return nil
	}
	var out []css.WrapPoint
	for _, c := range e.Kids() {
		if c.Space != ooxml.NSWP || (c.Local != "start" && c.Local != "lineTo") || len(out) >= maxPolygonPoints {
			continue
		}
		x, okX := ooxml.Int(attrOr(c, "x"))
		y, okY := ooxml.Int(attrOr(c, "y"))
		if !okX || !okY {
			continue
		}
		out = append(out, css.WrapPoint{X: float64(x) / wrapPolygonSpace, Y: float64(y) / wrapPolygonSpace})
	}
	if len(out) < 3 {
		return nil
	}
	return out
}

// pageAnchors reports whether a drawing being built can be positioned
// from its page: in the body, outside tables and text boxes, whose
// drawings Word lays out in their cell or box.
func (d *document) pageAnchors() bool { return d.inBody && d.nested == 0 }

// resolve sets the offsets of the pending drawings of section s from its
// page: the page box's left and top for a page-anchored box, the offset
// from the margin for one positioned from its paragraph.
func (a pendingAnchor) resolve(s *section) {
	p, st := a.p, a.st
	left := s.margin[css.Left] + s.gutter
	if a.word {
		a.resolveWord(s)
		return
	}
	x := hPos(s, p)
	switch {
	case a.float:
		st.Margin[css.Left] = css.Pts(max(0, x-left))
		return
	case !a.page:
		st.Left = css.Pts(x - left)
		return
	}
	if !fromPageH(p.relH) {
		x = left + p.posH
		switch p.alignH {
		case "left", "inside":
			x = left
		case "center":
			x = left + float64((s.contentWidth()-p.w)/2)
		case "right", "outside":
			x = left + s.contentWidth() - p.w
		}
	}
	if p.simple {
		x, y := p.simpleX, p.simpleY
		st.Left, st.Top = css.Pts(x), css.Pts(y)
		st.Right, st.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		return
	}
	y := vPos(s, p)
	st.Left, st.Top = css.Pts(x), css.Pts(y)
	st.Right, st.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	for _, k := range a.kids {
		k.Style.Left, k.Style.Top = css.Pts(x+k.Style.Left.Value), css.Pts(y+k.Style.Top.Value)
	}
}

// heightAnchor is a drawing positioned from the page of section geo at
// a height that depends on the page's: aligned to the middle or the
// bottom of the page or of its margins, at a percentage of it, or from
// its bottom margin (20.4.2.11). On a single page it is placed first on
// geo's page, then moved by what the single page's height changes.
type heightAnchor struct {
	a   pendingAnchor
	geo section
}

// heightAnchor returns the drawing a as a [heightAnchor] of section s,
// false when its position does not depend on the page's height.
func (a pendingAnchor) heightAnchor(s *section) (heightAnchor, bool) {
	if a.p.simple || !a.page && (!a.word || !fromPageV(a.p.relV)) {
		return heightAnchor{}, false
	}
	taller := *s
	taller.height++
	if vPos(s, a.p) == vPos(&taller, a.p) {
		return heightAnchor{}, false
	}
	return heightAnchor{a: a, geo: *s}, true
}

// moveTo moves the drawing, and the shapes detached from it, to where
// it sits on a page h points tall: by the difference of its positions on
// that page and on its section's. Its styles are shared, so each node
// takes a moved copy. It reports whether the drawing moved.
func (ha heightAnchor) moveTo(d *document, h float64) bool {
	page := ha.geo
	page.height = h
	dy := vPos(&page, ha.a.p) - vPos(&ha.geo, ha.a.p)
	if dy == 0 {
		return false
	}
	move := func(n *layout.Node) {
		st := *n.Style
		if st.WordAnchor.Set {
			st.WordAnchor.Y += dy
		} else {
			st.Top = css.Pts(st.Top.Value + dy)
		}
		n.Style = d.share(&st)
	}
	move(ha.a.n)
	for _, k := range ha.a.kids {
		move(k)
	}
	return true
}

// resolveWord sets the offsets of a float of css.Office.WordAnchor on a
// page of section s: the horizontal one from the start of the text area,
// the vertical one from its paragraph's top, or from the page area's top
// for a position from the page or its margins (20.4.3.4, 20.4.3.5).
func (a pendingAnchor) resolveWord(s *section) {
	p, st := a.p, a.st
	left := s.margin[css.Left] + s.gutter
	top := math.Abs(s.margin[css.Top])
	ax, ay := st.WordAnchor.AlignX, st.WordAnchor.AlignY
	switch {
	case p.simple:
		st.WordAnchor = css.WordAnchor{Set: true, X: p.simpleX - left, Y: p.simpleY - top, FromPage: true, NoOverlap: p.noOverlap}
	case !fromPageV(p.relV):
		st.WordAnchor = css.WordAnchor{Set: true, X: hPos(s, p) - left, Y: p.posV, NoOverlap: p.noOverlap}
	default:
		st.WordAnchor = css.WordAnchor{Set: true, X: hPos(s, p) - left, Y: vPos(s, p) - top, FromPage: true, NoOverlap: p.noOverlap}
	}
	st.WordAnchor.AlignX, st.WordAnchor.AlignY = ax, ay
	// A position from the page or its margins stands where it says
	// whatever column the drawing's paragraph lies in; one from the column
	// counts from that column (20.4.3.4).
	st.WordAnchor.FromFirstColumn = s.cols > 1 && (p.simple || fromPageH(p.relH))
}

// hPos returns the left edge, from the page's, of a drawing of placement
// p on a page of section s: its offset or alignment in the area its
// horizontal position is relative to (20.4.2.10).
func hPos(s *section, p placement) float64 {
	x0, aw := hArea(s, p.relH)
	switch p.alignH {
	case "left", "inside":
		return x0
	case "center":
		return x0 + float64((aw-p.w)/2)
	case "right", "outside":
		return x0 + aw - p.w
	}
	if p.hasPctH {
		return x0 + float64(p.pctH*aw)
	}
	return x0 + p.posH
}

// vPos returns the top edge, from the page's, of a drawing of placement p
// positioned from the page or its margins on a page of section s
// (20.4.2.11).
func vPos(s *section, p placement) float64 {
	y0, ah := vArea(s, p.relV)
	switch p.alignV {
	case "top", "inside":
		return y0
	case "center":
		return y0 + float64((ah-p.h)/2)
	case "bottom", "outside":
		return y0 + ah - p.h
	}
	if p.hasPctV {
		return y0 + float64(p.pctV*ah)
	}
	return y0 + p.posV
}

// detachKids takes the shapes of the group n, and of the groups in it,
// out of it and returns them in paint order, their offsets made relative
// to n. A group anchored to the page lays its shapes out as boxes anchored
// to the page beside it, each on the group's page whole.
func detachKids(n *layout.Node, dx, dy float64, depth int) []*layout.Node {
	if depth > maxNesting {
		return nil
	}
	var out []*layout.Node
	kept := n.Children[:0]
	for _, k := range n.Children {
		st := k.Style
		if st == nil || st.Position != css.Absolute || st.Left.Kind != css.Points || st.Top.Kind != css.Points {
			kept = append(kept, k)
			continue
		}
		st.Left, st.Top = css.Pts(dx+st.Left.Value), css.Pts(dy+st.Top.Value)
		out = append(out, k)
		out = append(out, detachKids(k, st.Left.Value, st.Top.Value, depth+1)...)
	}
	n.Children = kept
	return out
}

// contentWidth returns the width of the section's text area.
func (s *section) contentWidth() float64 {
	return max(minPageArea, s.width-s.margin[css.Left]-s.margin[css.Right]-s.gutter)
}

// hArea returns the left edge and the width of the area a horizontal
// position is relative to (20.4.3.4) on a page of section s: the page,
// a margin, else the text area.
func hArea(s *section, rel string) (float64, float64) {
	left := s.margin[css.Left] + s.gutter
	switch rel {
	case "page":
		return 0, s.width
	case "leftMargin", "insideMargin":
		return 0, left
	case "rightMargin", "outsideMargin":
		return s.width - s.margin[css.Right], s.margin[css.Right]
	case "margin":
		return left, s.contentWidth()
	}
	return left, s.columnWidth()
}

// columnWidth returns the width of the first column of section s, the
// column a position relative to the column is measured in: the layout
// sets a box of a later column at the same offset from that column's
// start.
func (s *section) columnWidth() float64 {
	if s.cols <= 1 {
		return s.contentWidth()
	}
	if len(s.colTracks) > 0 {
		return max(minPageArea, s.colTracks[0].w)
	}
	return max(minPageArea, (s.contentWidth()-float64(float64(s.cols-1)*s.colSpace))/float64(s.cols))
}

// vArea returns the top edge and the height of the area a vertical
// position is relative to (20.4.3.5) on a page of section s.
func vArea(s *section, rel string) (float64, float64) {
	top, bottom := math.Abs(s.margin[css.Top]), math.Abs(s.margin[css.Bottom])
	switch rel {
	case "page":
		return 0, s.height
	case "topMargin", "insideMargin":
		return 0, top
	case "bottomMargin", "outsideMargin":
		return s.height - bottom, bottom
	}
	return top, max(minPageArea, s.height-top-bottom)
}

// position reads a wp:positionH or wp:positionV (20.4.2.10, 20.4.2.11):
// its offset in points, its alignment and what it is relative to. An
// element with neither child, which the schema forbids but documents
// carry, positions at offset zero.
func position(e *ooxml.Element) (float64, string, string) {
	if e == nil {
		return 0, "", ""
	}
	rel := attrOr(e, "relativeFrom")
	if a := e.Child(ooxml.NSWP, "align"); a != nil {
		return 0, strings.TrimSpace(a.Text), rel
	}
	var off float64
	if o := e.Child(ooxml.NSWP, "posOffset"); o != nil {
		off, _ = ooxml.EMU(strings.TrimSpace(o.Text))
	}
	return off, "", rel
}

// pctPosition reads the wp14 percentage child name of e: the offset of a
// wp:positionH or wp:positionV (MS-ODRAWXML, 2.3.2.3, 2.3.2.4), or the
// width or height of a wp14:sizeRelH or wp14:sizeRelV, a fraction of the
// area it is relative to, the element holding thousandths of a percent.
func pctPosition(e *ooxml.Element, name string) (float64, bool) {
	c := e.Child(ooxml.NSWP14, name)
	if c == nil {
		return 0, false
	}
	v, ok := ooxml.Int(strings.TrimSpace(c.Text))
	if !ok {
		return 0, false
	}
	return float64(v) / 100000, true
}

// group builds a wpg:wgp (MS-ODRAWXML, 2.3.4.3): an inline block of w by h
// points holding its shapes and pictures positioned by their transforms in
// the group's child coordinates.
func (d *document) group(s *story, g *ooxml.Element, st *css.Style, w, h float64, depth int) *layout.Node {
	gs := inlineStyle(st)
	gs.Display = css.InlineBlock
	gs.Width, gs.Height = css.Pts(w), css.Pts(h)
	gs.Position = css.Relative
	gs.LineHeight = css.Dimension{Kind: css.Normal}
	var offX, offY, extX, extY float64
	if x := g.Child(ooxml.NSWPG, "grpSpPr").Child(ooxml.NSA, "xfrm"); x != nil {
		offX, _ = ooxml.EMU(attrOr(x.Child(ooxml.NSA, "chOff"), "x"))
		offY, _ = ooxml.EMU(attrOr(x.Child(ooxml.NSA, "chOff"), "y"))
		extX, _ = ooxml.EMU(attrOr(x.Child(ooxml.NSA, "chExt"), "cx"))
		extY, _ = ooxml.EMU(attrOr(x.Child(ooxml.NSA, "chExt"), "cy"))
	}
	sx, sy := 1.0, 1.0
	if extX > 0 && w > 0 {
		sx = w / extX
	}
	if extY > 0 && h > 0 {
		sy = h / extY
	}
	n := &layout.Node{Kind: layout.Element, Style: gs}
	if depth > maxNesting {
		return n
	}
	for _, c := range g.Kids() {
		var xfrm *ooxml.Element
		switch {
		case c.Is(ooxml.NSWPS, "wsp"):
			xfrm = c.Child(ooxml.NSWPS, "spPr").Child(ooxml.NSA, "xfrm")
		case c.Is(ooxml.NSPic, "pic"):
			xfrm = c.Child(ooxml.NSPic, "spPr").Child(ooxml.NSA, "xfrm")
		case c.Is(ooxml.NSWPG, "grpSp"):
			xfrm = c.Child(ooxml.NSWPG, "grpSpPr").Child(ooxml.NSA, "xfrm")
		default:
			continue
		}
		x, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "off"), "x"))
		y, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "off"), "y"))
		cw, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "ext"), "cx"))
		ch, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "ext"), "cy"))
		cw, ch = max(cw*sx, 0), max(ch*sy, 0)
		var kid *layout.Node
		switch {
		case c.Is(ooxml.NSWPS, "wsp"):
			kid = d.shape(s, c, st, cw, ch, "", depth+1)
		case c.Is(ooxml.NSPic, "pic"):
			kid = d.picture(s, ooxml.ParsePicture(c), st, cw, ch, "")
		default:
			kid = d.group(s, c, st, cw, ch, depth+1)
		}
		if kid == nil {
			continue
		}
		kid.Style.Position = css.Absolute
		kid.Style.Left, kid.Style.Top = css.Pts((x-offX)*sx), css.Pts((y-offY)*sy)
		if kid.Style.Display == css.Inline || kid.Style.Display == css.InlineBlock {
			kid.Style.Display = css.Block
		}
		n.Children = append(n.Children, kid)
	}
	return n
}

// breakAnywhere lets the text of n break between any two letters
// (css.Anywhere), n's descendants included: a run's style holds its own
// overflow-wrap. n's style is set in place, its descendants' are copied,
// a run's style being shareable.
func breakAnywhere(n *layout.Node, depth int) {
	if n == nil || depth > maxNesting {
		return
	}
	if n.Style != nil {
		if depth > 0 {
			st := *n.Style
			n.Style = &st
		}
		n.Style.OverflowWrap = css.Anywhere
	}
	for _, k := range n.Children {
		breakAnywhere(k, depth+1)
	}
}

// breakWords lets the text of the paragraph n of style st, not yet
// shared, break between any two letters.
func breakWords(n *layout.Node, st *css.Style) {
	st.OverflowWrap = css.Anywhere
	for _, k := range n.Children {
		breakAnywhere(k, 1)
	}
}

// frame lays a paragraph with a w:framePr (17.3.1.11) out as a float: a
// drop cap's letter floats at the start of the next paragraph's lines; a
// text frame of the body is placed with the paragraphs of its frame
// ([document.placeFrame]); any other text frame, in a header, a footer, a
// note or a section of columns, floats on the side its xAlign says, as
// wide as its w, an approximation of a position at page coordinates.
//
// # Corpus evidence
//
//   - libreoffice/docx layout/fdo48718-1.docx: Word writes a floating
//     table's position on its cells' paragraphs.
//   - libreoffice/docx ooxmlexport/fdo60957-2.docx: a table whose cells
//     repeat the frame properties of the paragraphs around it is part of
//     their frame.
//   - libreoffice/docx layout/tdf135035.docx: Writer breaks a word longer
//     than the column inside a frame without a width.
//
// st is the style n takes, which frame writes.
func (d *document) frame(n *layout.Node, st *css.Style, pp paraProps) {
	f := pp.framePr
	if f == nil || len(f.Attrs) == 0 {
		// A frame without properties is a plain paragraph, as LibreOffice
		// imports it.
		return
	}
	if d.framePlaced(f) {
		// The frame is placed with the paragraphs that share its
		// properties ([document.placeFrame]); a frame without a width
		// breaks a word longer than its column.
		if v, ok := ooxml.Twips(attrOr(f, "w")); !ok || v <= 0 {
			breakWords(n, st)
		}
		return
	}
	if d.inCell > 0 {
		// LibreOffice makes no frame of a paragraph inside a table cell:
		// it stays in the cell's flow. Word writes the floating table's
		// position on its cells' paragraphs, and a table whose cells
		// repeat the frame properties of the paragraphs around it is part
		// of their frame.
		return
	}
	switch attrOr(f, "wrap") {
	case "none", "notBeside":
		if attrOr(f, "dropCap") == "" {
			return
		}
	}
	st.Float = css.FloatLeft
	switch attrOr(f, "xAlign") {
	case "right", "outside":
		st.Float = css.FloatRight
	case "center":
		st.Float = css.FloatNone
		st.Margin[css.Left], st.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	}
	if v, ok := ooxml.Twips(attrOr(f, "w")); ok && v > 0 {
		st.Width = css.Pts(v)
	} else {
		// A frame without a width is as wide as its text, at most its
		// column: Writer breaks a word longer than the column inside it,
		// which a float's shrink-to-fit width allows only when the word's
		// letters are break opportunities.
		breakWords(n, st)
	}
	if v, ok := ooxml.Twips(attrOr(f, "hSpace")); ok && v > 0 {
		st.Margin[css.Right] = css.Pts(v)
	}
	if attrOr(f, "dropCap") == "" && (hasAttr(f, "x") || hasAttr(f, "y")) {
		d.warn(warn.ContentUnsupported, warn.Approximated, "a text frame at a page position floats beside its anchor")
	}
}

// framePlaced reports whether the text frame of properties f (17.3.1.11
// framePr) is placed as Word places a frame of the body
// ([document.placeFrame]): a frame that is no drop cap, names its
// horizontal and vertical anchors, has a position or an alignment, and
// wraps the text around it or keeps it above and below (wrap around,
// auto, tight, through or notBeside), in the body outside tables and text
// boxes. In a section of several columns, a frame positioned from the
// page or its margins counts from the first column
// (css.WordAnchor.FromFirstColumn).
//
// # Corpus evidence
//
//   - libreoffice/docx layout/tdf135035.docx: in a section of three
//     columns, Word sets the text of a frame of x 4569 from the page at
//     228.6 points across, in the second column, and draws a frame of x
//     4014 from the page a column wide from 200.7 points across.
func (d *document) framePlaced(f *ooxml.Element) bool {
	if f == nil || !d.pageAnchors() || d.inCell > 0 || hasAttr(f, "dropCap") && attrOr(f, "dropCap") != "none" {
		return false
	}
	switch attrOr(f, "wrap") {
	case "", "auto", "around", "tight", "through", "notBeside":
	default:
		return false
	}
	if !hasAttr(f, "hAnchor") || !hasAttr(f, "vAnchor") {
		// The areas a frame that names no anchor is placed in are not
		// measured: such a frame floats as before.
		return false
	}
	for _, name := range [4]string{"x", "y", "xAlign", "yAlign"} {
		if hasAttr(f, name) {
			return true
		}
	}
	return false
}

// sameFrame reports two frame properties that make one frame: the same
// attributes with the same values (17.3.1.11: consecutive paragraphs
// with identical frame properties are one text frame).
func sameFrame(a, b *ooxml.Element) bool {
	if a == nil || b == nil || len(a.Attrs) != len(b.Attrs) {
		return false
	}
	for _, x := range a.Attrs {
		v, ok := b.Attr(x.Name.Local)
		if !ok || v != x.Value {
			return false
		}
	}
	return true
}

// frameGroup is a text frame of the body: the paragraphs that share its
// properties pr, laid out in the box that frame places.
type frameGroup struct {
	pr      *ooxml.Element
	members []blockNode
}

// placeFrame makes the box of the text frame of properties f (17.3.1.11)
// and places it as Word places a drawing that wraps the text
// ([document.placeWord]): its left edge, where its paragraphs' text
// starts, x right of the area hAnchor names (the page, the margins, or
// the column for text), or aligned in it by xAlign; its top edge y below
// the top of the area vAnchor names, or below the paragraph after the
// frame for text, or aligned by yAlign, in the margins for a frame
// anchored to the text. A frame wraps the text on both sides, at hSpace
// beside it and vSpace above and below; one of wrap notBeside keeps it
// above and below. w sets its width, h its height, exact or at least by
// hRule (17.18.37 ST_HeightRule); a frame without a width is as wide as
// its text, at most its column.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/fdo60957-2.docx: Word sets the text of
//     three paragraphs of one frame of x 6345 and y 2225 from the page one
//     under the other, the first line's text at 317.4 points across, its
//     top at 111.3 points down.
//   - litho/docx para-dropcap-frames: a frame of w 2880, x 5000 from the
//     margin and y 200 from the text sits 322 points across, the text on
//     both of its sides; one of xAlign center from the page, w 4320, h
//     1440 exact and wrap notBeside is centred on the page, 72 points
//     tall, the text below it; one of xAlign right and yAlign bottom from
//     the page has its corner at the page's bottom right corner.
//   - libreoffice/docx layout/tdf124423.docx: a frame anchored to the
//     text has the top of its border y below the top of the paragraph
//     after it, its text starting at its x, the border outside.
func (d *document) placeFrame(f *ooxml.Element) *layout.Node {
	twips := func(name string) float64 {
		v, _ := ooxml.Twips(attrOr(f, name))
		return v
	}
	bv := d.blockOf()
	st := &bv
	p := placement{wrap: "wrapSquare", posH: twips("x"), posV: twips("y")}
	if attrOr(f, "wrap") == "notBeside" {
		p.wrap = "wrapTopAndBottom"
	}
	h, v := max(0, twips("hSpace")), max(0, twips("vSpace"))
	p.dist = [4]float64{v, h, v, h}
	switch a := attrOr(f, "hAnchor"); a {
	case "margin", "page":
		p.relH = a
	default:
		p.relH = "column"
	}
	switch a := attrOr(f, "xAlign"); a {
	case "left", "center", "right", "inside", "outside":
		p.alignH = a
	}
	switch a := attrOr(f, "yAlign"); a {
	case "top", "center", "bottom", "inside", "outside":
		p.alignV = a
	}
	switch a := attrOr(f, "vAnchor"); a {
	case "margin", "page":
		p.relV = a
	default:
		p.relV = "paragraph"
		if p.alignV != "" {
			p.relV = "margin"
		}
	}
	if w := twips("w"); w > 0 {
		st.Width = css.Pts(w)
		p.w = w
	}
	if ht := twips("h"); ht > 0 {
		switch attrOr(f, "hRule") {
		case "exact":
			st.Height = css.Pts(ht)
			p.h = ht
		case "atLeast", "auto":
			st.MinHeight = css.Pts(ht)
		}
	}
	n := &layout.Node{Kind: layout.Element, Style: st}
	d.placeWord(p, n)
	if p.w == 0 {
		switch p.alignH {
		case "center":
			st.WordAnchor.AlignX = 0.5
		case "right", "outside":
			st.WordAnchor.AlignX = 1
		}
	}
	if p.h == 0 {
		switch p.alignV {
		case "center":
			st.WordAnchor.AlignY = 0.5
		case "bottom", "outside":
			st.WordAnchor.AlignY = 1
		}
	}
	return n
}
