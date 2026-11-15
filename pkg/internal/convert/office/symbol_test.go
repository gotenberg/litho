package office

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/symbolfont"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func TestSymbolText(t *testing.T) {
	for _, tc := range []struct{ text, family, want string }{
		{"", "Symbol", "•"},
		{"a\tb", "Symbol", "α\tβ"},
		{"", "Arial", ""},
		{"x", "OpenSymbol", ""},
		{"\t", "Wingdings", ""},
		{"\uf0fc\uf06e", "Wingdings", "✓◼"},
	} {
		if got := SymbolText(tc.text, tc.family); got != tc.want {
			t.Errorf("SymbolText(%q, %q) = %q, want %q", tc.text, tc.family, got, tc.want)
		}
	}
}

// symbolClone indexes a "Standard Symbols PS" face, the metric clone of
// Symbol, covering U+0020 and U+0061 alone, as the URW face places its
// alpha at the Symbol code 0x61, and an "OpenSymbol" face covering U+0020
// and U+03B1, where LibreOffice's table recodes the code 0x61 of Symbol.
func symbolClone(t *testing.T) *text.Fonts {
	t.Helper()
	dir := t.TempDir()
	for name, face := range map[string]fontbuild.TrueType{
		"symbols.ttf": {
			PostScriptName: "StandardSymbolsPS", Family: "Standard Symbols PS", Subfamily: "Regular", NotdefAdvance: 500,
			Glyphs: []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 250, Empty: true}, {Name: "alpha", Unicode: 'a', Advance: 631}},
		},
		"opensymbol.ttf": {
			PostScriptName: "OpenSymbol", Family: "OpenSymbol", Subfamily: "Regular", NotdefAdvance: 500,
			Glyphs: []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 250, Empty: true}, {Name: "alpha", Unicode: '\u03b1', Advance: 600}},
		},
	} {
		if err := os.WriteFile(filepath.Join(dir, name), face.Build(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := text.NewFonts(context.Background(), nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestSymbolNode_DrawsNoMissingGlyph locks that a run of a symbol font the
// index lacks draws a glyph: Word's rule draws the codes in the clone,
// LibreOffice's recodes them into OpenSymbol, and neither leaves the
// private-use codes to the clone, which maps none of them.
func TestSymbolNode_DrawsNoMissingGlyph(t *testing.T) {
	t.Parallel()
	f := symbolClone(t)
	st := &css.Style{FontFamilies: []string{"Symbol", "serif"}}
	for _, tc := range []struct {
		rule       symbolfont.Rule
		text, fam0 string
	}{
		{symbolfont.RuleWord, "a", "Symbol"},
		{symbolfont.RuleLibreOffice, "α", "OpenSymbol"},
	} {
		tn := &layout.Node{Kind: layout.Text, Style: st, Text: ""}
		SymbolNode(tn, f, tc.rule, NewSymbolCodes())
		if tn.Text != tc.text || tn.Style.FontFamilies[0] != tc.fam0 || tn.ActualText != "α" {
			t.Errorf("rule %d: %q in %v, ActualText %q; want %q in %s, ActualText α", tc.rule, tn.Text, tn.Style.FontFamilies, tn.ActualText, tc.text, tc.fam0)
		}
		if !slices.Equal(st.FontFamilies, []string{"Symbol", "serif"}) {
			t.Fatalf("rule %d: the shared style changed to %v", tc.rule, st.FontFamilies)
		}
	}
}

// TestSymbolCodes_RestoreGivesTheGlyphsTheirCodes locks that a glyph drawn
// in place of a symbol code stands for the code in the extracted text, as
// Word and LibreOffice write it, and that a run of another ActualText
// keeps its text.
func TestSymbolCodes_RestoreGivesTheGlyphsTheirCodes(t *testing.T) {
	t.Parallel()
	f := symbolClone(t)
	codes := NewSymbolCodes()
	tn := &layout.Node{Kind: layout.Text, Style: &css.Style{FontFamilies: []string{"Symbol"}}, Text: ""}
	SymbolNode(tn, f, symbolfont.RuleWord, codes)
	symbol := &typeset.Text{Parent: &typeset.StructElem{ActualText: tn.ActualText}, Glyphs: []typeset.Glyph{{Text: tn.Text}}}
	other := &typeset.Text{Parent: &typeset.StructElem{ActualText: "x"}, Glyphs: []typeset.Glyph{{Text: "a"}}}
	codes.Restore(&typeset.Page{Content: []typeset.Fragment{&typeset.Group{Content: []typeset.Fragment{symbol}}, other}})
	if got := symbol.Glyphs[0].Text; got != "" {
		t.Errorf("symbol glyph stands for %q, want U+F061", got)
	}
	if got := other.Glyphs[0].Text; got != "a" {
		t.Errorf("other glyph stands for %q, want a", got)
	}
}
