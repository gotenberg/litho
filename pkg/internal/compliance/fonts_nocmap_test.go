package compliance

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// stripCmapTable returns an sfnt font with its cmap table removed from the
// table directory, leaving the rest of the program intact.
func stripCmapTable(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < 12 {
		t.Fatal("font too short")
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	out := bytes.NewBuffer(nil)
	out.Write(data[:12])
	kept := 0
	for i := range numTables {
		rec := data[12+i*16 : 12+i*16+16]
		if string(rec[:4]) == "cmap" {
			continue
		}
		out.Write(rec)
		kept++
	}
	// The records after the directory keep their absolute offsets, so pad the
	// gap the dropped record leaves rather than shifting the tables.
	out.Write(make([]byte, 16))
	out.Write(data[12+numTables*16:])
	b := out.Bytes()
	binary.BigEndian.PutUint16(b[4:6], uint16(kept))
	return b
}

// TestTrueTypeNonSymbolicCmap_NoCmapTable pins that a non-symbolic TrueType
// program carrying no cmap table at all fails the cmap rule.
//
// ISO 19005-2 §6.2.11.6 t1 (ISO 19005-4 §6.2.10.6 t1) asks the embedded program
// to contain "at least" a Microsoft Unicode (3,1) or Macintosh Roman (1,0)
// subtable. litho read only the parsed subtable set, and a font with no cmap
// table makes the parse return an error rather than an empty set, so the check
// passed silently: pdfjs TrueType_without_cmap.pdf embeds exactly such a font
// and veraPDF reports it.
func TestTrueTypeNonSymbolicCmap_NoCmapTable(t *testing.T) {
	t.Parallel()

	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}

	// fires reports whether the cmap rule fails for a non-symbolic TrueType
	// font embedding program.
	fires := func(t *testing.T, program []byte, clause string, test int) bool {
		t.Helper()
		data := cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			"<< /Length 40 >>\nstream\nBT /F1 12 Tf 10 50 Td (A) Tj ET\nendstream",
			"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 65 /LastChar 65"+
				" /Widths [722] /Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 32 /Ascent 700 /Descent -200"+
				" /CapHeight 700 /StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile2 7 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), string(program)),
		)
		return firesRuleProfile(t, data, models.PDFA2b, clause, test)
	}

	t.Run("no cmap table fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, stripCmapTable(t, ttf), "6.2.11.6", 1) {
			t.Error("a non-symbolic TrueType with no cmap table must fail the cmap rule")
		}
	})

	t.Run("a program with a cmap table does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, ttf, "6.2.11.6", 1) {
			t.Error("the unmodified fixture carries a cmap and must not fail the cmap rule")
		}
	})
}
