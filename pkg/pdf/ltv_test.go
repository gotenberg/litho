package pdf

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/gotenberg/litho/internal/pkitest"
	"github.com/gotenberg/litho/pkg/models"
)

// dssInfoOf returns the DSS summary of data.
func dssInfoOf(t *testing.T, data []byte) *models.DSSInfo {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	info, err := r.DSSInfo(ctx)
	if err != nil {
		t.Fatalf("DSSInfo: %v", err)
	}
	return info
}

// signLTV signs src with the PKI's signer as PAdES B-LT, with opts after the
// defaults, and returns the output or the WriteTo error.
func signLTV(t *testing.T, pki *pkitest.PKI, src []byte, opts ...SignOption) ([]byte, error) {
	t.Helper()
	ctx := context.Background()
	signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	w.Sign(signer, append([]SignOption{WithPAdES(), WithTimestampAuthority(pki.TSAURL()), WithLTV()}, opts...)...)
	var out bytes.Buffer
	err = w.WriteTo(ctx, &out)
	if err != nil && out.Len() != 0 {
		t.Errorf("a refused signature wrote %d bytes", out.Len())
	}
	return out.Bytes(), err
}

// TestWithLTV signs PAdES B-LT signatures whose validation data is fetched
// over OCSP, fetched over CRLs, or supplied, and checks litho's verifier
// finds it and calls the signature LTV-enabled; revocation and missing data
// refuse the write.
func TestWithLTV(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, out []byte, pki *pkitest.PKI, wantRevocation string) {
		t.Helper()
		results := verifyWith(t, out, pki)
		if len(results) != 1 {
			t.Fatalf("%d signatures, want 1", len(results))
		}
		v := results[0]
		if !v.HashValid || !v.CertChainValid || !v.HasDSS || !v.RevocationOK || !v.LTVEnabled {
			t.Errorf("hash %v, chain %v, DSS %v, revocation %v (%s), LTV %v: %s",
				v.HashValid, v.CertChainValid, v.HasDSS, v.RevocationOK, v.RevocationInfo, v.LTVEnabled, v.ErrorMessage)
		}
		if len(v.RevocationInfo) < len(wantRevocation) || v.RevocationInfo[:len(wantRevocation)] != wantRevocation {
			t.Errorf("revocation info %q, want it to start with %q", v.RevocationInfo, wantRevocation)
		}
		info := dssInfoOf(t, out)
		// The signer, its intermediate, the root and the TSA.
		if info == nil || info.CertCount != 4 || info.VRICount != 1 {
			t.Errorf("DSS %+v, want 4 certificates and 1 VRI", info)
		}
		if !hasESIC(t, out) {
			t.Error("the catalog of a pre-2.0 document declares no ESIC extension")
		}
	}

	t.Run("over OCSP", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		out, err := signLTV(t, pki, writerTestPDF())
		if err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		check(t, out, pki, "OCSP: good")
		// The signer, its intermediate and the TSA, each below a root.
		if info := dssInfoOf(t, out); info.OCSPCount != 3 || info.CRLCount != 0 {
			t.Errorf("DSS %+v, want 3 OCSP responses and no CRL", info)
		}
	})

	t.Run("over CRLs", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		pki.SetOCSPDown(true)
		out, err := signLTV(t, pki, writerTestPDF())
		if err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		check(t, out, pki, "CRL: certificate not revoked")
		if info := dssInfoOf(t, out); info.OCSPCount != 0 || info.CRLCount != 2 {
			t.Errorf("DSS %+v, want no OCSP response and 2 CRLs", info)
		}
	})

	t.Run("supplied", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		data := ValidationData{Certificates: []*x509.Certificate{pki.Root}, CRLs: [][]byte{pki.CRL(pki.Intermediate), pki.CRL(pki.Root)}}
		out, err := signLTV(t, pki, writerTestPDF(), WithValidationData(data))
		if err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		check(t, out, pki, "CRL: certificate not revoked")
		for _, path := range []string{"/ocsp", "/crl/root", "/crl/intermediate", "/cert/root", "/cert/intermediate"} {
			if n := pki.Requests(path); n != 0 {
				t.Errorf("%d requests to %s with the data supplied, want none", n, path)
			}
		}
	})

	t.Run("adds to a store", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		first, err := signLTV(t, pki, writerTestPDF(), WithSignatureField("First"))
		if err != nil {
			t.Fatal(err)
		}
		second, err := signLTV(t, pki, first, WithSignatureField("Second"))
		if err != nil {
			t.Fatal(err)
		}
		results := verifyWith(t, second, pki)
		if len(results) != 2 {
			t.Fatalf("%d signatures, want 2", len(results))
		}
		for _, v := range results {
			if !v.HashValid || !v.LTVEnabled {
				t.Errorf("%s: hash %v, LTV %v: %s", v.FieldName, v.HashValid, v.LTVEnabled, v.ErrorMessage)
			}
		}
		if info := dssInfoOf(t, second); info.CertCount != 4 || info.VRICount != 2 {
			t.Errorf("DSS %+v, want the 4 certificates once and 2 VRIs", info)
		}
	})

	t.Run("B-LTA", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
		if err != nil {
			t.Fatal(err)
		}
		out := timestampWith(t, writerTestPDF(), func(w *Writer) {
			w.Sign(signer, WithPAdES(), WithTimestampAuthority(pki.TSAURL()), WithLTV())
			w.Timestamp(pki.TSAURL())
		})
		results := verifyWith(t, out, pki)
		if len(results) != 2 {
			t.Fatalf("%d signatures, want 2", len(results))
		}
		for _, v := range results {
			if !v.HashValid || !v.CertChainValid || !v.HasDSS {
				t.Errorf("%s: hash %v, chain %v, DSS %v: %s", v.FieldName, v.HashValid, v.CertChainValid, v.HasDSS, v.ErrorMessage)
			}
		}
		if !results[0].LTVEnabled {
			t.Errorf("the signature under the document timestamp is not LTV-enabled: %s", results[0].RevocationInfo)
		}
	})

	for _, tc := range []struct {
		name  string
		setup func(pki *pkitest.PKI)
		opts  func(pki *pkitest.PKI) []SignOption
		want  error
	}{
		{"a revoked signer", func(pki *pkitest.PKI) { pki.Revoke(pki.Leaf, time.Now().Add(-time.Hour)) }, nil, ErrCertificateRevoked},
		{"a revoked signer on its CRL", func(pki *pkitest.PKI) {
			pki.SetOCSPDown(true)
			pki.Revoke(pki.Leaf, time.Now().Add(-time.Hour))
		}, nil, ErrCertificateRevoked},
		{"supplied data that misses a certificate", nil, func(pki *pkitest.PKI) []SignOption {
			return []SignOption{WithValidationData(ValidationData{Certificates: []*x509.Certificate{pki.Root}, CRLs: [][]byte{pki.CRL(pki.Intermediate)}})}
		}, ErrValidationData},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			t.Parallel()
			pki := pkitest.New(t)
			if tc.setup != nil {
				tc.setup(pki)
			}
			var opts []SignOption
			if tc.opts != nil {
				opts = tc.opts(pki)
			}
			if _, err := signLTV(t, pki, writerTestPDF(), opts...); !errors.Is(err, tc.want) {
				t.Errorf("WriteTo: %v, want %v", err, tc.want)
			}
		})
	}
}
