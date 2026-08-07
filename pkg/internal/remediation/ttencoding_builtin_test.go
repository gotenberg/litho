package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestKeepsBuiltInEncoding pins which TrueType fonts must keep their own
// encoding rather than be given /WinAnsiEncoding.
//
// A /Symbol font referenced without a FontDescriptor used to read as
// non-symbolic, so the fixer wrote WinAnsi onto it: code 0x61 is alpha in
// Symbol but "a" in WinAnsi, and a chemistry paper's "α-keto-glutarate"
// became "a-keto-glutarate" at identical length.
func TestKeepsBuiltInEncoding(t *testing.T) {
	ctx := context.Background()
	doc := &document.Document{Logger: document.NopLogger()}

	descriptor := func(flags int) ast.PdfDict {
		return ast.PdfDict{"Type": ast.PdfName("FontDescriptor"), "Flags": ast.PdfInt(flags)}
	}

	cases := []struct {
		name string
		font ast.PdfDict
		want bool
	}{
		{"Symbol without descriptor", ast.PdfDict{"BaseFont": ast.PdfName("Symbol")}, true},
		{"subset-prefixed Symbol", ast.PdfDict{"BaseFont": ast.PdfName("ABCDEF+Symbol")}, true},
		{"ZapfDingbats", ast.PdfDict{"BaseFont": ast.PdfName("ZapfDingbats")}, true},
		// A font with no descriptor keeps the non-symbolic treatment; only a
		// built-in-encoded base font is exempt on name alone.
		{"no descriptor at all", ast.PdfDict{"BaseFont": ast.PdfName("TimesNewRomanPSMT")}, false},
		{
			"symbolic flag set",
			ast.PdfDict{"BaseFont": ast.PdfName("Wingdings"), "FontDescriptor": descriptor(4)},
			true,
		},
		{
			"non-symbolic with descriptor",
			ast.PdfDict{"BaseFont": ast.PdfName("TimesNewRomanPSMT"), "FontDescriptor": descriptor(32)},
			false,
		},
		{
			"non-symbolic, Symbol-like name still wins",
			ast.PdfDict{"BaseFont": ast.PdfName("Symbol"), "FontDescriptor": descriptor(32)},
			true,
		},
	}

	for _, c := range cases {
		if got := keepsBuiltInEncoding(ctx, doc, c.font); got != c.want {
			t.Errorf("%s: keepsBuiltInEncoding = %v, want %v", c.name, got, c.want)
		}
	}
}
