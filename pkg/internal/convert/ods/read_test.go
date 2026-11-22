package ods

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/warn"
	"github.com/gotenberg/litho/pkg/models"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestRead_Tables(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="ta1" style:family="table" style:master-page-name="Default"><style:table-properties table:display="true"/></style:style>` +
			`<style:style style:name="ta2" style:family="table"><style:table-properties table:display="false"/></style:style>` +
			`<style:style style:name="ta3" style:family="table"><style:table-properties style:writing-mode="rl-tb"/></style:style>`,
		body: `<table:table table:name="One" table:style-name="ta1"/><table:table table:name="Two" table:style-name="ta2"/>` +
			`<table:table table:name="Three" table:style-name="ta3"/><table:table table:name="Four" table:print="false"/>`,
	}
	doc, _ := read(t, f.bytes())
	tests := []struct {
		name        string
		hidden, rtl bool
	}{{"One", false, false}, {"Two", true, false}, {"Three", false, true}, {"Four", true, false}}
	if len(doc.Sheets) != len(tests) {
		t.Fatalf("sheets = %d", len(doc.Sheets))
	}
	for i, tt := range tests {
		sh := doc.Sheets[i]
		if sh.Name != tt.name || sh.Hidden != tt.hidden || sh.RightToLeft != tt.rtl {
			t.Errorf("sheet %d = %q hidden %v rtl %v, want %+v", i, sh.Name, sh.Hidden, sh.RightToLeft, tt)
		}
	}
	if d := doc.Default; d.Font.Size != 10 || len(d.Font.Families) == 0 || d.Font.Families[0] != "Liberation Sans" || d.Format != "General" || d.Padding[0] != 1 {
		t.Errorf("default = %+v", d)
	}
}

func TestRead_PrintFalseAlone(t *testing.T) {
	doc, _ := read(t, flat{body: `<table:table table:name="A" table:print="false"/><table:table table:name="B" table:print="false"/>`}.bytes())
	if doc.Sheets[0].Hidden || doc.Sheets[1].Hidden {
		t.Errorf("sheets = %+v %+v", doc.Sheets[0], doc.Sheets[1])
	}
}

func TestRead_ColumnsRows(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="co1" style:family="table-column"><style:table-column-properties style:column-width="1in"/></style:style>` +
			`<style:style style:name="co2" style:family="table-column"><style:table-column-properties style:column-width="2in" fo:break-before="page"/></style:style>` +
			`<style:style style:name="ro1" style:family="table-row"><style:table-row-properties style:row-height="0.178in" style:use-optimal-row-height="true"/></style:style>` +
			`<style:style style:name="ro2" style:family="table-row"><style:table-row-properties style:row-height="0.5in" style:use-optimal-row-height="false" fo:break-before="page"/></style:style>`,
		body: table("S",
			`<table:table-header-columns><table:table-column table:style-name="co1"/></table:table-header-columns>`+
				`<table:table-column table:style-name="co2" table:number-columns-repeated="2"/>`+
				`<table:table-column-group table:display="false"><table:table-column table:style-name="co1"/></table:table-column-group>`+
				`<table:table-column table:style-name="co1" table:visibility="collapse"/>`+
				`<table:table-column table:style-name="co1" table:number-columns-repeated="16380"/>`+
				`<table:table-header-rows><table:table-row table:style-name="ro1"><table:table-cell office:value-type="string"><text:p>h</text:p></table:table-cell></table:table-row></table:table-header-rows>`+
				`<table:table-row table:style-name="ro1" table:number-rows-repeated="3"><table:table-cell office:value-type="float" office:value="1"><text:p>1</text:p></table:table-cell></table:table-row>`+
				`<table:table-row table:style-name="ro2"><table:table-cell/></table:table-row>`+
				`<table:table-row-group table:display="false"><table:table-row table:style-name="ro1"><table:table-cell office:value-type="string"><text:p>x</text:p></table:table-cell></table:table-row></table:table-row-group>`+
				`<table:table-row table:style-name="ro1" table:number-rows-repeated="1048570"><table:table-cell table:number-columns-repeated="1024"/></table:table-row>`),
	}
	doc, _ := read(t, f.bytes())
	sh := doc.Sheets[0]
	wantCols := []sheet.Column{
		{First: 0, Last: 0, Width: 72},
		{First: 1, Last: 2, Width: 144},
		{First: 3, Last: 3, Width: 72, Hidden: true},
		{First: 4, Last: 4, Width: 72, Hidden: true},
		{First: 5, Last: 16383, Width: 72},
	}
	if len(sh.Columns) != len(wantCols) {
		t.Fatalf("columns = %+v", sh.Columns)
	}
	for i, c := range wantCols {
		if g := sh.Columns[i]; g.First != c.First || g.Last != c.Last || !near(g.Width, c.Width) || g.Hidden != c.Hidden {
			t.Errorf("column %d = %+v, want %+v", i, g, c)
		}
	}
	if got := sh.Page.ColumnBreaks; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("column breaks = %v", got)
	}
	if rc := sh.Page.RepeatColumns; rc == nil || rc.FirstColumn != 0 || rc.LastColumn != 0 {
		t.Errorf("repeat columns = %+v", rc)
	}
	if rr := sh.Page.RepeatRows; rr == nil || rr.FirstRow != 0 || rr.LastRow != 0 {
		t.Errorf("repeat rows = %+v", rr)
	}
	var idx []int
	for _, r := range sh.Rows {
		idx = append(idx, r.Index)
	}
	if want := []int{0, 1, 2, 3, 4, 5}; len(idx) != len(want) {
		t.Fatalf("rows = %v", idx)
	}
	if r := sh.Rows[4]; !r.CustomHeight || !near(r.Height, 36) || len(r.Cells) != 0 {
		t.Errorf("row 4 = %+v", r)
	}
	if r := sh.Rows[5]; !r.Hidden || len(r.Cells) != 1 {
		t.Errorf("row 5 = %+v", r)
	}
	if got := sh.Page.RowBreaks; len(got) != 1 || got[0] != 4 {
		t.Errorf("row breaks = %v", got)
	}
	if !near(sh.DefaultRowHeight, 12.8) || !near(sh.DefaultColumnWidth, 72) {
		t.Errorf("defaults = %v, %v", sh.DefaultRowHeight, sh.DefaultColumnWidth)
	}
}

// TestCalcTwips checks the twips Calc holds a row height or column width
// read from a file in, measured in its PDFs: the length rounded to 1/100
// mm, then to twips.
func TestCalcTwips(t *testing.T) {
	tests := []struct {
		length string
		twips  float64
	}{
		{"0.208in", 299}, {"0.2in", 288}, {"0.5cm", 283}, {"5.3mm", 300},
		{"5.307mm", 301}, {"5.303mm", 300}, {"15.3pt", 306}, {"15.33pt", 307},
		{"0.6cm", 340}, {"0.3333in", 480}, {"0.889in", 1280}, {"0.667in", 960},
	}
	for _, tt := range tests {
		v, ok := odf.Length(tt.length)
		if !ok {
			t.Fatalf("odf.Length(%q) failed", tt.length)
		}
		if got := calcTwips(v); math.Abs(got*20-tt.twips) > 1e-9 {
			t.Errorf("calcTwips(%s) = %v twips, want %v", tt.length, got*20, tt.twips)
		}
	}
}

// TestRead_OptimalFloor checks that a row of optimal height shorter than
// Calc's standard height reads at the standard, as Calc recomputes it,
// while a row of the user's height and a taller row keep theirs.
func TestRead_OptimalFloor(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="ro1" style:family="table-row"><style:table-row-properties style:row-height="0.1681in" style:use-optimal-row-height="true"/></style:style>` +
			`<style:style style:name="ro2" style:family="table-row"><style:table-row-properties style:row-height="0.1in" style:use-optimal-row-height="false"/></style:style>` +
			`<style:style style:name="ro3" style:family="table-row"><style:table-row-properties style:row-height="0.5in"/></style:style>`,
		body: table("S",
			`<table:table-row table:style-name="ro1"><table:table-cell office:value-type="string"><text:p>a</text:p></table:table-cell></table:table-row>`+
				`<table:table-row table:style-name="ro2"><table:table-cell office:value-type="string"><text:p>b</text:p></table:table-cell></table:table-row>`+
				`<table:table-row table:style-name="ro3"><table:table-cell office:value-type="string"><text:p>c</text:p></table:table-cell></table:table-row>`),
	}
	doc, _ := read(t, f.bytes())
	rows := doc.Sheets[0].Rows
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	for i, want := range []float64{12.8, 7.2, 36} {
		if !near(rows[i].Height, want) {
			t.Errorf("row %d height %v, want %v", i, rows[i].Height, want)
		}
	}
}

func TestRead_Values(t *testing.T) {
	cells := []struct {
		xml  string
		want sheet.Value
		href string
	}{
		{`<table:table-cell office:value-type="float" office:value="2500.5"><text:p>2500.5</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Number, Number: 2500.5, Text: "2500.5"}, ""},
		{`<table:table-cell office:value-type="percentage" office:value="0.25"><text:p>25%</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Number, Number: 0.25, Text: "25%"}, ""},
		{`<table:table-cell office:value-type="currency" office:currency="USD" office:value="-3"><text:p>-$3.00</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Number, Number: -3, Text: "-$3.00"}, ""},
		{`<table:table-cell office:value-type="date" office:date-value="2026-01-31"><text:p>x</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Number, Number: 46053, Text: "x"}, ""},
		{`<table:table-cell office:value-type="date" office:date-value="1899-12-31T12:00:00"><text:p>x</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Number, Number: 1.5, Text: "x"}, ""},
		{`<table:table-cell office:value-type="time" office:time-value="PT36H00M00S"><text:p>x</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Number, Number: 1.5, Text: "x"}, ""},
		{`<table:table-cell office:value-type="boolean" office:boolean-value="true"><text:p>TRUE</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Bool, Number: 1, Text: "TRUE"}, ""},
		{`<table:table-cell office:value-type="boolean" office:boolean-value="8"><text:p>TRUE</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Bool, Number: 1, Text: "TRUE"}, ""},
		{`<table:table-cell office:value-type="string"><text:p>a<text:s text:c="3"/>b<text:tab/>c<text:line-break/>d</text:p><text:p>e  f</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Text, Text: "a   b\tc\nd\ne f"}, ""},
		{`<table:table-cell office:value-type="string" office:string-value="stored"><text:p>shown</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Text, Text: "stored"}, ""},
		{`<table:table-cell office:value-type="float" office:value="0" calcext:value-type="error"><text:p>#DIV/0!</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Error, Text: "#DIV/0!"}, ""},
		{`<table:table-cell office:value-type="string"><text:p><text:a xlink:href="https://example.com/">site</text:a></text:p></table:table-cell>`, sheet.Value{Kind: sheet.Text, Text: "site"}, "https://example.com/"},
		{`<table:table-cell><text:p>bare</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Text, Text: "bare"}, ""},
		{`<table:table-cell office:value-type="string"><text:p><text:ruby><text:ruby-base>漢</text:ruby-base><text:ruby-text>かん</text:ruby-text></text:ruby>字</text:p></table:table-cell>`, sheet.Value{Kind: sheet.Text, Text: "漢字"}, ""},
	}
	var row strings.Builder
	for _, c := range cells {
		row.WriteString(c.xml)
	}
	doc, _ := read(t, flat{body: table("S", `<table:table-row>`+row.String()+`</table:table-row>`)}.bytes())
	sh := doc.Sheets[0]
	for i, c := range cells {
		got := cellAt(sh, 0, i)
		if got == nil {
			t.Errorf("cell %d missing", i)
			continue
		}
		if got.Value.Kind != c.want.Kind || !near(got.Value.Number, c.want.Number) || got.Value.Text != c.want.Text || got.Href != c.href {
			t.Errorf("cell %d = %+v %q, want %+v %q", i, got.Value, got.Href, c.want, c.href)
		}
	}
}

func TestRead_RunsAndMerges(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="T1" style:family="text"><style:text-properties fo:font-weight="bold" fo:color="#ff0000"/></style:style>`,
		body: table("S", `<table:table-column table:number-columns-repeated="3"/><table:table-row>`+
			`<table:table-cell office:value-type="string" table:number-columns-spanned="2" table:number-rows-spanned="2"><text:p>plain <text:span text:style-name="T1">bold</text:span></text:p></table:table-cell>`+
			`<table:covered-table-cell/><table:table-cell table:number-columns-repeated="2" office:value-type="float" office:value="7"><text:p>7</text:p></table:table-cell></table:table-row>`),
	}
	doc, _ := read(t, f.bytes())
	sh := doc.Sheets[0]
	c := cellAt(sh, 0, 0)
	if c == nil || len(c.Runs) != 2 || c.Runs[0].Font != nil || c.Runs[1].Font == nil || !c.Runs[1].Font.Bold || c.Runs[1].Font.Color != (css.Color{R: 1, A: 1}) || c.Runs[1].Text != "bold" {
		t.Fatalf("runs = %+v", c)
	}
	if len(sh.Merges) != 1 || sh.Merges[0] != (sheet.Range{FirstRow: 0, FirstColumn: 0, LastRow: 1, LastColumn: 1}) {
		t.Errorf("merges = %+v", sh.Merges)
	}
	if cellAt(sh, 0, 2) == nil || cellAt(sh, 0, 3) == nil || cellAt(sh, 0, 1) != nil {
		t.Errorf("repeated cells = %+v", sh.Rows)
	}
}

func TestRead_Styles(t *testing.T) {
	f := flat{
		fonts: `<style:font-face style:name="DejaVu" svg:font-family="'DejaVu Sans'"/><style:font-face style:name="CJK" svg:font-family="'Noto Sans CJK JP'"/>`,
		styles: `<style:default-style style:family="table-cell"><style:text-properties style:font-name="DejaVu" fo:font-size="11pt"/></style:default-style>` +
			`<style:style style:name="Default" style:family="table-cell"/>` +
			`<style:style style:name="Base" style:family="table-cell" style:parent-style-name="Default"><style:table-cell-properties fo:background-color="#00ff00"/></style:style>`,
		auto: `<number:number-style style:name="N1"><number:number number:decimal-places="2" number:min-decimal-places="2" number:min-integer-digits="1" number:grouping="true"/></number:number-style>` +
			`<style:style style:name="ce1" style:family="table-cell" style:parent-style-name="Base" style:data-style-name="N1">` +
			`<style:table-cell-properties fo:border="0.74pt solid #808080" fo:border-top="1.1pt double #0000ff" style:border-line-width-top="0.02cm 0.05cm 0.02cm" fo:padding="0.1in" style:diagonal-tl-br="0.5pt dashed #000000" ` +
			`style:vertical-align="middle" fo:wrap-option="wrap" style:shrink-to-fit="true" style:rotation-angle="270" style:text-align-source="fix" style:direction="ttb"/>` +
			`<style:paragraph-properties fo:text-align="end" fo:margin-left="0.25in"/>` +
			`<style:text-properties style:font-name-asian="CJK" fo:font-size="150%" fo:font-weight="bold" fo:font-style="italic" style:text-underline-style="solid" style:text-underline-type="double" ` +
			`style:text-line-through-style="solid" fo:color="#336699" style:text-position="super 58%"/></style:style>` +
			`<style:style style:name="ce2" style:family="table-cell" style:parent-style-name="Default"><style:table-cell-properties style:text-align-source="value-type" style:repeat-content="false"/><style:paragraph-properties fo:text-align="center"/></style:style>` +
			`<style:style style:name="ce3" style:family="table-cell" style:parent-style-name="Default"><style:table-cell-properties fo:background-color="#ffcc00"/></style:style>`,
		body: table("S", `<table:table-row><table:table-cell table:style-name="ce1" office:value-type="float" office:value="1"><text:p>1</text:p></table:table-cell>`+
			`<table:table-cell table:style-name="ce2" office:value-type="float" office:value="1"><text:p>1</text:p></table:table-cell>`+
			`<table:table-cell table:style-name="ce3" table:number-columns-repeated="2"/><table:table-cell table:style-name="ce2"/></table:table-row>`),
	}
	doc, _ := read(t, f.bytes())
	if d := doc.Default.Font; d.Families[0] != "DejaVu Sans" || d.Size != 11 {
		t.Errorf("default font = %+v", d)
	}
	sh := doc.Sheets[0]
	st := cellAt(sh, 0, 0).Style
	if st == nil {
		t.Fatal("no style")
	}
	fn := st.Font
	if len(fn.Families) != 2 || fn.Families[0] != "DejaVu Sans" || fn.Families[1] != "Noto Sans CJK JP" || !near(fn.Size, 11) || !fn.Bold || !fn.Italic ||
		fn.Underline != sheet.Double || !fn.Strike || fn.Script != sheet.Superscript || !near(fn.Color.B, 0.6) {
		t.Errorf("font = %+v", fn)
	}
	if st.Fill.Color != (css.Color{G: 1, A: 1}) {
		t.Errorf("fill = %+v", st.Fill)
	}
	top, right := st.Border[css.Top], st.Border[css.Right]
	if top.Line != sheet.LineDouble || !near(top.Width, 0.09*72/2.54) || !near(top.Gap, 0.05*72/2.54) || top.Color != (css.Color{B: 1, A: 1}) {
		t.Errorf("top = %+v", top)
	}
	if right.Line != sheet.LineSolid || !near(right.Width, 0.74) {
		t.Errorf("right = %+v", right)
	}
	if !st.DiagonalDown || st.DiagonalUp || st.Diagonal.Line != sheet.LineDashed {
		t.Errorf("diagonal = %+v %v %v", st.Diagonal, st.DiagonalDown, st.DiagonalUp)
	}
	if !near(st.Padding[css.Left], 7.2) {
		t.Errorf("padding = %v", st.Padding)
	}
	a := st.Align
	if a.Horizontal != sheet.HRight || a.Vertical != sheet.VMiddle || !a.Wrap || !a.Shrink || !near(a.Indent, 18) || a.Rotation != -90 || !a.Stacked {
		t.Errorf("align = %+v", a)
	}
	if st.Format != "#,##0.00" {
		t.Errorf("format = %q", st.Format)
	}
	if g := cellAt(sh, 0, 1).Style; g == nil || g.Align.Horizontal != sheet.HGeneral || g.Padding[0] != 1 {
		t.Errorf("value-type alignment = %+v", g)
	}
	if cellAt(sh, 0, 2) == nil || cellAt(sh, 0, 3) == nil || cellAt(sh, 0, 4) != nil {
		t.Errorf("styled empty cells = %+v", sh.Rows[0].Cells)
	}
}

func TestRead_Pages(t *testing.T) {
	f := flat{
		styles: `<style:style style:name="Hd" style:family="text"><style:text-properties fo:font-weight="bold"/></style:style>`,
		auto: `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="29.7cm" fo:page-height="21cm" style:print-orientation="landscape" ` +
			`fo:margin="1cm" fo:margin-left="3cm" style:print="grid headers" style:print-page-order="ltr" style:first-page-number="5" style:scale-to="80%" style:table-centering="both"/>` +
			`<style:header-style><style:header-footer-properties fo:min-height="2cm" fo:margin-bottom="0.5cm"/></style:header-style>` +
			`<style:footer-style><style:header-footer-properties fo:min-height="1.5cm" fo:margin-top="0.3cm"/></style:footer-style></style:page-layout>` +
			`<style:page-layout style:name="pm2"><style:page-layout-properties style:print-orientation="landscape" style:scale-to-X="2" style:scale-to-Y="0"/></style:page-layout>` +
			`<style:style style:name="ta1" style:family="table" style:master-page-name="Report"/><style:style style:name="ta2" style:family="table" style:master-page-name="Fit"/>`,
		master: `<style:master-page style:name="Report" style:page-layout-name="pm1">` +
			`<style:header><style:region-left><text:p><text:sheet-name>?</text:sheet-name><text:s/>(<text:title>?</text:title>)</text:p></style:region-left>` +
			`<style:region-right><text:p><text:span text:style-name="Hd"><text:date>d</text:date></text:span>, <text:time>t</text:time></text:p></style:region-right></style:header>` +
			`<style:header-left><text:p>Even</text:p></style:header-left><style:header-first style:display="false"/>` +
			`<style:footer><text:p>Page <text:page-number>1</text:page-number> / <text:page-count>9</text:page-count> <text:file-name text:display="full">f</text:file-name></text:p></style:footer>` +
			`<style:footer-left style:display="false"/></style:master-page>` +
			`<style:master-page style:name="Fit" style:page-layout-name="pm2"><style:header style:display="false"/></style:master-page>`,
		body: `<table:table table:name="A" table:style-name="ta1" table:print-ranges="A.A1:A.C5 'A'.$E$2"/><table:table table:name="B" table:style-name="ta2"/><table:table table:name="C"/>`,
	}
	doc, _ := read(t, f.bytes())
	pg, hf := doc.Sheets[0].Page, doc.Sheets[0].HeaderFooter
	cm := 72 / 2.54
	if !near(pg.Width, 29.7*cm) || !near(pg.Height, 21*cm) {
		t.Errorf("size = %v x %v", pg.Width, pg.Height)
	}
	// Calc holds each length in whole twips: 1 cm is 567, 2 cm 1,134,
	// 3 cm 1,701, 1.5 cm 850, 0.5 cm 283 and 0.3 cm 170.
	tw := func(n float64) float64 { return n / 20 }
	if !near(pg.Margin[css.Top], tw(567+1134)) || !near(pg.Margin[css.Bottom], tw(567+850)) || !near(pg.Margin[css.Left], tw(1701)) || !near(pg.Margin[css.Right], tw(567)) ||
		!near(pg.HeaderTop, tw(567)) || !near(pg.FooterBottom, tw(567)) || !near(hf.HeaderMinHeight, tw(1134-283)) || !near(hf.FooterMinHeight, tw(850-170)) {
		t.Errorf("margins = %+v %v %v, hf %v %v", pg.Margin, pg.HeaderTop, pg.FooterBottom, hf.HeaderMinHeight, hf.FooterMinHeight)
	}
	if !pg.GridLines || !pg.Headings || !doc.Sheets[0].HideZeros || pg.Order != sheet.OverThenDown || pg.FirstPageNumber != 5 || pg.Scale != 80 || !pg.CenterHorizontally || !pg.CenterVertically {
		t.Errorf("print = %+v", pg)
	}
	if len(pg.PrintAreas) != 2 || pg.PrintAreas[0] != (sheet.Range{LastRow: 4, LastColumn: 2}) || pg.PrintAreas[1] != (sheet.Range{FirstRow: 1, FirstColumn: 4, LastRow: 1, LastColumn: 4}) {
		t.Errorf("print areas = %+v", pg.PrintAreas)
	}
	left := hf.Odd.Header[0]
	if len(left) != 4 || left[0].Field != sheet.FieldSheet || left[1].Text != " (" || left[2].Field != sheet.FieldTitle || left[3].Text != ")" {
		t.Errorf("header left = %+v", left)
	}
	right := hf.Odd.Header[2]
	if len(right) != 3 || right[0].Field != sheet.FieldDate || right[0].Font == nil || !right[0].Font.Bold || right[2].Field != sheet.FieldTime {
		t.Errorf("header right = %+v", right)
	}
	foot := hf.Odd.Footer[1]
	if len(foot) != 6 || foot[1].Field != sheet.FieldPage || foot[3].Field != sheet.FieldPages || foot[5].Field != sheet.FieldPath {
		t.Errorf("footer = %+v", foot)
	}
	if !hf.DifferentOddEven || hf.DifferentFirst || len(hf.Even.Header[1]) != 1 || hf.Even.Header[1][0].Text != "Even" || len(hf.Even.Footer[1]) != 6 {
		t.Errorf("variants = %+v", hf)
	}
	fit := doc.Sheets[1]
	if fit.Page.Scale != 0 || fit.Page.FitWidth != 2 || fit.Page.FitHeight != 0 || len(fit.HeaderFooter.Odd.Header[1]) != 0 ||
		!near(fit.Page.Margin[css.Top], 2*cm) || fit.Page.Width != sheet.FallbackWidth {
		t.Errorf("fit = %+v %+v", fit.Page, fit.HeaderFooter)
	}
	def := doc.Sheets[2]
	if def.Page.Width != sheet.FallbackWidth || def.Page.Height != sheet.FallbackHeight || !near(def.Page.Margin[css.Top], 2.75*cm) || def.HeaderFooter.Odd.Header[1][0].Field != sheet.FieldSheet ||
		def.HeaderFooter.Odd.Footer[1][1].Field != sheet.FieldPage || def.Page.Scale != 100 {
		t.Errorf("default page = %+v %+v", def.Page, def.HeaderFooter)
	}
}

// TestRead_Drawings locks how frames are read: a frame of the sheet
// placed from its origin, a frame of a cell from the cell's corner, its
// offsets free to run past the cell, as Calc reads them.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf125938_anchor_after_copy_paste.ods prints its picture at
//     its svg:x and svg:y from its anchor cell's corner.
func TestRead_Drawings(t *testing.T) {
	manifest := `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0">` +
		`<manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.spreadsheet"/>` +
		`<manifest:file-entry manifest:full-path="Object 1/" manifest:media-type="application/vnd.oasis.opendocument.chart"/>` +
		`<manifest:file-entry manifest:full-path="Object 2/" manifest:media-type="application/vnd.oasis.opendocument.formula"/></manifest:manifest>`
	content := `<office:document-content ` + nsDecl + `><office:automatic-styles>` +
		`<style:style style:name="co1" style:family="table-column"><style:table-column-properties style:column-width="1in"/></style:style>` +
		`<style:style style:name="ro1" style:family="table-row"><style:table-row-properties style:row-height="0.25in"/></style:style>` +
		`</office:automatic-styles><office:body><office:spreadsheet><table:table table:name="S">` +
		`<table:shapes><draw:frame draw:name="logo" svg:x="1in" svg:y="2in" svg:width="1in" svg:height="0.5in"><draw:image xlink:href="Pictures/a.png"/><svg:title>Logo</svg:title><svg:desc>Company logo</svg:desc></draw:frame>` +
		`<draw:caption svg:x="0in" svg:y="0in"/></table:shapes>` +
		`<table:table-column table:style-name="co1" table:number-columns-repeated="4"/>` +
		`<table:table-row table:style-name="ro1"><table:table-cell/></table:table-row>` +
		`<table:table-row table:style-name="ro1"><table:table-cell/><table:table-cell>` +
		`<draw:frame draw:name="chart" svg:x="1.25in" svg:y="0.3in" svg:width="2in" svg:height="1in" table:end-cell-address="S.D5" table:end-x="0.1in" table:end-y="0.2in">` +
		`<draw:object xlink:href="./Object 1"/><draw:image xlink:href="./ObjectReplacements/Object 1"/></draw:frame>` +
		`<draw:frame svg:x="1in" svg:y="0.25in" svg:width="1in" svg:height="1in"><draw:object xlink:href="./Object 2"/><draw:image xlink:href="Pictures/a.png"/></draw:frame>` +
		`</table:table-cell></table:table-row></table:table></office:spreadsheet></office:body></office:document-content>`
	chartDoc := `<office:document-content ` + nsDecl + ` xmlns:chart="urn:oasis:names:tc:opendocument:xmlns:chart:1.0">` +
		`<office:body><office:chart><chart:chart chart:class="chart:bar" svg:width="2in" svg:height="1in"><chart:plot-area>` +
		`<chart:series chart:values-cell-range-address="S.B2:S.B3"/></chart:plot-area></chart:chart></office:chart></office:body></office:document-content>`
	data := zipOf(t, "mimetype", "application/vnd.oasis.opendocument.spreadsheet", "META-INF/manifest.xml", manifest, "content.xml", content,
		"Object 1/content.xml", chartDoc, "Pictures/a.png", string(tinyPNG(t)))
	doc, w := read(t, data)
	ds := doc.Sheets[0].Drawings
	if len(ds) != 3 {
		t.Fatalf("drawings = %+v", ds)
	}
	if d := ds[0]; !d.Absolute || !near(d.X, 72) || !near(d.Y, 144) || !near(d.Width, 72) || d.Picture == nil || d.Name != "logo" || d.Alt != "Company logo" || d.Decorative {
		t.Errorf("logo = %+v", d)
	}
	if d := ds[1]; d.Chart == nil || d.Picture != nil || d.From.Row != 1 || d.From.Column != 1 || !near(d.From.DX, 90) || !near(d.From.DY, 21.6) || !d.Unbounded ||
		d.To == nil || d.To.Row != 4 || d.To.Column != 3 || !near(d.To.DX, 7.2) || !near(d.To.DY, 14.4) {
		t.Errorf("chart = %+v to %+v", d, d.To)
	}
	if d := ds[2]; d.Chart != nil || d.Picture == nil || d.To != nil || !near(d.Height, 72) {
		t.Errorf("formula replacement = %+v", d)
	}
	if !hasWarning(w, warn.ContentUnsupported) {
		t.Errorf("warnings = %+v", w)
	}
}

// TestRead_MetafileWarnings locks the warning code of each metafile
// format, which is not drawn.
func TestRead_MetafileWarnings(t *testing.T) {
	emf := make([]byte, 48)
	copy(emf[40:], " EMF")
	frame := func(name string) string {
		return `<draw:frame svg:width="1in" svg:height="1in"><draw:image xlink:href="Pictures/` + name + `"/></draw:frame>`
	}
	content := `<office:document-content ` + nsDecl + `><office:body><office:spreadsheet><table:table table:name="S"><table:shapes>` +
		frame("a.wmf") + frame("b.emf") + frame("c.svm") + `</table:shapes></table:table></office:spreadsheet></office:body></office:document-content>`
	data := zipOf(t, "mimetype", "application/vnd.oasis.opendocument.spreadsheet", "content.xml", content,
		"Pictures/a.wmf", "\xd7\xcd\xc6\x9a0000", "Pictures/b.emf", string(emf), "Pictures/c.svm", "VCLMTF0000")
	doc, w := read(t, data)
	if n := len(doc.Sheets[0].Drawings); n != 0 {
		t.Errorf("%d drawings, want none", n)
	}
	for _, format := range []string{"WMF", "EMF", "SVM"} {
		if !hasWarningAbout(w, warn.PictureFormatUnsupported, format) {
			t.Errorf("no %s warning about %s in %+v", warn.PictureFormatUnsupported, format, w)
		}
	}
}

func TestRead_MetaAndWarnings(t *testing.T) {
	f := flat{
		meta: `<dc:title>Budget</dc:title><dc:creator>Ann</dc:creator><meta:keyword>a</meta:keyword><meta:keyword>b</meta:keyword><dc:language>fr-FR</dc:language>`,
		body: `<table:table table:name="S"><calcext:conditional-formats/><table:table-row><table:table-cell office:value-type="string"><office:annotation><text:p>note</text:p></office:annotation><table:detective><table:highlighted-range table:marked-invalid="true"/></table:detective><text:p>v</text:p></table:table-cell></table:table-row></table:table>` +
			`<table:content-validations/><table:named-expressions><table:named-range table:name="x"/></table:named-expressions>`,
	}
	doc, w := read(t, f.bytes())
	if m := doc.Meta; m.Title != "Budget" || m.Author != "Ann" || m.Keywords != "a, b" || m.Lang != "fr-FR" {
		t.Errorf("meta = %+v", m)
	}
	if c := cellAt(doc.Sheets[0], 0, 0); c == nil || c.Value.Text != "v" || !c.MarkedInvalid {
		t.Errorf("annotated cell = %+v", c)
	}
	if n := doc.Sheets[0].Notes; len(n) != 1 || n[0].Text != "note" {
		t.Errorf("notes = %+v, want the cell's note, which its page style does not print", n)
	}
	for _, about := range []string{"cell validations", "named ranges"} {
		if !hasWarningAbout(w, warn.ContentUnsupported, about) {
			t.Errorf("no %s warning about %s in %+v", warn.ContentUnsupported, about, w)
		}
	}
}

func TestRead_Malformed(t *testing.T) {
	text := []byte(`<office:document ` + nsDecl + ` office:mimetype="application/vnd.oasis.opendocument.text"><office:body><office:text/></office:body></office:document>`)
	noBody := []byte(`<office:document ` + nsDecl + `><office:body><office:text/></office:body></office:document>`)
	for name, data := range map[string][]byte{"text": text, "no spreadsheet": noBody, "garbage": []byte("not xml")} {
		if _, _, err := Read(context.Background(), data, nil, office.Options{}); !errors.Is(err, office.ErrMalformed) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestRead_Caps(t *testing.T) {
	body := table("S", `<table:table-row table:number-rows-repeated="1048576"><table:table-cell table:number-columns-repeated="16384" office:value-type="float" office:value="1"><text:p>1</text:p></table:table-cell></table:table-row>`)
	doc, w := read(t, flat{body: body}.bytes())
	n := 0
	for _, r := range doc.Sheets[0].Rows {
		n += len(r.Cells)
	}
	if n > sheet.MaxCells || !hasWarning(w, warn.LimitReached) {
		t.Errorf("cells = %d, warnings %+v", n, w)
	}
}

func TestRead_Formulas(t *testing.T) {
	row := `<table:table-row><table:table-cell table:formula="of:=[.B1]+2" office:value-type="float" office:value="5"><text:p>5</text:p></table:table-cell>` +
		`<table:table-cell table:formula="of:=1"/><table:table-cell office:value-type="float" office:value="3"/></table:table-row>`
	doc, _ := read(t, flat{body: table("S", row)}.bytes())
	sh := doc.Sheets[0]
	for i, want := range []string{"of:=[.B1]+2", "of:=1", ""} {
		c := cellAt(sh, 0, i)
		if c == nil || c.Formula != want {
			t.Errorf("cell %d: %+v, want the formula %q", i, c, want)
		}
	}
	if c := cellAt(sh, 0, 0); c.Value.Number != 5 {
		t.Errorf("the stored value %+v", c.Value)
	}
	// Never keeps no formula: A1 shows its stored 5, B1, which stores
	// nothing, is not listed.
	doc, _, err := Read(context.Background(), flat{body: table("S", row)}.bytes(), nil, office.Options{Recalculation: models.RecalculateNever})
	if err != nil {
		t.Fatal(err)
	}
	sh = doc.Sheets[0]
	if c := cellAt(sh, 0, 0); c == nil || c.Formula != "" || c.Value.Number != 5 {
		t.Errorf("never: A1 %+v", c)
	}
	if c := cellAt(sh, 0, 1); c != nil {
		t.Errorf("never: B1 %+v, want nothing", c)
	}
}

func TestRead_DataStyleLocale(t *testing.T) {
	styles := `<number:number-style style:name="N1" number:language="de" number:country="DE"><number:number number:decimal-places="2" number:min-integer-digits="1" number:grouping="true"/></number:number-style>` +
		`<number:number-style style:name="N2" number:language="en" number:country="US"><number:number number:decimal-places="2" number:min-integer-digits="1"/></number:number-style>` +
		`<style:style style:name="ce1" style:family="table-cell" style:data-style-name="N1"/><style:style style:name="ce2" style:family="table-cell" style:data-style-name="N2"/>`
	row := `<table:table-row><table:table-cell table:style-name="ce1" office:value-type="float" office:value="1"/><table:table-cell table:style-name="ce2" office:value-type="float" office:value="1"/></table:table-row>`
	doc, _ := read(t, flat{auto: styles, body: table("S", row)}.bytes())
	if c := cellAt(doc.Sheets[0], 0, 0); c == nil || c.Style == nil || c.Style.Format != "[$-407]#,##0.00" {
		t.Errorf("German format %+v", c)
	}
	// en-US is named too: under another conversion locale it stays en-US.
	if c := cellAt(doc.Sheets[0], 0, 1); c == nil || c.Style == nil || c.Style.Format != "[$-409]0.00" {
		t.Errorf("en-US format %+v", c)
	}
}

// TestRead_TableGroupDepth locks the bound on the nesting of the elements
// that hold columns and rows, table:table-columns and table:table-rows
// alike: a column and a row inside [maxGroupDepth] of them are read, those
// inside one more are left out with a warning.
func TestRead_TableGroupDepth(t *testing.T) {
	auto := `<style:style style:name="co1" style:family="table-column"><style:table-column-properties style:column-width="1in"/></style:style>`
	nested := func(n int, local, inner string) string {
		return strings.Repeat(`<table:`+local+`>`, n) + inner + strings.Repeat(`</table:`+local+`>`, n)
	}
	column := `<table:table-column table:style-name="co1"/>`
	row := `<table:table-row><table:table-cell office:value-type="float" office:value="1"/></table:table-row>`
	for _, tc := range []struct {
		depth   int
		read    bool
		warning bool
	}{{maxGroupDepth, true, false}, {maxGroupDepth + 1, false, true}} {
		body := table("S", nested(tc.depth, "table-columns", column)+nested(tc.depth, "table-rows", row))
		doc, w := read(t, flat{auto: auto, body: body}.bytes())
		sh := doc.Sheets[0]
		if got := len(sh.Columns) == 1 && cellAt(sh, 0, 0) != nil; got != tc.read {
			t.Errorf("depth %d: columns %+v, rows %+v, want read %v", tc.depth, sh.Columns, sh.Rows, tc.read)
		}
		if got := hasWarning(w, warn.LimitReached); got != tc.warning {
			t.Errorf("depth %d: warnings %+v, want a table-group-depth warning %v", tc.depth, w, tc.warning)
		}
	}
}

// TestRead_HeaderOfManySpaces locks that a header part's text is joined
// in time linear in its nodes: a paragraph of 900,000 text:s, each
// merged into one span, is read within seconds, where joining the
// span's string at each node copies it as many times.
func TestRead_HeaderOfManySpaces(t *testing.T) {
	const n = 900_000
	f := flat{
		auto:   `<style:style style:name="ta1" style:family="table" style:master-page-name="Long"/>`,
		master: `<style:master-page style:name="Long"><style:header><text:p>` + strings.Repeat(`<text:s/>`, n) + `</text:p></style:header></style:master-page>`,
		body:   `<table:table table:name="S" table:style-name="ta1"/>`,
	}
	type result struct {
		doc *sheet.Document
		err error
	}
	done := make(chan result, 1)
	go func() {
		doc, _, err := Read(context.Background(), f.bytes(), nil, office.Options{})
		done <- result{doc, err}
	}()
	var res result
	select {
	case res = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the header is not read within 30 s")
	}
	if res.err != nil {
		t.Fatal(res.err)
	}
	spans := res.doc.Sheets[0].HeaderFooter.Odd.Header[1]
	if len(spans) != 1 || spans[0].Text != strings.Repeat(" ", min(n, maxPartText)) {
		t.Errorf("%d spans, want one of %d spaces", len(spans), min(n, maxPartText))
	}
}

// TestRead_PageNumberAdjust locks that a header's page number field shows
// the page's own number whatever text:page-adjust and text:select-page
// ask (ODF 1.3 Part 3, 7.3.4): Calc's header field holds no offset.
//
// # Corpus evidence
//
//   - probe, ods: a flat ODS of three pages from page 5 whose header holds
//     page numbers adjusted by 1, -1 and 10 and selecting the next and the
//     previous page, converted by Gotenberg 9.4.1, prints "P=5 A=5 B=5 C=5
//     N=5 V=5 T=3" on its first page, the page's number in every field.
func TestRead_PageNumberAdjust(t *testing.T) {
	f := flat{
		auto:   `<style:style style:name="ta1" style:family="table" style:master-page-name="Adjusted"/>`,
		master: `<style:master-page style:name="Adjusted"><style:header><text:p><text:page-number text:page-adjust="1">2</text:page-number><text:page-number text:page-adjust="-1" text:select-page="previous">0</text:page-number><text:page-number text:select-page="next">2</text:page-number></text:p></style:header></style:master-page>`,
		body:   `<table:table table:name="S" table:style-name="ta1"/>`,
	}
	doc, _ := read(t, f.bytes())
	spans := doc.Sheets[0].HeaderFooter.Odd.Header[1]
	if len(spans) != 3 {
		t.Fatalf("spans %+v, want three page numbers", spans)
	}
	for _, s := range spans {
		if s.Field != sheet.FieldPage || s.Offset != 0 {
			t.Errorf("span %+v, want the page number with no offset", s)
		}
	}
}

// TestRead_HeaderTextCap locks the bound on a header part's text: spaces
// past [maxPartText] bytes are left out.
func TestRead_HeaderTextCap(t *testing.T) {
	f := flat{
		auto:   `<style:style style:name="ta1" style:family="table" style:master-page-name="Long"/>`,
		master: `<style:master-page style:name="Long"><style:header><text:p>a` + strings.Repeat(`<text:s text:c="1024"/>`, 2000) + `</text:p></style:header></style:master-page>`,
		body:   `<table:table table:name="S" table:style-name="ta1"/>`,
	}
	doc, _ := read(t, f.bytes())
	n := 0
	for _, s := range doc.Sheets[0].HeaderFooter.Odd.Header[1] {
		n += len(s.Text)
	}
	if n == 0 || n > maxPartText {
		t.Errorf("%d bytes of header text, want at most %d", n, maxPartText)
	}
}

// TestRead_HeaderGeometry reads a header of fo:min-height as one that
// grows with its content and one of svg:height alone as fixed, both with
// their spacing, and gives header text the book's default cell font.
func TestRead_HeaderGeometry(t *testing.T) {
	hfStyle := func(props string) string {
		return `<style:header-style><style:header-footer-properties ` + props + `/></style:header-style>`
	}
	doc, _ := read(t, flat{
		styles: `<style:default-style style:family="table-cell"><style:text-properties style:font-name="Carlito" fo:font-size="11pt"/></style:default-style>`,
		auto: `<style:page-layout style:name="dyn"><style:page-layout-properties fo:margin-top="0.5in"/>` + hfStyle(`fo:min-height="0.2in" fo:margin-bottom="0.1in"`) + `</style:page-layout>` +
			`<style:page-layout style:name="fix"><style:page-layout-properties fo:margin-top="0.5in"/>` + hfStyle(`svg:height="0.2in" fo:margin-bottom="0.1in"`) + `</style:page-layout>` +
			`<style:style style:name="ta1" style:family="table" style:master-page-name="Dyn"/><style:style style:name="ta2" style:family="table" style:master-page-name="Fix"/>`,
		master: `<style:master-page style:name="Dyn" style:page-layout-name="dyn"><style:header><text:p>H</text:p></style:header></style:master-page>` +
			`<style:master-page style:name="Fix" style:page-layout-name="fix"><style:header><text:p>H</text:p></style:header></style:master-page>`,
		body: `<table:table table:name="A" table:style-name="ta1"><table:table-row><table:table-cell office:value-type="string"><text:p>a</text:p></table:table-cell></table:table-row></table:table>` +
			`<table:table table:name="B" table:style-name="ta2"><table:table-row><table:table-cell office:value-type="string"><text:p>b</text:p></table:table-cell></table:table-row></table:table>`,
	}.bytes())
	if len(doc.Sheets) != 2 {
		t.Fatalf("%d sheets, want 2", len(doc.Sheets))
	}
	for i, want := range []bool{true, false} {
		sh := doc.Sheets[i]
		hf := sh.HeaderFooter
		if hf.GrowHeader != want || !near(hf.HeaderSpacing, 7.2) || !near(hf.HeaderMinHeight, 7.2) || !near(sh.Page.Margin[css.Top], 36+14.4) {
			t.Errorf("%s: grow %v spacing %v min %v top %v, want grow %v, 7.2, 7.2, 50.4", sh.Name, hf.GrowHeader, hf.HeaderSpacing, hf.HeaderMinHeight, sh.Page.Margin[css.Top], want)
		}
		if len(hf.Font.Families) == 0 || hf.Font.Families[0] != "Carlito" || hf.Font.Size != 11 {
			t.Errorf("%s: header font %+v, want Carlito 11", sh.Name, hf.Font)
		}
	}
}

// TestRead_ChartPastACapStops checks that a chart part past the XML depth
// cap ends the reading with limits.ErrInputTooLarge, where a chart part
// that does not parse is left out with a warning.
func TestRead_ChartPastACapStops(t *testing.T) {
	manifest := `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0">` +
		`<manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.spreadsheet"/>` +
		`<manifest:file-entry manifest:full-path="Object 1/" manifest:media-type="application/vnd.oasis.opendocument.chart"/></manifest:manifest>`
	content := `<office:document-content ` + nsDecl + `><office:body><office:spreadsheet><table:table table:name="S">` +
		`<table:table-row><table:table-cell><draw:frame svg:width="2in" svg:height="1in"><draw:object xlink:href="./Object 1"/></draw:frame>` +
		`</table:table-cell></table:table-row></table:table></office:spreadsheet></office:body></office:document-content>`
	pkg := func(chart string) []byte {
		return zipOf(t, "mimetype", "application/vnd.oasis.opendocument.spreadsheet", "META-INF/manifest.xml", manifest,
			"content.xml", content, "Object 1/content.xml", chart)
	}
	deep := `<office:document-content ` + nsDecl + `>` + strings.Repeat("<office:body>", 300) + strings.Repeat("</office:body>", 300) + `</office:document-content>`
	if _, _, err := Read(context.Background(), pkg(deep), nil, office.Options{}); !errors.Is(err, limits.ErrInputTooLarge) {
		t.Errorf("a chart past the depth cap: %v, want limits.ErrInputTooLarge", err)
	}
	_, w := read(t, pkg(`<office:document-content `+nsDecl+`><office:body>`))
	if !hasWarning(w, warn.PartMalformed) {
		t.Errorf("a chart that does not parse: warnings %+v, want chart-malformed", w)
	}
}

// TestUnstyledRowHeight checks that a row whose style sets no height
// takes Calc's standard height, not the height of the rows around it.
//
// # Corpus evidence
//
//   - probe, ods: through gotenberg/gotenberg:9.4.1, rows without a style
//     before and after rows of 30 mm print 12.78 points apart.
//   - libreoffice/ods: sc/tdf121040.ods prints its rows, whose style says
//     style:use-optimal-row-height false and sets no height, as tall as
//     their content.
func TestUnstyledRowHeight(t *testing.T) {
	cell := `<table:table-cell office:value-type="string"><text:p>x</text:p></table:table-cell>`
	f := flat{
		auto: `<style:style style:name="ro" style:family="table-row"><style:table-row-properties style:row-height="30mm" style:use-optimal-row-height="false"/></style:style>` +
			`<style:style style:name="rn" style:family="table-row"><style:table-row-properties style:use-optimal-row-height="false"/></style:style>`,
		body: table("S", `<table:table-row>`+cell+`</table:table-row><table:table-row table:style-name="ro">`+cell+`</table:table-row>`+
			`<table:table-row>`+cell+`</table:table-row><table:table-row table:style-name="rn">`+cell+`</table:table-row>`),
	}
	doc, _ := read(t, f.bytes())
	rows := doc.Sheets[0].Rows
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	for i, want := range []struct {
		h      float64
		custom bool
	}{{defaultRowHeight, false}, {calcTwips(30 * 72 / 25.4), true}, {defaultRowHeight, false}, {defaultRowHeight, false}} {
		if rows[i].Height != want.h || rows[i].CustomHeight != want.custom {
			t.Errorf("row %d height = %v custom %v, want %v custom %v", i, rows[i].Height, rows[i].CustomHeight, want.h, want.custom)
		}
	}
}

// TestRead_RowsCalcMeasures locks which rows Calc measures again as it
// loads a spreadsheet: a stated height without style:use-optimal-row-height
// is the user's, and a row of optimal height past row 200 keeps the height
// its style states.
func TestRead_RowsCalcMeasures(t *testing.T) {
	row := func(style string) string {
		return `<table:table-row table:style-name="` + style + `"><table:table-cell office:value-type="string"><text:p>a</text:p></table:table-cell></table:table-row>`
	}
	f := flat{
		auto: `<style:style style:name="opt" style:family="table-row"><style:table-row-properties style:row-height="3mm" style:use-optimal-row-height="true"/></style:style>` +
			`<style:style style:name="set" style:family="table-row"><style:table-row-properties style:row-height="3mm"/></style:style>`,
		body: table("S", row("set")+strings.Repeat(row("opt"), 201)),
	}
	doc, _ := read(t, f.bytes())
	custom := map[int]bool{}
	for _, r := range doc.Sheets[0].Rows {
		custom[r.Index] = r.CustomHeight
	}
	for ri, want := range map[int]bool{0: true, 1: false, 200: false, 201: true} {
		if custom[ri] != want {
			t.Errorf("row %d custom %v, want %v", ri, custom[ri], want)
		}
	}
}

// TestRead_NamedStyleOverColumn locks an empty cell that names the
// Default style under a column whose default cell style draws a border:
// it is listed in the Default style, where the border does not reach,
// and a cell that names no style is left to the column's.
func TestRead_NamedStyleOverColumn(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="ce1" style:family="table-cell" style:parent-style-name="Default"><style:table-cell-properties fo:border="0.06pt solid #000000"/></style:style>` +
			`<style:style style:name="ce2" style:family="table-cell" style:parent-style-name="Default"><style:text-properties fo:font-size="18pt"/></style:style>`,
		body: table("S", `<table:table-column table:default-cell-style-name="ce1" table:number-columns-repeated="2"/>`+
			`<table:table-row><table:table-cell table:style-name="Default"/><table:table-cell/><table:table-cell table:style-name="ce2"/></table:table-row>`),
	}
	doc, _ := read(t, f.bytes())
	sh := doc.Sheets[0]
	if c := cellAt(sh, 0, 0); c == nil || c.Style != &doc.Default {
		t.Errorf("cell naming Default = %+v, want listed in the Default style", c)
	}
	if c := cellAt(sh, 0, 1); c != nil {
		t.Errorf("cell naming no style = %+v, want left to its column", c)
	}
	if c := cellAt(sh, 0, 2); c == nil || c.Style == nil || c.Style.Font.Size != 18 {
		t.Errorf("empty cell of 18 points = %+v, want listed", c)
	}
}
