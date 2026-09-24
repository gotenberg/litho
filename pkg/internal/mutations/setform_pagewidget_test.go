package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// setFormDoc builds a one-page document from the given objects and returns
// everything SetFormField needs.
func setFormDoc(t *testing.T, objs []string) (context.Context, *document.Document, *writer.ObjAllocator) {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc, writer.NewObjAllocator(doc.HighestObjectNumber())
}

// pageWidgetObjs builds a page carrying one text widget that the interactive
// form does not list. catalog selects how the form is stated.
func pageWidgetObjs(catalog string) []string {
	return []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (username) /V (alice) /Rect [0 0 10 10] >>",
	}
}

// pendingAcroForm resolves the interactive form the allocator holds, following the
// catalog it may have rewritten.
func pendingAcroForm(t *testing.T, ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) ast.PdfDict {
	t.Helper()

	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		t.Fatal("trailer has no /Root")
	}
	catalog := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalog == nil {
		t.Fatal("cannot resolve the catalog")
	}
	afObj, ok := catalog["AcroForm"]
	if !ok {
		return nil
	}
	if ref, isRef := afObj.(ast.PdfIndirectRef); isRef {
		return ResolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	d, _ := afObj.(ast.PdfDict)
	return d
}

// TestSetFormFieldOnAPageOnlyWidget pins that a field reachable only from the
// page can be set, and that doing so states it in the interactive form.
//
// ISO 32000-1, 12.7.2 lists fields in /AcroForm and 12.5.6.19 puts the widget
// on the page. Producers write the widget without registering the field, and
// set-form refused such a field with "no AcroForm in catalog" even though
// `litho forms` listed it.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: carries 27 fields whose widgets sit only on the
//     page and no /AcroForm at all, so set-form refused every one of them with
//     "no AcroForm in catalog" even though `litho forms` listed them.
func TestSetFormFieldOnAPageOnlyWidget(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, pageWidgetObjs("<< /Type /Catalog /Pages 2 0 R >>"))
	if err := SetFormField(ctx, alloc, doc, "username", "bob"); err != nil {
		t.Fatalf("SetFormField: %v", err)
	}

	widget, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatal("the widget was not rewritten")
	}
	if got, _ := widget["V"].(ast.PdfString); string(got) != "bob" {
		t.Errorf("/V = %q, want %q", got, "bob")
	}

	form := pendingAcroForm(t, ctx, doc, alloc)
	if form == nil {
		t.Fatal("no interactive form was created for the newly set field")
	}
	fields, _ := form["Fields"].(ast.PdfArray)
	if len(fields) != 1 {
		t.Fatalf("/Fields has %d entries, want 1: the field must be registered", len(fields))
	}
	if ref, ok := fields[0].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 4 {
		t.Errorf("/Fields[0] = %v, want the widget at object 4", fields[0])
	}
	if na, _ := form["NeedAppearances"].(ast.PdfBool); !bool(na) {
		t.Error("/NeedAppearances is not set, so a viewer will not draw the new value")
	}
}

// TestSetFormFieldRegistersIntoAnExistingForm pins that an existing form is
// extended rather than replaced when the field was missing from it.
func TestSetFormFieldRegistersIntoAnExistingForm(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, pageWidgetObjs(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [] /DA (/Helv 0 Tf 0 g) >> >>"))
	if err := SetFormField(ctx, alloc, doc, "username", "bob"); err != nil {
		t.Fatalf("SetFormField: %v", err)
	}

	form := pendingAcroForm(t, ctx, doc, alloc)
	if form == nil {
		t.Fatal("the interactive form disappeared")
	}
	if fields, _ := form["Fields"].(ast.PdfArray); len(fields) != 1 {
		t.Errorf("/Fields has %d entries, want 1", len(fields))
	}
	if _, kept := form["DA"]; !kept {
		t.Error("the form lost its /DA: the existing dictionary must be extended, not replaced")
	}
}

// TestSetFormFieldOnARegisteredFieldIsUnchanged is the control: the ordinary
// path must still work and must not register the field a second time.
func TestSetFormFieldOnARegisteredFieldIsUnchanged(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, pageWidgetObjs(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>"))
	if err := SetFormField(ctx, alloc, doc, "username", "bob"); err != nil {
		t.Fatalf("SetFormField: %v", err)
	}

	widget, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatal("the field was not rewritten")
	}
	if got, _ := widget["V"].(ast.PdfString); string(got) != "bob" {
		t.Errorf("/V = %q, want %q", got, "bob")
	}
	if fields, _ := pendingAcroForm(t, ctx, doc, alloc)["Fields"].(ast.PdfArray); len(fields) != 1 {
		t.Errorf("/Fields has %d entries, want 1: the field was registered twice", len(fields))
	}
}

// TestSetFormFieldUsesTheQualifiedName pins that a widget presenting a field
// through /Parent is matched by the full name, not the leaf.
func TestSetFormFieldUsesTheQualifiedName(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [0 0 10 10] >>",
		"<< /FT /Tx /T (inner) /Parent 6 0 R >>",
		"<< /T (outer) >>",
	})

	if err := SetFormField(ctx, alloc, doc, "outer.inner", "bob"); err != nil {
		t.Fatalf("SetFormField: %v", err)
	}
	field, ok := alloc.Pending()[5].(ast.PdfDict)
	if !ok {
		t.Fatal("the named field was not rewritten")
	}
	if got, _ := field["V"].(ast.PdfString); string(got) != "bob" {
		t.Errorf("/V = %q, want %q", got, "bob")
	}

	// ISO 32000-1, 12.7.2: /Fields lists ROOT fields. Registering the named
	// node would rename the field, since a viewer qualifies from the root down.
	fields, _ := pendingAcroForm(t, ctx, doc, alloc)["Fields"].(ast.PdfArray)
	if len(fields) != 1 {
		t.Fatalf("/Fields has %d entries, want 1", len(fields))
	}
	if ref, ok := fields[0].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 6 {
		t.Errorf("/Fields[0] = %v, want the ROOT field at object 6, not the named node", fields[0])
	}
}

// TestSetFormFieldUnknownNameStillFails is the control: a name the document
// does not carry must be reported, not silently invented.
func TestSetFormFieldUnknownNameStillFails(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, pageWidgetObjs("<< /Type /Catalog /Pages 2 0 R >>"))
	err := SetFormField(ctx, alloc, doc, "not-a-field", "bob")
	if err == nil {
		t.Fatal("setting an absent field reported success")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want it to say the field was not found", err)
	}
}
