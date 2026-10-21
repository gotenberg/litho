package mutations

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// outputIntentFixture builds a document whose catalog's /OutputIntents is
// object 4, an array holding a direct GTS_PDFA1 intent, a GTS_PDFX intent at
// object 5 and a direct ISO_PDFE1 intent.
func outputIntentFixture(t *testing.T) *document.Document {
	t.Helper()
	data := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /OutputIntents 4 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
		"4 0 obj\n[<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (old) /DestOutputProfileRef << >> >> 5 0 R" +
		" << /Type /OutputIntent /S /ISO_PDFE1 /OutputConditionIdentifier (e) >>]\nendobj\n" +
		"5 0 obj\n<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (x) >>\nendobj\n" +
		"trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n"
	doc, err := document.NewDocument(context.Background(), bytes.NewReader([]byte(data)), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestSetOutputIntent_ReplacesInAnIndirectArray pins the edit of an
// /OutputIntents array written as an object of its own: the array object is
// rewritten in place, its GTS_PDFA1 intent replaced and the others kept.
func TestSetOutputIntent_ReplacesInAnIndirectArray(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := outputIntentFixture(t)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	res, err := SetOutputIntent(ctx, alloc, doc, OutputIntentSpec{
		Profile: []byte("profile"), Components: 4, Condition: "CGATS TR 001", Registry: "http://www.color.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "replaced" || !slices.Equal(res.Kept, []string{"GTS_PDFX", "ISO_PDFE1"}) {
		t.Errorf("result = %+v", res)
	}
	if _, touched := alloc.Pending()[1]; touched {
		t.Error("the catalog was rewritten though the array is an object of its own")
	}
	arr, ok := alloc.Pending()[4].(ast.PdfArray)
	if !ok || len(arr) != 3 {
		t.Fatalf("array = %v", alloc.Pending()[4])
	}
	pdfa, ok := arr[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("intent 0 = %T", arr[0])
	}
	if _, kept := pdfa["DestOutputProfileRef"]; kept {
		t.Error("the replaced intent keeps /DestOutputProfileRef")
	}
	if pdfa["S"] != ast.PdfName("GTS_PDFA1") || pdfa["RegistryName"] == nil {
		t.Errorf("replaced intent = %v", pdfa)
	}
	ref, ok := pdfa["DestOutputProfile"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("profile = %v", pdfa["DestOutputProfile"])
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok || stream.Dict["N"] != ast.PdfInt(4) {
		t.Errorf("profile stream = %v", alloc.Pending()[ref.ObjectNum])
	}
	if arr[1] != (ast.PdfIndirectRef{ObjectNum: 5}) {
		t.Errorf("PDF/X entry = %v, want the reference kept", arr[1])
	}
	if _, touched := alloc.Pending()[5]; touched {
		t.Error("the PDF/X intent was rewritten")
	}
}

// TestSetOutputIntent_AddsWhenNone pins the catalog edit of a document with
// no output intent.
func TestSetOutputIntent_AddsWhenNone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := metadataFixture(t, false, false)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	res, err := SetOutputIntent(ctx, alloc, doc, OutputIntentSpec{Profile: []byte("p"), Components: 3, Condition: "Custom"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "added" || res.Kept != nil {
		t.Errorf("result = %+v", res)
	}
	arr, ok := pendingDictAt(t, alloc, 1)["OutputIntents"].(ast.PdfArray)
	if !ok || len(arr) != 1 {
		t.Fatalf("catalog intents = %v", arr)
	}
}
