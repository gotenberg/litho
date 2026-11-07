package odt

import (
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// maxRepeat bounds what one table:number-columns-repeated or
// table:number-rows-repeated expands to, and maxTableCells the cells of
// one table: 1,024 and 100,000. Anti-DoS guard, not spec-mandated: a text
// document's table has tens of columns and hundreds of rows, while a
// spreadsheet pasted into one may repeat an empty row a million times.
const (
	maxRepeat     = 1024
	maxTableCells = 100_000
)

// repeat returns the count of a number-*-repeated attribute, at least 1
// and at most [maxRepeat].
func repeat(el *odf.Element, attr string) int {
	n, err := strconv.Atoi(el.A(odf.NSTable, attr))
	if err != nil || n < 1 {
		return 1
	}
	return min(n, maxRepeat)
}

// tableBuild is one table being built.
type tableBuild struct {
	cells int
	ctx   blockCtx
}

// table builds a table:table (ODF 1.3, Part 3, 9.1) through the CSS table
// model: a fixed-layout table of its columns' widths, its header rows
// repeated on every page, its rows and cells with their spans, borders,
// padding, backgrounds and vertical alignment.
func (b *builder) table(c *odf.Element, ctx blockCtx) []*layout.Node {
	props := b.styles.Resolve(b.scope, odf.FamilyTable, c.A(odf.NSTable, "style-name"), odf.TableProps)
	st := baseStyle()
	st.Display = css.Table
	st.TableLayout = css.TableFixed
	st.BorderCollapse = css.CollapseBorders
	if props.Get("table:border-model") == "separating" {
		st.BorderCollapse = css.SeparateBorders
	}
	for i, s := range sideNames {
		if pt, ok := odf.Length(props.Get("fo:margin-" + s)); ok {
			st.Margin[i] = css.Pts(pt)
		}
	}
	width, hasWidth := odf.Length(props.Get("style:width"))
	switch props.Get("table:align") {
	case "center":
		st.Margin[css.Left], st.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	case "right":
		st.Margin[css.Left] = css.Dimension{Kind: css.Auto}
	case "margins":
		hasWidth = false
		st.Width = css.Dimension{Kind: css.Percent, Value: 1}
		st.BoxSizing = css.BorderBox
		if m := st.Margin[css.Left].Value + st.Margin[css.Right].Value; m != 0 {
			st.Width = css.Dimension{Kind: css.Auto}
		}
	}
	if hasWidth && width > 0 {
		st.Width = css.Pts(width)
	}
	if v := props.Get("fo:background-color"); v != "" {
		if col, ok := odf.ParseColor(v); ok {
			st.BackgroundColor = cssColor(col)
		}
	}
	st.BreakBefore = pageBreak(props.Get("fo:break-before"), css.BreakAuto)
	st.BreakAfter = pageBreak(props.Get("fo:break-after"), css.BreakAuto)
	if props.Get("fo:keep-with-next") == "always" {
		st.BreakAfter = css.BreakAvoid
	}
	if props.Get("style:may-break-between-rows") == "false" {
		st.BreakInside = css.InsideAvoid
	}
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "Table", ID: c.A(odf.NSTable, "name")})
	tb := &tableBuild{ctx: ctx}
	tb.ctx.item, tb.ctx.indent, tb.ctx.list = nil, nil, nil
	n.Children = b.tableParts(c, tb, false)
	if !hasWidth && st.Width.Kind == css.Auto {
		if sum := columnsWidth(n.Children); sum > 0 {
			st.Width = css.Pts(sum)
		}
	}
	return []*layout.Node{n}
}

// columnsWidth returns the sum of the widths of the column nodes of kids.
func columnsWidth(kids []*layout.Node) float64 {
	sum := 0.0
	for _, k := range kids {
		if k.Style.Display == css.TableColumn && k.Style.Width.Kind == css.Points {
			sum += k.Style.Width.Value * float64(max(k.Span, 1))
		}
	}
	return sum
}

// tableParts builds the columns, row groups and rows of el, a table or
// one of its groups; header marks the rows of table:table-header-rows.
func (b *builder) tableParts(el *odf.Element, tb *tableBuild, header bool) []*layout.Node {
	var out []*layout.Node
	for _, c := range el.Children {
		if b.err != nil {
			return out
		}
		if c.Name.Space != odf.NSTable {
			continue
		}
		switch c.Name.Local {
		case "table-column":
			out = append(out, b.column(c))
		case "table-columns", "table-header-columns", "table-column-group":
			out = append(out, b.tableParts(c, tb, header)...)
		case "table-header-rows":
			g := baseStyle()
			g.Display = css.TableHeaderGroup
			out = append(out, b.node(&layout.Node{Kind: layout.Element, Style: g, Role: "THead", Children: b.tableParts(c, tb, true)}))
		case "table-rows", "table-row-group":
			out = append(out, b.tableParts(c, tb, header)...)
		case "table-row":
			for range repeat(c, "number-rows-repeated") {
				if tb.cells > maxTableCells {
					b.warn("table-cells", "a table's rows past its cell cap are left out")
					return out
				}
				out = append(out, b.row(c, tb, header))
			}
		}
	}
	return out
}

// column builds a table:table-column (9.1.6): a column node of its
// style's width, spanning its repeat count.
func (b *builder) column(c *odf.Element) *layout.Node {
	st := baseStyle()
	st.Display = css.TableColumn
	props := b.styles.Resolve(b.scope, odf.FamilyColumn, c.A(odf.NSTable, "style-name"), odf.ColumnProps)
	if w, ok := odf.Length(props.Get("style:column-width")); ok && w > 0 {
		st.Width = css.Pts(w)
	}
	return b.node(&layout.Node{Kind: layout.Element, Style: st, Span: repeat(c, "number-columns-repeated")})
}

// row builds a table:table-row (9.1.3): its height, minimum height,
// keep-together and background, and its cells.
func (b *builder) row(c *odf.Element, tb *tableBuild, header bool) *layout.Node {
	st := baseStyle()
	st.Display = css.TableRow
	props := b.styles.Resolve(b.scope, odf.FamilyRow, c.A(odf.NSTable, "style-name"), odf.RowProps)
	if h, ok := odf.Length(props.Get("style:row-height")); ok && h > 0 {
		st.Height = css.Pts(h)
	}
	if h, ok := odf.Length(props.Get("style:min-row-height")); ok && h > 0 {
		st.Height = css.Pts(h)
	}
	if props.Get("fo:keep-together") == "always" {
		st.BreakInside = css.InsideAvoid
	}
	if v := props.Get("fo:background-color"); v != "" {
		if col, ok := odf.ParseColor(v); ok {
			st.BackgroundColor = cssColor(col)
		}
	}
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "TR"})
	for _, cell := range c.Children {
		if !cell.Is(odf.NSTable, "table-cell") {
			// A covered cell is the room a spanning cell takes.
			continue
		}
		for range repeat(cell, "number-columns-repeated") {
			tb.cells++
			if tb.cells > maxTableCells {
				return n
			}
			n.Children = append(n.Children, b.cell(cell, tb, header))
		}
	}
	return n
}

// cellAligns maps style:vertical-align of a cell (ODF 1.3, Part 3,
// 20.386) to css.
var cellAligns = map[string]css.AlignKind{"top": css.AlignTop, "middle": css.Middle, "bottom": css.AlignBottom}

// cell builds a table:table-cell (9.1.4): its spans, borders, padding,
// background and vertical alignment, and its content.
func (b *builder) cell(c *odf.Element, tb *tableBuild, header bool) *layout.Node {
	st := baseStyle()
	st.Display = css.TableCell
	props := b.styles.Resolve(b.scope, odf.FamilyCell, c.A(odf.NSTable, "style-name"), odf.CellProps)
	var pad [4]float64
	st.Border, pad = foldBox(st.Border, pad, props)
	for i := range pad {
		st.Padding[i] = css.Pts(pad[i])
	}
	if v := props.Get("fo:background-color"); v != "" {
		if col, ok := odf.ParseColor(v); ok {
			st.BackgroundColor = cssColor(col)
		}
	}
	st.VerticalAlign = css.VerticalAlign{Kind: css.AlignTop}
	if k, ok := cellAligns[props.Get("style:vertical-align")]; ok {
		st.VerticalAlign = css.VerticalAlign{Kind: k}
	}
	role := "TD"
	if header {
		role = "TH"
	}
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: role})
	if v, err := strconv.Atoi(c.A(odf.NSTable, "number-columns-spanned")); err == nil && v > 1 {
		n.ColSpan = min(v, maxRepeat)
	}
	if v, err := strconv.Atoi(c.A(odf.NSTable, "number-rows-spanned")); err == nil && v > 1 {
		n.RowSpan = min(v, maxRepeat)
	}
	n.Children = b.blocks(c, tb.ctx)
	return n
}
