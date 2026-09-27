package compliance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestFieldWalks_SharedKidsArrayFanOut pins that every compliance walk over
// the form field tree visits a /Kids array written as an object of its own
// once: its direct fields each name it again as their /Kids, and walked anew
// wherever it is reached it fans out 2^depth up to the walk's depth cap. Each
// walk still judges the direct field beside the repeat, once.
func TestFieldWalks_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> /StructTreeRoot 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[<< /T (a) /FT /Tx /TU (t) /RV (<p>y</p>) /V (x) /Kids 4 0 R >> << /T (b) /Kids 4 0 R >>]",
		"<< /T (p) /FT /Tx /Kids 4 0 R >>",
		"<< /Type /StructTreeRoot /ParentTree << /Nums [0 7 0 R] >> >>",
		"<< /S /Form /Lang (en) >>",
	}
	fields := ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}}
	tests := []struct {
		name string
		run  func(ctx context.Context, doc *document.Document, cat ast.PdfDict) error
	}{
		{"collectFieldObjNums", func(ctx context.Context, doc *document.Document, _ ast.PdfDict) error {
			covered := make(map[int]bool)
			collectFieldObjNums(ctx, doc, fields, covered, 0)
			if !covered[5] {
				return fmt.Errorf("covered = %v, want 5", covered)
			}
			return nil
		}},
		{"checkFormField", func(ctx context.Context, doc *document.Document, _ ast.PdfDict) error {
			var results []Violation
			checkFormField(ctx, doc, fields[0], "", 2, false, 0, &results, 0, make(map[int]bool), make(map[int]bool))
			return nil
		}},
		{"checkFieldTULang", func(ctx context.Context, doc *document.Document, cat ast.PdfDict) error {
			treeRoot, _ := document.ResolveDict(ctx, doc, cat["StructTreeRoot"])
			var results []Violation
			checkFieldTULang(ctx, doc, treeRoot, fields, make(map[int]bool), 0, &results)
			if n := countRule(results, Rule_14289_1_7_2_t25); n != 1 {
				return fmt.Errorf("7.2 t25 fired %d times, want 1, for field a: %v", n, results)
			}
			return nil
		}},
		{"walkTextFieldRichValue", func(ctx context.Context, doc *document.Document, _ ast.PdfDict) error {
			results := walkTextFieldRichValue(ctx, doc, fields[0], "", 0, make(map[int]bool))
			if n := countRule(results, Rule_14289_2_8_10_3_3_t01); n != 1 {
				return fmt.Errorf("8.10.3.3 t1 fired %d times, want 1, for field a: %v", n, results)
			}
			return nil
		}},
		{"structParentLangValues", func(ctx context.Context, doc *document.Document, cat ast.PdfDict) error {
			treeRoot, _ := document.ResolveDict(ctx, doc, cat["StructTreeRoot"])
			structParentLangValues(ctx, buildValidationCtx(ctx, doc, cat, "PDF/UA-1"), treeRoot)
			return nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			doc, cat := objsDocPDF(t, objs...)
			if err := tt.run(ctx, doc, cat); err != nil {
				t.Error(err)
			}
			if ctx.Err() != nil {
				t.Fatal("the walk did not end before the deadline")
			}
		})
	}
}
