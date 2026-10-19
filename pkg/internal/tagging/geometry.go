package tagging

import (
	"cmp"
	"context"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// pageGeometry reads where one page's text blocks paint and which rules the
// page strokes or fills, each once and only when a pass asks: placing the glyphs runs
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
	filled    []models.Line
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

// paths returns the line segments the page's content strokes
// ([graphics.ExtractLines]) and the horizontal rules it paints as thin filled
// rectangles ([filledRules]), in default user space, or none when the content
// does not parse.
func (g *pageGeometry) paths(ctx context.Context) (stroked, filled []models.Line) {
	if !g.linesRead {
		g.linesRead = true
		if ops, _, err := content.ParseStreamDataWithOffsets(ctx, g.data, content.WholeStream()); err == nil {
			g.lines = graphics.ExtractLines(ops)
			g.filled = filledRules(graphics.ExtractFilledBoxes(ops))
		}
	}
	return g.lines, g.filled
}

// maxRuleThickness is the tallest filled rectangle, in default user space
// units, that reads as a horizontal rule rather than as a filled area. A tuning
// constant, not spec-mandated: ISO 32000-1, 8.5.3.3 paints a filled path and a
// stroked one alike, so a producer may draw a rule either way, and Word through
// PDFMaker or Distiller, LibreOffice and TeX (\hrule, booktabs) fill
// rectangles a rule's width tall. Word's borders run from a quarter point to a
// point and a half and TeX's from 0.4 to 0.8 points, while a shaded row or
// panel is as tall as the text it sits behind.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, litho: 653 files fill horizontal bars at most 2 points
//     tall and 36 wide; the commonest heights are 0.48 (14975 bars), 0.72,
//     0.96, 0.30 and 1.00 points, and 1.50 is the commonest above one point.
const maxRuleThickness = 2.0

// ruleJoinGap is the widest gap, in default user space units, between two
// filled bars on one line that still reads as one rule. A tuning constant, not
// spec-mandated: Word draws a table's border cell by cell, each cell's segment
// meeting the next at the square it fills at the corner, so the segments abut;
// a dashed rule's gaps are wider and stay apart.
const ruleJoinGap = 1.0

// filledRules returns the horizontal rules among the boxes of filled paths: the
// bars at most maxRuleThickness tall and wider than they are tall, along their
// centre lines, with the bars that continue one another on one line, at most
// ruleJoinGap apart, joined into one rule.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, litho: 492 files fill bars at most 2 points tall and
//     under 36 wide, the per-cell segments of a border among them.
func filledRules(boxes [][4]float64) []models.Line {
	var bars []models.Line
	for _, b := range boxes {
		h, w := b[3]-b[1], b[2]-b[0]
		if h <= maxRuleThickness && w > h {
			y := (b[1] + b[3]) / 2
			bars = append(bars, models.Line{X1: b[0], Y1: y, X2: b[2], Y2: y})
		}
	}
	slices.SortFunc(bars, func(a, b models.Line) int { return cmp.Compare(a.Y1, b.Y1) })
	var rules []models.Line
	for start := 0; start < len(bars); {
		end := start + 1
		for end < len(bars) && bars[end].Y1-bars[start].Y1 <= maxRuleThickness/2 {
			end++
		}
		line := bars[start:end]
		slices.SortFunc(line, func(a, b models.Line) int { return cmp.Compare(a.X1, b.X1) })
		first := len(rules)
		for _, bar := range line {
			if n := len(rules); n > first && bar.X1 <= rules[n-1].X2+ruleJoinGap {
				rules[n-1].X2 = math.Max(rules[n-1].X2, bar.X2)
				continue
			}
			rules = append(rules, bar)
		}
		start = end
	}
	return rules
}

// proseColumns reports whether a grid the table pass found, its rows over
// g.blocks, reads as columns of running text set line by line rather than as
// a table: in every column of it, most cells read as a line of running text
// ([pageGeometry.proseLine]), and no rule crosses its gutters
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

// ruled reports whether a horizontal rule crosses every gutter of the grid
// within a line height of its rows: the rule a producer draws between the rows
// of a table, or along its top or foot, stroked as a line or filled as a
// hairline rectangle ([filledRules]). A rule that stops short of a gutter, such
// as one under a column's heading, rules no row of the grid, and a rule more
// than a line clear of it, such as one under a page's title, rules none either.
//
// Vertical rules are not read: a rule down the gutter between two columns of
// prose is as common in a page's layout as between the columns of a table.
func (g *pageGeometry) ruled(ctx context.Context, rows []blockRow, boxAt map[int][4]float64) bool {
	stroked, filled := g.paths(ctx)
	if len(stroked) == 0 && len(filled) == 0 {
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
	for _, l := range slices.Concat(stroked, filled) {
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
