package mutations

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// certifiedFormObjs builds a two-page certified form. Page 1 presents:
//
//   - "author", a signed signature field merged with its widget, whose
//     signature /Perms /DocMDP names, drawing "Signed by Jane";
//   - "blank", an unsigned signature field;
//   - "city", a text field;
//   - "form1.witness", a signed signature field below "form1", a field that is
//     not a signature field, beside its text field "form1.note".
//
// /Perms /UR3 names a usage rights signature no field holds.
func certifiedFormObjs() []string {
	const ap = "BT /Helv 10 Tf 5 20 Td (Signed by Jane) Tj ET"
	const witness = "BT /Helv 10 Tf 5 20 Td (Witnessed by Joe) Tj ET"
	sig := func(extra string) string {
		return "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached " +
			"/ByteRange [0 100 200 300] /Contents <30820001> " + extra + ">>"
	}
	form := func(content string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 50] "+
			"/Resources << /Font << /Helv 9 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(content), content)
	}
	return []string{
		// 1: catalog.
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R 11 0 R] /SigFlags 3 >> " +
			"/Perms << /DocMDP 7 0 R /UR3 12 0 R >> >>",
		// 2: pages.
		"<< /Type /Pages /Kids [3 0 R 13 0 R] /Count 2 >>",
		// 3: page 1.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R 14 0 R 16 0 R] >>",
		// 4: the certifying signature field.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (author) /V 7 0 R /Rect [50 700 250 750] /F 4 " +
			"/P 3 0 R /AP << /N 8 0 R >> >>",
		// 5: an unsigned signature field.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (blank) /Rect [50 600 250 650] /F 4 /P 3 0 R >>",
		// 6: a text field.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /V (Paris) /Rect [50 500 250 520] /F 4 /P 3 0 R >>",
		// 7: the certification signature.
		sig("/Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P 2 /V /1.2 >> >>] "),
		// 8: its appearance.
		form(ap),
		// 9: font.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		// 10: the witness signature.
		sig(""),
		// 11: form1, a field that is not a signature field.
		"<< /T (form1) /Kids [14 0 R 16 0 R] >>",
		// 12: the usage rights signature.
		sig("/Reference [<< /Type /SigRef /TransformMethod /UR3 >>] "),
		// 13: page 2.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		// 14: form1.witness.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (witness) /Parent 11 0 R /V 10 0 R " +
			"/Rect [300 700 500 750] /F 4 /P 3 0 R /AP << /N 15 0 R >> >>",
		// 15: its appearance.
		form(witness),
		// 16: form1.note.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (note) /Parent 11 0 R /V (hi) " +
			"/Rect [300 500 500 520] /F 4 /P 3 0 R >>",
	}
}

// TestSignedSignatures_ListsEverySignature pins what [SignedSignatures]
// finds: the signed fields the form reaches, nested ones included, with the
// /Perms entry naming one, and the usage rights signature no field holds.
func TestSignedSignatures_ListsEverySignature(t *testing.T) {
	t.Parallel()
	ctx, doc, _ := setFormDoc(t, certifiedFormObjs())

	got := SignedSignatures(ctx, doc)
	want := []SignedSignature{
		{Field: "author", Perms: "DocMDP"},
		{Field: "form1.witness"},
		{Perms: "UR3"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("SignedSignatures = %v, want %v", got, want)
	}
}

// TestSignedSignatures_FindsAWidgetTheFormDoesNotList pins that a signed
// widget only a page presents is listed, as a reader that builds the form from
// the pages sees it.
func TestSignedSignatures_FindsAWidgetTheFormDoesNotList(t *testing.T) {
	t.Parallel()
	ctx, doc, _ := setFormDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (loose) /V 5 0 R /Rect [0 0 10 10] >>",
		"<< /Type /Sig /ByteRange [0 1 2 3] /Contents <00> >>",
	})
	if got, want := SignedSignatures(ctx, doc), []SignedSignature{{Field: "loose"}}; !slices.Equal(got, want) {
		t.Errorf("SignedSignatures = %v, want %v", got, want)
	}
}

// TestBakeSignaturesForRewrite_LeavesAnUnsignedDocumentAlone pins that a
// document with no signature gets no override: the rewrite of an unsigned
// document is the one it was before.
func TestBakeSignaturesForRewrite_LeavesAnUnsignedDocumentAlone(t *testing.T) {
	t.Parallel()
	ctx, doc, _ := setFormDoc(t, pageWidgetObjs("<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>"))

	got, err := BakeSignaturesForRewrite(ctx, doc, nil)
	if err != nil {
		t.Fatalf("BakeSignaturesForRewrite: %v", err)
	}
	if got != nil {
		t.Errorf("BakeSignaturesForRewrite returned %d overrides for an unsigned document, want none", len(got))
	}
}

// TestBakeSignaturesForRewrite_TakesEverySignatureOut pins the catalog, form
// and page a full rewrite writes for a signed document.
//
// ISO 32000-1, 12.8.1: each signature covers bytes the rewrite moves. The
// signed fields leave the form, a parent keeps its other kid, the unsigned
// signature field stays and keeps SignaturesExist while AppendOnly goes (Table
// 219), /Perms loses both signatures (Table 258), and the page draws the two
// visible signatures instead of presenting their widgets.
func TestBakeSignaturesForRewrite_TakesEverySignatureOut(t *testing.T) {
	t.Parallel()
	ctx, doc, _ := setFormDoc(t, certifiedFormObjs())

	overrides, err := BakeSignaturesForRewrite(ctx, doc, nil)
	if err != nil {
		t.Fatalf("BakeSignaturesForRewrite: %v", err)
	}
	catalog, ok := overrides[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("the catalog was not rewritten: %T", overrides[1])
	}
	if perms, has := catalog["Perms"]; has {
		t.Errorf("/Perms = %v, want it gone with both signatures", perms)
	}
	form, ok := catalog["AcroForm"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/AcroForm = %T, want the direct form rewritten", catalog["AcroForm"])
	}
	if got, want := form["Fields"], (ast.PdfArray{sigTestRef(5), sigTestRef(6), sigTestRef(11)}); !slices.Equal(got.(ast.PdfArray), want) {
		t.Errorf("/Fields = %v, want %v", got, want)
	}
	if got := form["SigFlags"]; got != ast.PdfInt(sigFlagSignaturesExist) {
		t.Errorf("/SigFlags = %v, want SignaturesExist alone", got)
	}
	parent, ok := overrides[11].(ast.PdfDict)
	if !ok {
		t.Fatalf("form1 was not rewritten: %T", overrides[11])
	}
	if got, want := parent["Kids"], (ast.PdfArray{sigTestRef(16)}); !slices.Equal(got.(ast.PdfArray), want) {
		t.Errorf("form1 /Kids = %v, want %v", got, want)
	}
	page, ok := overrides[3].(ast.PdfDict)
	if !ok {
		t.Fatalf("page 1 was not rewritten: %T", overrides[3])
	}
	if got, want := page["Annots"], (ast.PdfArray{sigTestRef(5), sigTestRef(6), sigTestRef(16)}); !slices.Equal(got.(ast.PdfArray), want) {
		t.Errorf("page 1 /Annots = %v, want %v", got, want)
	}
	if xobjects := len(pageXObjects(t, overrides, page)); xobjects != 2 {
		t.Errorf("page 1 names %d XObjects, want the two signature appearances", xobjects)
	}
}

// TestBakeSignedSignatures_PrunesANestedSignedField pins that a copy of pages
// takes a signed field out of the parent it sits below.
//
// /FT and /V are inheritable (ISO 32000-1, 12.7.3.1), so a signed field can sit
// below a field that is not one. The copier follows the page's other widgets
// up to that parent and took its /Kids whole, the signed field with them.
func TestBakeSignedSignatures_PrunesANestedSignedField(t *testing.T) {
	t.Parallel()
	ctx, doc, _ := setFormDoc(t, certifiedFormObjs())
	pages := readerPages(ctx, doc, mustCatalog(t, ctx, doc))

	overrides, err := bakeSignedSignatures(ctx, doc, pages[:1], pageCopyOverrides(pages[:1]), false)
	if err != nil {
		t.Fatalf("bakeSignedSignatures: %v", err)
	}
	parent, ok := overrides[11].(ast.PdfDict)
	if !ok {
		t.Fatalf("form1 was not rewritten: %T", overrides[11])
	}
	if got, want := parent["Kids"], (ast.PdfArray{sigTestRef(16)}); !slices.Equal(got.(ast.PdfArray), want) {
		t.Errorf("form1 /Kids = %v, want %v", got, want)
	}
}

// sigTestRef is the indirect reference to object num, generation 0.
func sigTestRef(num int) ast.PdfIndirectRef {
	return ast.PdfIndirectRef{ObjectNum: num}
}

// mustCatalog resolves doc's catalog.
func mustCatalog(t *testing.T, ctx context.Context, doc *document.Document) *document.Catalog {
	t.Helper()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	return catalog
}

// pageXObjects returns the /XObject resources of page, a dictionary the
// overrides hold, following references into them.
func pageXObjects(t *testing.T, overrides map[int]ast.PdfObject, page ast.PdfDict) ast.PdfDict {
	t.Helper()
	pending := func(obj ast.PdfObject) ast.PdfDict {
		if r, isRef := obj.(ast.PdfIndirectRef); isRef {
			obj = overrides[r.ObjectNum]
		}
		d, _ := obj.(ast.PdfDict)
		return d
	}
	return pending(pending(page["Resources"])["XObject"])
}
