package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// attachmentPDF is a one-page document with a single embedded file reached
// through the catalog name tree, plus the catalog /AF array PDF/A-3 and
// Factur-X rely on.
func attachmentPDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(payload.txt) 5 0 R] >> >> /AF [5 0 R] >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /Filespec /F (payload.txt) /UF (payload.txt) /EF << /F 6 0 R >> >>\nendobj\n" +
		"6 0 obj\n<< /Length 5 >>\nstream\nhello\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

// embeddedNames reopens written bytes and returns the name-tree keys plus the
// /AF entry count.
func embeddedNames(t *testing.T, b []byte) ([]string, int) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(b), int64(len(b)), nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	af, _ := document.ResolveArray(ctx, doc, catalog.Dict["AF"])
	names, err := document.ResolveDict(ctx, doc, catalog.Dict["Names"])
	if err != nil || names == nil {
		return nil, len(af)
	}
	var entries []EFEntry
	collectSourceEmbeddedFiles(ctx, doc, names["EmbeddedFiles"], 0, &entries)
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.Name)
	}
	return keys, len(af)
}

// TestSplitMerge_KeepEmbeddedFiles pins that attachments survive both
// operations.
//
// They hang off the catalog name tree (ISO 32000-1, 7.7.4) and both operations
// build a fresh catalog, so every attachment used to be lost: all 150 facturx
// fixtures came out of a merge without the ZUGFeRD-invoice.xml that IS the
// invoice.
func TestSplitMerge_KeepEmbeddedFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, _ := newMutationDoc(t, attachmentPDF())
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}

	var split bytes.Buffer
	if err := Split(ctx, doc, pages, []int{1}, &split, "", false, CompactOptions{}, DefaultSplitBookmarkConfig(), nil); err != nil {
		t.Fatalf("Split: %v", err)
	}
	keys, afCount := embeddedNames(t, split.Bytes())
	if len(keys) != 1 || keys[0] != "payload.txt" {
		t.Errorf("split kept %v, want [payload.txt]", keys)
	}
	if afCount != 1 {
		t.Errorf("split /AF has %d entries, want 1", afCount)
	}

	doc2, _ := newMutationDoc(t, attachmentPDF())
	catalog2, _ := document.ResolveCatalog(ctx, doc2)
	pages2, _ := document.ResolvePages(ctx, doc2, catalog2)

	var merged bytes.Buffer
	err = Merge(ctx, []MergeSource{{Doc: doc, Pages: pages}, {Doc: doc2, Pages: pages2}},
		&merged, "", false, CompactOptions{}, DefaultMergeBookmarkConfig(), nil)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	keys, afCount = embeddedNames(t, merged.Bytes())
	if len(keys) != 2 {
		t.Fatalf("merge kept %v, want both payloads", keys)
	}
	// Keys in a name tree are unique (ISO 32000-1, 7.9.6), so the second copy
	// is renamed rather than dropped.
	if keys[0] == keys[1] {
		t.Errorf("merge wrote duplicate name-tree keys %v", keys)
	}
	if afCount != 2 {
		t.Errorf("merge /AF has %d entries, want 2", afCount)
	}
}

// TestUniqueEFName_SuffixesCollisions covers the renaming rule directly.
func TestUniqueEFName_SuffixesCollisions(t *testing.T) {
	used := make(map[string]int)
	if got := uniqueEFName(used, "a.xml"); got != "a.xml" {
		t.Errorf("first use = %q, want a.xml", got)
	}
	if got := uniqueEFName(used, "a.xml"); got != "a.xml (2)" {
		t.Errorf("second use = %q, want \"a.xml (2)\"", got)
	}
	if got := uniqueEFName(used, "a.xml"); got != "a.xml (3)" {
		t.Errorf("third use = %q, want \"a.xml (3)\"", got)
	}
	if got := uniqueEFName(used, "b.xml"); got != "b.xml" {
		t.Errorf("unrelated name = %q, want b.xml", got)
	}
}

// indirectKeyAttachmentPDF puts the /EmbeddedFiles name-tree key in its own
// object, which the spec permits for any object.
func indirectKeyAttachmentPDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [7 0 R 5 0 R] >> >> >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /Filespec /F (payload.txt) /UF (payload.txt) /EF << /F 6 0 R >> >>\nendobj\n" +
		"6 0 obj\n<< /Length 5 >>\nstream\nhello\nendstream\nendobj\n" +
		"7 0 obj\n(payload.txt)\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")
}

// TestCollectEmbeddedFiles_ResolvesIndirectKeys pins that an attachment whose
// name-tree key is an indirect reference is still found.
//
// A key may be indirect like any other object. Asserting the type directly
// skipped the entry, and because the collected set is what gets rewritten, both
// collectors turned that into DELETION: pdfjs issue18444.pdf lost
// quitecommands.xml on any attach, split or merge.
func TestCollectEmbeddedFiles_ResolvesIndirectKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := indirectKeyAttachmentPDF()
	doc, _ := newMutationDoc(t, data)
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	names, err := document.ResolveDict(ctx, doc, catalog.Dict["Names"])
	if err != nil || names == nil {
		t.Fatalf("resolving /Names: %v", err)
	}

	var viaSource []EFEntry
	collectSourceEmbeddedFiles(ctx, doc, names["EmbeddedFiles"], 0, &viaSource)
	if len(viaSource) != 1 || viaSource[0].Name != "payload.txt" {
		t.Errorf("collectSourceEmbeddedFiles found %v, want one payload.txt", viaSource)
	}

	alloc := writer.NewObjAllocator(20)
	var viaShared []EFEntry
	CollectEmbeddedFileEntries(ctx, alloc, doc, names["EmbeddedFiles"], 0, &viaShared)
	if len(viaShared) != 1 || viaShared[0].Name != "payload.txt" {
		t.Errorf("CollectEmbeddedFileEntries found %v, want one payload.txt", viaShared)
	}
}
