package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// insertPageBytes inserts sourcePage of source at pos of target and returns the
// written output.
func insertPageBytes(ctx context.Context, t *testing.T, target, source []byte, pos, sourcePage int) []byte {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(target), int64(len(target)))
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	defer r.Close()
	src, err := Open(ctx, bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	defer src.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := w.InsertPages(pos, src, sourcePage); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// formRootNames returns the partial name of every root field the output's
// /AcroForm lists, and fails the test when a widget on a page belongs to a
// field the form does not reach.
func formRootNames(ctx context.Context, t *testing.T, data []byte) []string {
	t.Helper()
	listed, outside := formRoots(ctx, t, data)
	for _, widget := range outside {
		t.Errorf("%s, which /AcroForm /Fields does not list", widget)
	}
	return listed
}

// formRoots returns the partial name of every root field the output's
// /AcroForm lists, sorted, and describes each widget on a page whose field the
// form does not reach.
func formRoots(ctx context.Context, t *testing.T, data []byte) (listedNames, outside []string) {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	doc := r.doc
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	acroForm, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"])
	if err != nil || acroForm == nil {
		t.Fatalf("the output has no /AcroForm (err %v)", err)
	}
	roots, err := document.ResolveArray(ctx, doc, acroForm["Fields"])
	if err != nil {
		t.Fatalf("resolving /Fields: %v", err)
	}
	listed := make(map[int]bool, len(roots))
	for _, root := range roots {
		ref, ok := root.(ast.PdfIndirectRef)
		if !ok {
			t.Fatalf("/Fields entry %v is not a reference", root)
		}
		listed[ref.ObjectNum] = true
		dict, err := document.ResolveDict(ctx, doc, ref)
		if err != nil {
			t.Fatalf("resolving root field %d: %v", ref.ObjectNum, err)
		}
		name, _ := document.ResolveString(ctx, doc, dict["T"])
		listedNames = append(listedNames, document.DecodePdfString(name))
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	for i, page := range pages {
		annots, _ := document.ResolveArray(ctx, doc, page.Dict["Annots"])
		for _, annot := range annots {
			ref, ok := annot.(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			root := ref
			var rootDict ast.PdfDict
			for range maxFieldParentDepthForTest {
				dict, err := document.ResolveDict(ctx, doc, root)
				if err != nil {
					t.Fatalf("resolving field %d: %v", root.ObjectNum, err)
				}
				rootDict = dict
				parent, hasParent := dict["Parent"].(ast.PdfIndirectRef)
				if !hasParent {
					break
				}
				root = parent
			}
			if !listed[root.ObjectNum] {
				name, _ := document.ResolveString(ctx, doc, rootDict["T"])
				outside = append(outside, fmt.Sprintf("page %d: widget %d belongs to field %d %q",
					i+1, ref.ObjectNum, root.ObjectNum, document.DecodePdfString(name)))
			}
		}
	}
	slices.Sort(listedNames)
	return listedNames, outside
}

// fieldNames returns the names [Reader.FormFields] reads from data, in the
// order it gives them.
func fieldNames(ctx context.Context, t *testing.T, data []byte) []string {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	fields, err := r.FormFields(ctx)
	if err != nil {
		t.Fatalf("FormFields: %v", err)
	}
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	return names
}

// maxFieldParentDepthForTest bounds the test's own /Parent ascent.
const maxFieldParentDepthForTest = 32

// TestWriter_InsertPages_ListsCopiedFields pins that a page inserted with its
// widgets brings their fields into the target's interactive form.
//
// ISO 32000-1, 12.7.2 has /AcroForm /Fields list the document's root fields,
// and a widget whose field it does not reach is outside the form: qpdf reports
// it as "not reachable from /AcroForm". A copy of a field whose name the target
// already uses keeps the name, as a merge keeps it: fields with one fully
// qualified name are one field (12.7.3.2).
//
// # Corpus evidence
//
//   - litho: 9 fixtures carried a widget the form did not reach after a page
//     move, each reported by qpdf.
func TestWriter_InsertPages_ListsCopiedFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := formPDF()
	out := insertPageBytes(ctx, t, data, data, 1, 1)

	got := formRootNames(ctx, t, out)
	want := []string{"email", "email", "name", "name"}
	if !slices.Equal(got, want) {
		t.Errorf("root fields = %v, want %v", got, want)
	}
}

// TestWriter_InsertPages_LinksFieldsAsMergeDoes pins that inserting a page of a
// form into a document holding the same form reads as a merge of the two does:
// each name once, one field whose widgets are on both pages (ISO 32000-1,
// 12.7.3.2). The copy used to be renamed "name_2", a second field.
func TestWriter_InsertPages_LinksFieldsAsMergeDoes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := formPDF()
	inserted := insertPageBytes(ctx, t, data, data, 1, 1)

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	var merged bytes.Buffer
	if err := Merge(ctx, []*Reader{r, r}, &merged); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	got, want := fieldNames(ctx, t, inserted), fieldNames(ctx, t, merged.Bytes())
	if !slices.Equal(got, want) {
		t.Errorf("inserted form reads fields %v, a merge of the same pages reads %v", got, want)
	}
	if !slices.Equal(got, []string{"name", "email"}) {
		t.Errorf("fields = %v, want [name email]", got)
	}
}

// pageMovePDF is a two-page form: the field "name" has a widget on each page,
// and the field "solo" is merged with its only widget, on page 1.
func pageMovePDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R 8 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R 8 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [7 0 R] >>",
		"<< /FT /Tx /T (name) /V (John) /Kids [6 0 R 7 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [50 700 250 720] /F 4 /P 3 0 R >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [50 700 250 720] /F 4 /P 4 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (solo) /V (x) /Rect [50 650 250 670] /F 4 /P 3 0 R >>",
	})
}

// TestWriter_InsertPages_PageMoveKeepsFieldNames pins that moving the first
// page of a form to the end, an insertion of it then a deletion of the
// original, leaves every widget in the field it presented. The widget moved
// with the page used to become a field of its own, "name_2", while the one left
// on page 2 stayed "name".
func TestWriter_InsertPages_PageMoveKeepsFieldNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := pageMovePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := w.InsertPages(2, r, 1); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}
	if err := w.DeletePages(1); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	got := fieldNames(ctx, t, out.Bytes())
	slices.Sort(got)
	if !slices.Equal(got, []string{"name", "solo"}) {
		t.Errorf("fields after the page move = %v, want [name solo], the source's", got)
	}
	for _, name := range formRootNames(ctx, t, out.Bytes()) {
		if name != "name" && name != "solo" {
			t.Errorf("the form lists a root field %q the source does not have", name)
		}
	}
}

// signedFormPDF is a one-page form holding a signed signature field, an
// unsigned one and a text field, each merged with its widget.
func signedFormPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R] /SigFlags 3 >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (signed) /V 7 0 R /Rect [50 700 250 750] /F 4 /P 3 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (blank) /Rect [50 600 250 650] /F 4 /P 3 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /V (Paris) /Rect [50 500 250 520] /F 4 /P 3 0 R >>",
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached " +
			"/ByteRange [0 100 200 300] /Contents <30820001> >>",
	})
}

// TestWriter_InsertPages_DropsCopiedSignatures pins that a signed signature
// field on an inserted page is left out of the form, as a merge leaves it out.
//
// The signature's digest covers the bytes of the source file its /ByteRange
// names (ISO 32000-1, 12.8.1), none of which the new document holds, so a
// copied signature can never verify. Its widget stays on the page, drawing
// the appearance it had, outside the form, as a merge leaves it. An unsigned
// signature field signs nothing and is listed like any other field.
func TestWriter_InsertPages_DropsCopiedSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	target := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	})
	out := insertPageBytes(ctx, t, target, signedFormPDF(), 1, 1)

	listed, outside := formRoots(ctx, t, out)
	if !slices.Equal(listed, []string{"blank", "city"}) {
		t.Errorf("root fields = %v, want [blank city]: the signed field left out, the unsigned one kept", listed)
	}
	if len(outside) != 1 {
		t.Fatalf("widgets outside the form = %v, want the signed field's widget alone", outside)
	}
	if !strings.Contains(outside[0], `"signed"`) {
		t.Errorf("widget outside the form = %q, want the one of the field \"signed\"", outside[0])
	}
}

// TestWriter_InsertPages_ListsTheTargetsUnlistedFields pins that a target whose
// pages present widget fields with no /AcroForm keeps them inside the form the
// insertion creates, rather than falling outside it.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: presents 27 widget fields with no /AcroForm, and a
//     form listing only the copied fields left the rest unreachable.
func TestWriter_InsertPages_ListsTheTargetsUnlistedFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	target := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (own) /V (x) /Rect [50 700 150 720] /F 4 >>",
	})
	source := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /V (Paris) /Rect [50 700 150 720] /F 4 >>",
	})
	out := insertPageBytes(ctx, t, target, source, 1, 1)

	if got := formRootNames(ctx, t, out); !slices.Equal(got, []string{"city", "own"}) {
		t.Errorf("root fields = %v, want [city own]", got)
	}
}

// TestWriter_InsertPages_CreatesTheForm pins that inserting a page with a field
// into a document with no interactive form gives it one, carrying the source
// form's default appearance so the field can still be drawn (ISO 32000-1,
// 12.7.3.3).
func TestWriter_InsertPages_CreatesTheForm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	target := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	})
	source := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /DA (/Helv 9 Tf 0 g) " +
			"/DR << /Font << /Helv 5 0 R >> >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /V (Paris) /Rect [50 700 150 720] /F 4 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
	out := insertPageBytes(ctx, t, target, source, 1, 1)

	if got := formRootNames(ctx, t, out); !slices.Equal(got, []string{"city"}) {
		t.Errorf("root fields = %v, want [city]", got)
	}

	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	catalog, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	acroForm, err := document.ResolveDict(ctx, r.doc, catalog.Dict["AcroForm"])
	if err != nil || acroForm == nil {
		t.Fatalf("no /AcroForm: %v", err)
	}
	if _, ok := acroForm["DA"]; !ok {
		t.Error("the new /AcroForm lost the source form's /DA")
	}
	if _, ok := acroForm["DR"]; !ok {
		t.Error("the new /AcroForm lost the source form's /DR")
	}
}
