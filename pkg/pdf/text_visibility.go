package pdf

import (
	"context"
	"errors"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// glyphKey names a glyph of a page by where text extraction places it, its
// origin and painted advance in hundredths of a point: the same glyph read
// twice, by two extractions of the page, has one key whatever each reads it
// as.
type glyphKey [3]int64

// glyphKeyScale is the resolution of a [glyphKey], in units a point.
const glyphKeyScale = 100

// keyOf returns the key of a glyph.
func keyOf(el models.TextElement) glyphKey {
	return glyphKey{
		int64(math.Round(el.X * glyphKeyScale)),
		int64(math.Round(el.Y * glyphKeyScale)),
		int64(math.Round(el.Width * glyphKeyScale)),
	}
}

// whiteVerdicts holds, for the glyphs of a page that text extraction judges
// invisible by their white fill alone, whether a rendering shows them. A
// glyph it holds no verdict for keeps the extraction's judgement.
type whiteVerdicts map[glyphKey]bool

// whiteVerdictBytes estimates what one verdict retains in a map: its key, its
// value and the map's own overhead. It sizes the entry for
// [document.Document.SetCachedTextVisibility].
const whiteVerdictBytes = 48

// revealWhiteText clears Invisible on each glyph of pg that text extraction
// judged invisible by its white fill alone and that a rendering of the page
// shows. whiteFills is parallel to elements ([graphics.Machine.WhiteFills]).
//
// White on white is the only judgement it overturns: a glyph in rendering mode
// 3 paints nothing (ISO 32000-1, 9.3.6, Table 106), so an OCR layer stays
// invisible without a rendering, and so does a glyph the page renders nothing
// for. A glyph it cannot judge, the page failing to render or the glyph
// sharing every pixel with another, keeps its judgement. It returns an error
// only when ctx ends.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu: the default reading gains 132138 characters on
//     773 pages of 134 files, white headings on a coloured bar mostly: govdocs
//     thread_000_000152.pdf's "TABLE OF CONTENTS" on navy, pdfjs
//     bug1734802.pdf's "Important Safety Information". 98.9% of the words it
//     gains are words pdftotext reads on the page; the rest are words the two
//     segment apart. litho, facturx and pdf20 read as before.
//   - govdocs thread_000_000325.pdf, thread_300_300113.pdf and three more: nine
//     pages whose only text is white on a scan no longer report
//     [ErrRequiresOCR].
//   - pdfjs issue9462.pdf: "KEY ATTRIBUTES", white over a band litho draws
//     and poppler leaves out of a damaged stream, stays out, nothing the
//     extraction reads being painted under it.
func (r *Reader) revealWhiteText(ctx context.Context, pg *document.Page, elements []models.TextElement, whiteFills []bool) error {
	if !typography.HasWhiteFillInvisible(elements, whiteFills) {
		return nil
	}
	verdicts, err := r.whiteVerdicts(ctx, pg)
	if err != nil {
		return err
	}
	for i := range elements {
		if elements[i].Invisible && whiteFills[i] && verdicts[keyOf(elements[i])] {
			elements[i].Invisible = false
		}
	}
	return nil
}

// whiteVerdicts returns the verdicts of pg's white glyphs, cached per page in
// the document ([document.Document.CachedTextVisibility]). A page that cannot
// be judged gets no verdict, logged at Debug, and keeps every judgement.
func (r *Reader) whiteVerdicts(ctx context.Context, pg *document.Page) (whiteVerdicts, error) {
	objNum := pg.Ref.ObjectNum
	if objNum > 0 {
		if v, ok := r.doc.CachedTextVisibility(objNum); ok {
			if verdicts, ok := v.(whiteVerdicts); ok {
				return verdicts, nil
			}
		}
	}
	verdicts, err := r.judgeWhiteText(ctx, pg)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		r.logger().Debug("text visibility: page left unjudged, white text keeps its judgement",
			slog.Int("page_object", objNum), slog.Any("err", err))
		verdicts = whiteVerdicts{}
	}
	if objNum > 0 {
		r.doc.SetCachedTextVisibility(objNum, verdicts, int64(len(verdicts)+1)*whiteVerdictBytes)
	}
	return verdicts, nil
}

// judgeWhiteText judges each run of pg's glyphs invisible by their white fill
// alone. A run nothing else paints under stays invisible without a drawing;
// otherwise pg is drawn with and without its text, and a run shows when its
// text marks a pixel no other glyph's box covers.
func (r *Reader) judgeWhiteText(ctx context.Context, pg *document.Page) (whiteVerdicts, error) {
	s, err := r.readScene(ctx, pg)
	if err != nil {
		return nil, err
	}
	verdicts := whiteVerdicts{}
	var runs [][]int
	var region [4]float64
	for _, run := range sceneRuns(s.elements, func(i int) bool { return s.elements[i].Invisible && s.whiteFills[i] }) {
		if !s.paintedUnder(run) {
			continue
		}
		runs = append(runs, run)
		for _, i := range run {
			region = unionBox(region, s.elements[i].InkBox)
		}
	}
	if len(runs) == 0 {
		return verdicts, nil
	}
	if err := s.draw(ctx, r, pg, region, false); err != nil {
		if errors.Is(err, errSceneOutsidePage) {
			return verdicts, nil
		}
		return nil, err
	}
	for _, run := range runs {
		st := s.stats(run)
		if st.area == 0 {
			continue
		}
		for _, i := range run {
			verdicts[keyOf(s.elements[i])] = st.changed > 0
		}
	}
	return verdicts, nil
}
