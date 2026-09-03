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

// buttonWidgetPDF builds a page with one widget whose field type lives on its
// parent field, the way every button in govdocs thread_400_400580.pdf does.
// annotExtra and parentFT let a caller move /FT or add /AS.
func buttonWidgetPDF(annotExtra, parentFT string) []byte {
	appearance := "q 1 0 0 RG 0 0 20 20 re S Q"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R " + annotExtra +
			" /Rect [10 700 30 720] /AP << /N 6 0 R >> >>",
		"<< " + parentFT + " /T (button) /Kids [4 0 R] >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length %d >>\nstream\n%s\nendstream",
			len(appearance), appearance),
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

// fixedAnnot runs the appearance fixer and returns the rewritten annotation.
func fixedAnnot(t *testing.T, data []byte) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}
	annot, _ := alloc.Pending()[4].(ast.PdfDict)
	return annot
}

// TestAnnotAppearanceWrapsAButtonWhoseTypeIsInherited pins that a widget whose
// /FT comes from its parent is recognised as a button.
//
// /FT is inheritable (ISO 32000-1, 12.7.3.1). Reading it off the annotation
// alone left /AP /N as a stream where ISO 19005-2, 6.3.3 test 3 requires a
// subdictionary, so govdocs thread_400_400580.pdf came out of remediation
// newly failing that rule, which veraPDF confirmed on the output.
func TestAnnotAppearanceWrapsAButtonWhoseTypeIsInherited(t *testing.T) {
	t.Parallel()

	annot := fixedAnnot(t, buttonWidgetPDF("", "/FT /Btn"))
	if annot == nil {
		t.Fatal("the annotation was not rewritten at all")
	}
	ap, ok := annot["AP"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/AP is %T, want a dict", annot["AP"])
	}
	states, ok := ap["N"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/AP /N is %T, want an appearance subdictionary", ap["N"])
	}
	if _, has := states["Off"]; !has {
		t.Errorf("/AP /N has states %v, want an /Off entry", states)
	}
	// ISO 32000-1, Table 168: /AS is required once /AP holds subdictionaries.
	if as, _ := annot["AS"].(ast.PdfName); as != "Off" {
		t.Errorf("/AS = %v, want /Off so a viewer can pick the state", annot["AS"])
	}
}

// TestAnnotAppearanceKeepsTheSelectedState pins that a state the annotation
// already selects is the one the wrapped stream is filed under, so the viewer
// keeps drawing what it drew before.
func TestAnnotAppearanceKeepsTheSelectedState(t *testing.T) {
	t.Parallel()

	annot := fixedAnnot(t, buttonWidgetPDF("/AS /On", "/FT /Btn"))
	if annot == nil {
		t.Fatal("the annotation was not rewritten at all")
	}
	ap, _ := annot["AP"].(ast.PdfDict)
	states, ok := ap["N"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/AP /N is %T, want an appearance subdictionary", ap["N"])
	}
	if _, has := states["On"]; !has {
		t.Errorf("/AP /N has states %v, want the selected /On entry", states)
	}
	if as, _ := annot["AS"].(ast.PdfName); as != "On" {
		t.Errorf("/AS = %v, want the state left as /On", annot["AS"])
	}
}

// TestAnnotAppearanceLeavesANonButtonStream is the control: 6.3.3 test 4
// requires a stream for every widget whose field type is not Btn, so a text
// field must not be wrapped.
func TestAnnotAppearanceLeavesANonButtonStream(t *testing.T) {
	t.Parallel()

	annot := fixedAnnot(t, buttonWidgetPDF("", "/FT /Tx"))
	if annot == nil {
		return // Nothing was rewritten, which is the outcome under test.
	}
	if ap, ok := annot["AP"].(ast.PdfDict); ok {
		if _, wrapped := ap["N"].(ast.PdfDict); wrapped {
			t.Errorf("a text field's /AP /N was wrapped into a state dict: %v", ap["N"])
		}
	}
}

// TestAnnotAppearanceWrapsACreatedButtonAppearance pins the sibling path: when
// the button has no /AP at all, the appearance the fixer creates must also be
// filed under a state, and the field type is still inherited.
//
// The two paths read /FT independently, so fixing only the one that rewrites an
// existing /AP would leave a button that has none producing the very shape
// ISO 19005-2, 6.3.3 test 3 forbids.
func TestAnnotAppearanceWrapsACreatedButtonAppearance(t *testing.T) {
	t.Parallel()

	// The same document, minus the annotation's /AP.
	data := bytes.Replace(buttonWidgetPDF("", "/FT /Btn"),
		[]byte("/AP << /N 6 0 R >> "), []byte(""), 1)

	annot := fixedAnnot(t, data)
	if annot == nil {
		t.Fatal("the annotation gained no appearance at all")
	}
	ap, ok := annot["AP"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/AP is %T, want a dict", annot["AP"])
	}
	if _, wrapped := ap["N"].(ast.PdfDict); !wrapped {
		t.Errorf("/AP /N is %T, want an appearance subdictionary", ap["N"])
	}
	if as, _ := annot["AS"].(ast.PdfName); as != "Off" {
		t.Errorf("/AS = %v, want /Off", annot["AS"])
	}
}

// TestAnnotAppearanceStillWrapsADirectFieldType is the control for the case
// that already worked: /FT on the annotation itself.
func TestAnnotAppearanceStillWrapsADirectFieldType(t *testing.T) {
	t.Parallel()

	annot := fixedAnnot(t, buttonWidgetPDF("/FT /Btn", "/T (parent)"))
	if annot == nil {
		t.Fatal("the annotation was not rewritten at all")
	}
	ap, _ := annot["AP"].(ast.PdfDict)
	if _, ok := ap["N"].(ast.PdfDict); !ok {
		t.Errorf("/AP /N is %T, want an appearance subdictionary", ap["N"])
	}
}
