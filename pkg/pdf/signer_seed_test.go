package pdf

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/gotenberg/litho/internal/pkitest"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// seedFieldPDF returns a one-page document with the text fields Name and
// Other and the unsigned signature field Approval, whose dictionary gains
// sigEntries, extra being objects 7 and on.
func seedFieldPDF(sigEntries string, extra ...string) []byte {
	return buildPDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /FT /Tx /T (Name) /V (Jane) >>",
		"<< /FT /Tx /T (Other) /V (x) >>",
		"<< /FT /Sig /T (Approval) /Type /Annot /Subtype /Widget /Rect [0 0 0 0] /P 3 0 R " + sigEntries + " >>",
	}, extra...))
}

// signSeeded signs src in the Approval field and returns the output or the
// WriteTo error.
func signSeeded(t *testing.T, src []byte, signer Signer, opts ...SignOption) ([]byte, error) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	w.Sign(signer, append([]SignOption{WithSignatureField("Approval")}, opts...)...)
	var out bytes.Buffer
	err = w.WriteTo(ctx, &out)
	if err != nil && out.Len() != 0 {
		t.Errorf("a refused signature wrote %d bytes", out.Len())
	}
	return out.Bytes(), err
}

// readOnly reports whether the root field named name of data has the
// ReadOnly bit of its /Ff set (ISO 32000-1, Table 221).
func readOnly(t *testing.T, data []byte, name string) bool {
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
		if s, _ := d["T"].(ast.PdfString); document.DecodePdfString(s) == name {
			ff, _ := d["Ff"].(ast.PdfInt)
			return ff&1 != 0
		}
	}
	t.Fatalf("no root field %q", name)
	return false
}

// TestSignatureFieldLock signs a field whose /Lock names fields and checks
// they turn read-only and the signature records a FieldMDP transform (ISO
// 32000-1, 12.7.4.5, Table 233; 12.8.2.4, Table 256).
func TestSignatureFieldLock(t *testing.T) {
	t.Parallel()
	pki := pkitest.New(t)
	signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		lock       string
		name1, oth bool
	}{
		{"Include", "<< /Type /SigFieldLock /Action /Include /Fields [(Name)] >>", true, false},
		{"Exclude", "<< /Type /SigFieldLock /Action /Exclude /Fields [(Name)] >>", false, true},
		{"All", "<< /Type /SigFieldLock /Action /All >>", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := signSeeded(t, seedFieldPDF("/Lock 7 0 R", tc.lock), signer)
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if got := readOnly(t, out, "Name"); got != tc.name1 {
				t.Errorf("Name read-only %v, want %v", got, tc.name1)
			}
			if got := readOnly(t, out, "Other"); got != tc.oth {
				t.Errorf("Other read-only %v, want %v", got, tc.oth)
			}
			sig := signatureDictOf(t, out, "Approval")
			ref, ok := sig["Reference"].(ast.PdfArray)
			if !ok || len(ref) != 1 {
				t.Fatalf("/Reference = %v, want one FieldMDP transform", sig["Reference"])
			}
			if d, _ := ref[0].(ast.PdfDict); d["TransformMethod"] != ast.PdfName("FieldMDP") {
				t.Errorf("/Reference %v is no FieldMDP transform", ref[0])
			}
			if v := verifyWith(t, out, pki)[0]; !v.HashValid || !v.CertChainValid {
				t.Errorf("hash %v, chain %v: %s", v.HashValid, v.CertChainValid, v.ErrorMessage)
			}
		})
	}

	t.Run("with a PDF 2.0 /P", func(t *testing.T) {
		t.Parallel()
		out, err := signSeeded(t, seedFieldPDF("/Lock 7 0 R", "<< /Type /SigFieldLock /Action /All /P 1 >>"), signer)
		if err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		if v := verifyWith(t, out, pki)[0]; v.MDPPolicy != 1 || !v.MDPValid {
			t.Errorf("MDP policy %d, valid %v: %s", v.MDPPolicy, v.MDPValid, v.MDPInfo)
		}
	})

	t.Run("refuses an unknown action", func(t *testing.T) {
		t.Parallel()
		if _, err := signSeeded(t, seedFieldPDF("/Lock << /Action /Some >>"), signer); !errors.Is(err, ErrSignatureFieldUnavailable) {
			t.Errorf("WriteTo: %v, want ErrSignatureFieldUnavailable", err)
		}
	})
}

// TestSignatureFieldSeedValues signs fields whose seed values ask for things
// the signature can be, which it becomes, and things it cannot, which are
// refused (ISO 32000-1, 12.7.4.5, Tables 234 and 235).
func TestSignatureFieldSeedValues(t *testing.T) {
	t.Parallel()
	pki := pkitest.New(t)
	signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
	if err != nil {
		t.Fatal(err)
	}
	other := pkitest.New(t)
	leafHex := "<" + hex.EncodeToString(pki.Leaf.Raw) + ">"
	otherHex := "<" + hex.EncodeToString(other.Leaf.Raw) + ">"
	interHex := "<" + hex.EncodeToString(pki.Intermediate.Raw) + ">"
	tsa := "(" + pki.TSAURL() + ")"

	for _, tc := range []struct {
		name  string
		sv    string
		opts  []SignOption
		check func(t *testing.T, out []byte)
	}{
		{"a preferred subfilter", "<< /SubFilter [/adbe.x509.rsa_sha1 /ETSI.CAdES.detached] >>", nil, func(t *testing.T, out []byte) {
			if got := signatureDictOf(t, out, "Approval")["SubFilter"]; got != ast.PdfName("ETSI.CAdES.detached") {
				t.Errorf("/SubFilter %v, want the first one litho writes", got)
			}
		}},
		{"a required reason", "<< /Ff 8 /Reasons [(Approved) (Reviewed)] >>", nil, func(t *testing.T, out []byte) {
			if got, _ := signatureDictOf(t, out, "Approval")["Reason"].(ast.PdfString); document.DecodePdfString(got) != "Approved" {
				t.Errorf("/Reason %q, want the first seed reason", got)
			}
		}},
		{"a given reason it lists", "<< /Ff 8 /Reasons [(Approved) (Reviewed)] >>", []SignOption{WithSignatureReason("Reviewed")}, nil},
		{"a required timestamp", "<< /TimeStamp << /URL " + tsa + " /Ff 1 >> >>", nil, func(t *testing.T, out []byte) {
			if v := verifyWith(t, out, pki)[0]; !v.HasTimestamp || v.TimestampError != "" {
				t.Errorf("timestamp %v %q, want the seed authority's", v.HasTimestamp, v.TimestampError)
			}
		}},
		{"a digest it signs with", "<< /Ff 64 /DigestMethod [/SHA1 /SHA256] >>", nil, nil},
		{"the signing certificate", "<< /Cert << /Ff 1 /Subject [" + otherHex + " " + leafHex + "] >> >>", nil, nil},
		{"an issuer of the chain", "<< /Cert << /Ff 2 /Issuer [" + interHex + "] >> >>", nil, nil},
		{"a subject name", "<< /Cert << /Ff 8 /SubjectDN [<< /CN (pkitest signer) >>] >> >>", nil, nil},
		{"a key usage", "<< /Cert << /Ff 32 /KeyUsage [(1XXXXXXXX)] >> >>", nil, nil},
		{"the handler and version", "<< /Ff 5 /Filter /Adobe.PPKLite /V 2 >>", nil, nil},
	} {
		t.Run("meets "+tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := signSeeded(t, seedFieldPDF("/SV 7 0 R", tc.sv), signer, tc.opts...)
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if v := verifyWith(t, out, pki)[0]; !v.HashValid || !v.CertChainValid {
				t.Errorf("hash %v, chain %v: %s", v.HashValid, v.CertChainValid, v.ErrorMessage)
			}
			if tc.check != nil {
				tc.check(t, out)
			}
		})
	}

	for _, tc := range []struct {
		name string
		sv   string
		opts []SignOption
	}{
		{"another handler", "<< /Ff 1 /Filter /Entrust.PPKEF >>", nil},
		{"a subfilter litho does not write", "<< /Ff 2 /SubFilter [/adbe.x509.rsa_sha1] >>", nil},
		{"a subfilter other than the one asked for", "<< /Ff 2 /SubFilter [/adbe.pkcs7.detached] >>", []SignOption{WithPAdES()}},
		{"a later seed value version", "<< /Ff 4 /V 3 >>", nil},
		{"a reason it does not list", "<< /Ff 8 /Reasons [(Approved)] >>", []SignOption{WithSignatureReason("Rejected")}},
		{"a legal attestation", "<< /Ff 16 /LegalAttestation [(I attest)] >>", nil},
		{"revocation information in the SignedData", "<< /Ff 32 /AddRevInfo true >>", nil},
		{"another digest", "<< /Ff 64 /DigestMethod [/SHA1] >>", nil},
		{"a locked document", "<< /Ff 128 /LockDocument /true >>", nil},
		{"a certification signature", "<< /MDP << /P 2 >> >>", nil},
		{"a timestamp from no authority", "<< /TimeStamp << /Ff 1 >> >>", nil},
		{"another signing certificate", "<< /Cert << /Ff 1 /Subject [" + otherHex + "] >> >>", nil},
		{"another issuer", "<< /Cert << /Ff 2 /Issuer [" + otherHex + "] >> >>", nil},
		{"another subject name", "<< /Cert << /Ff 8 /SubjectDN [<< /CN (someone else) >>] >> >>", nil},
		{"another key usage", "<< /Cert << /Ff 32 /KeyUsage [(0XXXXXXXX)] >> >>", nil},
		{"a flag no table defines", "<< /Ff 4096 >>", nil},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := signSeeded(t, seedFieldPDF("/SV 7 0 R", tc.sv), signer, tc.opts...); !errors.Is(err, ErrSeedValueUnmet) {
				t.Errorf("WriteTo: %v, want ErrSeedValueUnmet", err)
			}
		})
	}
}
