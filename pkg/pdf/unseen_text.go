package pdf

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// invisibleMode reports a text rendering mode that neither fills nor strokes
// the glyphs: 3, and 7, which only adds them to the clip (ISO 32000-1, 9.3.6,
// Table 106).
func invisibleMode(mode int) bool {
	return mode == 3 || mode == 7
}

// ExtractUnseenText returns the runs of text the given 1-based page draws
// that show nowhere on it, each with a [models.UnseenReason]:
//
//   - [models.UnseenRenderMode], text in rendering mode 3, which paints no
//     glyph (ISO 32000-1, 9.3.6, Table 106), as an OCR layer does, and text
//     in mode 7, which only adds its glyphs to the clip, when what the page
//     paints through that clip shows nothing inside them;
//   - [models.UnseenOutsidePage], text set outside the crop box;
//   - [models.UnseenPaintsNothing], text that marks no pixel even drawn alone
//     on a white page: glyphs describing no shape, text a clipping path
//     removes (8.5.4), fully transparent text, text in white;
//   - [models.UnseenCovered], text that marks pixels drawn alone and none on
//     the page: what the page paints after it covers it (8.2), or it is drawn
//     in the colour of what lies under it, white on white among them.
//
// Text extraction returns covered text and text that paints nothing: what a
// page shows and what its content stream says are two readings, and this
// method tells them apart, for an audit of text a reader is given and a
// sighted reader never sees, a redaction drawn as a box over text among it.
// [Reader.ExtractText] drops the first two kinds and white on white by
// default; [WithHiddenText] keeps them.
//
// A run of glyphs shows by its own ink, as [Reader.ExtractText] judges white
// text and [Writer.Redact] the copies of removed text: the page drawn
// without the run differs from the page drawn whole inside the run's glyph
// boxes, whatever other glyph's box also lies there. The page is drawn at
// 72 dpi over the glyph boxes, with and without its text, every optional
// content group shown as text extraction reads them, its annotations drawn
// as a viewer shows them, and again with its text alone for the runs that do
// not show; a run whose ink lies wholly under other runs' glyph boxes is
// drawn again without it, beside other such runs that share no pixel with
// it, in up to 16 drawings. A run past that bound is not reported, nor is
// one set vertically or turned that text extraction cannot place. A piece
// of text drawn several times over itself, a fake bold, a drop shadow or an
// outline pass, each copy the same characters at the same size under a
// quarter em from the others as default text extraction matches them, is
// judged once, as a viewer sees it: it shows when the page drawn without
// every copy differs, and a copy under another is not reported for lying
// under its own copy. A run covered or painting nothing that is drawn again
// later at its place in mode 7, by a copy that shows through its clip, is
// seen in that copy, as a heading clipping an image to its letters shows, and
// is not reported. A run in mode 7 whose clip lets nothing through is not
// reported either when a copy of it at its place, drawn before or after it in
// a mode that paints, shows: a reader sees the text in that copy. A run in
// mode 7 that clips nothing and has no such copy is reported for its mode.
//
// It returns [ErrInvalidPageNumber] for a page outside the document,
// [ErrFontMissing] when the page's text cannot be read, and
// [ErrRenderTooLarge] when the page cannot be drawn within
// [Limits.MaxRenderPixels] or its glyph boxes cover the page past 64 times.
// It is safe for concurrent use with every other read of the [Reader].
func (r *Reader) ExtractUnseenText(ctx context.Context, pageNum int) (_ []models.UnseenText, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractUnseenText", &err)
	pg, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	s, err := r.readScene(ctx, pg)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return nil, fmt.Errorf("page %d: %w", pageNum, ErrFontMissing)
		}
		return nil, fmt.Errorf("page %d: %w", pageNum, err)
	}
	runs := unseenRuns(s)
	isDrawn := func(k int) bool {
		return s.paints[runs[k][0]].Mode != 3 && !s.elements[runs[k][0]].IsClipped
	}
	// Copies drawn in mode 7 show through what the page paints after them,
	// the others by what they paint, so the two never join one stack.
	stacks, err := s.stackRuns(ctx, runs, func(a, b int) bool {
		return isDrawn(a) && isDrawn(b) && (s.paints[runs[a][0]].Mode == 7) == (s.paints[runs[b][0]].Mode == 7)
	})
	if err != nil {
		return nil, fmt.Errorf("page %d: %w", pageNum, err)
	}
	units := stackGlyphs(stacks)
	reasons := make([]models.UnseenReason, len(units))
	var drawn []int
	for n, unit := range units {
		switch {
		case s.paints[unit[0]].Mode == 3:
			reasons[n] = models.UnseenRenderMode
		case s.elements[unit[0]].IsClipped:
			reasons[n] = models.UnseenOutsidePage
		default:
			drawn = append(drawn, n)
		}
	}
	seen, err := s.judgeUnseen(ctx, r, pg, units, drawn, reasons)
	if err != nil {
		return nil, sceneError(pageNum, err)
	}
	s.seenThroughClipCopies(units, seen, reasons)
	s.seenInPaintedCopies(units, seen, reasons)
	var out []models.UnseenText
	for n, stack := range stacks {
		if reasons[n] != "" {
			out = append(out, unseenOf(s, runs[stack.members[0]], stack.glyphs, reasons[n]))
		}
	}
	return out, nil
}

// judgeUnseen sets, in reasons, the reason each run drawn names shows
// nowhere, leaving the runs that show, and those the judgement cannot
// settle, without one. A run shows when its own ink marks the page as a
// viewer shows it ([textScene.judgeOwnInk]); a run that does not is drawn
// again with the page's text alone, and is covered when its own ink marks
// that drawing, painting nothing otherwise.
//
// A glyph in a clipping mode, 4 to 7, also shows by what the page paints
// through its clip after it (ISO 32000-1, 9.3.6, Table 106), so the first
// drawing leaves the runs out of the clip too ([ownInkOptions.omitClip]): a
// run in mode 7 a later fill or image shows through is seen, as a viewer
// shows it, and one that lets nothing through is reported for its mode. A
// text object whose clipping glyphs are all left out still clips, to nothing
// ([page.Options.OmitGlyphClip]), so leaving a run out never lets through
// paint its clip kept from other runs' glyph boxes.
//
// It returns, parallel to runs, the runs judged seen by their own ink.
//
// # Corpus evidence
//
//   - govdocs thread_000_000149.pdf pages 9 and 10: mode 7 copies clip a
//     0.37-point sliver, and were judged seen while leaving them out of the
//     clip let the sliver paint their whole boxes; now judged for their
//     mode, and left unreported for their filled copies that show
//     ([textScene.seenInPaintedCopies]).
//   - pdfjs recursiveCompositGlyf.pdf: mode 7 glyphs with no outline clip a
//     red page to nothing in litho's drawing; now reported for their mode.
func (s *textScene) judgeUnseen(ctx context.Context, r *Reader, pg *document.Page, runs [][]int, drawn []int, reasons []models.UnseenReason) ([]bool, error) {
	pick := func(idx []int) [][]int {
		out := make([][]int, len(idx))
		for j, k := range idx {
			out[j] = runs[k]
		}
		return out
	}
	seen := make([]bool, len(runs))
	if len(drawn) == 0 {
		return seen, nil
	}
	verdicts, err := s.judgeOwnInk(ctx, r, pg, [4]float64{}, pick(drawn), ownInkOptions{omitClip: true})
	if err != nil {
		return nil, err
	}
	var unseen []int
	for j, k := range drawn {
		switch {
		case verdicts[j] == inkSeen:
			seen[k] = true
		case verdicts[j] != inkUnseen:
		case s.paints[runs[k][0]].Mode == 7:
			reasons[k] = models.UnseenRenderMode
		default:
			unseen = append(unseen, k)
		}
	}
	if len(unseen) == 0 {
		return seen, nil
	}
	// Clips stay in force with what they clip left out (the renderer keeps a
	// path's or a text object's clip, ISO 32000-1, 8.5.4 and 9.3.6), so text
	// a clip removes paints nothing alone either.
	alone, err := s.judgeOwnInk(ctx, r, pg, [4]float64{}, pick(unseen),
		ownInkOptions{omit: page.Paths | page.Images | page.Shadings | page.Annotations})
	if err != nil {
		return nil, err
	}
	for j, k := range unseen {
		switch alone[j] {
		case inkSeen:
			reasons[k] = models.UnseenCovered
		case inkUnseen:
			reasons[k] = models.UnseenPaintsNothing
		case inkUnjudged:
			// Unseen on the page, but past the bound drawn alone: no reason
			// stands, and an advisory report claims none.
		}
	}
	return seen, nil
}

// seenThroughClipCopies clears the reason of each unit that is covered or
// paints nothing but is drawn again later at its place by units judged seen
// in rendering mode 7 ([textScene.clipCopied]). A glyph in mode 7 adds its
// outline to the clip (ISO 32000-1, 9.3.6, Table 106), so what the page
// paints after it shows in the glyph's shape: a heading filled, painted over,
// and drawn again to clip an image over it shows its words in the image, and
// a reader sees the text there. The later copy is what shows, so the filled
// copy is not reported for lying under paint its own copy lets through; a
// run under paint that no copy of it clips stays reported.
//
// # Corpus evidence
//
//   - govdocs thread_000_000208.pdf: InDesign fills "Modified for the Web",
//     paints an image over it, then draws it again in mode 7 to clip a second
//     image to its letters, on 38 pages.
func (s *textScene) seenThroughClipCopies(units [][]int, seen []bool, reasons []models.UnseenReason) {
	var hidden, clips [][]int
	var at []int
	for n, unit := range units {
		switch {
		case seen[n] && s.paints[unit[0]].Mode == 7:
			clips = append(clips, unit)
		case reasons[n] == models.UnseenCovered || reasons[n] == models.UnseenPaintsNothing:
			hidden = append(hidden, unit)
			at = append(at, n)
		}
	}
	for j, shown := range s.clipCopied(hidden, clips) {
		if shown {
			reasons[at[j]] = ""
		}
	}
}

// seenInPaintedCopies clears the reason of each unit in rendering mode 7
// reported for its mode, its clip letting nothing through, that units judged
// seen in a mode that fills or strokes draw again at its place, before or
// after it ([textScene.copiedBy]). A glyph in mode 7 paints nothing (ISO
// 32000-1, 9.3.6, Table 106), so its text shows only through what its clip
// lets through or in another copy; a filled copy that shows gives a reader
// the text, as a producer drawing a heading once filled and once to clip
// nothing shows it. A clipping copy with no painted copy that shows stays
// reported, as does one beside a painted copy that is itself covered.
//
// # Corpus evidence
//
//   - govdocs thread_000_000149.pdf pages 9 and 10: the table caption and a
//     reference are filled, shown, and drawn again in mode 7 to clip a
//     0.37-point sliver ([textScene.judgeUnseen]); no longer reported.
func (s *textScene) seenInPaintedCopies(units [][]int, seen []bool, reasons []models.UnseenReason) {
	var clips, painted [][]int
	var at []int
	for n, unit := range units {
		mode := s.paints[unit[0]].Mode
		switch {
		case reasons[n] == models.UnseenRenderMode && mode == 7:
			clips = append(clips, unit)
			at = append(at, n)
		case seen[n] && !invisibleMode(mode):
			painted = append(painted, unit)
		}
	}
	for j, shown := range s.copiedBy(clips, painted, false) {
		if shown {
			reasons[at[j]] = ""
		}
	}
}

// clipCopied reports, parallel to runs, whether each run is drawn again later
// at its place by glyphs of clips ([textScene.copiedBy]).
func (s *textScene) clipCopied(runs, clips [][]int) []bool {
	return s.copiedBy(runs, clips, true)
}

// copiedBy reports, parallel to runs, whether each run is drawn again at its
// place by glyphs of by, after it when later is set: at least
// [stackMinShare] of its characters other than white space are copies
// ([typography.OverdrawCopies], the rule default text extraction matches
// copies by, read into [textScene.copies]) of a glyph of by, or copied by
// one. Two glyphs are copies of each other when they copy one first glyph,
// so a run and its copy match though both copy an earlier one, an OCR layer
// under them.
func (s *textScene) copiedBy(runs, by [][]int, later bool) []bool {
	out := make([]bool, len(runs))
	if s.copies == nil || len(runs) == 0 || len(by) == 0 {
		return out
	}
	root := func(i int) int {
		// A copy names an earlier glyph, which may copy one earlier still.
		for s.copies[i] >= 0 {
			i = s.copies[i]
		}
		return i
	}
	// last[r] is the last glyph of by drawn as first glyph r or a copy of it.
	last := make(map[int]int)
	for _, run := range by {
		for _, j := range run {
			r := root(j)
			if prev, ok := last[r]; !ok || j > prev {
				last[r] = j
			}
		}
	}
	for k, run := range runs {
		shown := 0
		for _, i := range run {
			if j, ok := last[root(i)]; ok && j != i && (!later || j > i) && strings.TrimSpace(s.elements[i].Text) != "" {
				shown++
			}
		}
		out[k] = shown > 0 && float64(shown) >= stackMinShare*float64(inked(s, run))
	}
	return out
}

// sceneError wraps the failure to draw a scene for a page in the public
// error it maps to.
func sceneError(pageNum int, err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, page.ErrTooLarge), errors.Is(err, errSceneTooCostly):
		return fmt.Errorf("page %d: %w: %v", pageNum, ErrRenderTooLarge, err)
	}
	return fmt.Errorf("drawing page %d: %w", pageNum, err)
}

// unseenRuns groups a scene's glyphs into runs judged as one: consecutive
// glyphs on one line drawn the same way, in mode 3, mode 7 or another, inside
// the crop box or not, the white space between words included and runs of
// white space alone left out.
func unseenRuns(s *textScene) [][]int {
	class := func(i int) [3]bool {
		return [3]bool{s.paints[i].Mode == 3, s.paints[i].Mode == 7, s.elements[i].IsClipped}
	}
	var runs [][]int
	for _, run := range sceneRuns(s.elements, func(int) bool { return true }) {
		start := 0
		for k := 1; k <= len(run); k++ {
			if k == len(run) || class(run[k]) != class(run[start]) {
				runs = appendTrimmed(runs, s, run[start:k])
				start = k
			}
		}
	}
	return runs
}

// appendTrimmed appends run to runs without the white space glyphs at its
// ends, and not at all when it holds nothing else: white space shows nothing
// wherever it is set.
func appendTrimmed(runs [][]int, s *textScene, run []int) [][]int {
	blank := func(i int) bool { return strings.TrimSpace(s.elements[i].Text) == "" }
	for len(run) > 0 && blank(run[0]) {
		run = run[1:]
	}
	for len(run) > 0 && blank(run[len(run)-1]) {
		run = run[:len(run)-1]
	}
	if len(run) == 0 {
		return runs
	}
	return append(runs, run)
}

// unseenOf builds the report of one run, named by its first copy run and
// boxed by every glyph of its stack, glyphs ([runStack]). A glyph of run that
// draws a glyph of the stack again ([textScene.copies]) is left out of its
// text, so a word drawn twice in a row for a fake bold is named once, as
// default text extraction reads it.
func unseenOf(s *textScene, run, glyphs []int, reason models.UnseenReason) models.UnseenText {
	var text strings.Builder
	var box [4]float64
	inStack := make(map[int]bool, len(glyphs))
	for _, i := range glyphs {
		inStack[i] = true
		box = unionBox(box, s.elements[i].InkBox)
	}
	for _, i := range run {
		if s.copies != nil && s.copies[i] >= 0 && inStack[s.copies[i]] {
			continue
		}
		text.WriteString(s.elements[i].Text)
	}
	return models.UnseenText{
		Text:     text.String(),
		BBox:     box,
		FontSize: math.Abs(s.elements[run[0]].FontSize),
		Reason:   reason,
	}
}
