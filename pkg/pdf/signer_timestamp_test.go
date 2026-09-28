package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/internal/pkitest"
	"github.com/gotenberg/litho/pkg/models"
)

// verifyWith opens data and verifies its signatures against the PKI's root.
func verifyWith(t *testing.T, data []byte, pki *pkitest.PKI) []models.SignatureValidation {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reopening the signed output: %v", err)
	}
	defer r.Close()
	results, err := r.VerifySignatures(ctx, pki.Roots())
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	return results
}

// TestWithTimestampAuthority signs with a signature timestamp from a local
// authority, with the signer of NewSigner and with a caller's Signer, and
// checks the verifier accepts the token; a token that outgrows its reserve is
// fitted by signing again, and an authority that refuses fails the write.
func TestWithTimestampAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		mode  pkitest.TSAMode
		wrap  bool
		calls int
	}{
		{"NewSigner", pkitest.TSAGrant, false, 1},
		{"a caller's Signer", pkitest.TSAGrant, true, 1},
		{"a token past the reserve", pkitest.TSALargeToken, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pki := pkitest.New(t)
			pki.SetTSAMode(tc.mode)
			var signer Signer
			signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wrap {
				signer = wrappedSigner{signer}
			}
			out := signWith(t, writerTestPDF(), signer, WithTimestampAuthority(pki.TSAURL()))
			results := verifyWith(t, out, pki)
			if len(results) != 1 {
				t.Fatalf("%d signatures, want 1", len(results))
			}
			v := results[0]
			if !v.HashValid || !v.CertChainValid {
				t.Errorf("hash %v, chain %v: %s", v.HashValid, v.CertChainValid, v.ErrorMessage)
			}
			if !v.HasTimestamp || v.TimestampError != "" || v.TimestampIssuer != pki.TSA.Subject.CommonName {
				t.Errorf("timestamp: present %v, issuer %q, error %q", v.HasTimestamp, v.TimestampIssuer, v.TimestampError)
			}
			if got := pki.Requests("/tsa"); got != tc.calls {
				t.Errorf("%d timestamp requests, want %d", got, tc.calls)
			}
		})
	}

	t.Run("PAdES B-T", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
		if err != nil {
			t.Fatal(err)
		}
		out := signWith(t, writerTestPDF(), signer, WithPAdES(), WithTimestampAuthority(pki.TSAURL()), WithSignatureField("BT"))
		if v := verifyWith(t, out, pki)[0]; !v.HashValid || !v.HasTimestamp || v.TimestampError != "" || v.SubFilter != "ETSI.CAdES.detached" {
			t.Errorf("hash %v, timestamp %v %q, subfilter %q", v.HashValid, v.HasTimestamp, v.TimestampError, v.SubFilter)
		}
	})

	t.Run("refuses a rejection", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		pki.SetTSAMode(pkitest.TSAReject)
		signer, err := NewSigner(pki.Leaf, pki.LeafKey)
		if err != nil {
			t.Fatal(err)
		}
		src := writerTestPDF()
		r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		w.Sign(signer, WithTimestampAuthority(pki.TSAURL()))
		var buf bytes.Buffer
		if err := w.WriteTo(ctx, &buf); !errors.Is(err, ErrTimestampAuthority) {
			t.Errorf("WriteTo: %v, want ErrTimestampAuthority", err)
		}
		if buf.Len() != 0 {
			t.Errorf("a refused signature wrote %d bytes", buf.Len())
		}
	})
}
