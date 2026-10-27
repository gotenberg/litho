package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// collectionType0Font builds a Type 0 font over a CIDFontType2 of the Adobe
// collection ordering, under the /Encoding enc. When embedded, the descendant
// carries a TrueType program whose glyph 1 is A and a /CIDToGIDMap stream
// sending CIDs 1 and 2 to it, so a reading through the program gives A.
func collectionType0Font(ordering string, enc ast.PdfObject, embedded bool) ast.PdfDict {
	cidFont := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("CIDFontType2"),
		"BaseFont": ast.PdfName("LithoCollection"),
		"CIDSystemInfo": ast.PdfDict{
			"Registry":   ast.PdfString("Adobe"),
			"Ordering":   ast.PdfString(ordering),
			"Supplement": ast.PdfInt(2),
		},
		"DW": ast.PdfInt(500),
	}
	fd := ast.PdfDict{
		"Type":     ast.PdfName("FontDescriptor"),
		"FontName": ast.PdfName("LithoCollection"),
		"Flags":    ast.PdfInt(4),
	}
	if embedded {
		program := fontbuild.TrueType{
			PostScriptName: "LithoCollection", Family: "Litho", Subfamily: "Regular",
			Glyphs: []fontbuild.Glyph{{Name: "A", Unicode: 'A', Advance: 500}},
		}.Build()
		fd["FontFile2"] = ast.NewStream(ast.PdfDict{}, program)
		cidFont["CIDToGIDMap"] = ast.NewStream(ast.PdfDict{}, []byte{0, 0, 0, 1, 0, 1})
	}
	cidFont["FontDescriptor"] = fd
	return ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName("LithoCollection"),
		"Encoding":        enc,
		"DescendantFonts": ast.PdfArray{cidFont},
	}
}

// TestResolveFont_CollectionBeforeProgram pins the order of ISO 32000-1,
// 9.10.2 for a Type 0 font with no /ToUnicode whose descendant uses the
// Adobe-Korea1 collection: the code maps to its CID through the font's CMap,
// and the CID to Unicode through Adobe-Korea1-UCS2, where CIDs 1 and 2 are
// U+0020 and U+0021. The glyph the embedded program paints for them, A, takes
// no part, under Identity-H and under the collection's own CMap,
// Adobe-Korea1-2, which PDF.js bundles though Table 118 does not list it.
func TestResolveFont_CollectionBeforeProgram(t *testing.T) {
	t.Parallel()

	doc := encodingCIDDocument(t, "")
	for _, c := range []struct {
		name     string
		enc      ast.PdfName
		embedded bool
	}{
		{name: "Identity-H over an embedded program", enc: "Identity-H", embedded: true},
		{name: "collection CMap over an embedded program", enc: "Adobe-Korea1-2", embedded: true},
		{name: "collection CMap, font not embedded", enc: "Adobe-Korea1-2"},
	} {
		font, err := ResolveFont(context.Background(), doc, collectionType0Font("Korea1", c.enc, c.embedded))
		if err != nil {
			t.Fatalf("%s: ResolveFont: %v", c.name, err)
		}
		for code, want := range map[string]string{"\x00\x01": " ", "\x00\x02": "!"} {
			if char, n, skip := decodeNextChar(font, []byte(code)); char != want || n != 2 || skip {
				t.Errorf("%s: decodeNextChar(%q) = (%q, %d, %v), want (%q, 2, false)", c.name, code, char, n, skip, want)
			}
		}
	}
}

// TestDecodeNextChar_CIDZeroNamesNoCharacter pins that a code a Type 0 font's
// encoding CMap maps to CID 0, the .notdef glyph, adds no character (ISO
// 32000-1, 9.7.6.3) and advances as one glyph, where litho read the code
// itself.
func TestDecodeNextChar_CIDZeroNamesNoCharacter(t *testing.T) {
	t.Parallel()

	const encodingCMap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		"2 begincidchar\n<0041> 34\n<0142> 0\nendcidchar\n" +
		"endcmap end end\n"
	doc := encodingCIDDocument(t, "")
	font, err := ResolveFont(context.Background(), doc,
		collectionType0Font("Japan1", ast.NewStream(ast.PdfDict{"Type": ast.PdfName("CMap")}, []byte(encodingCMap)), false))
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	for code, want := range map[string]string{"\x00\x41": "A", "\x01\x42": ""} {
		if char, n, skip := decodeNextChar(font, []byte(code)); char != want || n != 2 || skip {
			t.Errorf("decodeNextChar(%q) = (%q, %d, %v), want (%q, 2, false)", code, char, n, skip, want)
		}
	}
}

// TestResolveFont_Type0WithoutDescendantReadsNothing pins that a Type 0 font
// whose /DescendantFonts holds no CIDFont dictionary, which ISO 32000-1,
// 9.7.6.1 requires, and which carries no /ToUnicode, reads no character: no
// step of 9.10.2 applies.
func TestResolveFont_Type0WithoutDescendantReadsNothing(t *testing.T) {
	t.Parallel()

	doc := encodingCIDDocument(t, "")
	for name, descendants := range map[string]ast.PdfObject{
		"absent":         nil,
		"null entry":     ast.PdfArray{ast.PdfNull{}},
		"empty":          ast.PdfArray{},
		"dict, no array": collectionType0Font("Japan1", ast.PdfName("Identity-H"), false)["DescendantFonts"].(ast.PdfArray)[0],
	} {
		dict := ast.PdfDict{
			"Type":     ast.PdfName("Font"),
			"Subtype":  ast.PdfName("Type0"),
			"BaseFont": ast.PdfName("LithoNoDescendant"),
			"Encoding": ast.PdfName("Identity-H"),
		}
		if descendants != nil {
			dict["DescendantFonts"] = descendants
		}
		font, err := ResolveFont(context.Background(), doc, dict)
		if err != nil {
			t.Fatalf("%s: ResolveFont: %v", name, err)
		}
		if char, _, _ := decodeNextChar(font, []byte("\x00\x01")); char != "" {
			t.Errorf("%s: decodeNextChar(<0001>) = %q, want no character", name, char)
		}
	}
}

func TestIsCollectionCMapName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"Adobe-Korea1-2":    true,
		"Adobe-Japan1-7":    true,
		"Adobe-GB1-5":       true,
		"Adobe-CNS1-0":      true,
		"Adobe-KR-9":        false,
		"Adobe-Japan1-":     false,
		"Adobe-Japan1-x":    false,
		"Adobe-Japan1-UCS2": false,
		"UniJIS-UCS2-H":     false,
		"Identity-H":        false,
	} {
		if got := isCollectionCMapName(name); got != want {
			t.Errorf("isCollectionCMapName(%q) = %v, want %v", name, got, want)
		}
	}
}
