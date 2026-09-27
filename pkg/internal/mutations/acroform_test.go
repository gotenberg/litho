package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// widgetPage registers a page whose /Annots holds the given refs.
func widgetPage(alloc *writer.ObjAllocator, annots ...ast.PdfIndirectRef) ast.PdfIndirectRef {
	arr := make(ast.PdfArray, len(annots))
	for i, a := range annots {
		arr[i] = a
	}
	return alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Page"), "Annots": arr})
}

// TestCollectRootFields_NamesOnlySurvivingRoots pins that the rebuilt /Fields
// holds each surviving widget's ROOT field, once, and nothing else.
//
// /Fields is an array of the document's root fields (ISO 32000-1, 12.7.2).
// Collecting them from the copied widgets is what keeps split honest: a field
// whose widgets all landed on dropped pages is never named, so the array cannot
// dangle.
func TestCollectRootFields_NamesOnlySurvivingRoots(t *testing.T) {
	alloc := writer.NewObjAllocator(1)

	root := alloc.Alloc(ast.PdfDict{"T": ast.PdfString("group"), "FT": ast.PdfName("Tx")})
	kidA := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Parent": root})
	kidB := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Parent": root})
	lone := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Widget"), "FT": ast.PdfName("Btn")})
	link := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Link")})
	dropped := alloc.Alloc(ast.PdfDict{"T": ast.PdfString("gone"), "FT": ast.PdfName("Tx")})

	kept := []ast.PdfObject{
		widgetPage(alloc, kidA, link),
		widgetPage(alloc, kidB, lone),
	}

	got := collectRootFields(alloc, kept, true)
	if len(got) != 2 {
		t.Fatalf("/Fields = %v, want the shared root and the lone widget", got)
	}
	if got[0] != ast.PdfObject(root) {
		t.Errorf("first entry = %v, want the shared root %v", got[0], root)
	}
	if got[1] != ast.PdfObject(lone) {
		t.Errorf("second entry = %v, want the merged field-widget %v", got[1], lone)
	}
	for _, f := range got {
		if f == ast.PdfObject(link) {
			t.Error("a /Link annotation was named as a form field")
		}
		if f == ast.PdfObject(dropped) {
			t.Error("a field with no surviving widget was named, so /Fields dangles")
		}
	}
}

// TestRootFieldOf_StopsOnACycle pins that a /Parent loop terminates instead of
// hanging the writer.
func TestRootFieldOf_StopsOnACycle(t *testing.T) {
	alloc := writer.NewObjAllocator(1)

	a := alloc.Alloc(ast.PdfNull{})
	b := alloc.Alloc(ast.PdfNull{})
	alloc.AllocAt(a.ObjectNum, ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Parent": b})
	alloc.AllocAt(b.ObjectNum, ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Parent": a})

	done := make(chan bool, 1)
	go func() {
		_, ok := rootFieldOf(alloc, a)
		done <- ok
	}()
	if ok := <-done; ok {
		t.Error("a /Parent cycle reported a root field")
	}
}

// formPDF is a one-page document whose page carries a widget annotation whose
// /Parent is the root field named in /AcroForm /Fields.
func formPDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /DA (0 g) /NeedAppearances true >> >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [6 0 R] >>\nendobj\n" +
		"4 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"5 0 obj\n<< /FT /Tx /T (name) /V (value) >>\nendobj\n" +
		"6 0 obj\n<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [0 0 10 10] >>\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

// acroFormOf reopens written bytes and returns the /AcroForm field count plus
// whether the appearance defaults came across.
func acroFormOf(t *testing.T, b []byte) (int, bool) {
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
	form, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"])
	if err != nil || form == nil {
		return 0, false
	}
	fields, err := document.ResolveArray(ctx, doc, form["Fields"])
	if err != nil {
		return 0, false
	}
	_, hasDA := form["DA"]
	return len(fields), hasDA
}

// TestSplitMerge_KeepAcroForm pins that the interactive form survives both
// operations, so the widgets they copy are still fields.
//
// /AcroForm lives in the catalog (ISO 32000-1, 12.7.2) and both operations
// build a fresh one, so the form used to be dropped while its widget
// annotations came across with the pages.
//
// # Corpus evidence
//
//   - pdfjs 160F-2019.pdf: the output kept all 76 widgets and lost all 9
//     fields.
func TestSplitMerge_KeepAcroForm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, _ := newMutationDoc(t, formPDF())
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
	if n, hasDA := acroFormOf(t, split.Bytes()); n != 1 || !hasDA {
		t.Errorf("split: /Fields=%d hasDA=%v, want 1 and true", n, hasDA)
	}

	doc2, _ := newMutationDoc(t, formPDF())
	catalog2, _ := document.ResolveCatalog(ctx, doc2)
	pages2, _ := document.ResolvePages(ctx, doc2, catalog2)

	var merged bytes.Buffer
	err = Merge(ctx, []MergeSource{{Doc: doc, Pages: pages}, {Doc: doc2, Pages: pages2}},
		&merged, "", false, CompactOptions{}, DefaultMergeBookmarkConfig(), nil)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if n, hasDA := acroFormOf(t, merged.Bytes()); n != 2 || !hasDA {
		t.Errorf("merge: /Fields=%d hasDA=%v, want 2 and true", n, hasDA)
	}
}

// TestCollectRootFields_DropsSignedSignatureFields pins that a signed signature
// field is left behind and an unsigned one is kept.
//
// A signature's digest covers the entire file and its /ByteRange names the
// offsets it spans (ISO 32000-1, 12.8.1). Split and merge rewrite every byte,
// so a carried signature can never verify, and the output advertises a
// signature it cannot honour. An unsigned signature field carries no digest:
// it stays a field the output can still be signed in.
//
// # Corpus evidence
//
//   - litho docmdp_2b.pdf: veraPDF reported ISO 19005-2, 6.4.3 against a
//     split, a document that then advertised a signature it could not honour.
func TestCollectRootFields_DropsSignedSignatureFields(t *testing.T) {
	alloc := writer.NewObjAllocator(1)

	sigDict := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Sig"), "Contents": ast.PdfString("x")})
	sig := alloc.Alloc(ast.PdfDict{"FT": ast.PdfName("Sig"), "T": ast.PdfString("Signature1"), "V": sigDict})
	sigWidget := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Parent": sig})
	blank := alloc.Alloc(ast.PdfDict{"FT": ast.PdfName("Sig"), "T": ast.PdfString("Signature2")})
	blankWidget := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Parent": blank})
	text := alloc.Alloc(ast.PdfDict{"FT": ast.PdfName("Tx"), "T": ast.PdfString("name")})
	textWidget := alloc.Alloc(ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Parent": text})

	got := collectRootFields(alloc, []ast.PdfObject{widgetPage(alloc, sigWidget, blankWidget, textWidget)}, true)
	want := ast.PdfArray{blank, text}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("/Fields = %v, want the unsigned signature field and the text field %v", got, want)
	}
}

// TestDropsSignature_OnlySigned pins that a signature field counts as signed
// only when its /V is a signature dictionary (ISO 32000-1, 12.7.4.5), directly
// or through a reference, and that no other field does.
func TestDropsSignature_OnlySigned(t *testing.T) {
	t.Parallel()
	alloc := writer.NewObjAllocator(1)
	sigDict := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Sig"), "Contents": ast.PdfString("x")})

	for _, tc := range []struct {
		name  string
		field ast.PdfDict
		want  bool
	}{
		{"signed through a reference", ast.PdfDict{"FT": ast.PdfName("Sig"), "V": sigDict}, true},
		{"signed with a direct dictionary", ast.PdfDict{"FT": ast.PdfName("Sig"), "V": ast.PdfDict{"Type": ast.PdfName("Sig")}}, true},
		{"unsigned", ast.PdfDict{"FT": ast.PdfName("Sig"), "T": ast.PdfString("blank")}, false},
		{"a null value", ast.PdfDict{"FT": ast.PdfName("Sig"), "V": ast.PdfNull{}}, false},
		{"a text field", ast.PdfDict{"FT": ast.PdfName("Tx"), "V": ast.PdfDict{}}, false},
	} {
		ref := alloc.Alloc(tc.field)
		if got := dropsSignature(alloc, ref); got != tc.want {
			t.Errorf("%s: dropsSignature = %v, want %v", tc.name, got, tc.want)
		}
	}
}
