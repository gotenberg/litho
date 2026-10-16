package pdf

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/gotenberg/litho/internal/pkitest"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// countingSigner is a caller's own [Signer] that counts its Sign calls. It
// signs through the signer of NewSigner's public method, whose SignedData
// carries a signing-time and so is no PAdES CAdES signature.
type countingSigner struct {
	Signer
	calls *atomic.Int32
}

func (s countingSigner) Sign(digest []byte) ([]byte, error) {
	s.calls.Add(1)
	return s.Signer.Sign(digest)
}

// TestSeedSubFilterFallback signs fields whose seed values prefer
// ETSI.CAdES.detached with a caller's Signer that cannot write it. ISO
// 32000-1, 12.7.4.5, Table 234 lists the subfilters in preference order and
// binds the first the handler supports only when /Ff bit 2 is set, so a
// recommended list falls back to the next one the Signer writes, and a
// required one is refused.
func TestSeedSubFilterFallback(t *testing.T) {
	t.Parallel()
	pki := pkitest.New(t)
	builtin, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		sv        string
		builtin   bool
		want      ast.PdfName
		wantSigns int32
	}{
		{"a recommended list naming adbe.pkcs7.detached next", "<< /SubFilter [/adbe.x509.rsa_sha1 /ETSI.CAdES.detached /adbe.pkcs7.detached] >>", false, "adbe.pkcs7.detached", 2},
		{"a recommended list of CAdES alone", "<< /SubFilter [/ETSI.CAdES.detached] >>", false, "adbe.pkcs7.detached", 2},
		{"a recommended list, other entries required", "<< /Ff 8 /Reasons [(Approved)] /SubFilter [/ETSI.CAdES.detached] >>", false, "adbe.pkcs7.detached", 2},
		{"a recommended list the signer meets first", "<< /SubFilter [/adbe.pkcs7.detached /ETSI.CAdES.detached] >>", false, "adbe.pkcs7.detached", 1},
		{"a required list, signed by NewSigner's signer", "<< /Ff 2 /SubFilter [/ETSI.CAdES.detached /adbe.pkcs7.detached] >>", true, "ETSI.CAdES.detached", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			var signer Signer = countingSigner{Signer: builtin, calls: &calls}
			if tc.builtin {
				signer = builtin
			}
			out, err := signSeeded(t, seedFieldPDF("/SV 7 0 R", tc.sv), signer)
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if got := signatureDictOf(t, out, "Approval")["SubFilter"]; got != tc.want {
				t.Errorf("/SubFilter %v, want %v", got, tc.want)
			}
			if v := verifyWith(t, out, pki)[0]; !v.HashValid || !v.CertChainValid {
				t.Errorf("hash %v, chain %v: %s", v.HashValid, v.CertChainValid, v.ErrorMessage)
			}
			wantESIC := 0
			if tc.want == "ETSI.CAdES.detached" {
				wantESIC = 2
			}
			if got := esicLevel(t, out); got != wantESIC {
				t.Errorf("ESIC level %d, want %d", got, wantESIC)
			}
			if !tc.builtin && calls.Load() != tc.wantSigns {
				t.Errorf("the Signer signed %d times, want %d", calls.Load(), tc.wantSigns)
			}
		})
	}

	for _, tc := range []struct {
		name string
		sv   string
		opts []SignOption
	}{
		{"a required list of CAdES alone", "<< /Ff 2 /SubFilter [/ETSI.CAdES.detached] >>", nil},
		// Table 234 binds the first name the handler supports, which pyHanko's
		// validator holds a signature to, so a required list does not fall
		// back to the adbe.pkcs7.detached it names next.
		{"a required list naming adbe.pkcs7.detached next", "<< /Ff 2 /SubFilter [/ETSI.CAdES.detached /adbe.pkcs7.detached] >>", nil},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			_, err := signSeeded(t, seedFieldPDF("/SV 7 0 R", tc.sv), countingSigner{Signer: builtin, calls: &calls}, tc.opts...)
			if !errors.Is(err, ErrSeedValueUnmet) || !errors.Is(err, ErrSignatureNotPAdES) {
				t.Errorf("WriteTo: %v, want ErrSeedValueUnmet and ErrSignatureNotPAdES", err)
			}
			if calls.Load() != 1 {
				t.Errorf("the Signer signed %d times, want 1", calls.Load())
			}
		})
	}

	// WithPAdES asks for ETSI.CAdES.detached itself, so a list that would let
	// the signature fall back does not.
	_, err = signSeeded(t, seedFieldPDF("/SV 7 0 R", "<< /SubFilter [/ETSI.CAdES.detached /adbe.pkcs7.detached] >>"), wrappedSigner{builtin}, WithPAdES())
	if !errors.Is(err, ErrSignatureNotPAdES) || errors.Is(err, ErrSeedValueUnmet) {
		t.Errorf("WithPAdES and a Signer writing a signing-time: %v, want ErrSignatureNotPAdES alone", err)
	}
}

// TestSeedSubFilterFallbackFixture signs the seedmet field of the litho
// corpus fixture siglock_2b.pdf, whose required list is adbe.x509.rsa_sha1,
// ETSI.CAdES.detached and adbe.pkcs7.detached, with both kinds of Signer: the
// signer of NewSigner writes ETSI.CAdES.detached, the first litho writes, and
// a caller's Signer that cannot is refused. Its seedunmet field, which
// requires adbe.x509.rsa_sha1 alone, is refused for both.
func TestSeedSubFilterFallbackFixture(t *testing.T) {
	t.Parallel()
	src := readLithoFixture(t, "siglock_2b.pdf")
	pki := pkitest.New(t)
	builtin, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
	if err != nil {
		t.Fatal(err)
	}

	sign := func(signer Signer, field string) ([]byte, error) {
		ctx := context.Background()
		r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		w.Sign(signer, WithSignatureField(field))
		var out bytes.Buffer
		err = w.WriteTo(ctx, &out)
		return out.Bytes(), err
	}

	out, err := sign(builtin, "seedmet")
	if err != nil {
		t.Fatalf("seedmet: %v", err)
	}
	if got := signatureDictOf(t, out, "seedmet")["SubFilter"]; got != ast.PdfName("ETSI.CAdES.detached") {
		t.Errorf("seedmet: /SubFilter %v, want ETSI.CAdES.detached", got)
	}
	if v := verifyWith(t, out, pki)[0]; !v.HashValid || !v.CertChainValid {
		t.Errorf("seedmet: hash %v, chain %v: %s", v.HashValid, v.CertChainValid, v.ErrorMessage)
	}
	if _, err := sign(wrappedSigner{builtin}, "seedmet"); !errors.Is(err, ErrSeedValueUnmet) || !errors.Is(err, ErrSignatureNotPAdES) {
		t.Errorf("a caller's Signer, seedmet: %v, want ErrSeedValueUnmet and ErrSignatureNotPAdES", err)
	}
	for _, signer := range []Signer{builtin, wrappedSigner{builtin}} {
		if _, err := sign(signer, "seedunmet"); !errors.Is(err, ErrSeedValueUnmet) {
			t.Errorf("%T, seedunmet: %v, want ErrSeedValueUnmet", signer, err)
		}
	}
}
