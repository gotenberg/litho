package tagging

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// stagedTestDoc opens a single untagged page whose content stream, object 5,
// shows "source text", and returns it with an allocator past its objects.
func stagedTestDoc(t *testing.T) (*document.Document, *writer.ObjAllocator) {
	t.Helper()
	data := buildTaggedTestPDF("BT /F1 12 Tf 72 700 Td (source text) Tj ET\n")
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc, writer.NewObjAllocator(5)
}

// TestAutoTagMarksTheStagedContent pins that AutoTag classifies and marks the
// content stream an earlier stage queued, not the document's copy it replaces.
func TestAutoTagMarksTheStagedContent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := stagedTestDoc(t)
	const staged = "BT /F1 24 Tf 72 700 Td (Staged Heading) Tj ET\n" +
		"BT /F1 12 Tf 72 670 Td (staged body text, longer than the heading) Tj ET\n"
	alloc.AllocAt(5, ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(staged))}, []byte(staged)))

	if _, err := AutoTag(ctx, alloc, doc); err != nil {
		t.Fatalf("AutoTag: %v", err)
	}

	page, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Fatal("AutoTag queued no page dictionary")
	}
	ref, ok := page["Contents"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("the page /Contents is %T, want a reference", page["Contents"])
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d is not a queued stream", ref.ObjectNum)
	}
	data, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the marked content: %v", err)
	}
	got := string(data)
	for _, want := range []string{"/H1 <</MCID 0>> BDC", "(Staged Heading)", "(staged body text, longer than the heading)"} {
		if !strings.Contains(got, want) {
			t.Errorf("the marked content %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "source text") {
		t.Errorf("the marked content %q is the document's, not the staged stream", got)
	}
}

// TestAutoTagDefersToStagedStructure pins that AutoTag judges the document as
// the allocator stages it: a catalog an earlier stage tagged, or content an
// earlier stage marked, leaves AutoTag queuing nothing.
func TestAutoTagDefersToStagedStructure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		stage func(alloc *writer.ObjAllocator)
	}{
		{
			name: "a tagged catalog",
			stage: func(alloc *writer.ObjAllocator) {
				alloc.AllocAt(1, ast.PdfDict{
					"Type":           ast.PdfName("Catalog"),
					"Pages":          ast.PdfIndirectRef{ObjectNum: 2},
					"StructTreeRoot": alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("StructTreeRoot")}),
				})
			},
		},
		{
			name: "marked content",
			stage: func(alloc *writer.ObjAllocator) {
				const marked = "/Artifact BMC BT /F1 12 Tf 72 700 Td (stamp) Tj ET EMC\n"
				alloc.AllocAt(5, ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(marked))}, []byte(marked)))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, alloc := stagedTestDoc(t)
			tc.stage(alloc)
			before := alloc.Writes()
			res, err := AutoTag(context.Background(), alloc, doc)
			if err != nil {
				t.Fatalf("AutoTag: %v", err)
			}
			if len(res.Descriptions) != 0 || alloc.Writes() != before {
				t.Errorf("AutoTag tagged the document again (%v, %d objects queued)", res.Descriptions, alloc.Writes()-before)
			}
		})
	}
}
