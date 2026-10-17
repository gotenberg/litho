package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestSignatureLockCovers checks the fields each action of ISO 32000-1, Table
// 256 locks, a named non-terminal field reaching its kids.
func TestSignatureLockCovers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		lock signatureLock
		name string
		want bool
	}{
		{signatureLock{action: "All"}, "anything", true},
		{signatureLock{action: "Include", names: []string{"group"}}, "group.first", true},
		{signatureLock{action: "Include", names: []string{"group"}}, "groupie", false},
		{signatureLock{action: "Exclude", names: []string{"group"}}, "group.first", false},
		{signatureLock{action: "Exclude", names: []string{"group"}}, "Signature1", true},
		{signatureLock{action: "Some"}, "anything", false},
	} {
		if got := tc.lock.covers(tc.name); got != tc.want {
			t.Errorf("%s %q covers %q: %v, want %v", tc.lock.action, tc.lock.names, tc.name, got, tc.want)
		}
	}
}

// TestSignatureLocks_ReadsThroughACycle reads the lock of a signed field below
// a /Kids array that two direct kids name again. The walk marks the array, so
// it ends at once with the one lock, named by the field's qualified name.
func TestSignatureLocks_ReadsThroughACycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := minimalPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	sig := alloc.Alloc(ast.PdfDict{
		"Type": ast.PdfName("Sig"),
		"Reference": ast.PdfArray{ast.PdfDict{
			"TransformMethod": ast.PdfName("FieldMDP"),
			"TransformParams": ast.PdfDict{"Action": ast.PdfName("Exclude"), "Fields": ast.PdfArray{ast.PdfString("group.first")}},
		}},
	})
	signedField := alloc.Alloc(ast.PdfDict{"FT": ast.PdfName("Sig"), "T": ast.PdfString("approval"), "V": sig})
	kids := alloc.Alloc(ast.PdfNull{})
	alloc.AllocAt(kids.ObjectNum, ast.PdfArray{
		ast.PdfDict{"T": ast.PdfString("x"), "Kids": kids},
		ast.PdfDict{"T": ast.PdfString("y"), "Kids": kids},
		signedField,
	})
	root := alloc.Alloc(ast.PdfDict{"T": ast.PdfString("group"), "Kids": kids})

	locks, err := signatureLocks(ctx, alloc, doc, ast.PdfArray{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 1 || locks[0].field != "group.approval" || locks[0].action != "Exclude" {
		t.Fatalf("locks %+v, want the Exclude lock of group.approval", locks)
	}
}
