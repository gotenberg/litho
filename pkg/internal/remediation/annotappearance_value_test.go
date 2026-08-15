package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// valuedFieldPDF builds a page whose only content is a text field carrying a
// value and no appearance stream, the shape of pdfjs annotation-tx.pdf and of
// the field in bug1847733.pdf that reads "In order to submit this form...".
// When empty is set the field has no value, so nothing is lost by giving it a
// blank appearance.
func valuedFieldPDF(empty bool) []byte {
	value := "(In order to submit this form)"
	if empty {
		value = "()"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (msg) /V " + value +
			" /Rect [10 700 400 722] /F 4 >>",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// TestAnnotAppearanceLeavesAValuedFieldAlone pins that a field a viewer draws
// from its own value is not given a blank appearance.
//
// A variable-text field with a value and no /AP is drawn from /V and /DA (ISO
// 32000-1, 12.7.3.3). Supplying a blank form XObject satisfies ISO 19005-2,
// 6.3.3 by replacing what the viewer would draw with nothing: pdfjs
// annotation-tx.pdf lost the 12 characters that were the whole page, and
// bug1847733.pdf lost the line "In order to submit this form, you should open it
// with Adobe Acrobat Reader." Drawing the value here needs the /DA font out of
// /DR and the field's own layout, so the violation is left as a residual the
// caller can see rather than answered with a document that lost its content.
func TestAnnotAppearanceLeavesAValuedFieldAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := valuedFieldPDF(false)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}
	if queued, ok := alloc.Pending()[4].(ast.PdfDict); ok {
		if _, gained := queued["AP"]; gained {
			t.Error("a text field with a value was given a blank appearance, replacing what the viewer draws with nothing")
		}
	}
}

// TestAnnotAppearanceStillFixesAnEmptyField pins the other side: a field that
// would draw nothing loses nothing to a blank appearance, so the fixer still
// supplies one and the ISO 19005-2, 6.3.3 violation is answered.
func TestAnnotAppearanceStillFixesAnEmptyField(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := valuedFieldPDF(true)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}
	queued, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("the empty field was not rewritten: %T", alloc.Pending()[4])
	}
	if _, gained := queued["AP"]; !gained {
		t.Error("an empty field draws nothing, so it should still receive the appearance 6.3.3 requires")
	}
}

// freeTextPDF builds a page whose only content is a free text annotation with
// no appearance stream, as pdfjs issue16021.pdf is. When empty is set the
// annotation carries no text, so a blank appearance costs nothing.
func freeTextPDF(empty bool) []byte {
	contents := "(Hello World)"
	if empty {
		contents = "()"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FreeText /Contents " + contents +
			" /DA (/Helv 10 Tf 0 g) /Rect [185 558 241 576] /F 4 >>",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// TestAnnotAppearanceLeavesAFreeTextAlone pins that a free text annotation
// carrying text keeps being drawn from it.
//
// A free text annotation displays the text in /Contents through /DA when it
// stores no appearance (ISO 32000-1, 12.5.6.6), the same machinery a
// variable-text field uses. Giving it a blank form to satisfy ISO 19005-2,
// 6.3.3 replaced that with nothing: pdfjs issue16021.pdf, whose whole page is
// one /FreeText reading "Hello World", came out of remediation empty.
func TestAnnotAppearanceLeavesAFreeTextAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := freeTextPDF(false)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}
	if queued, ok := alloc.Pending()[4].(ast.PdfDict); ok {
		if _, gained := queued["AP"]; gained {
			t.Error("a free text annotation carrying text was given a blank appearance, replacing what the viewer draws")
		}
	}
}

// TestAnnotAppearanceStillFixesAnEmptyFreeText pins that an annotation with no
// text still receives the appearance 6.3.3 requires.
func TestAnnotAppearanceStillFixesAnEmptyFreeText(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := freeTextPDF(true)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}
	queued, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("the empty annotation was not rewritten: %T", alloc.Pending()[4])
	}
	if _, gained := queued["AP"]; !gained {
		t.Error("an annotation with no text draws nothing, so it should still receive an appearance")
	}
}

// lineAnnotPDF builds a page with one line annotation carrying /Contents and no
// appearance. captioned controls /Cap, which is what decides whether that text
// is drawn (ISO 32000-1, Table 175).
func lineAnnotPDF(captioned bool) []byte {
	cap := "false"
	if captioned {
		cap = "true"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Line /L [10 10 200 10] /Contents ( Please note ) " +
			"/Cap " + cap + " /CP /Inline /Rect [10 0 200 30] /F 4 >>",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

func lineAnnotGainedAP(t *testing.T, captioned bool) bool {
	t.Helper()
	ctx := context.Background()
	data := lineAnnotPDF(captioned)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}
	queued, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		return false
	}
	_, gained := queued["AP"]
	return gained
}

// TestAnnotAppearanceLeavesACaptionedLineAlone pins that a line annotation whose
// caption is enabled keeps being drawn from /Contents.
//
// /Cap true replicates /Contents as a caption in the line's appearance (ISO
// 32000-1, 12.5.6.7). Supplying a blank form to satisfy ISO 19005-2, 6.3.3
// replaced it with nothing: pdfjs issue14203.pdf page 11 lost the caption
// "Please note".
func TestAnnotAppearanceLeavesACaptionedLineAlone(t *testing.T) {
	t.Parallel()

	if lineAnnotGainedAP(t, true) {
		t.Error("a captioned line annotation was given a blank appearance, dropping its caption")
	}
}

// TestAnnotAppearanceStillFixesAnUncaptionedLine pins the other side: without
// /Cap the /Contents is only an alternate description, nothing is drawn from it,
// and the annotation still needs the appearance 6.3.3 requires.
func TestAnnotAppearanceStillFixesAnUncaptionedLine(t *testing.T) {
	t.Parallel()

	if !lineAnnotGainedAP(t, false) {
		t.Error("an uncaptioned line draws no text, so it should still receive an appearance")
	}
}
