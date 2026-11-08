package xlsx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestRead_ConditionalFormats locks the reading of conditionalFormatting
// and cfRule: the ranges, each rule kind with its operands, priority and
// differential style, the thresholds of colour scales and data bars
// resolved over the range, a data bar's Excel 2010 extension, and the
// rules kept with their formula.
func TestRead_ConditionalFormats(t *testing.T) {
	t.Parallel()
	styles := minimalStyles + `<dxfs>` +
		`<dxf><font><b/><color rgb="FFFF0000"/></font><fill><patternFill><bgColor rgb="FF92D050"/></patternFill></fill></dxf>` +
		`<dxf><numFmt numFmtId="200" formatCode="0.000"/><border><bottom style="thin"><color rgb="FF0000FF"/></bottom></border></dxf>` +
		`</dxfs>`
	ws := `<sheetData>` +
		`<row r="1"><c r="A1"><v>1</v></c></row><row r="2"><c r="A2"><v>2</v></c></row>` +
		`<row r="3"><c r="A3"><v>3</v></c></row><row r="4"><c r="A4"><v>-1</v></c></row></sheetData>` +
		`<conditionalFormatting sqref="A1:A4 C1"><cfRule type="cellIs" dxfId="0" priority="2" operator="between" stopIfTrue="1"><formula>1</formula><formula>"x""y"</formula></cfRule>` +
		`<cfRule type="cellIs" dxfId="1" priority="1" operator="equal"><formula>$B$1</formula></cfRule></conditionalFormatting>` +
		`<conditionalFormatting sqref="A1:A4"><cfRule type="containsText" priority="3" operator="containsText" text="ab"><formula>NOT(ISERROR(SEARCH("ab",A1)))</formula></cfRule>` +
		`<cfRule type="top10" priority="4" percent="1" rank="50"/><cfRule type="top10" priority="5" bottom="1" rank="2"/>` +
		`<cfRule type="aboveAverage" priority="6" aboveAverage="0"/><cfRule type="duplicateValues" priority="7"/><cfRule type="containsBlanks" priority="8"/>` +
		`<cfRule type="expression" priority="9"><formula>MOD(ROW(),2)=0</formula></cfRule><cfRule type="iconSet" priority="10"><iconSet><cfvo type="min"/></iconSet></cfRule>` +
		`<cfRule type="colorScale" priority="11"><colorScale><cfvo type="percentile" val="50"/><cfvo type="max"/><color rgb="FFFF0000"/><color rgb="FF0000FF"/></colorScale></cfRule>` +
		`<cfRule type="colorScale" priority="12"><colorScale><cfvo type="min"/><cfvo type="formula" val="$A$1*2"/><color rgb="FFFF0000"/><color rgb="FF0000FF"/></colorScale></cfRule>` +
		`<cfRule type="dataBar" priority="13"><dataBar><cfvo type="min"/><cfvo type="max"/><color rgb="FF638EC6"/></dataBar>` +
		`<extLst><ext uri="{B025F937-C7B1-47D3-B67F-A62EFF666E3E}" xmlns:x14="` + nsX14 + `"><x14:id>{1}</x14:id></ext></extLst></cfRule>` +
		`<cfRule type="dataBar" priority="14"><dataBar><cfvo type="percent" val="50"/><cfvo type="num" val="10"/><color rgb="FF00FF00"/></dataBar></cfRule>` +
		`</conditionalFormatting>` +
		`<extLst><ext uri="{78C0D931-6437-407d-A8EE-F0AAD7539E65}" xmlns:x14="` + nsX14 + `"><x14:conditionalFormattings><x14:conditionalFormatting xmlns:xm="` + nsXM + `">` +
		`<x14:cfRule type="dataBar" id="{1}"><x14:dataBar><x14:cfvo type="autoMin"/><x14:cfvo type="autoMax"/></x14:dataBar></x14:cfRule><xm:sqref>A1:A4</xm:sqref>` +
		`</x14:conditionalFormatting></x14:conditionalFormattings></ext></extLst>`
	doc, warnings := readBytes(t, zipFiles(t, workbookFiles([]testSheet{{name: "S", xml: ws}}, styles, "", "", nil)))
	for _, w := range warnings {
		if w.Code == "unsupported-content" {
			t.Errorf("warning %v", w)
		}
	}
	cf := doc.Sheets[0].Conditional
	if len(cf) != 2 {
		t.Fatalf("%d conditional formats, want 2", len(cf))
	}
	if r := cf[0].Ranges; len(r) != 2 || r[0] != (sheet.Range{LastRow: 3}) || r[1] != (sheet.Range{FirstColumn: 2, LastColumn: 2}) {
		t.Errorf("ranges %+v", r)
	}
	between, equal := cf[0].Rules[0], cf[0].Rules[1]
	if between.Kind != sheet.RuleCellIs || between.Operator != "between" || between.Priority != 2 || !between.StopIfTrue ||
		len(between.Values) != 2 || between.Values[0].Number != 1 || between.Values[1] != (sheet.Value{Kind: sheet.Text, Text: `x"y`}) {
		t.Errorf("between %+v", between)
	}
	if s := between.Style; s == nil || !s.Font.Bold || s.Font.Color != rgb(0xFF0000) || s.Fill.Color != rgb(0x92D050) || s.Format != "" {
		t.Errorf("first differential style %+v", between.Style)
	}
	if equal.Formula != "$B$1" || equal.Style == nil || equal.Style.Format != "0.000" || equal.Style.Border[css.Bottom].Color != rgb(0x0000FF) || equal.Style.Font.Bold {
		t.Errorf("equal %+v %+v", equal, equal.Style)
	}
	rules := cf[1].Rules
	want := []struct {
		kind   sheet.RuleKind
		op     string
		values []float64
	}{
		{sheet.RuleText, "containsText", nil},
		{sheet.RuleTop, "", []float64{2}},
		{sheet.RuleBottom, "", []float64{2}},
		{sheet.RuleBelowAverage, "", nil},
		{sheet.RuleDuplicates, "", nil},
		{sheet.RuleBlanks, "", nil},
		{sheet.RuleExpression, "", nil},
		{sheet.RuleIconSet, "", nil},
		{sheet.RuleColorScale, "", []float64{1.5, 3}},
		{sheet.RuleExpression, "", nil},
		{sheet.RuleDataBar, "", []float64{-1, 3}},
		{sheet.RuleDataBar, "", []float64{1, 10}},
	}
	if len(rules) != len(want) {
		t.Fatalf("%d rules, want %d", len(rules), len(want))
	}
	for i, w := range want {
		r := rules[i]
		if r.Kind != w.kind || r.Operator != w.op || r.Priority != i+3 {
			t.Errorf("rule %d: %+v, want kind %v %q", i, r, w.kind, w.op)
			continue
		}
		if w.values == nil {
			continue
		}
		if len(r.Values) != len(w.values) {
			t.Errorf("rule %d: values %+v, want %v", i, r.Values, w.values)
			continue
		}
		for k, v := range w.values {
			if r.Values[k].Kind != sheet.Number || r.Values[k].Number != v {
				t.Errorf("rule %d: values %+v, want %v", i, r.Values, w.values)
			}
		}
	}
	if rules[0].Values[0].Text != "ab" || rules[6].Formula != "MOD(ROW(),2)=0" {
		t.Errorf("text %+v, expression %+v", rules[0], rules[6])
	}
	if s := rules[8].Scale; len(s) != 2 || s[0] != rgb(0xFF0000) || s[1] != rgb(0x0000FF) {
		t.Errorf("scale %+v", s)
	}
	if rules[10].Bar != rgb(0x638EC6) {
		t.Errorf("bar %+v", rules[10].Bar)
	}
}
