package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// flattenAppearance is the appearance every widget of the tagged form fixtures
// draws: a filled box, so a flattened page paints something.
const flattenAppearance = "0 0 1 rg 0 0 100 20 re f"

// taggedFormPDF builds a tagged page presenting three widgets: a text field its
// own Form element encloses through a single /OBJR kid written directly, a
// check box whose Form element lists its /OBJR in an array, and a text field no
// structure element encloses. The page's text is tagged as a paragraph.
func taggedFormPDF() []byte {
	const pageContent = "/P <</MCID 0>> BDC BT /F1 12 Tf 50 740 Td (form) Tj ET EMC\n"
	return buildPDF([]string{
		// 1: catalog.
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R 6 0 R 7 0 R] >> " +
			"/StructTreeRoot 8 0 R /MarkInfo << /Marked true >> /Lang (en-US) " +
			"/ViewerPreferences << /DisplayDocTitle true >> >>",
		// 2: pages.
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// 3: page.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 13 0 R >> >> /Annots [5 0 R 6 0 R 7 0 R] " +
			"/StructParents 0 /Tabs /S >>",
		// 4: page content.
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageContent), pageContent),
		// 5: text field enclosed by the Form element 11.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /TU (Name) /V (Ada) " +
			"/Rect [50 700 150 720] /F 4 /AP << /N 12 0 R >> /StructParent 1 >>",
		// 6: check box enclosed by the Form element 14.
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (agree) /TU (Agree) /V /Yes /AS /Yes " +
			"/Rect [50 660 150 680] /F 4 /AP << /N << /Yes 12 0 R >> >> /StructParent 2 >>",
		// 7: text field outside the structure tree.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (loose) /TU (Loose) /V (x) " +
			"/Rect [50 620 150 640] /F 4 /AP << /N 12 0 R >> >>",
		// 8: structure tree root.
		"<< /Type /StructTreeRoot /K 9 0 R " +
			"/ParentTree << /Nums [0 [10 0 R] 1 11 0 R 2 14 0 R] >> /ParentTreeNextKey 3 >>",
		// 9: document element.
		"<< /Type /StructElem /S /Document /P 8 0 R /K [10 0 R 11 0 R 14 0 R] >>",
		// 10: paragraph.
		"<< /Type /StructElem /S /P /P 9 0 R /Pg 3 0 R /K 0 >>",
		// 11: Form element whose only kid is written directly.
		"<< /Type /StructElem /S /Form /P 9 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 5 0 R >> >>",
		// 12: the shared appearance.
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 20] /Length %d >>\nstream\n%s\nendstream",
			len(flattenAppearance), flattenAppearance),
		// 13: font.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		// 14: Form element listing its kid in an array.
		"<< /Type /StructElem /S /Form /P 9 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 6 0 R /Pg 3 0 R >>] >>",
	})
}

// uaRuleIDs returns the rule ids data fails at profile, as clause-tNN strings.
func uaRuleIDs(ctx context.Context, t *testing.T, data []byte, profile models.PDFUAProfile) []string {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	violations, err := r.ValidatePDFUAReport(ctx, profile)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport(%s): %v", profile, err)
	}
	var ids []string
	for _, v := range violations {
		id := fmt.Sprintf("%s-t%02d", v.Clause, v.TestNumber)
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// flattenBytes flattens data's form and returns the written output.
func flattenBytes(ctx context.Context, t *testing.T, data []byte) []byte {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	w.FlattenForm()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// TestWriter_FlattenForm_TaggedDocumentStaysTagged pins that flattening a tagged
// document's form leaves every flattened appearance either tagged or an
// artifact, and adds no PDF/UA violation.
//
// ISO 14289-1, 7.1 and ISO 14289-2, 8.2.2 require each piece of page content to
// be one or the other. The flattened appearance used to land in the page as
// bare content, and the Form elements kept object references to widgets no page
// carried any more.
//
// # Corpus evidence
//
//   - litho annotstruct_ua2, formstruct_ua2, idfields_ua2: flattening added ISO
//     14289-1, 7.1 test 3 and ISO 14289-2, 8.2.2 test 1, and veraPDF agreed.
func TestWriter_FlattenForm_TaggedDocumentStaysTagged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := taggedFormPDF()
	out := flattenBytes(ctx, t, in)

	for _, tc := range []struct {
		profile models.PDFUAProfile
		content string
	}{
		{models.PDFUA1, "7.1-t03"},
		{models.PDFUA2, "8.2.2-t01"},
	} {
		before := uaRuleIDs(ctx, t, in, tc.profile)
		after := uaRuleIDs(ctx, t, out, tc.profile)
		if slices.Contains(before, tc.content) {
			t.Fatalf("%s: the input already fails %s, so the check proves nothing", tc.profile, tc.content)
		}
		for _, id := range after {
			if !slices.Contains(before, id) {
				t.Errorf("%s: flattening added %s (before %v, after %v)", tc.profile, id, before, after)
			}
		}
	}

	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("Open flattened: %v", err)
	}
	defer r.Close()
	if fields, err := r.HasFormFields(ctx); err != nil || fields {
		t.Errorf("HasFormFields = %v, %v; want false after flattening", fields, err)
	}
}

// TestWriter_FlattenForm_DropsWidgetsThatDrawNothing pins that a widget with no
// appearance and no value to draw leaves with the form it belonged to.
//
// A viewer draws such a widget from nothing: it has no appearance stream, and
// the appearance ISO 32000-1, 12.7.3.3 has a viewer generate for a
// variable-text field shows its empty value, so the page looks the same without
// it. Keeping it left a form field in a document the flatten promised had none.
// A signature field is drawn only through its appearance stream, signed or not.
//
// # Corpus evidence
//
//   - litho filestruct_1b, sigcms_2b, typedreads_1b: each kept such a widget
//     after flattening, and litho still read it as a form field.
func TestWriter_FlattenForm_DropsWidgetsThatDrawNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R] /NeedAppearances true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 7 0 R] >>",
		// An empty text field with no appearance.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (empty) /Rect [50 700 150 720] /F 4 >>",
		// An unsigned signature field with no appearance.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (sig) /Rect [50 660 150 680] /F 132 >>",
		// A parent field whose only kid widget stores no appearance.
		"<< /FT /Tx /T (parent) /Kids [7 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [50 620 150 640] /F 4 >>",
	})
	out := flattenBytes(ctx, t, in)

	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("Open flattened: %v", err)
	}
	defer r.Close()
	fields, err := r.HasFormFields(ctx)
	if err != nil {
		t.Fatalf("HasFormFields: %v", err)
	}
	if fields {
		t.Error("a widget that draws nothing survived the flattening as a form field")
	}
}

// TestWriter_FlattenForm_EmptiedFormElementGoes pins that a Form element whose
// only widget draws nothing leaves the structure tree with it.
//
// ISO 14289-1, 7.18.4 test 2 requires a Form element without a PrintField Role
// attribute to hold exactly one widget reference, so an element emptied of its
// widget and kept fails it, where the input did not.
func TestWriter_FlattenForm_EmptiedFormElementGoes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const pageContent = "/P <</MCID 0>> BDC BT /F1 12 Tf 50 740 Td (form) Tj ET EMC\n"
	in := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> " +
			"/StructTreeRoot 6 0 R /MarkInfo << /Marked true >> /Lang (en-US) " +
			"/ViewerPreferences << /DisplayDocTitle true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 10 0 R >> >> /Annots [5 0 R] /StructParents 0 /Tabs /S >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageContent), pageContent),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (empty) /TU (Empty) " +
			"/Rect [50 700 150 720] /F 4 /StructParent 1 >>",
		"<< /Type /StructTreeRoot /K 7 0 R /ParentTree << /Nums [0 [8 0 R] 1 9 0 R] >> /ParentTreeNextKey 2 >>",
		"<< /Type /StructElem /S /Document /P 6 0 R /K [8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /P /P 7 0 R /Pg 3 0 R /K 0 >>",
		"<< /Type /StructElem /S /Form /P 7 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 5 0 R >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	})
	out := flattenBytes(ctx, t, in)

	before := uaRuleIDs(ctx, t, in, models.PDFUA1)
	after := uaRuleIDs(ctx, t, out, models.PDFUA1)
	for _, id := range after {
		if !slices.Contains(before, id) {
			t.Errorf("flattening added %s (before %v, after %v)", id, before, after)
		}
	}
}

// TestWriter_FlattenForm_DropsUndisplayedTaggedWidgets pins that a hidden or
// no-view widget leaves a tagged document with its form and its Form element,
// while a displayed widget beside them is still baked and tagged.
//
// Per ISO 32000-1, Table 165 a Hidden widget is neither displayed nor printed,
// and a NoView one is not displayed, so neither is baked into the page. Content
// that shows only when printed needs an optional content group whose /AS applies
// its print usage (ISO 32000-1, 8.11.4.4), which ISO 14289-1, 7.10 and ISO
// 14289-2, 8.7 forbid, so the printed no-view widget goes as well. Its Form
// element is left holding nothing and goes with it, since ISO 14289-1, 7.18.4
// test 2 fails an element that holds no widget reference.
func TestWriter_FlattenForm_DropsUndisplayedTaggedWidgets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const pageContent = "/P <</MCID 0>> BDC BT /F1 12 Tf 50 740 Td (form) Tj ET EMC\n"
	in := buildPDF([]string{
		// 1: catalog.
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R 6 0 R 7 0 R] >> " +
			"/StructTreeRoot 8 0 R /MarkInfo << /Marked true >> /Lang (en-US) " +
			"/ViewerPreferences << /DisplayDocTitle true >> >>",
		// 2: pages.
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// 3: page.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 13 0 R >> >> /Annots [5 0 R 6 0 R 7 0 R] " +
			"/StructParents 0 /Tabs /S >>",
		// 4: page content.
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageContent), pageContent),
		// 5: a hidden text field, its Print flag set as well.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (secret) /TU (Secret) /V (hidden) " +
			"/Rect [50 700 150 720] /F 6 /AP << /N 12 0 R >> /StructParent 1 >>",
		// 6: a text field shown only when printed.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (stamp) /TU (Stamp) /V (printed) " +
			"/Rect [50 660 150 680] /F 36 /AP << /N 12 0 R >> /StructParent 2 >>",
		// 7: a displayed text field.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /TU (Name) /V (Ada) " +
			"/Rect [50 620 150 640] /F 4 /AP << /N 12 0 R >> /StructParent 3 >>",
		// 8: structure tree root.
		"<< /Type /StructTreeRoot /K 9 0 R " +
			"/ParentTree << /Nums [0 [10 0 R] 1 11 0 R 2 14 0 R 3 15 0 R] >> /ParentTreeNextKey 4 >>",
		// 9: document element.
		"<< /Type /StructElem /S /Document /P 8 0 R /K [10 0 R 11 0 R 14 0 R 15 0 R] >>",
		// 10: paragraph.
		"<< /Type /StructElem /S /P /P 9 0 R /Pg 3 0 R /K 0 >>",
		// 11: the hidden widget's Form element.
		"<< /Type /StructElem /S /Form /P 9 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 5 0 R >> >>",
		// 12: the shared appearance.
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 20] /Length %d >>\nstream\n%s\nendstream",
			len(flattenAppearance), flattenAppearance),
		// 13: font.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		// 14: the printed-only widget's Form element.
		"<< /Type /StructElem /S /Form /P 9 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 6 0 R >>] >>",
		// 15: the displayed widget's Form element.
		"<< /Type /StructElem /S /Form /P 9 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 7 0 R >> >>",
	})
	out := flattenBytes(ctx, t, in)

	for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
		before := uaRuleIDs(ctx, t, in, profile)
		after := uaRuleIDs(ctx, t, out, profile)
		for _, id := range after {
			if !slices.Contains(before, id) {
				t.Errorf("%s: flattening added %s (before %v, after %v)", profile, id, before, after)
			}
		}
	}

	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("Open flattened: %v", err)
	}
	defer r.Close()
	if fields, err := r.HasFormFields(ctx); err != nil || fields {
		t.Errorf("HasFormFields = %v, %v; want false after flattening", fields, err)
	}

	catalog, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	root, err := document.ResolveDict(ctx, r.doc, catalog.Dict["StructTreeRoot"])
	if err != nil || root == nil {
		t.Fatalf("resolving the structure tree root: %v", err)
	}
	docElem, err := document.ResolveDict(ctx, r.doc, root["K"])
	if err != nil || docElem == nil {
		t.Fatalf("resolving the Document element: %v", err)
	}
	kids, err := document.ResolveArray(ctx, r.doc, docElem["K"])
	if err != nil {
		t.Fatalf("resolving the Document element's kids: %v", err)
	}
	forms := 0
	for _, kid := range kids {
		elem, err := document.ResolveDict(ctx, r.doc, kid)
		if err != nil || elem == nil {
			continue
		}
		if s, _ := document.ResolveName(ctx, r.doc, elem["S"]); s == "Form" {
			forms++
		}
	}
	if forms != 1 {
		t.Errorf("the Document element holds %d Form elements, want 1: the displayed field's", forms)
	}
}
