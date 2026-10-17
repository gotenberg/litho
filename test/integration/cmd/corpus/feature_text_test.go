package main

import (
	"testing"

	"golang.org/x/text/unicode/norm"
)

// TestVerticalFormsAgreeUnderNFKC pins why the content comparison needs no
// fold of its own: NFKC sends each vertical form PDF.js substitutes and the
// character it substitutes it for to the same string.
func TestVerticalFormsAgreeUnderNFKC(t *testing.T) {
	t.Parallel()
	for form, base := range verticalForms {
		if !comparePageTextContent(string(form), string(base)) {
			t.Errorf("NFKC(U+%04X) = %q, NFKC(U+%04X) = %q", form, norm.NFKC.String(string(form)), base, norm.NFKC.String(string(base)))
		}
	}
}

// TestComparePageTextFoldsVerticalForms pins that the word-level text
// comparison reads a vertical presentation form PDF.js substitutes as the
// character it stands for, on either side, and still reports a real
// difference next to one.
//
// # Corpus evidence
//
//   - pdfjs: bug1905623 sets 26 pages in an unembedded vertical font. PDF.js
//     reports U+FE12 for each U+3002 and U+FE11 for each U+3001 there, and
//     litho reports the characters the font maps the codes to.
func TestComparePageTextFoldsVerticalForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		golden string
		litho  string
		match  bool
	}{
		{"full stop and comma", "ものです︒ 取り扱い︑小説", "ものです。 取り扱い、小説", true},
		{"corner brackets and question mark", "﹁壮﹂︖", "「壮」？", true},
		{"ellipsis and dashes", "a︙b︱c︲d", "a…b—c–d", true},
		{"form on the litho side", "。", "︒", true},
		{"form on both sides", "︸x︷", "︸x︷", true},
		{"real difference beside a form", "︒あ", "。い", false},
		{"different base characters", "︒", "、", false},
		{"form without a pdf.js substitution", "﹅", "。", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mm := comparePageText("f.pdf", 1, c.golden, c.litho)
			if got := mm == nil; got != c.match {
				t.Errorf("comparePageText(%q, %q) match = %v, want %v (%+v)", c.golden, c.litho, got, c.match, mm)
			}
		})
	}
}
