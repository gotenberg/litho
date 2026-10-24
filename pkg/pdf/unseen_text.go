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
// drawn without it alone, up to 16 such runs. A run past that bound
// is not reported, nor is one set vertically or turned that text extraction
// cannot place.
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
	reasons := make([]models.UnseenReason, len(runs))
	var drawn []int
	for k, run := range runs {
		switch {
		case s.paints[run[0]].Mode == 3:
			reasons[k] = models.UnseenRenderMode
		case s.elements[run[0]].IsClipped:
			reasons[k] = models.UnseenOutsidePage
		default:
			drawn = append(drawn, k)
		}
	}
	if err := s.judgeUnseen(ctx, r, pg, runs, drawn, reasons); err != nil {
		return nil, sceneError(pageNum, err)
	}
	var out []models.UnseenText
	for k, run := range runs {
		if reasons[k] != "" {
			out = append(out, unseenOf(s, run, reasons[k]))
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
// shows it, and one that lets nothing through is reported for its mode.
func (s *textScene) judgeUnseen(ctx context.Context, r *Reader, pg *document.Page, runs [][]int, drawn []int, reasons []models.UnseenReason) error {
	pick := func(idx []int) [][]int {
		out := make([][]int, len(idx))
		for j, k := range idx {
			out[j] = runs[k]
		}
		return out
	}
	if len(drawn) == 0 {
		return nil
	}
	verdicts, err := s.judgeOwnInk(ctx, r, pg, [4]float64{}, pick(drawn), ownInkOptions{omitClip: true})
	if err != nil {
		return err
	}
	var unseen []int
	for j, k := range drawn {
		switch {
		case verdicts[j] != inkUnseen:
		case s.paints[runs[k][0]].Mode == 7:
			reasons[k] = models.UnseenRenderMode
		default:
			unseen = append(unseen, k)
		}
	}
	if len(unseen) == 0 {
		return nil
	}
	// Clips stay in force with what they clip left out (the renderer keeps a
	// path's or a text object's clip, ISO 32000-1, 8.5.4 and 9.3.6), so text
	// a clip removes paints nothing alone either.
	alone, err := s.judgeOwnInk(ctx, r, pg, [4]float64{}, pick(unseen),
		ownInkOptions{omit: page.Paths | page.Images | page.Shadings | page.Annotations})
	if err != nil {
		return err
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
	return nil
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

// unseenOf builds the report of one run.
func unseenOf(s *textScene, run []int, reason models.UnseenReason) models.UnseenText {
	var text strings.Builder
	var box [4]float64
	for _, i := range run {
		text.WriteString(s.elements[i].Text)
		box = unionBox(box, s.elements[i].InkBox)
	}
	return models.UnseenText{
		Text:     text.String(),
		BBox:     box,
		FontSize: math.Abs(s.elements[run[0]].FontSize),
		Reason:   reason,
	}
}
