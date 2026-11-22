package text

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	gofont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"

	"github.com/gotenberg/litho/pkg/internal/dictbreak"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
	// BreakAs is the character the object stands for in line breaking,
	// 0 for U+FFFC, which breaks before and after (UAX #14, LB20): a ruby
	// column breaks as its base text does (CSS Ruby 1, 3.4).
	BreakAs rune
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
	// BreakWord is a soft wrap opportunity at a word boundary a word list
	// finds inside text written without spaces between its words.
	BreakWord
)

// ParagraphRun is one piece of a [Paragraph] in logical order: text in one
// style, or an atomic object.
type ParagraphRun struct {
	// Text is UTF-8; invalid bytes read as U+FFFD. It is shaped as given:
	// the caller has already processed white space. A control character
	// keeps its place in the paragraph's text and draws nothing.
	Text  string
	Style Style
	// Extract, when set, is the text the run's glyphs extract as in place
	// of Text, of as many characters: the characters before a case
	// mapping the caller applied for drawing, as synthesised small
	// capitals are. An Extract of another length is ignored.
	Extract string
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
	Text *typeset.Text
	// X is the offset of the piece from the line start, Width its advance,
	// both in points.
	X, Width float64
	// Ascent and Descent are the piece's extent above and below the
	// baseline: the face's at its size, or the object's.
	Ascent, Descent float64
	// LineGap is the face's line gap at its size, 0 for an object.
	LineGap float64
	// EmAscent and EmDescent split the face's em box at the baseline
	// ([Metrics] EmAscent), 0 for an object.
	EmAscent, EmDescent float64
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
	text []rune
	// extract is the text the glyphs extract as, character for character,
	// nil when it is text ([ParagraphRun.Extract]).
	extract []rune
	// runOf is the run of each character: a run index fits 16 bits, since
	// a paragraph holds at most [MaxLayoutRuns] runs.
	runOf []uint16
	adv   []float64
	brk   []Break
	graph []bool
	// objects holds the object of each object run, nil for a text run: an
	// object run is one character.
	objects []*Object
	base    Direction
	items   []shapedItem
	// level is each character's resolved embedding level (UAX #9, X1 to
	// I2).
	level []int8
	// mem is the conversion's memory budget the glyph runs of its lines
	// are charged to, nil for none.
	mem *limits.Budget
	// ink is the class the glyph of each fullwidth ink-classed punctuation
	// gives it ([inkClass]), punctNone for the other characters; nil when
	// the paragraph holds none.
	ink []punct
}

// The largest run index of a [Paragraph] fits the 16 bits a character's
// run takes: the constant fails the build if [MaxLayoutRuns] outgrows
// them.
const _ uint16 = MaxLayoutRuns - 1

// shapedItem is one shaped piece of a paragraph, in logical order: a run of
// one style, face and direction, or one object.
type shapedItem struct {
	start, end int
	run        int
	level      int
	rtl        bool
	face       *typeset.Face
	// palette is the palette of the face's layered glyphs, nil for its
	// first (see [layoutState.palette]).
	palette []typeset.Color
	// plain is whether the face is a colour face drawn without its colour
	// glyphs (see [Fonts.WithPlainGlyphs]).
	plain  bool
	style  Style
	glyphs []shapedGlyph
	// ascent, descent and gap are the face's line bounds at the size;
	// underline and strike its decoration metrics.
	ascent, descent, gap   float64
	emAscent, emDescent    float64
	ulPos, ulThick         float64
	strikePos, strikeThick float64
	// stroke and skew synthesise the bold and italic the face lacks.
	stroke, skew float64
	object       bool
	// upright reports glyphs set upright in a vertical line
	// ([Style.Orientation]); squeeze, in (0, 1), the scale that narrows
	// the glyphs of a combined run, 0 for none.
	upright bool
	squeeze float64
	// fullQuotes reports a face that draws “ one em wide, read when the
	// item holds a CJK punctuation: its fullwidth punctuation takes a
	// quotation mark next to it as fullwidth, whatever face draws the mark
	// (trimAdjacent).
	fullQuotes bool
}

// shapedGlyph is one glyph of a [shapedItem], in points. A paragraph
// holds one for each glyph of its text as long as its lines may be laid
// out again, so the glyph ID, the first character of the glyph's cluster
// and the cluster's length share one word ([packGlyph]).
type shapedGlyph struct {
	xoff, yoff float64
	advance    float64
	packed     uint64
}

// clusterBits is the width of each of the two character counts a
// [shapedGlyph] packs, a cluster's first character and its length, both
// at most [MaxLayoutRunes]; the glyph ID takes the 16 bits above them.
const clusterBits = 24

// The packed counts hold every character index of a paragraph: an array
// of negative length fails the build if [MaxLayoutRunes] outgrows them.
var _ [1<<clusterBits - 1 - MaxLayoutRunes]struct{}

// clusterMask keeps one count of a packed [shapedGlyph].
const clusterMask = 1<<clusterBits - 1

// packGlyph returns the packed word of a glyph of ID gid whose cluster
// starts at character cluster and spans n characters, both in [0,
// MaxLayoutRunes].
func packGlyph(gid uint16, cluster, n int) uint64 {
	return uint64(gid)<<(2*clusterBits) | uint64(n&clusterMask)<<clusterBits | uint64(cluster&clusterMask) //nolint:gosec // both in [0, MaxLayoutRunes], checked above
}

// gid returns the glyph's ID.
func (g *shapedGlyph) gid() uint16 { return uint16(g.packed >> (2 * clusterBits)) }

// cluster returns the first character of the glyph's cluster.
func (g *shapedGlyph) cluster() int { return int(g.packed & clusterMask) } // fits int: masked to clusterBits, 24 bits

// clusterLen returns the number of characters of the glyph's cluster.
func (g *shapedGlyph) clusterLen() int { return int(g.packed >> clusterBits & clusterMask) } // fits int: masked to clusterBits, 24 bits

// Shape shapes runs into one bidirectional paragraph for a caller that
// breaks lines itself: the HTML layout engine, which needs per-line widths,
// atomic inline boxes and its own white space rules.
//
// Shape resolves the paragraph's base direction from opts, itemizes the
// text by direction, script, style and face, shapes each item with HarfBuzz
// with the face fallback [Fonts.Layout] documents, applies word spacing to
// the word separators and letter spacing after every character cluster (CSS
// Text 3, 8.2), and records the UAX #14 line break opportunities and the
// UAX #29 grapheme boundaries of the text. A variable document face draws
// its instance as [Fonts.Layout] documents.
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
	// The instances of variable faces the paragraph creates are charged
	// to the conversion.
	st.mem = limits.BudgetFrom(ctx)
	// total is the paragraph's character count: the per-character
	// slices, held as long as the paragraph, take no spare capacity.
	p := &Paragraph{
		mem:     limits.BudgetFrom(ctx),
		text:    make([]rune, 0, total),
		runOf:   make([]uint16, 0, total),
		objects: make([]*Object, len(runs)),
	}
	shapeText := make([]rune, 0, total)
	for i, r := range runs {
		run := uint16(i) //nolint:gosec // fits uint16: at most MaxLayoutRuns runs, checked above
		if r.Object != nil {
			p.text = append(p.text, objectReplacement)
			shapeText = append(shapeText, objectReplacement)
			p.runOf = append(p.runOf, run)
			p.objects[i] = r.Object
			p.setExtract(len(p.text)-1, "", total)
			continue
		}
		from := len(p.text)
		chromium := r.Style.Fallback.rule() == FallbackChromium
		for _, c := range r.Text {
			p.text = append(p.text, c)
			switch {
			case unicode.IsControl(c):
				c = wordJoiner
			case chromium && isSeparator(c):
				c = ' '
			}
			shapeText = append(shapeText, c)
			p.runOf = append(p.runOf, run)
		}
		p.setExtract(from, r.Extract, total)
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
	level, err := resolveLevels(ctx, bidiText, at, p.base)
	if err != nil {
		return nil, Report{}, fmt.Errorf("shaping text: %w", err)
	}
	p.level = level
	p.segment(st, segmentText(runs, p.text, shapeText, p.runOf))
	if err := p.shape(ctx, st, shapeText); err != nil {
		return nil, Report{}, err
	}
	p.trimAdjacent()
	p.autospace()
	return p, st.report(), nil
}

// segmentText returns the text the line breaks are found in: text, the
// shaped form of the paragraph's characters orig, with the character an
// object breaks as (Object.BreakAs) in its place, and a no-break space in
// place of a separator shaped as a space. It copies text only when it
// changes a character.
func segmentText(runs []ParagraphRun, orig, text []rune, runOf []uint16) []rune {
	out := text
	for i, r := range text {
		if r == ' ' && isSeparator(orig[i]) {
			if &out[0] == &text[0] {
				out = slices.Clone(text)
			}
			out[i] = noBreakSpace
			continue
		}
		if r != objectReplacement {
			continue
		}
		o := runs[runOf[i]].Object
		if o == nil || o.BreakAs == 0 || !utf8.ValidRune(o.BreakAs) || unicode.IsControl(o.BreakAs) {
			continue
		}
		if &out[0] == &text[0] {
			out = slices.Clone(text)
		}
		out[i] = o.BreakAs
	}
	return out
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
	p.dictionaryBreaks(text)
}

// dictionaryBreaks opens a [BreakWord] opportunity at each word boundary
// inside a run of Thai letters, where a grapheme cluster starts (UAX #29)
// and nothing else closes the boundary. UAX #14 gives Thai class SA and
// leaves its breaks to a language-specific analysis outside its rules
// (LB1); the boundaries are those [dictbreak.Thai] finds, ICU's, which
// Chromium breaks Thai lines at.
func (p *Paragraph) dictionaryBreaks(text []rune) {
	var ends []int
	for i := 0; i < len(text); {
		if !dictbreak.IsThai(text[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(text) && dictbreak.IsThai(text[j]) {
			j++
		}
		ends = dictbreak.Thai(text[i:j], ends[:0])
		for _, e := range ends {
			if k := i + e; p.brk[k] == NoBreak && p.graph[k] {
				p.brk[k] = BreakWord
			}
		}
		i = j
	}
}

// noBreakSpace is U+00A0, which breaks as UAX #14 class GL.
const noBreakSpace = '\u00a0'

// isSeparator reports U+2028 LINE SEPARATOR and U+2029 PARAGRAPH
// SEPARATOR. Under the Chromium fallback rule each is shaped as a space
// of its run's face and offers no line break, and no white space
// processing folds it: UAX #14 makes them mandatory breaks (class BK),
// which Chromium does not honour, and CSS Text 3, 4 counts neither as
// white space.
//
// # Corpus evidence
//
//   - wpt: css-fonts/separators draws each as the space of a font that
//     holds glyphs for both.
//   - probe, html: Chromium 156 draws "VV", U+2028 and "OO" in Ahem at
//     20px on one line in a 70px wide box, under white-space normal, pre,
//     pre-wrap and nowrap alike; two of them in a row and the spaces
//     beside one keep their width.
func isSeparator(r rune) bool {
	return r == '\u2028' || r == '\u2029'
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
		if o := p.object(start); o != nil {
			lv := int(p.level[start])
			p.items = append(p.items, shapedItem{
				start: start, end: start + 1, run: int(p.runOf[start]), level: lv, rtl: lv%2 == 1,
				ascent: o.Ascent, descent: o.Descent, object: true,
			})
			p.adv[start] = o.Width
			start++
			continue
		}
		end := start + 1
		for end < len(text) && p.object(end) == nil {
			end++
		}
		if err := p.shapeSegment(ctx, st, text, start, end, baseDir); err != nil {
			return err
		}
		start = end
	}
	return nil
}

// shapeSegment shapes text[start:end], which holds no object. ctx's
// error stops it between two items.
func (p *Paragraph) shapeSegment(ctx context.Context, st *layoutState, text []rune, start, end int, baseDir di.Direction) error {
	first := st.styles[p.runOf[start]]
	items, err := st.split(ctx, shaping.Input{
		Text:      text,
		RunStart:  start,
		RunEnd:    end,
		Direction: baseDir,
		Face:      st.face(first.primary),
		Size:      first.size,
		Language:  first.lang,
	}, fixedFontmap{face: st.face(first.primary)})
	if err != nil {
		return fmt.Errorf("shaping text: %w", err)
	}
	seqs := emojiSequences(text[start:end],
		func(i int) EmojiVariant { return st.emojiVariant(st.styles[p.runOf[start+i]].style.emojiVariant()) },
		func(i int) bool { return st.primaryDrawsAsText(int(p.runOf[start+i]), text[start+i]) })
	for i := range seqs {
		seqs[i].start += start
		seqs[i].end += start
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("shaping text: %w", err)
		}
		for s := item.RunStart; s < item.RunEnd; {
			e := s + 1
			for e < item.RunEnd && p.level[e] == p.level[s] && (p.runOf[e] == p.runOf[s] || shapesTogether(st.styles[p.runOf[s]].style, st.styles[p.runOf[e]].style)) {
				e++
			}
			idx := int(p.runOf[s])
			ss := st.styles[idx]
			piece := item
			piece.RunStart, piece.RunEnd = s, e
			// The run's own first face draws its leading spaces, which
			// select no face of their own: not the face of the
			// paragraph's first run.
			piece.Face = st.face(ss.primary)
			piece.Size = ss.size
			if ss.lang != "" {
				piece.Language = ss.lang
			}
			piece.FontFeatures = ss.style.features()
			// The resolved level, explicit embeddings and overrides
			// included, decides the direction (UAX #9, L2).
			piece.Direction = di.DirectionLTR
			if p.level[s]%2 == 1 {
				piece.Direction = di.DirectionRTL
			}
			inputs := st.splitByFace(piece, idx, seqs)
			if ss.style.Fallback.rule() == FallbackChromium {
				inputs = st.spacesOwnFace(inputs, text, idx)
			}
			if o := ss.style.Orientation; o != OrientNone {
				var oriented []shaping.Input
				for _, in := range inputs {
					oriented = append(oriented, splitByOrientation(in, o)...)
				}
				inputs = oriented
			}
			for _, in := range inputs {
				if err := st.instanceInput(ctx, &in, &ss.style); err != nil {
					return fmt.Errorf("shaping text: %w", err)
				}
				st.faceInput(&in, &ss.style)
				out := st.kit.shaper.Shape(in)
				rescale(&out, in.Size)
				st.instanceAdvances(&out, in.Size)
				if ss.style.Orientation == OrientCombine {
					out = st.combineWidth(in, out)
				}
				st.collectMissing(&out, text)
				if ss.word != 0 {
					out.AddWordSpacing(text, ss.word)
				}
				for _, sub := range p.splitByRun(&out) {
					r := int(p.runOf[sub.Runes.Offset])
					it, err := p.convert(ctx, st, &sub, r, st.styles[r].style)
					if err != nil {
						return err
					}
					p.items = append(p.items, it)
				}
			}
			s = e
		}
	}
	return nil
}

// convert turns one shaped output into a [shapedItem] in points, adding the
// letter spacing after each cluster and recording every cluster's advance
// on its first character.
func (p *Paragraph) convert(ctx context.Context, st *layoutState, out *shaping.Output, run int, style Style) (shapedItem, error) {
	fc := st.byFace[out.Face]
	var doc *FaceDescriptor
	if fc != nil {
		doc = fc.doc
		style.Size *= faceScale(&style, fc, out.Face)
	}
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
	if doc != nil {
		m := Metrics{Ascent: it.ascent, Descent: it.descent, LineGap: it.gap}
		doc.override(&m, style.Size)
		it.ascent, it.descent, it.gap = m.Ascent, m.Descent, m.LineGap
	}
	if fc != nil {
		// Metrics applies the rule's size-adjust, which style.Size holds
		// already.
		m := fc.Metrics(style.Size / doc.sizeAdjust())
		it.emAscent, it.emDescent = m.EmAscent, m.EmDescent
		cf, err := fc.Typeset(ctx)
		if err != nil {
			return shapedItem{}, fmt.Errorf("shaping text: %w", err)
		}
		it.face = cf
		it.palette = st.palette(fc, cf, &style)
		it.plain = st.fonts.plain && fc.colourTables() != 0
		it.stroke, it.skew = synthesis(requestOf(style), fc, style.Size)
		if m, ok := fc.wordMetrics(style, style.Size); ok {
			it.ascent, it.descent, it.gap = m.Ascent, m.Descent, m.LineGap
		}
	}
	if it.upright = out.Direction.IsVertical() && !out.Direction.IsSideways(); it.upright && fc != nil {
		// The line box of upright glyphs is the face's horizontal one: the
		// vertical font extents HarfBuzz reports measure across the
		// column.
		m := fc.Metrics(style.Size / doc.sizeAdjust())
		it.ascent, it.descent, it.gap = m.Ascent, m.Descent, m.LineGap
	}
	// center is the distance from the alphabetic baseline down to the
	// central one, the middle of the em box, on which an upright glyph is
	// centred (CSS Inline 3, 4.1, the central baseline; CSS Writing Modes
	// 4, 4.2).
	center := float64((it.emDescent - it.emAscent) / 2)
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
	plainVertical := it.upright && out.Face != nil && !out.Face.HasVerticalMetrics()
	n := len(p.text)
	punctuated := false
	for k, g := range out.Glyphs {
		start, end := clusterRange(g, n)
		gid := glyphID(g.GlyphID)
		if gid == 0 {
			gid = blankEmoji(out.Face, p.text[start:end])
		}
		it.glyphs[k] = shapedGlyph{
			xoff:    st.toPoints(g.XOffset) * style.scale(),
			yoff:    st.toPoints(-g.YOffset),
			advance: st.toPoints(g.Advance) * style.scale(),
			packed:  packGlyph(gid, start, end-start),
		}
		if it.upright {
			// HarfBuzz places a glyph shaped top to bottom by its
			// horizontal origin, offset from the pen at its vertical
			// origin, and moves the pen down by the vertical advance, both
			// with y up. Along the line, which runs down the page, the
			// glyph sits that far below the pen; across it, the vertical
			// origin is on the central baseline.
			it.glyphs[k].xoff = st.toPoints(-g.YOffset)
			it.glyphs[k].yoff = center + st.toPoints(-g.XOffset)
			it.glyphs[k].advance = st.toPoints(-g.Advance)
			if plainVertical {
				uprightWithoutVmtx(&it.glyphs[k], out.Face, g.GlyphID, it.ascent, it.descent, style.Size)
			}
			continue
		}
		if end-start == 1 && cjkPunctuation(p.text[start]) {
			punctuated = true
			if strings.ContainsRune(inkClassed, p.text[start]) {
				if p.ink == nil {
					p.ink = make([]punct, n)
				}
				p.ink[start] = inkClass(float64(g.XOffset+g.XBearing+g.Width/2), float64(g.Advance))
			}
		}
	}
	if style.OfficeEmoji && fc != nil {
		w := float64(float64(segoeEmojiAdvance*style.Size) * style.scale())
		switch {
		case fc.colourTables() != 0:
			segoeAdvances(it.glyphs, p.text, w)
		case (style.settings().Emoji == EmojiText || st.fonts.plain) && fc != st.styles[run].primary:
			if !segoeTextAdvances(it.glyphs, p.text, w) {
				break
			}
			if style.WordMetrics {
				it.ascent, it.descent, it.gap = float64(segoeEmojiAscent*style.Size), float64(segoeEmojiDescent*style.Size), 0
			}
			if style.weight() >= syntheticBoldThreshold {
				it.stroke = min(style.Size/wordEmojiBold, style.Size)
			}
		}
	}
	if punctuated {
		if gid, ok := out.Face.NominalGlyph('“'); ok {
			it.fullQuotes = fullwidthGlyph(float64(out.Face.HorizontalAdvance(gid)), float64(out.Face.Upem()))
		}
	}
	if style.Orientation == OrientCombine {
		p.combine(&it, style.Size)
		return it, nil
	}
	if style.LetterSpacing != 0 {
		// CSS Text 3, 7.2: the spacing goes after every typographic
		// character unit, which is the visually last glyph of a cluster in
		// a left-to-right run and the visually first in a right-to-left one.
		for k := range it.glyphs {
			last := k == len(it.glyphs)-1 || it.glyphs[k+1].cluster() != it.glyphs[k].cluster()
			if it.rtl {
				last = k == 0 || it.glyphs[k-1].cluster() != it.glyphs[k].cluster()
			}
			// A default ignorable character, a format control such as a
			// zero width space or a bidi control, is no typographic
			// character unit: it takes no spacing. Nor does a letter of a
			// cursive script, whose joins a gap would break (CSS Text 3,
			// 7.2.1).
			if c := it.glyphs[k].cluster(); last && c < n && !unicode.Is(unicode.Cf, p.text[c]) && !cursive(p.text[c]) {
				it.glyphs[k].advance += style.LetterSpacing
			}
		}
	}
	for k := range it.glyphs {
		g := &it.glyphs[k]
		if c := g.cluster(); c < n {
			p.adv[c] += g.advance
		}
	}
	return it, nil
}

// Len returns the number of characters of the paragraph's text.
func (p *Paragraph) Len() int { return len(p.text) }

// Rune returns the character at i, which lies in [0, Len()).
func (p *Paragraph) Rune(i int) rune { return p.text[i] }

// RunOf returns the index of the run the character at i comes from.
func (p *Paragraph) RunOf(i int) int { return int(p.runOf[i]) }

// object returns the object of the character at i, nil for a character
// of text.
func (p *Paragraph) object(i int) *Object { return p.objects[p.runOf[i]] }

// Advance returns the advance the character at i contributes, in points: a
// cluster's advance sits on its first character, 0 on the others.
func (p *Paragraph) Advance(i int) float64 { return p.adv[i] }

// SetAdvance sets the advance of the object character at i, for an object
// whose width depends on where it lands, a tab. It does nothing on a
// character that is not an object or for a width that is not finite and
// positive.
func (p *Paragraph) SetAdvance(i int, w float64) {
	if i < 0 || i >= len(p.text) || p.object(i) == nil || !finiteWithin(w, maxLineWidth) || w < 0 {
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
			EmAscent: it.emAscent, EmDescent: it.emDescent,
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
// their advance. The run's glyph slice is allocated at its length: the
// run lives as long as the page that draws it.
func (p *Paragraph) glyphRun(sp *Span, it *shapedItem, start, end, hangFrom int) float64 {
	t := &typeset.Text{Face: it.face, Size: it.style.Size, Color: it.style.Color, Origin: typeset.Point{X: sp.X}, Stroke: it.stroke, Skew: it.skew, Palette: it.palette, Plain: it.plain, Upright: it.upright}
	hs := it.style.scale()
	if hs != 1 {
		t.HScale = hs
	}
	if it.squeeze > 0 && it.squeeze < 1 {
		// An upright run's positions are its own: the scale narrows each
		// glyph alone.
		t.HScale, hs = it.squeeze, 1
	}
	// first and last bound the glyphs of the range, which the second pass
	// alone visits.
	drawn, first, last := 0, len(it.glyphs), -1
	for k := range it.glyphs {
		if c := it.glyphs[k].cluster(); c >= start && c < end {
			first, last = min(first, k), k
			if !p.control(c) {
				drawn++
			}
		}
	}
	if drawn > 0 {
		t.Glyphs = make([]typeset.Glyph, 0, drawn)
	}
	pen := 0.0
	cluster := -1
	for k := first; k <= last; k++ {
		g := &it.glyphs[k]
		c := g.cluster()
		if c < start || c >= end {
			continue
		}
		adv := g.advance
		if c >= hangFrom {
			adv = 0
		}
		if p.control(c) {
			pen += adv
			continue
		}
		// The positions are written unscaled: the run's HScale scales
		// them back (ISO 32000-1, 9.3.4).
		cg := typeset.Glyph{GID: g.gid(), X: (pen + g.xoff) / hs, Y: g.yoff}
		if c != cluster {
			cluster = c
			e := min(c+max(g.clusterLen(), 1), len(p.text))
			cg.Text = clusterText(p.extractText(c, e))
		}
		t.Glyphs = append(t.Glyphs, cg)
		pen += adv
	}
	if len(t.Glyphs) > 0 && t.Face != nil && t.Size > 0 {
		// A run drawn at size 0, as font-size-adjust: 0 draws one, takes
		// its place and draws nothing. A refusal sticks and cancels the conversion, which stops at its
		// next checkpoint: a line has no error to return.
		p.mem.Take(int64(len(t.Glyphs)), glyphCost, "glyph runs")
		sp.Text = t
	}
	return pen
}

// setExtract records the extracted text of the characters starting at
// from, those of the run just added: ex when it has as many characters, else
// the characters themselves. total is the paragraph's character count.
func (p *Paragraph) setExtract(from int, ex string, total int) {
	valid := ex != "" && utf8.RuneCountInString(ex) == len(p.text)-from
	switch {
	case valid && p.extract == nil:
		p.extract = make([]rune, from, total)
		copy(p.extract, p.text[:from])
		p.extract = append(p.extract, []rune(ex)...)
	case valid:
		p.extract = append(p.extract, []rune(ex)...)
	case p.extract != nil:
		p.extract = append(p.extract, p.text[from:]...)
	}
}

// extractText returns the text the glyphs of the characters [c, e)
// extract as.
func (p *Paragraph) extractText(c, e int) []rune {
	if p.extract != nil && e <= len(p.extract) {
		return p.extract[c:e]
	}
	return p.text[c:e]
}

// control reports whether the glyph of the cluster at i stands for a
// control character, which draws nothing.
func (p *Paragraph) control(i int) bool {
	return unicode.IsControl(p.text[min(i, len(p.text)-1)])
}

// asciiText holds every ASCII character once, so that the text of a
// one-character ASCII cluster is a slice of it rather than a string of
// its own: a page holds one such string for each glyph it draws.
var asciiText = func() string {
	b := make([]byte, utf8.RuneSelf)
	for i := range b {
		b[i] = byte(i)
	}
	return string(b)
}()

// clusterText returns the characters of a cluster without its control
// characters, which draw nothing.
func clusterText(rs []rune) string {
	if len(rs) == 1 && rs[0] >= 0 && rs[0] < utf8.RuneSelf && !unicode.IsControl(rs[0]) {
		return asciiText[rs[0] : rs[0]+1]
	}
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

// shapesTogether reports whether runs of the styles a and b shape as one
// piece, kerning and contextual forms across their boundary: they differ
// in colour alone, a change that does not affect shaping, across which
// CSS Text 3, 7.3 does not break shaping; or, in Office runs
// ([Style.OfficeEmoji]), in colour and emoji presentation, which a Word
// run's colour sets and which picks only the face of an emoji, each
// character's from its own run.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws "marks’"
//     and "marks<span>’</span>" in Liberation Serif at 32px at the same
//     width, the pair kerned in both; css/css-content/quotes-001 and
//     fifteen other quotes tests compare a quote of generated content
//     with the same characters in the text.
func shapesTogether(a, b Style) bool {
	return slices.Equal(a.Families, b.Families) && a.Weight == b.Weight && a.Italic == b.Italic &&
		a.Fallback == b.Fallback && a.Oblique == b.Oblique && a.Stretch == b.Stretch &&
		a.NoSyntheticBold == b.NoSyntheticBold && a.NoSyntheticItalic == b.NoSyntheticItalic &&
		a.Autospace == b.Autospace && a.AutospaceAlpha == b.AutospaceAlpha && a.AutospaceNumeric == b.AutospaceNumeric &&
		a.NoSpacingTrim == b.NoSpacingTrim && a.Size == b.Size && a.Lang == b.Lang &&
		a.LetterSpacing == b.LetterSpacing && a.WordSpacing == b.WordSpacing && a.Scale == b.Scale &&
		a.NoKerning == b.NoKerning && a.WordMetrics == b.WordMetrics && a.OfficeEmoji == b.OfficeEmoji &&
		a.Orientation == b.Orientation && a.Orientation != OrientCombine && sameShaping(a.Settings, b.Settings) &&
		(a.OfficeEmoji || a.Settings.emoji() == b.Settings.emoji())
}

// splitByRun cuts out into one output per run of its characters, each
// glyph going with the run of its cluster's first character, so that a
// glyph keeps the advance shaping gave it across a run boundary.
func (p *Paragraph) splitByRun(out *shaping.Output) []shaping.Output {
	start, end := out.Runes.Offset, out.Runes.Offset+out.Runes.Count
	one := true
	for i := start + 1; i < end; i++ {
		if p.runOf[i] != p.runOf[start] {
			one = false
			break
		}
	}
	if one {
		return []shaping.Output{*out}
	}
	var subs []shaping.Output
	for s := start; s < end; {
		e := s + 1
		for e < end && p.runOf[e] == p.runOf[s] {
			e++
		}
		sub := *out
		sub.Runes = shaping.Range{Offset: s, Count: e - s}
		sub.Glyphs = nil
		sub.Advance = 0
		for _, g := range out.Glyphs {
			if c := g.TextIndex(); c >= s && c < e {
				sub.Glyphs = append(sub.Glyphs, g)
				sub.Advance += g.Advance
			}
		}
		subs = append(subs, sub)
		s = e
	}
	return subs
}

// cursiveScripts are the cursive scripts of CSS Text 3, Appendix D, whose
// letters take no letter-spacing (7.2.1): litho cannot stretch their joins.
var cursiveScripts = []*unicode.RangeTable{
	unicode.Arabic, unicode.Hanifi_Rohingya, unicode.Mandaic, unicode.Mongolian,
	unicode.Nko, unicode.Phags_Pa, unicode.Syriac,
}

// cursive reports a character of a cursive script.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) adds no letter-spacing
//     to Arabic letters and digits, Syriac, N'Ko, Mongolian, Mandaic and
//     Hanifi Rohingya, and adds it to a space between Arabic words, to
//     Latin letters before Arabic ones, and to Adlam, Hebrew, Devanagari
//     and Thai.
func cursive(r rune) bool {
	return unicode.In(r, cursiveScripts...)
}

// uprightWithoutVmtx sets the upright glyph g of a face that has no
// vertical metrics (no vmtx table, OpenType 1.9, which OpenType's vertical
// layout otherwise reads) as Chromium sets it: its vertical advance the
// face's ascent and descent and its horizontal origin the ascent below the
// pen, each rounded to whole CSS pixels, where HarfBuzz centres the glyph's
// ink in the face's height. ascent and descent are the face's at size
// points, which HarfBuzz scales gid's vertical origin to.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 advances the
//     upright letters of Liberation Sans, Liberation Serif and DejaVu Sans
//     at 12, 16 and 23 px by the rounded ascent plus the rounded descent and
//     sets each baseline the rounded ascent below the top of its advance,
//     in vertical-rl and vertical-lr under text-orientation: upright.
func uprightWithoutVmtx(g *shapedGlyph, face *gofont.Face, gid gofont.GID, ascent, descent, size float64) {
	a, d := wholePixels(ascent), wholePixels(descent)
	if upem := float64(face.Upem()); upem > 0 {
		_, oy := face.GlyphVOrigin(gid)
		g.xoff -= float64(float64(oy) * size / upem)
	}
	g.xoff += a
	g.advance = a + d
}

// wholePixels returns v points rounded to whole CSS pixels, halves up, as
// Blink rounds a face's ascent and descent.
func wholePixels(v float64) float64 {
	return float64(math.Floor(float64(v*cssPixelsPerPoint)+0.5) / cssPixelsPerPoint)
}
