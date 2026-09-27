package remediation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestTreeWalks_SharedKidsArrayFanOut pins that the remediation walks over the
// EmbeddedFiles name tree visit a /Kids array written as an object of its own
// once, so direct nodes that name it again cannot fan out 2^depth, and that
// each walk still reaches the leaf beside them.
func TestTreeWalks_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 4 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Kids 5 0 R >>",
		"[<< /Kids 5 0 R >> << /Kids 5 0 R >> << /Names [(k) 6 0 R] >>]",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) >>",
	)
	tests := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{"walkEmbeddedFileTree", func(t *testing.T) error {
			doc, alloc := newTestDoc(t, data)
			count := 0
			walkEmbeddedFileTree(context.Background(), alloc, doc, ast.PdfDict{"Kids": ast.PdfIndirectRef{ObjectNum: 5}}, &count, make(map[int]bool), 0)
			return nil
		}},
		{"fixAFInNameTree", func(t *testing.T) error {
			doc, alloc := newTestDoc(t, data)
			specs := make(map[int]bool)
			fixAFInNameTree(context.Background(), doc, alloc, ast.PdfIndirectRef{ObjectNum: 4}, make(map[int]bool), 0, specs)
			if !specs[6] {
				return fmt.Errorf("specs = %v, want 6", specs)
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
