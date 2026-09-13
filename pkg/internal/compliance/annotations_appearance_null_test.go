package compliance

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestAnnotAppearance_NullAndUndefined pins how an appearance dictionary reads
// when it, or one of its entries, is null or names an undefined object.
//
// ISO 32000-1, 7.3.7 treats a dictionary entry whose value is null as absent,
// and 7.3.10 treats a reference to an undefined object as a reference to the
// null object. veraPDF applies them this way: an entry written as null is
// dropped, so /AP << /N null >> has no /N; an entry whose value is a reference
// is kept and judged by what it resolves to, so /N naming an undefined object is
// a null that is no appearance stream; and an /AP that resolves to no
// dictionary is no appearance dictionary. pdfjs pypdf2332.pdf points /N at an
// object past its /Size on four markup annotations, and veraPDF reports ISO
// 19005-4, 6.3.3 t4 on each while litho said nothing. Every case was measured
// against veraPDF.
func TestAnnotAppearance_NullAndUndefined(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Length 0 >>\nstream\n\nendstream",
	)
	undefined := ast.PdfIndirectRef{ObjectNum: 99}
	stream := ast.PdfIndirectRef{ObjectNum: 2}
	rect := ast.PdfArray{ast.PdfInt(10), ast.PdfInt(10), ast.PdfInt(100), ast.PdfInt(100)}

	annot := func(ap ast.PdfObject) ast.PdfDict {
		return ast.PdfDict{"Subtype": ast.PdfName("Square"), "Rect": rect, "AP": ap}
	}

	tests := []struct {
		name    string
		subtype ast.PdfName
		annot   ast.PdfDict
		part    int
		want    []string
	}{
		{"/N naming an undefined object", "Square", annot(ast.PdfDict{"N": undefined}), 2, []string{"6.3.3/4"}},
		{"/N written as null", "Square", annot(ast.PdfDict{"N": ast.PdfNull{}}), 2, []string{"6.3.3/2"}},
		{"an extra entry written as null", "Square", annot(ast.PdfDict{"N": stream, "D": ast.PdfNull{}}), 2, nil},
		{"an extra entry naming an undefined object", "Square", annot(ast.PdfDict{"N": stream, "D": undefined}), 2, []string{"6.3.3/2"}},
		{"/AP written as null", "Square", annot(ast.PdfNull{}), 2, []string{"6.3.3/1"}},
		{"/AP naming an undefined object", "Square", annot(undefined), 2, []string{"6.3.3/1"}},
		{"/AP that is no dictionary", "Square", annot(ast.PdfInt(5)), 2, []string{"6.3.3/1"}},
		{"a button's /N naming an undefined object", "Widget",
			ast.PdfDict{"Subtype": ast.PdfName("Widget"), "FT": ast.PdfName("Btn"), "Rect": rect, "AP": ast.PdfDict{"N": undefined}},
			2, []string{"6.3.3/3"}},
		{"part 1: /N naming an undefined object", "Square", annot(ast.PdfDict{"N": undefined}), 1, []string{"6.5.3/6"}},
		{"part 1: /N written as null", "Square", annot(ast.PdfDict{"N": ast.PdfNull{}}), 1, []string{"6.5.3/4"}},
		{"part 1: /AP written as null", "Square", annot(ast.PdfNull{}), 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, v := range checkAnnotAppearance(ctx, doc, tt.annot, tt.subtype, tt.part, 'b', "annot") {
				got = append(got, fmt.Sprintf("%s/%d", v.Rule.Clause, v.Rule.TestNumber))
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
