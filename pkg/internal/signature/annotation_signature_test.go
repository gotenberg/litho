package signature

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// buildSigPDF assembles a document from the given object bodies.
func buildSigPDF(objs []string, trailerExtra string) []byte {
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R %s>>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, trailerExtra, xref)
	return buf.Bytes()
}

// unregisteredSigPDF builds a signed document whose signature widget is on the
// page but absent from any interactive form, the shape of pdfjs issue17794.pdf.
func unregisteredSigPDF(withAcroForm bool) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if withAcroForm {
		catalog = "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>"
	}
	return buildSigPDF([]string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (sig-on-page) /Rect [0 0 10 10] /V 5 0 R >>",
		"<< /Type /Sig /ByteRange [0 100 200 50] /Contents <30820101> /M (D:20240101000000Z) >>",
	}, "")
}

func sigsOf(t *testing.T, data []byte) []string {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	sigs, err := ExtractSignatures(ctx, doc)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	names := make([]string, len(sigs))
	for i, s := range sigs {
		names[i] = s.FieldName
	}
	return names
}

// TestSignatureFoundWithoutAnAcroForm pins that a signature is reported when
// its field is reachable only from the page.
//
// ISO 32000-1, 12.7.2 says an interactive form lists its fields in /AcroForm,
// and 12.5.6.19 says the page carries the widget that presents one. Producers
// sign documents without registering the field: pdfjs issue17794.pdf has no
// /AcroForm at all, and issue18466.pdf and a veraPDF 6.1.12 fixture are the
// same shape. litho answered "(no signatures)" for all three while poppler's
// pdfsig reported them.
func TestSignatureFoundWithoutAnAcroForm(t *testing.T) {
	t.Parallel()

	got := sigsOf(t, unregisteredSigPDF(false))
	if len(got) != 1 {
		t.Fatalf("found %d signatures %v, want 1: the widget is only on the page", len(got), got)
	}
	if got[0] != "sig-on-page" {
		t.Errorf("field name = %q, want %q", got[0], "sig-on-page")
	}
}

// TestRegisteredSignatureReportedOnce pins that a field listed in the form and
// present on the page is not counted twice.
func TestRegisteredSignatureReportedOnce(t *testing.T) {
	t.Parallel()

	got := sigsOf(t, unregisteredSigPDF(true))
	if len(got) != 1 {
		t.Errorf("found %d signatures %v, want 1: the page walk duplicated the registered field",
			len(got), got)
	}
}

// TestUnsignedFieldIsNotASignature is the control: an empty signature field is
// a placeholder, not a signature. poppler's pdfsig lists it and says "The
// signature form field is not signed"; litho reports only real signatures, as
// pdfjs issue18801.pdf shows.
func TestUnsignedFieldIsNotASignature(t *testing.T) {
	t.Parallel()

	data := buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (empty) /Rect [0 0 10 10] >>",
	}, "")

	if got := sigsOf(t, data); len(got) != 0 {
		t.Errorf("found %d signatures %v, want 0: the field carries no /V", len(got), got)
	}
}

// TestCyclicFieldKidsTerminates pins that a field hierarchy pointing back at
// itself cannot recurse without end.
//
// ISO 32000-1, 12.7.3 describes a shallow hierarchy, and nothing stops a
// crafted file from making /Kids cyclic. The walk had no depth bound, so such a
// file exhausted the stack.
func TestCyclicFieldKidsTerminates(t *testing.T) {
	t.Parallel()

	data := buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		// Two fields naming each other through /Kids.
		"<< /T (a) /Kids [5 0 R] >>",
		"<< /T (b) /Kids [4 0 R] >>",
	}, "")

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = sigsOf(t, data)
	}()
	<-done
}
