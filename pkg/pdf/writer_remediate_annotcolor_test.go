package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// annotColorFixpointPDF builds a one-page document with no output intent whose
// page paints DeviceCMYK and carries one Square annotation with both colour
// arrays. ap, when not empty, is the annotation's /N appearance content.
func annotColorFixpointPDF(ap string) []byte {
	annot := "<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 /C [1 0 0] /IC [0 0 1] >>"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R /Annots [4 0 R] >>",
		annot,
		streamObj("", []byte("0 0 0 1 k 100 100 200 200 re f")),
	}
	if ap != "" {
		objs[3] = "<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 /C [1 0 0] /IC [0 0 1] /AP << /N 6 0 R >> >>"
		objs = append(objs, streamObj("/Type /XObject /Subtype /Form /BBox [0 0 50 50] /Resources << >>", []byte(ap)))
	}
	return limitsPDF(objs...)
}

// remediateAnnotColor remediates data at PDF/A-1b through the fixpoint loop and
// returns the reopened output with its first annotation and the colour space of
// its first output intent's destination profile.
func remediateAnnotColor(t *testing.T, data []byte) (*Reader, ast.PdfDict, string) {
	t.Helper()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	w := NewWriter(r)
	if _, err := w.RemediatePDFA(ctx, models.PDFA1b); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	t.Cleanup(func() { r2.Close() })

	doc := r2.doc
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	intents, err := document.ResolveArray(ctx, doc, catalog.Dict["OutputIntents"])
	if err != nil || len(intents) == 0 {
		t.Fatalf("the output carries no output intent: %v", err)
	}
	intent, err := document.ResolveDict(ctx, doc, intents[0])
	if err != nil {
		t.Fatalf("resolving the output intent: %v", err)
	}
	profile, err := document.ResolveStream(ctx, doc, intent["DestOutputProfile"])
	if err != nil {
		t.Fatalf("resolving the destination profile: %v", err)
	}
	data, err = doc.DecodeStream(ctx, profile)
	if err != nil || len(data) < 20 {
		t.Fatalf("decoding the destination profile: %v", err)
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) != 1 {
		t.Fatalf("ResolvePages: %v", err)
	}
	annots, err := document.ResolveArray(ctx, doc, pages[0].Dict["Annots"])
	if err != nil || len(annots) != 1 {
		t.Fatalf("the output page carries %d annotations, want 1: %v", len(annots), err)
	}
	annot, err := document.ResolveDict(ctx, doc, annots[0])
	if err != nil {
		t.Fatalf("resolving the annotation: %v", err)
	}
	return r2, annot, string(data[16:20])
}

// annotColorResidual counts ISO 19005-1, 6.5.3 test 3 violations in r.
func annotColorResidual(t *testing.T, r *Reader) int {
	t.Helper()
	vs, err := r.ValidatePDFAReport(context.Background(), models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}
	n := 0
	for _, v := range vs {
		if v.Clause == "6.5.3" && v.TestNumber == 3 {
			n++
		}
	}
	return n
}

// TestWriter_RemediatePDFA1_DropsAnnotColorUnderTheCMYKIntent pins the conflict
// the annot-color fixer resolves, through the fixpoint loop: DeviceCMYK content
// needs a CMYK output intent (ISO 19005-1, 6.2.3.3), which forbids annotation
// colour arrays (6.5.3), so the output carries the intent and not the arrays.
func TestWriter_RemediatePDFA1_DropsAnnotColorUnderTheCMYKIntent(t *testing.T) {
	t.Parallel()

	r, annot, colorSpace := remediateAnnotColor(t, annotColorFixpointPDF(""))
	if colorSpace != "CMYK" {
		t.Fatalf("control: the output intent is %q, want CMYK, so there is no conflict", colorSpace)
	}
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := annot[key]; has {
			t.Errorf("%s survived under a CMYK output intent; annotation = %v", key, annot)
		}
	}
	if n := annotColorResidual(t, r); n != 0 {
		t.Errorf("the output still reports %d 6.5.3 test 3 violation(s)", n)
	}
}

// TestWriter_RemediatePDFA1_KeepsAnnotColorWhenTheIntentTurnsRGB pins the
// deferral. The appearance paints DeviceRGB under resources of its own, which
// the /DefaultRGB that output-intent injects into the page does not reach, so
// the round after the one that installed the CMYK intent replaces it with an
// sRGB one, which permits the colour arrays. Removing them in the first round
// lost them for nothing.
func TestWriter_RemediatePDFA1_KeepsAnnotColorWhenTheIntentTurnsRGB(t *testing.T) {
	t.Parallel()

	r, annot, colorSpace := remediateAnnotColor(t, annotColorFixpointPDF("1 0 0 rg 0 0 50 50 re f"))
	if colorSpace != "RGB " {
		t.Fatalf("control: the output intent is %q, want RGB, so no later round replaced the CMYK one", colorSpace)
	}
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := annot[key]; !has {
			t.Errorf("%s was removed although the output intent is RGB; annotation = %v", key, annot)
		}
	}
	if n := annotColorResidual(t, r); n != 0 {
		t.Errorf("the output still reports %d 6.5.3 test 3 violation(s)", n)
	}
}
