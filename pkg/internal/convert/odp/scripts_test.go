package odp

import (
	"testing"
)

// TestScriptRuns checks that a paragraph's runs are cut by script, each
// part in its script's face and size, Impress's 18 points for the
// scripts a style leaves.
//
// # Corpus evidence
//
//   - probe, odp: through gotenberg/gotenberg:9.4.1, "abc 漢字 عربي" in a
//     paragraph style setting fo:font-size 20pt alone prints at 20, 18
//     and 18 points; with style:font-size-asian 24pt and
//     style:font-size-complex 16pt alone, at 18, 24 and 16.
func TestScriptRuns(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="P" style:family="paragraph"><style:text-properties fo:font-size="20pt" ` +
			`style:font-family-asian="'Noto Sans CJK JP'" style:font-size-complex="16pt" style:font-weight-complex="bold"/></style:style>`,
		body: page(`<draw:frame svg:width="10cm" svg:height="2cm"><draw:text-box><text:p text:style-name="P">abc 漢字 عربي</text:p></draw:text-box></draw:frame>`),
	}
	got := firstShape(t, f).Text.Paragraphs[0].Runs
	want := []struct {
		text string
		size float64
		lead string
		bold bool
	}{
		{"abc ", 20, "", false},
		{"漢字 ", 18, "Noto Sans CJK JP", false},
		{"عربي", 16, "", true},
	}
	if len(got) != len(want) {
		t.Fatalf("runs = %+v, want %d runs", got, len(want))
	}
	for i, w := range want {
		r := got[i]
		if r.Text != w.text || r.Style.Size != w.size || r.Style.Bold != w.bold {
			t.Errorf("run %d = %q at %v bold %v, want %q at %v bold %v", i, r.Text, r.Style.Size, r.Style.Bold, w.text, w.size, w.bold)
		}
		if w.lead != "" && (len(r.Style.Families) == 0 || r.Style.Families[0] != w.lead) {
			t.Errorf("run %d families = %v, want %q first", i, r.Style.Families, w.lead)
		}
	}
}
