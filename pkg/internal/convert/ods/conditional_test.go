package ods

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestRead_ConditionalFormats locks the reading of
// calcext:conditional-formats: the target ranges, each condition's kind,
// operator and operands (a cell of the table read as its value), the
// named style as the fields it sets past Default, the colour scales and
// data bars with their thresholds resolved, and the rules kept with their
// formula.
func TestRead_ConditionalFormats(t *testing.T) {
	f := flat{
		styles: `<style:style style:name="Default" style:family="table-cell"><style:text-properties fo:font-size="10pt" fo:color="#000000"/></style:style>` +
			`<style:style style:name="Status" style:family="table-cell" style:parent-style-name="Default"><style:text-properties fo:font-weight="bold"/></style:style>` +
			`<style:style style:name="Good" style:family="table-cell" style:parent-style-name="Status"><style:table-cell-properties fo:background-color="#ccffcc"/><style:text-properties fo:color="#006600"/></style:style>`,
		body: `<table:table table:name="S">` +
			`<table:table-row><table:table-cell office:value-type="float" office:value="1"/><table:table-cell office:value-type="string"><text:p>ab</text:p></table:table-cell></table:table-row>` +
			`<table:table-row><table:table-cell office:value-type="float" office:value="2"/></table:table-row>` +
			`<table:table-row><table:table-cell office:value-type="float" office:value="3"/></table:table-row>` +
			`<table:table-row><table:table-cell office:value-type="float" office:value="4"/></table:table-row>` +
			`<calcext:conditional-formats>` +
			`<calcext:conditional-format calcext:target-range-address="S.A1:S.A4 S.C1">` +
			`<calcext:condition calcext:apply-style-name="Good" calcext:value="&lt;=2" calcext:base-cell-address="S.A1"/>` +
			`<calcext:condition calcext:value="not-between(1,&quot;x&quot;&quot;y&quot;)" calcext:base-cell-address="S.A1"/>` +
			`<calcext:condition calcext:value="begins-with([.$B$1])" calcext:base-cell-address="S.A1"/>` +
			`<calcext:condition calcext:value="!=[.A2]+1" calcext:base-cell-address="S.A1"/>` +
			`<calcext:condition calcext:value="top-percent(50)" calcext:base-cell-address="S.A1"/>` +
			`<calcext:condition calcext:value="bottom-elements(3)" calcext:base-cell-address="S.A1"/>` +
			`<calcext:condition calcext:value="above-equal-average(" calcext:base-cell-address="S.A1"/>` +
			`<calcext:condition calcext:value="unique" calcext:base-cell-address="S.A1"/>` +
			`<calcext:condition calcext:value="formula-is(ISEVEN(ROW()))" calcext:base-cell-address="S.A1"/>` +
			`</calcext:conditional-format>` +
			`<calcext:conditional-format calcext:target-range-address="S.A1:S.A4">` +
			`<calcext:color-scale><calcext:color-scale-entry calcext:value="50" calcext:type="percentile" calcext:color="#ff0000"/><calcext:color-scale-entry calcext:value="[.A2]" calcext:type="number" calcext:color="#0000ff"/></calcext:color-scale>` +
			`<calcext:data-bar calcext:positive-color="#00ff00"><calcext:formatting-entry calcext:value="0" calcext:type="auto-minimum"/><calcext:formatting-entry calcext:value="0" calcext:type="maximum"/></calcext:data-bar>` +
			`<calcext:color-scale><calcext:color-scale-entry calcext:value="0" calcext:type="minimum" calcext:color="#ff0000"/><calcext:color-scale-entry calcext:value="=2*[.A1]" calcext:type="formula" calcext:color="#0000ff"/></calcext:color-scale>` +
			`<calcext:icon-set calcext:icon-set-type="3Arrows"/>` +
			`</calcext:conditional-format>` +
			`<calcext:conditional-format calcext:target-range-address="S.A3:S.A3"><calcext:condition calcext:value="=[$S.$A5]" calcext:base-cell-address="S.A4"/></calcext:conditional-format>` +
			`</calcext:conditional-formats></table:table>`,
	}
	doc, _ := read(t, f.bytes())
	cf := doc.Sheets[0].Conditional
	if len(cf) != 3 {
		t.Fatalf("%d conditional formats, want 3", len(cf))
	}
	if r := cf[2].Rules[0]; r.Formula != "" || len(r.Values) != 1 || r.Values[0] != (sheet.Value{Kind: sheet.Number, Number: 4}) {
		t.Errorf("a relative reference from the base cell %+v", r)
	}
	if r := cf[0].Ranges; len(r) != 2 || r[0] != (sheet.Range{LastRow: 3}) || r[1] != (sheet.Range{FirstColumn: 2, LastColumn: 2}) {
		t.Errorf("ranges %+v", r)
	}
	num := func(v float64) sheet.Value { return sheet.Value{Kind: sheet.Number, Number: v} }
	text := func(s string) sheet.Value { return sheet.Value{Kind: sheet.Text, Text: s} }
	want := []struct {
		kind    sheet.RuleKind
		op      string
		values  []sheet.Value
		formula bool
	}{
		{sheet.RuleCellIs, "lessThanOrEqual", []sheet.Value{num(2)}, false},
		{sheet.RuleCellIs, "notBetween", []sheet.Value{num(1), text(`x"y`)}, false},
		{sheet.RuleText, "beginsWith", []sheet.Value{text("ab")}, false},
		{sheet.RuleCellIs, "notEqual", nil, true},
		{sheet.RuleTop, "", []sheet.Value{num(2)}, false},
		{sheet.RuleBottom, "", []sheet.Value{num(3)}, false},
		{sheet.RuleAboveAverage, "", nil, false},
		{sheet.RuleUniques, "", nil, false},
		{sheet.RuleExpression, "", nil, true},
	}
	rules := cf[0].Rules
	if len(rules) != len(want) {
		t.Fatalf("%d rules, want %d: %+v", len(rules), len(want), rules)
	}
	for i, w := range want {
		r := rules[i]
		if r.Kind != w.kind || r.Operator != w.op || (r.Formula != "") != w.formula {
			t.Errorf("rule %d: %+v", i, r)
		}
		if w.values != nil && (len(r.Values) != len(w.values) || r.Values[0] != w.values[0] || r.Values[len(r.Values)-1] != w.values[len(w.values)-1]) {
			t.Errorf("rule %d values %+v, want %+v", i, r.Values, w.values)
		}
	}
	good := rules[0].Style
	if good == nil || !good.Font.Bold || good.Font.Color != (css.Color{G: 0x66 / 255.0, A: 1}) || good.Fill.Color != (css.Color{R: 0xcc / 255.0, G: 1, B: 0xcc / 255.0, A: 1}) || good.Font.Size != 0 {
		t.Errorf("Good %+v", good)
	}
	if rules[1].Style != nil {
		t.Errorf("a condition without a style has %+v", rules[1].Style)
	}
	scales := cf[1].Rules
	if len(scales) != 4 {
		t.Fatalf("%d scale rules, want 4", len(scales))
	}
	if s := scales[0]; s.Kind != sheet.RuleColorScale || len(s.Values) != 2 || s.Values[0] != num(2.5) || s.Values[1] != num(2) || len(s.Scale) != 2 {
		t.Errorf("colour scale %+v", s)
	}
	if b := scales[1]; b.Kind != sheet.RuleDataBar || len(b.Values) != 2 || b.Values[0] != num(0) || b.Values[1] != num(4) || b.Bar != (css.Color{G: 1, A: 1}) {
		t.Errorf("data bar %+v", b)
	}
	if s := scales[2]; s.Kind != sheet.RuleExpression {
		t.Errorf("a formula threshold %+v", s)
	}
	if s := scales[3]; s.Kind != sheet.RuleIconSet {
		t.Errorf("icon set %+v", s)
	}
}
