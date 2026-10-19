package tagging

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// pageGeometry reads where one page's text blocks paint and which lines the
// page strokes, each once and only when a pass asks: placing the glyphs runs
// text extraction over the page, which most pages never need. blocks are the
// page's content blocks in content order, over the decoded content data.
//
// A pageGeometry belongs to one page's classification and is not safe for
// concurrent use.
type pageGeometry struct {
	alloc  *writer.ObjAllocator
	doc    *document.Document
	page   *document.Page
	data   []byte
	blocks []textBlock

	boxesRead bool
	boxAt     map[int][4]float64
	gapAt     map[int]float64
	rtl       bool
	boxesOK   bool

	linesRead bool
	lines     []models.Line
}

// inkBoxes returns the ink box of every block, keyed by its start byte, as
// [pageInkBoxes] places them, whether the page's text reads right to left,
// and whether every block has a box. The widest gap inside each block is in
// g.gapAt once it returns true.
func (g *pageGeometry) inkBoxes(ctx context.Context) (map[int][4]float64, bool, bool) {
	if !g.boxesRead {
		g.boxesRead = true
		boxes, gaps, rtl, ok := pageInkBoxes(ctx, g.alloc, g.doc, g.page, g.data, g.blocks)
		if ok {
			g.boxAt = make(map[int][4]float64, len(g.blocks))
			g.gapAt = make(map[int]float64, len(g.blocks))
			for i, b := range g.blocks {
				g.boxAt[b.startByte], g.gapAt[b.startByte] = boxes[i], gaps[i]
			}
			g.rtl, g.boxesOK = rtl, true
		}
	}
	return g.boxAt, g.rtl, g.boxesOK
}

// strokedLines returns the line segments the page's content strokes, in
// default user space ([graphics.ExtractLines]), or none when the content does
// not parse.
func (g *pageGeometry) strokedLines(ctx context.Context) []models.Line {
	if !g.linesRead {
		g.linesRead = true
		if ops, _, err := content.ParseStreamDataWithOffsets(ctx, g.data, content.WholeStream()); err == nil {
			g.lines = graphics.ExtractLines(ops)
		}
	}
	return g.lines
}

// proseColumns reports whether a grid the table pass found, its rows over
// g.blocks, reads as columns of running text set line by line rather than as
// a table: in every column of it, most cells read as a line of running text
// ([pageGeometry.proseLine]), and no stroked rule crosses its gutters
// ([pageGeometry.ruled]).
//
// ISO 14289-1, 7.5 reserves Table for content whose meaning is tabular, and 7.2
// asks the structure tree to follow the reading order. Two columns of prose a
// producer sets line by line, each line on its neighbour's baseline, align row
// by row as a table does; read as one, the text crosses the gutter at every
// line. What the cells of a table hold is short, a label, a figure, a date, so
// at least one of its columns is narrower than a column of prose or sets its
// figures apart inside one text object; and a table whose every cell is a
// line of text is told by its ruling. A key column beside long values keeps
// its grid a table: reading its keys as a column would read every key before
// any value.
//
// Without the ink boxes of every block the widths cannot be told, and the grid
// stays a table, as the reading order then keeps content order too.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, litho: of the 1617 files auto-tagging reaches, 115 carry
//     1402 tables. Five grids in five files now read as columns: two columns
//     of prose (govdocs thread_300_300104.pdf, pdfjs issue11518.pdf), the
//     captions under a two-by-two array of gradients (pdfjs
//     radial_gradients.pdf, shading_extend.pdf) and a screen capture drawn in
//     characters (govdocs thread_100_100672.pdf). No other tree changes.
//   - govdocs thread_400_400901.pdf: a radionuclide table sets each side of a
//     row in one text object, its figures a gutter apart; read by width alone
//     its 21 tables read as columns. govdocs thread_400_400393.pdf and
//     thread_000_000351.pdf: an option beside its description and a folder
//     number beside its contents stay tables, one column of each falling
//     short of a majority of prose lines; read by a half of the cells and ten
//     sizes of width, both read as columns.
func (g *pageGeometry) proseColumns(ctx context.Context, rows []blockRow) bool {
	boxAt, _, ok := g.inkBoxes(ctx)
	if !ok || len(rows) == 0 {
		return false
	}
	for c := range rows[0].idx {
		lines := 0
		for _, r := range rows {
			if g.proseLine(g.blocks[r.idx[c]]) {
				lines++
			}
		}
		if 2*lines <= len(rows) {
			return false
		}
	}
	return !g.ruled(ctx, rows, boxAt)
}

// minProseLineEm is the narrowest line, in its own font sizes, that reads as a
// line of a column of running text. A tuning constant, not spec-mandated: at
// about two characters to the size, it is some 24 characters, half the
// shortest measure a column of prose is commonly set to, while the labels of a
// form and the captions under a row of figures fall short of it. It is above
// minColumnWidthEm, so the reading order takes such a column as one.
const minProseLineEm = 12.0

// proseLine reports whether a block reads as a line of a column of running
// text: a single line, minProseLineEm of its own font size wide, and with no
// gap inside it as wide as a gutter, minGutterEm of its font size, as a row of
// figures one text object sets apart has. g.inkBoxes must have returned true.
func (g *pageGeometry) proseLine(b textBlock) bool {
	if b.fontSize <= 0 || len(b.lines) != 1 {
		return false
	}
	box := g.boxAt[b.startByte]
	return box[2]-box[0] >= minProseLineEm*b.fontSize && g.gapAt[b.startByte] < minGutterEm*b.fontSize
}

// ruled reports whether a stroked horizontal line crosses every gutter of the
// grid within a line height of its rows: the rule a producer draws between the
// rows of a table, or along its top or foot. A rule that stops short of a
// gutter, such as one under a column's heading, rules no row of the grid.
//
// Only stroked lines are read: a rule painted as a filled rectangle is not, and
// a grid of wide cells ruled that way reads as columns.
func (g *pageGeometry) ruled(ctx context.Context, rows []blockRow, boxAt map[int][4]float64) bool {
	lines := g.strokedLines(ctx)
	if len(lines) == 0 {
		return false
	}
	cols := len(rows[0].idx)
	left := make([]float64, cols)
	right := make([]float64, cols)
	for c := range cols {
		left[c], right[c] = math.Inf(1), math.Inf(-1)
	}
	top, bottom, size := math.Inf(-1), math.Inf(1), 0.0
	for _, r := range rows {
		for c, bi := range r.idx {
			b := g.blocks[bi]
			box := boxAt[b.startByte]
			left[c], right[c] = math.Min(left[c], box[0]), math.Max(right[c], box[2])
			top, bottom = math.Max(top, b.maxUY), math.Min(bottom, b.minUY)
			size = math.Max(size, b.fontSize)
		}
	}
	lead := lineHeightFactor * size
	for _, l := range lines {
		if math.Abs(l.Y1-l.Y2) > rowBaselineTol {
			continue
		}
		y := (l.Y1 + l.Y2) / 2
		if y < bottom-lead || y > top+lead {
			continue
		}
		lo, hi := math.Min(l.X1, l.X2), math.Max(l.X1, l.X2)
		crosses := true
		for c := 1; c < cols; c++ {
			if lo > right[c-1] || hi < left[c] {
				crosses = false
				break
			}
		}
		if crosses {
			return true
		}
	}
	return false
}
