package ods

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// TestHyperlinkFormula locks the targets read from HYPERLINK formulas
// (see [hyperlinkFormula]).
func TestHyperlinkFormula(t *testing.T) {
	for _, tc := range []struct {
		f, want string
		ok      bool
	}{
		{`of:=HYPERLINK("http://www.example.com"; "Hyperlink")`, "http://www.example.com", true},
		{`of:=hyperlink("https://a.example/""q""")`, `https://a.example/"q"`, true},
		{`=HYPERLINK("#Sheet2.A1")`, "#Sheet2.A1", true},
		{`of:=HYPERLINK([.A1]; "x")`, "", false},
		{`of:=HYPERLINK("http://a" & "b")`, "", false},
		{`of:=SUM([.A1:.A2])`, "", false},
		{`of:=HYPERLINK("unterminated`, "", false},
	} {
		got, ok := hyperlinkFormula(tc.f)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: %q %v, want %q %v", tc.f, got, ok, tc.want, tc.ok)
		}
	}
}

// TestSoleHyperlinkArgument locks the HYPERLINK calls of one argument
// (see [soleHyperlinkArgument]).
func TestSoleHyperlinkArgument(t *testing.T) {
	for f, want := range map[string]bool{
		`of:=HYPERLINK([.O10])`:                true,
		`of:=HYPERLINK([.O10]&"x;y")`:          true,
		`of:=HYPERLINK([.O10]; "text")`:        false,
		`of:=HYPERLINK(IF([.A1];[.B1];[.C1]))`: true,
		`of:=SUM([.A1])`:                       false,
		`of:=HYPERLINK([.A1]`:                  false,
	} {
		if got := soleHyperlinkArgument(f); got != want {
			t.Errorf("%s: %v, want %v", f, got, want)
		}
	}
}

// TestRead_LinkTextColour locks the look of a link's text in a cell (see
// [linkColor]): #000080 and underlined, the text around it in the cell's
// own font.
func TestRead_LinkTextColour(t *testing.T) {
	auto := `<style:style style:name="T1" style:family="text"><style:text-properties fo:color="#00aa00"/></style:style>`
	cell := `<table:table-cell office:value-type="string"><text:p>before <text:a xlink:href="https://example.com/"><text:span text:style-name="T1">site</text:span></text:a></text:p></table:table-cell>`
	doc, _ := read(t, flat{auto: auto, body: table("S", `<table:table-row>`+cell+`</table:table-row>`)}.bytes())
	c := cellAt(doc.Sheets[0], 0, 0)
	if c == nil || len(c.Runs) != 2 {
		t.Fatalf("cell %+v, want two runs", c)
	}
	if f := c.Runs[0].Font; f != nil && f.Color == linkColor {
		t.Errorf("the text before the link in the link's colour")
	}
	if f := c.Runs[1].Font; f == nil || f.Color != linkColor || f.Underline != sheet.Single {
		t.Errorf("link run %+v, want #000080 underlined", f)
	}
}
