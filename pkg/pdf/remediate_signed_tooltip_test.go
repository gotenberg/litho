package pdf

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestRemediatePDFUA_TooltipsASignedFieldAndKeepsItsSignature pins why the
// form-tooltip fixer carries no signedness guard. It writes /TU and /Contents
// on the field dictionary, never on the signature dictionary its /V names,
// whose /ByteRange fixes the bytes the digest covers (ISO 32000-1, 12.8.1). An
// incremental update keeps the signed revision byte for byte, so the signature
// verifies after the repair; a full rewrite drops the signed field whole (see
// [Writer.DroppedSignatures]). annot-nesting writes /StructParent on the same
// widget in the same run, so a guard on the tooltip alone would leave the field
// written and the PDF/UA-1 7.18.1 violation unrepaired.
func TestRemediatePDFUA_TooltipsASignedFieldAndKeepsItsSignature(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pki := newTestPKI(t, key)
	signer, err := NewSigner(pki.leaf, key)
	if err != nil {
		t.Fatal(err)
	}
	src := signWith(t, sigFieldPDF(), signer, WithSignatureField("Approval"))
	srcField := rootFieldNamed(t, src, "Approval")

	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.RemediatePDFUA(ctx, models.PDFUA1); err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	out := buf.Bytes()
	if !bytes.HasPrefix(out, src) {
		t.Fatal("the remediation rewrote the file, so the incremental path never ran")
	}

	field := rootFieldNamed(t, out, "Approval")
	if _, ok := field["TU"]; !ok {
		t.Errorf("the signed field carries no /TU: %v", field)
	}
	if field["V"] != srcField["V"] {
		t.Errorf("the field's /V moved from %v to %v", srcField["V"], field["V"])
	}
	r2, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	results, err := r2.VerifySignatures(ctx, pki.roots())
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	verified := false
	for _, v := range results {
		if v.FieldName != "Approval" {
			continue
		}
		verified = true
		if !v.HashValid || !v.CertChainValid {
			t.Errorf("the Approval signature no longer verifies: %q", v.ErrorMessage)
		}
	}
	if !verified {
		t.Error("the output holds no Approval signature")
	}
}

// rootFieldNamed returns the root form field whose /T is name in data.
func rootFieldNamed(t *testing.T, data []byte, name string) ast.PdfDict {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cat, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil {
		t.Fatal(err)
	}
	form, err := document.ResolveDict(ctx, r.doc, cat.Dict["AcroForm"])
	if err != nil {
		t.Fatal(err)
	}
	fields, err := document.ResolveArray(ctx, r.doc, form["Fields"])
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		d, err := document.ResolveDict(ctx, r.doc, f)
		if err != nil {
			continue
		}
		if s, err := document.ResolveObject(ctx, r.doc, d["T"]); err == nil && s == ast.PdfString(name) {
			return d
		}
	}
	t.Fatalf("no field %q", name)
	return nil
}
