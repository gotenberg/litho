package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestNullResourceEntryIsUndefined pins ISO 19005-4, 6.2.2 t3 for a resource
// entry whose value is null. ISO 32000-1, 7.3.7 treats such an entry as absent,
// and 7.3.10 makes a reference to an undefined object a reference to null, so
// the name the content stream uses is undefined. Because the stream's own
// dictionary holds the key, veraPDF reports it as undefined (t3) even when the
// Pages tree defines the name, never as inherited (t2), and ISO 19005-2 states
// nothing for it. Every case was measured against veraPDF.
func TestNullResourceEntryIsUndefined(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		"null",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
	ops, err := content.ParseStreamData(ctx, []byte("BT /F0 12 Tf 10 50 Td (Hi) Tj ET"))
	if err != nil {
		t.Fatalf("ParseStreamData: %v", err)
	}
	fonts := func(f0 ast.PdfObject) ast.PdfDict {
		return ast.PdfDict{"Font": ast.PdfDict{"F0": f0}}
	}
	nullRef := ast.PdfIndirectRef{ObjectNum: 3}
	fontRef := ast.PdfIndirectRef{ObjectNum: 4}

	tests := []struct {
		name      string
		own       ast.PdfDict
		inherited ast.PdfDict
		part      int
		want      Rule // the zero Rule when nothing shall fire
	}{
		{"a reference to a null object", fonts(nullRef), nil, 4, Rule_19005_4_6_2_2_t03},
		{"a direct null", fonts(ast.PdfNull{}), nil, 4, Rule_19005_4_6_2_2_t03},
		{"a reference to an undefined object", fonts(ast.PdfIndirectRef{ObjectNum: 40}), nil, 4, Rule_19005_4_6_2_2_t03},
		{"a null entry beside a defined inherited one", fonts(nullRef), fonts(fontRef), 4, Rule_19005_4_6_2_2_t03},
		{"a defined font", fonts(fontRef), nil, 4, Rule{}},
		{"PDF/A-2 and a null entry beside a defined inherited one", fonts(nullRef), fonts(fontRef), 2, Rule{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkContentStreamResourcesOps(ctx, doc, ops, nil, tt.own, tt.inherited, tt.part, 1, false, "")
			if tt.want.Specification == "" {
				if len(got) != 0 {
					t.Errorf("got %v, want no violation", got)
				}
				return
			}
			if len(got) != 1 || got[0].Rule != tt.want {
				t.Errorf("got %v, want exactly %s t%d", got, tt.want.Clause, tt.want.TestNumber)
			}
		})
	}
}
