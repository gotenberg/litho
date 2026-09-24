package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

const metadataFixtureXMP = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
	`<x:xmpmeta xmlns:x="adobe:ns:meta/"></x:xmpmeta><?xpacket end="w"?>`

// metadataFixture builds a one-page document carrying an /Info dictionary and a
// catalog /Metadata stream as requested, so each branch of [ClearMetadata] gets
// a document shaped for it. Object 1 is the catalog and object 4 the /Info dict.
func metadataFixture(t *testing.T, withInfo, withMetadata bool) *document.Document {
	t.Helper()
	ctx := context.Background()

	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if withMetadata {
		catalog = "<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>"
	}

	data := "%PDF-1.7\n" +
		"1 0 obj\n" + catalog + "\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n"
	if withInfo {
		data += "4 0 obj\n<< /Title (Secret Title) /Author (Someone) >>\nendobj\n"
	}
	if withMetadata {
		data += fmt.Sprintf("5 0 obj\n<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream\nendobj\n",
			len(metadataFixtureXMP), metadataFixtureXMP)
	}
	trailer := "trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n"
	if withInfo {
		trailer = "trailer\n<< /Size 6 /Root 1 0 R /Info 4 0 R >>\n%%EOF\n"
	}
	data += trailer

	raw := []byte(data)
	doc, err := document.NewDocument(ctx, bytes.NewReader(raw), int64(len(raw)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// pendingDictAt returns the dictionary queued at objNum, failing when the
// allocator holds something else: asserting on a dict that was never written
// back would pass for the wrong reason.
func pendingDictAt(t *testing.T, alloc *writer.ObjAllocator, objNum int) ast.PdfDict {
	t.Helper()

	obj, ok := alloc.Pending()[objNum]
	if !ok {
		t.Fatalf("object %d was not written back", objNum)
	}
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d = %T, want a dictionary", objNum, obj)
	}
	return dict
}

func TestClearMetadata_EmptiesTheInfoDictionary(t *testing.T) {
	doc := metadataFixture(t, true, true)
	alloc := writer.NewObjAllocator(20)

	if err := ClearMetadata(context.Background(), alloc, doc); err != nil {
		t.Fatalf("ClearMetadata: %v", err)
	}

	info := pendingDictAt(t, alloc, 4)
	if len(info) != 0 {
		t.Errorf("/Info = %v, want an empty dictionary", info)
	}
}

func TestClearMetadata_RemovesTheCatalogMetadataStream(t *testing.T) {
	doc := metadataFixture(t, true, true)
	alloc := writer.NewObjAllocator(20)

	if err := ClearMetadata(context.Background(), alloc, doc); err != nil {
		t.Fatalf("ClearMetadata: %v", err)
	}

	catalog := pendingDictAt(t, alloc, 1)
	if _, ok := catalog["Metadata"]; ok {
		t.Error("the catalog still carries /Metadata")
	}
	// The XMP stream is the only entry that should go: a catalog stripped of
	// /Type or /Pages would be unusable even though the metadata is gone.
	if _, ok := catalog["Pages"]; !ok {
		t.Error("/Pages was dropped from the catalog")
	}
	if _, ok := catalog["Type"]; !ok {
		t.Error("/Type was dropped from the catalog")
	}
}

func TestClearMetadata_LeavesTheCatalogAloneWhenThereIsNoMetadata(t *testing.T) {
	doc := metadataFixture(t, true, false)
	alloc := writer.NewObjAllocator(20)

	if err := ClearMetadata(context.Background(), alloc, doc); err != nil {
		t.Fatalf("ClearMetadata: %v", err)
	}

	if _, ok := alloc.Pending()[1]; ok {
		t.Error("the catalog was rewritten although it carried no /Metadata")
	}
	// The /Info clear still has to happen: this asserts the catalog is spared,
	// not that the call did nothing.
	if _, ok := alloc.Pending()[4]; !ok {
		t.Error("/Info was not emptied")
	}
}

// TestClearMetadata_CreatesAnEmptyInfoWhenTheDocumentHasNone pins that a
// document with no /Info gains one. That is the same idiom [SetMetadata] uses
// and documents ("creates or replaces the /Info dictionary"), so this pins the
// behaviour rather than endorsing it: a function contracted to remove metadata
// adds a trailer key that was not there.
func TestClearMetadata_CreatesAnEmptyInfoWhenTheDocumentHasNone(t *testing.T) {
	doc := metadataFixture(t, false, true)
	alloc := writer.NewObjAllocator(20)

	if _, ok := doc.Index.Trailer["Info"]; ok {
		t.Fatal("the fixture already carries /Info, so this branch is untested")
	}

	if err := ClearMetadata(context.Background(), alloc, doc); err != nil {
		t.Fatalf("ClearMetadata: %v", err)
	}

	ref, ok := doc.Index.Trailer["Info"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("trailer /Info = %T, want an indirect reference", doc.Index.Trailer["Info"])
	}
	if info := pendingDictAt(t, alloc, ref.ObjectNum); len(info) != 0 {
		t.Errorf("/Info = %v, want an empty dictionary", info)
	}
}

// catalogLessFixture builds a document holding no /Type /Catalog object at all.
// Deleting the trailer /Root is not enough to reach [ClearMetadata]'s refusal
// paths: [CatalogRef] recovers by scanning the object table, so a fixture that
// still contains a catalog resolves one either way. root is written into the
// trailer verbatim, and an empty root omits it, which selects the path: with no
// /Root at all CatalogRef reports !ok, while a /Root naming a missing object
// leaves the raw-reference fallback returning something that cannot resolve.
func catalogLessFixture(t *testing.T, root string) *document.Document {
	t.Helper()
	ctx := context.Background()

	data := "%PDF-1.7\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
		"4 0 obj\n<< /Title (Secret Title) >>\nendobj\n" +
		"trailer\n<< /Size 5 /Info 4 0 R " + root + ">>\n%%EOF\n"

	raw := []byte(data)
	doc, err := document.NewDocument(ctx, bytes.NewReader(raw), int64(len(raw)), nil)
	if err != nil {
		t.Skipf("a catalog-less document does not parse, so this refusal path is unreachable from a fixture: %v", err)
	}
	return doc
}

func TestClearMetadata_StopsWhenThereIsNoCatalogToEdit(t *testing.T) {
	doc := catalogLessFixture(t, "")
	alloc := writer.NewObjAllocator(20)

	if err := ClearMetadata(context.Background(), alloc, doc); err != nil {
		t.Fatalf("ClearMetadata: %v", err)
	}

	// The /Info clear precedes the catalog work, so it must still have happened:
	// this asserts the function returned early, not that it did nothing.
	if info := pendingDictAt(t, alloc, 4); len(info) != 0 {
		t.Errorf("/Info = %v, want an empty dictionary", info)
	}
}

func TestClearMetadata_StopsWhenTheCatalogCannotBeResolved(t *testing.T) {
	doc := catalogLessFixture(t, "/Root 9 0 R ")
	alloc := writer.NewObjAllocator(20)

	if err := ClearMetadata(context.Background(), alloc, doc); err != nil {
		t.Fatalf("ClearMetadata: %v", err)
	}

	if info := pendingDictAt(t, alloc, 4); len(info) != 0 {
		t.Errorf("/Info = %v, want an empty dictionary", info)
	}
	if _, ok := alloc.Pending()[9]; ok {
		t.Error("object 9 was written although it resolves to no catalog")
	}
}
