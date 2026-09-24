package mutations

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// documentLevelPDF carries the two page-independent catalog entries that split
// and merge used to drop: /Collection, which marks a PDF Portfolio, and
// /PronunciationLexicon, the document's PLS array.
//
// The portfolio also holds the embedded file its /Collection describes, because
// the incoherence is the point: rebuildEmbeddedFiles already carried the
// payload, so a split kept the contents of a container while denying it was
// one.
func documentLevelPDF() []byte {
	const pls = `<?xml version="1.0"?><lexicon version="1.0"><lexeme><grapheme>tomato</grapheme></lexeme></lexicon>`
	const data = "col,umns\n1,2\n"

	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Collection 7 0 R " +
		"/PronunciationLexicon [8 0 R] /Names 6 0 R /SpiderInfo 9 0 R >>\nendobj\n" +
		"9 0 obj\n<< /V 1.0 /C [] >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
		"4 0 obj\n<< /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /Filespec /F (part.csv) /UF (part.csv) /EF << /F 4 0 R >> >>\nendobj\n" +
		"6 0 obj\n<< /EmbeddedFiles << /Names [(part.csv) 5 0 R] >> >>\nendobj\n" +
		"7 0 obj\n<< /Type /Collection /View /D >>\nendobj\n" +
		"8 0 obj\n<< /Length " + strconv.Itoa(len(pls)) + " >>\nstream\n" + pls + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 10 /Root 1 0 R /ID [<0102> <0304>] >>\n%%EOF\n")
}

// TestSplitMerge_KeepDocumentLevelEntries pins /Collection and
// /PronunciationLexicon through both operations.
//
// Both describe the document rather than any page, so they satisfy the rule
// copyCatalogEntries states for itself, and neither needs the remapping that
// keeps /StructTreeRoot and /PageLabels out. Measured before the fix: a
// one-lexicon document came out of a split and a merge with none, and a
// portfolio came out keeping its embedded file while [pdf.Reader.IsPortfolio]
// answered false over it.
//
// The earlier survey could not have found these. It diffed the catalog keys
// real files carry, and no surveyed file carries either key, so only
// enumerating what a catalog MAY hold reaches them.
//
// # Corpus evidence
//
//   - All corpora, 120 files: the catalog-key survey found no file carrying
//     /Collection or /PronunciationLexicon.
func TestSplitMerge_KeepDocumentLevelEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	check := func(what string, b []byte) {
		t.Helper()
		catalog, _ := catalogKeysOf(t, b)
		for _, key := range []ast.PdfName{"Collection", "PronunciationLexicon", "SpiderInfo"} {
			if _, ok := catalog[key]; !ok {
				t.Errorf("%s dropped %s", what, key)
			}
		}
		// The payload the portfolio describes must still be there, so the
		// entry is not left pointing at nothing.
		if _, ok := catalog["Names"]; !ok {
			t.Errorf("%s dropped /Names, so /Collection describes no files", what)
		}
	}

	doc, _ := newMutationDoc(t, documentLevelPDF())
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}

	var split bytes.Buffer
	if err := Split(ctx, doc, pages, []int{1}, &split, "", false, CompactOptions{},
		DefaultSplitBookmarkConfig(), nil); err != nil {
		t.Fatalf("Split: %v", err)
	}
	check("split", split.Bytes())

	doc2, _ := newMutationDoc(t, documentLevelPDF())
	catalog2, _ := document.ResolveCatalog(ctx, doc2)
	pages2, _ := document.ResolvePages(ctx, doc2, catalog2)

	var merged bytes.Buffer
	if err := Merge(ctx, []MergeSource{{Doc: doc, Pages: pages}, {Doc: doc2, Pages: pages2}},
		&merged, "", false, CompactOptions{}, DefaultMergeBookmarkConfig(), nil); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	check("merge", merged.Bytes())
}

// TestSplitMerge_StillRefusePageKeyedEntries is the control against
// over-correcting the test above: adding document-level keys to keepKeys must
// not tempt anyone into adding page-keyed ones, which would describe pages the
// output does not have.
func TestSplitMerge_StillRefusePageKeyedEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := newMutationDoc(t, conformantPDF())
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}

	var split bytes.Buffer
	if err := Split(ctx, doc, pages, []int{1}, &split, "", false, CompactOptions{},
		DefaultSplitBookmarkConfig(), nil); err != nil {
		t.Fatalf("Split: %v", err)
	}
	out, _ := catalogKeysOf(t, split.Bytes())
	for _, key := range []ast.PdfName{"StructTreeRoot", "PageLabels"} {
		if _, ok := out[key]; ok {
			t.Errorf("split carried page-keyed %s without remapping it", key)
		}
	}
}
