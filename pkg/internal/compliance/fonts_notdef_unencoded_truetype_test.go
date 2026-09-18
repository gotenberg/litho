package compliance

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// withPostNames returns the sfnt carrying a format 2.0 post table that names
// every glyph, so the .notdef check takes its glyph-name branch rather than the
// cmap one. The shipped fixture carries post 3.0, which names nothing.
//
// The new table is appended and the directory entry repointed, so every other
// table keeps its absolute offset.
func withPostNames(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < 12 {
		t.Fatal("font too short")
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))

	numGlyphs := 0
	for i := range numTables {
		rec := data[12+i*16 : 12+i*16+16]
		if string(rec[:4]) == "maxp" {
			off := int(binary.BigEndian.Uint32(rec[8:12]))
			if off+6 > len(data) {
				t.Fatal("maxp offset past end of font")
			}
			numGlyphs = int(binary.BigEndian.Uint16(data[off+4 : off+6]))
		}
	}
	if numGlyphs == 0 || numGlyphs > 0xFFFF {
		t.Fatalf("unusable numGlyphs %d", numGlyphs)
	}

	var post bytes.Buffer
	post.Write([]byte{0x00, 0x02, 0x00, 0x00}) // version 2.0
	post.Write(make([]byte, 28))               // italicAngle through maxMemType1
	idx := make([]byte, 2)
	binary.BigEndian.PutUint16(idx, uint16(numGlyphs)) //nolint:gosec // G115: bounded by the guard above.
	post.Write(idx)
	for i := range numGlyphs {
		binary.BigEndian.PutUint16(idx, uint16(258+i)) //nolint:gosec // G115: 258+numGlyphs stays inside uint16 for this fixture.
		post.Write(idx)
	}
	for i := range numGlyphs {
		name := fmt.Sprintf("g%d", i)
		if i == 0 {
			name = ".notdef"
		}
		post.WriteByte(byte(len(name)))
		post.WriteString(name)
	}

	out := append([]byte{}, data...)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	offset, length := len(out), post.Len()
	out = append(out, post.Bytes()...)
	for i := range numTables {
		rec := out[12+i*16 : 12+i*16+16]
		if string(rec[:4]) == "post" {
			binary.BigEndian.PutUint32(rec[8:12], uint32(offset))  //nolint:gosec // G115: fixture size is far below 4 GB.
			binary.BigEndian.PutUint32(rec[12:16], uint32(length)) //nolint:gosec // G115: table size is a few KB.
		}
	}
	return out
}

// TestNotdef_UnencodedTrueTypeIsJudgedByItsCmap pins that a TrueType declaring
// no /Encoding is not condemned code by code.
//
// The glyph-name branch of the .notdef check reads the name the font's
// /Encoding gives each code. A font that declares no /Encoding names nothing,
// so every used code came back empty and was reported: ISO 32000-1, 9.6.6.4
// resolves such a TrueType through the program's own cmap instead, which the
// no-names branch already consults. pdfjs issue6410.pdf embeds four nonsymbolic
// TrueTypes of exactly this shape, whose post 2.0 tables send them down this
// branch, and litho named charCode 32 although all three cmap subtables map it
// to "space"; veraPDF judged 149 glyphs there and found no .notdef.
//
// The MacExpertEncoding case is the guard that matters: a font that DOES
// declare an encoding leaving the shown code undefined must still fire, so the
// fix narrows the check rather than silencing it.
func TestNotdef_UnencodedTrueTypeIsJudgedByItsCmap(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	program := withPostNames(t, raw)

	fires := func(t *testing.T, encoding string) bool {
		t.Helper()
		data := cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			"<< /Length 40 >>\nstream\nBT /F1 12 Tf 10 50 Td (A) Tj ET\nendstream",
			"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 65 /LastChar 65"+
				" /Widths [722] "+encoding+"/FontDescriptor 6 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 32 /Ascent 700 /Descent -200"+
				" /CapHeight 700 /StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile2 7 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), string(program)),
		)
		return firesRuleProfile(t, data, models.PDFA2b, "6.2.11.8", 1)
	}

	t.Run("no /Encoding is judged by the cmap, not condemned", func(t *testing.T) {
		t.Parallel()
		if fires(t, "") {
			t.Error("a TrueType declaring no /Encoding must not report every shown code as .notdef")
		}
	})

	t.Run("a declared encoding leaving the code undefined still fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, "/Encoding << /Type /Encoding /BaseEncoding /MacExpertEncoding >> ") {
			t.Error("MacExpertEncoding defines no glyph at 'A', so the code maps to .notdef and must fire")
		}
	})
}
