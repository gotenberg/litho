package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// optionalContentPDF builds a one-page document with two optional content
// groups, one of them switched off, reached both from the catalog's
// /OCProperties and from the page's /Properties. The two paths name the same
// objects, which is what makes the switched-off group hide anything.
func optionalContentPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")

	offsets := make([]int, 8)
	write := func(num int, body string) {
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	const content = "/OC /MCvis BDC BT ET EMC /OC /MChid BDC BT ET EMC\n"
	write(1, "<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [6 0 R 7 0 R] "+
		"/D << /Order [6 0 R 7 0 R] /OFF [7 0 R] >> >> >>")
	write(2, "<< /Type /Pages /Count 1 /Kids [3 0 R] >>")
	write(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R "+
		"/Resources << /Properties << /MCvis 6 0 R /MChid 7 0 R >> >> >>")
	write(4, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content))
	write(6, "<< /Type /OCG /Name (Visible) >>")
	write(7, "<< /Type /OCG /Name (Hidden) >>")

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 8\n0000000000 65535 f \n")
	for i := 1; i <= 7; i++ {
		if offsets[i] == 0 {
			fmt.Fprintf(&buf, "0000000000 65535 f \n")
			continue
		}
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 8 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return buf.Bytes()
}

// ocgIdentity reports, across every page of data, how many /Properties groups
// the configuration switches off and how many are missing from /OCGs.
//
// Every page, not just the first: an earlier version of this test looked only
// at page one and passed while every later source's groups were still orphaned,
// because page one comes from the source whose catalog entry was carried.
func ocgIdentity(t *testing.T, data []byte) (off []ast.PdfName, absent []ast.PdfName) {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("no pages")
	}

	ocp, err := document.ResolveDict(ctx, doc, catalog.Dict["OCProperties"])
	if err != nil {
		t.Fatalf("resolving /OCProperties: %v", err)
	}
	def, err := document.ResolveDict(ctx, doc, ocp["D"])
	if err != nil {
		t.Fatalf("resolving /D: %v", err)
	}

	nums := func(obj ast.PdfObject) map[int]bool {
		out := map[int]bool{}
		arr, aerr := document.ResolveArray(ctx, doc, obj)
		if aerr != nil {
			return out
		}
		for _, e := range arr {
			if ref, ok := e.(ast.PdfIndirectRef); ok {
				out[ref.ObjectNum] = true
			}
		}
		return out
	}
	offSet, allSet := nums(def["OFF"]), nums(ocp["OCGs"])

	for _, page := range pages {
		resources, rerr := document.ResolveDict(ctx, doc, page.Dict["Resources"])
		if rerr != nil {
			t.Fatalf("resolving /Resources: %v", rerr)
		}
		properties, perr := document.ResolveDict(ctx, doc, resources["Properties"])
		if perr != nil {
			t.Fatalf("resolving /Properties: %v", perr)
		}
		for name, val := range properties {
			ref, ok := val.(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			if offSet[ref.ObjectNum] {
				off = append(off, name)
			}
			if !allSet[ref.ObjectNum] {
				absent = append(absent, name)
			}
		}
	}
	return off, absent
}

// TestMergeKeepsOptionalContentGroupIdentity pins that a merged document's
// pages and its optional content configuration still name the same groups.
//
// Merge copied the pages with one copier and then built a second one for the
// catalog entries. A copier memoises what it has already copied, so the fresh
// one duplicated every group reached through /OCProperties: the output held two
// disjoint sets, and the page's /Properties named neither the /OFF list nor
// /OCGs. ISO 32000-1, 8.11.2.3 decides visibility by matching the group a
// marked-content sequence names against the configuration, so every switched-off
// layer became visible; 8.11.4.3 requires /OCGs to hold all the document's
// groups, which it no longer did. Merging pdfjs issue9462.pdf revealed two
// hidden layers, 157 lines of text a viewer had been told not to show.
//
// litho's own reader cannot catch this: it extracts hidden optional content on
// purpose, so `litho text` reads the same before and after.
func TestMergeKeepsOptionalContentGroupIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := optionalContentPDF()

	if off, absent := ocgIdentity(t, src); len(off) != 1 || len(absent) != 0 {
		t.Fatalf("fixture is wrong: switched-off=%v absent-from-/OCGs=%v, want exactly one off and none absent", off, absent)
	}

	docs := make([]MergeSource, 2)
	for i := range docs {
		doc, err := document.NewDocument(ctx, bytes.NewReader(src), int64(len(src)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			t.Fatalf("ResolveCatalog: %v", err)
		}
		pages, err := document.ResolvePages(ctx, doc, catalog)
		if err != nil {
			t.Fatalf("ResolvePages: %v", err)
		}
		docs[i] = MergeSource{Doc: doc, Pages: pages}
	}

	var merged bytes.Buffer
	if err := Merge(ctx, docs, &merged, "", false, false, DefaultMergeBookmarkConfig(), nil); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	off, absent := ocgIdentity(t, merged.Bytes())
	if len(absent) != 0 {
		t.Errorf("merged pages name %v, which /OCProperties /OCGs does not list", absent)
	}
	// One hidden group per source: carrying only the first source's
	// /OCProperties left every later source's layers switched on.
	if len(off) != len(docs) {
		t.Errorf("merged pages have %v switched off, want one per source (%d)", off, len(docs))
	}
}
