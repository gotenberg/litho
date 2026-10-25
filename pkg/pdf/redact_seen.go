package pdf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// seenCopyBoxSlack is how far, in points, a glyph's ink box may reach past
// the box of the run it belongs to and still count among the run's glyphs.
// Not spec-mandated: the run's box is the union of the same glyphs' boxes,
// read by a second extraction of the written page, so the two agree to the
// rounding of the output's numbers.
const seenCopyBoxSlack = 0.5

// unseenSamePageCopies returns the copies of copies, each a
// [RedactReport.CopiesOnRedactedPages] entry, that no rendering of after
// shows: a run a later opaque fill, an image, other text or anything else
// paints over, or one drawn in the colour of what lies under it. Such a copy
// is a hidden duplicate of the removed text, not the page's other text, so
// [Writer.Redact] refuses over it.
//
// Each redacted page that holds a copy with boxes is judged as text
// extraction judges white text (see [Reader.judgeWhiteText]), by
// [textScene.showsOwnInk] over the union of the copies' boxes alone: a copy
// is seen when the page drawn without its glyphs differs from the page drawn
// whole inside their boxes, its own ink marking the page there whatever other
// glyph's box also lies there (ISO 32000-1, 8.2: later marks paint over
// earlier ones, so a copy painted over changes nothing). A copy with no boxes
// is spelt only by numbers no text is drawn with and needs no drawing. A page
// that cannot be drawn, or a copy whose glyphs cannot be found, counts as
// unseen: refusing is the answer that leaks nothing.
//
// # Corpus evidence
//
//   - pdfjs issue13971.pdf, the centred quarter: the copy of the removed
//     "tück" drawn plainly below the region, in "Stück", was refused as unseen
//     while the boxes of the large "99" beside it held every pixel it marks;
//     judged by its own ink it shows, and the redaction succeeds reporting it.
func unseenSamePageCopies(ctx context.Context, after *Reader, copies []RedactionHit) ([]RedactionHit, error) {
	byPage := make(map[int][]int)
	for i, c := range copies {
		if len(c.Boxes) > 0 && len(c.Pages) > 0 {
			byPage[c.Pages[0]] = append(byPage[c.Pages[0]], i)
		}
	}
	if len(byPage) == 0 {
		return nil, nil
	}
	pages, err := after.resolvePages(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		after.logger().Debug("redact: the written pages do not resolve, copies on them count as unseen", slog.Any("err", err))
		return slices.Clone(copies), nil
	}
	var unseen []RedactionHit
	for _, pageNum := range slices.Sorted(maps.Keys(byPage)) {
		idx := byPage[pageNum]
		seen, err := seenOnPage(ctx, after, pages, pageNum, copies, idx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			after.logger().Debug("redact: page left undrawn, its copies count as unseen",
				slog.Int("page", pageNum), slog.Any("err", err))
		}
		for k, i := range idx {
			if err != nil || !seen[k] {
				unseen = append(unseen, copies[i])
			}
		}
	}
	return unseen, nil
}

// seenOnPage draws page pageNum and reports, parallel to idx, whether each
// copy it names shows (see [unseenSamePageCopies]).
func seenOnPage(ctx context.Context, after *Reader, pages []*document.Page, pageNum int, copies []RedactionHit, idx []int) ([]bool, error) {
	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("page %d of %d", pageNum, len(pages))
	}
	pg := pages[pageNum-1]
	s, err := after.readScene(ctx, pg)
	if err != nil {
		return nil, err
	}
	var region [4]float64
	runs := make([][]int, len(idx))
	for k, i := range idx {
		for _, box := range copies[i].Boxes {
			region = unionBox(region, box)
		}
		var within []int
		for e, el := range s.elements {
			if el.InkBox == [4]float64{} {
				continue
			}
			for _, box := range copies[i].Boxes {
				if boxWithin(el.InkBox, box, seenCopyBoxSlack) {
					within = append(within, e)
					break
				}
			}
		}
		runs[k] = spelling(s, within, copies[i].Token)
	}
	return s.showsOwnInk(ctx, after, pg, region, runs)
}

// spelling returns the glyphs of within, glyphs of s in the order the content
// shows them, that spell token: each stretch of consecutive glyphs whose
// characters, white space aside, are the token's. A glyph drawn inside the
// copy's boxes that spells something else, another word set over the copy or
// at the origin of one of its glyphs, is no part of it, and judging the copy
// by its ink would let that glyph's ink show for the copy's. When no stretch
// spells the token, a copy extraction reads differently from how the
// content shows it, every glyph inside the boxes stands for the copy.
func spelling(s *textScene, within []int, token string) []int {
	want := strings.Join(strings.Fields(token), "")
	if want == "" {
		return within
	}
	var out []int
	taken := make(map[int]bool)
	for start := 0; start < len(within); start++ {
		if strings.TrimSpace(s.elements[within[start]].Text) == "" {
			continue
		}
		var got strings.Builder
		for end := start; end < len(within); end++ {
			got.WriteString(strings.Join(strings.Fields(s.elements[within[end]].Text), ""))
			if !strings.HasPrefix(want, got.String()) {
				break
			}
			if got.Len() == len(want) {
				for _, e := range within[start : end+1] {
					if !taken[e] {
						taken[e] = true
						out = append(out, e)
					}
				}
				start = end
				break
			}
		}
	}
	if len(out) == 0 {
		return within
	}
	return out
}

// boxWithin reports inner lying inside outer grown by slack on every side,
// both [minX minY maxX maxY].
func boxWithin(inner, outer [4]float64, slack float64) bool {
	return inner[0] >= outer[0]-slack && inner[1] >= outer[1]-slack &&
		inner[2] <= outer[2]+slack && inner[3] <= outer[3]+slack
}

// seenCopyKey names a copy [Writer.Redact] judged seen: its page, its token
// and its boxes rounded to whole points, so [Writer.WriteTo] can tolerate
// the same copy drawn again at the same place and no other.
func seenCopyKey(page int, token string, boxes [][4]float64) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(page))
	b.WriteByte(0)
	b.WriteString(token)
	for _, box := range boxes {
		for _, v := range box {
			b.WriteByte(0)
			b.WriteString(strconv.FormatInt(int64(math.Round(v)), 10))
		}
	}
	return b.String()
}
