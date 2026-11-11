package text

import (
	"context"
	"fmt"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	gofont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// objectReplacement is the character an [Object] run stands for in a
// [Paragraph]'s text: U+FFFC OBJECT REPLACEMENT CHARACTER, which UAX #14
// classes CB (a break opportunity before and after, LB20) and UAX #9 classes
// as a neutral.
const objectReplacement = '￼'

// wordJoiner replaces a control character for segmentation and shaping: it
// is default ignorable, so it shapes to nothing, and prohibits a break on
// either side (UAX #14, class WJ).
const wordJoiner = '⁠'

// Direction is a paragraph or override direction (UAX #9).
type Direction uint8

// The directions.
const (
	// DirectionAuto takes a paragraph's direction from its first strong
	// character (UAX #9, P2 and P3), left to right without one. As a run
	// override it means none.
	DirectionAuto Direction = iota
	// LeftToRight is the left-to-right direction.
	LeftToRight
	// RightToLeft is the right-to-left direction.
	RightToLeft
)

// Object is an atomic inline box a [Paragraph] carries through line
// breaking: an opaque piece of the given advance and vertical extent, such
// as an image or an inline block.
type Object struct {
	// Width is the advance in points, at least 0.
	Width float64
	// Ascent and Descent are the extent above and below the baseline, in
	// points.
	Ascent, Descent float64
	// Tab makes the object a tab for the embedding levels: a segment
	// separator, which takes the paragraph's level (UAX #9, L1), so that
	// the text on either side of it orders as separate segments.
	Tab bool
}

// Break is the kind of line break opportunity before a character.
type Break uint8

// The break kinds (UAX #14).
const (
	// NoBreak prohibits a break.
	NoBreak Break = iota
	// BreakAllowed is a soft wrap opportunity.
	BreakAllowed
	// BreakMandatory is a forced break, after a line separator.
	BreakMandatory
)

// ParagraphRun is one piece of a [Paragraph] in logical order: text in one
// style, or an atomic object.
type ParagraphRun struct {
	// Text is UTF-8; invalid bytes read as U+FFFD. It is shaped as given:
	// the caller has already processed white space. A control character
	// keeps its place in the paragraph's text and draws nothing.
	Text  string
	Style Style
	// Object, when set, makes the run one U+FFFC character of the object's
	// advance; Text and Style are then ignored.
	Object *Object
	// Override forces the direction of the run's characters, as CSS
	// unicode-bidi: bidi-override does (UAX #9, explicit overrides); the
	// zero value overrides nothing.
	Override Direction
	// Open lists the explicit directional formatting characters of UAX
	// #9 that open before the run (LRE, RLE, LRO, RLO, LRI, RLI or FSI),
	// outermost first, and Close the ones that close after it (PDF or
	// PDI), innermost first: the controls CSS Writing Modes 4, 2.4.2 maps
	// unicode-bidi onto. They take part in resolving the embedding levels
	// alone: they are not characters of the paragraph's text. Other
	// characters are ignored.
	Open, Close []rune
}

// ParagraphOptions tunes a [Fonts.Shape] call.
type ParagraphOptions struct {
	// Direction is the paragraph's base direction; [DirectionAuto], the
	// zero value, takes it from the text.
	Direction Direction
}

// Span is one piece of a [ParagraphLine] in visual order: the glyphs of one
// run in one direction, or one object.
type Span struct {
	// Run is the index of the [ParagraphRun] the piece comes from.
	Run int
	// Start and End are the piece's characters in the paragraph's text,
	// End exclusive.
	Start, End int
	// Text is the piece's glyph run, its Origin.X the piece's offset from
	// the line start and Origin.Y 0; nil for an object or a piece that draws
	// no glyph.
	Text *compose.Text
	// X is the offset of the piece from the line start, Width its advance,
	// both in points.
	X, Width float64
	// Ascent and Descent are the piece's extent above and below the
	// baseline: the face's at its size, or the object's.
	Ascent, Descent float64
	// LineGap is the face's line gap at its size, 0 for an object.
	LineGap float64
	// UnderlinePosition is the distance from the baseline down to the top
	// of the underline and UnderlineThickness its thickness, from the
	// face's post table; StrikeoutPosition is the distance from the
	// baseline up to the top of the strikethrough and StrikeoutThickness
	// its thickness, from its OS/2 table. A thickness of 0 means the face
	// records none.
	UnderlinePosition, UnderlineThickness float64
	StrikeoutPosition, StrikeoutThickness float64
	// RTL reports a piece drawn right to left.
	RTL bool
}

// ParagraphLine is one line of a [Paragraph].
type ParagraphLine struct {
	// Spans are the line's pieces in visual order, left to right.
	Spans []Span
	// Width is the advance of the line without the spaces that hang at
	// its end.
	Width float64
}

// Paragraph is text shaped once, ready to be broken into lines by a caller
// that decides the breaks itself.
//
// A Paragraph is not safe for concurrent use.
type Paragraph struct {
	text  []rune
	runOf []int
	adv   []float64
	brk   []Break
	graph []bool
	// object holds the object of each object character, nil elsewhere.
	object []*Object
	base   Direction
	items  []shapedItem
	// level is each character's resolved embedding level (UAX #9, X1 to
	// I2).
	level []int8
	// mem is the conversion's memory budget the glyph runs of its lines
	// are charged to, nil for none.
	mem *limits.Budget
}

// shapedItem is one shaped piece of a paragraph, in logical order: a run of
// one style, face and direction, or one object.
type shapedItem struct {
	start, end int
	run        int
	level      int
	rtl        bool
	face       *compose.Face
	style      Style
	glyphs     []shapedGlyph
	// ascent, descent and gap are the face's line bounds at the size;
	// underline and strike its decoration metrics.
	ascent, descent, gap   float64
	ulPos, ulThick         float64
	strikePos, strikeThick float64
	// stroke and skew synthesise the bold and italic the face lacks.
	stroke, skew float64
	object       bool
}

// shapedGlyph is one glyph of a [shapedItem], in points.
type shapedGlyph struct {
	gid          uint16
	xoff, yoff   float64
	advance      float64
	cluster, len int
}

// Shape shapes runs into one bidirectional paragraph for a caller that
// breaks lines itself: the HTML layout engine, which needs per-line widths,
// atomic inline boxes and its own white space rules.
//
// Shape resolves the paragraph's base direction from opts, itemizes the
// text by direction, script, style and face, shapes each item with HarfBuzz
// with the face fallback [Fonts.Layout] documents, applies word spacing to
// the word separators and letter spacing after every character cluster (CSS
// Text 3, 8.2), and records the UAX #14 line break opportunities and the
// UAX #29 grapheme boundaries of the text.
//
// It returns [ErrInvalidStyle], [ErrTooManyRuns], [ErrTextTooLong] and
// [ErrNoFonts] as [Fonts.Layout] does, an error for an object whose width
// or extent is not finite or negative width, and ctx's error.
func (f *Fonts) Shape(ctx context.Context, runs []ParagraphRun, opts ParagraphOptions) (*Paragraph, Report, error) {
	if len(runs) > MaxLayoutRuns {
		return nil, Report{}, fmt.Errorf("%w: %d runs, at most %d", ErrTooManyRuns, len(runs), MaxLayoutRuns)
	}
	total := 0
	fallback := -1
	for i, r := range runs {
		if r.Object != nil {
			o := r.Object
			if !finiteWithin(o.Width, maxLineWidth) || o.Width < 0 || !finiteWithin(o.Ascent, maxLineWidth) || !finiteWithin(o.Descent, maxLineWidth) {
				return nil, Report{}, fmt.Errorf("%w: run %d: object %+v", ErrInvalidStyle, i, *o)
			}
			total++
		} else {
			total += utf8.RuneCountInString(r.Text)
			if err := r.Style.validate(); err != nil {
				return nil, Report{}, fmt.Errorf("run %d: %w", i, err)
			}
			if fallback < 0 {
				fallback = i
			}
		}
		if total > MaxLayoutRunes {
			return nil, Report{}, fmt.Errorf("%w: more than %d characters", ErrTextTooLong, MaxLayoutRunes)
		}
	}
	if len(f.faces) == 0 {
		return nil, Report{}, ErrNoFonts
	}
	if err := ctx.Err(); err != nil {
		return nil, Report{}, fmt.Errorf("shaping text: %w", err)
	}
	// Object runs take a text run's style, so matching never sees a zero
	// style.
	styled := make([]Run, len(runs))
	for i, r := range runs {
		styled[i] = Run{Text: r.Text, Style: r.Style}
		if r.Object != nil {
			styled[i] = Run{Style: Style{Size: 10}}
			if fallback >= 0 {
				styled[i].Style = runs[fallback].Style
			}
		}
	}
	st, err := newLayoutState(f, styled)
	if err != nil {
		return nil, Report{}, err
	}
	defer st.release()
	p := &Paragraph{mem: limits.BudgetFrom(ctx)}
	var shapeText []rune
	for i, r := range runs {
		if r.Object != nil {
			p.text = append(p.text, objectReplacement)
			shapeText = append(shapeText, objectReplacement)
			p.runOf = append(p.runOf, i)
			p.object = append(p.object, r.Object)
			continue
		}
		for _, c := range r.Text {
			p.text = append(p.text, c)
			if unicode.IsControl(c) {
				c = wordJoiner
			}
			shapeText = append(shapeText, c)
			p.runOf = append(p.runOf, i)
			p.object = append(p.object, nil)
		}
	}
	n := len(p.text)
	p.adv = make([]float64, n)
	p.brk = make([]Break, n)
	p.graph = make([]bool, n)
	bidiText, at := bidiInput(runs, shapeText, p.runOf)
	p.base = opts.Direction
	if p.base != LeftToRight && p.base != RightToLeft {
		p.base = LeftToRight
		if paragraphDirection(bidiText) == di.DirectionRTL {
			p.base = RightToLeft
		}
	}
	if n == 0 {
		return p, st.report(), nil
	}
	p.level = resolveLevels(bidiText, at, p.base)
	p.segment(st, shapeText)
	if err := p.shape(ctx, st, shapeText); err != nil {
		return nil, Report{}, err
	}
	p.trimAdjacent()
	p.autospace()
	return p, st.report(), nil
}

// segment records the break opportunities and grapheme boundaries of text.
func (p *Paragraph) segment(st *layoutState, text []rune) {
	st.lineSeg.Init(text)
	it := st.lineSeg.LineIterator()
	for it.Next() {
		line := it.Line()
		if line.Offset > 0 && line.Offset < len(text) && p.brk[line.Offset] == NoBreak {
			p.brk[line.Offset] = BreakAllowed
		}
		if end := line.Offset + len(line.Text); line.IsMandatoryBreak && end < len(text) {
			p.brk[end] = BreakMandatory
		}
	}
	gi := st.lineSeg.GraphemeIterator()
	for gi.Next() {
		if g := gi.Grapheme(); g.Offset < len(text) {
			p.graph[g.Offset] = true
		}
	}
}

// shape shapes the text between objects, item by item.
func (p *Paragraph) shape(ctx context.Context, st *layoutState, text []rune) error {
	baseDir := di.DirectionLTR
	if p.base == RightToLeft {
		baseDir = di.DirectionRTL
	}
	for start := 0; start < len(text); {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("shaping text: %w", err)
		}
		if o := p.object[start]; o != nil {
			lv := int(p.level[start])
			p.items = append(p.items, shapedItem{
				start: start, end: start + 1, run: p.runOf[start], level: lv, rtl: lv%2 == 1,
				ascent: o.Ascent, descent: o.Descent, object: true,
			})
			p.adv[start] = o.Width
			start++
			continue
		}
		end := start + 1
		for end < len(text) && p.object[end] == nil {
			end++
		}
		if err := p.shapeSegment(st, text, start, end, baseDir); err != nil {
			return err
		}
		start = end
	}
	return nil
}

// shapeSegment shapes text[start:end], which holds no object.
func (p *Paragraph) shapeSegment(st *layoutState, text []rune, start, end int, baseDir di.Direction) error {
	first := st.styles[p.runOf[start]]
	items := slices.Clone(st.seg.Split(shaping.Input{
		Text:      text,
		RunStart:  start,
		RunEnd:    end,
		Direction: baseDir,
		Face:      st.face(first.primary),
		Size:      first.size,
		Language:  first.lang,
	}, fixedFontmap{face: st.face(first.primary)}))
	for _, item := range items {
		for s := item.RunStart; s < item.RunEnd; {
			e := s + 1
			for e < item.RunEnd && p.runOf[e] == p.runOf[s] && p.level[e] == p.level[s] {
				e++
			}
			idx := p.runOf[s]
			ss := st.styles[idx]
			piece := item
			piece.RunStart, piece.RunEnd = s, e
			piece.Size = ss.size
			if ss.style.Lang != "" {
				piece.Language = ss.lang
			}
			piece.FontFeatures = ss.style.features()
			// The resolved level, explicit embeddings and overrides
			// included, decides the direction (UAX #9, L2).
			piece.Direction = di.DirectionLTR
			if p.level[s]%2 == 1 {
				piece.Direction = di.DirectionRTL
			}
			for _, in := range shaping.SplitByFace(piece, styleFontmap{st: st, style: idx, script: piece.Script}) {
				out := st.kit.shaper.Shape(in)
				rescale(&out, in.Size)
				st.collectMissing(&out, text)
				if ss.word != 0 {
					out.AddWordSpacing(text, ss.word)
				}
				it, err := p.convert(st, &out, idx, ss.style)
				if err != nil {
					return err
				}
				p.items = append(p.items, it)
			}
			s = e
		}
	}
	return nil
}

// convert turns one shaped output into a [shapedItem] in points, adding the
// letter spacing after each cluster and recording every cluster's advance
// on its first character.
func (p *Paragraph) convert(st *layoutState, out *shaping.Output, run int, style Style) (shapedItem, error) {
	it := shapedItem{
		start: out.Runes.Offset, end: out.Runes.Offset + out.Runes.Count, run: run, style: style,
		rtl:     out.Direction.Progression() == di.TowardTopLeft,
		ascent:  st.toPoints(out.LineBounds.Ascent),
		descent: -st.toPoints(out.LineBounds.Descent),
		gap:     st.toPoints(out.LineBounds.Gap),
	}
	if it.start >= 0 && it.start < len(p.level) {
		it.level = int(p.level[it.start])
	}
	if fc := st.byFont[out.Face.Font]; fc != nil {
		cf, err := fc.Compose()
		if err != nil {
			return shapedItem{}, fmt.Errorf("shaping text: %w", err)
		}
		it.face = cf
		it.stroke, it.skew = synthesis(requestOf(style), fc, style.Size)
	}
	if upem := float64(out.Face.Upem()); upem > 0 {
		k := style.Size / upem
		it.ulPos = -float64(out.Face.LineMetric(gofont.UnderlinePosition)) * k
		it.ulThick = float64(out.Face.LineMetric(gofont.UnderlineThickness)) * k
		it.strikePos = float64(out.Face.LineMetric(gofont.StrikethroughPosition)) * k
		it.strikeThick = float64(out.Face.LineMetric(gofont.StrikethroughThickness)) * k
		for _, v := range []*float64{&it.ulPos, &it.ulThick, &it.strikePos, &it.strikeThick} {
			if !finiteWithin(*v, maxLineWidth) {
				*v = 0
			}
		}
	}
	it.glyphs = make([]shapedGlyph, len(out.Glyphs))
	n := len(p.text)
	for k, g := range out.Glyphs {
		start, end := clusterRange(g, n)
		gid := glyphID(g.GlyphID)
		if gid == 0 {
			gid = blankEmoji(out.Face, p.text[start:end])
		}
		it.glyphs[k] = shapedGlyph{
			gid:     gid,
			xoff:    st.toPoints(g.XOffset) * style.scale(),
			yoff:    st.toPoints(-g.YOffset),
			advance: st.toPoints(g.Advance) * style.scale(),
			cluster: start,
			len:     end - start,
		}
	}
	if style.LetterSpacing != 0 {
		// CSS Text 3, 8.2: the spacing goes after every typographic
		// character unit, which is the visually last glyph of a cluster in
		// a left-to-right run and the visually first in a right-to-left one.
		for k := range it.glyphs {
			last := k == len(it.glyphs)-1 || it.glyphs[k+1].cluster != it.glyphs[k].cluster
			if it.rtl {
				last = k == 0 || it.glyphs[k-1].cluster != it.glyphs[k].cluster
			}
			// A default ignorable character, a format control such as a
			// zero width space or a bidi control, is no typographic
			// character unit: it takes no spacing.
			if c := it.glyphs[k].cluster; last && c < n && !unicode.Is(unicode.Cf, p.text[c]) {
				it.glyphs[k].advance += style.LetterSpacing
			}
		}
	}
	for _, g := range it.glyphs {
		if g.cluster < n {
			p.adv[g.cluster] += g.advance
		}
	}
	return it, nil
}

// Len returns the number of characters of the paragraph's text.
func (p *Paragraph) Len() int { return len(p.text) }

// Rune returns the character at i, which lies in [0, Len()).
func (p *Paragraph) Rune(i int) rune { return p.text[i] }

// RunOf returns the index of the run the character at i comes from.
func (p *Paragraph) RunOf(i int) int { return p.runOf[i] }

// Advance returns the advance the character at i contributes, in points: a
// cluster's advance sits on its first character, 0 on the others.
func (p *Paragraph) Advance(i int) float64 { return p.adv[i] }

// SetAdvance sets the advance of the object character at i, for an object
// whose width depends on where it lands, a tab. It does nothing on a
// character that is not an object or for a width that is not finite and
// positive.
func (p *Paragraph) SetAdvance(i int, w float64) {
	if i < 0 || i >= len(p.text) || p.object[i] == nil || !finiteWithin(w, maxLineWidth) || w < 0 {
		return
	}
	p.adv[i] = w
}

// Break returns the line break opportunity before the character at i,
// always [NoBreak] at 0.
func (p *Paragraph) Break(i int) Break { return p.brk[i] }

// GraphemeStart reports whether a grapheme cluster starts at i (UAX #29).
func (p *Paragraph) GraphemeStart(i int) bool { return p.graph[i] }

// Direction returns the paragraph's resolved base direction, [LeftToRight]
// or [RightToLeft].
func (p *Paragraph) Direction() Direction { return p.base }

// IsHangingSpace reports whether r hangs at the end of a line: a space
// separator other than the no-break spaces (CSS Text 3, 4.1.3).
func IsHangingSpace(r rune) bool {
	return allHanging([]rune{r})
}

// MaxContent returns the paragraph's width laid out on one line, without
// the spaces that would hang at its end: its max-content inline size (CSS
// Sizing 3, 5.1) when nothing forces a break.
func (p *Paragraph) MaxContent() float64 {
	best, cur := 0.0, 0.0
	end := 0
	for i := range p.text {
		if p.brk[i] == BreakMandatory {
			best = max(best, p.width(end, i))
			end, cur = i, 0
		}
		cur += p.adv[i]
	}
	return max(best, p.width(end, len(p.text)))
}

// MinContent returns the widest piece of text between two break
// opportunities, without the spaces that hang at its end: its min-content
// inline size (CSS Sizing 3, 5.1).
func (p *Paragraph) MinContent() float64 {
	best := 0.0
	start := 0
	for i := 1; i <= len(p.text); i++ {
		if i == len(p.text) || p.brk[i] != NoBreak {
			best = max(best, p.width(start, i))
			start = i
		}
	}
	return best
}

// width returns the advance of text[start:end] without its trailing
// hanging spaces.
func (p *Paragraph) width(start, end int) float64 {
	for end > start && IsHangingSpace(p.text[end-1]) {
		end--
	}
	w := 0.0
	for i := start; i < end; i++ {
		w += p.adv[i]
	}
	return w
}

// Line lays the characters [start, end) out as one line: it cuts the shaped
// items at the range, orders them visually (UAX #9, L2, over the levels of
// the items), and positions their glyphs. When hang is set, the spaces at
// the logical end of the range hang: they draw no advance (CSS Text 3,
// 4.1.3). A range outside the text is clamped to it.
func (p *Paragraph) Line(start, end int, hang bool) ParagraphLine {
	start = min(max(start, 0), len(p.text))
	end = min(max(end, start), len(p.text))
	hangFrom := end
	if hang {
		for hangFrom > start && IsHangingSpace(p.text[hangFrom-1]) {
			hangFrom--
		}
	}
	return p.LineTrimmed(start, end, hangFrom)
}

// LineTrimmed is [Paragraph.Line] with the characters from trim to end
// drawing no advance, whatever they are: a caller that tells the spaces
// it removes at a line end (CSS Text 3, 4.1.2) from the ones that hang
// and still draw (4.1.3) passes the first removed one. ParagraphLine.Width
// then counts the hanging ones.
func (p *Paragraph) LineTrimmed(start, end, trim int) ParagraphLine {
	start = min(max(start, 0), len(p.text))
	end = min(max(end, start), len(p.text))
	hangFrom := min(max(trim, start), end)
	type piece struct {
		item  *shapedItem
		start int
		end   int
	}
	var pieces []piece
	for k := range p.items {
		it := &p.items[k]
		s, e := max(it.start, start), min(it.end, end)
		if s < e {
			pieces = append(pieces, piece{item: it, start: s, end: e})
		}
	}
	order := visualOrder(len(pieces), func(i int) int { return pieces[i].item.level })
	var line ParagraphLine
	x := 0.0
	for _, i := range order {
		pc := pieces[i]
		it := pc.item
		sp := Span{
			Run: it.run, Start: pc.start, End: pc.end, X: x,
			Ascent: it.ascent, Descent: it.descent, LineGap: it.gap,
			UnderlinePosition: it.ulPos, UnderlineThickness: it.ulThick,
			StrikeoutPosition: it.strikePos, StrikeoutThickness: it.strikeThick,
			RTL: it.rtl,
		}
		if it.object {
			if pc.start < hangFrom {
				sp.Width = p.adv[pc.start]
			}
		} else {
			sp.Width = p.glyphRun(&sp, it, pc.start, pc.end, hangFrom)
		}
		line.Spans = append(line.Spans, sp)
		x += sp.Width
	}
	line.Width = x
	return line
}

// glyphRun fills sp.Text with the glyphs of it whose cluster lies in
// [start, end), the ones from hangFrom on drawing no advance, and returns
// their advance.
func (p *Paragraph) glyphRun(sp *Span, it *shapedItem, start, end, hangFrom int) float64 {
	t := &compose.Text{Face: it.face, Size: it.style.Size, Color: it.style.Color, Origin: compose.Point{X: sp.X}, Stroke: it.stroke, Skew: it.skew}
	hs := it.style.scale()
	if hs != 1 {
		t.HScale = hs
	}
	pen := 0.0
	cluster := -1
	for _, g := range it.glyphs {
		if g.cluster < start || g.cluster >= end {
			continue
		}
		adv := g.advance
		if g.cluster >= hangFrom {
			adv = 0
		}
		if c := p.text[min(g.cluster, len(p.text)-1)]; unicode.IsControl(c) {
			pen += adv
			continue
		}
		// The positions are written unscaled: the run's HScale scales
		// them back (ISO 32000-1, 9.3.4).
		cg := compose.Glyph{GID: g.gid, X: (pen + g.xoff) / hs, Y: g.yoff}
		if g.cluster != cluster {
			cluster = g.cluster
			e := min(g.cluster+max(g.len, 1), len(p.text))
			cg.Text = clusterText(p.text[g.cluster:e])
		}
		t.Glyphs = append(t.Glyphs, cg)
		pen += adv
	}
	if len(t.Glyphs) > 0 && t.Face != nil {
		// A refusal sticks and cancels the conversion, which stops at its
		// next checkpoint: a line has no error to return.
		p.mem.Take(int64(len(t.Glyphs)), glyphCost, "glyph runs")
		sp.Text = t
	}
	return pen
}

// clusterText returns the characters of a cluster without its control
// characters, which draw nothing.
func clusterText(rs []rune) string {
	b := make([]rune, 0, len(rs))
	for _, r := range rs {
		if !unicode.IsControl(r) {
			b = append(b, r)
		}
	}
	return string(b)
}

// visualOrder returns the indices of n pieces in visual order given each
// piece's embedding level: from the highest level down to 1, every maximal
// sequence at that level or above is reversed (UAX #9, L2).
func visualOrder(n int, level func(int) int) []int {
	order := make([]int, n)
	top := 0
	for i := range order {
		order[i] = i
		top = max(top, level(i))
	}
	for lv := top; lv >= 1; lv-- {
		for i := 0; i < n; {
			if level(order[i]) < lv {
				i++
				continue
			}
			j := i
			for j < n && level(order[j]) >= lv {
				j++
			}
			slices.Reverse(order[i:j])
			i = j
		}
	}
	return order
}
