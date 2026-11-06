package text

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/harfbuzz"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/segmenter"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// MaxLayoutRunes bounds the characters of one [Fonts.Layout] call, counted
// over the text of every run before white space handling. Anti-DoS guard,
// not spec-mandated: shaping and line breaking hold a few hundred bytes per
// character, so the bound keeps one call under a few hundred MiB, and a
// million characters fill some 300 pages; a converter lays a document out
// paragraph by paragraph, far below it.
const MaxLayoutRunes = 1 << 20

// MaxLayoutRuns bounds the runs of one [Fonts.Layout] call. Anti-DoS guard,
// not spec-mandated: every run costs a face match, and a paragraph changes
// style a few times per sentence, not tens of thousands of times.
const MaxLayoutRuns = 1 << 16

// maxMissingGlyphs bounds [Report.MissingGlyphs]. Anti-DoS guard, not
// spec-mandated: the report tells a caller which scripts lack a font, and a
// few hundred characters name them all; text in an uncovered script would
// otherwise list every distinct character of it.
const maxMissingGlyphs = 256

// maxLineWidth is the widest line [Fonts.Layout] lays out, in points: a
// wider width acts as this one. Overflow guard, not spec-mandated: go-text
// measures lines in 32-bit 26.6 fixed point at up to 64 shaping units per
// point, and a 65536-point line, 23 metres, is wider than any page.
const maxLineWidth = 1 << 16

// maxShapingUnitsPerPoint is the finest scale [Fonts.Layout] shapes at.
// go-text rounds the shaping size up to a whole unit, so text shapes at a
// size of a whole number of 1/64 points when its largest size allows, and
// glyph positions are rescaled to the exact size after shaping.
const maxShapingUnitsPerPoint = 64

// maxWordRunes bounds the characters go-text's line wrapper sees between two
// break opportunities. Complexity guard, not spec-mandated: the wrapper
// rescans the text up to the next opportunity for every line it ends inside
// a word, so a word of n characters costs n squared. A longer word is cut
// into pieces of at most this length, each wrapped on its own, which may end
// a line before it is full; real words, URLs included, are far shorter.
const maxWordRunes = 1024

// tabSize is the number of columns a preserved tab advances to, as CSS
// tab-size: 8 does.
const tabSize = 8

// Whitespace selects how [Fonts.Layout] treats white space.
type Whitespace uint8

const (
	// Collapse folds every sequence of spaces, tabs and line breaks (LF,
	// CR, VT, FF, U+0085, U+2028, U+2029) into one space, and drops the
	// spaces at the start and end of the text, as CSS white-space: normal
	// does. The text forms one paragraph. It is the zero value.
	Collapse Whitespace = iota
	// Preserve keeps every space, expands a tab to the next multiple of 8
	// columns, and starts a new line at every forced break: LF, CR LF, a
	// lone CR, VT, FF, U+0085, U+2028 and U+2029, as CSS white-space:
	// pre-wrap does. A break at the very end of the text ends the last
	// line and starts no new one.
	Preserve
)

// Options tunes a [Fonts.Layout] call.
type Options struct {
	// Whitespace is the white space handling, [Collapse] by default. A
	// value other than [Preserve] acts as [Collapse].
	Whitespace Whitespace
}

// Run is a piece of a paragraph in logical order, drawn in one style.
type Run struct {
	// Text is UTF-8; invalid bytes read as U+FFFD. Control characters
	// other than the white space [Whitespace] handles are dropped.
	Text  string
	Style Style
}

// Line is one laid-out line.
type Line struct {
	// Runs are the line's glyph runs in visual order, left to right. Each
	// Origin is relative to the line's left baseline point: X is the run's
	// offset from the line start, Y is 0. Parent is nil. A blank line has
	// no runs.
	Runs []*compose.Text
	// Width is the advance of the line, in points, without the spaces
	// that hang at its end.
	Width float64
	// Ascent and Descent are the largest ascent and descent, positive, of
	// the faces on the line at their sizes. A blank line takes those of the
	// face its style matches.
	Ascent, Descent float64
	// LineGap is the largest line gap of the faces on the line.
	LineGap float64
}

// Report records what a [Fonts.Layout] call could not draw faithfully.
type Report struct {
	// MissingGlyphs lists the characters no indexed face covers, drawn as
	// the matched face's .notdef glyph. It is sorted, holds each character
	// once and keeps at most the first 256 distinct characters met.
	MissingGlyphs []rune
}

// Layout lays runs out into lines no wider than width points.
//
// It splits the text into paragraphs at forced breaks ([Preserve] only),
// takes each paragraph's direction from its first strong character (UAX #9,
// P2 and P3; left to right without one), itemizes it by direction, script,
// style and face, shapes each item with HarfBuzz and breaks the shaped text
// at UAX #14 opportunities. A word wider than the line breaks at a grapheme
// boundary (UAX #29); a single grapheme wider than the line overflows it. A
// word longer than 1024 characters is also cut every 1024 characters, which
// may end a line before it is full.
// Spaces at the end of a line hang: they draw no advance and do not count
// toward [Line.Width].
//
// Within a line, runs are ordered visually by go-text's line wrapper: a
// sequence of runs whose direction differs from the paragraph's is
// reversed as a block. Deeper embedding levels (UAX #9, L2) are not
// reordered further.
//
// When the face [Fonts.Match] picks for a run lacks a character, the face
// go-text resolves for that character under the same families, weight and
// style draws it, and failing that any indexed face that covers it, closest
// weight and style first. A character no face covers draws the matched
// face's .notdef glyph and is listed in [Report.MissingGlyphs].
//
// Layout returns [ErrInvalidWidth] for a width that is not finite and
// positive (a width past 65536 points acts as 65536), [ErrInvalidStyle] for
// a run whose style is out of range, [ErrTooManyRuns] past [MaxLayoutRuns],
// [ErrTextTooLong] past [MaxLayoutRunes], [ErrNoFonts] on an empty index,
// and ctx's error, checked per line. It never truncates text.
//
// Layout does not modify runs. The returned lines share [compose.Face]
// values with other calls on the same Fonts.
func (f *Fonts) Layout(ctx context.Context, runs []Run, width float64, opts Options) ([]Line, Report, error) {
	if math.IsNaN(width) || math.IsInf(width, 0) || width <= 0 {
		return nil, Report{}, fmt.Errorf("%w: %v", ErrInvalidWidth, width)
	}
	width = min(width, maxLineWidth)
	if len(runs) > MaxLayoutRuns {
		return nil, Report{}, fmt.Errorf("%w: %d runs, at most %d", ErrTooManyRuns, len(runs), MaxLayoutRuns)
	}
	total := 0
	for _, r := range runs {
		total += utf8.RuneCountInString(r.Text)
		if total > MaxLayoutRunes {
			return nil, Report{}, fmt.Errorf("%w: more than %d characters", ErrTextTooLong, MaxLayoutRunes)
		}
	}
	for i, r := range runs {
		if err := r.Style.validate(); err != nil {
			return nil, Report{}, fmt.Errorf("run %d: %w", i, err)
		}
	}
	if len(f.faces) == 0 {
		return nil, Report{}, ErrNoFonts
	}
	if err := ctx.Err(); err != nil {
		return nil, Report{}, fmt.Errorf("laying out text: %w", err)
	}
	st := newLayoutState(f, runs)
	norm := normalize(runs, opts.Whitespace)
	maxWidth := st.toFixed(width)
	var lines []Line
	for _, p := range norm.paras {
		para, styleOf := norm.text[p.start:p.end], norm.style[p.start:p.end]
		dir := paragraphDirection(para)
		start := 0
		for _, end := range st.wrapPieces(para) {
			var err error
			lines, err = st.paragraph(ctx, para[start:end], styleOf[start:end], p.style, dir, maxWidth, lines)
			if err != nil {
				return nil, Report{}, err
			}
			start = end
		}
	}
	return lines, st.report(), nil
}

// layoutState is the state of one [Fonts.Layout] call. It is never shared
// between goroutines: go-text's shaper, segmenter, line wrapper and faces
// keep caches that are not safe for concurrent use.
type layoutState struct {
	fonts  *Fonts
	styles []styleState
	// unit is the number of shaping units per point.
	unit float64

	local   map[*Face]*font.Face
	cache   map[resolveKey]*Face
	missing map[rune]struct{}

	shaper  shaping.HarfbuzzShaper
	seg     shaping.Segmenter
	wrapper shaping.LineWrapper
	lineSeg segmenter.Segmenter
}

// styleState is a run's style resolved for shaping.
type styleState struct {
	style   Style
	query   fontscan.Query
	primary *Face
	size    fixed.Int26_6
	letter  fixed.Int26_6
	word    fixed.Int26_6
	lang    language.Language
}

// resolveKey identifies one per-character face resolution.
type resolveKey struct {
	style  int
	script language.Script
	r      rune
}

// newLayoutState resolves the styles of runs. The shaping scale is the
// finest power of two, up to [maxShapingUnitsPerPoint], at which the
// largest size stays within [maxFontSize] units.
func newLayoutState(f *Fonts, runs []Run) *layoutState {
	largest := 0.0
	for _, r := range runs {
		largest = max(largest, r.Style.Size)
	}
	unit := float64(maxShapingUnitsPerPoint)
	for unit > 1 && largest*unit > maxFontSize {
		unit /= 2
	}
	st := &layoutState{
		fonts:   f,
		styles:  make([]styleState, len(runs)),
		unit:    unit,
		local:   make(map[*Face]*font.Face),
		cache:   make(map[resolveKey]*Face),
		missing: make(map[rune]struct{}),
	}
	for i, r := range runs {
		s := styleState{
			style:   r.Style,
			query:   queryFor(r.Style),
			primary: f.Match(r.Style),
			size:    st.toFixed(r.Style.Size),
			letter:  st.toFixed(r.Style.LetterSpacing),
			word:    st.toFixed(r.Style.WordSpacing),
		}
		if r.Style.Lang != "" {
			s.lang = language.NewLanguage(r.Style.Lang)
		}
		st.styles[i] = s
	}
	return st
}

// toFixed converts points to shaping units in 26.6 fixed point. Callers
// bound v first, so the result fits.
func (st *layoutState) toFixed(v float64) fixed.Int26_6 {
	return fixed.Int26_6(math.Round(v * st.unit * 64))
}

// toPoints converts shaping units in 26.6 fixed point to points.
func (st *layoutState) toPoints(v fixed.Int26_6) float64 {
	return float64(v) / 64 / st.unit
}

// face returns this call's own go-text face for fc: go-text faces cache
// glyph lookups and are not safe for concurrent use.
func (st *layoutState) face(fc *Face) *font.Face {
	if face, ok := st.local[fc]; ok {
		return face
	}
	face := font.NewFace(fc.font)
	st.local[fc] = face
	return face
}

// resolve returns the face that draws r in the run of style index style.
func (st *layoutState) resolve(style int, script language.Script, r rune) *Face {
	key := resolveKey{style: style, script: script, r: r}
	if fc, ok := st.cache[key]; ok {
		return fc
	}
	s := &st.styles[style]
	fc := s.primary
	if !covers(fc, r) {
		st.fonts.mu.Lock()
		fc = st.fonts.resolveLocked(s.query, script, r)
		st.fonts.mu.Unlock()
		if !covers(fc, r) {
			fc = s.primary
		}
	}
	st.cache[key] = fc
	return fc
}

// covers reports whether fc maps r to a glyph.
func covers(fc *Face, r rune) bool {
	gid, ok := fc.font.NominalGlyph(r)
	return ok && gid != 0
}

// styleFontmap resolves faces for one style and script, the
// [shaping.Fontmap] go-text's face splitting asks.
type styleFontmap struct {
	st     *layoutState
	style  int
	script language.Script
}

// ResolveFace returns this call's face for the face that draws r.
func (m styleFontmap) ResolveFace(r rune) *font.Face {
	return m.st.face(m.st.resolve(m.style, m.script, r))
}

// fixedFontmap resolves every character to one face. Itemizing by
// direction and script runs with it, faces being resolved per style
// afterwards.
type fixedFontmap struct {
	face *font.Face
}

// ResolveFace returns the one face.
func (m fixedFontmap) ResolveFace(rune) *font.Face {
	return m.face
}

// paragraph lays out one paragraph, or one piece of it [layoutState.wrapPieces]
// cut, para, whose runes come from the runs styleOf names, in the paragraph
// direction dir, and appends its lines to lines. style is the run a blank
// paragraph takes its metrics from.
func (st *layoutState) paragraph(ctx context.Context, para []rune, styleOf []int, style int, dir di.Direction, maxWidth fixed.Int26_6, lines []Line) ([]Line, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("laying out text: %w", err)
	}
	if len(para) == 0 {
		s := st.styles[style]
		m := s.primary.Metrics(s.style.Size)
		return append(lines, Line{Ascent: m.Ascent, Descent: m.Descent, LineGap: m.LineGap}), nil
	}
	first := st.styles[styleOf[0]]
	items := slices.Clone(st.seg.Split(shaping.Input{
		Text:      para,
		RunStart:  0,
		RunEnd:    len(para),
		Direction: dir,
		Face:      st.face(first.primary),
		Size:      first.size,
		Language:  first.lang,
	}, fixedFontmap{face: st.face(first.primary)}))

	var outputs []shaping.Output
	for _, item := range items {
		for start := item.RunStart; start < item.RunEnd; {
			end := start + 1
			for end < item.RunEnd && styleOf[end] == styleOf[start] {
				end++
			}
			idx := styleOf[start]
			s := st.styles[idx]
			piece := item
			piece.RunStart, piece.RunEnd = start, end
			piece.Size = s.size
			if s.style.Lang != "" {
				piece.Language = s.lang
			}
			for _, in := range shaping.SplitByFace(piece, styleFontmap{st: st, style: idx, script: piece.Script}) {
				out := st.shaper.Shape(in)
				rescale(&out, in.Size)
				st.collectMissing(&out, para)
				outputs = append(outputs, out)
			}
			start = end
		}
	}
	for i := range outputs {
		s := st.styles[styleOf[outputs[i].Runes.Offset]]
		if s.word != 0 {
			outputs[i].AddWordSpacing(para, s.word)
		}
		if s.letter != 0 {
			outputs[i].AddLetterSpacing(s.letter, i == 0, i == len(outputs)-1)
		}
	}

	st.wrapper.Prepare(shaping.WrapConfig{Direction: dir, BreakPolicy: shaping.WhenNecessary}, para, shaping.NewSliceIterator(outputs))
	// Every line holds at least one grapheme, so a paragraph has at most
	// len(para) lines; the bound only guards against a wrapper that stops
	// advancing.
	for range len(para) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("laying out text: %w", err)
		}
		wrapped, done := st.wrapper.WrapNextLineF(maxWidth)
		if len(wrapped.Line) > 0 {
			line, err := st.line(wrapped.Line, para, styleOf, dir)
			if err != nil {
				return nil, err
			}
			lines = append(lines, line)
		}
		if done {
			break
		}
	}
	return lines, nil
}

// continuesGrapheme reports whether r extends the grapheme before it: a
// combining mark, a joiner or a variation selector (UAX #29, Extend and ZWJ,
// approximated).
func continuesGrapheme(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc) || r == '\u200D' || unicode.Is(unicode.Variation_Selector, r)
}

// wrapPieces returns the ends of the pieces para is wrapped in: the whole
// paragraph, unless a word, the text between two UAX #14 break
// opportunities, runs past [maxWordRunes] characters. Such a word is cut
// every [maxWordRunes] characters, before a character that starts a
// grapheme.
func (st *layoutState) wrapPieces(para []rune) []int {
	var ends []int
	st.lineSeg.Init(para)
	it := st.lineSeg.LineIterator()
	for it.Next() {
		line := it.Line()
		pos, end := line.Offset, line.Offset+len(line.Text)
		for end-pos > maxWordRunes {
			cut := pos + maxWordRunes
			for cut > pos+1 && continuesGrapheme(para[cut]) {
				cut--
			}
			ends = append(ends, cut)
			pos = cut
		}
	}
	return append(ends, len(para))
}

// rescale corrects the glyph metrics of out for go-text shaping at the
// whole number of units above size: positions scale linearly with the size,
// since faces shape unhinted.
func rescale(out *shaping.Output, size fixed.Int26_6) {
	shaped := size.Ceil()
	if shaped <= 0 || fixed.I(shaped) == size {
		return
	}
	k := float64(size) / float64(fixed.I(shaped))
	sc := func(v *fixed.Int26_6) { *v = fixed.Int26_6(math.Round(float64(*v) * k)) }
	for i := range out.Glyphs {
		g := &out.Glyphs[i]
		sc(&g.Width)
		sc(&g.Height)
		sc(&g.XBearing)
		sc(&g.YBearing)
		sc(&g.Advance)
		sc(&g.XOffset)
		sc(&g.YOffset)
	}
	sc(&out.LineBounds.Ascent)
	sc(&out.LineBounds.Descent)
	sc(&out.LineBounds.Gap)
	out.RecalculateAll()
}

// collectMissing records the characters out draws as .notdef because its
// face lacks them.
func (st *layoutState) collectMissing(out *shaping.Output, para []rune) {
	fc := st.fonts.byFont[out.Face.Font]
	for _, g := range out.Glyphs {
		if g.GlyphID != 0 {
			continue
		}
		start, end := clusterRange(g, len(para))
		for _, r := range para[start:end] {
			if harfbuzz.IsDefaultIgnorable(r) || (fc != nil && covers(fc, r)) {
				continue
			}
			if _, ok := st.missing[r]; !ok && len(st.missing) < maxMissingGlyphs {
				st.missing[r] = struct{}{}
			}
		}
	}
}

// clusterRange returns the rune range of g's cluster, clamped to n runes.
func clusterRange(g shaping.Glyph, n int) (int, int) {
	start := min(max(g.TextIndex(), 0), n)
	end := min(max(start+g.RunesCount(), start), n)
	return start, end
}

// line converts one wrapped line to a [Line].
func (st *layoutState) line(runs shaping.Line, para []rune, styleOf []int, dir di.Direction) (Line, error) {
	hangTrailingSpaces(runs, para, dir)
	order := make([]int, len(runs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return int(runs[a].VisualIndex) - int(runs[b].VisualIndex) })

	var line Line
	x := 0.0
	for _, i := range order {
		out := &runs[i]
		line.Ascent = max(line.Ascent, st.toPoints(out.LineBounds.Ascent))
		line.Descent = max(line.Descent, -st.toPoints(out.LineBounds.Descent))
		line.LineGap = max(line.LineGap, st.toPoints(out.LineBounds.Gap))
		if len(out.Glyphs) == 0 {
			continue
		}
		fc := st.fonts.byFont[out.Face.Font]
		cf, err := fc.Compose()
		if err != nil {
			return Line{}, fmt.Errorf("laying out text: %w", err)
		}
		s := st.styles[styleOf[out.Runes.Offset]].style
		t := &compose.Text{
			Face:   cf,
			Size:   s.Size,
			Color:  s.Color,
			Origin: compose.Point{X: x},
			Glyphs: make([]compose.Glyph, len(out.Glyphs)),
		}
		pen := 0.0
		cluster := -1
		for k, g := range out.Glyphs {
			cg := compose.Glyph{
				GID: glyphID(g.GlyphID),
				X:   pen + st.toPoints(g.XOffset),
				Y:   st.toPoints(-g.YOffset),
			}
			if c := g.TextIndex(); c != cluster {
				cluster = c
				start, end := clusterRange(g, len(para))
				cg.Text = string(para[start:end])
			}
			t.Glyphs[k] = cg
			pen += st.toPoints(g.Advance)
		}
		line.Runs = append(line.Runs, t)
		x += pen
	}
	line.Width = x
	return line, nil
}

// glyphID narrows a go-text glyph index to an sfnt one. sfnt glyph indices
// are 16-bit (ISO/IEC 14496-22, maxp numGlyphs); anything wider is no glyph
// of the face and draws .notdef.
func glyphID(g font.GID) uint16 {
	if g > math.MaxUint16 {
		return 0
	}
	return uint16(g)
}

// hangTrailingSpaces zeroes the advance of every space at the logical end of
// a line, so trailing spaces hang (CSS Text 3, 4.1.3). go-text's wrapper
// trims the last one only.
func hangTrailingSpaces(runs shaping.Line, para []rune, dir di.Direction) {
	for i := len(runs) - 1; i >= 0; i-- {
		out := &runs[i]
		if out.Direction.Progression() != dir.Progression() {
			return
		}
		n := len(out.Glyphs)
		stopped := false
		for k := range n {
			gi := n - 1 - k
			if out.Direction.Progression() == di.TowardTopLeft {
				gi = k
			}
			g := &out.Glyphs[gi]
			start, end := clusterRange(*g, len(para))
			if start == end || !allHanging(para[start:end]) {
				stopped = true
				break
			}
			g.Advance = 0
		}
		out.RecomputeAdvance()
		if stopped {
			return
		}
	}
}

// allHanging reports whether every rune of rs is a space that hangs at the
// end of a line: a space separator other than the no-break spaces.
func allHanging(rs []rune) bool {
	for _, r := range rs {
		switch r {
		case '\u00A0', '\u2007', '\u202F':
			return false
		}
		if !unicode.Is(unicode.Zs, r) {
			return false
		}
	}
	return true
}

// report returns the call's [Report].
func (st *layoutState) report() Report {
	if len(st.missing) == 0 {
		return Report{}
	}
	return Report{MissingGlyphs: slices.Sorted(maps.Keys(st.missing))}
}
