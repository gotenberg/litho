package slides

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/text"
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
//   - pptx: sd/bnc862510_7.pptx (Segoe UI) prints in Noto Sans; odp: frames
//     in NoSuchFont, Segoe UI and Tahoma print in Noto Sans.
const fallbackFamily = "Noto Sans"

// defaultTab is the distance between the default tab stops of a
// PresentationML text, in points: one inch, what PowerPoint's masters state
// (ECMA-376 Part 1, 21.1.2.2.7 defTabSz). odfDefaultTab is an OpenDocument
// text's, half an inch, Impress's default.
//
// # Corpus evidence
//
//   - odp: a frame of Liberation Sans 20 with three tabs and no tab stop
//     distance prints its digits 36 points apart.
const (
	defaultTab    = 72
	odfDefaultTab = 36
)

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

// scriptShift returns how far a superscript or a subscript of a text of
// size moves in faces of metrics m, as Impress moves an automatic one
// (SvxFont::SetNonAutoEscapement): the share of the face's height above
// its baseline, or below for a subscript, of the 42% the script's size
// gives up, in whole percents of the size.
//
// # Corpus evidence
//
//   - odp: Liberation Sans 20 with text-position super 58% prints 6.8 points
//     up, sub 58% 1.4 points down, 33% 58% 6.6 points up.
func scriptShift(size float64, m text.Metrics, sub bool) float64 {
	h := m.Ascent + m.Descent
	if !(h > 0) {
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
	box compose.Rect
	// frags, links and structure are the laid-out text, in slide
	// coordinates before the shape's turn.
	frags     []compose.Fragment
	links     []compose.Link
	structure []*compose.StructElem
}

// laidBody is a text body laid out at the origin: its fragments, links
// and structure, and its height.
type laidBody struct {
	frags     []compose.Fragment
	links     []compose.Link
	structure []*compose.StructElem
	height    float64
}

// insetRect returns r less the insets, indexed by css.Side, at least a
// point wide and tall.
func insetRect(r compose.Rect, in [4]float64) compose.Rect {
	side := func(v float64) float64 {
		if !(v > 0) || math.IsInf(v, 0) {
			return 0
		}
		return min(v, maxCoordinate)
	}
	l, t, rr, bb := side(in[css.Left]), side(in[css.Top]), side(in[css.Right]), side(in[css.Bottom])
	out := compose.Rect{X: r.X + l, Y: r.Y + t, Width: r.Width - l - rr, Height: r.Height - t - bb}
	if out.Width < 1 {
		out.X, out.Width = r.X+r.Width/2-0.5, 1
	}
	if out.Height < 1 {
		out.Y, out.Height = r.Y+r.Height/2-0.5, 1
	}
	return out
}

// shapeText lays the text of body out in the text rectangle of the
// geometry g fitted to box.
func (b *builder) shapeText(body *TextBody, g *shape.Geometry, box compose.Rect) *textResult {
	res := &textResult{box: box}
	inner := insetRect(shape.TextRect(g, box), body.Insets)
	vertical := body.Vertical.Vertical()
	if body.Vertical == css.VerticalLR {
		b.warn(CodeVertical, "vertical text with lines from left to right is laid out as from right to left")
	}
	lw, lh := inner.Width, inner.Height
	if vertical {
		lw, lh = lh, lw
	}
	f := b.fitBody(body, lw, lh)
	dx := 0.0
	if !body.Wrap && !vertical {
		// Unwrapped lines wider than the rectangle widen the text block to
		// the longest, which overflows the rectangle on the side its first
		// paragraph's alignment leaves free: right of a start-aligned text,
		// both sides of a centred one, left of an end-aligned one
		// (SdrObjCustomShape::TakeTextRect).
		if nat := b.naturalWidth(body, f); nat > lw {
			switch overflowSide(body) {
			case css.AlignCenter:
				dx = (lw - nat) / 2
			case css.AlignRight:
				dx = lw - nat
			}
			lw = nat
		}
	}
	lay := b.layoutBody(body, lw, lh, f)
	if lay == nil {
		return res
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
			box.Y -= grow / 2
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
	}
	var m compose.Matrix
	switch body.Vertical {
	case css.HorizontalTB:
		m = translate(inner.X+dx, inner.Y+dy)
	case css.SidewaysLR:
		// Turned a quarter turn counter-clockwise: the lines read bottom
		// to top and stack left to right (21.1.10.83 vert270).
		m = compose.Matrix{A: 0, B: -1, C: 1, D: 0, E: inner.X + dy, F: inner.Y + inner.Height}
	case css.VerticalRL, css.VerticalLR, css.SidewaysRL:
		// Turned a quarter turn clockwise: the lines read top to bottom
		// and stack right to left (vert, eaVert).
		m = compose.Matrix{A: 0, B: 1, C: -1, D: 0, E: inner.X + inner.Width - dy, F: inner.Y}
	default:
		m = translate(inner.X+dx, inner.Y+dy)
	}
	frags := lay.frags
	if body.Clip && !vertical {
		// A clipped body draws no line whose baseline falls outside its
		// rectangle and cuts the others at it, as Impress prints a text
		// whose overflow behaviour is clip (sd/style-overflow-behavior-clip.fodp
		// leaves its second line out).
		frags = keepLines(frags, compose.Identity, -dy, lh-dy)
		r := shape.TextRect(g, box)
		res.frags = []compose.Fragment{&compose.Group{
			Transform: compose.Identity, Opacity: 1,
			Clip:    &compose.Path{Ops: rectOps(r), Fill: &compose.Fill{}},
			Content: group(m, frags),
		}}
	} else {
		res.frags = group(m, frags)
	}
	for _, l := range lay.links {
		l.Rect = transformRect(m, l.Rect)
		res.links = append(res.links, l)
	}
	res.structure = lay.structure
	return res
}

// keepLines returns the fragments of frags, mapped by m, less the text
// runs whose baseline lies above top or below bottom.
func keepLines(frags []compose.Fragment, m compose.Matrix, top, bottom float64) []compose.Fragment {
	out := make([]compose.Fragment, 0, len(frags))
	for _, f := range frags {
		switch v := f.(type) {
		case *compose.Text:
			_, y := apply(m, v.Origin.X, v.Origin.Y)
			if y < top-0.01 || y > bottom+0.01 {
				continue
			}
		case *compose.Group:
			t := v.Transform
			if t == (compose.Matrix{}) {
				t = compose.Identity
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
		if w := sp.MaxContent() + ml; w > best && !math.IsInf(w, 0) {
			best = w
		}
	}
	return min(best, office.MaxPageSide)
}

// overflowSide returns how a body's unwrapped text wider than its
// rectangle sits on it: by its first paragraph's alignment, left,
// centred or right.
func overflowSide(body *TextBody) css.TextAlign {
	if len(body.Paragraphs) == 0 {
		return css.AlignLeft
	}
	p := &body.Paragraphs[0]
	rtl := p.RightToLeft || body.RightToLeft
	switch p.Align {
	case css.AlignCenter:
		return css.AlignCenter
	case css.AlignRight:
		return css.AlignRight
	case css.AlignEnd:
		if !rtl {
			return css.AlignRight
		}
	case css.AlignStart, css.AlignAuto, css.AlignJustify:
		if rtl {
			return css.AlignRight
		}
	}
	return css.AlignLeft
}

// fitBody returns the scaling the body is laid out at in a box w by h: a
// body that shrinks to fit takes the first of Impress's scale levels at
// which its text fits, else the last one, and none when it fits as it is
// (ImpEditEngine::ScaleContentToFitWindow). The scale a file saved is not
// read: Impress computes the fit again whatever the file says.
//
// # Corpus evidence
//
//   - pptx: sd/font-scale.pptx saves a font scale of 85% with 20% less
//     spacing and prints at the fourth level whatever it saves (100%, 50% or
//     none measured): its three empty closing paragraphs would push the
//     fit to 70%.
func (b *builder) fitBody(body *TextBody, w, h float64) fit {
	if body.Autofit != AutofitShrink {
		return noFit
	}
	cols := max(body.Columns, 1)
	colW := w
	if cols > 1 {
		colW = (w - float64(cols-1)*max(body.ColumnGap, 0)) / float64(cols)
		colW = max(colW, 1)
	}
	room := h * float64(cols)
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
	if !(s > 0) {
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
		b.warn(CodeText, fmt.Sprintf("a text body has more than %d paragraphs; the rest are left out", maxParagraphs))
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
	tree := &layout.Node{Kind: layout.Element, Style: root, ID: "slides-body"}
	nums := numbers(paras)
	budget := maxBodyText
	for i := range paras {
		tree.Children = append(tree.Children, b.paragraph(&paras[i], i, len(paras), body, f, nums[i], &budget))
	}
	res, err := layout.Layout(b.ctx, tree, b.fonts, layout.Options{
		Page:     layout.PageBox{Width: min(max(w, 1), office.MaxPageSide), Height: office.MaxPageSide},
		Lang:     b.lang,
		MaxPages: 2,
		Logger:   b.opts.Logger,
	})
	if err != nil {
		if !errors.Is(err, b.ctx.Err()) {
			b.warn(CodeText, "a text body could not be laid out: "+err.Error())
		}
		return nil
	}
	b.warnings = append(b.warnings, res.Warnings...)
	out := &laidBody{}
	if len(res.Pages) > 0 {
		out.frags = res.Pages[0].Content
		out.links = res.Pages[0].Links
	}
	if res.Structure != nil {
		out.structure = res.Structure.Kids
	}
	if e, ok := res.Extents["slides-body"]; ok {
		out.height = e.Height
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
// spacing rules): a natural line is the face's ascent plus descent, or
// 1.2 times the size with the size as ascent when the body spaces lines
// independently of the faces; a proportional spacing above 1 adds its
// room above the glyphs, below 1 it shrinks the line and caps the ascent
// at four fifths of it; an exact spacing changes the ascent by the
// difference.
func (b *builder) lines(p *Paragraph, ts text.Style, f fit, fontLines bool) lineBox {
	s := ts.Size
	a, n := s, s*fixedLineFactor
	if fontLines {
		m := b.faceMetrics(ts)
		a, n = m.Ascent, m.Ascent+m.Descent
	}
	sf := f.spacing
	lb := lineBox{height: n, ascent: a, text: n}
	switch prop := p.LineSpacing; {
	case p.LineSpacingPoints > 0 && !math.IsInf(p.LineSpacingPoints, 0):
		lb.height = min(p.LineSpacingPoints, maxCoordinate) * sf
		lb.ascent = a + lb.height - n
	case !(prop > 0) || prop == 1 || math.IsInf(prop, 0):
		if sf < 1 {
			lb.height = n * sf
			lb.ascent = min(a, n*sf)
		}
	case prop < 1:
		lb.height = n * prop * sf
		lb.ascent = min(a, n*sf*prop*0.8)
	default:
		lb.height = n * min(prop, maxLineSpacing) * sf
		lb.ascent = a - (n - lb.height)
	}
	lb.height = max(lb.height, 0.01)
	return lb
}

// maxLineSpacing caps a proportional line spacing. Anti-DoS guard, not
// spec-mandated: DrawingML allows 132 times single spacing (21.1.10.64
// ST_TextSpacingPercentOrPercentString).
const maxLineSpacing = 132

// faceMetrics returns the metrics of the face ts matches at its size.
func (b *builder) faceMetrics(ts text.Style) text.Metrics {
	k := fmt.Sprintf("%s|%d|%v|%g", strings.Join(ts.Families, ","), ts.Weight, ts.Italic, ts.Size)
	if m, ok := b.metrics[k]; ok {
		return m
	}
	m := text.Metrics{Ascent: ts.Size * 0.9, Descent: ts.Size * 0.2}
	if fc := b.fonts.Match(ts); fc != nil {
		if mm := fc.Metrics(ts.Size); mm.Ascent+mm.Descent > 0 {
			m = mm
		}
	}
	if len(b.metrics) < maxMetrics {
		b.metrics[k] = m
	}
	return m
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

// inline returns the style of a run of style rs at the font scale k,
// inside a paragraph of style ps.
func (b *builder) inline(ps *css.Style, rs *RunStyle, k float64) *css.Style {
	st := css.Initial()
	st.Display = css.Inline
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
	if rs.Underline {
		st.TextDecoration |= css.Underline
	}
	if rs.Strike {
		st.TextDecoration |= css.LineThrough
	}
	if sp := rs.Spacing; sp != 0 && !math.IsNaN(sp) && math.Abs(sp) < maxFontSize {
		st.LetterSpacing = css.Pts(sp * k)
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
	return st
}

// paragraph returns the block of the i-th of n paragraphs of body at the
// scaling f, number its list number, budget the characters left to lay
// out.
func (b *builder) paragraph(p *Paragraph, i, n int, body *TextBody, f fit, number int, budget *int) *layout.Node {
	runs := p.Runs
	if len(runs) > maxRuns {
		b.warn(CodeText, fmt.Sprintf("a paragraph has more than %d runs; the rest are left out", maxRuns))
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
	lb := b.lines(p, bigTS, lf, body.FontLineSpacing)
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
	st.Top = css.Pts(lb.ascent - 0.8*lb.height)
	st.TabSize = css.Pts(defaultTab)
	if body.FontLineSpacing {
		st.TabSize = css.Pts(odfDefaultTab)
	}
	st.Orphans, st.Widows = 1, 1
	st.TextAlign = p.Align
	if p.Align == css.AlignAuto {
		st.TextAlign = css.AlignStart
	}
	sideLen := func(v float64) float64 {
		if math.IsNaN(v) || math.IsInf(v, 0) {
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
		if pct > 0 && !(pts > 0) {
			v = pct * size
		}
		if !(v > 0) || math.IsInf(v, 0) {
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
			if prev.SpaceAfterPct > 0 && !(after > 0) {
				after = prev.SpaceAfterPct * psize
			}
			if after > 0 && !math.IsInf(after, 0) {
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
			b.warn(CodeText, fmt.Sprintf("a text body has more than %d characters; the rest are left out", maxBodyText))
			break
		}
		*budget -= utf8.RuneCountInString(s)
		is := b.inline(st, &r.Style, f.font)
		run := &layout.Node{Kind: layout.Element, Style: is, Lang: r.Style.Lang, Href: r.Href, Children: []*layout.Node{{Kind: layout.Text, Style: is, Text: s}}}
		if r.Href != "" {
			run.Role = "Link"
		}
		node.Children = append(node.Children, run)
	}
	if last := runs[len(runs)-1]; last.Break {
		// A line break that ends the paragraph keeps the empty line after
		// it, as tall as the paragraph's end mark makes it.
		if k := len(node.Children) - 1; k > 0 && hasText(runs) {
			node.Children = node.Children[:k]
			empty := b.lines(p, b.textStyle(&p.Style, f.font), emptyFit, body.FontLineSpacing)
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
//   - pptx: sd/numfmt.pptx's plain datetime field prints 11/8/26 on 8
//     November 2026; odp: a text:date and a text:time without a data
//     style print 11/8/26 and 11:05:48 AM.
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
		b.warn(CodeText, "the field format "+quote(code)+" is not understood; the default one shows")
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
