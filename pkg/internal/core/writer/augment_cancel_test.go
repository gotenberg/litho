package writer

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestIncrementalAfterCancelledAugmentationKeepsUnlistedObject pins that an
// allocator seeded after a cancelled raw-byte augmentation still clears the
// object the source's table and /Size leave out, so the incremental update does
// not shadow it. The source defines object 4 past its table, as a file whose
// cross-reference stream does not list itself does.
//
// A cancelled first run spent the Document's one augmentation, the allocator
// was seeded from the index alone, and the update wrote its new object as
// object 4, so the reopened file resolved object 4 to the new object.
func TestIncrementalAfterCancelledAugmentationKeepsUnlistedObject(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	listed := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	offsets := make([]int, len(listed))
	for i, obj := range listed {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	b.WriteString("4 0 obj\n<< /Unlisted true >>\nendobj\n")
	xrefAt := b.Len()
	b.WriteString("xref\n0 4\n0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefAt)
	src := b.Bytes()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(src), int64(len(src)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	// The answer under a cancelled ctx is the index's and is not used: the
	// cancelled run is the point.
	doc.HighestObjectNumber(cancelled)

	alloc := NewObjAllocator(doc.HighestObjectNumber(ctx))
	added := alloc.Alloc(ast.PdfDict{"Added": ast.PdfBool(true)})
	if added.ObjectNum == 4 {
		t.Errorf("the allocator numbered its new object 4, the object the source's table leaves out")
	}

	startXRef, err := document.FindStartXRef(doc.RawReader(), doc.FileSize())
	if err != nil {
		t.Fatalf("FindStartXRef: %v", err)
	}
	trailer := ast.PdfDict{"Root": doc.Index.Trailer["Root"]}
	var out bytes.Buffer
	if err := WriteIncremental(ctx, &out, IncrementalConfig{
		Source:          doc.RawReader(),
		SourceSize:      doc.FileSize(),
		StartXRefOffset: startXRef,
		Allocator:       alloc,
		Trailer:         trailer,
		Generation:      doc.ObjectGeneration,
	}); err != nil {
		t.Fatalf("WriteIncremental: %v", err)
	}

	reopened, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the update: %v", err)
	}
	obj, err := reopened.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving object 4 in the update: %v", err)
	}
	if dict, ok := obj.(ast.PdfDict); !ok || dict["Unlisted"] != ast.PdfBool(true) {
		t.Errorf("object 4 in the update is %v, want the source's << /Unlisted true >>", obj)
	}
	obj, err = reopened.Resolve(ctx, added)
	if err != nil {
		t.Fatalf("resolving the new object %d: %v", added.ObjectNum, err)
	}
	if dict, ok := obj.(ast.PdfDict); !ok || dict["Added"] != ast.PdfBool(true) {
		t.Errorf("new object %d in the update is %v, want << /Added true >>", added.ObjectNum, obj)
	}
}
