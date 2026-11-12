package odp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// tableRow is one table:table-row read, before the columns are known.
type tableRow struct {
	height float64
	cells  []*odf.Element
}

// table reads a table:table in a draw:frame (ODF 1.3, Part 3, 9.1.2 in
// 10.4.2): the column widths of its table:table-column styles, the
// heights of its rows, and each cell's text, fill, borders and spans,
// over the table template its table:template-name names (9.1.2
// table:template-name, 16.13 table:table-template) as the flags
// table:use-first-row-styles and its siblings select. Columns and rows
// with no size share the frame's.
func (r *reader) table(t *odf.Element, box compose.Rect, pc *pageCtx) *slides.Table {
	var cols []float64
	var rows []tableRow
	budget := maxTableCells
	var walk func(el *odf.Element, depth int)
	walk = func(el *odf.Element, depth int) {
		if depth > 2 {
			return
		}
		for _, k := range el.Children {
			switch {
			case k.Is(odf.NSTable, "table-column"):
				w, _ := odf.Length(r.styles.Resolve(pc.scope, odf.FamilyColumn, k.A(odf.NSTable, "style-name"), odf.ColumnProps).Get("style:column-width"))
				for range min(repeatCount(k, "number-columns-repeated"), budget) {
					cols = append(cols, w)
					budget--
				}
			case k.Is(odf.NSTable, "table-row"):
				rp := r.styles.Resolve(pc.scope, odf.FamilyRow, k.A(odf.NSTable, "style-name"), odf.RowProps)
				h, ok := odf.Length(rp.Get("style:row-height"))
				if !ok {
					h, _ = odf.Length(rp.Get("style:min-row-height"))
				}
				var cells []*odf.Element
				for _, c := range k.Children {
					if c.Is(odf.NSTable, "table-cell") || c.Is(odf.NSTable, "covered-table-cell") {
						for range min(repeatCount(c, "number-columns-repeated"), budget) {
							cells = append(cells, c)
							budget--
						}
					}
				}
				// A row of no cells still takes a cell of the grid, which
				// the columns pad it to.
				for range min(repeatCount(k, "number-rows-repeated"), max(budget, 0)/max(len(cells), 1)) {
					rows = append(rows, tableRow{height: h, cells: cells})
					budget -= max(len(cells), 1)
				}
			case k.Is(odf.NSTable, "table-columns"), k.Is(odf.NSTable, "table-header-columns"), k.Is(odf.NSTable, "table-column-group"),
				k.Is(odf.NSTable, "table-rows"), k.Is(odf.NSTable, "table-header-rows"), k.Is(odf.NSTable, "table-row-group"):
				walk(k, depth+1)
			}
		}
	}
	walk(t, 0)
	n := len(cols)
	for _, row := range rows {
		n = max(n, len(row.cells))
	}
	// Every row is padded to the widest: the grid, not the cells read,
	// holds the cap.
	if n > 0 && len(rows) > maxTableCells/n {
		rows = rows[:maxTableCells/n]
		budget = 0
	}
	if budget <= 0 {
		r.warn("table-cells", fmt.Sprintf("table cells past %d are not read", maxTableCells))
	}
	if len(rows) == 0 {
		return nil
	}
	for len(cols) < n {
		cols = append(cols, 0)
	}
	fillSizes(cols, box.Width)
	heights := make([]float64, len(rows))
	for i, row := range rows {
		heights[i] = row.height
	}
	fillSizes(heights, box.Height)
	tmpl := r.template(t)
	out := &slides.Table{ColumnWidths: cols}
	for i, row := range rows {
		tr := slides.TableRow{Height: heights[i]}
		for j := range n {
			var c *odf.Element
			if j < len(row.cells) {
				c = row.cells[j]
			}
			tr.Cells = append(tr.Cells, r.tableCell(c, tmpl.styleOf(i, j, len(rows), n), pc))
		}
		out.Rows = append(out.Rows, tr)
	}
	return out
}

// repeatCount returns a repetition attribute of el, at least 1.
func repeatCount(el *odf.Element, local string) int {
	n, err := strconv.Atoi(el.A(odf.NSTable, local))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// fillSizes shares what total leaves of the sizes set among those at 0.
func fillSizes(sizes []float64, total float64) {
	used, zero := 0.0, 0
	for _, s := range sizes {
		if s > 0 {
			used += s
		} else {
			zero++
		}
	}
	if zero == 0 {
		return
	}
	each := max(total-used, 0) / float64(zero)
	for i, s := range sizes {
		if s <= 0 {
			sizes[i] = each
		}
	}
}

// tableTemplate is the cell styles of a table:table-template (ODF 1.3,
// Part 3, 16.13) a table uses, by the parts its flags select: "" for a
// part not used.
type tableTemplate struct {
	body, firstRow, lastRow, firstCol, lastCol, oddRows, oddCols string
}

// template reads the template a table names and the parts its flags
// (9.1.2 table:use-first-row-styles, table:use-last-row-styles,
// table:use-first-column-styles, table:use-last-column-styles,
// table:use-banding-rows-styles, table:use-banding-columns-styles) use.
func (r *reader) template(t *odf.Element) tableTemplate {
	name := t.A(odf.NSTable, "template-name")
	if name == "" {
		return tableTemplate{}
	}
	var el *odf.Element
	for _, part := range []*odf.Element{r.pkg.Styles, r.pkg.Content} {
		for _, k := range odf.Section(part, "styles").All(odf.NSTable, "table-template") {
			if k.A(odf.NSTable, "name") == name || k.A(odf.NSText, "style-name") == name {
				el = k
			}
		}
		if el != nil {
			break
		}
	}
	if el == nil {
		return tableTemplate{}
	}
	part := func(local, flag string) string {
		if flag != "" && t.A(odf.NSTable, flag) != "true" {
			return ""
		}
		return el.Child(odf.NSTable, local).A(odf.NSTable, "style-name")
	}
	return tableTemplate{
		body:     part("body", ""),
		firstRow: part("first-row", "use-first-row-styles"), lastRow: part("last-row", "use-last-row-styles"),
		firstCol: part("first-column", "use-first-column-styles"), lastCol: part("last-column", "use-last-column-styles"),
		oddRows: part("odd-rows", "use-banding-rows-styles"), oddCols: part("odd-columns", "use-banding-columns-styles"),
	}
}

// styleOf returns the template's cell style for the cell at row i,
// column j of a rows by cols table: the first and last rows win over the
// first and last columns, which win over the banding, which wins over
// the body, as Impress applies a table design.
func (tt tableTemplate) styleOf(i, j, rows, cols int) string {
	switch {
	case i == 0 && tt.firstRow != "":
		return tt.firstRow
	case i == rows-1 && tt.lastRow != "":
		return tt.lastRow
	case j == 0 && tt.firstCol != "":
		return tt.firstCol
	case j == cols-1 && tt.lastCol != "":
		return tt.lastCol
	}
	// The banding counts from the first body row: the first row after a
	// header row is odd.
	bi, bj := i, j
	if tt.firstRow != "" {
		bi--
	}
	if tt.firstCol != "" {
		bj--
	}
	switch {
	case tt.oddRows != "" && bi%2 == 0:
		return tt.oddRows
	case tt.oddCols != "" && bj%2 == 0:
		return tt.oddCols
	}
	return tt.body
}

// cellProps returns the properties a cell style family:name sets for its
// fill and borders: its graphic properties (LibreOffice writes them as
// loext:graphic-properties), its table-cell and paragraph properties.
func (r *reader) cellProps(scope odf.Scope, name string) odf.Props {
	var p odf.Props
	for _, st := range r.styles.Chain(scope, odf.FamilyCell, name) {
		p = p.Over(odf.PropsOf(st.El.Child(odf.NSLOExt, "graphic-properties"))).Over(st.Props(odf.GraphicProps)).
			Over(st.Props(odf.CellProps)).Over(st.Props(odf.ParagraphProps))
	}
	return p
}

// tableCell reads one cell (ODF 1.3, Part 3, 9.1.4 and 9.1.5): its
// template style under its own, its fill, its borders (fo:border and
// its sides), its spans, and its text with its padding and vertical
// alignment.
func (r *reader) tableCell(c *odf.Element, tmpl string, pc *pageCtx) slides.TableCell {
	out := slides.TableCell{ColSpan: 1, RowSpan: 1}
	if c == nil {
		return out
	}
	if c.Is(odf.NSTable, "covered-table-cell") {
		out.Covered = true
		return out
	}
	out.ColSpan = repeatCount(c, "number-columns-spanned")
	out.RowSpan = repeatCount(c, "number-rows-spanned")
	var p odf.Props
	if tmpl != "" {
		p = r.cellProps(odf.ScopeStyles, tmpl)
	}
	p = p.Over(r.cellProps(pc.scope, c.A(odf.NSTable, "style-name")))
	out.Fill = r.fill(p, "none")
	if out.Fill.Kind == slides.FillNone {
		if col, ok := odf.ParseColor(p.Get("fo:background-color")); ok {
			out.Fill = slides.Fill{Kind: slides.FillSolid, Color: cssColor(col)}
		}
	}
	all := p.Get("fo:border")
	for i, side := range [4]string{"top", "right", "bottom", "left"} {
		v := all
		if s, ok := p.Lookup("fo:border-" + side); ok {
			v = s
		}
		out.Borders[i] = borderLine(v)
	}
	ss := &shapeStyle{scope: pc.scope, graphic: p, text: r.styleLayers(pc.scope, odf.FamilyGraphic, "")}
	if tmpl != "" {
		for _, st := range r.styles.Chain(odf.ScopeStyles, odf.FamilyCell, tmpl) {
			ss.text = append(ss.text, layerOf(st))
		}
	}
	for _, st := range r.styles.Chain(pc.scope, odf.FamilyCell, c.A(odf.NSTable, "style-name")) {
		ss.text = append(ss.text, layerOf(st))
	}
	if hasParagraphs(c) {
		out.Text = r.textBody(c, ss, pc)
		r.textBodyProps(out.Text, p, false)
		out.Text.Wrap = true
	}
	return out
}

// borderLine reads an fo:border value (ODF 1.3, Part 3, 20.183; XSL 1.0,
// 7.31.3): a width, a style and a colour in any order; none or a zero
// width draws nothing. Double, dotted and dashed borders are drawn solid.
func borderLine(v string) slides.Line {
	f := strings.Fields(v)
	if len(f) == 0 || f[0] == "none" || f[0] == "hidden" {
		return slides.Line{}
	}
	ln := slides.Line{Width: 0.75, Color: css.Color{A: 1}}
	for _, t := range f {
		if w, ok := odf.Length(t); ok {
			ln.Width = w
		} else if c, ok := odf.ParseColor(t); ok {
			ln.Color = cssColor(c)
		} else if t == "none" || t == "hidden" {
			return slides.Line{}
		}
	}
	if ln.Width <= 0 {
		return slides.Line{}
	}
	return ln
}
