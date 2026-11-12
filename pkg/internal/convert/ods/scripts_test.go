package ods

import (
	"testing"
)

// TestScriptRuns checks that a text cell is cut into runs by script, each
// part in its script's face and size, Calc's 10 points for the scripts a
// style leaves.
//
// # Corpus evidence
//
//   - probe, ods: through gotenberg/gotenberg:9.4.1, "abc 漢字 عربي" in a
//     cell style setting fo:font-size 20pt alone prints at 20, 10 and 10
//     points; with style:font-size-asian 24pt and style:font-size-complex
//     16pt alone, at 10, 24 and 16.
func TestScriptRuns(t *testing.T) {
	f := flat{
		styles: `<style:style style:name="C" style:family="table-cell"><style:text-properties fo:font-size="20pt" ` +
			`style:font-family-asian="'Noto Sans CJK JP'" style:font-size-complex="16pt" style:font-weight-complex="bold"/></style:style>`,
		body: table("S", `<table:table-row><table:table-cell table:style-name="C" office:value-type="string"><text:p>abc 漢字 عربي</text:p></table:table-cell></table:table-row>`),
	}
	doc, _ := read(t, f.bytes())
	c := cellAt(doc.Sheets[0], 0, 0)
	if c == nil {
		t.Fatal("no cell")
	}
	want := []struct {
		text string
		size float64
		lead string
		bold bool
	}{
		{"abc ", 20, "", false},
		{"漢字 ", 10, "Noto Sans CJK JP", false},
		{"عربي", 16, "", true},
	}
	if len(c.Runs) != len(want) {
		t.Fatalf("runs = %+v, want %d runs", c.Runs, len(want))
	}
	for i, w := range want {
		r := c.Runs[i]
		if r.Font == nil || r.Text != w.text || r.Font.Size != w.size || r.Font.Bold != w.bold {
			t.Errorf("run %d = %+v, want %q at %v bold %v", i, r, w.text, w.size, w.bold)
			continue
		}
		if w.lead != "" && (len(r.Font.Families) == 0 || r.Font.Families[0] != w.lead) {
			t.Errorf("run %d families = %v, want %q first", i, r.Font.Families, w.lead)
		}
	}
}
