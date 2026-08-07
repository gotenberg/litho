package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// buildEncodedFontPDF builds a one-page PDF whose single simple font declares
// the given /Encoding value verbatim.
func buildEncodedFontPDF(encoding string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /TimesNewRomanPSMT /FirstChar 32 /LastChar 213 " +
			"/Encoding " + encoding + " >>",
	}

	header := "%PDF-1.7\n"
	body := ""
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = len(header) + len(body)
		body += fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, o)
	}

	var xref bytes.Buffer
	fmt.Fprintf(&xref, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&xref, "%010d 00000 n\r\n", off)
	}
	trailer := fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, len(header)+len(body))

	return []byte(header + body + xref.String() + trailer)
}

// TestOverlaySimpleFontEncoding pins that a generated /ToUnicode takes its
// destinations from the font's /Encoding.
//
// What a code MEANS is stated by /Encoding through glyph names (ISO 32000-1,
// 9.6.6); the font program's cmap only says which glyph is drawn. Under
// /MacRomanEncoding code 0xD5 is quoteright (U+2019), and taking the program's
// answer instead wrote U+00D5, turning every ’ into Õ at identical length.
func TestOverlaySimpleFontEncoding(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		encoding string
		code     uint16
		want     rune
		what     string
	}{
		{"/MacRomanEncoding", 0xD5, 0x2019, "MacRoman 0xD5 is quoteright"},
		{"/WinAnsiEncoding", 0x92, 0x2019, "WinAnsi 0x92 is quoteright"},
		{"/WinAnsiEncoding", 0x41, 'A', "ASCII is unchanged"},
		{"<< /BaseEncoding /MacRomanEncoding /Differences [213 /bullet] >>", 0xD5, 0x2022, "/Differences wins"},
	}

	for _, c := range cases {
		data := buildEncodedFontPDF(c.encoding)
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil || fontDict == nil {
			t.Fatalf("%s: resolving font: %v", c.what, err)
		}

		// Seed the destination the font program would have produced.
		mapping := map[uint16]rune{c.code: rune(c.code)}
		overlaySimpleFontEncoding(ctx, doc, fontDict, mapping)

		if got := mapping[c.code]; got != c.want {
			t.Errorf("%s: code 0x%02X mapped to U+%04X, want U+%04X", c.what, c.code, got, c.want)
		}
	}
}
