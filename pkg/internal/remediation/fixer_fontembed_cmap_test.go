package remediation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/models"
)

// macintoshProgram returns prog with the cmap of a program made for the
// Macintosh, Apple's Times and Symbol among them: a (0,3) Unicode subtable
// holding what prog's (3,1) one maps, and a (1,0) one holding what the from
// subtable maps, and no Windows subtable.
func macintoshProgram(t *testing.T, prog []byte, from truetype.CmapSubtable) []byte {
	t.Helper()
	out, err := truetype.RewriteCmap(context.Background(), prog, []truetype.CmapAlias{
		{Target: truetype.CmapSubtable{PlatformID: 0, EncodingID: 3}, From: truetype.CmapSubtable{PlatformID: 3, EncodingID: 1}},
		{Target: truetype.CmapSubtable{PlatformID: 1, EncodingID: 0}, From: from},
	})
	if err != nil {
		t.Fatalf("RewriteCmap: %v", err)
	}
	return out
}

// symbolProgram returns a TrueType program drawing Alpha and Beta at the
// Symbol font's widths, its (3,0) subtable mapping codes 0x41 and 0x42 to
// Alpha and Beta, or to Beta and Alpha when swapped is set. A missing name
// leaves its glyph out.
func symbolProgram(swapped bool, missing string) []byte {
	alpha := fontbuild.Glyph{Name: "Alpha", Unicode: 0x391, Symbol: 0x41, Advance: 722}
	beta := fontbuild.Glyph{Name: "Beta", Unicode: 0x392, Symbol: 0x42, Advance: 667}
	if swapped {
		alpha.Symbol, beta.Symbol = beta.Symbol, alpha.Symbol
	}
	var glyphs []fontbuild.Glyph
	for _, g := range []fontbuild.Glyph{alpha, beta} {
		if g.Name != missing {
			glyphs = append(glyphs, g)
		}
	}
	return fontbuild.TrueType{PostScriptName: "TestSymbol", Family: "Test Symbol", Subfamily: "Regular", NotdefAdvance: 500, Glyphs: glyphs}.Build()
}

// TestFixFontEmbed_FitsTheCmapOfAMacintoshProgram pins the cmap a TrueType
// program made for the Macintosh receives: a non-symbolic font with
// /Differences needs a (3,1) subtable (ISO 19005-2, 6.2.11.6 t2), and a
// symbolic one a single subtable (ISO 19005-1, 6.3.7 t3), or a (3,0) one
// (ISO 19005-2, 6.2.11.6 t4). Both fonts were refused under those rules.
func TestFixFontEmbed_FitsTheCmapOfAMacintoshProgram(t *testing.T) {
	t.Parallel()
	const descriptor = " /FontDescriptor << /Type /FontDescriptor /FontName /%s /Flags %d /FontBBox [0 0 1000 800]" +
		" /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >> >>"
	data := unembeddedFontsPDF(
		"<< /Type /Font /Subtype /Type1 /BaseFont /MacSerif /FirstChar 65 /LastChar 66 /Widths [667 667]"+
			" /Encoding << /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [65 /B /A] >>"+
			strings.Replace(strings.Replace(descriptor, "%s", "MacSerif", 1), "%d", "32", 1),
		"<< /Type /Font /Subtype /TrueType /BaseFont /MacSymbol /FirstChar 65 /LastChar 66 /Widths [667 667]"+
			strings.Replace(strings.Replace(descriptor, "%s", "MacSymbol", 1), "%d", "4", 1),
	)
	unicode := macintoshProgram(t, sansProgram(0, 667, 667), truetype.CmapSubtable{PlatformID: 3, EncodingID: 1})
	provider := func(_ context.Context, _ FontRequest) ([]byte, error) { return unicode, nil }
	for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
		doc, alloc := newTestDoc(t, data)
		report, err := RemediatePDFA(context.Background(), doc, alloc, profile, Options{PreserveContent: true, Fonts: provider})
		if err != nil {
			t.Fatalf("%s: RemediatePDFA: %v", profile, err)
		}
		want := map[ast.PdfName][]truetype.CmapSubtable{
			"F1": {{PlatformID: 0, EncodingID: 3}, {PlatformID: 1, EncodingID: 0}, {PlatformID: 3, EncodingID: 1}},
			"F2": {{PlatformID: 1, EncodingID: 0}},
		}
		if profile == models.PDFA1b {
			// ISO 19005-1, 6.3.7 t1 admits no /Differences on a TrueType
			// font, whatever its program.
			if len(report.FontRefusals) != 1 || report.FontRefusals[0].BaseFont != "MacSerif" || !errors.Is(report.FontRefusals[0].Err, ErrFontRejected) {
				t.Fatalf("%s: refusals %v, want MacSerif's alone", profile, report.FontRefusals)
			}
			delete(want, "F1")
		} else if len(report.FontRefusals) != 0 {
			t.Fatalf("%s: refusals %v", profile, report.FontRefusals)
		}
		reread := rewriteAndReopen(t, doc, alloc)
		if findings := fontFindings(t, reread, string(profile)); len(findings) != 0 && profile != models.PDFA1b {
			t.Errorf("%s: output fails %v", profile, findings)
		}
		for name, records := range want {
			_, prog := fontProgram(t, reread, pageFont(t, reread, name))
			got, err := truetype.CmapSubtables(prog)
			if err != nil || !slices.Equal(got, records) {
				t.Errorf("%s: %s program cmap records %v (%v), want %v", profile, name, got, err, records)
			}
		}
	}
}

// TestFixFontEmbed_SymbolicType1OverTrueType pins a symbolic Type 1 font given
// a TrueType program: it becomes a symbolic TrueType font, with no /Encoding
// (ISO 19005-2, 6.2.11.6), over a program whose single (3,0) cmap subtable
// takes each code to the glyph of the name the Type 1 font gave it, whatever
// the program's own cmap maps it to, and is refused where the program has no
// glyph of a shown code's name.
func TestFixFontEmbed_SymbolicType1OverTrueType(t *testing.T) {
	t.Parallel()
	data := unembeddedFontsPDF("<< /Type /Font /Subtype /Type1 /BaseFont /Symbol >>")
	symbol := truetype.CmapSubtable{PlatformID: 3, EncodingID: 0}
	for _, tc := range []struct {
		name string
		prog []byte
		want error
	}{
		{"Windows symbol cmap", symbolProgram(false, ""), nil},
		{"Macintosh cmap", macintoshProgram(t, symbolProgram(false, ""), symbol), nil},
		{"codes the program maps elsewhere", symbolProgram(true, ""), nil},
		{"a name no glyph bears", symbolProgram(false, "Beta"), ErrFontCoverage},
	} {
		provider := func(_ context.Context, _ FontRequest) ([]byte, error) { return tc.prog, nil }
		for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
			doc, alloc := newTestDoc(t, data)
			report, err := RemediatePDFA(context.Background(), doc, alloc, profile, Options{PreserveContent: true, Fonts: provider})
			if err != nil {
				t.Fatalf("%s, %s: RemediatePDFA: %v", tc.name, profile, err)
			}
			if tc.want != nil {
				if len(report.FontRefusals) != 1 || !errors.Is(report.FontRefusals[0].Err, tc.want) {
					t.Errorf("%s, %s: refusals %v, want one wrapping %v", tc.name, profile, report.FontRefusals, tc.want)
				}
				continue
			}
			if len(report.FontRefusals) != 0 {
				t.Fatalf("%s, %s: refusals %v", tc.name, profile, report.FontRefusals)
			}
			reread := rewriteAndReopen(t, doc, alloc)
			if findings := fontFindings(t, reread, string(profile)); len(findings) != 0 {
				t.Errorf("%s, %s: output fails %v", tc.name, profile, findings)
			}
			font := pageFont(t, reread, "F1")
			if font["Subtype"] != ast.PdfName("TrueType") {
				t.Errorf("%s, %s: subtype %v, want TrueType", tc.name, profile, font["Subtype"])
			}
			if _, ok := font["Encoding"]; ok {
				t.Errorf("%s, %s: the symbolic TrueType font carries /Encoding %v", tc.name, profile, font["Encoding"])
			}
			_, prog := fontProgram(t, reread, font)
			if records, err := truetype.CmapSubtables(prog); err != nil || !slices.Equal(records, []truetype.CmapSubtable{symbol}) {
				t.Errorf("%s, %s: cmap records %v (%v), want (3,0) alone", tc.name, profile, records, err)
			}
			codes, err := truetype.SymbolCharCodeToGID(context.Background(), prog)
			if err != nil {
				t.Fatalf("%s, %s: SymbolCharCodeToGID: %v", tc.name, profile, err)
			}
			names, err := truetype.PostGlyphNames(context.Background(), prog)
			if err != nil {
				t.Fatalf("%s, %s: PostGlyphNames: %v", tc.name, profile, err)
			}
			if names[codes['A']] != "Alpha" || names[codes['B']] != "Beta" {
				t.Errorf("%s, %s: codes A and B draw %q and %q, want Alpha and Beta", tc.name, profile, names[codes['A']], names[codes['B']])
			}
			// The text of the codes stays what the Symbol encoding made it.
			resolved, err := typography.ResolveFont(context.Background(), reread, font)
			if err != nil || resolved.ToUnicode == nil {
				t.Fatalf("%s, %s: no /ToUnicode CMap (%v)", tc.name, profile, err)
			}
			if _, text, ok := resolved.ToUnicode.Map([]byte("A")); !ok || text != "Α" {
				t.Errorf("%s, %s: code A reads %q, want U+0391", tc.name, profile, text)
			}
		}
	}
}
