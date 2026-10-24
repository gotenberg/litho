package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// samePageCheckInterval is how many show-text operators the visible-copy
// walk tallies between context checks.
const samePageCheckInterval = 100

// SamePageCopy is a surviving copy of a removed string that the redacted
// page itself still draws, as visible text outside every region.
type SamePageCopy struct {
	Hit TokenHit
	// Page is the 1-based page whose content stream holds the copy.
	Page int
	// Boxes are the ink boxes, in default user space, of the show-text
	// operators that spell the copy, in content order. It is empty when the
	// stream spells the string only in the numbers of operators that draw no
	// text (see [numericOperation]).
	Boxes [][4]float64
}

// SplitSamePageCopies separates the hits that a redacted page's own content
// stream holds only as visible text outside the regions from the rest.
//
// A region redaction removes what the region covers (ISO 32000-2, 12.5.6.23:
// a Redact annotation's region, /QuadPoints or /Rect, is what "the content
// that lies within" removes). The same characters drawn again elsewhere on
// the page are text the region never covered: a running head, a repeated
// word. They are not the redacted content. A hit counts as such a copy when
// its object is a content stream the redacted page's /Contents names and,
// once every show-text operator of the page that draws only visible text
// outside the regions, and every operator that takes only numbers, is set
// aside, that stream no longer holds the token in any form the verification
// scan reads (its bytes, its string operands, and its stored tail; see
// [ScanOutputForTokens]).
//
// A show-text operator counts as visible text outside the regions when every
// code it shows produced a glyph, and every glyph:
//
//   - paints with a text rendering mode that fills or strokes (ISO 32000-1,
//     9.3.6, Table 106: not 3, invisible, nor 7, clip only);
//   - is not white in a device colour space ([models.TextElement.Invisible]);
//   - falls inside the clipping path and the crop box
//     ([models.TextElement.Hidden], [models.TextElement.IsClipped]), and is
//     not in an optional-content group that is off (the extraction skips
//     those, so the operator has codes with no glyph);
//   - has an ink box that meets no region of the page.
//
// So a copy that is invisible, clipped, off the page, in a hidden layer, or
// that still meets a region (a copy behind it) keeps its hit, and the
// caller refuses over it. A copy a form XObject, a pattern or an annotation
// draws, and a hit in a stream dictionary's strings, keep theirs too. What
// this cannot see: text painted over by later content, a page's own image or
// an opaque fill, which shows nothing a viewer reads; it is not set aside on
// that account, and is tolerated like any other visible text.
//
// byPage maps each redacted page to its regions. Pages are read as alloc
// stages them, so a redacted page is judged by the content that replaced its
// own.
func SplitSamePageCopies(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	byPage map[int][][4]float64,
	hits []TokenHit,
) ([]SamePageCopy, []TokenHit, error) {
	if len(hits) == 0 {
		return nil, hits, nil
	}
	byObject := make(map[int][]TokenHit)
	for _, hit := range hits {
		if hit.ObjectNum != 0 && hit.Where == "stream" {
			byObject[hit.ObjectNum] = append(byObject[hit.ObjectNum], hit)
		}
	}
	tolerated := make(map[TokenHit]bool)
	var copies []SamePageCopy
	for _, pageNum := range slices.Sorted(maps.Keys(byPage)) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if pageNum < 1 || pageNum > len(pages) {
			continue
		}
		pageCopies, err := samePageCopies(ctx, alloc, doc, pages[pageNum-1], pageNum, byPage[pageNum], byObject)
		if err != nil {
			return nil, nil, err
		}
		for _, c := range pageCopies {
			if !tolerated[c.Hit] {
				tolerated[c.Hit] = true
				copies = append(copies, c)
			}
		}
	}
	var rest []TokenHit
	for _, hit := range hits {
		if !tolerated[hit] {
			rest = append(rest, hit)
		}
	}
	return copies, rest, nil
}

// pageContentStream is one stream of a page's /Contents and where its decoded
// data sits in the page's joined content.
type pageContentStream struct {
	objNum     int
	stream     ast.PdfStream
	start, end int
}

// samePageCopies returns the hits of byObject that page's content holds only
// as visible text outside regions (see [SplitSamePageCopies]).
func samePageCopies(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	pageNum int,
	regions [][4]float64,
	byObject map[int][]TokenHit,
) ([]SamePageCopy, error) {
	page = latestPage(ctx, alloc, doc, page)
	streams, data, err := pageContentStreams(ctx, alloc, doc, page.Dict)
	if err != nil || len(data) == 0 {
		// A page whose content cannot be read again is not proved to hold
		// only visible copies, so its hits stay.
		if err != nil {
			doc.Logger.Debug("redaction same-page copies: page content unreadable, its copies refuse",
				slog.Int("page", pageNum), slog.Any("err", err))
		}
		return nil, nil
	}
	held := false
	for _, s := range streams {
		if len(byObject[s.objNum]) > 0 {
			held = true
			break
		}
	}
	if !held {
		return nil, nil
	}

	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.SplitGluedOperators())
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		doc.Logger.Debug("redaction same-page copies: page content does not parse, its copies refuse",
			slog.Int("page", pageNum), slog.Any("err", err))
		return nil, nil
	}
	machine := graphics.NewMachine(pageBox(ctx, alloc, doc, page))
	machine.RecordGlyphAdvances = true
	machine.Staged = alloc.Pending()
	elements, attr, err := typography.ExtractElementsAttributed(ctx, doc, machine, page, ops)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		doc.Logger.Debug("redaction same-page copies: page text does not extract, its copies refuse",
			slog.Int("page", pageNum), slog.Any("err", err))
		return nil, nil
	}

	visible, boxes, err := visibleOutsideOps(ctx, elements, attr, machine.GlyphAdvances(), regionZones(regions))
	if err != nil {
		return nil, err
	}

	masked := slices.Clone(data)
	blank := func(r content.ByteRange) {
		for i := r.Start; i < r.End && i < len(masked); i++ {
			masked[i] = ' '
		}
	}
	for _, op := range slices.Sorted(maps.Keys(visible)) {
		blank(ranges[op])
	}
	for i, op := range ops {
		if numericOperation(op) {
			blank(ranges[i])
		}
	}

	var copies []SamePageCopy
	for _, s := range streams {
		hits := byObject[s.objNum]
		if len(hits) == 0 {
			continue
		}
		tokens := make([]string, 0, len(hits))
		for _, hit := range hits {
			tokens = append(tokens, hit.Token)
		}
		left, err := streamTokens(ctx, doc, alloc, s, masked[s.start:s.end], tokens)
		if err != nil {
			return nil, err
		}
		if left == nil {
			continue
		}
		for _, hit := range hits {
			if left[hit.Token] {
				continue
			}
			copies = append(copies, SamePageCopy{
				Hit:   hit,
				Page:  pageNum,
				Boxes: spellingBoxes(ctx, doc, alloc, data, ranges, visible, boxes, s, hit.Token),
			})
		}
	}
	return copies, nil
}

// visibleOutsideOps returns the show-text operators of the page whose every
// code drew a visible glyph outside zones (see [SplitSamePageCopies]), each
// with the union of its glyphs' ink boxes.
func visibleOutsideOps(
	ctx context.Context,
	elements []models.TextElement,
	attr typography.Attribution,
	advances map[int][]graphics.GlyphAdvance,
	zones []heuristics.Zone,
) (map[int]bool, map[int][4]float64, error) {
	emitted := make(map[int]int)
	ok := make(map[int]bool)
	boxes := make(map[int][4]float64)
	for i, el := range elements {
		if i%samePageCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		if i >= len(attr.Origins) {
			break
		}
		origin := attr.Origins[i]
		if origin == typography.OriginUnattributable {
			continue
		}
		emitted[origin]++
		if _, seen := ok[origin]; !seen {
			ok[origin] = true
		}
		zone := heuristics.ElementZone(el)
		meets := el.InkBox == [4]float64{}
		for _, z := range zones {
			if heuristics.ZonesOverlap(zone, z) {
				meets = true
				break
			}
		}
		if el.Hidden || el.Invisible || el.IsClipped || meets {
			ok[origin] = false
			continue
		}
		if b, seen := boxes[origin]; seen {
			boxes[origin] = [4]float64{min(b[0], el.InkBox[0]), min(b[1], el.InkBox[1]), max(b[2], el.InkBox[2]), max(b[3], el.InkBox[3])}
		} else {
			boxes[origin] = el.InkBox
		}
	}

	visible := make(map[int]bool)
	for origin, good := range ok {
		if !good {
			continue
		}
		codes := advances[origin]
		shown := 0
		allPaint := true
		for _, g := range codes {
			if g.Bytes == 0 {
				continue
			}
			shown++
			// ISO 32000-1, 9.3.6, Table 106: mode 3 neither fills nor
			// strokes, and mode 7 only adds to the clipping path.
			if !g.Emitted || g.RenderingMode == 3 || g.RenderingMode == 7 {
				allPaint = false
			}
		}
		if !allPaint || shown == 0 || shown != emitted[origin] {
			continue
		}
		visible[origin] = true
	}
	return visible, boxes, nil
}

// numericOperation reports whether op takes only numbers: a path, a matrix, a
// colour or a text state parameter (cm, re, Tm, Td, rg, Tc and their kin).
// Such an operator draws no text and holds none. ISO 32000-1, 9.10.1 reads a
// page's text from the string operands of its show-text operators, so a
// removed run of digits that its numbers happen to spell, a date or a page
// number matching a coordinate, is no copy of it. An operator with no operand
// is not one: an inline image's data follows its ID (8.9.7).
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu, litho, redacting page 1 whole, its centred
//     quarter and a text band: 108 more redactions succeed once these
//     operators are set aside too (634 against 526 with visible text alone),
//     69 of them whole-page, where no text is left outside the region and
//     the removed digits matched only numbers of cm, Tm, re, rg and their
//     kin.
func numericOperation(op content.Operation) bool {
	if len(op.Operands) == 0 {
		return false
	}
	switch op.Operator {
	case "BI", "ID", "EI":
		return false
	}
	for _, operand := range op.Operands {
		switch operand.(type) {
		case ast.PdfInt, ast.PdfReal:
		default:
			return false
		}
	}
	return true
}

// pageContentStreams returns the streams of a page's /Contents, read as alloc
// stages them, and their decoded data joined by newlines (ISO 32000-1, 7.8.2).
func pageContentStreams(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageDict ast.PdfDict) ([]pageContentStream, []byte, error) {
	obj, ok := pageDict["Contents"]
	if !ok {
		return nil, nil, nil
	}
	var items []ast.PdfObject
	resolved, err := resolveAllocFirst(ctx, alloc, doc, obj)
	if err != nil {
		return nil, nil, err
	}
	switch v := resolved.(type) {
	case ast.PdfStream:
		items = []ast.PdfObject{obj}
	case ast.PdfArray:
		items = v
	default:
		return nil, nil, fmt.Errorf("unsupported /Contents type %T", resolved)
	}
	var streams []pageContentStream
	var data []byte
	limit := doc.Limits().MaxPageContentBytes()
	for _, item := range items {
		objNum := 0
		if ref, isRef := item.(ast.PdfIndirectRef); isRef {
			objNum = ref.ObjectNum
		}
		got, err := resolveAllocFirst(ctx, alloc, doc, item)
		if err != nil {
			return nil, nil, err
		}
		stream, isStream := got.(ast.PdfStream)
		if !isStream {
			continue
		}
		decoded, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			return nil, nil, fmt.Errorf("decoding content stream %d: %w", objNum, err)
		}
		if len(data) > 0 {
			data = append(data, '\n')
		}
		start := len(data)
		data = append(data, decoded...)
		if int64(len(data)) > limit {
			return nil, nil, fmt.Errorf("page content past %d bytes", limit)
		}
		streams = append(streams, pageContentStream{objNum: objNum, stream: stream, start: start, end: len(data)})
	}
	return streams, data, nil
}

// streamTokens returns which of tokens the content stream s still holds once
// its data reads as data: its decoded bytes, its string operands, and its
// stored tail, compared as [ScanOutputForTokens] compares them. It returns
// nil when the stream cannot be read in full, which proves nothing.
func streamTokens(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, s pageContentStream, data []byte, tokens []string) (map[string]bool, error) {
	w := newScanWalk(doc, alloc, tokens)
	w.contents[s.objNum] = true
	w.matchBytes(s.objNum, data, "stream")
	w.scanContentStrings(ctx, s.objNum, data)
	w.scanStoredTail(ctx, s.objNum, s.stream, doc.Limits().MaxPageContentBytes())
	if w.err != nil {
		return nil, w.err
	}
	if len(w.unreadable) > 0 {
		return nil, nil
	}
	left := make(map[string]bool, len(tokens))
	for hit := range w.hits {
		left[hit.Token] = true
	}
	return left, nil
}

// spellingBoxes returns the ink boxes of the operators set aside on s whose
// own bytes spell token, in content order.
func spellingBoxes(
	ctx context.Context,
	doc *document.Document,
	alloc *writer.ObjAllocator,
	data []byte,
	ranges []content.ByteRange,
	visible map[int]bool,
	boxes map[int][4]float64,
	s pageContentStream,
	token string,
) [][4]float64 {
	var out [][4]float64
	for _, op := range slices.Sorted(maps.Keys(visible)) {
		r := ranges[op]
		if r.Start < s.start || r.End > s.end {
			continue
		}
		w := newScanWalk(doc, alloc, []string{token})
		w.matchBytes(s.objNum, data[r.Start:r.End], "stream")
		w.scanContentStrings(ctx, s.objNum, data[r.Start:r.End])
		if len(w.hits) > 0 {
			out = append(out, boxes[op])
		}
	}
	return out
}
