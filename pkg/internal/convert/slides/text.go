package slides

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/officefx"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/symbolfont"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// defaultFontSize is the size of a run whose style gives none: Impress's
// default text size, 18 points.
const defaultFontSize = 18

// maxFontSize caps a run's font size, in points. Anti-DoS guard, not
// spec-mandated: DrawingML stops at 4000 points (20.1.10.81
// ST_TextFontSize), the text stack at 1000.
const maxFontSize = 1000

// fallbackFamily is the face a run falls back to when none of its
// families is installed and no metric alias serves them, the face Impress
// draws such a family with in the pinned image.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/bnc862510_7.pptx (Segoe UI) prints in Noto Sans.
//   - probe, odp: frames in NoSuchFont, Segoe UI and Tahoma print in Noto
//     Sans.
const fallbackFamily = "Noto Sans"

// defaultTab is the distance between the default tab stops of a
// PresentationML text, in points: one inch, what PowerPoint's masters state
// (ECMA-376 Part 1, 21.1.2.2.7 defTabSz). odfDefaultTab is an OpenDocument
// text's, half an inch, Impress's default.
//
// # Corpus evidence
//
//   - probe, odp: a frame of Liberation Sans 20 with three tabs and no tab stop
//     distance prints its digits 36 points apart.
const (
	defaultTab    = 72
	odfDefaultTab = 36
)

// minDefaultTab is the smallest interval of default tab stops laid out,
// in points. Anti-DoS guard, not spec-mandated: a line of tabs at a
// smaller interval takes thousands of stops.
const minDefaultTab = 1

// maxParagraphs caps the paragraphs of a text body laid out. Anti-DoS
// guard, not spec-mandated: a slide's text box shows a few dozen.
const maxParagraphs = 4096

// maxRuns caps the runs of a paragraph laid out. Anti-DoS guard, not
// spec-mandated: a paragraph holds a few dozen runs.
const maxRuns = 4096

// maxBodyText caps the characters of a text body laid out. Anti-DoS
// guard, not spec-mandated: a full slide of 10-point text holds about
// 5,000 characters.
const maxBodyText = 1 << 18

// fixedLineFactor is a line's height over its largest font size when
// the line's height does not follow the faces' metrics: Impress's
// font-independent line spacing, the size plus 20%
// (ImplCalculateFontIndependentLineSpacing), the baseline the size below
// the line's top.
const fixedLineFactor = 1.2

// scriptScale is the size of a superscript or a subscript, 58% of the
// text's (DFLT_ESC_PROP); superRise and subDrop how far a PresentationML
// one moves, 30% of the size up or 25% down, the shifts PowerPoint writes
// (ECMA-376 Part 1, 21.1.2.3.9 baseline) and Impress prints as stated.
// An OpenDocument one moves automatically instead (scriptShift).
const (
	scriptScale = 0.58
	superRise   = 0.30
	subDrop     = 0.25
)

// maxRise caps a run's shift as a fraction of its size. Anti-DoS guard,
// not spec-mandated: PowerPoint and Impress offer shifts of up to 100% of
// the size, a run so shifted standing a line above or below its own.
const maxRise = 1

// scriptShift returns how far a superscript or a subscript of a text of
// size moves in faces of metrics m, as Impress moves an automatic one
// (SvxFont::SetNonAutoEscapement): the share of the face's height above
// its baseline, or below for a subscript, of the 42% the script's size
// gives up, in whole percents of the size.
//
// # Corpus evidence
//
//   - probe, odp: Liberation Sans 20 with text-position super 58% prints 6.8 points
//     up, sub 58% 1.4 points down, 33% 58% 6.6 points up.
func scriptShift(size float64, m text.Metrics, sub bool) float64 {
	h := m.Ascent + m.Descent
	if !numeric.Positive(h) {
		return 0
	}
	share := m.Ascent / h
	if sub {
		share = m.Descent / h
	}
	return math.Trunc(share*(1-scriptScale)*100) / 100 * size
}

// fit is the scaling a text body is laid out at to fit its box: the font
// scale and the spacing scale, 1 for none (ImpEditEngine's
// ScalingParameters).
type fit struct {
	font, spacing float64
}

// noFit lays text out at its own sizes.
var noFit = fit{1, 1}

// scaleLevels are the scalings Impress tries in turn on text that shrinks
// to fit, until the text fits (ImpEditEngine::ScaleContentToFitWindow,
// constScaleLevels).
var scaleLevels = [...]fit{
	{1, 0.9},
	{0.925, 0.9},
	{0.85, 0.9},
	{0.85, 0.8},
	{0.775, 0.8},
	{0.7, 0.8},
	{0.625, 0.8},
	{0.55, 0.8},
	{0.475, 0.8},
	{0.4, 0.8},
	{0.325, 0.8},
	{0.25, 0.8},
}

// textResult is a text body laid out in its shape.
type textResult struct {
	// box is the shape's box, grown to the text when the body asks.
	box typeset.Rect
	// frags, links and structure are the laid-out text, in slide
	// coordinates before the shape's turn.
	frags     []typeset.Fragment
	links     []typeset.Link
	structure []*typeset.StructElem
}

// laidBody is a text body laid out at the origin: its fragments, links
// and structure, and its height.
type laidBody struct {
	frags     []typeset.Fragment
	links     []typeset.Link
	structure []*typeset.StructElem
	height    float64
}

// insetRect returns r less the insets, indexed by css.Side, as Impress
// places a text frame's text rectangle (SdrTextObj::AdjustRectToTextDistance):
// a negative inset widens the rectangle; an inset larger than the box
// stops at the box's opposite edge; when the two insets of an axis
// overlap, the rectangle shrinks to nothing at the middle of its two
// edges.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd-data/TextDistancesInsets3.pptx's 56.7-point boxes with top
//     and bottom insets of 0 and 4, 4 and 0, 1 and 2, 2 and 1, 2 and 2
//     centimetres print their top-anchored text at 28.35 points above the
//     box, at its bottom edge, and 14.17 points below, 42.52 below and
//     28.35 below its top.
func insetRect(r typeset.Rect, in [4]float64) typeset.Rect {
	side := func(v float64) float64 {
		if !numeric.Finite(v) {
			return 0
		}
		return min(max(v, -maxCoordinate), maxCoordinate)
	}
	axis := func(start, size, a, b float64) (float64, float64) {
		lo := start + min(a, size)
		hi := start + size - b
		if hi < lo {
			mid := (lo + hi) / 2
			return mid, 0
		}
		return lo, hi - lo
	}
	x, w := axis(r.X, r.Width, side(in[css.Left]), side(in[css.Right]))
	y, h := axis(r.Y, r.Height, side(in[css.Top]), side(in[css.Bottom]))
	out := typeset.Rect{X: x, Y: y, Width: w, Height: h}
	if out.Width < 1 {
		out.X, out.Width = out.X+float64(out.Width/2)-0.5, 1
	}
	return out
}

// shapeText lays the text of body out in the text rectangle of the
// geometry g fitted to box.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/tdf99729-new.odp's bottom-anchored frame of 1 centimetre,
//     sd/tdf150966_hugeInset.odp's bottom-anchored shape whose insets meet
//     in the middle print their overflowing text from the rectangle's top;
//     sd/invalidAudio.odp's middle-anchored title overflows both ways.
//   - libreoffice/pptx:
//     sd-data/TextFittingComparisonWithMSO_TopBottomMiddleAlignment.pptx
//     overflows a bottom-anchored body upwards; oox/tdf151008_eaVertAnchor.pptx
//     overflows its unwrapped vertical lines past their end.
//   - libreoffice/odp: sd/style-overflow-behavior-clip.fodp, whose
//     overflow behaviour is clip, leaves its second line out.
func (b *builder) shapeText(body *TextBody, g *shape.Geometry, box typeset.Rect) *textResult {
	res := &textResult{box: box}
	inner := insetRect(shape.TextRect(g, box), body.Insets)
	vertical := body.Vertical.Vertical()
	if body.Vertical == css.VerticalLR {
		b.warn(warn.ContentUnsupported, warn.Approximated, "vertical text with lines from left to right is laid out as from right to left")
	}
	lw, lh := inner.Width, inner.Height
	if vertical {
		lw, lh = lh, lw
	}
	if body.Autofit == AutofitStretch && !vertical {
		return b.stretchText(body, inner, res)
	}
	f := b.fitBody(body, lw, lh)
	if g, ok := b.groupFit[body.ShrinkGroup]; ok && body.ShrinkGroup != 0 && body.Autofit == AutofitShrink {
		f = g
	}
	dx := 0.0
	centred := powerPointCentresBlock(b.doc, body)
	if body.HorizontalAnchor != HorizontalStart && (!vertical || centred) {
		// A block of lines anchored at the centre or the end is as wide as
		// its widest line, each line aligned in it, and sits at the
		// rectangle's centre or end (SdrTextObj::TakeTextRect,
		// SDRTEXTHORZADJUST_CENTER and _RIGHT).
		if nat := b.naturalWidth(body, f) + blockSlack; nat < lw {
			dx = lw - nat
			if body.HorizontalAnchor == HorizontalCenter {
				dx /= 2
			}
			lw = nat
		}
	}
	if !body.Wrap {
		// Unwrapped lines wider than the rectangle widen the text block to
		// the longest, which overflows the rectangle on the side its first
		// paragraph's alignment leaves free: right of a start-aligned text,
		// both sides of a centred one, left of an end-aligned one
		// (SdrObjCustomShape::TakeTextRect), and past the end of a vertical
		// text's lines alike.
		if nat := b.naturalWidth(body, f); nat > lw {
			share := overflowShare(body)
			if centred {
				share = 0.5
			}
			dx = (lw - nat) * share
			lw = nat
		}
	}
	lay := b.layoutBody(body, lw, lh, f)
	if lay == nil {
		return res
	}
	if centred && body.Wrap && dx == 0 {
		// The lines wrap at the rectangle; the block then narrows to the
		// widest of them and sits in the rectangle's middle.
		if w := b.widestWrapped(body, lay, lw, lh, f) + maxRightMargin(body) + blockSlack; w < lw {
			if narrow := b.layoutBody(body, w, lh, f); narrow != nil {
				dx = float64(lw-w) / 2
				lw, lay = w, narrow
			}
		}
	}
	h := lay.height
	if body.Autofit == AutofitGrow && h > lh && !vertical {
		// Impress grows a frame's height to its text from the anchor:
		// down from the top, up from the bottom, both ways from the
		// middle (SdrTextObj::AdjustTextFrameWidthAndHeight).
		grow := h - lh
		switch body.Anchor {
		case AnchorBottom:
			box.Y -= grow
		case AnchorMiddle:
			box.Y -= float64(grow / 2)
		case AnchorTop:
		}
		box.Height += grow
		res.box = box
		inner = insetRect(shape.TextRect(g, box), body.Insets)
		lh = inner.Height
	}
	dy := 0.0
	if body.Columns <= 1 {
		switch body.Anchor {
		case AnchorMiddle:
			dy = (lh - h) / 2
		case AnchorBottom:
			dy = lh - h
		case AnchorTop:
		}
		if body.Anchor == AnchorBottom && body.FontLineSpacing && !vertical {
			// OpenDocument text anchored at the bottom of a rectangle it
			// overflows starts at the rectangle's top instead.
			dy = max(dy, 0)
		}
	}
	var m typeset.Matrix
	switch body.Vertical {
	case css.HorizontalTB:
		m = translate(inner.X+dx, inner.Y+dy)
	case css.SidewaysLR:
		// Turned a quarter turn counter-clockwise: the lines read bottom
		// to top and stack left to right (21.1.10.83 vert270).
		m = typeset.Matrix{A: 0, B: -1, C: 1, D: 0, E: inner.X + dy, F: inner.Y + inner.Height - dx}
	case css.VerticalRL, css.VerticalLR, css.SidewaysRL:
		// Turned a quarter turn clockwise: the lines read top to bottom
		// and stack right to left (vert, eaVert).
		m = typeset.Matrix{A: 0, B: 1, C: -1, D: 0, E: inner.X + inner.Width - dy, F: inner.Y + dx}
	default:
		m = translate(inner.X+dx, inner.Y+dy)
	}
	if body.Rotation != 0 {
		m = then(m, skewedAboutCentre(shape.TextRect(g, box), body.Rotation, 0, false, false))
	}
	frags := lay.frags
	if body.Warp != nil {
		if warped, ok := b.warpText(body, res.box, m, lw, frags); ok {
			res.frags = warped
			res.structure = lay.structure
			return res
		}
	}
	if body.Clip && !vertical {
		// A clipped body draws no line whose baseline falls outside its
		// rectangle and cuts the others at it, as Impress prints a text
		// whose overflow behaviour is clip.
		frags = keepLines(frags, typeset.Identity, -dy, lh-dy)
		r := shape.TextRect(g, box)
		res.frags = []typeset.Fragment{&typeset.Group{
			Transform: typeset.Identity, Opacity: 1,
			Clip:    &typeset.Path{Ops: rectOps(r), Fill: &typeset.Fill{}},
			Content: group(m, frags),
		}}
	} else {
		res.frags = group(m, frags)
	}
	for _, l := range lay.links {
		if !b.resolveJump(&l) {
			continue
		}
		l.Rect = transformRect(m, l.Rect)
		res.links = append(res.links, l)
	}
	res.structure = lay.structure
	return res
}

// warpText returns the laid-out text frags of body, mapped by m, warped
// to the shape body.Warp fitted to the shape's box, and true; false when
// the warp draws nothing, the caller drawing the text unwarped. Vertical
// text is not warped, with a warning. The links of the text keep their
// unwarped places.
func (b *builder) warpText(body *TextBody, box typeset.Rect, m typeset.Matrix, block float64, frags []typeset.Fragment) ([]typeset.Fragment, bool) {
	lanes := shape.WarpLanes(body.Warp, box)
	if lanes == nil {
		if body.Warp.Custom != nil || body.Warp.Preset != "textNoShape" {
			b.warn(warn.ContentUnsupported, warn.Approximated, "warped text of an unknown shape is drawn unwarped")
		}
		return nil, false
	}
	if body.Vertical.Vertical() {
		b.warn(warn.ContentUnsupported, warn.Approximated, "warped vertical text is drawn unwarped")
		return nil, false
	}
	out, notes, err := officefx.WarpText(b.ctx, frags, m, &officefx.Warp{Lanes: lanes, Fit: body.WarpFit, Block: block, LinesOutside: powerPointLinesOutside(b.doc)}, &b.outlines)
	if err != nil {
		return nil, false
	}
	if notes.Capped {
		b.warn(warn.LimitReached, warn.Approximated, fmt.Sprintf("warped text of more than %d glyphs is drawn unwarped", officefx.MaxWarpGlyphs))
	}
	if notes.Effects {
		b.warn(warn.ContentUnsupported, warn.Omitted, "the effects of warped text are not drawn")
	}
	// The warped outlines hold path operations a document can multiply
	// by its glyphs; the page holds them until it is handed over.
	cost := typeset.ContentCost(out)
	if err := b.mem.Charge(cost, "warped text"); err != nil {
		// The refusal stops the conversion, which reports it.
		return nil, true
	}
	b.warped += cost
	if body.WarpFill != nil || body.WarpLine != nil {
		out = b.paintWarped(body, box, out, 0)
	}
	return out, true
}

// paintWarped returns the warped text frags with each glyph outline
// filled by body.WarpFill, fitted to the shape's box, and stroked by
// body.WarpLine; glyphs set on a path as text keep their colour.
func (b *builder) paintWarped(body *TextBody, box typeset.Rect, frags []typeset.Fragment, depth int) []typeset.Fragment {
	out := make([]typeset.Fragment, 0, len(frags))
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Path:
			if v.Fill == nil || depth > maxGroupDepth {
				out = append(out, v)
				continue
			}
			if body.WarpFill != nil {
				fill := b.fill(body.WarpFill, box, rectOps(box), shape.FillNorm)
				out = append(out, &typeset.Group{Transform: typeset.Identity, Opacity: 1, Clip: &typeset.Path{Ops: v.Ops, Fill: &typeset.Fill{Rule: v.Fill.Rule}}, Content: fill})
			} else {
				out = append(out, v)
			}
			out = append(out, b.stroke(body.WarpLine, v.Ops)...)
		case *typeset.Group:
			if v.Transform != typeset.Identity {
				out = append(out, v)
				continue
			}
			g := *v
			g.Content = b.paintWarped(body, box, v.Content, depth+1)
			out = append(out, &g)
		default:
			out = append(out, f)
		}
	}
	return out
}

// jumpPrefix starts the target a run's jump to a slide carries through
// the layout, the slide's show position after it; the NUL keeps it apart
// from any hyperlink a file gives.
const jumpPrefix = "\x00slide:"

// resolveJump turns the link l of a run's jump into a link to the page of
// the slide it names, and reports whether l is still a link: false for a
// jump to a slide that does not print.
func (b *builder) resolveJump(l *typeset.Link) bool {
	n, ok := strings.CutPrefix(l.URI, jumpPrefix)
	if !ok {
		return true
	}
	i, err := intconv.Atoi(n)
	if err != nil || i < 1 || i > len(b.doc.Slides) {
		return false
	}
	d := b.dest(b.doc.Slides[i-1])
	if d == nil {
		return false
	}
	l.URI, l.Dest = "", d
	return true
}

// blockSlack widens a block of lines as wide as its widest line, in
// points, so that rounding never wraps that line.
const blockSlack = 0.01

// powerPointCentresBlock reports whether the block of lines of body sits
// in the middle of its text rectangle as PowerPoint places a body anchored
// at the centre (ECMA-376 Part 1, 21.1.2.1.1 bodyPr anchorCtr): the block
// as wide as its widest line, once wrapped at the rectangle, each line
// aligned in it; an unwrapped block wider than the rectangle overflows
// both of its sides; vertical text alike, along its lines. Impress
// centres only a horizontal block narrower than its rectangle, and
// overflows an unwrapped one on the side its alignment leaves free.
// Right-to-left text keeps Impress's placement: no page shows it.
//
// # Corpus evidence
//
//   - probe, pptx: 70-point boxes of Calibri 18 with no insets, wrapped,
//     anchorCtr: lines aligned left start where the widest wrapped line,
//     centred, starts (41.8 for 40 + (70 - 66.5) / 2); lines aligned right
//     end where it ends (368.7 for 368.2); centred lines stay centred; a
//     vert270 line of 116 points in a 150-point box is centred along it.
//   - libreoffice/pptx: oox/tdf151008_eaVertAnchor.pptx's unwrapped eaVert
//     lines of 97 points in 56.7-point squares anchored at the centre
//     overflow both ends equally (207.2 to 303.9 over 226.8 to 283.5).
//   - libreoffice/pptx: oox/tdf149551_TextRot.pptx prints its wrapped,
//     left-aligned Courier New lines centred on the wider one.
func powerPointCentresBlock(doc *Document, body *TextBody) bool {
	if doc == nil || doc.Fallback == text.FallbackLibreOffice {
		return false
	}
	if body.HorizontalAnchor != HorizontalCenter || body.RightToLeft {
		return false
	}
	for i := range body.Paragraphs {
		if body.Paragraphs[i].RightToLeft {
			return false
		}
	}
	return true
}

// maxRightMargin returns the largest right margin of body's paragraphs,
// at least 0.
func maxRightMargin(body *TextBody) float64 {
	m := 0.0
	for i := range body.Paragraphs {
		if v := body.Paragraphs[i].MarginRight; numeric.Finite(v) {
			m = max(m, v)
		}
	}
	return m
}

// widestWrapped returns the right edge of the widest line of body wrapped
// at lw, as if every line started at the left: lay itself when every
// paragraph is aligned at the start, else body laid out again with its
// paragraphs aligned left.
func (b *builder) widestWrapped(body *TextBody, lay *laidBody, lw, lh float64, f fit) float64 {
	left := true
	for i := range body.Paragraphs {
		if a := body.Paragraphs[i].Align; a != css.AlignAuto && a != css.AlignStart && a != css.AlignLeft {
			left = false
			break
		}
	}
	if left {
		return b.widestLine(lay.frags)
	}
	nb := *body
	nb.Paragraphs = make([]Paragraph, len(body.Paragraphs))
	copy(nb.Paragraphs, body.Paragraphs)
	for i := range nb.Paragraphs {
		nb.Paragraphs[i].Align = css.AlignLeft
	}
	l := b.layoutBody(&nb, lw, lh, f)
	if l == nil {
		return lw
	}
	return b.widestLine(l.frags)
}

// widestLine returns the right edge of the furthest glyph of frags, a text
// body laid out at the origin, white space left out: the width of its
// widest line when every line starts at the left.
func (b *builder) widestLine(frags []typeset.Fragment) float64 {
	right := 0.0
	var walk func([]typeset.Fragment, typeset.Matrix, int)
	walk = func(frags []typeset.Fragment, m typeset.Matrix, depth int) {
		if depth > maxGroupDepth {
			return
		}
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Text:
				hs := 1.0
				if v.HScale > 0 {
					hs = v.HScale
				}
				for i, g := range v.Glyphs {
					if strings.TrimSpace(g.Text) == "" {
						continue
					}
					x := v.Origin.X + float64(g.X*hs) + b.outlines.Advance(v, i)
					right = max(right, float64(m.A*x)+m.E)
				}
			case *typeset.Group:
				gm := v.Transform
				if gm == (typeset.Matrix{}) {
					gm = typeset.Identity
				}
				walk(v.Content, then(gm, m), depth+1)
			}
		}
	}
	walk(frags, typeset.Identity, 0)
	return right
}

// stretchText lays the body out unwrapped and scales it to fill the
// rectangle inner, its font size to the height and its glyphs' width to
// the width, as Impress draws a frame whose text fits its size
// (SdrTextObj::impDecomposeStretchTextPrimitive; ODF 1.3 Part 3, 20.130
// draw:fit-to-size true or all).
//
// # Corpus evidence
//
//   - libreoffice/odp: sd-data/fit-to-size.fodp's frames 2 (true) and 3 (all), 357 by
//     144 and 357 by 127 points, print "foo" 8 times its width and 2 and
//     1.78 times its height, the two lines filling the frame.
func (b *builder) stretchText(body *TextBody, inner typeset.Rect, res *textResult) *textResult {
	nb := *body
	nb.Wrap, nb.Autofit = false, AutofitNone
	w := b.naturalWidth(&nb, noFit) + blockSlack
	lay := b.layoutBody(&nb, w, inner.Height, noFit)
	if lay == nil || w <= blockSlack || !numeric.Positive(lay.height) {
		return res
	}
	sx, sy := inner.Width/w, inner.Height/lay.height
	m := typeset.Matrix{A: sx, D: sy, E: inner.X, F: inner.Y}
	res.frags = group(m, lay.frags)
	for _, l := range lay.links {
		if !b.resolveJump(&l) {
			continue
		}
		l.Rect = transformRect(m, l.Rect)
		res.links = append(res.links, l)
	}
	res.structure = lay.structure
	return res
}

// keepLines returns the fragments of frags, mapped by m, less the text
// runs whose baseline lies above top or below bottom.
func keepLines(frags []typeset.Fragment, m typeset.Matrix, top, bottom float64) []typeset.Fragment {
	out := make([]typeset.Fragment, 0, len(frags))
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Text:
			_, y := apply(m, v.Origin.X, v.Origin.Y)
			if y < top-0.01 || y > bottom+0.01 {
				continue
			}
		case *typeset.Group:
			t := v.Transform
			if t == (typeset.Matrix{}) {
				t = typeset.Identity
			}
			g := *v
			g.Content = keepLines(v.Content, then(t, m), top, bottom)
			out = append(out, &g)
			continue
		}
		out = append(out, f)
	}
	return out
}

// naturalWidth returns the width of the body's longest line laid out
// without wrapping at the scaling f, its paragraph's margins included.
func (b *builder) naturalWidth(body *TextBody, f fit) float64 {
	return b.contentWidth(body, f, false)
}

// contentWidth returns the body's widest paragraph at the scaling f, its
// margins included: laid out without wrapping, or, when words is set, as
// wide as its widest word (a paragraph's min-content width).
func (b *builder) contentWidth(body *TextBody, f fit, words bool) float64 {
	best := 0.0
	for i := range body.Paragraphs {
		if i >= maxParagraphs {
			break
		}
		p := &body.Paragraphs[i]
		var runs []text.ParagraphRun
		for k := range p.Runs {
			if k >= maxRuns {
				break
			}
			r := &p.Runs[k]
			ts := b.textStyle(&r.Style, f.font)
			if r.Break {
				runs = append(runs, text.ParagraphRun{Text: "\n", Style: ts})
				continue
			}
			if s := b.fieldText(r); s != "" {
				runs = append(runs, text.ParagraphRun{Text: s, Style: ts})
			}
		}
		if len(runs) == 0 {
			continue
		}
		sp, _, err := b.fonts.Shape(b.ctx, runs, text.ParagraphOptions{})
		if err != nil {
			continue
		}
		ml := max(p.MarginLeft, 0) + max(p.Indent, 0) + max(p.MarginRight, 0)
		content := sp.MaxContent()
		if words {
			content = sp.MinContent()
		}
		if w := content + ml; w > best && !math.IsInf(w, 0) {
			best = w
		}
	}
	return min(best, office.MaxPageSide)
}

// overflowShare returns the share of the overflow of a body's unwrapped
// text wider than its rectangle that falls left of it: by its first
// paragraph's alignment, none when it starts at the left, half when it is
// centred, all when it ends at the right.
func overflowShare(body *TextBody) float64 {
	if len(body.Paragraphs) == 0 {
		return 0
	}
	p := &body.Paragraphs[0]
	rtl := p.RightToLeft || body.RightToLeft
	switch p.Align {
	case css.AlignCenter:
		return 0.5
	case css.AlignRight:
		return 1
	case css.AlignEnd:
		if !rtl {
			return 1
		}
	case css.AlignStart, css.AlignAuto, css.AlignJustify, css.AlignJustifyAll:
		if rtl {
			return 1
		}
	case css.AlignLeft:
	}
	return 0
}

// fitBody returns the scaling the body is laid out at in a box w by h: a
// body that shrinks to fit takes the first of Impress's scale levels at
// which its text fits, else the last one, and none when it fits as it is
// (ImpEditEngine::ScaleContentToFitWindow). The scale a file saved is not
// read: Impress computes the fit again whatever the file says. A body
// with SavedFit takes the scale its file saved instead, as PowerPoint
// prints it (ECMA-376 Part 1, 21.1.2.1.3 normAutofit fontScale and
// lnSpcReduction, 100% and 0% by default), whether the text fits or not.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/font-scale.pptx saves a font scale of 85% with 20% less
//     spacing and prints at the fourth level whatever it saves (100%, 50% or
//     none measured): its three empty closing paragraphs would push the
//     fit to 70%.
//   - litho/pptx: size-banner's title and subtitle, whose shrinking bodies
//     save no scale, print in PowerPoint's reference at their full size,
//     overflowing the inch-tall slide; text-autofit's bodies
//     print at the scale each saves.
func (b *builder) fitBody(body *TextBody, w, h float64) fit {
	if body.Autofit != AutofitShrink {
		return noFit
	}
	if body.SavedFit {
		return savedFit(body)
	}
	cols := max(body.Columns, 1)
	colW := w
	if cols > 1 {
		colW = (w - float64(float64(cols-1)*max(body.ColumnGap, 0))) / float64(cols)
		colW = max(colW, 1)
	}
	room := float64(h * float64(cols))
	// The empty paragraphs that end the body take no room in the fit, the
	// first paragraph excepted (ImpEditEngine::FormatParagraphs and
	// isInEmptyClusterAtTheEnd).
	paras := body.Paragraphs
	for len(paras) > 1 && emptyParagraph(&paras[len(paras)-1]) {
		paras = paras[:len(paras)-1]
	}
	fits := func(f fit) bool {
		lay := b.layoutBody(&TextBody{
			Paragraphs: paras, Wrap: body.Wrap, RightToLeft: body.RightToLeft,
			FontLineSpacing: body.FontLineSpacing, MaxParagraphSpacing: body.MaxParagraphSpacing,
		}, colW, h, f)
		return lay == nil || lay.height <= room+1e-6
	}
	if fits(noFit) {
		return noFit
	}
	for _, f := range scaleLevels {
		if fits(f) {
			return f
		}
	}
	return scaleLevels[len(scaleLevels)-1]
}

// savedFit returns the scaling a body's file saved, each part in (0, 1],
// none for a part it saved none of or out of range.
func savedFit(body *TextBody) fit {
	f := noFit
	if k := body.FontScale; k > 0 && k <= 1 {
		f.font = k
	}
	if r := body.SpacingReduction; r > 0 && r < 1 {
		f.spacing = 1 - r
	}
	return f
}

// emptyParagraph reports whether p holds no character at all, a line
// break being one.
func emptyParagraph(p *Paragraph) bool {
	for i := range p.Runs {
		if r := &p.Runs[i]; r.Break || r.Text != "" || r.Field != FieldNone {
			return false
		}
	}
	return true
}

// roundPt rounds a font size to a whole point, as Impress rounds the
// sizes of scaled text (ImpEditEngine::SeekCursor, roundToNearestPt).
func roundPt(v float64) float64 {
	return math.Max(1, math.Round(v))
}

// scaledSize returns the size s at the font scale k: s rounded to a
// point, scaled, and rounded again when k is not 1.
func scaledSize(s, k float64) float64 {
	if !numeric.Positive(s) {
		s = defaultFontSize
	}
	s = min(s, maxFontSize)
	if k == 1 {
		return s
	}
	return min(roundPt(roundPt(s)*k), maxFontSize)
}

// layoutBody lays the body out at the origin in a box w wide and h tall
// at the scaling f, nil when the layout fails.
func (b *builder) layoutBody(body *TextBody, w, h float64, f fit) *laidBody {
	paras := body.Paragraphs
	if len(paras) > maxParagraphs {
		b.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("a text body has more than %d paragraphs; the rest are left out", maxParagraphs))
		paras = paras[:maxParagraphs]
	}
	root := css.Initial()
	root.Display = css.Block
	root.Width = css.Pts(w)
	root.FontFamilies = []string{fallbackFamily}
	root.FontSize = defaultFontSize
	if body.RightToLeft {
		root.Direction = css.RTL
	}
	if body.Columns > 1 {
		root.Columns = css.Columns{Count: min(body.Columns, maxColumns), Width: css.Dimension{Kind: css.Auto}, Fill: css.FillAuto, Height: css.Dimension{Kind: css.Auto}}
		root.ColumnGap = css.Pts(max(body.ColumnGap, 0))
		root.Height = css.Pts(h)
	}
	// OpenDocument text, spaced by its faces, moves its scripts
	// automatically.
	b.autoScript = body.FontLineSpacing
	b.recode = map[string]recoded{}
	tree := &layout.Node{Kind: layout.Element, Style: root, ID: "slides-body"}
	nums := numbers(paras)
	budget := maxBodyText
	for i := range paras {
		tree.Children = append(tree.Children, b.paragraph(&paras[i], i, len(paras), body, f, nums[i], &budget))
	}
	res, err := layout.Layout(b.ctx, tree, b.fonts, layout.Options{
		Page:     layout.PageBox{Width: min(max(w, 1), office.MaxPageSide), Height: office.MaxPageSide},
		Lang:     b.lang,
		Fallback: b.doc.Fallback,
		MaxPages: 2,
		Logger:   b.opts.Logger,
		// A slide's run effects count against its effect pixels with its
		// shapes' ([effect.MaxPagePixels]).
		EffectPixels: &b.effectPixels,
	})
	if err != nil {
		if !errors.Is(err, b.ctx.Err()) {
			b.warn(warn.TextNotLaidOut, warn.Omitted, "a text body could not be laid out: "+err.Error())
		}
		return nil
	}
	b.warnings = append(b.warnings, res.Warnings...)
	out := &laidBody{}
	if len(res.Pages) > 0 {
		out.frags = unclipped(res.Pages[0].Content, min(max(w, 1), office.MaxPageSide))
		out.links = res.Pages[0].Links
		if len(b.recode) > 0 {
			out.frags = b.recodeLabels(out.frags, b.recode)
		}
	}
	if res.Structure != nil {
		out.structure = res.Structure.Kids
	}
	if e, ok := res.Extents["slides-body"]; ok {
		out.height = e.Height
	}
	return out
}

// unclipped returns frags, the content of a text body's one page laid out
// w wide, with the clip of the page area the layout cuts each run of the
// flow at opened above and below: a text body is no page, and what its
// lines draw past their tops, a bullet larger than its text or glyphs
// taller than a line that spacing narrows, prints, as PowerPoint prints
// it. The clip keeps the page's sides. The groups are copied, frags left
// unchanged.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/tdf126234.pptx's bullet of 400% of 18 points
//     prints whole above its line's top.
func unclipped(frags []typeset.Fragment, w float64) []typeset.Fragment {
	out := make([]typeset.Fragment, len(frags))
	open := &typeset.Path{Ops: rectOps(typeset.Rect{X: 0, Y: -office.MaxPageSide, Width: w, Height: 3 * office.MaxPageSide}), Fill: &typeset.Fill{}}
	for i, f := range frags {
		if g, ok := f.(*typeset.Group); ok && g.Clip != nil {
			c := *g
			c.Clip = open
			f = &c
		}
		out[i] = f
	}
	return out
}

// maxColumns caps a text body's columns. Anti-DoS guard, not
// spec-mandated: DrawingML stops at 16 (21.1.2.1.1 numCol).
const maxColumns = 16

// numbers returns the number of each numbered paragraph of paras, 0 for
// the others: paragraphs count per level, from the StartAt of the first
// of a run of paragraphs of one level and format, a paragraph of a
// shallower level restarting the deeper ones, one of the level without
// the format restarting its own (Outliner::ImplCalcBulletText). A
// paragraph whose StartAt is not 0 and differs from the one its level's
// count began at restarts the count there, as a new start value starts a
// new numbering; the same start again continues it, as the paragraphs of
// one list all state it.
func numbers(paras []Paragraph) []int {
	out := make([]int, len(paras))
	type level struct {
		format string
		n      int
		// start is the StartAt the level's count began from.
		start int
	}
	var levels []level
	for i := range paras {
		p := &paras[i]
		lv := min(max(p.Level, 0), maxLevel)
		if len(levels) > lv+1 {
			levels = levels[:lv+1]
		}
		for len(levels) <= lv {
			levels = append(levels, level{})
		}
		if p.Bullet == nil || p.Bullet.Kind != BulletNumber || len(p.Runs) == 0 {
			if len(p.Runs) > 0 {
				levels[lv] = level{}
			}
			continue
		}
		at := p.Bullet.StartAt
		if l := &levels[lv]; l.format == p.Bullet.Format && l.n > 0 && (at <= 0 || at == l.start) {
			l.n = min(l.n+1, maxNumber)
		} else {
			start := at
			if start <= 0 {
				start = 1
			}
			levels[lv] = level{format: p.Bullet.Format, n: min(start, maxNumber), start: at}
		}
		out[i] = levels[lv].n
	}
	return out
}

// maxLevel caps a paragraph's list level. Anti-DoS guard, not
// spec-mandated: DrawingML has nine levels (21.1.2.2.7 lvl).
const maxLevel = 8

// maxNumber caps a list number. Anti-DoS guard, not spec-mandated:
// DrawingML stops at 32,767 (21.1.2.4.1 buAutoNum startAt).
const maxNumber = 32767

// lineBox is how the lines of a paragraph are spaced: each line's height
// and its baseline's distance below the line's top.
type lineBox struct {
	height, ascent float64
	// text is the line's natural height, before the spacing applies.
	text float64
}

// lines returns the line box of a paragraph whose largest run is set in
// the style ts at the scaling f, with the faces' metrics when fontLines (ImpEditEngine::CreateLines, the line
// spacing rules): a natural line is the ascent plus descent of the faces
// that draw the runs of that size (drawnMetrics), or
// 1.2 times the size with the size as ascent when the body spaces lines
// independently of the faces; a proportional spacing above 1 adds its
// room above the glyphs, below 1 it shrinks the line and caps the ascent
// at four fifths of it; an exact spacing changes the ascent by the
// difference.
func (b *builder) lines(p *Paragraph, ts text.Style, f fit, fontLines bool, runs []Run, big *RunStyle) lineBox {
	s := ts.Size
	a, n := s, float64(s*fixedLineFactor)
	if fontLines {
		m, fc := b.faceAndMetrics(ts)
		if d, ok := b.drawnMetrics(runs, ts, f.font, fc, big); ok {
			// Shaped bounds are on the shaper's 1/64 grid: a face that
			// draws the runs itself keeps its own metrics.
			if math.Abs(d.Ascent-m.Ascent) > shapedGrid {
				m.Ascent = d.Ascent
			}
			if math.Abs(d.Descent-m.Descent) > shapedGrid {
				m.Descent = d.Descent
			}
		}
		a, n = m.Ascent, m.Ascent+m.Descent
	}
	sf := f.spacing
	lb := lineBox{height: n, ascent: a, text: n}
	switch prop := p.LineSpacing; {
	case numeric.PositiveFinite(p.LineSpacingPoints):
		lb.height = float64(min(p.LineSpacingPoints, maxCoordinate) * sf)
		lb.ascent = a + lb.height - n
	case !numeric.PositiveFinite(prop) || prop == 1:
		if sf < 1 {
			lb.height = float64(n * sf)
			lb.ascent = min(a, n*sf)
		}
	case prop < 1:
		lb.height = float64(n * prop * sf)
		lb.ascent = min(a, n*sf*prop*0.8)
	default:
		lb.height = float64(n * min(prop, maxLineSpacing) * sf)
		lb.ascent = a - (n - lb.height)
	}
	if least := p.LineSpacingAtLeast; least > lb.height && !math.IsInf(least, 0) {
		// A line shorter than the floor grows to it, the room going above
		// its glyphs (SvxLineSpaceRule::Min).
		least = min(least, maxCoordinate)
		lb.ascent += least - lb.height
		lb.height = least
	}
	lb.height = max(lb.height, 0.01)
	return lb
}

// drawnMetrics returns the line bounds of the faces that draw the runs
// set at the size of ts at the font scale k: the largest ascent and the
// largest descent among them, false when no such run draws a glyph. Impress measures a line by
// the fonts its portions are drawn in (ImpEditEngine::CreateLines,
// FormatterFontMetric), so a run whose family is missing, or whose
// characters fall back to another face, takes that face's height: a
// Chinese run in a Latin family is as tall as its CJK face.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/tdf153161_FlushToSave.odp, Chinese text in a
//     family the fonts lack, 140 % proportional spacing: Impress spaces
//     the lines 40.05 pt apart at 22 pt with 90 % spacing, 1.448 em times
//     1.4 times 0.9, the height of Noto Sans CJK SC, which draws it.
func (b *builder) drawnMetrics(runs []Run, ts text.Style, k float64, fc *text.Face, big *RunStyle) (text.Metrics, bool) {
	if len(runs) == 0 || ownFaceDraws(runs, ts, k, fc, big) {
		return text.Metrics{}, false
	}
	key := drawnKey{run: &runs[0], size: ts.Size}
	if d, ok := b.drawn[key]; ok {
		return d.m, d.ok
	}
	m, ok := b.measureDrawn(runs, ts, k)
	if b.drawn == nil {
		b.drawn = map[drawnKey]drawnBounds{}
	}
	if len(b.drawn) < maxMetrics {
		b.drawn[key] = drawnBounds{m: m, ok: ok}
	}
	return m, ok
}

// ownFaceDraws reports whether fc, the face the largest run big matches
// in ts, draws every run of ts's size by itself: each such run is set in
// big's families, weight, posture and language, and fc holds its
// characters. The line then takes fc's metrics, as before any run is
// shaped.
func ownFaceDraws(runs []Run, ts text.Style, k float64, fc *text.Face, big *RunStyle) bool {
	if fc == nil || big == nil {
		return false
	}
	for i := range runs {
		r := &runs[i]
		if r.Break || r.Formula != nil || r.Text == "" || scaledSize(r.Style.Size, k) != ts.Size {
			continue
		}
		if r.Style.Bold != big.Bold || r.Style.Italic != big.Italic || r.Style.Lang != big.Lang ||
			!slices.Equal(r.Style.Families, big.Families) || !drawsAll(fc, r.Text) {
			return false
		}
	}
	return true
}

// drawnKey and drawnBounds cache drawnMetrics: a paragraph is laid out
// once a scale level a fit tries.
type drawnKey struct {
	run  *Run
	size float64
}

type drawnBounds struct {
	m  text.Metrics
	ok bool
}

// measureDrawn shapes the runs drawnMetrics measures.
//
// A run its own face draws whole takes that face's metrics unshaped; only
// a run with characters another face draws is shaped.
func (b *builder) measureDrawn(runs []Run, ts text.Style, k float64) (text.Metrics, bool) {
	var in []text.ParagraphRun
	var m text.Metrics
	left := maxMeasuredText
	for i := range runs {
		r := &runs[i]
		if r.Break || r.Formula != nil || r.Text == "" || left <= 0 {
			continue
		}
		rs := b.textStyle(&r.Style, k)
		if rs.Size != ts.Size {
			continue
		}
		t := r.Text
		if n := utf8.RuneCountInString(t); n > left {
			t = string([]rune(t)[:left])
		}
		left -= utf8.RuneCountInString(t)
		st := ts
		st.Families, st.Weight, st.Italic, st.Lang = rs.Families, rs.Weight, rs.Italic, rs.Lang
		st.Fallback = b.doc.Fallback
		if em, ok := b.ownFace(r, st); ok {
			m.Ascent = max(m.Ascent, float64(em.Ascent*st.Size))
			m.Descent = max(m.Descent, float64(em.Descent*st.Size))
			continue
		}
		in = append(in, text.ParagraphRun{Text: t, Style: st})
	}
	if len(in) == 0 {
		return m, m.Ascent+m.Descent > 0
	}
	p, _, err := b.fonts.Shape(b.ctx, in, text.ParagraphOptions{})
	if err != nil {
		return text.Metrics{}, false
	}
	for _, sp := range p.Line(0, p.Len(), false).Spans {
		if sp.Text == nil || len(sp.Text.Glyphs) == 0 {
			continue
		}
		m.Ascent = max(m.Ascent, sp.Ascent)
		m.Descent = max(m.Descent, sp.Descent)
	}
	return m, m.Ascent+m.Descent > 0
}

// ownFace returns the metrics at 1 point of the face the run r matches in
// st, true when that face draws every character of r; the answer is kept
// for the run, whatever its size.
func (b *builder) ownFace(r *Run, st text.Style) (text.Metrics, bool) {
	if o, ok := b.own[r]; ok {
		return o.em, o.ok
	}
	var o ownBounds
	if fc := b.faceOf(st); fc != nil && drawsAll(fc, r.Text) {
		o = ownBounds{em: fc.Metrics(1), ok: true}
	}
	if b.own == nil {
		b.own = map[*Run]ownBounds{}
	}
	if len(b.own) < maxMetrics {
		b.own[r] = o
	}
	return o.em, o.ok
}

// faceOf returns the face st matches, kept by its families, weight,
// posture and language.
func (b *builder) faceOf(st text.Style) *text.Face {
	k := faceKey{families: strings.Join(st.Families, ","), weight: st.Weight, italic: st.Italic, lang: st.Lang}
	if fc, ok := b.faces[k]; ok {
		return fc
	}
	fc := b.fonts.Match(st)
	if b.faces == nil {
		b.faces = map[faceKey]*text.Face{}
	}
	if len(b.faces) < maxMetrics {
		b.faces[k] = fc
	}
	return fc
}

// faceKey is what faceOf keeps a face by.
type faceKey struct {
	families, lang string
	weight         int
	italic         bool
}

// ownBounds is what ownFace found for a run.
type ownBounds struct {
	em text.Metrics
	ok bool
}

// drawsAll reports whether the face fc holds every character of s but
// the white space and the controls, which draw no ink of their own.
func drawsAll(fc *text.Face, s string) bool {
	for _, r := range s {
		if r < 0x20 || unicode.IsSpace(r) {
			continue
		}
		if !fc.Covers(r) {
			return false
		}
	}
	return true
}

// shapedGrid is the step of the line bounds the shaper gives, 1/64 of a
// point: a shaped bound within it of a face's own is that face's.
const shapedGrid = 1.0 / 64

// maxMeasuredText caps the characters of a paragraph shaped to find the
// faces that draw it. Anti-DoS guard, not spec-mandated: the faces of a
// paragraph show in its first characters, and a slide's paragraph seldom
// holds more.
const maxMeasuredText = 1024

// maxLineSpacing caps a proportional line spacing. Anti-DoS guard, not
// spec-mandated: DrawingML allows 132 times single spacing (21.1.10.64
// ST_TextSpacingPercentOrPercentString).
const maxLineSpacing = 132

// faceMetrics returns the metrics of the face ts matches at its size.
func (b *builder) faceMetrics(ts text.Style) text.Metrics {
	m, _ := b.faceAndMetrics(ts)
	return m
}

// faceAndMetrics returns the metrics of the face ts matches at its size
// and the face, nil when none matches.
func (b *builder) faceAndMetrics(ts text.Style) (text.Metrics, *text.Face) {
	k := fmt.Sprintf("%s|%d|%v|%g", strings.Join(ts.Families, ","), ts.Weight, ts.Italic, ts.Size)
	if e, ok := b.metrics[k]; ok {
		return e.m, e.fc
	}
	m := text.Metrics{Ascent: ts.Size * 0.9, Descent: ts.Size * 0.2}
	fc := b.fonts.Match(ts)
	if fc != nil {
		if mm := fc.Metrics(ts.Size); mm.Ascent+mm.Descent > 0 {
			m = mm
		}
	}
	if len(b.metrics) < maxMetrics {
		b.metrics[k] = faceEntry{m: m, fc: fc}
	}
	return m, fc
}

// faceEntry is a face faceAndMetrics matched, with its metrics.
type faceEntry struct {
	m  text.Metrics
	fc *text.Face
}

// maxMetrics caps the cached face metrics. Anti-DoS guard, not
// spec-mandated: a deck uses a few dozen faces and sizes.
const maxMetrics = 4096

// width returns the advance of s set in ts on one line.
func (b *builder) width(s string, ts text.Style) float64 {
	if s == "" {
		return 0
	}
	p, _, err := b.fonts.Shape(b.ctx, []text.ParagraphRun{{Text: s, Style: ts}}, text.ParagraphOptions{})
	if err != nil {
		return 0
	}
	return p.MaxContent()
}

// families returns the family list of a run style, fontconfig's generic
// aliases resolved, ending with the fallback face.
func families(fams []string) []string {
	out := make([]string, 0, len(fams)+1)
	for _, f := range fams {
		f = strings.TrimSpace(f)
		switch strings.ToLower(f) {
		case "":
			continue
		case "sans":
			f = "sans-serif"
		case "mono":
			f = "monospace"
		}
		out = append(out, f)
	}
	return append(out, fallbackFamily)
}

// textStyle returns the text stack's style of rs at the font scale k.
func (b *builder) textStyle(rs *RunStyle, k float64) text.Style {
	w := 400
	if rs.Bold {
		w = 700
	}
	lang := rs.Lang
	if lang == "" {
		lang = b.lang
	}
	return text.Style{Families: families(rs.Families), Weight: w, Italic: rs.Italic, Size: scaledSize(rs.Size, k), Lang: lang, Color: colorOf(textColor(rs.Color))}
}

// textColor returns the colour text of colour c is drawn in: black for
// the zero colour.
func textColor(c css.Color) css.Color {
	if c.A == 0 && c.R == 0 && c.G == 0 && c.B == 0 {
		return css.Color{A: 1}
	}
	return c
}

// symbolRule returns the rule a run of a symbol font draws by: Impress's
// for an OpenDocument presentation, Word's, which PowerPoint shares, for
// the others.
func (b *builder) symbolRule() symbolfont.Rule {
	if b.doc.Fallback == text.FallbackLibreOffice {
		return symbolfont.RuleLibreOffice
	}
	return symbolfont.RuleWord
}

// inline returns the style of a run of style rs at the font scale k,
// inside a paragraph of style ps.
func (b *builder) inline(ps *css.Style, rs *RunStyle, k float64) *css.Style {
	st := css.Initial()
	st.Display = css.Inline
	st.KeepHangul = b.doc != nil && b.doc.KeepHangulWords
	st.WhiteSpace = ps.WhiteSpace
	st.OverflowWrap = ps.OverflowWrap
	st.Direction = ps.Direction
	st.LineRule = ps.LineRule
	st.LineHeight = ps.LineHeight
	st.TabSize = ps.TabSize
	st.FontFamilies = families(rs.Families)
	size := scaledSize(rs.Size, k)
	st.FontSize = size
	st.FontWeight = 400
	if rs.Bold {
		st.FontWeight = 700
	}
	if rs.Italic {
		st.FontStyle = css.FontItalic
	}
	st.Color = textColor(rs.Color)
	st.TextDecorationColor = st.Color
	if rs.Highlight.A > 0 {
		st.BackgroundColor = rs.Highlight
	}
	st.TextEffects = b.textEffects(rs)
	st.TextPaint = b.textPaint(rs)
	// PowerPoint draws emoji in Segoe UI Emoji, at its advances, and
	// breaks no line between an emoji and an emoji or a letter.
	st.OfficeEmoji = b.doc != nil && b.doc.Fallback != text.FallbackLibreOffice
	st.KeepEmoji = st.OfficeEmoji
	st.KeepInWord = powerPointKeepsInWord(b.doc)
	st.ClusterAlone = powerPointClusterAlone(b.doc)
	st.NonNegativeAdvance = powerPointFloorsAdvances(b.doc)
	st.ZigzagWave = powerPointZigzagWave(b.doc)
	if tp := st.TextPaint; tp != nil && (tp.NoFill || len(tp.Gradient) >= 2) && !(st.Color.A > 0) {
		// The glyphs are painted invisible under their paint: a text of
		// no fill, or of a gradient whose first stop is transparent,
		// keeps an opaque colour for its line's opacity.
		st.Color.A = 1
	}
	if rs.Underline {
		st.TextDecoration |= css.Underline
		st.TextDecorationStyle = rs.UnderlineStyle
		switch {
		case rs.UnderlineHeavy:
			// A tenth of the size, unrounded: PowerPoint draws a heavy
			// underline 1.44 pt thick under 14 pt text, twice its single
			// one.
			st.TextDecorationThickness = css.Pts(st.FontSize / 10)
		case rs.UnderlineStyle == css.DecorationDouble:
			// Two lines a twenty-fifth of the size thick: PowerPoint
			// draws them 0.72 pt thick under 18 pt text, where the
			// rounded tenth of a single line would set them 4.5 pt deep.
			st.TextDecorationThickness = css.Pts(st.FontSize / 25)
		}
	}
	if rs.Strike {
		st.TextDecoration |= css.LineThrough
		// One decoration style serves every line of a run: a double
		// strike over an underline keeps the underline's.
		if rs.DoubleStrike && !rs.Underline {
			st.TextDecorationStyle = css.DecorationDouble
		}
	}
	if sp := rs.Spacing; sp != 0 && !math.IsNaN(sp) {
		switch {
		case math.Abs(sp) < maxFontSize:
			st.LetterSpacing = css.Pts(sp * k)
		case sp < 0 && st.NonNegativeAdvance:
			// A spacing past the cap takes every advance to zero, as
			// the cap itself does once floored.
			st.LetterSpacing = css.Pts(-maxFontSize)
		}
	}
	switch rs.Caps {
	case CapsAll:
		st.TextTransform = css.Uppercase
	case CapsSmall:
		st.FontVariant = css.SmallCaps
	case CapsNone:
	}
	switch rs.Script {
	case Superscript:
		st.FontSize = size * scriptScale
		up := size * superRise
		if b.autoScript {
			up = scriptShift(size, b.faceMetrics(b.textStyle(rs, k)), false)
		}
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(up)}
	case Subscript:
		st.FontSize = size * scriptScale
		down := size * subDrop
		if b.autoScript {
			down = scriptShift(size, b.faceMetrics(b.textStyle(rs, k)), true)
		}
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(-down)}
	case Baseline:
	}
	if r := rs.Rise; r != 0 && !math.IsNaN(r) && math.Abs(r) <= maxRise {
		// An explicit shift replaces the script's own, as a fraction of
		// the text's size (ECMA-376 Part 1, 21.1.2.3.9 baseline; ODF 1.3
		// Part 3, 20.374 style:text-position).
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(r * size)}
	}
	return st
}

// paragraph returns the block of the i-th of n paragraphs of body at the
// scaling f, number its list number, budget the characters left to lay
// out.
func (b *builder) paragraph(p *Paragraph, i, n int, body *TextBody, f fit, number int, budget *int) *layout.Node {
	runs := p.Runs
	if len(runs) > maxRuns {
		b.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("a paragraph has more than %d runs; the rest are left out", maxRuns))
		runs = runs[:maxRuns]
	}
	// The style the line heights and the bullet follow: the largest run's,
	// the first run's for the bullet, the paragraph's own when empty.
	first := &p.Style
	if len(runs) > 0 {
		first = &runs[0].Style
	}
	big := first
	for k := range runs {
		if !runs[k].Break && runs[k].Style.Size > big.Size {
			big = &runs[k].Style
		}
	}
	bigTS := b.textStyle(big, f.font)
	// An empty line keeps its spacing whatever the fit scales the others
	// by (ImpEditEngine::CreateAndInsertEmptyLine), its font scaled alone.
	emptyFit := fit{font: f.font, spacing: 1}
	lf := f
	if len(runs) == 0 {
		lf = emptyFit
	}
	lb := b.lines(p, bigTS, lf, body.FontLineSpacing, runs, big)
	boxes := b.formulaBoxes(runs, f.font)
	for _, fb := range boxes {
		// A line grows to hold its formulas: its ascent to the tallest
		// formula's, its descent to the deepest one's.
		if fb != nil {
			below := max(float64(lb.height-lb.ascent), fb.Descent)
			lb.ascent = max(lb.ascent, fb.Ascent)
			lb.height = float64(lb.ascent + below)
		}
	}
	size := bigTS.Size

	st := css.Initial()
	st.Display = css.Block
	st.FontFamilies = bigTS.Families
	st.FontSize = size
	st.Color = textColor(first.Color)
	st.WhiteSpace = css.PreWrap
	// A word wider than the line breaks at the line's end, as Impress
	// breaks it.
	st.OverflowWrap = css.BreakWord
	if !body.Wrap {
		st.WhiteSpace = css.Pre
	}
	if p.RightToLeft || body.RightToLeft {
		st.Direction = css.RTL
	}
	st.LineRule = css.LineRuleExact
	st.LineHeight = css.Pts(lb.height)
	// The exact rule puts the baseline at four fifths of the line; the
	// paragraph moves down by what Impress's ascent adds to that.
	st.Position = css.Relative
	st.Top = css.Pts(lb.ascent - float64(0.8*lb.height))

	st.TabSize = css.Pts(defaultTab)
	if body.FontLineSpacing {
		st.TabSize = css.Pts(odfDefaultTab)
	}
	if t := p.DefaultTab; numeric.PositiveFinite(t) {
		st.TabSize = css.Pts(max(min(t, maxCoordinate), minDefaultTab))
	}
	st.Orphans, st.Widows = 1, 1
	st.TextAlign = p.Align
	if p.Align == css.AlignAuto {
		st.TextAlign = css.AlignStart
	}
	st.TextJustify = p.Justify
	sideLen := func(v float64) float64 {
		if !numeric.Finite(v) {
			return 0
		}
		return min(max(v, -maxCoordinate), maxCoordinate)
	}
	ml, indent, mr := sideLen(p.MarginLeft), sideLen(p.Indent), max(sideLen(p.MarginRight), 0)
	// A first line that would start left of the box pushes the paragraph
	// right until it starts at the box's edge (Outliner::ImpCalcBulletArea).
	if ml+indent < 0 {
		ml = -indent
	}
	ml = max(ml, 0)
	start, end := css.Left, css.Right
	if st.Direction == css.RTL {
		start, end = css.Right, css.Left
	}
	st.Margin[start], st.Margin[end] = css.Pts(ml), css.Pts(mr)
	st.TextIndent = css.Pts(indent)
	stops := make([]css.TabStop, 0, len(p.TabStops))
	for _, t := range p.TabStops {
		if pos := t - ml; pos >= 0 && !math.IsInf(pos, 0) && len(stops) < maxTabStops {
			stops = append(stops, css.TabStop{Pos: pos})
		}
	}
	sortStops(stops)
	st.TabStops = stops
	space := func(pts, pct float64) float64 {
		v := pts
		if pct > 0 && !numeric.Positive(pts) {
			v = pct * pctLine(size)
		}
		if !numeric.PositiveFinite(v) {
			return 0
		}
		return min(v, maxCoordinate) * f.spacing
	}
	// The spacing between two paragraphs is the first's after plus the
	// second's before, or the larger of the two (IsSummationOfParagraphs),
	// the first paragraph's before and the last one's after dropped.
	if i > 0 {
		before := space(p.SpaceBefore, p.SpaceBeforePct)
		if body.MaxParagraphSpacing {
			prev := &body.Paragraphs[i-1]
			psize := size
			if len(prev.Runs) > 0 {
				psize = scaledSize(prev.Runs[0].Style.Size, f.font)
			}
			after := prev.SpaceAfter
			if prev.SpaceAfterPct > 0 && !numeric.Positive(after) {
				after = prev.SpaceAfterPct * pctLine(psize)
			}
			if numeric.PositiveFinite(after) {
				before = max(before, min(after, maxCoordinate)*f.spacing)
			}
		}
		st.Padding[css.Top] = css.Pts(before)
	}
	if i < n-1 && !body.MaxParagraphSpacing {
		st.Padding[css.Bottom] = css.Pts(space(p.SpaceAfter, p.SpaceAfterPct))
	}
	node := &layout.Node{Kind: layout.Element, Style: st, Role: "P"}
	if len(runs) == 0 {
		// An empty paragraph keeps its line.
		st.Height = css.Pts(lb.height)
		return node
	}
	for k := range runs {
		r := &runs[k]
		if r.Break {
			node.Children = append(node.Children, &layout.Node{Kind: layout.LineBreak, Style: st})
			continue
		}
		s := b.fieldText(r)
		if s == "" {
			continue
		}
		if utf8.RuneCountInString(s) > *budget {
			b.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("a text body has more than %d characters; the rest are left out", maxBodyText))
			break
		}
		*budget -= utf8.RuneCountInString(s)
		is := b.inline(st, &r.Style, f.font)
		if k < len(boxes) && boxes[k] != nil {
			fb := boxes[k]
			node.Children = append(node.Children, formulaNode(r, is, fb))
			continue
		}
		href := r.Href
		if href == "" && r.Jump > 0 {
			href = jumpPrefix + strconv.Itoa(r.Jump)
		}
		tn := &layout.Node{Kind: layout.Text, Style: is, Text: s}
		office.SymbolNode(tn, b.fonts, b.symbolRule(), b.symbols)
		run := &layout.Node{Kind: layout.Element, Style: is, Lang: r.Style.Lang, Href: href, Children: []*layout.Node{tn}}
		if href != "" {
			run.Role = "Link"
		}
		node.Children = append(node.Children, run)
	}
	if last := runs[len(runs)-1]; last.Break {
		// A line break that ends the paragraph keeps the empty line after
		// it, as tall as the paragraph's end mark makes it.
		if k := len(node.Children) - 1; k > 0 && hasText(runs) {
			node.Children = node.Children[:k]
			empty := b.lines(p, b.textStyle(&p.Style, f.font), emptyFit, body.FontLineSpacing, nil, nil)
			st.Padding[css.Bottom] = css.Pts(st.Padding[css.Bottom].Value + empty.height)
		} else {
			node.Children = append(node.Children, &layout.Node{Kind: layout.Text, Style: st, Text: "\u200b"})
		}
	}
	if p.Bullet != nil && hasText(runs) {
		b.bullet(node, p, first, lb, f, number, indent)
	}
	return node
}

// powerPointKeepsInWord reports whether the runs of doc keep a closing
// bracket, a solidus, a question or an exclamation mark or an ellipsis
// with the character after it ([css.Office.KeepInWord]): under PowerPoint's rules,
// which break a line inside a word only after a hyphen, or where the line
// ends when no other opportunity is left; Impress breaks where UAX #14
// does.
//
// # Corpus evidence
//
//   - openxmlsdk/pptx: v2FxTestFiles/presentation's
//     AutoShapes_3D_Bevel_Circle_Shadow_Offset_Diagonal_ and
//     AutoShapes_3D_Bevel_Riblet_Reflection_full_ print their titles
//     "(Circle)+Shadow(Outer_Diagonal" and "(3D_Bevel(Riblet)+Reflection(full,8"
//     whole on their lines, moved down a line where they do not fit,
//     though UAX #14 breaks between ")" and "+".
//   - probe, pptx: in 70-point Calibri 18 boxes, "aaaa/bbbb/cccc" prints
//     "aaaa/bbb" and "b/cccc", "aaaa?bbbb!cccc" "aaaa?bb" and "bb!cccc",
//     "aaaa(bb)(cccc" "aaaa(bb)(" and "cccc", each broken where the line
//     ends, and "aaaa-bbbb-cccc" breaks after each hyphen.
//   - probe, pptx: in the same boxes "aaaa}bbbb}cccc" prints "aaaa}bbb"
//     and "b}cccc", "aaaa…bbbb…cc" "aaaa…bb" and "bb…cc", where
//     "aaaa—bbbb—cc" breaks after the first em dash; ";", ":", ">", "&",
//     "%" between digits and straight quotes break only where the line
//     ends, as UAX #14 has them.
func powerPointKeepsInWord(doc *Document) bool {
	return doc != nil && doc.Fallback != text.FallbackLibreOffice
}

// powerPointClusterAlone reports whether the runs of doc set a character
// too wide for its line alone on the line ([css.Office.ClusterAlone]):
// under PowerPoint's rules, which break a line between any two characters
// when not even one fits; Impress lets the word overflow.
//
// # Corpus evidence
//
//   - openxmlsdk/pptx: O14ISOStrict/PowerPoint's
//     marR51206400_mainLvl2ppr_MarginRightLevel2Maximum, whose second
//     paragraph's right margin leaves its lines no room, prints that
//     paragraph one character a line, each space after its character.
func powerPointClusterAlone(doc *Document) bool {
	return doc != nil && doc.Fallback != text.FallbackLibreOffice
}

// powerPointFloorsAdvances reports whether the runs of doc floor at zero
// each glyph's advance once their letter spacing is added
// ([css.Office.NonNegativeAdvance]): under PowerPoint's rules, whose
// negative spacing (ECMA-376 Part 1, 21.1.2.3.9 rPr spc) never sets a
// glyph left of the one before it; Impress moves it back by the spacing.
//
// # Corpus evidence
//
//   - probe, pptx: "ABCDE iiii WWWW" in Calibri 18 at spc -2, -5, -10 and
//     -20 points prints each glyph's advance plus the spacing, at least 0,
//     on PowerPoint's 0.72-point grid: at -5 the spaces and each "i" take
//     no room, at -10 every glyph but W, at -20 every glyph.
//   - openxmlsdk/pptx: O14ISOStrict/PowerPoint/spc-400000_rPr_CharacterSpacingMinimum
//     prints its run at -4000 points as glyphs set on one another.
func powerPointFloorsAdvances(doc *Document) bool {
	return doc != nil && doc.Fallback != text.FallbackLibreOffice
}

// powerPointZigzagWave reports whether the runs of doc draw a wavy
// underline (ECMA-376 Part 1, 20.1.10.82 ST_TextUnderlineType wavy) as a
// filled zigzag strip ([css.Office.ZigzagWave]): under PowerPoint's
// rules; Impress strokes a wave.
//
// # Corpus evidence
//
//   - probe, pptx: Calibri 12, 18, 36 and 54 point runs underlined wavy
//     print a strip of 0.72, 1.44, 2.16 and 3.6 points zigzagging over a
//     band of twice that, 0.67, 1.32, 2.66 and 3.2 points below the
//     baseline.
func powerPointZigzagWave(doc *Document) bool {
	return doc != nil && doc.Fallback != text.FallbackLibreOffice
}

// powerPointLinesOutside reports whether the lines of doc that follow a
// text warp's path stand on its outer side, each on its own curve
// ([officefx.Warp.LinesOutside]): under PowerPoint's rules; Impress sets
// every line on the path's points.
//
// # Corpus evidence
//
//   - libreoffice/pptx: oox/tdf125582_TextOnCircle.pptx prints its two
//     lines on textCircle with the second on the circle and the first
//     43.6 points outside it, its glyphs 0.91 to 0.94 of their advances
//     apart on both baselines; sd/tdf125573_FontWorkScaleX.pptx prints
//     its first line on textArchDown and its second below it, the glyphs
//     of each about their advances apart.
func powerPointLinesOutside(doc *Document) bool {
	return doc != nil && doc.Fallback != text.FallbackLibreOffice
}

// pctLine returns the length a percentage spacing before or after a
// paragraph whose largest size is size takes its share of: a single line
// of that size, 1.2 times it, where ECMA-376 Part 1, 21.1.2.2.11 spcPct
// names the text size, as PowerPoint spaces a PresentationML paragraph.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/n778859.pptx's 18-point paragraphs with a
//     spacing before of 20% print 25.95 points apart, 21.6 plus 20% of it.
//   - openxmlsdk/pptx: v2FxTestFiles/presentation/Text_200chars.pptx's
//     32-point paragraphs with a spacing before of 20% print 7.9 points
//     more apart than their lines.
func pctLine(size float64) float64 {
	return float64(size * fixedLineFactor)
}

// maxTabStops caps a paragraph's tab stops. Anti-DoS guard, not
// spec-mandated: DrawingML stops at 32 (21.1.2.2.13 tabLst).
const maxTabStops = 64

// sortStops sorts tab stops by position, insertion sort over a short
// list.
func sortStops(s []css.TabStop) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Pos < s[j-1].Pos; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// hasText reports whether runs hold a character to draw: Impress draws
// no bullet on an empty paragraph.
func hasText(runs []Run) bool {
	for _, r := range runs {
		if !r.Break && (r.Text != "" || r.Field != FieldNone) {
			return true
		}
	}
	return false
}

// fieldText returns what the run r shows: its field's value on the slide
// being drawn, else its text (ECMA-376 Part 1, 21.1.2.3.6 fld; ODF 1.3
// Part 3, 7.3).
func (b *builder) fieldText(r *Run) string {
	switch r.Field {
	case FieldSlideNumber:
		return strconv.Itoa(b.number)
	case FieldSlideCount:
		return strconv.Itoa(b.count)
	case FieldDate, FieldTime:
		if !b.opts.PrintDate.IsZero() {
			return b.dateText(r)
		}
	case FieldSlideName:
		if b.slide != nil {
			return b.slide.Name
		}
		return ""
	case FieldTitle:
		return b.doc.Meta.Title
	case FieldAuthor:
		return b.doc.Meta.Author
	case FieldFile:
		return ""
	case FieldNone:
	}
	return r.Text
}

// The number formats a date and a time field show without one of their
// own, as Impress prints them: the month, day and year of the date, the
// hours, minutes and seconds of the time on a 12-hour clock.
//
// # Corpus evidence
//
//   - probe, odp: a text:date and a text:time without a data style print
//     11/8/26 and 11:05:48 AM.
const (
	defaultDateFormat = "M/D/YY"
	defaultTimeFormat = "HH:MM:SS AM/PM"
)

// dateText returns the print date as the date or time field r shows it,
// through its number format, else the kind's default.
func (b *builder) dateText(r *Run) string {
	code := r.Format
	if code == "" {
		code = defaultDateFormat
		if r.Field == FieldTime {
			code = defaultTimeFormat
		}
	}
	f, err := numfmt.Parse(code)
	if err != nil {
		b.warn(warn.ValueFormatUnsupported, warn.Approximated, "the field format "+quote(code)+" is not understood; the default one shows")
		if r.Field == FieldTime {
			f, err = numfmt.Parse(defaultTimeFormat)
		} else {
			f, err = numfmt.Parse(defaultDateFormat)
		}
		if err != nil {
			return r.Text
		}
	}
	// The serial number of the print date's wall clock, days since the
	// spreadsheet epoch, 30 December 1899.
	d := b.opts.PrintDate
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	serial := math.Floor(day.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours()/24+0.5) +
		float64(d.Hour()*3600+d.Minute()*60+d.Second())/86400
	return f.Apply(numfmt.Value{Kind: numfmt.Number, Number: serial}).Text
}

// quote returns s quoted for a warning, shortened.
func quote(s string) string {
	if utf8.RuneCountInString(s) > 64 {
		s = string([]rune(s)[:64]) + "…"
	}
	return strconv.Quote(s)
}

// groupFits returns the scaling of each shrink group of the shapes, the
// smallest any AutofitShrink body of the group needs in its own text
// rectangle, as Impress fits the text of a SmartArt diagram with one
// scale (TextBody.ShrinkGroup).
func (b *builder) groupFits(shapes []*Shape) map[int]fit {
	var out map[int]fit
	var walk func(shapes []*Shape, depth int)
	walk = func(shapes []*Shape, depth int) {
		for _, s := range shapes {
			if s == nil || !finite(s.Box) || depth > maxGroupDepth || b.ctx.Err() != nil {
				continue
			}
			if s.Kind == KindGroup {
				child := s.Child
				if !usable(child) {
					child = s.Box
				}
				sx, sy := s.Box.Width/child.Width, s.Box.Height/child.Height
				kids := make([]*Shape, 0, len(s.Kids))
				for _, k := range s.Kids {
					if k == nil {
						continue
					}
					kk := *k
					kk.Box = kidBox(k, child, s.Box, sx, sy)
					kids = append(kids, &kk)
				}
				walk(kids, depth+1)
				continue
			}
			t := s.Text
			if s.Kind != KindGeometry || t == nil || t.ShrinkGroup == 0 || t.Autofit != AutofitShrink || len(t.Paragraphs) == 0 {
				continue
			}
			inner := insetRect(shape.TextRect(s.Geometry, s.Box), t.Insets)
			w, h := inner.Width, inner.Height
			if t.Vertical.Vertical() {
				w, h = h, w
			}
			f := b.fitBody(t, w, h)
			if out == nil {
				out = map[int]fit{}
			}
			if g, ok := out[t.ShrinkGroup]; !ok || f.font < g.font || (f.font == g.font && f.spacing < g.spacing) {
				out[t.ShrinkGroup] = f
			}
		}
	}
	walk(shapes, 0)
	return out
}

// formulaBoxes returns the formulas of runs laid out at the font scale
// k, by run, nil for a run that draws no formula; nil when no run does.
// A formula that does not lay out is drawn as its text.
func (b *builder) formulaBoxes(runs []Run, k float64) []*formula.Box {
	var out []*formula.Box
	for i := range runs {
		r := &runs[i]
		if r.Formula == nil {
			continue
		}
		if b.formulas == nil {
			b.formulas = formula.NewSetter(b.fonts, b.opts.Logger)
		}
		st := r.Formula.Style()
		st.Size = scaledSize(st.Size, k)
		st.Lang = b.textStyle(&r.Style, k).Lang
		box, err := r.Formula.Layout(b.ctx, b.formulas, st)
		if err != nil {
			if b.ctx.Err() == nil {
				b.warn(warn.ContentUnsupported, warn.Approximated, "an equation is drawn as its plain text: "+err.Error())
			}
			continue
		}
		for _, w := range box.Warnings {
			b.warn(w.Code, w.Effect, w.Message)
		}
		if out == nil {
			out = make([]*formula.Box, len(runs))
		}
		out[i] = box
	}
	return out
}

// formulaNode returns the replaced box of the formula run r, styled is,
// laid out as box: on its line by its own baseline, a Formula structure
// element whose alternate description is the formula's (ISO 32000-1,
// 14.8.4.5).
func formulaNode(r *Run, is *css.Style, box *formula.Box) *layout.Node {
	node := &layout.Node{Kind: layout.Replaced, Style: is, Lang: r.Style.Lang, Role: "Formula", Alt: r.Formula.AltText()}
	if box.Width > 0 && box.Ascent+box.Descent > 0 {
		node.Picture = &layout.Picture{
			Width: box.Width, Height: box.Ascent + box.Descent, NoRatio: true,
			HasBaseline: true, Baseline: box.Ascent,
			Vector: func(_ context.Context, rect typeset.Rect) []typeset.Fragment {
				return box.Draw(rect.X, float64(rect.Y+box.Ascent))
			},
		}
	}
	return node
}
