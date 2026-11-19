package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// javaScriptPDF is a one-page document whose catalog runs a JavaScript action
// on opening, which every part of ISO 19005 forbids (ISO 19005-2, 6.6.1).
func javaScriptPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OpenAction 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /S /JavaScript /JS (app.alert\\('litho-removed-script'\\);) >>",
	})
}

// remediateTo remediates data to profile and returns the output.
func remediateTo(t *testing.T, data []byte, profile models.PDFAProfile) []byte {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.RemediatePDFA(ctx, profile); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// TestRemediate_UnsignedOutputKeepsNothingItRemoved pins that the remediation
// of an unsigned document rewrites it in full. An incremental update kept the
// source's revision in the file, so the JavaScript the remediation removed was
// still in the bytes of a PDF/A output that validates.
func TestRemediate_UnsignedOutputKeepsNothingItRemoved(t *testing.T) {
	t.Parallel()
	data := javaScriptPDF()
	out := remediateTo(t, data, models.PDFA2b)

	if bytes.HasPrefix(out, data) {
		t.Error("the remediation appended an update to an unsigned file")
	}
	if n := bytes.Count(out, []byte("%%EOF")); n != 1 {
		t.Errorf("the output holds %d revisions, want 1", n)
	}
	if bytes.Contains(out, []byte("litho-removed-script")) {
		t.Error("the output still holds the bytes of the JavaScript the remediation removed")
	}
}

// TestRemediate_SignedOutputKeepsItsSignature pins that the remediation of a
// signed document stays an incremental update, which keeps its signature
// verifiable: a full rewrite drops it, and only [WithSignatureRemoval] allows
// that.
func TestRemediate_SignedOutputKeepsItsSignature(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := signWith(t, javaScriptPDF(), newCMSTestSigner(t))
	before := validSignatures(ctx, t, data)
	if len(before) != 1 {
		t.Fatalf("the fixture carries %d valid signatures, want 1", len(before))
	}
	out := remediateTo(t, data, models.PDFA2b)

	if !bytes.HasPrefix(out, data) {
		t.Fatal("the remediation rewrote a signed file")
	}
	if got := validSignatures(ctx, t, out); len(got) != 1 {
		t.Errorf("the output carries %d valid signatures, want 1", len(got))
	}
}
