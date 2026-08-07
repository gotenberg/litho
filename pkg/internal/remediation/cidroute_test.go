package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestIdentityCIDRoute pins the two links a generated /ToUnicode CMap assumes:
// the /Encoding CMap taking a code to a CID, and /CIDToGIDMap taking a CID to a
// GID. The generator keys destinations by GID and writes them as codes, so it
// may only run when both links are the identity.
//
// ArialMT carries a /CIDToGIDMap stream that is deliberately not the identity;
// assuming otherwise emitted another glyph's destination and "THANN" extracted
// as "qe^kk".
func TestIdentityCIDRoute(t *testing.T) {
	ctx := context.Background()
	doc := &document.Document{Logger: document.NopLogger()}

	stream := ast.NewStream(ast.PdfDict{}, []byte{0x00, 0x03, 0x00, 0x0A})

	cases := []struct {
		name     string
		encoding ast.PdfObject
		cidToGID ast.PdfObject
		want     bool
	}{
		{"Identity-H, no CIDToGIDMap", ast.PdfName("Identity-H"), nil, true},
		{"Identity-V, no CIDToGIDMap", ast.PdfName("Identity-V"), nil, true},
		{"Identity-H, /Identity", ast.PdfName("Identity-H"), ast.PdfName("Identity"), true},
		{"Identity-H, stream", ast.PdfName("Identity-H"), stream, false},
		{"predefined CMap", ast.PdfName("UniJIS-UCS2-H"), nil, false},
		{"predefined CMap with /Identity", ast.PdfName("90ms-RKSJ-H"), ast.PdfName("Identity"), false},
	}

	for _, c := range cases {
		fontDict := ast.PdfDict{"Subtype": ast.PdfName("Type0")}
		if c.encoding != nil {
			fontDict["Encoding"] = c.encoding
		}
		cidFont := ast.PdfDict{"Subtype": ast.PdfName("CIDFontType2")}
		if c.cidToGID != nil {
			cidFont["CIDToGIDMap"] = c.cidToGID
		}
		if got := identityCIDRoute(ctx, doc, fontDict, cidFont); got != c.want {
			t.Errorf("%s: identityCIDRoute = %v, want %v", c.name, got, c.want)
		}
	}
}
