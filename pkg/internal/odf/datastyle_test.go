package odf

import (
	"context"
	"testing"
)

// TestDataStyles_Code checks the translation against the codes
// LibreOffice's XLSX export writes for the same data styles.
//
// # Corpus evidence
//
//   - litho: a flat document with these data styles, exported to XLSX by
//     the image's soffice, carries the numFmts #,##0.00; 0.0%;
//     #,##0.00\ [$€-407];[RED]\-#,##0.00\ [$€-407]; dd\.mm\.yy;
//     [hh]:mm:ss.00; h:mm\ AM/PM; 0.000E+00; # ??/??; "TRUE";"TRUE";"FALSE";
//     "Note: "@; 0,\K; #.0##; 0 ?/8; mmm\-yy; 000\-0000.00; General.
func TestDataStyles_Code(t *testing.T) {
	tests := []struct {
		name, xml, want string
	}{
		{"N0", `<number:number-style style:name="N0"><number:number number:min-integer-digits="1"/></number:number-style>`, "General"},
		{"N1", `<number:number-style style:name="N1"><number:number number:decimal-places="2" number:min-decimal-places="2" number:min-integer-digits="1" number:grouping="true"/></number:number-style>`, "#,##0.00"},
		{"N2", `<number:percentage-style style:name="N2"><number:number number:decimal-places="1" number:min-decimal-places="1" number:min-integer-digits="1"/><number:text>%</number:text></number:percentage-style>`, "0.0%"},
		{
			"N3", `<number:currency-style style:name="N3"><style:text-properties fo:color="#ff0000"/><number:text>-</number:text><number:number number:decimal-places="2" number:min-decimal-places="2" number:min-integer-digits="1" number:grouping="true"/><number:text> </number:text><number:currency-symbol number:language="de" number:country="DE">€</number:currency-symbol><style:map style:condition="value()&gt;=0" style:apply-style-name="N3P0"/></number:currency-style>`,
			`#,##0.00\ [$€-407];[RED]\-#,##0.00\ [$€-407]`,
		},
		{"N4", `<number:date-style style:name="N4"><number:day-of-week number:style="long"/><number:text>, </number:text><number:month number:style="long" number:textual="true"/><number:text> </number:text><number:day/><number:text>, </number:text><number:year number:style="long"/></number:date-style>`, `dddd", "mmmm\ d", "yyyy`},
		{"N5", `<number:date-style style:name="N5"><number:day number:style="long"/><number:text>.</number:text><number:month number:style="long"/><number:text>.</number:text><number:year/></number:date-style>`, `dd\.mm\.yy`},
		{"N6", `<number:time-style style:name="N6" number:truncate-on-overflow="false"><number:hours number:style="long"/><number:text>:</number:text><number:minutes number:style="long"/><number:text>:</number:text><number:seconds number:style="long" number:decimal-places="2"/></number:time-style>`, "[hh]:mm:ss.00"},
		{"N7", `<number:time-style style:name="N7"><number:hours/><number:text>:</number:text><number:minutes number:style="long"/><number:text> </number:text><number:am-pm/></number:time-style>`, `h:mm\ AM/PM`},
		{"N8", `<number:number-style style:name="N8"><number:scientific-number number:decimal-places="3" number:min-decimal-places="3" number:min-integer-digits="1" number:min-exponent-digits="2"/></number:number-style>`, "0.000E+00"},
		{"N9", `<number:number-style style:name="N9"><number:fraction number:min-integer-digits="0" number:min-numerator-digits="2" number:min-denominator-digits="2"/></number:number-style>`, "# ??/??"},
		{"N10", `<number:boolean-style style:name="N10"><number:boolean/></number:boolean-style>`, `"TRUE";"TRUE";"FALSE"`},
		{"N11", `<number:text-style style:name="N11"><number:text>Note: </number:text><number:text-content/></number:text-style>`, `"Note: "@`},
		{"N12", `<number:number-style style:name="N12"><number:number number:decimal-places="0" number:min-decimal-places="0" number:min-integer-digits="1" number:display-factor="1000"/><number:text>K</number:text></number:number-style>`, `0,\K`},
		{"N13", `<number:number-style style:name="N13"><number:number number:decimal-places="3" number:min-decimal-places="1" number:min-integer-digits="0"/></number:number-style>`, "#.0##"},
		{"N14", `<number:number-style style:name="N14"><number:fraction number:min-integer-digits="1" number:min-numerator-digits="1" number:denominator-value="8"/></number:number-style>`, "0 ?/8"},
		{"N15", `<number:date-style style:name="N15"><number:month number:textual="true"/><number:text>-</number:text><number:year/></number:date-style>`, `mmm\-yy`},
		{"N16", `<number:number-style style:name="N16"><number:number number:decimal-places="2" number:min-decimal-places="2" number:min-integer-digits="7"><number:embedded-text number:position="4">-</number:embedded-text></number:number></number:number-style>`, `000\-0000.00`},
		{"N17", `<number:number-style style:name="N17"><number:number number:decimal-places="0" number:min-integer-digits="1"/><style:map style:condition="value()&gt;0" style:apply-style-name="N3P0"/><style:map style:condition="value()&lt;0" style:apply-style-name="N1"/></number:number-style>`, `#,##0.00\ [$€-407];#,##0.00;0`},
		{"N18", `<number:number-style style:name="N18"><number:number number:decimal-places="0" number:min-integer-digits="1"/><style:map style:condition="value()=1" style:apply-style-name="N1"/></number:number-style>`, `[=1]#,##0.00;0`},
		{"N19", `<number:currency-style style:name="N19"><number:currency-symbol number:language="xx" number:country="YY">¤</number:currency-symbol><number:number number:decimal-places="0" number:min-integer-digits="1"/></number:currency-style>`, `[$¤]0`},
	}
	var styles string
	for _, tt := range tests {
		styles += tt.xml
	}
	styles += `<number:currency-style style:name="N3P0" style:volatile="true"><number:number number:decimal-places="2" number:min-decimal-places="2" number:min-integer-digits="1" number:grouping="true"/><number:text> </number:text><number:currency-symbol number:language="de" number:country="DE">€</number:currency-symbol></number:currency-style>`
	doc := `<office:document ` + ns + ` xmlns:number="urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0"><office:styles>` + styles + `</office:styles></office:document>`
	p, err := Open(context.Background(), []byte(doc), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ds := NewDataStyles(p)
	for _, tt := range tests {
		if got := ds.Code(ScopeContent, tt.name); got != tt.want {
			t.Errorf("%s = %s, want %s", tt.name, got, tt.want)
		}
	}
	if got := ds.Code(ScopeContent, "missing"); got != "" {
		t.Errorf("missing = %q", got)
	}
}
