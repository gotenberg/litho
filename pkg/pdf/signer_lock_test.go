package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/internal/pkitest"
)

// lockedPairPDF returns a one-page document with the text field Name and the
// unsigned signature fields Approval, whose /Lock is lock, and Second.
func lockedPairPDF(lock string) []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R 6 0 R] >>",
		"<< /FT /Tx /T (Name) /V (Jane) >>",
		"<< /FT /Sig /T (Approval) /Type /Annot /Subtype /Widget /Rect [0 0 0 0] /P 3 0 R /Lock " + lock + " >>",
		"<< /FT /Sig /T (Second) /Type /Annot /Subtype /Widget /Rect [0 0 0 0] /P 3 0 R >>",
	})
}

// writeAfter writes src with set applied to its Writer, and returns the
// output or the WriteTo error.
func writeAfter(t *testing.T, src []byte, set func(*Writer)) ([]byte, error) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	set(w)
	var out bytes.Buffer
	err = w.WriteTo(ctx, &out)
	if err != nil && out.Len() != 0 {
		t.Errorf("a refused write wrote %d bytes", out.Len())
	}
	return out.Bytes(), err
}

// TestSignatureAfterFieldLock signs the Approval field, whose /Lock names
// fields, then signs again and timestamps. A signature in a field the lock
// reaches, existing or new, is refused with ErrSignatureFieldLocked (ISO
// 32000-1, 12.8.2.4, Table 256), one elsewhere signs, and a document
// timestamp in a new field is written whatever the lock, and leaves the
// locked signature valid.
func TestSignatureAfterFieldLock(t *testing.T) {
	t.Parallel()
	pki := pkitest.New(t)
	signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name                  string
		lock                  string
		secondLocked, newLock bool
	}{
		{"Include", "<< /Action /Include /Fields [(Name)] >>", false, false},
		{"Include naming Second", "<< /Action /Include /Fields [(Second)] >>", true, false},
		{"Exclude", "<< /Action /Exclude /Fields [(Second)] >>", false, true},
		{"All", "<< /Action /All >>", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			signed, err := writeAfter(t, lockedPairPDF(tc.lock), func(w *Writer) { w.Sign(signer, WithSignatureField("Approval")) })
			if err != nil {
				t.Fatalf("signing Approval: %v", err)
			}
			for _, step := range []struct {
				field  string
				locked bool
			}{{"Second", tc.secondLocked}, {"", tc.newLock}} {
				_, err := writeAfter(t, signed, func(w *Writer) {
					if step.field != "" {
						w.Sign(signer, WithSignatureField(step.field))
					} else {
						w.Sign(signer)
					}
				})
				if step.locked != errors.Is(err, ErrSignatureFieldLocked) || (step.locked && !errors.Is(err, ErrSignatureFieldUnavailable)) {
					t.Errorf("signing %q: %v, locked %v", step.field, err, step.locked)
				}
				if !step.locked && err != nil {
					t.Errorf("signing %q: %v", step.field, err)
				}
			}

			stamped, err := writeAfter(t, signed, func(w *Writer) { w.Timestamp(pki.TSAURL()) })
			if err != nil {
				t.Fatalf("timestamping: %v", err)
			}
			results := verifyWith(t, stamped, pki)
			if len(results) != 2 {
				t.Fatalf("%d signatures, want 2", len(results))
			}
			if v := results[0]; v.FieldName != "Approval" || !v.HashValid || !v.CertChainValid {
				t.Errorf("Approval after the timestamp: field %q, hash %v, chain %v: %s", v.FieldName, v.HashValid, v.CertChainValid, v.ErrorMessage)
			}

			_, err = writeAfter(t, signed, func(w *Writer) { w.Timestamp(pki.TSAURL(), WithTimestampField("Second")) })
			if tc.secondLocked != errors.Is(err, ErrSignatureFieldLocked) || (!tc.secondLocked && err != nil) {
				t.Errorf("timestamping in Second: %v, locked %v", err, tc.secondLocked)
			}
		})
	}
}
