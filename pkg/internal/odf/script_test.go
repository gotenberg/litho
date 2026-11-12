package odf

import (
	"strings"
	"testing"
)

// scriptString writes the script of each character as W, A or C.
func scriptString(s []Script, n int) string {
	if s == nil {
		return strings.Repeat("W", n)
	}
	var b strings.Builder
	for _, sc := range s {
		b.WriteByte("WAC"[sc])
	}
	return b.String()
}

// TestScripts locks the script LibreOffice gives each character of a
// paragraph.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, each paragraph
//     printed in Western, Asian and complex sizes of 10, 20 and 30 points
//     shows the scripts below, character by character; probe, ods and
//     probe, odp print the same paragraphs alike.
func TestScripts(t *testing.T) {
	for _, tc := range []struct {
		text string
		rtl  bool
		want string
	}{
		{"abc 123 def", false, "WWWWWWWWWWW"},
		{"漢字123漢字", false, "AAWWWAA"},
		{"漢字 abc 漢字", false, "AAAWWWWAA"},
		{"123 漢字", false, "WWWWAA"},
		{"(漢字)", false, "WAAW"},
		{"漢字。abc、def", false, "AAAWWWAWWW"},
		{"“漢字”", false, "AAAA"},
		{"★漢字", false, "AAA"},
		{" 漢字", false, "AAA"},
		{"€ 100", false, "WWWWW"},
		{"漢字—漢字…“漢字”", false, "AAAAAAAAAA"},
		{"漢字 ¹²³ abc", false, "AAAAAAAWWW"},
		{"漢字 · abc", false, "AAAWWWWW"},
		{"漢字-漢字", false, "AAWAA"},
		{"a\u3000b", false, "WAW"},
		{"\u00a0漢", false, "AA"},
		{"한국어 abc", false, "AAAAWWW"},
		{"１２３ ＡＢＣ", false, "AAAAAAA"},
		{"ไทย abc", false, "CCCCWWW"},
		{"हिन्दी 123", false, "CCCCCCCWWW"},
		{"عربي 123 abc", false, "CCCCCCCCWWWW"},
		{"abc عربي def", false, "WWWWCCCCCWWW"},
		{"שלום 123", false, "CCCCCCCC"},
		{"abc 123 שלום", false, "WWWWWWWWCCCC"},
		{"abc שלום 123 def", false, "WWWWCCCCCCCCWWWW"},
		{"abc (عربي) abc", false, "WWWWWCCCCWWWWW"},
		{"٣٤٥ abc", false, "CCCCWWW"},
		{"“漢字”", true, "CAAC"},
		{"★漢字", true, "CAA"},
		{"€ 100", true, "CCCCC"},
		{"1 2 3 漢字", true, "CCCCCCAA"},
		{"abc 123", true, "WWWWWWW"},
		{"שלום 。漢字", true, "CCCCCAAA"},
		{"שלום「漢字」", true, "CCCCAAAA"},
		{"שלום (abc) 123", true, "CCCCCCWWWCCCCC"},
	} {
		rs := []rune(tc.text)
		got, err := Scripts(t.Context(), rs, tc.rtl)
		if err != nil {
			t.Fatal(err)
		}
		if s := scriptString(got, len(rs)); s != tc.want {
			t.Errorf("Scripts(%q, rtl %v) = %s, want %s", tc.text, tc.rtl, s, tc.want)
		}
	}
}

// TestScriptsWesternIsNil checks that a left-to-right paragraph of
// Western characters alone needs no split.
func TestScriptsWesternIsNil(t *testing.T) {
	got, err := Scripts(t.Context(), []rune("Plain text, 1 2 3 € ★"), false)
	if err != nil || got != nil {
		t.Fatalf("Scripts = %v, %v, want nil, nil", got, err)
	}
}

// TestSplitScripts checks that runs are cut at each change of script,
// the context running across them.
func TestSplitScripts(t *testing.T) {
	got, err := SplitScripts(t.Context(), []string{"abc 漢", "字 1", "23"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]ScriptPart{
		{{"abc ", Western}, {"漢", Asian}},
		{{"字 ", Asian}, {"1", Western}},
		{{"23", Western}},
	}
	if len(got) != len(want) {
		t.Fatalf("SplitScripts = %v, want %v", got, want)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("run %d = %v, want %v", i, got[i], want[i])
		}
		for k := range want[i] {
			if got[i][k] != want[i][k] {
				t.Errorf("run %d part %d = %v, want %v", i, k, got[i][k], want[i][k])
			}
		}
	}
}

// TestScriptClassBlocksSorted checks that the ranges are in order and do
// not overlap, as the binary search needs.
func TestScriptClassBlocksSorted(t *testing.T) {
	for i := 1; i < len(classBlocks); i++ {
		if classBlocks[i].lo <= classBlocks[i-1].hi {
			t.Errorf("classBlocks[%d] starts at %X, inside or before %X", i, classBlocks[i].lo, classBlocks[i-1].hi)
		}
	}
}
