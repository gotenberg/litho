package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestToUnicode_ZapfDingbatsImplicitBase pins that a ZapfDingbats font with a
// /Differences-only encoding takes no StandardEncoding base.
//
// ISO 32000-1, Table 114 makes the implicit base of a symbolic font its
// built-in encoding, and ZapfDingbats is symbolic whether or not a font
// descriptor says so; its built-in names (a1 to a191) carry no Unicode value.
// litho read a descriptor-less ZapfDingbats as non-symbolic and gave it
// StandardEncoding, so govdocs thread_300_300031.pdf's code 110, which its
// /Differences does not name, read as the letter n. veraPDF reports the glyph
// under ISO 14289-1 §7.21.7 t1.
func TestToUnicode_ZapfDingbatsImplicitBase(t *testing.T) {
	t.Parallel()

	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"1 begincodespacerange <00> <FF> endcodespacerange " +
		"1 beginbfchar <6E> <25A0> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end"

	tests := []struct {
		name  string
		font  string
		extra []string
		fires bool
	}{
		{"a code the Differences leave unnamed", "<< /Type /Font /Subtype /Type1 /BaseFont /ZapfDingbats " +
			"/Encoding << /Differences [128 /a89] >> >>", nil, true},
		{
			"the same code mapped by a ToUnicode", "<< /Type /Font /Subtype /Type1 /BaseFont /ZapfDingbats " +
				"/Encoding << /Differences [128 /a89] >> /ToUnicode 3 0 R >>",
			[]string{fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap)},
			false,
		},
		{"a Latin font keeps its StandardEncoding base", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica " +
			"/Encoding << /Differences [128 /bullet] >> >>", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, _ := objsDocPDF(t, append([]string{"<< /Type /Catalog >>", tt.font}, tt.extra...)...)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{110: true}}
			got := checkToUnicode(ctx, doc, fontDict, "F1", 1, "PDF/UA-1", usage)
			if fired := hasRule(got, toUnicodeRule("PDF/UA-1")); fired != tt.fires {
				t.Errorf("fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
