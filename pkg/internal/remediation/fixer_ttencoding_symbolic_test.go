package remediation

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixTrueTypeEncoding_DropsASymbolicEncodingNoGlyphReads pins the repair
// of ISO 19005-2, 6.2.11.6 t3 on a symbolic TrueType font.
//
// A program whose only lookup is the (3,0) subtable draws every code the same
// with the base encoding as without it (ISO 32000-1, 9.6.6.4), so the encoding
// goes, and a /Differences array, which the clause allows, stays. A program
// with a (3,1) or a (1,0) subtable may draw a code another glyph once it goes,
// and keeps the residual. The text the encoding gave each code moves into a
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
			"dictionary keeps its differences", []cmapSubtableRecord{symbol},
			" /Encoding << /BaseEncoding /WinAnsiEncoding /Differences [65 /a20] >>", nil, true, differences,
		},
		{
			"shared dictionary is copied onto the font", []cmapSubtableRecord{symbol},
			" /Encoding 7 0 R", []string{"<< /Type /Encoding /BaseEncoding /MacRomanEncoding /Differences [65 /a20] >>"},
			true, ast.PdfDict{"Type": ast.PdfName("Encoding"), "Differences": ast.PdfArray{ast.PdfInt(65), ast.PdfName("a20")}},
		},
		{"base encoding alone", []cmapSubtableRecord{symbol}, " /Encoding << /BaseEncoding /WinAnsiEncoding >>", nil, true, nil},
		{
			"stated map is kept", []cmapSubtableRecord{symbol},
			" /Encoding /WinAnsiEncoding /ToUnicode 7 0 R", []string{forbiddenToUnicode()}, true, nil,
		},
		{"differences alone pass already", []cmapSubtableRecord{symbol}, " /Encoding << /Differences [65 /a20] >>", nil, false, nil},
		{"unicode subtable", []cmapSubtableRecord{symbol, unicode}, " /Encoding /WinAnsiEncoding", nil, false, nil},
		{"roman subtable", []cmapSubtableRecord{symbol, roman}, " /Encoding /WinAnsiEncoding", nil, false, nil},
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
