package docx

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestTables(t *testing.T) {
	t.Parallel()
	styles := stylesPart(`<w:style w:type="table" w:styleId="Grid"><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="8"/><w:bottom w:val="single" w:sz="8"/><w:insideH w:val="single" w:sz="4" w:color="FF0000"/></w:tblBorders><w:tblCellMar><w:left w:w="100" w:type="dxa"/><w:right w:w="100" w:type="dxa"/></w:tblCellMar></w:tblPr>` +
		`<w:tblStylePr w:type="firstRow"><w:rPr><w:b/></w:rPr><w:tcPr><w:shd w:val="clear" w:fill="000080"/></w:tcPr></w:tblStylePr>` +
		`<w:tblStylePr w:type="band2Horz"><w:tcPr><w:shd w:val="clear" w:fill="EEEEEE"/></w:tcPr></w:tblStylePr></w:style>`)
	settings := `<w:settings ` + nsDecl + `><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`
	tc := func(text, pr string) string {
		return `<w:tc><w:tcPr>` + pr + `</w:tcPr><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:tc>`
	}
	body := `<w:tbl><w:tblPr><w:tblStyle w:val="Grid"/><w:tblInd w:w="144" w:type="dxa"/><w:tblLook w:firstRow="1" w:noVBand="1"/></w:tblPr>` +
		`<w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="1000"/><w:gridCol w:w="3000"/></w:tblGrid>` +
		`<w:tr><w:trPr><w:tblHeader/></w:trPr>` + tc("H1", "") + tc("H2", "") + tc("H3", "") + `</w:tr>` +
		`<w:tr><w:trPr><w:cantSplit/><w:trHeight w:val="600"/></w:trPr>` + tc("wide", `<w:gridSpan w:val="2"/>`) + tc("tall", `<w:vMerge w:val="restart"/><w:vAlign w:val="center"/>`) + `</w:tr>` +
		`<w:tr>` + tc("a", "") + tc("b", `<w:shd w:val="clear" w:fill="FF0000"/><w:tcMar><w:left w:w="400" w:type="dxa"/></w:tcMar>`) + tc("", `<w:vMerge/>`) + `</w:tr>` +
		`</w:tbl>`
	b := build(t, body, map[string]string{"word/styles.xml": styles, "word/settings.xml": settings})
	tables := role(b.root, "Table")
	if len(tables) != 1 {
		t.Fatalf("%d tables", len(tables))
	}
	tbl := tables[0]
	if st := tbl.Style; st.Display != css.Table || st.BorderCollapse != css.CollapseBorders || st.TableLayout != css.TableFixed ||
		st.Width != css.Pts(300) || st.Margin[css.Left] != css.Pts(7.2) {
		t.Errorf("table style display %v collapse %v layout %v width %v margin %v", st.Display, st.BorderCollapse, st.TableLayout, st.Width, st.Margin[css.Left])
	}
	cols := find(tbl, func(n *layout.Node) bool { return n.Style != nil && n.Style.Display == css.TableColumn })
	if len(cols) != 3 || cols[0].Style.Width != css.Pts(100) {
		t.Errorf("columns %d", len(cols))
	}
	if h := find(tbl, func(n *layout.Node) bool { return n.Style != nil && n.Style.Display == css.TableHeaderGroup }); len(h) != 1 || len(role(h[0], "TH")) != 3 {
		t.Error("no header row group of three header cells")
	}
	for _, th := range role(tbl, "TH") {
		if th.Scope != typeset.ScopeColumn {
			t.Errorf("header cell %q scope %d, want column", textOf(th), th.Scope)
		}
	}
	cells := append(role(tbl, "TH"), role(tbl, "TD")...)
	byText := map[string]*layout.Node{}
	for _, c := range cells {
		byText[textOf(c)] = c
	}
	if c := byText["H1"]; c.Style.BackgroundColor != (css.Color{B: 128.0 / 255, A: 1}) || runWith(t, c, "H1").Style.FontWeight != 700 {
		t.Errorf("first row formatting: background %+v", c.Style.BackgroundColor)
	}
	if c := byText["wide"]; c.ColSpan != 2 || c.Style.BackgroundColor != (css.Color{}) {
		t.Errorf("wide cell span %d background %+v (band 1 has no shading)", c.ColSpan, c.Style.BackgroundColor)
	}
	if c := byText["tall"]; c.RowSpan != 2 || c.Style.VerticalAlign.Kind != css.Middle {
		t.Errorf("merged cell row span %d align %v", c.RowSpan, c.Style.VerticalAlign)
	}
	if c := byText["a"]; c.Style.BackgroundColor.R < 0.9 || c.Style.Border[css.Top].Color != (css.Color{R: 1, A: 1}) || c.Style.Border[css.Bottom].Width != 1 || c.Style.BoxDecorationBreak != css.DecorationCloneAll {
		t.Errorf("band 2 cell: background %+v borders %+v", c.Style.BackgroundColor, c.Style.Border)
	}
	if c := byText["b"]; c.Style.BackgroundColor != (css.Color{R: 1, A: 1}) || c.Style.Padding[css.Left] != css.Pts(20) || c.Style.Padding[css.Right] != css.Pts(5) {
		t.Errorf("direct cell formatting: background %+v padding %+v", c.Style.BackgroundColor, c.Style.Padding)
	}
	rows := role(tbl, "TR")
	if rows[1].Style.BreakInside != css.InsideAvoid || rows[1].Style.Height != css.Pts(30) {
		t.Errorf("row 2 break %v height %v", rows[1].Style.BreakInside, rows[1].Style.Height)
	}
	if len(rows[2].Children) != 2 {
		t.Errorf("continued merge cell built: %d cells", len(rows[2].Children))
	}
}

// TestTables_LegacyIndent locks where a table's edge sits from its indent
// and its alignment.
//
// # Corpus evidence
//
//   - litho/docx tables-row-heights: Word prints the table whose last row
//     carries a centred row alignment at the left margin, the text of its
//     first column at 77.8 pt on a 72 pt margin, as every other table.
//   - openxmlsdk/docx O14ISOStrict/Word/top-Table_Top_Border-sz-255.docx:
//     Word, in the Word 2007 mode, draws a table's left border of sz 255
//     (31.875 pt) from 40 to 72 pt on a 72 pt margin, centred 16 pt out,
//     where the cell margin of 5.4 pt is narrower than half the border.
//   - probe, docx: Word draws the left border of a floating table of
//     Table Grid, 0.5 pt wide, from 66.3 pt on a 72 pt margin.
func TestTables_LegacyIndent(t *testing.T) {
	t.Parallel()
	table := func(tblPr, rowPr string) string {
		return `<w:tbl><w:tblPr>` + tblPr + `</w:tblPr><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:trPr>` + rowPr + `</w:trPr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	}
	// Before Word 2013 an indent places the text, the table's edge the
	// cell margin left of it; without an indent the edge is at the
	// margin, as LibreOffice prints both. A row's alignment leaves the
	// table where its own properties put it.
	for _, tc := range []struct {
		name, tblPr, rowPr string
		want               css.Dimension
	}{
		{"indent", `<w:tblInd w:w="0" w:type="dxa"/>`, ``, css.Pts(-defaultCellMargin)},
		{"no indent", ``, ``, css.Pts(0)},
		{"row alignment", ``, `<w:jc w:val="center"/>`, css.Pts(0)},
		{"table alignment first", `<w:jc w:val="left"/>`, `<w:jc w:val="right"/>`, css.Pts(0)},
		// The border is centred on the edge: half of it lies further out.
		{"thin border", `<w:tblInd w:w="0" w:type="dxa"/><w:tblBorders><w:left w:val="single" w:sz="4"/></w:tblBorders>`, ``, css.Pts(-defaultCellMargin - 0.25)},
		{"border wider than twice the cell margin", `<w:tblInd w:w="0" w:type="dxa"/><w:tblBorders><w:left w:val="single" w:sz="255"/></w:tblBorders>`, ``, css.Pts(-31.875)},
	} {
		b := build(t, table(tc.tblPr, tc.rowPr), nil)
		if m := role(b.root, "Table")[0].Style.Margin[css.Left]; m != tc.want {
			t.Errorf("%s: table margin %v, want %v", tc.name, m, tc.want)
		}
	}
}

// TestTables_Nested locks a nested table: built inside its cell, its
// edge at its indent from the cell's text whatever the compatibility
// mode, and the empty paragraph that ends its cell after it left out.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/NumberedList.docx: in a Word 2010
//     document Word prints the text of a nested table with a 360 twip
//     indent and 86 twip cell margins at 131.1 pt, 18 + 4.3 pt and half
//     its 1.5 pt border right of its cell's text at 108 pt, where the
//     legacy rule of a table in the body would put it 4.3 pt further
//     left.
//   - libreoffice/docx ooxmlexport/fdo73389.docx: the text of a nested
//     table without its own indent starts 5.9 pt right of its cell's
//     text.
func TestTables_Nested(t *testing.T) {
	t.Parallel()
	inner := func(tblPr string) string {
		return `<w:tbl><w:tblPr>` + tblPr + `</w:tblPr><w:tblGrid><w:gridCol w:w="500"/></w:tblGrid><w:tr><w:tc><w:p><w:r><w:t>in</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	}
	outer := func(cell string) string {
		return `<w:tbl><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc>` + cell + `</w:tc></w:tr></w:tbl>`
	}
	for _, tc := range []struct {
		name, cell string
		paras      int
	}{
		{"empty paragraph after the table", inner(``) + `<w:p/>`, 0},
		{"paragraph with text after the table", inner(``) + `<w:p><w:r><w:t>after</w:t></w:r></w:p>`, 1},
		{"empty paragraph after text", `<w:p><w:r><w:t>before</w:t></w:r></w:p><w:p/>`, 2},
		{"numbered empty paragraph", inner(``) + `<w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:p>`, 1},
	} {
		b := build(t, outer(tc.cell), nil)
		td := role(role(b.root, "Table")[0], "TD")[0]
		paras := 0
		for _, k := range td.Children {
			if k.Role == "P" || k.Role == "L" {
				paras++
			}
		}
		if paras != tc.paras {
			t.Errorf("%s: %d paragraphs in the cell, want %d", tc.name, paras, tc.paras)
		}
	}
	b := build(t, outer(inner(`<w:tblInd w:w="360" w:type="dxa"/>`)+`<w:p/>`), nil)
	tables := role(b.root, "Table")
	if len(tables) != 2 {
		t.Fatalf("%d tables, want 2", len(tables))
	}
	if m := tables[1].Style.Margin[css.Left]; m != css.Pts(18) {
		t.Errorf("nested table margin %v, want 18pt: no legacy shift in a cell", m)
	}
}

// TestTables_BorderWidths locks the widths Word draws a table's
// compound borders at and the margins it measures from their middle.
//
// # Corpus evidence
//
//   - litho/docx tables-borders: Word prints the first cell's text of a
//     table with 1.5 pt single borders at 78.3 pt on a 72 pt margin, with
//     4.5 pt double borders at 79.8 pt, with 7.5 pt triple borders at
//     81.3 pt: 5.4 pt from the middle of the border.
//   - litho/docx tables-nested: the same with 2.25 pt double borders, at
//     78.5 pt.
//   - libreoffice/docx ooxmlexport/all_gaps_word.docx: the widths of the
//     gap and 3D borders, read from the lines Word draws.
//   - openxmlsdk/docx O14ISOStrict/Word/top-Table_Top_Border-sz-255.docx:
//     borders of sz 255 drawn 32 points thick.
func TestTables_BorderWidths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		border         string
		width, padLeft float64
		cellSpacingXML string
	}{
		{`w:val="single" w:sz="12"`, 1.5, 4.65, ``},
		{`w:val="double" w:sz="12"`, 4.5, 3.15, ``},
		{`w:val="triple" w:sz="12"`, 7.5, 1.65, ``},
		{`w:val="thinThickSmallGap" w:sz="12"`, 3, 3.9, ``},
		{`w:val="threeDEmboss" w:sz="12"`, 3, 3.9, ``},
		{`w:val="wave" w:sz="6"`, 3, 3.9, ``},
		{`w:val="thickThinSmallGap" w:sz="48"`, 7.5, 1.65, ``},
		{`w:val="thickThinMediumGap" w:sz="6"`, 1.25, 4.775, ``},
		{`w:val="thickThinLargeGap" w:sz="4"`, 2.75, 4.025, ``},
		{`w:val="threeDEngrave" w:sz="36"`, 7.5, 1.65, ``},
		{`w:val="single" w:sz="48"`, 6, 2.4, ``},
		{`w:val="dashSmallGap" w:sz="255"`, 31.875, 0, ``},
		{`w:val="single" w:sz="4000"`, 31.875, 0, ``},
		{`w:val="single" w:sz="12"`, 1.5, 5.4, `<w:tblCellSpacing w:w="72" w:type="dxa"/>`},
	} {
		var sides strings.Builder
		for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
			sides.WriteString(`<w:` + side + ` ` + tc.border + `/>`)
		}
		body := `<w:tbl><w:tblPr>` + tc.cellSpacingXML + `<w:tblBorders>` + sides.String() + `</w:tblBorders></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/></w:tblGrid>` +
			`<w:tr><w:tc><w:p/></w:tc><w:tc><w:p/></w:tc></w:tr></w:tbl>`
		b := build(t, body, nil)
		td := role(b.root, "TD")
		if len(td) != 2 {
			t.Fatalf("%s: %d cells", tc.border, len(td))
		}
		st := td[1].Style
		if w := st.Border[css.Left].Width; math.Abs(w-tc.width) > 1e-9 {
			t.Errorf("%s: border width %v, want %v", tc.border, w, tc.width)
		}
		if p := st.Padding[css.Left].Value; math.Abs(p-tc.padLeft) > 1e-9 {
			t.Errorf("%s%s: left padding %v, want %v", tc.border, tc.cellSpacingXML, p, tc.padLeft)
		}
	}
}

// TestTables_CellSpacing locks a table with cell spacing: separated
// borders, twice the spacing between the cells, the table's outer
// borders on the table box.
func TestTables_CellSpacing(t *testing.T) {
	t.Parallel()
	body := `<w:tbl><w:tblPr><w:tblCellSpacing w:w="72" w:type="dxa"/><w:tblBorders><w:top w:val="outset" w:sz="12"/><w:left w:val="outset" w:sz="12"/></w:tblBorders></w:tblPr>` +
		`<w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	b := build(t, body, nil)
	st := role(b.root, "Table")[0].Style
	if st.BorderCollapse != css.SeparateBorders || st.BorderSpacing != [2]float64{7.2, 7.2} {
		t.Errorf("collapse %v spacing %v, want separate 7.2", st.BorderCollapse, st.BorderSpacing)
	}
	if st.Border[css.Top].Width != 1.5 || st.Border[css.Left].Style != css.Outset || st.Border[css.Right].Width != 0 {
		t.Errorf("table borders %+v", st.Border)
	}
}

// TestTables_Floating locks where a floating table of the body is placed
// (17.4.58 tblpPr): at its offsets or alignments in the areas its anchors
// name, as Word places an anchored drawing, whether a paragraph follows
// it or not; as a float of its cell when nested.
//
// # Corpus evidence
//
//   - probe, docx: Word prints a table of vertAnchor page and tblpY 10261
//     that ends its body, no paragraph after it, 513 points down the
//     page, the paragraph before it at the top margin.
func TestTables_Floating(t *testing.T) {
	t.Parallel()
	table := func(tblpPr string, width int) string {
		w := strconv.Itoa(width)
		return `<w:tbl><w:tblPr>` + tblpPr + `<w:tblW w:w="` + w + `" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="` + w + `"/></w:tblGrid>` +
			`<w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	}
	// The default page is Letter with 72-point margins: 468 points of
	// text between them. A document without settings is of a mode
	// before Word 2013: an offset places the first cell's text, the
	// table's edge its 5.4-point margin further out.
	for _, tc := range []struct {
		name, tblpPr string
		width        int
		want         css.WordAnchor
	}{
		{"below the text", `<w:tblpPr w:leftFromText="180" w:tblpY="400"/>`, 2000, css.WordAnchor{X: -5.4, Y: 20}},
		{"right of the column", `<w:tblpPr w:tblpXSpec="right"/>`, 2000, css.WordAnchor{X: 368}},
		{"centred, from the page's top", `<w:tblpPr w:tblpXSpec="center" w:vertAnchor="page" w:tblpY="400"/>`, 2000, css.WordAnchor{X: 184, Y: -52, FromPage: true}},
		{"wider, centred on the margins", `<w:tblpPr w:tblpXSpec="center" w:horzAnchor="margin"/>`, 10080, css.WordAnchor{X: -18}},
		{"at the page's corner", `<w:tblpPr w:vertAnchor="page" w:horzAnchor="page" w:tblpYSpec="top"/>`, 2000, css.WordAnchor{X: -77.4, Y: -72, FromPage: true}},
		{"at the margin's bottom", `<w:tblpPr w:tblpXSpec="center" w:tblpYSpec="bottom"/>`, 2000, css.WordAnchor{X: 184, Y: 648, AlignY: 1, FromPage: true}},
	} {
		b := build(t, table(tc.tblpPr, tc.width)+`<w:p/>`, nil)
		b.secs[0].pending[0].resolve(b.secs[0])
		st := role(b.root, "Table")[0].Style
		got := st.WordAnchor
		got.Set = false
		if math.Abs(got.X-tc.want.X) > 1e-9 || math.Abs(got.Y-tc.want.Y) > 1e-9 || got.AlignX != tc.want.AlignX || got.AlignY != tc.want.AlignY || got.FromPage != tc.want.FromPage || st.Float != css.FloatLeft {
			t.Errorf("%s: float %v at %+v, want %+v", tc.name, st.Float, got, tc.want)
		}
	}
	b := build(t, table(`<w:tblpPr w:leftFromText="180" w:rightFromText="180" w:tblpY="400"/>`, 2000)+`<w:p/>`, nil)
	b.secs[0].pending[0].resolve(b.secs[0])
	if gap := role(b.root, "Table")[0].Style.WrapGap; gap != [4]float64{0, 9, 0, 9} {
		t.Errorf("wrap gap %v, want 9 points on both sides", gap)
	}
	// A table no paragraph follows is placed all the same.
	b = build(t, `<w:p/>`+table(`<w:tblpPr w:vertAnchor="page" w:tblpY="10261"/>`, 2000), nil)
	if len(b.secs[0].pending) != 1 {
		t.Fatalf("table ending the body: %d tables placed, want 1", len(b.secs[0].pending))
	}
	b.secs[0].pending[0].resolve(b.secs[0])
	if st := role(b.root, "Table")[0].Style; !st.WordAnchor.Set || !st.WordAnchor.FromPage || math.Abs(st.WordAnchor.Y-(513.05-72)) > 1e-9 {
		t.Errorf("table ending the body: anchor %+v; want it 513.05 points down its page", st.WordAnchor)
	}
	// A table nested in a cell is a float of the cell.
	nested := `<w:tbl><w:tblGrid><w:gridCol w:w="4000"/></w:tblGrid><w:tr><w:tc>` + table(`<w:tblpPr w:tblpXSpec="right"/>`, 2000) + `<w:p/></w:tc></w:tr></w:tbl>`
	b = build(t, nested, nil)
	if inner := role(b.root, "Table")[1].Style; inner.WordAnchor.Set || inner.Float != css.FloatRight {
		t.Errorf("nested table: float %v, anchor %+v; want a right float", inner.Float, inner.WordAnchor)
	}
}

// TestTables_PositionBeforeStyle locks that a w:tblpPr written before
// the w:tblStyle, against the order of 17.4.60 tblPr, places nothing: the
// table is laid out in the flow, and one in schema order floats.
//
// # Corpus evidence
//
//   - probe, docx: Word prints a table of tblpPr vertAnchor page, tblpY
//     10261 before its tblStyle at the top margin, its style's borders
//     drawn and the paragraph after it below it, and the same table of
//     tblStyle first 513 points down the page, the paragraph at the top.
//   - libreoffice/docx: ooxmlimport/table_width.docx writes its tblpPr
//     before its tblStyle, and Word prints its table at the top margin.
func TestTables_PositionBeforeStyle(t *testing.T) {
	t.Parallel()
	tblpPr := `<w:tblpPr w:vertAnchor="page" w:horzAnchor="margin" w:tblpY="10261"/>`
	table := func(tblPr string) string {
		return `<w:tbl><w:tblPr>` + tblPr + `<w:tblW w:w="2000" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid>` +
			`<w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p/>`
	}
	b := build(t, table(tblpPr+`<w:tblStyle w:val="TableGrid"/>`), nil)
	if st := role(b.root, "Table")[0].Style; st.WordAnchor.Set || st.Float != css.FloatNone || len(b.secs[0].pending) != 0 {
		t.Errorf("tblpPr before tblStyle: float %v, anchor %+v; want the table in the flow", st.Float, st.WordAnchor)
	}
	b = build(t, table(`<w:tblStyle w:val="TableGrid"/>`+tblpPr), nil)
	if len(b.secs[0].pending) != 1 {
		t.Fatalf("tblStyle before tblpPr: %d tables placed, want 1", len(b.secs[0].pending))
	}
	b.secs[0].pending[0].resolve(b.secs[0])
	if st := role(b.root, "Table")[0].Style; !st.WordAnchor.Set || !st.WordAnchor.FromPage {
		t.Errorf("tblStyle before tblpPr: anchor %+v; want it placed from the page", st.WordAnchor)
	}
}

func TestTables_FramesInCells(t *testing.T) {
	t.Parallel()
	frame := `<w:framePr w:w="2000" w:hSpace="141" w:wrap="around" w:vAnchor="page" w:hAnchor="margin" w:xAlign="right" w:y="2266"/>`
	body := `<w:tbl><w:tblPr><w:tblW w:w="2000" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid>` +
		`<w:tr><w:tc><w:p><w:pPr>` + frame + `</w:pPr><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		`<w:p><w:pPr>` + frame + `</w:pPr><w:r><w:t>body</w:t></w:r></w:p>`
	b := build(t, body, nil)
	frames := find(b.root, func(n *layout.Node) bool { return n.Style != nil && n.Style.WordAnchor.Set })
	if len(frames) != 1 || textOf(frames[0]) != "body" {
		t.Fatalf("%d frames placed, want the body paragraph's alone", len(frames))
	}
	for _, p := range role(b.root, "P") {
		if st := p.Style; st.Float != css.FloatNone || st.Width.Kind != css.Auto {
			t.Errorf("%q: float %v width %v, want a paragraph in the flow of its cell or frame", textOf(p), st.Float, st.Width)
		}
	}
}

func TestFrames_WithoutWidthBreakAnywhere(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:pPr><w:framePr w:hSpace="142" w:wrap="around" w:vAnchor="text" w:hAnchor="page" w:x="4569" w:y="340"/></w:pPr><w:r><w:t>narrow</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:framePr w:w="2000" w:wrap="around" w:hAnchor="page" w:x="4569"/></w:pPr><w:r><w:t>sized</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>plain</w:t></w:r></w:p>`
	b := build(t, body, nil)
	for _, p := range role(b.root, "P") {
		want := textOf(p) == "narrow"
		texts := find(p, func(n *layout.Node) bool { return n.Kind == layout.Text })
		if len(texts) == 0 {
			t.Fatalf("%q: no text", textOf(p))
		}
		for _, n := range append(texts, p) {
			if got := n.Style.OverflowWrap == css.Anywhere; got != want {
				t.Errorf("%q: anywhere %v, want %v", textOf(p), got, want)
			}
		}
	}
}

func TestTables_DecorationsCloned(t *testing.T) {
	t.Parallel()
	b := build(t, `<w:tbl><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`, nil)
	for _, r := range []string{"Table", "TD"} {
		for _, n := range role(b.root, r) {
			if n.Style.BoxDecorationBreak != css.DecorationCloneAll {
				t.Errorf("%s: decoration break %v, want clone", r, n.Style.BoxDecorationBreak)
			}
		}
	}
}

// TestTables_RowTops locks the content of a row's cells starting below
// the row's thickest top border.
func TestTables_RowTops(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spacing string
		want    float64
	}{{``, 0.75}, {`<w:tblCellSpacing w:w="20" w:type="dxa"/>`, 1.5}} {
		body := `<w:tbl><w:tblPr>` + tc.spacing + `</w:tblPr><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/></w:tblGrid><w:tr>` +
			`<w:tc><w:tcPr><w:tcBorders><w:top w:val="single" w:sz="4"/></w:tcBorders></w:tcPr><w:p/></w:tc>` +
			`<w:tc><w:tcPr><w:tcBorders><w:top w:val="single" w:sz="16"/></w:tcBorders></w:tcPr><w:p/></w:tc></w:tr></w:tbl>`
		td := role(build(t, body, nil).root, "TD")
		if p := td[0].Style.Padding[css.Top].Value; p != tc.want {
			t.Errorf("spacing %q: thin cell's top padding %v, want %v", tc.spacing, p, tc.want)
		}
		if p := td[1].Style.Padding[css.Top].Value; p != 0 {
			t.Errorf("spacing %q: thick cell's top padding %v, want 0", tc.spacing, p)
		}
	}
}

// TestTables_OwnShading locks a table's own shading: left out of the
// cells of a table without cell spacing, the table style's shading kept.
func TestTables_OwnShading(t *testing.T) {
	t.Parallel()
	styles := stylesPart(`<w:style w:type="table" w:styleId="Shaded"><w:tblPr><w:shd w:val="clear" w:fill="00FF00"/></w:tblPr></w:style>`)
	for _, tc := range []struct {
		name, tblPr string
		want        css.Color
	}{
		{"own shading", `<w:shd w:val="clear" w:fill="FF0000"/>`, css.Color{}},
		{"style shading under own", `<w:tblStyle w:val="Shaded"/><w:shd w:val="clear" w:fill="FF0000"/>`, css.Color{G: 1, A: 1}},
		{"own shading with cell spacing", `<w:tblCellSpacing w:w="20" w:type="dxa"/><w:shd w:val="clear" w:fill="FF0000"/>`, css.Color{R: 1, A: 1}},
	} {
		body := `<w:tbl><w:tblPr>` + tc.tblPr + `</w:tblPr><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
		td := role(build(t, body, map[string]string{"word/styles.xml": styles}).root, "TD")
		if c := td[0].Style.BackgroundColor; c != tc.want {
			t.Errorf("%s: cell background %+v, want %+v", tc.name, c, tc.want)
		}
	}
}

// TestTables_FixedCellsClip locks the cells of a table of fixed layout
// clipping their content, those of an autofit table not.
func TestTables_FixedCellsClip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		layout string
		want   css.Overflow
	}{{`<w:tblLayout w:type="fixed"/>`, css.OverflowHidden}, {``, css.OverflowVisible}} {
		body := `<w:tbl><w:tblPr>` + tc.layout + `</w:tblPr><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
		if o := role(build(t, body, nil).root, "TD")[0].Style.Overflow; o != tc.want {
			t.Errorf("layout %q: cell overflow %v, want %v", tc.layout, o, tc.want)
		}
	}
}

func TestHoldsAnchored(t *testing.T) {
	t.Parallel()
	floated := css.Initial()
	floated.Float = css.FloatLeft
	plain := css.Initial()
	deep := &layout.Node{Style: plain, Children: []*layout.Node{{Style: floated}}}
	if !holdsAnchored([]*layout.Node{deep}, 0) {
		t.Error("a nested float not found")
	}
	if holdsAnchored([]*layout.Node{{Style: plain}}, 0) {
		t.Error("a plain block taken for an anchored one")
	}
}

// TestFrames_Placed places a text frame of the body as Word places a
// drawing that wraps the text, at its x and y from the area its anchors
// name or aligned in it, its text starting at its left edge, the
// consecutive paragraphs and tables of identical frame properties in one
// frame.
func TestFrames_Placed(t *testing.T) {
	t.Parallel()
	para := func(framePr, text string) string {
		return `<w:p><w:pPr>` + framePr + `</w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
	}
	// The default page is Letter with 72-point margins.
	for _, tc := range []struct {
		name, framePr string
		want          css.WordAnchor
		wrap          css.FrameWrap
		width, height css.Dimension
	}{
		{"from the page", `<w:framePr w:w="2000" w:h="1000" w:hRule="exact" w:wrap="around" w:vAnchor="page" w:hAnchor="page" w:x="3000" w:y="4000"/>`,
			css.WordAnchor{X: 78, Y: 128, FromPage: true}, css.FrameWrapBoth, css.Pts(100), css.Pts(50)},
		{"right of the page", `<w:framePr w:w="2000" w:wrap="around" w:vAnchor="text" w:hAnchor="page" w:xAlign="right" w:y="200"/>`,
			css.WordAnchor{X: 440, Y: 10}, css.FrameWrapBoth, css.Pts(100), css.Dimension{Kind: css.Auto}},
		{"above and below the text", `<w:framePr w:w="2000" w:h="1000" w:hRule="atLeast" w:wrap="notBeside" w:vAnchor="margin" w:hAnchor="margin" w:x="200"/>`,
			css.WordAnchor{X: 10, FromPage: true}, css.FrameWrapNone, css.Pts(100), css.Dimension{Kind: css.Auto}},
		{"at the bottom right of the page, as wide as its text", `<w:framePr w:wrap="around" w:vAnchor="page" w:hAnchor="page" w:xAlign="right" w:yAlign="bottom"/>`,
			css.WordAnchor{X: 540, Y: 720, AlignX: 1, AlignY: 1, FromPage: true}, css.FrameWrapBoth, css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}},
		{"naming no anchor, from the column and the margins", `<w:framePr w:w="2880" w:wrap="around" w:x="2880" w:y="2880"/>`,
			css.WordAnchor{X: 144, Y: 144, FromPage: true}, css.FrameWrapBoth, css.Pts(144), css.Dimension{Kind: css.Auto}},
		{"naming no vAnchor, at the bottom margin", `<w:framePr w:w="2880" w:wrap="around" w:hAnchor="text" w:yAlign="bottom"/>`,
			css.WordAnchor{Y: 648, AlignY: 1, FromPage: true}, css.FrameWrapBoth, css.Pts(144), css.Dimension{Kind: css.Auto}},
	} {
		b := build(t, para(tc.framePr, "framed")+`<w:p/>`, nil)
		for _, a := range b.secs[0].pending {
			a.resolve(b.secs[0])
		}
		frames := find(b.root, func(n *layout.Node) bool { return n.Style != nil && n.Style.WordAnchor.Set })
		if len(frames) != 1 {
			t.Fatalf("%s: %d frames, want 1", tc.name, len(frames))
		}
		st := frames[0].Style
		got := st.WordAnchor
		got.Set = false
		if math.Abs(got.X-tc.want.X) > 1e-9 || math.Abs(got.Y-tc.want.Y) > 1e-9 || got.AlignX != tc.want.AlignX || got.AlignY != tc.want.AlignY || got.FromPage != tc.want.FromPage {
			t.Errorf("%s: at %+v, want %+v", tc.name, got, tc.want)
		}
		if st.FrameWrap != tc.wrap || st.Width != tc.width || (tc.height.Kind == css.Points && st.Height != tc.height) {
			t.Errorf("%s: wrap %v width %v height %v min %v, want %v %v %v", tc.name, st.FrameWrap, st.Width, st.Height, st.MinHeight, tc.wrap, tc.width, tc.height)
		}
		if textOf(frames[0]) != "framed" {
			t.Errorf("%s: frame holds %q", tc.name, textOf(frames[0]))
		}
	}
	// A frame of yAlign inline naming no vertical anchor stays at its
	// paragraph.
	b := build(t, para(`<w:framePr w:wrap="auto" w:hAnchor="text" w:xAlign="left" w:yAlign="inline"/>`, "inline")+`<w:p/>`, nil)
	if frames := find(b.root, func(n *layout.Node) bool { return n.Style != nil && n.Style.WordAnchor.Set }); len(frames) != 0 {
		t.Errorf("yAlign inline: %d frames placed, want none", len(frames))
	}
	f := `<w:framePr w:wrap="around" w:vAnchor="page" w:hAnchor="page" w:x="3000" w:y="4000"/>`
	other := `<w:framePr w:wrap="around" w:vAnchor="page" w:hAnchor="page" w:x="3000" w:y="9000"/>`
	tbl := `<w:tbl><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc>` + para(f, "cell") + `</w:tc></w:tr></w:tbl>`
	b = build(t, para(f, "one")+tbl+para(f, "two")+para(other, "three")+para("", "flow"), nil)
	frames := find(b.root, func(n *layout.Node) bool { return n.Style != nil && n.Style.WordAnchor.Set })
	if len(frames) != 2 || textOf(frames[0]) != "onecelltwo" || textOf(frames[1]) != "three" {
		t.Fatalf("frames %d, want onecelltwo and three", len(frames))
	}
	if w := frames[0].Style.Width; w != css.Pts(468) {
		t.Errorf("frame of several blocks without a width: width %v, want the column's 468", w)
	}
	if w := frames[1].Style.Width; w.Kind != css.Auto {
		t.Errorf("frame of one paragraph without a width: width %v, want auto", w)
	}
	// In a section of three columns 18 points apart, a frame positioned
	// from the page counts from the first column whatever column it lands
	// in, one positioned from its column from its own, and a frame of
	// several paragraphs is a column wide.
	cols := `<w:sectPr><w:cols w:num="3" w:space="360"/></w:sectPr>`
	text := `<w:framePr w:wrap="around" w:vAnchor="text" w:hAnchor="text" w:x="200" w:y="200"/>`
	b = build(t, para(f, "one")+para(f, "two")+para(text, "three")+para("", "flow")+cols, nil)
	for _, a := range b.secs[0].pending {
		a.resolve(b.secs[0])
	}
	frames = find(b.root, func(n *layout.Node) bool { return n.Style != nil && n.Style.WordAnchor.Set })
	if len(frames) != 2 {
		t.Fatalf("columns: %d frames, want 2", len(frames))
	}
	if a := frames[0].Style.WordAnchor; !a.FromFirstColumn || math.Abs(a.X-78) > 1e-9 {
		t.Errorf("columns: frame from the page %+v, want X 78 from the first column", a)
	}
	if a := frames[1].Style.WordAnchor; a.FromFirstColumn || math.Abs(a.X-10) > 1e-9 {
		t.Errorf("columns: frame from its column %+v, want X 10 from its own column", a)
	}
	if w := frames[0].Style.Width; w != css.Pts(144) {
		t.Errorf("columns: frame of several paragraphs: width %v, want the column's 144", w)
	}
}

// TestFrames_InsideOutside locks that a text frame aligned outside sits
// at the right edge of an odd page and the left edge of an even one, and
// one aligned inside the other way (17.18.107 ST_XAlign).
//
// # Corpus evidence
//
//   - probe, docx: Word prints frames of xAlign outside from the page
//     flush with the right edge of the first page, its text at 521 to 612
//     with hSpace 397, 468 to 541 for a frame 2880 wide, and flush with
//     the left edge of the second page, its text from 0; one of xAlign
//     inside flush with the first page's left edge.
func TestFrames_InsideOutside(t *testing.T) {
	t.Parallel()
	frame := func(align, text string) string {
		return `<w:p><w:pPr><w:framePr w:w="1440" w:wrap="around" w:vAnchor="page" w:hAnchor="page" w:xAlign="` + align + `" w:y="1440"/></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
	}
	body := frame("outside", "oddout") + `<w:p><w:r><w:t>one</w:t></w:r></w:p><w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
		frame("outside", "evenout") + `<w:p><w:r><w:t>two</w:t></w:r></w:p><w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
		frame("inside", "oddin") + `<w:p><w:r><w:t>three</w:t></w:r></w:p><w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
		frame("inside", "evenin") + `<w:p><w:r><w:t>four</w:t></w:r></w:p>` +
		`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`
	doc := convertBytes(t, docx(t, body, nil), office.Options{})
	at := textPlaces(doc)
	// A frame 72 points wide on a page 612 wide: at 540 on the right
	// edge, at 0 on the left.
	for _, tc := range []struct {
		text string
		page int
		x    float64
	}{{"oddout", 0, 540}, {"evenout", 1, 0}, {"oddin", 2, 0}, {"evenin", 3, 540}} {
		got := at[tc.text]
		if int(got[0]) != tc.page || math.Abs(got[1]-tc.x) > 0.5 {
			t.Errorf("%s: page %d at %.1f, want page %d at %.0f", tc.text, int(got[0])+1, got[1], tc.page+1, tc.x)
		}
	}
}

// textPlaces returns the page index and the x of the first glyph of
// each text fragment of doc, by its text.
func textPlaces(doc *typeset.Document) map[string][2]float64 {
	at := map[string][2]float64{}
	for p, pg := range doc.Pages {
		var walk func([]typeset.Fragment, float64)
		walk = func(frags []typeset.Fragment, dx float64) {
			for _, f := range frags {
				switch v := f.(type) {
				case *typeset.Text:
					var b strings.Builder
					for _, g := range v.Glyphs {
						b.WriteString(g.Text)
					}
					at[b.String()] = [2]float64{float64(p), dx + v.Origin.X}
				case *typeset.Group:
					walk(v.Content, dx+v.Transform.E)
				}
			}
		}
		walk(pg.Content, 0)
	}
	return at
}

// TestFrames_AfterABreak locks that a page or column break before a
// paragraph of a text frame moves the frame: the frame prints on the
// page or in the column the break opens, with the text after it.
//
// # Corpus evidence
//
//   - probe, docx: Word prints a frame of vAnchor page whose paragraph
//     follows a page break on the second page, the text after it beside
//     it, and a frame naming no hAnchor after a column break in the
//     second column, its x from that column.
func TestFrames_AfterABreak(t *testing.T) {
	t.Parallel()
	frame := `<w:p><w:pPr><w:framePr w:w="1440" w:wrap="around" w:vAnchor="page" w:x="720" w:y="4320"/></w:pPr><w:r><w:t>framed</w:t></w:r></w:p>`
	for _, tc := range []struct {
		name, sect string
		page       int
		minX       float64
	}{
		// Letter, 72-point margins: the frame 36 points right of the
		// column it lands in.
		{"page", "", 1, 100},
		{"column", `<w:cols w:num="2" w:space="720"/>`, 0, 350},
	} {
		body := `<w:p><w:r><w:t>before</w:t></w:r></w:p><w:p><w:r><w:br w:type="` + tc.name + `"/></w:r></w:p>` + frame + `<w:p><w:r><w:t>after</w:t></w:r></w:p>` +
			`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/>` + tc.sect + `</w:sectPr>`
		doc := convertBytes(t, docx(t, body, nil), office.Options{})
		at := textPlaces(doc)
		f, a := at["framed"], at["after"]
		if int(f[0]) != tc.page || int(a[0]) != tc.page || f[1] < tc.minX {
			t.Errorf("%s break: frame on page %d at %.1f, text after it on page %d; want page %d, past %.0f", tc.name, int(f[0])+1, f[1], int(a[0])+1, tc.page+1, tc.minX)
		}
	}
}

// TestTables_GridBefore holds the place of the grid columns a row's
// gridBefore skips with one empty cell spanning them, so that the row's
// first cell starts in the column after them.
func TestTables_GridBefore(t *testing.T) {
	t.Parallel()
	body := `<w:tbl><w:tblGrid><w:gridCol w:w="500"/><w:gridCol w:w="500"/><w:gridCol w:w="2000"/></w:tblGrid>` +
		`<w:tr><w:trPr><w:gridBefore w:val="2"/></w:trPr><w:tc><w:p><w:r><w:t>late</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:tcPr><w:gridSpan w:val="3"/></w:tcPr><w:p><w:r><w:t>whole</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p/>`
	rows := role(build(t, body, nil).root, "TR")
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	first := rows[0].Children
	if len(first) != 2 || first[0].ColSpan != 2 || len(first[0].Children) != 0 || first[0].Style.Border != [4]css.Border{} || textOf(first[1]) != "late" {
		t.Fatalf("first row: %d cells, want an empty one spanning 2 columns, then late", len(first))
	}
	if len(rows[1].Children) != 1 {
		t.Errorf("second row: %d cells, want 1", len(rows[1].Children))
	}
}

// TestTables_FloatingBreaks locks where a floating table breaks across
// pages (17.4.58 tblpPr): its first page holds the rows it would hold in
// the flow, raised by as far as the table stands above its place there;
// one of several rows offset from the text that starts above the bottom
// margin and ends below it breaks there, its first row staying past the
// margin.
//
// # Corpus evidence
//
//   - probe, docx: Word prints 45 rows of a 60-row table of tblpY 1440
//     from the page, after a one-line paragraph, on page 1, as many as
//     the table in the flow, where a 46th fits above the margin; a
//     4-row table of vertAnchor text whose top lies a point above the
//     bottom margin keeps its first row there, past the margin, and sets
//     the others at the top of page 2, the text after the table beside
//     them.
//   - openxmlsdk/docx v2FxTestFiles/wordprocessing/O12_templates/RoomForRentFlyer_TP10220233.dotx:
//     Word prints a one-row table of vertAnchor text across the bottom
//     margin whole on its page.
func TestTables_FloatingBreaks(t *testing.T) {
	t.Parallel()
	const styles = `<w:styles ` + nsDecl + `><w:docDefaults><w:rPrDefault><w:rPr><w:sz w:val="22"/></w:rPr></w:rPrDefault>` +
		`<w:pPrDefault><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults></w:styles>`
	table := func(tblpPr string, rows int) string {
		var b strings.Builder
		b.WriteString(`<w:tbl><w:tblPr>` + tblpPr + `<w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="2400"/></w:tblGrid>`)
		for r := range rows {
			b.WriteString(`<w:tr><w:tc><w:p><w:r><w:t>Row ` + strconv.Itoa(r) + `</w:t></w:r></w:p></w:tc></w:tr>`)
		}
		b.WriteString(`</w:tbl>`)
		return b.String()
	}
	places := func(body string) map[string][2]float64 {
		return textPlaces(convertBytes(t, docx(t, body, map[string]string{"word/styles.xml": styles}), office.Options{}))
	}
	onFirstPage := func(body string) int {
		at := places(body)
		n := 0
		for r := range 200 {
			if p, ok := at["Row "+strconv.Itoa(r)]; ok && p[0] == 0 {
				n++
			}
		}
		return n
	}
	const first = `<w:p><w:r><w:t>First</w:t></w:r></w:p>`
	inFlow := onFirstPage(first + table(``, 80) + `<w:p/>`)
	floating := onFirstPage(first + table(`<w:tblpPr w:vertAnchor="page" w:horzAnchor="margin" w:tblpY="1440"/>`, 80) + `<w:p/>`)
	if inFlow == 0 || floating != inFlow {
		t.Errorf("floating table from the top margin: %d rows on page 1, want %d as in the flow", floating, inFlow)
	}
	for _, tc := range []struct {
		name        string
		tblpY, rows int
		want        int
		after       float64
	}{
		{"above the margin", 10000, 4, 4, 0},
		{"across the margin", 12940, 4, 1, 1},
		{"one row across the margin", 12940, 1, 1, 0},
	} {
		body := table(`<w:tblpPr w:vertAnchor="text" w:horzAnchor="margin" w:tblpY="`+strconv.Itoa(tc.tblpY)+`"/>`, tc.rows) + `<w:p><w:r><w:t>After</w:t></w:r></w:p>`
		if got, after := onFirstPage(body), places(body)["After"][0]; got != tc.want || after != tc.after {
			t.Errorf("%s: %d rows on page 1, the text after on page %v; want %d and page %v", tc.name, got, after+1, tc.want, tc.after+1)
		}
	}
}

// TestTables_FloatingDropsPageBreaks locks that a page break in a
// floating table's cell (17.3.3.1 br, 17.4.58 tblpPr) breaks nothing:
// the text after it runs on in its line, where a table in the flow
// breaks there.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/fdo68607.docx: Word prints the cells of
//     a floating table whose runs hold five page breaks with the words
//     on each side of a break joined on one line, "describe thefeeling".
func TestTables_FloatingDropsPageBreaks(t *testing.T) {
	t.Parallel()
	cell := `<w:tblGrid><w:gridCol w:w="4000"/></w:tblGrid><w:tr><w:tc><w:p><w:r><w:t>before</w:t></w:r><w:r><w:br w:type="page"/><w:t>after</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p/>`
	for _, tc := range []struct {
		name, tblPr string
		joined      bool
	}{
		{"floating", `<w:tblpPr w:tblpY="200"/>`, true},
		{"in the flow", ``, false},
	} {
		doc := convertBytes(t, docx(t, `<w:tbl><w:tblPr>`+tc.tblPr+`</w:tblPr>`+cell, nil), office.Options{})
		joined := len(doc.Pages) == 1 && strings.Contains(pageText(doc.Pages[0]), "before after")
		if joined != tc.joined {
			t.Errorf("%s: %d pages, text %q; want the words joined %v", tc.name, len(doc.Pages), docText(doc), tc.joined)
		}
	}
}
