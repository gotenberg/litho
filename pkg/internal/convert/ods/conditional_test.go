package ods

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
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
			`<calcext:data-bar calcext:positive-color="#00ff00" calcext:axis-position="middle"><calcext:formatting-entry calcext:value="0" calcext:type="auto-minimum"/><calcext:formatting-entry calcext:value="0" calcext:type="maximum"/></calcext:data-bar>` +
			`<calcext:color-scale><calcext:color-scale-entry calcext:value="0" calcext:type="minimum" calcext:color="#ff0000"/><calcext:color-scale-entry calcext:value="=2*[.A1]" calcext:type="formula" calcext:color="#0000ff"/></calcext:color-scale>` +
			`<calcext:icon-set calcext:icon-set-type="3Flags" calcext:show-value="false"><calcext:formatting-entry calcext:value="0" calcext:type="percent"/>` +
			`<calcext:formatting-entry calcext:value="50" calcext:type="percent" calcext:greater-equal="false"/><calcext:formatting-entry calcext:value="3" calcext:type="number"/></calcext:icon-set>` +
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
	if b := scales[1]; b.Kind != sheet.RuleDataBar || len(b.Values) != 2 || b.Values[0] != num(0) || b.Values[1] != num(4) || b.Bar != (css.Color{G: 1, A: 1}) ||
		b.BarAxis != sheet.BarAxisMiddle || b.BarMinLength != 0 || b.BarMaxLength != 1 || b.BarSolid {
		t.Errorf("data bar %+v", b)
	}
	if s := scales[2]; s.Kind != sheet.RuleColorScale || len(s.Values) != 2 || s.Values[1] != (sheet.Value{Kind: sheet.Text, Text: "=2*[.A1]"}) {
		t.Errorf("a formula threshold %+v", s)
	}
	if s := scales[3]; s.Kind != sheet.RuleIconSet || s.Icons != "3Flags" || !s.IconsOnly || len(s.Values) != 3 ||
		len(s.IconsAbove) != 3 || s.IconsAbove[0] || !s.IconsAbove[1] || s.Values[2] != num(3) {
		t.Errorf("icon set %+v", s)
	}
}

// TestEncodeStyleName locks LibreOffice's encoding of a display name.
func TestEncodeStyleName(t *testing.T) {
	for in, want := range map[string]string{"ConditionalStyle_1": "ConditionalStyle_5f_1", "Good": "Good", "My style": "My_20_style", "1st": "_31_st", "a-b.c": "a-b.c"} {
		if got := encodeStyleName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// TestRead_StyleMaps locks the style:map conditions of a table without
// calcext conditional formats: one format per mapped cell style over its
// column and its cells, the comparisons read, the others kept with their
// formula; a table with calcext formats ignores the maps.
func TestRead_StyleMaps(t *testing.T) {
	styles := `<style:style style:name="grey" style:family="table-cell" style:parent-style-name="Default"><style:table-cell-properties fo:background-color="#999999"/></style:style>`
	auto := `<style:style style:name="ce1" style:family="table-cell" style:parent-style-name="Default">` +
		`<style:map style:condition="cell-content()=1" style:apply-style-name="grey" style:base-cell-address="S.A1"/>` +
		`<style:map style:condition="cell-content-is-between(2,&quot;b&quot;)" style:apply-style-name="grey" style:base-cell-address="S.A1"/>` +
		`<style:map style:condition="cell-content()!=x" style:apply-style-name="grey" style:base-cell-address="S.A1"/></style:style>`
	body := func(extra string) string {
		return `<table:table table:name="S"><table:table-column table:default-cell-style-name="ce1"/><table:table-column/>` +
			`<table:table-row><table:table-cell office:value-type="float" office:value="1"/><table:table-cell table:style-name="ce1" office:value-type="float" office:value="1"/></table:table-row>` +
			extra + `</table:table>`
	}
	doc, _ := read(t, flat{styles: styles, auto: auto, body: body("")}.bytes())
	cf := doc.Sheets[0].Conditional
	if len(cf) != 1 || len(cf[0].Ranges) != 2 || cf[0].Ranges[0] != (sheet.Range{LastRow: 1<<20 - 1}) || cf[0].Ranges[1] != (sheet.Range{FirstColumn: 1, LastColumn: 1}) {
		t.Fatalf("conditional formats %+v", cf)
	}
	rules := cf[0].Rules
	if len(rules) != 3 || rules[0].Operator != "equal" || rules[0].Values[0].Number != 1 || rules[1].Operator != "between" || rules[1].Values[1].Text != "b" ||
		rules[2].Formula != "x" || rules[0].Style == nil || rules[0].Style.Fill.Color.A == 0 {
		t.Errorf("rules %+v", rules)
	}
	doc, _ = read(t, flat{styles: styles, auto: auto, body: body(`<calcext:conditional-formats/>`)}.bytes())
	if cf := doc.Sheets[0].Conditional; len(cf) != 0 {
		t.Errorf("a calcext table reads its maps: %+v", cf)
	}
}

// TestConditionalFormats_OperandsOfALongSheet locks that a cell operand
// is found without walking the sheet's rows: every operand of the most
// conditions a table reads, over a sheet of many rows, resolves within
// a few seconds, where a walk per operand takes minutes.
func TestConditionalFormats_OperandsOfALongSheet(t *testing.T) {
	sh := &sheet.Sheet{Name: "S", Rows: make([]sheet.Row, 500_000)}
	for i := range sh.Rows {
		sh.Rows[i].Index = i + 1
	}
	sh.Rows[len(sh.Rows)-1].Cells = []sheet.Cell{{Column: 1, Value: sheet.Value{Kind: sheet.Number, Number: 7}}}
	var b strings.Builder
	b.WriteString(`<calcext:conditional-formats ` + nsDecl + `>`)
	for range maxCondFormats {
		b.WriteString(`<calcext:conditional-format calcext:target-range-address="S.A1:S.A2">`)
		for range maxCondRules {
			// A1 is listed in no row, B500001 in the last one.
			b.WriteString(`<calcext:condition calcext:value="between([.$A$1],[.$B$500001])"/>`)
		}
		b.WriteString(`</calcext:conditional-format>`)
	}
	b.WriteString(`</calcext:conditional-formats>`)
	el, err := odf.ParseXML(context.Background(), []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&reader{}).conditionalFormats(sh, el)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the operands are not resolved within 20 s")
	}
	if n := len(sh.Conditional); n != maxCondFormats {
		t.Fatalf("%d conditional formats, want %d", n, maxCondFormats)
	}
	want := []sheet.Value{{Kind: sheet.Number}, {Kind: sheet.Number, Number: 7}}
	for _, r := range sh.Conditional[maxCondFormats-1].Rules {
		if len(r.Values) != 2 || r.Values[0] != want[0] || r.Values[1] != want[1] {
			t.Fatalf("operands %+v, want %+v", r.Values, want)
		}
	}
}
