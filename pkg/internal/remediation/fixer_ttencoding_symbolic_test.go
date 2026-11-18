package remediation

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixTrueTypeEncoding_DropsASymbolicEncodingNoGlyphReads pins the repair
// of ISO 19005-2, 6.2.11.6 t3 on a symbolic TrueType font.
//
// A program whose only lookup is the (3,0) subtable draws every code the same
// with the base encoding as without it (ISO 32000-1, 9.6.6.4), so the encoding
// goes, and a /Differences array, which the clause allows, stays. A program
// with a (3,1) or a (1,0) subtable loses it too when no shown code changes
// glyph, and these documents show none
// ([TestFixTrueTypeEncoding_SymbolicRoutesAgreeOnShownCodes]). The text the encoding gave each code moves into a
// /ToUnicode, so extraction reads the same, and a font that has one keeps it.
// The repair runs with PreserveContent set, the
// default, since it changes no glyph.
func TestFixTrueTypeEncoding_DropsASymbolicEncodingNoGlyphReads(t *testing.T) {
	t.Parallel()

	symbol := cmapSubtableRecord{3, 0, cmapFormat0(map[byte]byte{0x41: 1})}
	unicode := cmapSubtableRecord{3, 1, cmapFormat0(map[byte]byte{0x41: 1})}
	roman := cmapSubtableRecord{1, 0, cmapFormat0(map[byte]byte{0x41: 1})}
	differences := ast.PdfDict{"Differences": ast.PdfArray{ast.PdfInt(65), ast.PdfName("a20")}}

	cases := []struct {
		name      string
		records   []cmapSubtableRecord
		fontExtra string
		extra     []string
		dropped   bool
		want      ast.PdfObject // the font's /Encoding after the repair; nil is absent
	}{
		{"named encoding", []cmapSubtableRecord{symbol}, " /Encoding /WinAnsiEncoding", nil, true, nil},
		{
			"dictionary keeps its differences",
			[]cmapSubtableRecord{symbol},
			" /Encoding << /BaseEncoding /WinAnsiEncoding /Differences [65 /a20] >>", nil, true, differences,
		},
		{
			"shared dictionary is copied onto the font",
			[]cmapSubtableRecord{symbol},
			" /Encoding 7 0 R",
			[]string{"<< /Type /Encoding /BaseEncoding /MacRomanEncoding /Differences [65 /a20] >>"},
			true,
			ast.PdfDict{"Type": ast.PdfName("Encoding"), "Differences": ast.PdfArray{ast.PdfInt(65), ast.PdfName("a20")}},
		},
		{"base encoding alone", []cmapSubtableRecord{symbol}, " /Encoding << /BaseEncoding /WinAnsiEncoding >>", nil, true, nil},
		{
			"stated map is kept",
			[]cmapSubtableRecord{symbol},
			" /Encoding /WinAnsiEncoding /ToUnicode 7 0 R",
			[]string{forbiddenToUnicode()},
			true, nil,
		},
		{"differences alone pass already", []cmapSubtableRecord{symbol}, " /Encoding << /Differences [65 /a20] >>", nil, false, nil},
		{"unicode subtable", []cmapSubtableRecord{symbol, unicode}, " /Encoding /WinAnsiEncoding", nil, true, nil},
		{"roman subtable", []cmapSubtableRecord{symbol, roman}, " /Encoding /WinAnsiEncoding", nil, true, nil},
		{"no symbol subtable", []cmapSubtableRecord{roman}, " /Encoding /WinAnsiEncoding", nil, false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			program := trueTypeWithCmapTable(cmapTableOf(c.records...))
			doc, alloc := newTestDoc(t, symbolicTrueTypePDF(program, c.fontExtra, c.extra...))

			descs, err := fixTrueTypeEncoding(context.Background(), alloc, doc, Options{PreserveContent: true}, nil, "PDF/A-2b")
			if err != nil {
				t.Fatalf("fixTrueTypeEncoding: %v", err)
			}
			_, pending := alloc.Pending()[4]
			if !c.dropped {
				if pending || len(descs) != 0 {
					t.Fatalf("font rewritten (%v), want it left alone", descs)
				}
				return
			}
			if !pending || len(descs) != 1 {
				t.Fatalf("font not rewritten (%v)", descs)
			}
			if got := pendingDict(t, alloc, 4)["Encoding"]; !reflect.DeepEqual(got, c.want) {
				t.Errorf("/Encoding = %v, want %v", got, c.want)
			}
			if _, shared := alloc.Pending()[7]; shared && len(c.extra) > 0 {
				t.Error("the shared encoding object was rewritten")
			}
			ref, ok := pendingDict(t, alloc, 4)["ToUnicode"].(ast.PdfIndirectRef)
			if !ok {
				t.Fatal("no /ToUnicode keeps the reading the encoding gave")
			}
			if strings.Contains(c.fontExtra, "/ToUnicode") {
				if ref.ObjectNum != 7 {
					t.Errorf("/ToUnicode = %v, want the stated 7 0 R", ref)
				}
				return
			}
			// Code 0x42, which no /Differences renames, read as "B" through the
			// encoding, and still does.
			stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
			if !ok {
				t.Fatalf("/ToUnicode %v is no stream", ref)
			}
			data, err := stream.Bytes(context.Background())
			if err != nil || !strings.Contains(string(data), "<42> <0042>") {
				t.Errorf("/ToUnicode does not read code 0x42 as B (%v):\n%s", err, data)
			}
		})
	}
}

// TestFixTrueTypeEncoding_SymbolicRoutesAgreeOnShownCodes pins the repair of
// ISO 19005-2, 6.2.11.6 t3 on a symbolic TrueType font whose program carries
// a (3,1) or (1,0) subtable beside its (3,0) one.
//
// ISO 32000-1, 9.6.6.4 reads a code through its glyph name while the font has
// a base encoding, and through the code itself once it has none. The encoding
// goes when both routes select the same glyph for every code the page shows,
// whatever they select for the others, and stays when one shown code changes
// glyph. A font the form's default resources name can be typed with any code,
// so there every code must agree.
func TestFixTrueTypeEncoding_SymbolicRoutesAgreeOnShownCodes(t *testing.T) {
	t.Parallel()

	symbol := cmapSubtableRecord{3, 0, cmapFormat0(map[byte]byte{0x41: 1, 0x42: 2})}
	cases := []struct {
		name    string
		records []cmapSubtableRecord
		form    bool
		dropped bool
	}{
		{
			// The reporter's shape: (1,0) agrees on A and B, and code 0x80,
			// which no page shows, reads Euro, Mac OS Roman 0xDB, glyph 3.
			"roman agrees on shown codes", []cmapSubtableRecord{
				symbol, {1, 0, cmapFormat0(map[byte]byte{0x41: 1, 0x42: 2, 0xDB: 3})},
			}, false, true,
		},
		{
			"roman agrees but the form may type any code", []cmapSubtableRecord{
				symbol, {1, 0, cmapFormat0(map[byte]byte{0x41: 1, 0x42: 2, 0xDB: 3})},
			}, true, false,
		},
		{
			"roman agrees on every code under the form", []cmapSubtableRecord{
				symbol, {1, 0, cmapFormat0(map[byte]byte{0x41: 1, 0x42: 2})},
			}, true, true,
		},
		{
			"roman swaps a shown code", []cmapSubtableRecord{
				symbol, {1, 0, cmapFormat0(map[byte]byte{0x41: 2, 0x42: 1})},
			}, false, false,
		},
		{
			"unicode agrees on shown codes", []cmapSubtableRecord{
				symbol, {3, 1, cmapFormat0(map[byte]byte{0x41: 1, 0x42: 2})},
			}, false, true,
		},
		{
			"unicode selects another glyph for a shown code", []cmapSubtableRecord{
				symbol, {3, 1, cmapFormat0(map[byte]byte{0x41: 1, 0x42: 1})},
			}, false, false,
		},
		{
			"name route selects nothing", []cmapSubtableRecord{
				symbol, {1, 0, cmapFormat0(map[byte]byte{0x61: 1})},
			}, false, true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			program := trueTypeWithCmapTable(cmapTableOf(c.records...))
			catalog := "<< /Type /Catalog /Pages 2 0 R >>"
			if c.form {
				catalog = "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [] /DR << /Font << /F1 4 0 R >> >> >> >>"
			}
			doc, alloc := newTestDoc(t, assemblePDF([]string{
				catalog,
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R /Resources << /Font << /F1 4 0 R >> >> >>",
				"<< /Type /Font /Subtype /TrueType /BaseFont /LithoSym /FirstChar 65 /LastChar 66 /Widths [500 500] /FontDescriptor 5 0 R /Encoding /WinAnsiEncoding >>",
				"<< /Type /FontDescriptor /FontName /LithoSym /Flags 4 /FontFile2 6 0 R >>",
				streamObject(program),
				streamObject([]byte("BT /F1 12 Tf 72 700 Td (AB) Tj ET")),
			}))

			descs, err := fixTrueTypeEncoding(context.Background(), alloc, doc, Options{PreserveContent: true}, nil, "PDF/A-2b")
			if err != nil {
				t.Fatalf("fixTrueTypeEncoding: %v", err)
			}
			_, pending := alloc.Pending()[4]
			switch {
			case c.dropped && (!pending || len(descs) != 1):
				t.Fatalf("font not rewritten (%v)", descs)
			case c.dropped:
				if enc, ok := pendingDict(t, alloc, 4)["Encoding"]; ok {
					t.Errorf("/Encoding = %v, want none", enc)
				}
			case pending || len(descs) != 0:
				t.Fatalf("font rewritten (%v), want it left alone", descs)
			}
		})
	}
}

// TestFixTrueTypeEncoding_MacRomanNameTakesItsLowestCode pins the glyph name's
// route of ISO 32000-1, 9.6.6.4 through the (1,0) subtable for a name
// MacRomanEncoding gives two codes. Annex D, D.2 names both 0o40 and 0o312
// "space", and Mac OS Roman puts the no-break space at 0o312, so "space" reads
// code 0x20: a font whose (1,0) subtable maps 0xCA to another glyph still
// loses its encoding when it shows code 0x20.
func TestFixTrueTypeEncoding_MacRomanNameTakesItsLowestCode(t *testing.T) {
	t.Parallel()

	lowest := make(map[string]byte)
	names := compliance.PredefinedEncodingNames("MacRomanEncoding")
	for c := 255; c >= 0; c-- {
		if name, ok := names[byte(c)]; ok {
			lowest[name] = byte(c)
		}
	}
	if !reflect.DeepEqual(macRomanCodes, lowest) {
		t.Errorf("macRomanCodes does not take each name's lowest code: space = %#x", macRomanCodes["space"])
	}

	program := trueTypeWithCmapTable(cmapTableOf(
		cmapSubtableRecord{3, 0, cmapFormat0(map[byte]byte{0x20: 1})},
		cmapSubtableRecord{1, 0, cmapFormat0(map[byte]byte{0x20: 1, 0xCA: 2})},
	))
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /LithoSym /FirstChar 32 /LastChar 32 /Widths [250] /FontDescriptor 5 0 R /Encoding /WinAnsiEncoding >>",
		"<< /Type /FontDescriptor /FontName /LithoSym /Flags 4 /FontFile2 6 0 R >>",
		streamObject(program),
		streamObject([]byte("BT /F1 12 Tf 72 700 Td ( ) Tj ET")),
	}))

	descs, err := fixTrueTypeEncoding(context.Background(), alloc, doc, Options{PreserveContent: true}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixTrueTypeEncoding: %v", err)
	}
	if _, pending := alloc.Pending()[4]; !pending || len(descs) != 1 {
		t.Fatalf("font not rewritten (%v)", descs)
	}
	if enc, ok := pendingDict(t, alloc, 4)["Encoding"]; ok {
		t.Errorf("/Encoding = %v, want none", enc)
	}
}
