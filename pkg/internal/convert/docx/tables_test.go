package docx

import (
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
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
		if th.Scope != compose.ScopeColumn {
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

func TestTables_Floating(t *testing.T) {
	t.Parallel()
	table := func(tblpPr string) string {
		return `<w:tbl><w:tblPr>` + tblpPr + `<w:tblW w:w="2000" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid>` +
			`<w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	}
	for _, tc := range []struct {
		name, tblpPr string
		float        css.Float
		auto         bool
		top, pageTop float64
	}{
		{"left", `<w:tblpPr w:leftFromText="180" w:tblpY="400"/>`, css.FloatLeft, false, 0, 0},
		{"right", `<w:tblpPr w:tblpXSpec="right"/>`, css.FloatRight, false, 0, 0},
		{"centred below the text", `<w:tblpPr w:tblpXSpec="center" w:tblpY="400"/>`, css.FloatNone, true, 20, 0},
		{"centred on the page", `<w:tblpPr w:tblpXSpec="center" w:vertAnchor="page" w:tblpY="400"/>`, css.FloatNone, true, 0, 20},
	} {
		b := build(t, table(tc.tblpPr), nil)
		tbl := role(b.root, "Table")[0].Style
		if tbl.Float != tc.float || (tbl.Margin[css.Left].Kind == css.Auto) != tc.auto || tbl.Margin[css.Top].Value != tc.top || tbl.PageTop != tc.pageTop {
			t.Errorf("%s: float %v margins %v page top %v", tc.name, tbl.Float, tbl.Margin, tbl.PageTop)
		}
	}
}

func TestTables_FramesInCells(t *testing.T) {
	t.Parallel()
	frame := `<w:framePr w:w="2000" w:hSpace="141" w:wrap="around" w:vAnchor="page" w:hAnchor="margin" w:xAlign="right" w:y="2266"/>`
	body := `<w:tbl><w:tblPr><w:tblW w:w="2000" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid>` +
		`<w:tr><w:tc><w:p><w:pPr>` + frame + `</w:pPr><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		`<w:p><w:pPr>` + frame + `</w:pPr><w:r><w:t>body</w:t></w:r></w:p>`
	b := build(t, body, nil)
	for _, p := range role(b.root, "P") {
		st := p.Style
		framed := st.Float != css.FloatNone || st.Width.Kind != css.Auto
		if want := textOf(p) == "body"; framed != want {
			t.Errorf("%q: float %v width %v, framed %v, want %v", textOf(p), st.Float, st.Width, framed, want)
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
