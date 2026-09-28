package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// objsPDF builds a document whose objects are objs, numbered from 1, with
// object 1 as the catalog.
func objsPDF(objs ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// sharedKArrayPDF builds a tagged one-page document whose structure tree root,
// object 4, lists the Sect element 7, whose /K is object 5: an array of two
// direct Div elements that each name object 5 again as their /K, beside the P
// element 6 marking content on the page. Walked anew wherever it is reached,
// the array fans out 2^depth up to the walk's depth cap.
func sharedKArrayPDF() []byte {
	return objsPDF(
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K [7 0 R] /ParentTree << /Nums [0 [6 0 R]] >> >>",
		"[<< /Type /StructElem /S /Div /P 7 0 R /K 5 0 R >> << /Type /StructElem /S /Div /P 7 0 R /K 5 0 R >> 6 0 R]",
		"<< /Type /StructElem /S /P /P 7 0 R /Pg 3 0 R /K 0 >>",
		"<< /Type /StructElem /S /Sect /P 4 0 R /K 5 0 R >>",
	)
}

// TestStructWalks_SharedKArrayFanOut pins that the mutation walks down a
// structure tree read a /K array written as an object of its own once, so
// direct elements naming it again cannot fan out 2^depth.
func TestStructWalks_SharedKArrayFanOut(t *testing.T) {
	t.Parallel()
	shared := ast.PdfIndirectRef{ObjectNum: 5}
	tests := []struct {
		name string
		run  func(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) error
	}{
		{"StructPageTargets", func(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) error {
			targets := StructPageTargets(ctx, AllocResolver(ctx, alloc, doc), ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 7}})
			if _, found := targets[3]; !found {
				return fmt.Errorf("no element opens page 3, want the P, object 6")
			}
			return nil
		}},
		{"structPrune.kids", func(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) error {
			p := newStructPrune(alloc, doc, func(ref ast.PdfIndirectRef) bool { return ref.ObjectNum == 3 }, nil, true)
			p.kids(ctx, shared, false, 0)
			if !p.memo[6].drop {
				return fmt.Errorf("the P marking content on the removed page stays")
			}
			return nil
		}},
		{"structPrune.keptNames", func(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) error {
			p := newStructPrune(alloc, doc, func(ast.PdfIndirectRef) bool { return false }, nil, true)
			p.memo[7] = structElemVerdict{carries: true}
			if names := p.keptNames(ctx); !names.types["Div"] {
				return fmt.Errorf("kept names %v, want Div among them", names.types)
			}
			return nil
		}},
		{"elementNames", func(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) error {
			if names := elementNames(ctx, alloc, doc, shared); !names.types["Div"] || !names.types["P"] {
				return fmt.Errorf("names %v, want Div and P", names.types)
			}
			return nil
		}},
		{"structOutput.firstPage", func(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) error {
			out := emptyStructOutput(alloc, "")
			out.target = doc
			if first, found := out.firstPage(ctx, ast.PdfIndirectRef{ObjectNum: 7}, map[int]int{3: 0}); !found || first != 0 {
				return fmt.Errorf("first page %d, %v, want index 0", first, found)
			}
			return nil
		}},
		{"structOutput.renameElement", func(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) error {
			out := emptyStructOutput(alloc, "")
			out.target = doc
			sect, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 7})
			if err != nil {
				return fmt.Errorf("resolving the Sect: %w", err)
			}
			out.renameElement(ctx, &structSource{}, sect, make(map[int]bool), 0)
			return nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			doc, alloc := newMutationDoc(t, sharedKArrayPDF())
			err := tt.run(ctx, doc, alloc)
			if ctx.Err() != nil {
				t.Fatal("the walk did not end before the deadline")
			}
			if err != nil {
				t.Error(err)
			}
		})
	}
}

// TestCompleteWrittenPageTree_SharedKidsArrayFanOut pins that the page tree
// repair reads a /Kids array written as an object of its own once, as the
// reader does: its direct nodes each name it again as their /Kids.
func TestCompleteWrittenPageTree_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc, alloc := newMutationDoc(t, objsPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids 4 0 R /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[3 0 R << /Type /Pages /Kids 4 0 R /Count 1 >> << /Type /Pages /Kids 4 0 R /Count 1 >>]",
	))
	alloc.AllocAt(2, ast.PdfDict{"Type": ast.PdfName("Pages"), "Kids": ast.PdfIndirectRef{ObjectNum: 4}, "Count": ast.PdfInt(1)})
	err := CompleteWrittenPageTree(ctx, alloc, doc, nil)
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if err != nil {
		t.Fatalf("CompleteWrittenPageTree: %v", err)
	}
}
