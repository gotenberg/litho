package pptx

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// maxTableColumns and maxTableRows cap a table's grid, maxTableCells its
// cells. Anti-DoS guards, not spec-mandated: PowerPoint holds at most 75
// columns and 75 rows.
const (
	maxTableColumns = 1000
	maxTableRows    = 1000
	maxTableCells   = 100_000
)

// The default cell margins (ECMA-376 Part 1, 21.1.3.17 tcPr: marL and
// marR 91440 EMU, marT and marB 45720 EMU).
const (
	cellInsetLR = 7.2
	cellInsetTB = 3.6
)

// readTableStyles reads the presentation's table styles part (20.1.4.2.27
// tblStyleLst): its styles by ID and its default style's ID.
func (r *reader) readTableStyles() {
	r.tableStyles = map[string]*ooxml.Element{}
	name := r.related(r.main, relTableStyles)
	if name == "" {
		return
	}
	root, err := r.pkg.ParseXML(r.ctx, name, understood)
	if err != nil {
		r.warn("malformed-part", fmt.Sprintf("the table styles %s do not parse: %v", name, err))
		return
	}
	r.defTable = strings.ToUpper(attr(root, "def"))
	for _, s := range root.Kids() {
		if s.Is(ooxml.NSA, "tblStyle") {
			if id := strings.ToUpper(attr(s, "styleId")); id != "" {
				if _, dup := r.tableStyles[id]; !dup {
					r.tableStyles[id] = s
				}
			}
		}
	}
}

// tableStyle returns the table style of a table's properties: its inline
// a:tableStyle, the style its a:tableStyleId names in the presentation's
// part or among the built-in styles, the presentation's default style for
// a table naming none, as Impress applies it; nil for none.
func (r *reader) tableStyle(pr *ooxml.Element) *ooxml.Element {
	if s := pr.Child(ooxml.NSA, "tableStyle"); s != nil {
		return s
	}
	id := r.defTable
	if e := pr.Child(ooxml.NSA, "tableStyleId"); e != nil {
		id = strings.ToUpper(strings.TrimSpace(e.Text))
	}
	if id == "" {
		return nil
	}
	if s, ok := r.tableStyles[id]; ok {
		return s
	}
	s := builtinTableStyle(r.ctx, id)
	if s == nil {
		r.warn("approximated", fmt.Sprintf("table style %s is not one litho knows: its tables print without it", id))
		return nil
	}
	r.tableStyles[id] = s
	return s
}

// table reads an a:tbl (21.1.3.13) of a frame of box: its grid, rows and
// cells, each cell's text, margins, anchor, fill and borders, its own
// properties over the parts of the table style the table's flags turn on
// (20.1.4.2: wholeTbl, the row then the column bands, firstCol, lastCol,
// firstRow, lastRow and the corner cells, in that order). It returns
// ctx's error when ctx ends first.
func (r *reader) table(c *shapeCtx, tbl *ooxml.Element) (*slides.Table, error) {
	if tbl == nil {
		return nil, nil
	}
	pr := tbl.Child(ooxml.NSA, "tblPr")
	t := &slides.Table{}
	for _, g := range tbl.Child(ooxml.NSA, "tblGrid").Kids() {
		if !g.Is(ooxml.NSA, "gridCol") {
			continue
		}
		if len(t.ColumnWidths) >= maxTableColumns {
			r.warn("too-large", fmt.Sprintf("a table has more than %d columns: the rest are left out", maxTableColumns))
			break
		}
		t.ColumnWidths = append(t.ColumnWidths, max(0, emuAttr(g, "w")))
	}
	ncols := len(t.ColumnWidths)
	if ncols == 0 {
		return nil, nil
	}
	var trs []*ooxml.Element
	for _, tr := range tbl.Kids() {
		if tr.Is(ooxml.NSA, "tr") {
			if len(trs) >= maxTableRows || (len(trs)+1)*ncols > maxTableCells {
				r.warn("too-large", fmt.Sprintf("a table has more than %d rows or %d cells: the rest are left out", maxTableRows, maxTableCells))
				break
			}
			trs = append(trs, tr)
		}
	}
	if len(trs) == 0 {
		return nil, nil
	}
	ts := &tableStyle{
		style: r.tableStyle(pr),
		flags: map[string]bool{},
		rows:  len(trs), cols: ncols,
	}
	for _, f := range []string{"firstRow", "firstCol", "lastRow", "lastCol", "bandRow", "bandCol"} {
		ts.flags[f] = boolAttr(pr, f, false)
	}
	fc := &fillCtx{sc: c.sc, part: c.part}
	background, hasBackground := r.fillOf(fc, pr)
	if !hasBackground {
		background, hasBackground = r.tableBackground(fc, ts.style)
	}
	for i, tr := range trs {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
		row := slides.TableRow{Height: max(0, emuAttr(tr, "h"))}
		j := 0
		for _, tc := range tr.Kids() {
			if !tc.Is(ooxml.NSA, "tc") || j >= ncols {
				continue
			}
			cell := r.cell(c, tc, ts, i, j)
			if hasBackground {
				cell.Fill = overBackground(cell.Fill, background)
			}
			row.Cells = append(row.Cells, cell)
			j++
		}
		for ; j < ncols; j++ {
			row.Cells = append(row.Cells, slides.TableCell{ColSpan: 1, RowSpan: 1})
		}
		t.Rows = append(t.Rows, row)
	}
	return t, nil
}

// tableBackground returns the background a table style gives its table
// (20.1.4.2.25 tblBg: a fill, or a fillRef into the theme's fill styles),
// false for none.
func (r *reader) tableBackground(fc *fillCtx, style *ooxml.Element) (slides.Fill, bool) {
	bg := style.Child(ooxml.NSA, "tblBg")
	if bg == nil {
		return slides.Fill{}, false
	}
	if ref := bg.Child(ooxml.NSA, "fillRef"); ref != nil {
		f := r.styleFill(fc, ref)
		return f, f.Kind != slides.FillNone
	}
	return r.fillOf(fc, bg)
}

// overBackground returns a cell's fill over its table's background bg as
// Impress imports it: the background's colour where the cell has no fill,
// a translucent solid fill blended over that colour into an opaque one,
// any other fill as it is. The background paints as one colour, its best
// solid colour: a gradient's first stop, its second when it has more than
// two (oox FillProperties::getBestSolidColor), a pattern's background.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/bnc480256's "Themed Style 1 - Accent 1" table, its tblBg
//     the theme's second fill style, prints its second band C6DDFF, the
//     gradient's second stop, and its first band 9BC3EE, accent1 5B9BD5
//     at 40% over it; its "Themed Style 2" table 4697E0 and 6BACE6, white
//     at 20% over it.
func overBackground(cell, bg slides.Fill) slides.Fill {
	var col css.Color
	switch bg.Kind {
	case slides.FillSolid:
		col = bg.Color
	case slides.FillGradient:
		stops := bg.Gradient.Stops
		if len(stops) == 0 {
			return cell
		}
		col = stops[0].Color
		if len(stops) > 2 {
			col = stops[1].Color
		}
	case slides.FillPattern:
		col = bg.Background
	case slides.FillNone, slides.FillPicture:
		if cell.Kind == slides.FillNone {
			return bg
		}
		return cell
	}
	switch {
	case cell.Kind == slides.FillNone:
		return slides.Fill{Kind: slides.FillSolid, Color: col}
	case cell.Kind == slides.FillSolid && cell.Color.A < 1:
		a := max(0, cell.Color.A)
		mix := func(c, b float64) float64 { return a*c + (1-a)*b }
		return slides.Fill{Kind: slides.FillSolid, Color: css.Color{R: mix(cell.Color.R, col.R), G: mix(cell.Color.G, col.G), B: mix(cell.Color.B, col.B), A: max(col.A, a)}}
	}
	return cell
}

// tableStyle is a table style applied to one table: the style, the flags
// of its table's properties and the grid's size.
type tableStyle struct {
	style      *ooxml.Element
	flags      map[string]bool
	rows, cols int
}

// region says where a style part applies: the whole table, a row band, a
// column band, or one cell.
type region uint8

const (
	regionTable region = iota
	regionRow
	regionColumn
	regionCell
)

// partRef is one table style part that applies to a cell, with its region.
type partRef struct {
	el *ooxml.Element
	rg region
}

// parts returns the style parts that apply to the cell at row i and
// column j, in the order they apply.
func (ts *tableStyle) parts(i, j int) []partRef {
	if ts.style == nil {
		return nil
	}
	var out []partRef
	add := func(name string, rg region) {
		if p := ts.style.Child(ooxml.NSA, name); p != nil {
			out = append(out, partRef{p, rg})
		}
	}
	first, last := ts.flags["firstRow"], ts.flags["lastRow"]
	firstC, lastC := ts.flags["firstCol"], ts.flags["lastCol"]
	add("wholeTbl", regionTable)
	if ts.flags["bandRow"] {
		k := i
		if first {
			k--
		}
		if k >= 0 && (!last || i != ts.rows-1) {
			if k%2 == 0 {
				add("band1H", regionRow)
			} else {
				add("band2H", regionRow)
			}
		}
	}
	if ts.flags["bandCol"] {
		k := j
		if firstC {
			k--
		}
		if k >= 0 && (!lastC || j != ts.cols-1) {
			if k%2 == 0 {
				add("band1V", regionColumn)
			} else {
				add("band2V", regionColumn)
			}
		}
	}
	if firstC && j == 0 {
		add("firstCol", regionColumn)
	}
	if lastC && j == ts.cols-1 {
		add("lastCol", regionColumn)
	}
	if first && i == 0 {
		add("firstRow", regionRow)
	}
	if last && i == ts.rows-1 {
		add("lastRow", regionRow)
	}
	switch {
	case first && i == 0 && firstC && j == 0:
		add("nwCell", regionCell)
	case first && i == 0 && lastC && j == ts.cols-1:
		add("neCell", regionCell)
	case last && i == ts.rows-1 && firstC && j == 0:
		add("swCell", regionCell)
	case last && i == ts.rows-1 && lastC && j == ts.cols-1:
		add("seCell", regionCell)
	}
	return out
}

// borderName returns the tcBdr child of a part that strokes side of the
// cell at row i and column j: the part's own side at the edge of its
// region, its insideH or insideV within it.
func (ts *tableStyle) borderName(rg region, side css.Side, i, j int) string {
	edgeTop, edgeBottom := i == 0, i == ts.rows-1
	edgeLeft, edgeRight := j == 0, j == ts.cols-1
	switch rg {
	case regionRow:
		edgeTop, edgeBottom = true, true
	case regionColumn:
		edgeLeft, edgeRight = true, true
	case regionCell:
		edgeTop, edgeBottom, edgeLeft, edgeRight = true, true, true, true
	case regionTable:
	}
	switch side {
	case css.Top:
		if edgeTop {
			return "top"
		}
		return "insideH"
	case css.Bottom:
		if edgeBottom {
			return "bottom"
		}
		return "insideH"
	case css.Left:
		if edgeLeft {
			return "left"
		}
		return "insideV"
	case css.Right:
	}
	if edgeRight {
		return "right"
	}
	return "insideV"
}

// cell reads one a:tc at row i and column j.
func (r *reader) cell(c *shapeCtx, tc *ooxml.Element, ts *tableStyle, i, j int) slides.TableCell {
	cell := slides.TableCell{ColSpan: 1, RowSpan: 1}
	if n, ok := intAttr(tc, "gridSpan"); ok && n > 1 {
		cell.ColSpan = min(n, ts.cols-j)
	}
	if n, ok := intAttr(tc, "rowSpan"); ok && n > 1 {
		cell.RowSpan = min(n, ts.rows-i)
	}
	cell.Covered = boolAttr(tc, "hMerge", false) || boolAttr(tc, "vMerge", false)
	pr := tc.Child(ooxml.NSA, "tcPr")
	fc := &fillCtx{sc: c.sc, part: c.part}
	parts := ts.parts(i, j)
	if f, ok := r.fillOf(fc, pr); ok {
		cell.Fill = f
	} else {
		for _, p := range parts {
			st := p.el.Child(ooxml.NSA, "tcStyle")
			if f := st.Child(ooxml.NSA, "fill"); f != nil {
				cell.Fill, _ = r.fillOf(fc, f)
			} else if ref := st.Child(ooxml.NSA, "fillRef"); ref != nil {
				cell.Fill = r.styleFill(fc, ref)
			}
		}
	}
	for side, own := range [4]string{"lnT", "lnR", "lnB", "lnL"} {
		if ln := pr.Child(ooxml.NSA, own); ln != nil {
			cell.Borders[side] = r.lineOf([]lineSource{{ln, fc}})
			continue
		}
		for _, p := range parts {
			b := p.el.Child(ooxml.NSA, "tcStyle").Child(ooxml.NSA, "tcBdr").Child(ooxml.NSA, ts.borderName(p.rg, css.Side(side), i, j))
			if b == nil {
				continue
			}
			if ln := b.Child(ooxml.NSA, "ln"); ln != nil {
				cell.Borders[side] = r.lineOf([]lineSource{{ln, fc}})
			} else if ref := b.Child(ooxml.NSA, "lnRef"); ref != nil {
				cell.Borders[side] = r.lineRef(fc, ref)
			}
		}
	}
	if tb := tc.Child(ooxml.NSA, "txBody"); tb != nil {
		m := c.layer.masterOf()
		tstyles := &textStyles{extra: cellTextLevel(parts), master: m.other, def: r.defaultText, sc: c.sc, part: c.part, owners: map[*ooxml.Element]string{m.other: m.part}}
		if l := tb.Child(ooxml.NSA, "lstStyle"); l != nil {
			tstyles.lists = []*ooxml.Element{l}
		}
		body := &slides.TextBody{Wrap: true, Insets: [4]float64{cellInsetTB, cellInsetLR, cellInsetTB, cellInsetLR}}
		for side, name := range [4]string{"marT", "marR", "marB", "marL"} {
			if v, ok := pr.Attr(name); ok {
				if pt, ok := ooxml.EMU(v); ok {
					body.Insets[side] = max(0, pt)
				}
			}
		}
		switch attr(pr, "anchor") {
		case "ctr":
			body.Anchor = slides.AnchorMiddle
		case "b":
			body.Anchor = slides.AnchorBottom
		}
		switch attr(pr, "vert") {
		case "vert":
			body.Vertical = css.SidewaysRL
		case "vert270":
			body.Vertical = css.SidewaysLR
		case "eaVert":
			// A cell's wordArtVert and wordArtVertRtl print horizontally
			// in Impress, which has no stacked text in tables.
			body.Vertical = css.VerticalRL
		}
		body.Paragraphs = r.paragraphs(tb, tstyles)
		if len(body.Paragraphs) > 0 {
			cell.Text = body
		}
	}
	return cell
}

// lineRef returns the theme line style a style matrix line reference
// names (20.1.4.2.19 lnRef), phClr standing for its colour.
func (r *reader) lineRef(fc *fillCtx, ref *ooxml.Element) slides.Line {
	idx, ok := intAttr(ref, "idx")
	if !ok || idx < 1 || idx > len(fc.sc.theme.lines) {
		return slides.Line{}
	}
	ph, _ := fc.sc.color(ref, nil)
	return r.lineOf([]lineSource{{fc.sc.theme.lines[idx-1], fc.withPh(ph)}})
}

// cellTextLevel merges the text styles of the table style parts that
// apply to a cell (20.1.4.2.30 tcTxStyle: b, i, a font or font reference,
// a colour), the later parts over the earlier, into a list level of its
// own; nil when no part sets any.
func cellTextLevel(parts []partRef) *ooxml.Element {
	rpr := &ooxml.Element{Space: ooxml.NSA, Local: "defRPr"}
	var face, fill *ooxml.Element
	set := false
	for _, p := range parts {
		tx := p.el.Child(ooxml.NSA, "tcTxStyle")
		if tx == nil {
			continue
		}
		for _, name := range []string{"b", "i"} {
			switch attr(tx, name) {
			case "on":
				rpr.Attrs = setAttr(rpr.Attrs, name, "1")
				set = true
			case "off":
				rpr.Attrs = setAttr(rpr.Attrs, name, "0")
				set = true
			}
		}
		if f := tx.Child(ooxml.NSA, "font"); f != nil {
			if l := f.Child(ooxml.NSA, "latin"); l != nil {
				face, set = l, true
			}
		}
		if ref := tx.Child(ooxml.NSA, "fontRef"); ref != nil {
			lv := fontRefLevel(ref)
			d := lv.Children[0]
			face, set = d.Children[0], true
			if len(d.Children) > 1 {
				fill = d.Children[1]
			}
		}
		for _, k := range tx.Kids() {
			switch k.Local {
			case "srgbClr", "schemeClr", "scrgbClr", "hslClr", "sysClr", "prstClr":
				fill, set = &ooxml.Element{Space: ooxml.NSA, Local: "solidFill", Children: []*ooxml.Element{k}}, true
			}
		}
	}
	if !set {
		return nil
	}
	if face != nil {
		rpr.Children = append(rpr.Children, face)
	}
	if fill != nil {
		rpr.Children = append(rpr.Children, fill)
	}
	return &ooxml.Element{Space: ooxml.NSA, Local: "lvlpPr", Children: []*ooxml.Element{rpr}}
}

// setAttr sets the attribute name in list to v.
func setAttr(list []xml.Attr, name, v string) []xml.Attr {
	for i := range list {
		if list[i].Name.Local == name {
			list[i].Value = v
			return list
		}
	}
	return append(list, attrs(name, v)...)
}

// builtinTableStyle returns the built-in table style of id (ECMA-376 Part
// 1, 20.1.4.2.26 tableStyleId: the GUIDs of the styles PowerPoint ships),
// as Impress draws it, nil for an ID it does not know.
func builtinTableStyle(ctx context.Context, id string) *ooxml.Element {
	src, ok := builtinTableStyleXML(id)
	if !ok {
		return nil
	}
	root, err := ooxml.Parse(ctx, []byte(src), nil)
	if err != nil {
		return nil
	}
	return root
}
