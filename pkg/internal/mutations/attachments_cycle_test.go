package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// duplicateKidsAttachmentPDF names the same leaf twice in one /Kids array. A
// name tree is a strict tree (ISO 32000-1, 7.9.6), so this is malformed, but
// nothing stops a file from carrying it.
func duplicateKidsAttachmentPDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 7 0 R >> >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /Filespec /F (payload.txt) /UF (payload.txt) /EF << /F 6 0 R >> >>\nendobj\n" +
		"6 0 obj\n<< /Length 5 >>\nstream\nhello\nendstream\nendobj\n" +
		"7 0 obj\n<< /Kids [8 0 R 8 0 R] >>\nendobj\n" +
		"8 0 obj\n<< /Names [(payload.txt) 5 0 R] >>\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

// selfKidsAttachmentPDF points a /Kids entry back at its own node.
func selfKidsAttachmentPDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 7 0 R >> >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"7 0 obj\n<< /Kids [7 0 R] >>\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")
}

// embeddedFilesNode returns the /EmbeddedFiles name-tree root of data.
func embeddedFilesNode(
	t *testing.T,
	data []byte,
) (context.Context, *document.Document, *writer.ObjAllocator, ast.PdfObject) {
	t.Helper()
	ctx := context.Background()
	doc, alloc := newMutationDoc(t, data)
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	names, err := document.ResolveDict(ctx, doc, catalog.Dict["Names"])
	if err != nil || names == nil {
		t.Fatalf("resolving /Names: %v", err)
	}
	return ctx, doc, alloc, names["EmbeddedFiles"]
}

// TestCollectEmbeddedFiles_AKidNamedTwiceIsWalkedOnce pins the answer, not
// merely termination.
//
// Both collectors feed a rebuilt name tree, so walking one leaf twice appends
// it twice and [uniqueEFName] renames the copy to "payload.txt (2)", handing
// the caller an attachment the document never carried. The depth bound cannot
// catch this: the tree is two levels deep, well inside it.
func TestCollectEmbeddedFiles_AKidNamedTwiceIsWalkedOnce(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc, efObj := embeddedFilesNode(t, duplicateKidsAttachmentPDF())

	var viaSource []EFEntry
	collectSourceEmbeddedFiles(ctx, doc, efObj, 0, &viaSource)
	if len(viaSource) != 1 {
		t.Errorf("collectSourceEmbeddedFiles found %d entries %v, want exactly one", len(viaSource), viaSource)
	} else if viaSource[0].Name != "payload.txt" {
		t.Errorf("collectSourceEmbeddedFiles name = %q, want payload.txt", viaSource[0].Name)
	}

	var viaShared []EFEntry
	CollectEmbeddedFileEntries(ctx, alloc, doc, efObj, 0, &viaShared)
	if len(viaShared) != 1 {
		t.Errorf("CollectEmbeddedFileEntries found %d entries %v, want exactly one", len(viaShared), viaShared)
	} else if viaShared[0].Name != "payload.txt" {
		t.Errorf("CollectEmbeddedFileEntries name = %q, want payload.txt", viaShared[0].Name)
	}
}

// TestCollectEmbeddedFiles_SelfReferencingKidsTerminates pins termination only.
// A node whose /Kids names itself carries no /Names, so the guarded and the
// unguarded walk both come back empty; this would not have failed before the
// visited set was added.
func TestCollectEmbeddedFiles_SelfReferencingKidsTerminates(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc, efObj := embeddedFilesNode(t, selfKidsAttachmentPDF())

	var viaSource []EFEntry
	collectSourceEmbeddedFiles(ctx, doc, efObj, 0, &viaSource)
	if len(viaSource) != 0 {
		t.Errorf("collectSourceEmbeddedFiles found %v, want none", viaSource)
	}

	var viaShared []EFEntry
	CollectEmbeddedFileEntries(ctx, alloc, doc, efObj, 0, &viaShared)
	if len(viaShared) != 0 {
		t.Errorf("CollectEmbeddedFileEntries found %v, want none", viaShared)
	}
}
