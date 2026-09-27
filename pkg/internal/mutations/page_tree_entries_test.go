package mutations

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// bareTreeObjects are the objects of a two-page document whose page tree
// states short of what ISO 32000-1, 7.7.2 and 7.7.3 require, in every way a
// rewrite has to repair:
//
//   - the catalog (1) has no /Type;
//   - the root (2) has no /Type and no /Count, lists page 3 twice and names
//     object 9, which the file does not define;
//   - page 3 has no /Type and no /MediaBox, and inherits the root's empty
//     /Resources;
//   - node 4 has no /Type, a /Count of 7 and lists the root among its kids;
//   - page 5 names the root as its /Parent, although node 4 lists it, and its
//     /Resources names object 8, which the file does not define.
//
// The reader reads pages 3 and 5.
var bareTreeObjects = []string{
	"<< /Pages 2 0 R >>",
	"<< /Kids [3 0 R 4 0 R 3 0 R 9 0 R] /Resources << >> >>",
	"<< /Contents 6 0 R /Parent 2 0 R >>",
	"<< /Kids [5 0 R 2 0 R] /Parent 2 0 R /Count 7 >>",
	"<< /Type /Page /Contents 6 0 R /Parent 2 0 R /MediaBox [0 0 300 300] /Resources 8 0 R >>",
	"<< /Length 0 >>\nstream\n\nendstream",
}

// outputTree reads the page tree of a written document by hand, so what the
// assertions see is what the file states, not what the reader's leniency
// makes of it.
type outputTree struct {
	t   *testing.T
	doc *document.Document
}

func openOutputTree(t *testing.T, data []byte) outputTree {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v\n%s", err, data)
	}
	return outputTree{t: t, doc: doc}
}

// dict resolves obj to a dictionary, failing the test when it is none.
func (o outputTree) dict(obj ast.PdfObject) ast.PdfDict {
	o.t.Helper()
	d, err := document.ResolveDict(context.Background(), o.doc, obj)
	if err != nil || d == nil {
		o.t.Fatalf("%v is no dictionary (%v)", obj, err)
	}
	return d
}

func (o outputTree) catalog() ast.PdfDict {
	o.t.Helper()
	return o.dict(o.doc.Index.Trailer["Root"])
}

func (o outputTree) root() (ast.PdfIndirectRef, ast.PdfDict) {
	o.t.Helper()
	ref, ok := o.catalog()["Pages"].(ast.PdfIndirectRef)
	if !ok {
		o.t.Fatalf("the catalog names its root as %v, want a reference", o.catalog()["Pages"])
	}
	return ref, o.dict(ref)
}

// kids returns the /Kids of node, each a reference.
func (o outputTree) kids(node ast.PdfDict) []ast.PdfIndirectRef {
	o.t.Helper()
	arr, ok := node["Kids"].(ast.PdfArray)
	if !ok {
		o.t.Fatalf("/Kids is %v, want an array", node["Kids"])
	}
	refs := make([]ast.PdfIndirectRef, len(arr))
	for i, k := range arr {
		ref, isRef := k.(ast.PdfIndirectRef)
		if !isRef {
			o.t.Fatalf("/Kids holds %v, want references only", k)
		}
		refs[i] = ref
	}
	return refs
}

func (o outputTree) pageCount() int {
	o.t.Helper()
	catalog, err := document.ResolveCatalog(context.Background(), o.doc)
	if err != nil {
		o.t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(context.Background(), o.doc, catalog)
	if err != nil {
		o.t.Fatalf("ResolvePages: %v", err)
	}
	return len(pages)
}

// TestFullRewrite_WritesThePageTreeTheReaderReads pins that a full rewrite
// writes every entry ISO 32000-1 requires of the catalog, the page tree nodes
// and the pages, deriving each from the tree the reader walks, and drops from
// /Kids what the reader passes over.
//
// # Corpus evidence
//
//   - pdfjs issue9105_other.pdf: its catalog, root and page carry no /Type,
//     its root no /Count and its page no /MediaBox, and the rewrite wrote them
//     so, which qpdf warned about four times.
//   - pdfjs Pages-tree-refs.pdf: a node lists an ancestor, and the rewrite
//     wrote the cycle, which qpdf refuses.
func TestFullRewrite_WritesThePageTreeTheReaderReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc := attachDoc(t, bareTreeObjects...)
	var out bytes.Buffer
	if err := FullRewrite(ctx, &out, FullRewriteConfig{Doc: doc}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	o := openOutputTree(t, out.Bytes())

	if typ := o.catalog()["Type"]; typ != ast.PdfName("Catalog") {
		t.Errorf("catalog /Type = %v, want /Catalog", typ)
	}
	rootRef, root := o.root()
	if root["Type"] != ast.PdfName("Pages") || root["Count"] != ast.PdfInt(2) {
		t.Errorf("root /Type %v /Count %v, want /Pages and 2", root["Type"], root["Count"])
	}
	if _, has := root["Parent"]; has {
		t.Errorf("root carries /Parent %v, which Table 29 prohibits", root["Parent"])
	}
	kids := o.kids(root)
	if len(kids) != 2 {
		t.Fatalf("root lists %v, want page 3 and node 4 once each", kids)
	}
	page3, node4 := o.dict(kids[0]), o.dict(kids[1])
	if page3["Type"] != ast.PdfName("Page") || page3["Parent"] != rootRef {
		t.Errorf("page 3 /Type %v /Parent %v, want /Page and %v", page3["Type"], page3["Parent"], rootRef)
	}
	if box, _ := page3["MediaBox"].(ast.PdfArray); !slices.Equal(box, letterMediaBox()) {
		t.Errorf("page 3 /MediaBox = %v, want %v", page3["MediaBox"], letterMediaBox())
	}
	if _, has := page3["Resources"]; has {
		t.Errorf("page 3 states /Resources %v, where it inherits the root's", page3["Resources"])
	}
	if node4["Type"] != ast.PdfName("Pages") || node4["Count"] != ast.PdfInt(1) || node4["Parent"] != rootRef {
		t.Errorf("node 4 /Type %v /Count %v /Parent %v, want /Pages, 1 and %v", node4["Type"], node4["Count"], node4["Parent"], rootRef)
	}
	grandKids := o.kids(node4)
	if len(grandKids) != 1 {
		t.Fatalf("node 4 lists %v, want page 5 alone", grandKids)
	}
	page5 := o.dict(grandKids[0])
	if page5["Parent"] != kids[1] {
		t.Errorf("page 5 /Parent = %v, want node 4 at %v", page5["Parent"], kids[1])
	}
	if res, isDict := page5["Resources"].(ast.PdfDict); !isDict || len(res) != 0 {
		t.Errorf("page 5 /Resources = %v, want an empty dictionary", page5["Resources"])
	}
	if got := o.pageCount(); got != 2 {
		t.Errorf("the output reads as %d pages, want the 2 the source reads as", got)
	}
}

// TestFullRewrite_KeepsTheEditsWhileCompletingTheTree is the clobber guard of
// the completion: the objects it completes are the ones the overrides put in
// the output, and every entry they carry survives.
func TestFullRewrite_KeepsTheEditsWhileCompletingTheTree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc := attachDoc(t, bareTreeObjects...)
	var out bytes.Buffer
	if err := FullRewrite(ctx, &out, FullRewriteConfig{Doc: doc, Overrides: map[int]ast.PdfObject{
		1: ast.PdfDict{"Pages": ast.PdfIndirectRef{ObjectNum: 2}, "Lang": ast.PdfString("fr")},
		3: ast.PdfDict{"Contents": ast.PdfIndirectRef{ObjectNum: 6}, "Parent": ast.PdfIndirectRef{ObjectNum: 2}, "Rotate": ast.PdfInt(90)},
	}}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	o := openOutputTree(t, out.Bytes())
	catalog := o.catalog()
	if catalog["Lang"] != ast.PdfString("fr") || catalog["Type"] != ast.PdfName("Catalog") {
		t.Errorf("catalog = %v, want the edit's /Lang (fr) and /Type /Catalog", catalog)
	}
	_, root := o.root()
	page3 := o.dict(o.kids(root)[0])
	if page3["Rotate"] != ast.PdfInt(90) || page3["Type"] != ast.PdfName("Page") {
		t.Errorf("page 3 = %v, want the edit's /Rotate 90 and /Type /Page", page3)
	}
}

// TestFullRewrite_GivesARootReadAsNoNodeOne pins that a catalog naming a page
// as its page tree root gets a node listing it, and one naming an object the
// reader reads as neither a node nor a page gets an empty node, the object
// itself left as it is.
//
// ISO 32000-1, 7.7.2, Table 28 makes /Pages the root of the page tree, a node
// (7.7.3.2). The reader reads the page in the first case and no page in the
// second, and the output reads the same.
func TestFullRewrite_GivesARootReadAsNoNodeOne(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		root  string
		pages int
	}{
		{name: "a page", root: "<< /Type /Page /MediaBox [0 0 100 100] /Contents 3 0 R >>", pages: 1},
		{name: "an action", root: "<< /S /JavaScript /JS (app.alert\\(1\\)) >>", pages: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := attachDoc(t,
				"<< /Type /Catalog /Pages 2 0 R /OpenAction 2 0 R >>",
				tc.root,
				"<< /Length 0 >>\nstream\n\nendstream",
			)
			var out bytes.Buffer
			if err := FullRewrite(ctx, &out, FullRewriteConfig{Doc: doc}); err != nil {
				t.Fatalf("FullRewrite: %v", err)
			}
			o := openOutputTree(t, out.Bytes())
			rootRef, root := o.root()
			if root["Type"] != ast.PdfName("Pages") || root["Count"] != ast.PdfInt(tc.pages) {
				t.Errorf("root /Type %v /Count %v, want /Pages and %d", root["Type"], root["Count"], tc.pages)
			}
			kids := o.kids(root)
			if len(kids) != tc.pages {
				t.Fatalf("root lists %v, want %d kids", kids, tc.pages)
			}
			if tc.pages == 1 {
				if page := o.dict(kids[0]); page["Parent"] != rootRef {
					t.Errorf("the page names %v as its /Parent, want %v", page["Parent"], rootRef)
				}
			}
			if action := o.dict(o.catalog()["OpenAction"]); action["Type"] != nil && tc.pages == 0 {
				t.Errorf("the action the source named as its root gained /Type %v", action["Type"])
			}
			if got := o.pageCount(); got != tc.pages {
				t.Errorf("the output reads as %d pages, want %d", got, tc.pages)
			}
		})
	}
}

// TestCompleteWrittenPageTree_CompletesOnlyWhatTheUpdateWrites pins that an
// update's completion writes the entries a page it writes lacks, keeping the
// edit, and writes nothing the update does not already write.
func TestCompleteWrittenPageTree_CompletesOnlyWhatTheUpdateWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc := attachDoc(t, bareTreeObjects...)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	alloc.AllocAt(3, ast.PdfDict{"Contents": ast.PdfIndirectRef{ObjectNum: 6}, "Parent": ast.PdfIndirectRef{ObjectNum: 2}, "Rotate": ast.PdfInt(90)})
	if err := CompleteWrittenPageTree(ctx, alloc, doc); err != nil {
		t.Fatalf("CompleteWrittenPageTree: %v", err)
	}
	page := pendingDictAt(t, alloc, 3)
	if page["Type"] != ast.PdfName("Page") || page["Rotate"] != ast.PdfInt(90) {
		t.Errorf("page 3 = %v, want /Type /Page beside the edit's /Rotate 90", page)
	}
	if box, _ := page["MediaBox"].(ast.PdfArray); !slices.Equal(box, letterMediaBox()) {
		t.Errorf("page 3 /MediaBox = %v, want %v", page["MediaBox"], letterMediaBox())
	}
	if got := len(alloc.Pending()); got != 1 {
		t.Errorf("the update writes %d objects, want the one page it edited: %v", got, alloc.Pending())
	}

	untouched := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	untouched.AllocAt(7, ast.PdfDict{"Type": ast.PdfName("Font")})
	if err := CompleteWrittenPageTree(ctx, untouched, doc); err != nil {
		t.Fatalf("CompleteWrittenPageTree: %v", err)
	}
	if got := fmt.Sprint(untouched.Pending()); len(untouched.Pending()) != 1 {
		t.Errorf("an update writing no page tree object wrote %s", got)
	}
}
