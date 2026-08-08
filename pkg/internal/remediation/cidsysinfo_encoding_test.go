package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestEncodingIsIdentityCompatible pins which Type 0 /Encoding values may be
// relabelled /Identity-H to sidestep the CIDSystemInfo rule.
//
// Relabelling only preserves the text when the codes already ARE the CIDs. A
// CMap defines its own codespace (ISO 32000-1, 9.7.5): issue18117.pdf embeds
// /Uni-Utf8-H, whose codes run one to four bytes, and relabelling it left an
// odd-length code tail and glyphs resolving to .notdef.
func TestEncodingIsIdentityCompatible(t *testing.T) {
	ctx := context.Background()
	doc := &document.Document{Logger: document.NopLogger()}

	cmapStream := ast.NewStream(ast.PdfDict{
		"Type":     ast.PdfName("CMap"),
		"CMapName": ast.PdfName("Uni-Utf8-H"),
	}, []byte("%!PS"))

	cases := []struct {
		name     string
		encoding ast.PdfObject
		want     bool
	}{
		{"no /Encoding", nil, true},
		{"Identity-H", ast.PdfName("Identity-H"), true},
		{"Identity-V", ast.PdfName("Identity-V"), true},
		{"embedded CMap stream", cmapStream, false},
		{"predefined UniJIS-UCS2-H", ast.PdfName("UniJIS-UCS2-H"), false},
		{"predefined 90ms-RKSJ-H", ast.PdfName("90ms-RKSJ-H"), false},
	}

	for _, c := range cases {
		fontDict := ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type0")}
		if c.encoding != nil {
			fontDict["Encoding"] = c.encoding
		}
		if got := encodingIsIdentityCompatible(ctx, doc, fontDict); got != c.want {
			t.Errorf("%s: encodingIsIdentityCompatible = %v, want %v", c.name, got, c.want)
		}
	}
}
