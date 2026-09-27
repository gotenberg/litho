package compliance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestTreeWalks_SharedKidsArrayFanOut pins that the compliance walks over
// name and number trees visit a /Kids array written as an object of its own
// once, so direct nodes that name it again cannot fan out 2^depth, and that
// each walk still reaches the leaf beside them.
func TestTreeWalks_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Kids 5 0 R >>",
		"[<< /Kids 5 0 R >> << /Kids 5 0 R >> << /Names [(k) 6 0 R] /Nums [0 [6 0 R]] >>]",
		"<< /Type /Filespec /F (a.txt) >>",
	}
	root := ast.PdfIndirectRef{ObjectNum: 4}
	tests := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{"collectNameTreeSpecRefs", func(t *testing.T) error {
			doc, _ := objsDocPDF(t, objs...)
			specs := make(map[int]bool)
			collectNameTreeSpecRefs(context.Background(), doc, root, specs, make(map[int]bool), 0)
			if !specs[6] {
				return fmt.Errorf("specs = %v, want 6", specs)
			}
			return nil
		}},
		{"numberTreeLookup", func(t *testing.T) error {
			doc, _ := objsDocPDF(t, objs...)
			node := ast.PdfDict{"Kids": ast.PdfIndirectRef{ObjectNum: 5}}
			if _, complete := numberTreeLookup(context.Background(), doc, node, 1, 0, make(map[int]bool)); !complete {
				return fmt.Errorf("absent key read incomplete")
			}
			if got, _ := numberTreeLookup(context.Background(), doc, node, 0, 0, make(map[int]bool)); len(got) != 1 {
				return fmt.Errorf("key 0 = %v, want [6 0 R]", got)
			}
			return nil
		}},
		{"numberTreeValue", func(t *testing.T) error {
			doc, _ := objsDocPDF(t, objs...)
			node := ast.PdfDict{"Kids": ast.PdfIndirectRef{ObjectNum: 5}}
			if _, complete := numberTreeValue(context.Background(), doc, node, 1, 0, make(map[int]bool)); !complete {
				return fmt.Errorf("absent key read incomplete")
			}
			if got, _ := numberTreeValue(context.Background(), doc, node, 0, 0, make(map[int]bool)); got == nil {
				return fmt.Errorf("key 0 not found")
			}
			return nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			done := make(chan error, 1)
			go func() { done <- tt.run(t) }()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the walk did not end before the deadline")
			}
		})
	}
}
