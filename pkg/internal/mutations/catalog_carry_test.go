package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// conformantPDF carries the catalog entries and trailer /ID that PDF/A
// requires of every conforming file.
func conformantPDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R /OutputIntents [6 0 R] " +
		"/OCProperties << /OCGs [] /D << >> >> /Lang (en-GB) >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /Metadata /Subtype /XML /Length 9 >>\nstream\n<x:xmpmeta/>\nendstream\nendobj\n" +
		"6 0 obj\n<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) >>\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R /ID [<0102> <0304>] >>\n%%EOF\n")
}

// catalogKeysOf reopens written bytes and reports which catalog entries and
// trailer keys survived.
func catalogKeysOf(t *testing.T, b []byte) (ast.PdfDict, ast.PdfDict) {
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
	return catalog.Dict, doc.Index.Trailer
}

// TestSplitMerge_KeepConformanceEntries pins the page-independent catalog
// entries and the trailer /ID that a conforming file cannot lose.
//
// A PDF/A-1a document veraPDF passed with zero failures came out of a split
// failing 6.7.2 (the XMP that PDF/A requires) and 6.1.3 (the trailer /ID),
// because both operations built a fresh catalog and trailer carrying neither.
// A PDF/A-1b file now survives both operations still conformant.
func TestSplitMerge_KeepConformanceEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	check := func(what string, b []byte) {
		t.Helper()
		catalog, trailer := catalogKeysOf(t, b)
		for _, key := range []ast.PdfName{"Metadata", "OutputIntents", "OCProperties", "Lang"} {
			if _, ok := catalog[key]; !ok {
				t.Errorf("%s dropped /%s", what, key)
			}
		}
		if _, ok := trailer["ID"]; !ok {
			t.Errorf("%s dropped the trailer /ID", what)
		}
		// Page-keyed entries stay out: copying them wholesale would describe
		// pages the output does not have.
		if _, ok := catalog["StructTreeRoot"]; ok {
			t.Errorf("%s carried /StructTreeRoot without remapping it", what)
		}
	}

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
	if err := Split(ctx, doc, pages, []int{1}, &split, "", false, false, DefaultSplitBookmarkConfig(), nil); err != nil {
		t.Fatalf("Split: %v", err)
	}
	check("split", split.Bytes())

	doc2, _ := newMutationDoc(t, conformantPDF())
	catalog2, _ := document.ResolveCatalog(ctx, doc2)
	pages2, _ := document.ResolvePages(ctx, doc2, catalog2)

	var merged bytes.Buffer
	if err := Merge(ctx, []MergeSource{{Doc: doc, Pages: pages}, {Doc: doc2, Pages: pages2}},
		&merged, "", false, false, DefaultMergeBookmarkConfig(), nil); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	check("merge", merged.Bytes())
}
