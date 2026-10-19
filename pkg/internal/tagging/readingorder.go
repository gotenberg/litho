package tagging

import (
	"cmp"
	"context"
	"log/slog"
	"math"
	"slices"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// Reading-order segmentation bounds. Tuning constants, not spec-mandated: the
// pass only claims a column layout it cannot mistake, and leaves every other
// page in content order, which is the order the producer wrote and the order
// the tagger used before.
const (
	// minGutterEm is the narrowest gap, in body font sizes, between two
	// columns. A word space is a quarter of one, so the gaps between the
	// words of a line, each shown by its own text object, never read as
	// one.
	minGutterEm = 1.0
	// minColumnLines is the least height, in body line heights, of a
	// column. A line or two beside the text, a page number or a date, is
	// not a column.
	minColumnLines = 3
	// minColumnWidthEm is the least width, in body font sizes, of a column.
	// The labels of a form, set down the left of the page with their
	// values beside them, are narrower, and reading them as a column would
	// read every label before any value.
	minColumnWidthEm = 10.0
	// maxOrderItems bounds the blocks, figures and tables one page orders.
	// An anti-DoS guard, not spec-mandated: the segmentation sorts the
	// items at each level of its recursion, and a page past the bound,
	// thousands of text objects of one glyph each, keeps content order.
	maxOrderItems = 4000
	// maxOrderDepth bounds the recursion of the segmentation. A guard on
	// litho's own stack, not spec-mandated: each level splits a region in
	// two at least, and a region past the bound keeps content order.
	maxOrderDepth = 64
	// maxLevelAngle is the largest turn of a glyph's baseline, in radians,
	// that still reads as horizontal: one degree, the drift text
	// reconstruction allows a horizontal line (see the typography package).
	maxLevelAngle = math.Pi / 180
)

// orderItem is one thing the reading-order pass places: a text block, a
// figure or a table, by the box it paints in default user space and the
// content-stream offset that is its place in content order.
type orderItem struct {
	minX, minY, maxX, maxY float64
	start                  int
}

// readingOrder returns the order a reader takes the items in, as indices into
// items, or nil when it is content order.
//
// ISO 32000-1, 14.8.2.3 and ISO 14289-1, 7.2 ask the logical structure to
// follow the reading order, which the content stream need not: a producer
// may set the two columns of a page line by line across both, or draw a
// sidebar or a caption before the text it accompanies. The pass segments the
// page as XY-cut does. A region whose items a vertical gutter parts into
// columns ([columnSplit]) reads column by column, left to right, or right to
// left when the page's text is right-to-left (rtl); otherwise a region a
// horizontal gap parts ([bandSplit]) reads band by band, top to bottom; and a
// region neither parts keeps content order, so text set over text, a
// superscript and a line of words each its own text object stay as the
// producer wrote them.
//
// body is the page's body font size, which scales the gutter and column
// bounds; with none the pass keeps content order.
func readingOrder(items []orderItem, body float64, rtl bool) []int {
	if len(items) < 2 || len(items) > maxOrderItems || body <= 0 {
		return nil
	}
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	s := segmenter{items: items, body: body, rtl: rtl}
	order := s.cut(idx, 0)
	if slices.IsSortedFunc(order, s.byContent) {
		return nil
	}
	return order
}

// segmenter holds what one page's reading-order pass reads.
type segmenter struct {
	items []orderItem
	body  float64
	rtl   bool
}

// byContent compares two items by content order.
func (s *segmenter) byContent(a, b int) int {
	return cmp.Or(cmp.Compare(s.items[a].start, s.items[b].start), cmp.Compare(a, b))
}

// cut returns the items of one region in reading order.
//
// A region no gutter parts into columns is parted into bands at every
// horizontal gap no item crosses. The lines of two columns set side by side
// are bands of their own, each holding a line of either column, so a run of
// bands that keeps a common gutter open is read again as one region, which
// parts into its columns: a title over two columns is one band, the columns
// under it another region.
func (s *segmenter) cut(idx []int, depth int) []int {
	if len(idx) < 2 || depth >= maxOrderDepth {
		return s.contentOrder(idx)
	}
	if cols := s.columnSplit(idx); cols != nil {
		if s.rtl {
			slices.Reverse(cols)
		}
		out := make([]int, 0, len(idx))
		for _, col := range cols {
			out = append(out, s.cut(col, depth+1)...)
		}
		return out
	}
	bands := s.bands(idx)
	if len(bands) < 2 {
		return s.contentOrder(idx)
	}
	out := make([]int, 0, len(idx))
	for i := 0; i < len(bands); {
		j := s.gutterRun(bands, i)
		if run := slices.Concat(bands[i:j]...); j > i+1 && j-i < len(bands) && s.columnSplit(run) != nil {
			out = append(out, s.cut(run, depth+1)...)
			i = j
			continue
		}
		// The bands of a run that holds no columns are read one by one,
		// and none of them starts a run again: measuring the run from each
		// would make a page of n bands cost n runs of up to n bands.
		for _, band := range bands[i:j] {
			out = append(out, s.cut(band, depth+1)...)
		}
		i = j
	}
	return out
}

// contentOrder returns idx sorted into content order.
func (s *segmenter) contentOrder(idx []int) []int {
	out := slices.Clone(idx)
	slices.SortFunc(out, s.byContent)
	return out
}

// columnSplit parts a region at the vertical gutters no item crosses, left to
// right, and returns the columns, or nil when the region holds no column
// layout. Every part must be a column ([segmenter.isColumn]); when some are
// not, the widest single gutter whose two sides both are parts the region in
// two, and the recursion parts each side further.
func (s *segmenter) columnSplit(idx []int) [][]int {
	sorted := slices.Clone(idx)
	slices.SortFunc(sorted, func(a, b int) int {
		return cmp.Or(cmp.Compare(s.items[a].minX, s.items[b].minX), s.byContent(a, b))
	})
	minGutter := minGutterEm * s.body
	type gutter struct {
		at    int // index in sorted of the first item right of the gutter
		width float64
	}
	var gutters []gutter
	reach := s.items[sorted[0]].maxX
	for k := 1; k < len(sorted); k++ {
		it := s.items[sorted[k]]
		if w := it.minX - reach; w >= minGutter {
			gutters = append(gutters, gutter{at: k, width: w})
		}
		reach = math.Max(reach, it.maxX)
	}
	if len(gutters) == 0 {
		return nil
	}
	parts := make([][]int, 0, len(gutters)+1)
	from := 0
	for _, g := range gutters {
		parts = append(parts, sorted[from:g.at])
		from = g.at
	}
	parts = append(parts, sorted[from:])
	if !slices.ContainsFunc(parts, func(p []int) bool { return !s.isColumn(p) }) {
		return parts
	}
	slices.SortStableFunc(gutters, func(a, b gutter) int { return cmp.Compare(b.width, a.width) })
	for _, g := range gutters {
		left, right := sorted[:g.at], sorted[g.at:]
		if s.isColumn(left) && s.isColumn(right) {
			return [][]int{left, right}
		}
	}
	return nil
}

// isColumn reports whether a set of items is tall and wide enough to be a
// column of text: minColumnLines body lines high and minColumnWidthEm body
// sizes wide.
func (s *segmenter) isColumn(idx []int) bool {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, i := range idx {
		it := s.items[i]
		minX, minY = math.Min(minX, it.minX), math.Min(minY, it.minY)
		maxX, maxY = math.Max(maxX, it.maxX), math.Max(maxY, it.maxY)
	}
	return maxY-minY >= minColumnLines*s.body*lineHeightFactor &&
		maxX-minX >= minColumnWidthEm*s.body
}

// bands parts a region at every horizontal gap no item crosses and returns
// the bands top to bottom, a single band when no gap parts it.
func (s *segmenter) bands(idx []int) [][]int {
	sorted := slices.Clone(idx)
	slices.SortFunc(sorted, func(a, b int) int {
		return cmp.Or(cmp.Compare(s.items[b].maxY, s.items[a].maxY), s.byContent(a, b))
	})
	var out [][]int
	from := 0
	floor := s.items[sorted[0]].minY
	for k := 1; k < len(sorted); k++ {
		it := s.items[sorted[k]]
		if it.maxY < floor {
			out = append(out, sorted[from:k])
			from = k
		}
		floor = math.Min(floor, it.minY)
	}
	return append(out, sorted[from:])
}

// gutterRun returns the end of the longest run of bands from bands[from]
// whose items, taken together, leave a vertical gutter of minGutterEm body
// sizes open between their leftmost and rightmost extent.
func (s *segmenter) gutterRun(bands [][]int, from int) int {
	var covered []span
	covered = s.cover(covered, bands[from])
	end := from + 1
	for end < len(bands) {
		next := s.cover(slices.Clone(covered), bands[end])
		if !hasGutter(next, minGutterEm*s.body) {
			break
		}
		covered = next
		end++
	}
	return end
}

// span is one horizontal interval, [lo, hi].
type span struct{ lo, hi float64 }

// cover merges the horizontal extents of the items into the sorted, disjoint
// intervals covered and returns the result.
func (s *segmenter) cover(covered []span, idx []int) []span {
	for _, i := range idx {
		covered = append(covered, span{s.items[i].minX, s.items[i].maxX})
	}
	slices.SortFunc(covered, func(a, b span) int { return cmp.Compare(a.lo, b.lo) })
	merged := covered[:0]
	for _, c := range covered {
		if n := len(merged); n > 0 && c.lo <= merged[n-1].hi {
			merged[n-1].hi = math.Max(merged[n-1].hi, c.hi)
			continue
		}
		merged = append(merged, c)
	}
	return merged
}

// hasGutter reports whether sorted, disjoint intervals leave a gap of at least
// width between two of them.
func hasGutter(covered []span, width float64) bool {
	for k := 1; k < len(covered); k++ {
		if covered[k].lo-covered[k-1].hi >= width {
			return true
		}
	}
	return false
}

// pageInkBoxes returns, per text block, the box in default user space of the
// ink its glyphs paint, as text extraction places them (see
// [models.TextElement.InkBox]), and whether every block has one. The blocks
// must be the page's, in content order, over the same decoded content data.
//
// The boxes come from the extraction litho reads text with, its fonts and
// rendering matrix, rather than from a second reckoning of glyph widths here.
// A block none of whose glyphs is placed, its font missing, leaves ok false,
// and so does a glyph set in vertical writing or turned off the horizontal.
func pageInkBoxes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, data []byte, blocks []textBlock) (boxes [][4]float64, rtl bool, ok bool) {
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.WholeStream())
	if err != nil {
		return nil, false, false
	}
	x0, y0, x1, y1 := boxCorners(page.ViewedBox())
	machine := graphics.NewMachine([4]float64{x0, y0, x1, y1})
	machine.IgnoreOCVisibility = true
	if alloc != nil {
		machine.Staged = alloc.Pending()
	}
	elements, origins, err := typography.ExtractElementsWithOrigins(ctx, doc, machine, page, ops)
	if err != nil {
		doc.Logger.Debug("tagging: page text cannot be placed; keeping content order", slog.Any("err", err))
		return nil, false, false
	}
	boxes = make([][4]float64, len(blocks))
	placed := make([]bool, len(blocks))
	var rtlRunes, letterRunes int
	for k, el := range elements {
		// Vertical writing reads its lines top to bottom and its columns
		// right to left (ISO 32000-1, 9.7.4.3), and a turned glyph reads
		// along its own baseline: the segmentation reads neither.
		if el.Vertical || math.Abs(el.Angle) > maxLevelAngle {
			return nil, false, false
		}
		origin := origins[k]
		if origin < 0 || origin >= len(ranges) || el.InkBox == [4]float64{} {
			continue
		}
		off := ranges[origin].Start
		bi, found := slices.BinarySearchFunc(blocks, off, func(b textBlock, off int) int {
			switch {
			case b.endByte <= off:
				return -1
			case b.startByte > off:
				return 1
			}
			return 0
		})
		if !found {
			continue
		}
		if !placed[bi] {
			boxes[bi], placed[bi] = el.InkBox, true
		} else {
			b := &boxes[bi]
			b[0], b[1] = math.Min(b[0], el.InkBox[0]), math.Min(b[1], el.InkBox[1])
			b[2], b[3] = math.Max(b[2], el.InkBox[2]), math.Max(b[3], el.InkBox[3])
		}
		for _, r := range el.Text {
			if !unicode.IsLetter(r) {
				continue
			}
			letterRunes++
			if unicode.In(r, unicode.Hebrew, unicode.Arabic, unicode.Syriac, unicode.Thaana, unicode.Nko) {
				rtlRunes++
			}
		}
	}
	if slices.Contains(placed, false) {
		return nil, false, false
	}
	return boxes, 2*rtlRunes > letterRunes, true
}

// boxCorners returns the corners of a page box, lower left first, or zeros
// when it is not a usable rectangle, which [graphics.NewMachine] reads as no
// clip.
func boxCorners(box ast.PdfArray) (x0, y0, x1, y1 float64) {
	if !document.UsableBox(box) {
		return 0, 0, 0, 0
	}
	a, _ := document.Number(box[0])
	b, _ := document.Number(box[1])
	c, _ := document.Number(box[2])
	d, _ := document.Number(box[3])
	return math.Min(a, c), math.Min(b, d), math.Max(a, c), math.Max(b, d)
}

// classifyInReadingOrder classifies one page's content in reading order
// ([readingOrder]) and returns its elements, or false when the reading order
// is content order or cannot be told, and the caller classifies in content
// order as before. blocks is every content block of the page, rest those
// [carveTables] left, and tables the tables it carved from the others.
//
// A page turned by /Rotate, or holding text that does not read upright, keeps
// content order: the segmentation reads columns left to right and bands top
// to bottom in default user space, which is not the way such a page reads.
//
// The blocks are classified in reading order, so heading levels follow the
// order a reader meets the headings in (see [headingLevels]). A marked-content
// sequence is one contiguous run of the content (ISO 32000-1, 14.6), so two
// blocks share a sequence only when they are adjacent in it; a paragraph whose
// lines the stream sets apart, around the lines of another column, is one
// element whose lines are sequences of their own (see [classifyBlocks]).
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu, facturx, pdf20, litho: of the 1911 files
//     auto-tagging reaches, 341 have a page whose reading order is not content
//     order, 2510 pages in all, 533 of them read column by column. The trees
//     of the other pages are unchanged.
//   - govdocs thread_300_300082.pdf: every page draws its page number first.
//     govdocs thread_200_200182.pdf: a page draws its footnotes before the
//     counsel list at its head. govdocs thread_200_200412.pdf: two columns of
//     figures, each drawn apart from its caption.
//   - pdfjs bug1905623.pdf: 201 pages of vertical Japanese, whose columns the
//     segmentation read left to right, keep content order.
//   - litho, verapdf: the PDF/UA-1 and PDF/UA-2 remediations, default and
//     aggressive, of the 173 litho fixtures and the 434 veraPDF PDF/UA files
//     write the same bytes on a second run, and bytes that differ from
//     before on 3 litho fixtures only.
//   - All corpora: those remediations of the litho fixtures and of the 339
//     other files above write 1364 outputs that differ from before, and on
//     none does litho or veraPDF report a rule it did not report before, or
//     miss one it did.
func classifyInReadingOrder(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, pageIndex int, data []byte, blocks, rest []textBlock, images []imageBlock, tables []*Element) ([]*Element, bool) {
	if page.Rotate != 0 || len(rest)+len(images)+len(tables) < 2 || len(blocks) > maxOrderItems {
		return nil, false
	}
	if slices.ContainsFunc(blocks, func(b textBlock) bool { return !b.upright }) {
		return nil, false
	}
	body := bodyFontSize(rest)
	if body <= 0 {
		return nil, false
	}
	boxes, rtl, ok := pageInkBoxes(ctx, alloc, doc, page, data, blocks)
	if !ok {
		return nil, false
	}
	boxAt := make(map[int][4]float64, len(blocks))
	for i, b := range blocks {
		boxAt[b.startByte] = boxes[i]
	}

	items := make([]orderItem, 0, len(rest)+len(images)+len(tables))
	for _, b := range rest {
		items = append(items, boxItem(boxAt[b.startByte], b.startByte))
	}
	for _, img := range images {
		items = append(items, boxItem(img.box, img.startByte))
	}
	for _, t := range tables {
		items = append(items, tableItem(t, boxAt))
	}
	order := readingOrder(items, body, rtl)
	if order == nil {
		return nil, false
	}

	ordered := make([]textBlock, 0, len(rest))
	for _, i := range order {
		if i < len(rest) {
			ordered = append(ordered, rest[i])
		}
	}
	elems := classifyBlocks(ordered, pageIndex, data, boxAt)
	elems = append(elems, figureElements(images, pageIndex)...)
	elems = append(elems, tables...)
	byStart := make(map[int]*Element, len(elems))
	for _, e := range elems {
		byStart[elemStart(e)] = e
	}
	out := make([]*Element, 0, len(elems))
	for _, i := range order {
		if e, ok := byStart[items[i].start]; ok {
			out = append(out, e)
			delete(byStart, items[i].start)
		}
	}
	// Every element starts at an item, so none is left; were one left, it
	// keeps its content-order place at the end rather than be dropped.
	for _, e := range elems {
		if _, ok := byStart[elemStart(e)]; ok {
			out = append(out, e)
		}
	}
	return out, true
}

// boxItem returns the order item of a box, [minX minY maxX maxY].
func boxItem(box [4]float64, start int) orderItem {
	return orderItem{minX: box[0], minY: box[1], maxX: box[2], maxY: box[3], start: start}
}

// tableItem returns the order item of a carved table: the union of its cells'
// boxes, placed at its first cell.
func tableItem(t *Element, boxAt map[int][4]float64) orderItem {
	it := orderItem{minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1), start: elemStart(t)}
	for _, tr := range t.Kids {
		for _, td := range tr.Kids {
			if td.Span == nil {
				continue
			}
			b := boxAt[td.Span.Start]
			it.minX, it.minY = math.Min(it.minX, b[0]), math.Min(it.minY, b[1])
			it.maxX, it.maxY = math.Max(it.maxX, b[2]), math.Max(it.maxY, b[3])
		}
	}
	return it
}
