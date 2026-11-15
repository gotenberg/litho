package formula

import (
	"fmt"
	"strconv"
	"strings"

	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// The span bounds of the HTML Standard's algorithm for processing rows
// (13.2.6 in the tables model, "colspan" clamped to 1000 and "rowspan" to
// 65534), which MathML Core, 3.5.3 applies to columnspan and rowspan.
const (
	maxColSpan = 1000
	maxRowSpan = 65534
)

// maxTableSlots bounds the slots, rows times columns, a table's spans
// reach. Anti-DoS guard, not spec-mandated: a span is up to 1000
// columns and 65534 rows, and the slots are a grid in memory; real
// matrices hold tens of slots.
const maxTableSlots = 1 << 20

// tableCell is one cell of an mtable laid out.
type tableCell struct {
	content          *box
	st               *Style
	row, col         int
	rowSpan, colSpan int
	// mtd marks a cell of an mtd element, whose own padding, border and
	// background the table draws over its whole slot; another child is
	// an anonymous cell around a box that holds its own.
	mtd bool
}

// mtable lays the table e out, styled st (MathML Core, 3.5): a CSS table
// of its mtr rows and their mtd cells, each cell's content an anonymous
// mrow centred in its column on its row's baseline, the table's middle on
// the math axis. A child of mtable that is not an mtr is a row of one
// cell, and a child of mtr that is not an mtd a cell, as CSS 2.1, 17.2.1
// wraps them in anonymous table boxes. Rows and columns have no spacing,
// border-spacing being 0 in MathML (the user agent stylesheet sets none).
func (l *layouter) mtable(e *Element, st *Style, depth int) *box {
	var cells []tableCell
	occupied := [][]bool{}
	rows := 0
	slot := func(r, c int) bool {
		return r < len(occupied) && c < len(occupied[r]) && occupied[r][c]
	}
	take := func(r, c int) {
		for len(occupied) <= r {
			occupied = append(occupied, nil)
		}
		for len(occupied[r]) <= c {
			occupied[r] = append(occupied[r], false)
		}
		occupied[r][c] = true
	}
	kids, index := inFlow(e)
	total := 0
	for ri, r := range kids {
		if total >= MaxTableCells {
			l.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("formula table cells past the first %d were left out", MaxTableCells))
			break
		}
		if !l.admit(depth + 1) {
			break
		}
		rst := l.childStyle(r, e, index[ri], st)
		rowKids, rowIndex := []*Element{r}, []int{index[ri]}
		parent, pst := e, st
		if r.Name == "mtr" {
			rowKids, rowIndex = inFlow(r)
			parent, pst = r, rst
		}
		col := 0
		for ci, ce := range rowKids {
			if total >= MaxTableCells {
				break
			}
			if !l.admit(depth + 2) {
				break
			}
			total++
			for slot(ri, col) {
				col++
			}
			cst := l.childStyle(ce, parent, rowIndex[ci], pst)
			cell := tableCell{st: cst, row: ri, col: col, rowSpan: 1, colSpan: 1, mtd: ce.Name == "mtd"}
			if cell.mtd {
				cell.colSpan = spanAttr(ce, "columnspan", 1, maxColSpan)
				cell.rowSpan = spanAttr(ce, "rowspan", 0, maxRowSpan)
				cell.content = l.row(ce, cst, lctx{}, depth+2)
			} else {
				cell.content = l.layout(ce, cst, lctx{}, depth+2)
			}
			if cell.rowSpan == 0 {
				// rowspan="0" spans to the end of the row group, which
				// holds every row of the table.
				cell.rowSpan = len(kids) - ri
			}
			cell.rowSpan = max(1, min(cell.rowSpan, len(kids)-ri))
			if (ri+cell.rowSpan)*(col+cell.colSpan) > maxTableSlots {
				l.warn(warn.LimitReached, warn.Approximated, fmt.Sprintf("formula table spans past %d slots were drawn as one cell", maxTableSlots))
				cell.rowSpan, cell.colSpan = 1, 1
			}
			for dr := range cell.rowSpan {
				for dc := range cell.colSpan {
					take(ri+dr, col+dc)
				}
			}
			col += cell.colSpan
			cells = append(cells, cell)
		}
		rows = ri + 1
	}
	b := &box{}
	if len(cells) == 0 {
		return b
	}
	cols := 0
	for _, c := range cells {
		cols = max(cols, c.col+c.colSpan)
	}
	widths := make([]float64, cols)
	asc := make([]float64, rows)
	desc := make([]float64, rows)
	outer := func(c tableCell) (w, a, d float64) {
		if !c.mtd {
			return c.content.w, c.content.asc, c.content.desc
		}
		p, bd := c.st.Padding, c.st.Border
		w = p[left] + bd[left] + c.content.w + p[right] + bd[right]
		a = p[top] + bd[top] + c.content.asc
		d = c.content.desc + p[bottom] + bd[bottom]
		return w, a, d
	}
	for _, c := range cells {
		w, a, d := outer(c)
		if c.colSpan == 1 {
			widths[c.col] = max(widths[c.col], w)
		}
		asc[c.row] = max(asc[c.row], a)
		if c.rowSpan == 1 {
			desc[c.row] = max(desc[c.row], d)
		}
	}
	for _, c := range cells {
		w, a, d := outer(c)
		if c.colSpan > 1 {
			have := 0.0
			for k := c.col; k < c.col+c.colSpan; k++ {
				have += widths[k]
			}
			if w > have {
				add := (w - have) / float64(c.colSpan)
				for k := c.col; k < c.col+c.colSpan; k++ {
					widths[k] += add
				}
			}
		}
		if c.rowSpan > 1 {
			have := asc[c.row]
			for k := c.row; k < c.row+c.rowSpan; k++ {
				have += desc[k]
				if k > c.row {
					have += asc[k]
				}
			}
			if need := a + d; need > have {
				desc[c.row+c.rowSpan-1] += need - have
			}
		}
	}
	colX := make([]float64, cols+1)
	for k, w := range widths {
		colX[k+1] = colX[k] + w
	}
	rowY := make([]float64, rows+1)
	for k := range rows {
		rowY[k+1] = rowY[k] + asc[k] + desc[k]
	}
	height := rowY[rows]
	axis := l.faceOf(st).constant(mt.AxisHeight, st.Size)
	b.w = colX[cols]
	b.asc = float64(height/2) + axis
	b.desc = float64(height/2) - axis
	b.inkAsc, b.inkDesc = b.asc, b.desc
	tableTop := -b.asc
	for ri, r := range kids[:rows] {
		if r.Name == "mtr" && r.Style != nil && !r.Style.Hidden {
			b.decorate(0, tableTop+rowY[ri], b.w, rowY[ri+1]-rowY[ri], r.Style.Background)
		}
	}
	for _, c := range cells {
		x0, w := colX[c.col], colX[c.col+c.colSpan]-colX[c.col]
		y0, h := tableTop+rowY[c.row], rowY[c.row+c.rowSpan]-rowY[c.row]
		baseline := y0 + asc[c.row]
		inner := c.content
		if c.mtd && !c.st.Hidden {
			bd := c.st.Border
			b.decorate(x0, y0, w, h, c.st.Background)
			b.decorate(x0, y0, w, bd[top], c.st.BorderColor[top])
			b.decorate(x0, y0+h-bd[bottom], w, bd[bottom], c.st.BorderColor[bottom])
			b.decorate(x0, y0, bd[left], h, c.st.BorderColor[left])
			b.decorate(x0+w-bd[right], y0, bd[right], h, c.st.BorderColor[right])
		}
		ow, _, _ := outer(c)
		x := x0 + float64((w-ow)/2)
		if c.mtd {
			x += c.st.Padding[left] + c.st.Border[left]
		}
		b.add(inner, x, baseline)
	}
	return b
}

// spanAttr reads the span attribute name of e as the HTML Standard reads
// colspan and rowspan: a non-negative integer, def when absent or not
// one, at most limit.
func spanAttr(e *Element, name string, def, limit int) int {
	v, ok := e.Attr(name)
	if !ok {
		return 1
	}
	v = strings.TrimSpace(v)
	end := 0
	for end < len(v) && v[end] >= '0' && v[end] <= '9' {
		end++
	}
	n, err := strconv.ParseInt(v[:end], 10, 32)
	if err != nil {
		return 1
	}
	if n == 0 {
		return def
	}
	return int(min(n, int64(limit))) // fits int: at most limit
}
