package tagging

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// headingSizeRatio is the font-size multiple over body text at or above which a
// text block is treated as a heading. 1.15 (15% larger) reuses the same ratio
// as the bookmark heading heuristic; it is a tuning constant, not spec-mandated.
const headingSizeRatio = 1.15

// maxHeadingLevels caps synthesized heading depth at H6, the deepest standard
// heading role in ISO 32000-1 Table 337.
const maxHeadingLevels = 6

// Result is what [AutoTag] did: the descriptions [Emit] returned when it built
// a structure tree, or the reason it left the document as it was.
type Result struct {
	// Descriptions are Emit's, nil when AutoTag skipped.
	Descriptions []string
	// Skipped is nil when AutoTag built a structure tree. Otherwise it is one
	// of [ErrAlreadyTagged], [ErrMarkedContent], [ErrUnreadableContent] and
	// [ErrNothingToTag], wrapped with the page it concerns when there is one.
	Skipped error
	// Page is the 1-based page Skipped concerns, 0 when it concerns the
	// document.
	Page int
}

// AutoTag classifies each page's text into a logical structure tree and emits
// it through [Emit]. It leaves alone, and says why in [Result.Skipped], an
// already-tagged document (one with a /StructTreeRoot or /MarkInfo /Marked
// true), a document whose content is already marked (any BDC/BMC sequence) or
// cannot be assembled, and one with nothing to classify, leaving such documents
// to other processors. Pages whose content cannot be parsed are skipped. The
// error reports a failure, never a skip.
//
// It tags the document as alloc stages it: every object it reads, the catalog
// it judges tagged or not included, is the one alloc has queued at that number
// when there is one (see [stagedValue]).
//
// AutoTag mutates alloc and is not safe for concurrent use.
func AutoTag(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (Result, error) {
	tagged, err := alreadyTagged(ctx, alloc, doc)
	if err != nil {
		return Result{}, err
	}
	if tagged {
		return Result{Skipped: ErrAlreadyTagged}, nil
	}
	skip, page, err := markedContentPage(ctx, alloc, doc)
	if err != nil {
		return Result{}, err
	}
	if skip != nil {
		return Result{Skipped: fmt.Errorf("page %d: %w", page, skip), Page: page}, nil
	}
	root, err := Classify(ctx, alloc, doc)
	if err != nil {
		return Result{}, err
	}
	// Page furniture alone is no structure: a tree of no element would claim
	// a tagged document with nothing in it.
	if !slices.ContainsFunc(root.Kids, func(k *Element) bool { return k.Pagination == "" }) {
		return Result{Skipped: ErrNothingToTag}, nil
	}
	descs, err := Emit(ctx, alloc, doc, root)
	if err != nil {
		return Result{}, err
	}
	return Result{Descriptions: descs}, nil
}

// markedContentPage returns the first page, 1-based, whose content already
// contains a marked-content operator (BDC or BMC) with [ErrMarkedContent], or
// cannot be assembled with [ErrUnreadableContent], whichever comes first. skip
// is nil when every page is unmarked. Auto-tagging assumes unmarked content:
// re-marking a stream a producer already marked would nest or cross the
// existing sequences and corrupt them (a BDC/EMC mismatch), and content that
// cannot be assembled cannot be shown to be unmarked. A document with
// pre-existing marked content but no [alreadyTagged] structure is left to the
// flat tagging fixers, which skip already-marked pages rather than re-wrapping
// them.
//
// The check is conservative: a false positive (a "BDC" or "BMC" literal inside a
// string) only makes AutoTag defer, never corrupt. It scans the parsed page
// content [pageContentBytes] so it agrees with the bytes Emit would rewrite.
func markedContentPage(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (skip error, page int, err error) {
	pages, err := stagedPages(ctx, alloc, doc)
	if err != nil {
		return nil, 0, err
	}
	for pi := range pages {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		data, err := pageContentBytes(ctx, alloc, doc, pages[pi])
		if err != nil {
			// The reason goes to the caller as a skip, and the error that
			// caused it to the log, since it fails no operation.
			doc.Logger.Debug("tagging: page content cannot be assembled; deferring",
				slog.Int("page", pi+1), slog.Any("err", err))
			return ErrUnreadableContent, pi + 1, nil
		}
		if containsMarkedContentOp(data) {
			return ErrMarkedContent, pi + 1, nil
		}
	}
	return nil, 0, nil
}

// containsMarkedContentOp reports whether data contains a BDC or BMC token,
// the operators that begin a marked-content sequence (ISO 32000-1, 14.6). It
// mirrors the byte scan in the content-tag remediation fixer.
func containsMarkedContentOp(data []byte) bool {
	for i := 0; i+2 < len(data); i++ {
		if data[i] == 'B' && ((data[i+1] == 'D' && data[i+2] == 'C') || (data[i+1] == 'M' && data[i+2] == 'C')) {
			return true
		}
	}
	return false
}

// catalogRefForTagging returns the object the reader, the validator and the
// writer all treat as the catalog: [document.Document.CatalogRef], which honours
// the trailer /Root when it resolves to a catalog and recovers by scanning when
// it does not.
//
// Per ISO 32000-1, 7.5.5, Table 15 /Root shall be the catalog, so a file whose
// damaged incremental update points it elsewhere is malformed. Reading the raw
// /Root regardless made tagging address a non-catalog: when the final trailer
// names the /Info dictionary, [alreadyTagged] inspected /Info, found no
// structure and reported an untagged document, and wireCatalog wrote
// /StructTreeRoot and /MarkInfo into /Info while the real catalog kept
// neither. The false "untagged" verdict is the more dangerous half: on a file
// whose real catalog is tagged, it lets AutoTag overwrite the existing tree,
// which is precisely what alreadyTagged exists to prevent.
//
// The raw trailer ref remains the fallback: when recovery finds no catalog at
// all, a reference the file at least names beats no reference.
//
// # Corpus evidence
//
//   - pdfjs issue9418.pdf: its final trailer names the /Info dictionary, and
//     reading the raw /Root made tagging inspect and write into /Info while
//     the real catalog kept neither /StructTreeRoot nor /MarkInfo.
func catalogRefForTagging(ctx context.Context, doc *document.Document) (ast.PdfIndirectRef, bool) {
	if ref, ok := doc.CatalogRef(ctx); ok {
		return ref, true
	}
	if ref, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef); ok {
		return ref, true
	}
	return ast.PdfIndirectRef{}, false
}

// alreadyTagged reports whether the document already declares a tagged
// structure, in which case AutoTag must not overwrite it. It reads the catalog
// alloc stages, which holds the structure an earlier stage built.
func alreadyTagged(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (bool, error) {
	catalogRef, ok := catalogRefForTagging(ctx, doc)
	if !ok {
		return false, nil
	}
	catalogDict, err := stagedDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return false, fmt.Errorf("tagging: resolving catalog: %w", err)
	}
	if _, ok := catalogDict["StructTreeRoot"]; ok {
		return true, nil
	}
	if mi, err := stagedDict(ctx, alloc, doc, catalogDict["MarkInfo"]); err == nil {
		if mi["Marked"] == ast.PdfBool(true) {
			return true, nil
		}
	}
	return false, nil
}

// Classify analyzes the document's text and images and returns a logical
// structure tree ready for [Emit]: a Document whose children are headings
// (H1..Hn), paragraphs (P), lists (L > LI > LBody), tables (Table > TR > TD)
// and figures (Figure), plus the page furniture leaves (see
// [Element.Pagination]) Emit marks as pagination artifacts. It does not mutate
// the document or alloc.
//
// It reads the pages as alloc stages them (see [stagedValue]), a nil alloc
// reading the document alone, and [Emit] must be given the same alloc, so the
// spans index the content Emit marks.
//
// Scope: text in one column or several. Text objects are segmented per
// BT…ET. Running headers, footers and page numbers are set apart first (see
// [detectFurniture]). Strongly grid-like runs of blocks become tables
// ([carveTables]). The rest are classified as headings, by font size relative
// to body text and numbered into a no-skip H1..Hn hierarchy, or paragraphs;
// adjacent paragraph lines merge into one element by baseline proximity, so a
// wrapped paragraph is a single element, and runs of marker-led paragraphs
// become lists ([listify]). Each image paint outside a text object is a Figure
// ([figureElements]). The elements follow the page's reading order
// ([classifyInReadingOrder]): columns read one after the other and the page
// top to bottom, wherever the content stream draws them; a page whose layout
// the segmentation cannot read keeps content order.
//
// Font size is transform-aware: it is the Tf size scaled by the text matrix and
// the CTM (ISO 32000-1, 9.4.4), so text enlarged by a cm or by Tm's own scale
// reads as the heading it looks like. See textVScale. Baselines deliberately
// stay in text space; the reason is on [textBlock].
//
// Known limitations: text inside Form XObjects (Do) is not descended into for
// classification; a text object mixing sizes is classified by its dominant
// size; tables carry no TH and lists no Lbl; and a paragraph whose lines the
// content stream sets apart, around another column's, stays one paragraph per
// text object. Content not placed in a structure element
// (graphics, and text the heuristic misses) is marked as an artifact by [Emit]
// so the output stays fully marked.
func Classify(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (*Element, error) {
	pages, err := stagedPages(ctx, alloc, doc)
	if err != nil {
		return nil, err
	}

	furniture, err := detectFurniture(ctx, alloc, doc, pages)
	if err != nil {
		return nil, err
	}

	root := &Element{Role: RoleDocument}
	for pi := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		blocks, images, data, err := pageTextBlocks(ctx, alloc, doc, pages[pi])
		if err != nil || (len(blocks) == 0 && len(images) == 0) {
			continue // skip unparseable or content-free pages
		}
		blocks, artifacts := splitFurniture(blocks, pi, furniture[pi])
		tables, rest := carveTables(blocks, pi)
		if kids, ok := classifyInReadingOrder(ctx, alloc, doc, pages[pi], pi, data, blocks, rest, images, tables); ok {
			root.Kids = append(append(root.Kids, kids...), artifacts...)
			continue
		}
		elems := mergeByOffset(classifyBlocks(rest, pi, data), figureElements(images, pi))
		elems = mergeByOffset(elems, tables)
		root.Kids = append(root.Kids, mergeByOffset(elems, artifacts)...)
	}
	return root, nil
}

// furniturePositionTol is how far, in points, the repeats of a running header
// or footer may sit from one distance to the page edge. A tuning constant, not
// spec-mandated: a producer sets a running head at one position on every
// page, so a few points absorb rounding and a changed font size, while a line
// that merely starts near the edge on some pages scatters past it.
const furniturePositionTol = 4.0

// detectFurniture finds the running headers and footers of the document and
// returns them per page index, keyed by the start byte of their text block.
// It reads the pages as [Classify] does, a pass ahead of it, since a running
// head is only known as one once every page is read.
//
// A block is furniture when it lies wholly in the top or bottom margin band of
// its page (see [heuristics.MarginEdge]), either repeats, with digits compared
// as equal and at the same distance from the edge, on a strict majority of the
// pages that show text or reads as the page's own number, and stands apart
// from the page's content (see [separatedFurniture]). A running head that
// changes with the chapter, a page number in a font whose codes are not ASCII,
// a table's header row repeated atop each page and any page turned by /Rotate
// stay content: missing furniture leaves a paragraph a reader can skip, while
// taking content for furniture hides it from assistive technology (ISO
// 32000-1, 14.8.2.2).
//
// # Corpus evidence
//
//   - govdocs, pdfjs, litho: of the 1602 files auto-tagging reaches, 138 carry
//     furniture, 2853 blocks in all (2150 footers, 703 headers). Before, 2624
//     were tagged P, 203 H1 to H3, and 26 were folded into a paragraph with
//     body text. The trees of the other 1464 files are unchanged.
//   - govdocs, pdfjs, litho: the PDF/UA-1 and PDF/UA-2 remediations of those
//     138 files draw no rule from veraPDF they did not draw before, and the
//     text they extract with artifacts is the same.
func detectFurniture(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page) (map[int]map[int]Pagination, error) {
	var lines []heuristics.Line
	var lineBlocks []furnitureBlock
	// geometry keeps every block of a page that holds a margin line, for the
	// separation test; other pages need none.
	geometry := make(map[int][]furnitureBlock)
	textPages := 0
	for pi, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		blocks, _, _, err := pageTextBlocks(ctx, alloc, doc, page)
		if err != nil || len(blocks) == 0 {
			continue
		}
		textPages++
		bottom, top, ok := visibleYRange(page)
		if !ok || page.Rotate != 0 {
			continue
		}
		var pageGeometry []furnitureBlock
		inBand := false
		for _, b := range blocks {
			fb := furnitureBlock{start: b.startByte, minY: b.minUY, maxY: b.maxUY, size: b.fontSize}
			pageGeometry = append(pageGeometry, fb)
			if !b.upright || b.shownOverflow || b.shown == "" {
				continue
			}
			edge, offset := heuristics.MarginEdge(b.minUY, b.maxUY, bottom, top)
			if edge == heuristics.EdgeNone {
				continue
			}
			inBand = true
			fb.edge = edge
			lines = append(lines, heuristics.Line{Page: pi, Text: b.shown, Edge: edge, Offset: offset})
			lineBlocks = append(lineBlocks, fb)
		}
		if inBand {
			geometry[pi] = pageGeometry
		}
	}

	var running []bool
	if textPages >= 2 {
		running = heuristics.RunningLines(lines, heuristics.RunningOptions{
			MinPages:          textPages/2 + 1,
			PositionTolerance: furniturePositionTol,
			FoldDigits:        true,
		})
	}
	type pageBand struct {
		page int
		edge heuristics.Edge
	}
	candidates := make(map[pageBand][]furnitureBlock)
	var order []pageBand
	for i, l := range lines {
		if (running == nil || !running[i]) && !heuristics.IsPageNumber(l.Text, l.Page+1) {
			continue
		}
		k := pageBand{page: l.Page, edge: l.Edge}
		if _, seen := candidates[k]; !seen {
			order = append(order, k)
		}
		candidates[k] = append(candidates[k], lineBlocks[i])
	}

	out := make(map[int]map[int]Pagination)
	for _, k := range order {
		furniture := candidates[k]
		if !separatedFurniture(furniture, geometry[k.page], k.edge) {
			continue
		}
		kind := PaginationFooter
		if k.edge == heuristics.EdgeTop {
			kind = PaginationHeader
		}
		if out[k.page] == nil {
			out[k.page] = make(map[int]Pagination)
		}
		for _, f := range furniture {
			out[k.page][f.start] = kind
		}
	}
	return out, nil
}

// furnitureBlock is the geometry of one text block the furniture pass weighs:
// its start byte, the default user space span of its line origins, its font
// size and, for a margin line, the band it sits in.
type furnitureBlock struct {
	start      int
	minY, maxY float64
	size       float64
	edge       heuristics.Edge
}

// furnitureGapLines is the least distance, in line heights, between page
// furniture and the nearest content toward the middle of the page. A tuning
// constant, not spec-mandated: a running head or a page number sits in the
// margin, set off from the body by at least a blank line, while the header
// row of a table repeated atop each page, or a diagram drawn up to the edge,
// runs on into what follows at single spacing.
//
// # Corpus evidence
//
//   - pdfjs bug1946953.pdf, govdocs thread_100_100981.pdf: a table's header
//     row repeats atop every page, directly above its rows, and repetition
//     alone read it as a running head.
//   - pdfjs bug1354114.pdf: the boxes of a diagram repeat atop every page.
const furnitureGapLines = 2.0

// maxFurnitureLines is the most text lines the furniture of one page band may
// hold. A tuning constant, not spec-mandated: a running head or foot is a
// line or two (a title, a date, a page number, a letter's continuation
// heading), while a block of several lines repeating at the edge, every
// number in it compared as equal, is content drawn the same way on each page.
//
// # Corpus evidence
//
//   - pdfjs bug1354114.pdf: the seven-line box of a diagram repeats at the
//     foot of pages 9 to 11, its numbers aside, apart from the rest of the
//     drawing.
const maxFurnitureLines = 3

// separatedFurniture reports whether the furniture blocks of one page band
// stand apart from the page's content: they hold at most maxFurnitureLines
// lines, no other block is interleaved with them, and the nearest one toward
// the middle of the page lies at least furnitureGapLines line heights away.
// blocks is every block of the page.
func separatedFurniture(furniture, blocks []furnitureBlock, edge heuristics.Edge) bool {
	if furnitureLines(furniture) > maxFurnitureLines {
		return false
	}
	isFurniture := make(map[int]bool, len(furniture))
	lo, hi := math.Inf(1), math.Inf(-1)
	var inner furnitureBlock
	for i, f := range furniture {
		isFurniture[f.start] = true
		lo, hi = math.Min(lo, f.minY), math.Max(hi, f.maxY)
		if i == 0 || (edge == heuristics.EdgeTop && f.minY < inner.minY) ||
			(edge == heuristics.EdgeBottom && f.maxY > inner.maxY) {
			inner = f
		}
	}
	gap := math.Inf(1)
	var next furnitureBlock
	for _, b := range blocks {
		if isFurniture[b.start] {
			continue
		}
		if b.maxY >= lo && b.minY <= hi {
			return false // content among the furniture
		}
		var d float64
		switch {
		case edge == heuristics.EdgeTop && b.maxY < lo:
			d = lo - b.maxY
		case edge == heuristics.EdgeBottom && b.minY > hi:
			d = b.minY - hi
		default:
			continue // nearer the edge than the furniture
		}
		if d < gap {
			gap, next = d, b
		}
	}
	if math.IsInf(gap, 1) {
		return true
	}
	return gap >= furnitureGapLines*lineHeightFactor*math.Max(inner.size, next.size)
}

// furnitureLines counts the distinct baselines the blocks' first and last
// lines sit on, two within rowBaselineTol of each other being one. A block's
// lines between its first and last are not counted, so the count is a floor.
func furnitureLines(blocks []furnitureBlock) int {
	ys := make([]float64, 0, 2*len(blocks))
	for _, b := range blocks {
		ys = append(ys, b.minY, b.maxY)
	}
	slices.Sort(ys)
	n := 0
	for i, y := range ys {
		if i == 0 || y-ys[i-1] > rowBaselineTol {
			n++
		}
	}
	return n
}

// visibleYRange returns the bottom and top of the page's crop box, the region
// a viewer shows (ISO 32000-1, 14.11.2.1), as the page tree walk reads it.
func visibleYRange(page *document.Page) (bottom, top float64, ok bool) {
	box := page.CropBox
	if !document.UsableBox(box) {
		return 0, 0, false
	}
	y0, _ := document.Number(box[1])
	y1, _ := document.Number(box[3])
	return math.Min(y0, y1), math.Max(y0, y1), true
}

// splitFurniture moves the blocks furniture names out of blocks and returns
// them as pagination artifact leaves, in content order, with the blocks that
// remain.
func splitFurniture(blocks []textBlock, pageIndex int, furniture map[int]Pagination) ([]textBlock, []*Element) {
	if len(furniture) == 0 {
		return blocks, nil
	}
	rest := make([]textBlock, 0, len(blocks))
	var artifacts []*Element
	for _, b := range blocks {
		kind, ok := furniture[b.startByte]
		if !ok {
			rest = append(rest, b)
			continue
		}
		artifacts = append(artifacts, &Element{
			Pagination: kind,
			Span:       &ContentSpan{PageIndex: pageIndex, Start: b.startByte, End: b.endByte},
		})
	}
	return rest, artifacts
}

// Table detection bounds. Tuning constants, not spec-mandated: the pass
// only claims layouts that are unmistakably grids, leaving everything
// else to the paragraph flow (over-claiming would fabricate wrong table
// semantics; under-claiming just leaves paragraphs).
const (
	// rowBaselineTol is the baseline difference, in points, within which
	// two blocks sit on the same table row.
	rowBaselineTol = 2.0
	// colAlignTol is the column start-x difference, in points, within
	// which cells of consecutive rows belong to the same column.
	colAlignTol = 3.0
	// minTableRows and minTableCols gate the smallest claimed grid.
	minTableRows = 2
	minTableCols = 2
)

// carveTables extracts strongly grid-like block runs as Table > TR > TD
// elements and returns them with the remaining blocks. A table is at
// least minTableRows consecutive rows of minTableCols or more blocks
// each, where a row shares one baseline and consecutive rows have the
// same column count with aligned start positions. Cells become TD
// leaves; no TH is synthesized, which keeps the result inside ISO/TS
// 32005's Table rows without guessing header semantics (a wrong TH
// would trigger the PDF/UA header-determination rules).
func carveTables(blocks []textBlock, pageIndex int) ([]*Element, []textBlock) {
	rows := blockRows(blocks)

	var tables []*Element
	consumed := make(map[int]bool)
	for i := 0; i < len(rows); {
		if len(rows[i].idx) < minTableCols {
			i++
			continue
		}
		j := i + 1
		for j < len(rows) && rowsAlign(blocks, rows[j-1], rows[j]) {
			j++
		}
		if j-i < minTableRows {
			i = j
			continue
		}
		table := &Element{Role: RoleTable}
		for _, row := range rows[i:j] {
			tr := &Element{Role: RoleTR}
			for _, bi := range row.idx {
				b := blocks[bi]
				tr.Kids = append(tr.Kids, &Element{
					Role: RoleTD,
					Span: &ContentSpan{PageIndex: pageIndex, Start: b.startByte, End: b.endByte},
				})
				consumed[bi] = true
			}
			table.Kids = append(table.Kids, tr)
		}
		tables = append(tables, table)
		i = j
	}
	if len(tables) == 0 {
		return nil, blocks
	}

	rest := make([]textBlock, 0, len(blocks))
	for bi, b := range blocks {
		if !consumed[bi] {
			rest = append(rest, b)
		}
	}
	return tables, rest
}

// blockRow is one same-baseline run of consecutive blocks.
type blockRow struct {
	idx []int
	y   float64
}

// blockRows groups consecutive blocks that share a baseline (within
// rowBaselineTol) with strictly increasing x into rows. Blocks without
// a tracked baseline form single-member rows and never join a grid.
func blockRows(blocks []textBlock) []blockRow {
	var rows []blockRow
	for bi := 0; bi < len(blocks); {
		b := blocks[bi]
		row := blockRow{idx: []int{bi}, y: b.y}
		j := bi + 1
		for j < len(blocks) && b.hasY && blocks[j].hasY &&
			math.Abs(blocks[j].y-row.y) <= rowBaselineTol &&
			blocks[j].x > blocks[j-1].x {
			row.idx = append(row.idx, j)
			j++
		}
		rows = append(rows, row)
		bi = j
	}
	return rows
}

// rowsAlign reports whether next reads as the row below prev in the
// same grid: same column count, each column start within colAlignTol,
// and next sits below prev.
func rowsAlign(blocks []textBlock, prev, next blockRow) bool {
	if len(next.idx) != len(prev.idx) || len(next.idx) < minTableCols {
		return false
	}
	if next.y >= prev.y {
		return false
	}
	for k := range prev.idx {
		if math.Abs(blocks[prev.idx[k]].x-blocks[next.idx[k]].x) > colAlignTol {
			return false
		}
	}
	return true
}

// elemStart returns the byte offset of an element's first leaf, for
// content-order interleaving of containers and leaves.
func elemStart(e *Element) int {
	for e != nil {
		if e.Span != nil {
			return e.Span.Start
		}
		if len(e.Kids) == 0 {
			return 0
		}
		e = e.Kids[0]
	}
	return 0
}

// imageBlock is one image paint outside any text object: a Do of an
// image XObject or an inline image (BI..EI), with its byte range in the
// decoded content and the resource name for the synthesized /Alt.
type imageBlock struct {
	startByte, endByte int
	name               string
	// box is where the image paints in default user space, the unit square
	// carried through the CTM (ISO 32000-1, 8.9.4 and 8.9.7).
	box [4]float64
}

// unitSquareBox returns the axis-aligned box of the unit square under ctm,
// [minX minY maxX maxY].
func unitSquareBox(ctm [6]float64) [4]float64 {
	box := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range [4][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		x := p[0]*ctm[0] + p[1]*ctm[2] + ctm[4]
		y := p[0]*ctm[1] + p[1]*ctm[3] + ctm[5]
		box[0], box[1] = math.Min(box[0], x), math.Min(box[1], y)
		box[2], box[3] = math.Max(box[2], x), math.Max(box[3], y)
	}
	return box
}

// figureElements builds one Figure leaf per image paint. The /Alt value
// is a synthesized placeholder naming the image resource: ISO 14289
// requires alternative text on every Figure, and litho cannot infer the
// image's meaning, so the placeholder keeps the requirement satisfied
// while staying recognizable as machine-generated for a human editor.
func figureElements(images []imageBlock, pageIndex int) []*Element {
	out := make([]*Element, 0, len(images))
	for _, img := range images {
		out = append(out, &Element{
			Role: RoleFigure,
			Alt:  "Image: " + img.name,
			Span: &ContentSpan{PageIndex: pageIndex, Start: img.startByte, End: img.endByte},
		})
	}
	return out
}

// mergeByOffset interleaves two per-page element lists into content
// order by the byte offsets of their first leaves. Both inputs are
// already ordered.
func mergeByOffset(a, b []*Element) []*Element {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	out := make([]*Element, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if elemStart(a[i]) <= elemStart(b[j]) {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}

// textBlock is one text object (BT…ET) that painted text: its byte range, the
// dominant font size across its show-text operators, the total byte length of
// the strings it showed (a proxy for text quantity), and the baselines of its
// first and last text lines (used to merge adjacent lines into paragraphs).
//
// The font size IS transform-aware: it is the Tf size scaled by the text matrix
// and the CTM (ISO 32000-1, 9.4.4), so 12pt drawn under a doubling cm is
// recorded as 24 and reads as the heading it looks like. See textVScale.
//
// Baselines are deliberately NOT, and the asymmetry is the design. They are
// tracked from the text positioning operators only (Tm translation, Td/TD/T*,
// TL), leaving them in text space; the CTM translation and scale, text rise
// (Ts, a per-glyph offset rather than a line advance) and rotation do not move
// them. Mapping them to device space would silently redefine rowBaselineTol and
// colAlignTol, which are absolute point tolerances, and a page under a 0.1 scale
// would collapse unrelated rows into one and fabricate a table. The two
// adjacency tests that compare a baseline gap against a size (lineAdjacent,
// listItemAdjacent) stay consistent because q/Q/cm may not appear inside a text
// object (ISO 32000-1, 8.2, Table 31), so one block has one CTM, and
// classifyBlocks refuses to merge across the non-blank bytes any intervening cm
// leaves behind (see blankGap).
type textBlock struct {
	startByte, endByte int
	fontSize           float64
	textLen            int
	y, lastY           float64
	hasY               bool
	// x is the block's first text-line start position, tracked from Tm
	// translation and Td accumulation like y. Used only by the table
	// pass to test column alignment; the same caveats as y apply (cm
	// and rotation are ignored).
	x float64
	// firstText is a short raw-byte prefix of the block's first shown
	// string, used by the list pass to spot bullet and enumeration
	// markers. Raw bytes, not decoded text: the marker check matches
	// ASCII and the WinAnsi bullet only, so font-encoding differences
	// degrade to "no marker" rather than a wrong match.
	firstText string
	// minUY and maxUY bound the block's line origins in default user space,
	// the CTM and the full line matrix applied, which is where the margin
	// bands of the page box are: the furniture pass reads them, nothing
	// else. upright is false when any line reads other than left to right
	// and bottom to top, which the margin bands do not describe.
	minUY, maxUY float64
	upright      bool
	// shown is the block's shown string bytes with whitespace dropped, so a
	// TJ kerning gap and a space compare equal across pages. shownOverflow
	// records that they exceeded maxFurnitureTextBytes, and such a block is
	// never furniture.
	shown         string
	shownOverflow bool
}

// appendShown adds the bytes op shows to b.shown, whitespace dropped, until
// they pass maxFurnitureTextBytes.
func (b *textBlock) appendShown(op content.Operation) {
	if b.shownOverflow {
		return
	}
	var strs []ast.PdfString
	switch op.Operator {
	case "Tj", "'":
		if len(op.Operands) >= 1 {
			if s, ok := op.Operands[0].(ast.PdfString); ok {
				strs = append(strs, s)
			}
		}
	case "\"":
		if len(op.Operands) >= 3 {
			if s, ok := op.Operands[2].(ast.PdfString); ok {
				strs = append(strs, s)
			}
		}
	case "TJ":
		if len(op.Operands) >= 1 {
			if arr, ok := op.Operands[0].(ast.PdfArray); ok {
				for _, el := range arr {
					if s, ok := el.(ast.PdfString); ok {
						strs = append(strs, s)
					}
				}
			}
		}
	}
	buf := []byte(b.shown)
	for _, s := range strs {
		for _, c := range []byte(s) {
			if isSpaceByte(c) {
				continue
			}
			if len(buf) >= maxFurnitureTextBytes {
				b.shown, b.shownOverflow = "", true
				return
			}
			buf = append(buf, c)
		}
	}
	b.shown = string(buf)
}

// isSpaceByte reports whether c is PDF white space (ISO 32000-1, 7.2.2,
// Table 1).
func isSpaceByte(c byte) bool {
	switch c {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

// moveLine applies a Td of (tx, ty) to the text matrix's translation: the new
// line matrix is the translation times the current one (ISO 32000-1, 9.4.2,
// Table 108), so the offset is scaled and turned by the matrix it moves in.
// The scale entries are left as they are.
func moveLine(tm *[6]float64, tx, ty float64) {
	tm[4] += tx*tm[0] + ty*tm[2]
	tm[5] += tx*tm[1] + ty*tm[3]
}

// lineOrigin returns the default user space Y of the current line's origin,
// the text matrix and then the CTM applied (ISO 32000-1, 9.4.4), and whether
// the line reads upright: its baseline runs left to right, within
// uprightSkew, and its glyphs stand up the page. A slant (an oblique made by
// shearing the vertical) keeps a line upright.
func lineOrigin(tm, ctm [6]float64) (float64, bool) {
	y := tm[4]*ctm[1] + tm[5]*ctm[3] + ctm[5]
	bx := tm[0]*ctm[0] + tm[1]*ctm[2]
	by := tm[0]*ctm[1] + tm[1]*ctm[3]
	uy := tm[2]*ctm[1] + tm[3]*ctm[3]
	upright := bx > 0 && uy > 0 && math.Abs(by) <= uprightSkew*bx
	return y, upright
}

// uprightSkew is the largest rise of a baseline, as a share of its run, for a
// line to still read upright. A tuning constant, not spec-mandated: it admits
// rounding in a producer's matrices and a slight tilt, and nothing turned far
// enough to change which page edge the line faces.
const uprightSkew = 0.1

// maxFurnitureTextBytes bounds the shown bytes a block keeps for the
// furniture pass. A tuning constant, not spec-mandated: a running header or
// footer is a line or two, well inside 256 bytes, and a block beyond it is
// body text, so the bound only spares memory.
const maxFurnitureTextBytes = 256

// firstTextPrefixLen bounds the captured first-string prefix. Markers
// ("•", "12.", "(a)") fit well within 16 bytes.
const firstTextPrefixLen = 16

// pageTextBlocks segments a page's content into per-text-object blocks (one per
// BT…ET) and returns them with the length of the decoded content they index
// into. Marking whole text objects keeps marked-content sequences cleanly
// nested with BT/ET and lets the gaps between them be artifacted as a unit.
// The content is read whole ([content.WholeStream]): a viewer draws the text
// after a stray "]" that validation reads no further than, and ISO 32000-1,
// 14.8.2.2 asks that it be tagged too.
func pageTextBlocks(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) ([]textBlock, []imageBlock, []byte, error) {
	data, err := pageContentBytes(ctx, alloc, doc, page)
	if err != nil || len(data) == 0 {
		return nil, nil, data, err
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.WholeStream())
	if err != nil {
		return nil, nil, data, err
	}

	imageNames := pageImageXObjectNames(ctx, alloc, doc, page)

	var blocks []textBlock
	var images []imageBlock
	var cur *textBlock
	curFont := 0.0
	// The text state belongs to the graphics state, so Q restores every
	// parameter of it (ISO 32000-1, 8.4.2 and Table 104), and so does the CTM.
	// This loop tracks three of them, the font size, the leading and the CTM,
	// so the frame carries all three: save exactly what you track, the rule
	// savedTextState follows in collectFontUsage.
	//
	// The leaks differ in how they show. A size raised inside q…Q used to stay
	// in effect after the Q, and since fontSize drives bodyFontSize and the
	// fontSize >= body*headingSizeRatio test, that promoted ordinary body text
	// to a heading. A leaked leading moves a baseline instead:
	// textY -= leading feeds cur.lastY and cur.y, which lineAdjacent compares,
	// so a paragraph that should merge splits in two. A leaked CTM mis-sizes
	// every glyph drawn after the Q.
	type textStateFrame struct {
		font    float64
		leading float64
		ctm     [6]float64
	}
	var stateStack []textStateFrame
	textY, textX, leading := 0.0, 0.0, 0.0
	// ctm is graphics state and rides the q/Q stack; tm is text-object state,
	// set absolutely by Tm and reset to identity by BT (ISO 32000-1, 9.4.2), so
	// q/Q must NOT save it. Together they scale the Tf size into the size a
	// reader sees (9.4.4).
	ctm := [6]float64{1, 0, 0, 1, 0, 0}
	tm := [6]float64{1, 0, 0, 1, 0, 0}
	sizeChars := make(map[float64]int) // dominant-size accumulator for the current block

	flush := func() {
		if cur != nil && cur.textLen > 0 {
			cur.fontSize = dominantSize(sizeChars)
			blocks = append(blocks, *cur)
		}
		cur = nil
		clear(sizeChars)
	}
	extend := func(end int) {
		if cur != nil {
			cur.endByte = end
		}
	}

	for i, op := range ops {
		if i%100 == 0 { //nolint:mnd // per-~100-operator ctx checkpoint, per CONTRIBUTING
			if err := ctx.Err(); err != nil {
				return nil, nil, data, err
			}
		}
		r := ranges[i]
		switch op.Operator {
		case "Do":
			// Image paints become Figure leaves. Only outside text
			// objects: an image inside BT..ET would break the clean
			// nesting of the injected marked-content operators.
			if cur == nil && len(op.Operands) == 1 {
				if n, ok := op.Operands[0].(ast.PdfName); ok && imageNames[string(n)] {
					images = append(images, imageBlock{startByte: r.Start, endByte: r.End, name: string(n), box: unitSquareBox(ctm)})
				}
			}
		case "BI":
			// The parser surfaces a whole inline image (BI..EI) as one op.
			if cur == nil {
				images = append(images, imageBlock{startByte: r.Start, endByte: r.End, name: "inline", box: unitSquareBox(ctm)})
			}
		case "q":
			stateStack = append(stateStack, textStateFrame{font: curFont, leading: leading, ctm: ctm})
		case "Q":
			if n := len(stateStack); n > 0 {
				curFont, leading, ctm = stateStack[n-1].font, stateStack[n-1].leading, stateStack[n-1].ctm
				stateStack = stateStack[:n-1]
			}
		case "cm":
			// New CTM = the operand matrix times the current one (ISO 32000-1,
			// 8.4.4). Same multiplication order as handleConcatMatrix in the
			// graphics machine and as ExtractLines, so the three walks agree.
			if len(op.Operands) == 6 {
				var m [6]float64
				ok := true
				for k := range m {
					v, good := toFloat(op.Operands[k])
					if !good {
						ok = false
						break
					}
					m[k] = v
				}
				if ok {
					ctm = [6]float64{
						m[0]*ctm[0] + m[1]*ctm[2],
						m[0]*ctm[1] + m[1]*ctm[3],
						m[2]*ctm[0] + m[3]*ctm[2],
						m[2]*ctm[1] + m[3]*ctm[3],
						m[4]*ctm[0] + m[5]*ctm[2] + ctm[4],
						m[4]*ctm[1] + m[5]*ctm[3] + ctm[5],
					}
				}
			}
		case "BT":
			flush()
			cur = &textBlock{startByte: r.Start, endByte: r.End}
			textY, textX, leading = 0, 0, 0 // BT resets the text and line matrices to identity
			tm = [6]float64{1, 0, 0, 1, 0, 0}
		case "ET":
			extend(r.End)
			flush()
		case "Tf":
			if len(op.Operands) >= 2 {
				if s, ok := toFloat(op.Operands[1]); ok {
					curFont = s
				}
			}
			extend(r.End)
		case "Tm":
			if len(op.Operands) >= 6 {
				if f, ok := toFloat(op.Operands[5]); ok {
					textY = f
				}
				if f, ok := toFloat(op.Operands[4]); ok {
					textX = f
				}
				// Tm SETS the text matrix, it does not concatenate
				// (ISO 32000-1, 9.4.2). Operands 0-3 carry the scale and shear
				// that decide how large the Tf size actually draws.
				for k := range 6 {
					if f, ok := toFloat(op.Operands[k]); ok {
						tm[k] = f
					}
				}
			}
			extend(r.End)
		case "Td", "TD":
			if len(op.Operands) >= 2 {
				tx, okX := toFloat(op.Operands[0])
				if okX {
					textX += tx
				}
				ty, okY := toFloat(op.Operands[1])
				if okY {
					textY += ty
					if op.Operator == "TD" {
						leading = -ty
					}
				}
				if okX && okY {
					moveLine(&tm, tx, ty)
				}
			}
			extend(r.End)
		case "TL":
			if len(op.Operands) >= 1 {
				if l, ok := toFloat(op.Operands[0]); ok {
					leading = l
				}
			}
			extend(r.End)
		case "T*":
			textY -= leading
			moveLine(&tm, 0, -leading)
			extend(r.End)
		default:
			// Show-text operators; ' and " advance to the next line first.
			if op.Operator == "'" || op.Operator == "\"" {
				textY -= leading
				moveLine(&tm, 0, -leading)
			}
			if cur != nil {
				cur.endByte = r.End
				if n := showTextLen(op); n > 0 {
					uy, upright := lineOrigin(tm, ctm)
					if cur.textLen == 0 {
						cur.firstText = firstShownPrefix(op)
						cur.minUY, cur.maxUY, cur.upright = uy, uy, upright
					}
					cur.minUY, cur.maxUY = math.Min(cur.minUY, uy), math.Max(cur.maxUY, uy)
					cur.upright = cur.upright && upright
					cur.textLen += n
					cur.appendShown(op)
					sizeChars[roundHalf(curFont*textVScale(tm, ctm))] += n
					cur.lastY = textY
					if !cur.hasY {
						cur.y, cur.x, cur.hasY = textY, textX, true
					}
				}
			}
		}
	}
	flush()
	return blocks, images, data, nil
}

// pageImageXObjectNames resolves the page's /Resources /XObject entries
// and returns the names whose streams carry /Subtype /Image. Form
// XObjects are excluded: their content is not descended into (see the
// Classify scope note), so wrapping their Do in a Figure would mark
// arbitrary nested content as an image. It reads the page's resources as alloc
// stages them, its own or those it inherits from the page tree (ISO 32000-1,
// 7.7.3.4; see [stagedResources]).
func pageImageXObjectNames(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) map[string]bool {
	res := stagedResources(ctx, alloc, doc, page)
	if res == nil {
		return nil
	}
	xobjs, err := stagedDict(ctx, alloc, doc, res["XObject"])
	if err != nil || xobjs == nil {
		return nil
	}
	names := make(map[string]bool, len(xobjs))
	for name, obj := range xobjs {
		stream, err := stagedStream(ctx, alloc, doc, obj)
		if err != nil {
			continue
		}
		if st, _ := stream.Dict["Subtype"].(ast.PdfName); st == "Image" {
			names[string(name)] = true
		}
	}
	return names
}

// textVScale returns how much the text matrix and the CTM magnify a glyph
// vertically, so a Tf size becomes the size a reader sees (ISO 32000-1, 9.4.4:
// a glyph reaches the page through the text matrix and then the CTM).
//
// A matrix [a b c d e f] maps (x, y) to (ax+cy+e, bx+dy+f), so text space's
// vertical unit (0, 1) leaves tm as (tm.c, tm.d) and arrives in user space as
// (tm.c*ctm.a + tm.d*ctm.c, tm.c*ctm.b + tm.d*ctm.d). Its length is the factor.
//
// The length rather than d alone: under rotation the vertical extent moves into
// the other component, and reading d would report a quarter-turned heading as
// zero-sized. With no rotation or shear this is exactly |tm.d * ctm.d|.
func textVScale(tm, ctm [6]float64) float64 {
	x := tm[2]*ctm[0] + tm[3]*ctm[2]
	y := tm[2]*ctm[1] + tm[3]*ctm[3]
	return math.Hypot(x, y)
}

// dominantSize returns the rounded font size that painted the most characters,
// breaking ties toward the larger size so a heading is not demoted by a shorter
// trailing run at body size.
func dominantSize(sizeChars map[float64]int) float64 {
	var best float64
	bestN := 0
	for sz, n := range sizeChars {
		if n > bestN || (n == bestN && sz > best) {
			best, bestN = sz, n
		}
	}
	return best
}

// lineHeightFactor estimates a text line's height as font size times this.
const lineHeightFactor = 1.2

// maxLineGapRatio is the largest vertical gap, in line-heights, between two
// lines that still merge into one paragraph. A wider gap reads as a paragraph
// break. Kept near a single leading so a new short paragraph at typical spacing
// breaks rather than folds. A tuning constant, not spec-mandated.
const maxLineGapRatio = 1.3

// classifyBlocks assigns a role to each block, merges adjacent paragraph lines
// into multi-line paragraphs, and builds the leaf elements for one page in
// content (reading) order.
//
// Only paragraphs are merged: a wrapped heading authored as separate text
// objects stays as separate heading elements, which over-segments (safe) rather
// than folding two distinct same-size headings into one (which would drop a
// heading from the hierarchy). Two blocks merge only when nothing but
// whitespace lies between them, so intervening graphics (a rule, a background)
// is never swallowed into a paragraph and stays an artifact.
func classifyBlocks(blocks []textBlock, pageIndex int, data []byte) []*Element {
	if len(blocks) == 0 {
		return nil
	}
	body := bodyFontSize(blocks)
	levels := headingLevels(blocks, body)

	roles := make([]Role, len(blocks))
	for i, b := range blocks {
		roles[i] = roleOf(b, body, levels[i])
	}

	var elems []*Element
	var metas []elemMeta
	for i := 0; i < len(blocks); {
		role := roles[i]
		start, end := blocks[i].startByte, blocks[i].endByte
		j := i + 1
		// A marker-starting line opens a new list item and never folds
		// into the preceding paragraph or item.
		for j < len(blocks) && role == RoleP && roles[j] == RoleP &&
			!isListMarker(blocks[j].firstText) &&
			blankGap(data, blocks[j-1].endByte, blocks[j].startByte) &&
			lineAdjacent(blocks[j-1], blocks[j]) {
			end = blocks[j].endByte
			j++
		}
		meta := elemMeta{
			firstText: blocks[i].firstText,
			y:         blocks[i].y,
			lastY:     blocks[j-1].lastY,
			fontSize:  blocks[i].fontSize,
			hasY:      blocks[i].hasY && blocks[j-1].hasY,
		}
		i = j

		if start < 0 {
			start = 0
		}
		if end > len(data) {
			end = len(data)
		}
		if start >= end {
			continue
		}
		elems = append(elems, &Element{
			Role: role,
			Span: &ContentSpan{PageIndex: pageIndex, Start: start, End: end},
		})
		metas = append(metas, meta)
	}
	return listify(elems, metas)
}

// elemMeta carries the layout facts of one classified element that the
// list pass needs: the first shown bytes (marker detection) and the
// first/last baselines (item adjacency).
type elemMeta struct {
	firstText string
	y, lastY  float64
	fontSize  float64
	hasY      bool
}

// minListItems is the smallest run of marker-starting lines treated as a
// list. A single bullet-looking line is more often a dash-prefixed
// sentence than a one-item list, so it stays a paragraph (FP guard, not
// spec-mandated).
const minListItems = 2

// maxListItemGapRatio is the largest vertical gap between two list
// items, in line-heights, that keeps them in the same list. Items are
// commonly spaced wider than wrapped lines, so this sits above
// maxLineGapRatio. A tuning constant, not spec-mandated.
const maxListItemGapRatio = 2.0

// listItemAdjacent reports whether the element described by b reads as
// the next item of the list ending with a.
func listItemAdjacent(a, b elemMeta) bool {
	if !a.hasY || !b.hasY {
		return false
	}
	lh := math.Max(a.fontSize, b.fontSize) * lineHeightFactor
	if lh <= 0 {
		return false
	}
	gap := a.lastY - b.y
	return gap > 0 && gap <= lh*maxListItemGapRatio
}

// listify wraps runs of marker-starting paragraphs into L > LI > LBody
// structures. The marker glyphs stay inside the LBody span: spans must
// sit on operator boundaries, so the Lbl cannot be split out of the
// item's first show-text operand. ISO/TS 32005 permits LI > LBody
// without an Lbl, and a missing Lbl keeps the PDF/UA-2 ListNumbering
// requirement (ISO 14289-2, 8.2.5.25) out of scope.
func listify(elems []*Element, metas []elemMeta) []*Element {
	var out []*Element
	for i := 0; i < len(elems); {
		if elems[i].Role != RoleP || !isListMarker(metas[i].firstText) {
			out = append(out, elems[i])
			i++
			continue
		}
		j := i + 1
		for j < len(elems) && elems[j].Role == RoleP && isListMarker(metas[j].firstText) &&
			listItemAdjacent(metas[j-1], metas[j]) {
			j++
		}
		if j-i < minListItems {
			out = append(out, elems[i:j]...)
			i = j
			continue
		}
		list := &Element{Role: RoleL}
		for _, item := range elems[i:j] {
			body := &Element{Role: RoleLBody, Span: item.Span}
			list.Kids = append(list.Kids, &Element{Role: RoleLI, Kids: []*Element{body}})
		}
		out = append(out, list)
		i = j
	}
	return out
}

// isListMarker reports whether a block's first shown bytes start with a
// bullet or enumeration marker followed by a space (or are the marker
// alone). Matched conservatively against ASCII plus the WinAnsi bullet
// (0x95) and en dash (0x96): unknown font encodings degrade to "not a
// marker".
func isListMarker(s string) bool {
	if s == "" {
		return false
	}
	var rest string
	switch s[0] {
	case '\x95', '\xb7', '*', '-', '\x96':
		rest = s[1:]
	default:
		n := 0
		for n < len(s) && n < 3 && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		if n == 0 || n >= len(s) || (s[n] != '.' && s[n] != ')') {
			return false
		}
		rest = s[n+1:]
	}
	return rest == "" || rest[0] == ' '
}

// blankGap reports whether the bytes between aEnd and bStart are absent or only
// whitespace. Because operation byte ranges are contiguous, a non-blank gap
// means an intervening operator (graphics, a cm reposition), across which two
// text objects must not be merged.
func blankGap(data []byte, aEnd, bStart int) bool {
	if aEnd < 0 || bStart > len(data) || aEnd > bStart {
		return false
	}
	return isBlank(data[aEnd:bStart])
}

// roleOf classifies one block as a heading (by font size relative to body) or a
// paragraph. level is the block's reading-order heading level from
// [headingLevels], and is 0 when the block is not a heading.
func roleOf(b textBlock, body float64, level int) Role {
	if level > 0 && body > 0 && b.fontSize >= body*headingSizeRatio {
		return headingRole(level)
	}
	return RoleP
}

// lineAdjacent reports whether block b is the next line of block a: its first
// baseline sits below a's last baseline (PDF Y increases upward) by no more than
// maxLineGapRatio line-heights. Using a's last line lets a multi-line block join
// a following continuation. Blocks emitted out of top-to-bottom order yield a
// non-positive gap and never merge, the intended fail-safe. Blocks without a
// tracked baseline never merge.
func lineAdjacent(a, b textBlock) bool {
	if !a.hasY || !b.hasY {
		return false
	}
	lh := math.Max(a.fontSize, b.fontSize) * lineHeightFactor
	if lh <= 0 {
		return false
	}
	gap := a.lastY - b.y
	return gap > 0 && gap <= lh*maxLineGapRatio
}

// bodyFontSize returns the dominant text size: the rounded size carrying the
// most characters across the page's blocks.
func bodyFontSize(blocks []textBlock) float64 {
	weight := make(map[float64]int)
	for _, b := range blocks {
		if b.fontSize > 0 {
			weight[roundHalf(b.fontSize)] += b.textLen
		}
	}
	var best float64
	bestW := 0
	for sz, w := range weight {
		if w > 0 && (w > bestW || (w == bestW && sz < best)) {
			best, bestW = sz, w
		}
	}
	return best
}

// headingLevels assigns a heading level to every block, in reading order, and
// returns one level per block index (0 where the block is not a heading).
//
// ISO 14289-1, 7.4.2 judges the sequence as it is READ, not as it is sized: the
// first heading in the document must be H1, and no heading may jump more than
// one level past the deepest level seen so far. Ranking the distinct sizes and
// handing each block its rank satisfies neither, because the first heading a
// page happens to contain is rarely the largest one on it, so remediation
// introduced the very violation litho's own validator reports.
//
// A monotone stack of the sizes still open gives the level instead. A heading
// closes every open heading its own size or smaller, then sits one level below
// whatever remains, so the first heading always lands on H1, a larger heading
// re-opens at a shallower level, and the level can only ever grow by one.
//
// # Corpus evidence
//
//   - govdocs thread_000_000337.pdf: tagged to H6 first and drew 7.4.2 test 1
//     from litho's own validator, one of 35 files where remediation
//     introduced that violation.
func headingLevels(blocks []textBlock, body float64) []int {
	levels := make([]int, len(blocks))
	var open []float64
	for i, b := range blocks {
		if body <= 0 || b.fontSize < body*headingSizeRatio {
			continue
		}
		sz := roundHalf(b.fontSize)
		for len(open) > 0 && open[len(open)-1] <= sz {
			open = open[:len(open)-1]
		}
		levels[i] = len(open) + 1
		open = append(open, sz)
	}
	return levels
}

func headingRole(level int) Role {
	if level < 1 {
		level = 1
	}
	if level > maxHeadingLevels {
		level = maxHeadingLevels
	}
	return Role(fmt.Sprintf("H%d", level))
}

// roundHalf rounds to the nearest 0.5 point so near-equal font sizes cluster.
func roundHalf(x float64) float64 {
	return math.Round(x*2) / 2
}

func toFloat(o ast.PdfObject) (float64, bool) {
	switch v := o.(type) {
	case ast.PdfInt:
		return float64(v), true
	case ast.PdfReal:
		return float64(v), true
	default:
		return 0, false
	}
}

// firstShownPrefix returns up to firstTextPrefixLen raw bytes of the
// first string an op shows, for list-marker detection.
func firstShownPrefix(op content.Operation) string {
	var s ast.PdfString
	switch op.Operator {
	case "Tj", "'":
		if len(op.Operands) >= 1 {
			s, _ = op.Operands[0].(ast.PdfString)
		}
	case "\"":
		if len(op.Operands) >= 3 {
			s, _ = op.Operands[2].(ast.PdfString)
		}
	case "TJ":
		if len(op.Operands) >= 1 {
			if arr, ok := op.Operands[0].(ast.PdfArray); ok {
				for _, item := range arr {
					if str, ok := item.(ast.PdfString); ok {
						s = str
						break
					}
				}
			}
		}
	}
	if len(s) > firstTextPrefixLen {
		s = s[:firstTextPrefixLen]
	}
	return string(s)
}

// showTextLen returns the byte length of the strings a text-showing operator
// paints, a proxy for text quantity (not a decoded character count: multi-byte
// or CID-encoded strings overcount, which only skews the relative weighting).
func showTextLen(op content.Operation) int {
	switch op.Operator {
	case "Tj", "'":
		if len(op.Operands) >= 1 {
			if s, ok := op.Operands[0].(ast.PdfString); ok {
				return len(string(s))
			}
		}
	case "\"":
		if len(op.Operands) >= 3 {
			if s, ok := op.Operands[2].(ast.PdfString); ok {
				return len(string(s))
			}
		}
	case "TJ":
		if len(op.Operands) >= 1 {
			if arr, ok := op.Operands[0].(ast.PdfArray); ok {
				n := 0
				for _, el := range arr {
					if s, ok := el.(ast.PdfString); ok {
						n += len(string(s))
					}
				}
				return n
			}
		}
	}
	return 0
}
