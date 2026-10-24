package pdf

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

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
//   - [models.UnseenRenderMode], text in rendering mode 3 or 7, which paints no
//     glyph (ISO 32000-1, 9.3.6, Table 106), as an OCR layer does;
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
// The page is drawn three times at 72 dpi, with and without its text and with
// its text alone, every optional content group shown as text extraction reads
// them, its annotations drawn as a viewer shows them. A run of glyphs is
// judged by the pixels its glyph boxes cover and no other glyph's does, so a
// run overlapping other text everywhere it lies is not reported, nor is one
// set vertically or turned that text extraction cannot place.
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
	var out []models.UnseenText
	var drawn bool
	for _, run := range unseenRuns(s) {
		first := s.elements[run[0]]
		var reason models.UnseenReason
		switch {
		case invisibleMode(s.paints[run[0]].Mode):
			reason = models.UnseenRenderMode
		case first.IsClipped:
			reason = models.UnseenOutsidePage
		default:
			if !drawn {
				if err := s.draw(ctx, r, pg, [4]float64{}, true); err != nil {
					return nil, sceneError(pageNum, err)
				}
				drawn = true
			}
			st := s.stats(run)
			switch {
			case st.area == 0, st.changed > 0:
				continue
			case st.alone == 0:
				reason = models.UnseenPaintsNothing
			default:
				reason = models.UnseenCovered
			}
		}
		out = append(out, unseenOf(s, run, reason))
	}
	return out, nil
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

// unseenRuns groups a scene's glyphs that read as more than white space into
// runs judged as one: consecutive glyphs on one line drawn the same way, in a
// mode that paints or not, inside the crop box or not.
func unseenRuns(s *textScene) [][]int {
	class := func(i int) [2]bool {
		return [2]bool{invisibleMode(s.paints[i].Mode), s.elements[i].IsClipped}
	}
	var runs [][]int
	for _, run := range sceneRuns(s.elements, func(i int) bool { return strings.TrimSpace(s.elements[i].Text) != "" }) {
		start := 0
		for k := 1; k <= len(run); k++ {
			if k == len(run) || class(run[k]) != class(run[start]) {
				runs = append(runs, run[start:k])
				start = k
			}
		}
	}
	return runs
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
