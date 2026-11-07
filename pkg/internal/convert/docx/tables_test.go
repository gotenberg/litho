package docx

import (
	"testing"

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

func TestTables_LegacyIndent(t *testing.T) {
	t.Parallel()
	body := `<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	b := build(t, body, nil)
	// Before Word 2013 the table's edge sits the cell margin left of the
	// text.
	if m := role(b.root, "Table")[0].Style.Margin[css.Left]; m != css.Pts(-defaultCellMargin) {
		t.Errorf("legacy table margin %v", m)
	}
}

func TestTables_Nested(t *testing.T) {
	t.Parallel()
	inner := `<w:tbl><w:tblGrid><w:gridCol w:w="500"/></w:tblGrid><w:tr><w:tc><w:p><w:r><w:t>in</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	body := `<w:tbl><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc>` + inner + `<w:p/></w:tc></w:tr></w:tbl>`
	b := build(t, body, nil)
	if n := len(role(b.root, "Table")); n != 2 {
		t.Errorf("%d tables, want 2", n)
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
		top          float64
	}{
		{"left", `<w:tblpPr w:leftFromText="180" w:tblpY="400"/>`, css.FloatLeft, false, 0},
		{"right", `<w:tblpPr w:tblpXSpec="right"/>`, css.FloatRight, false, 0},
		{"centred below the text", `<w:tblpPr w:tblpXSpec="center" w:tblpY="400"/>`, css.FloatNone, true, 20},
		{"centred on the page", `<w:tblpPr w:tblpXSpec="center" w:vertAnchor="page" w:tblpY="400"/>`, css.FloatNone, true, 0},
	} {
		b := build(t, table(tc.tblpPr), nil)
		tbl := role(b.root, "Table")[0].Style
		if tbl.Float != tc.float || (tbl.Margin[css.Left].Kind == css.Auto) != tc.auto || tbl.Margin[css.Top].Value != tc.top {
			t.Errorf("%s: float %v margins %v", tc.name, tbl.Float, tbl.Margin)
		}
	}
}
