package pdf

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// testPKI is a root certificate authority and a signing certificate it issued.
type testPKI struct {
	root    *x509.Certificate
	leaf    *x509.Certificate
	leafKey crypto.Signer
}

// roots returns a pool trusting the PKI's root.
func (p testPKI) roots() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(p.root)
	return pool
}

// newTestPKI issues a signing certificate for leafKey under a fresh ECDSA
// root.
func newTestPKI(t *testing.T, leafKey crypto.Signer) testPKI {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issue := func(tmpl, parent *x509.Certificate, pub crypto.PublicKey) *x509.Certificate {
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, rootKey)
		if err != nil {
			t.Fatalf("creating %s: %v", tmpl.Subject.CommonName, err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return cert
	}
	validity := func(tmpl *x509.Certificate) *x509.Certificate {
		tmpl.NotBefore = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		tmpl.NotAfter = time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC)
		return tmpl
	}
	rootTmpl := validity(&x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "litho test root"},
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true,
	})
	root := issue(rootTmpl, rootTmpl, rootKey.Public())
	leaf := issue(validity(&x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "litho test signer"},
		KeyUsage: x509.KeyUsageDigitalSignature,
	}), root, leafKey.Public())
	return testPKI{root: root, leaf: leaf, leafKey: leafKey}
}

// signWith signs src with signer and opts and returns the output.
func signWith(t *testing.T, src []byte, signer Signer, opts ...SignOption) []byte {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	w.Sign(signer, opts...)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// verifyOne checks data holds one signature per wantSigs, the last of which
// verifies against pki, and returns the validations.
func verifyOne(t *testing.T, data []byte, pki testPKI, wantSigs int) []models.SignatureValidation {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reopening the signed output: %v", err)
	}
	defer r.Close()
	results, err := r.VerifySignatures(ctx, pki.roots())
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	if len(results) != wantSigs {
		t.Fatalf("%d signatures, want %d", len(results), wantSigs)
	}
	v := results[len(results)-1]
	if !v.HashValid || !v.CertChainValid || v.SignerCN != pki.leaf.Subject.CommonName {
		t.Errorf("signature %q: hash %v, chain %v, signer %q, error %q", v.FieldName, v.HashValid, v.CertChainValid, v.SignerCN, v.ErrorMessage)
	}
	return results
}

// signatureDictOf returns the signature dictionary of the root field named
// field in data.
func signatureDictOf(t *testing.T, data []byte, field string) ast.PdfDict {
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
		if name, _ := d["T"].(ast.PdfString); document.DecodePdfString(name) == field {
			sig, err := document.ResolveDict(ctx, r.doc, d["V"])
			if err != nil {
				t.Fatalf("field %q has no signature: %v", field, err)
			}
			return sig
		}
	}
	t.Fatalf("no root field named %q", field)
	return nil
}

// TestNewSignerSignsWhatVerifiersAccept signs with every key type [NewSigner]
// takes and checks litho's verifier accepts the signature and its chain, and
// the options reach the signature dictionary.
func TestNewSignerSignsWhatVerifiersAccept(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		key  func() (crypto.Signer, error)
	}{
		{"RSA-2048", func() (crypto.Signer, error) { return rsa.GenerateKey(rand.Reader, 2048) }},
		{"RSA-3072", func() (crypto.Signer, error) { return rsa.GenerateKey(rand.Reader, 3072) }},
		{"ECDSA-P256", func() (crypto.Signer, error) { return ecdsa.GenerateKey(elliptic.P256(), rand.Reader) }},
		{"ECDSA-P384", func() (crypto.Signer, error) { return ecdsa.GenerateKey(elliptic.P384(), rand.Reader) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			key, err := tc.key()
			if err != nil {
				t.Fatal(err)
			}
			pki := newTestPKI(t, key)
			signer, err := NewSigner(pki.leaf, key, pki.root)
			if err != nil {
				t.Fatalf("NewSigner: %v", err)
			}
			at := time.Date(2026, 9, 28, 10, 11, 12, 0, time.FixedZone("CEST", 2*3600))
			out := signWith(t, writerTestPDF(), signer,
				WithSignatureReason("Approved"), WithSignatureLocation("Paris"),
				WithSignatureContactInfo("signer@example.com"), WithSignerName("Jane Doe"),
				WithSigningTime(at))
			results := verifyOne(t, out, pki, 1)
			if results[0].SigningTime != "D:20260928101112+02'00'" {
				t.Errorf("SigningTime = %q, want the /M of WithSigningTime", results[0].SigningTime)
			}

			sig := signatureDictOf(t, out, "Signature1")
			for key, want := range map[ast.PdfName]string{
				"Reason": "Approved", "Location": "Paris", "ContactInfo": "signer@example.com", "Name": "Jane Doe",
			} {
				if got, _ := sig[key].(ast.PdfString); document.DecodePdfString(got) != want {
					t.Errorf("/%s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

// TestNewSignerIsReproducibleWithAFixedTime pins that an RSA signature, whose
// PKCS #1 v1.5 value is deterministic, gives the same output twice once
// WithSigningTime fixes the only other varying input.
func TestNewSignerIsReproducibleWithAFixedTime(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pki := newTestPKI(t, key)
	signer, err := NewSigner(pki.leaf, key)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	first := signWith(t, writerTestPDF(), signer, WithSigningTime(at))
	second := signWith(t, writerTestPDF(), signer, WithSigningTime(at))
	if !bytes.Equal(first, second) {
		t.Error("two signatures of one document at one time differ")
	}
	if !bytes.Contains(first, []byte("/M (D:20260928080000Z)")) {
		t.Error("the signature dictionary does not record the UTC signing time as D:...Z")
	}
}

// TestNewSignerSignsARemediation signs a PDF/A-2b remediation with the built-in
// signer: the signature verifies, and the output validates like the unsigned
// remediation.
func TestNewSignerSignsARemediation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pki := newTestPKI(t, key)
	signer, err := NewSigner(pki.leaf, key, pki.root)
	if err != nil {
		t.Fatal(err)
	}
	remediate := func(sign bool) []byte {
		data := signablePDF()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
		if sign {
			w.Sign(signer, WithSignatureReason("Archived"))
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	unsigned, signed := remediate(false), remediate(true)
	verifyOne(t, signed, pki, 1)
	want, got := violationTexts(validatePDFA2b(t, unsigned)), violationTexts(validatePDFA2b(t, signed))
	if len(got) != len(want) {
		t.Errorf("the signed remediation fails %q, the unsigned one %q", got, want)
	}
}

// sigFieldPDF builds a one-page document whose form holds an unsigned
// signature field Approval with a visible widget on the page, a text field
// Name, and a signed signature field Done.
func sigFieldPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /FT /Sig /T (Approval) /Type /Annot /Subtype /Widget /Rect [72 72 272 122] /F 4 /P 3 0 R >>",
		"<< /FT /Tx /T (Name) /V (Jane) >>",
		"<< /FT /Sig /T (Done) /V 7 0 R >>",
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached >>",
	})
}

// TestWithSignatureField signs an existing empty signature field in place,
// names a new field, and refuses the names the signature cannot take.
func TestWithSignatureField(t *testing.T) {
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

	t.Run("fills the empty field", func(t *testing.T) {
		t.Parallel()
		out := signWith(t, sigFieldPDF(), signer, WithSignatureField("Approval"))
		sig := signatureDictOf(t, out, "Approval")
		if sig["Contents"] == nil {
			t.Fatal("the Approval field holds no signature")
		}
		r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		results, err := r.VerifySignatures(ctx, pki.roots())
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, v := range results {
			names = append(names, v.FieldName)
			if v.FieldName == "Approval" && (!v.HashValid || !v.CertChainValid) {
				t.Errorf("the Approval signature does not verify: %q", v.ErrorMessage)
			}
		}
		for _, v := range results {
			if v.FieldName != "Approval" && v.FieldName != "Done" {
				t.Errorf("signatures on %q, want Approval and Done only", names)
			}
		}
	})

	t.Run("names a new field", func(t *testing.T) {
		t.Parallel()
		out := signWith(t, writerTestPDF(), signer, WithSignatureField("Manager"))
		if sig := signatureDictOf(t, out, "Manager"); sig["Contents"] == nil {
			t.Error("the Manager field holds no signature")
		}
	})

	for _, tc := range []struct{ name, field string }{
		{"a text field", "Name"},
		{"a signed field", "Done"},
		{"a qualified name no field has", "Form.Approval"},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			t.Parallel()
			src := sigFieldPDF()
			r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			w.Sign(signer, WithSignatureField(tc.field))
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); !errors.Is(err, ErrSignatureFieldUnavailable) {
				t.Errorf("WriteTo: %v, want ErrSignatureFieldUnavailable", err)
			}
			if out.Len() != 0 {
				t.Errorf("a refused signature wrote %d bytes", out.Len())
			}
		})
	}
}

// TestParseSignerPEM parses each private key form and a certificate file
// holding the chain before the signer, and refuses what it cannot sign with.
func TestParseSignerPEM(t *testing.T) {
	t.Parallel()

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := func(certs ...*x509.Certificate) []byte {
		var out []byte
		for _, c := range certs {
			out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
		}
		return out
	}
	block := func(typ string, der []byte, err error) []byte {
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
	}
	ecPKI, rsaPKI := newTestPKI(t, ecKey), newTestPKI(t, rsaKey)
	der, err := x509.MarshalPKCS8PrivateKey(ecKey)
	pkcs8EC := block("PRIVATE KEY", der, err)
	der, err = x509.MarshalECPrivateKey(ecKey)
	sec1 := block("EC PRIVATE KEY", der, err)
	pkcs1 := block("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(rsaKey), nil)

	for _, tc := range []struct {
		name      string
		cert, key []byte
		pki       testPKI
	}{
		{"PKCS #8, chain first", certPEM(ecPKI.root, ecPKI.leaf), pkcs8EC, ecPKI},
		{"SEC 1", certPEM(ecPKI.leaf, ecPKI.root), sec1, ecPKI},
		{"PKCS #1", certPEM(rsaPKI.leaf), pkcs1, rsaPKI},
		{"one file for both", append(certPEM(ecPKI.leaf, ecPKI.root), sec1...), append(certPEM(ecPKI.leaf), sec1...), ecPKI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			signer, err := ParseSignerPEM(tc.cert, tc.key)
			if err != nil {
				t.Fatalf("ParseSignerPEM: %v", err)
			}
			if !bytes.Equal(signer.Certificate().Raw, tc.pki.leaf.Raw) {
				t.Errorf("signs as %q, want the leaf", signer.Certificate().Subject.CommonName)
			}
			verifyOne(t, signWith(t, writerTestPDF(), signer), tc.pki, 1)
		})
	}

	encryptedLegacy := pem.EncodeToMemory(&pem.Block{
		Type: "EC PRIVATE KEY", Bytes: []byte{0x30, 0},
		Headers: map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-256-CBC,00"},
	})
	for _, tc := range []struct {
		name      string
		cert, key []byte
		want      error
	}{
		{"an encrypted PKCS #8 key", certPEM(ecPKI.leaf), block("ENCRYPTED PRIVATE KEY", []byte{0x30, 0}, nil), ErrSignerKeyEncrypted},
		{"a legacy encrypted key", certPEM(ecPKI.leaf), encryptedLegacy, ErrSignerKeyEncrypted},
		{"no key", certPEM(ecPKI.leaf), certPEM(ecPKI.leaf), ErrSignerPEM},
		{"no certificate", sec1, sec1, ErrSignerPEM},
		{"two keys", certPEM(ecPKI.leaf), append(append([]byte{}, sec1...), pkcs1...), ErrSignerPEM},
		{"a damaged key", certPEM(ecPKI.leaf), block("EC PRIVATE KEY", []byte{0x30, 0}, nil), ErrSignerPEM},
		{"another key's certificate", certPEM(rsaPKI.leaf), sec1, ErrSignerKeyMismatch},
	} {
		if _, err := ParseSignerPEM(tc.cert, tc.key); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}
