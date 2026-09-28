package signature

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"testing"
	"time"

	"github.com/gotenberg/litho/internal/pkitest"
)

// padesSeeds returns a PKI, a timestamped CAdES SignedData, its token, an
// OCSP response for the PKI's signer and the intermediate's CRL, the valid
// inputs the fuzz targets below start from.
func padesSeeds(f *testing.F) (pki *pkitest.PKI, cms, token, ocsp, crl []byte) {
	f.Helper()
	ctx := context.Background()
	pki = pkitest.New(f)
	s, err := NewCMSSigner(pki.Leaf, pki.LeafKey, []*x509.Certificate{pki.Intermediate})
	if err != nil {
		f.Fatal(err)
	}
	digest := sha256.Sum256([]byte("signed bytes"))
	der, err := s.Sign(digest[:], time.Now(), true)
	if err != nil {
		f.Fatal(err)
	}
	value, err := SignatureValue(der)
	if err != nil {
		f.Fatal(err)
	}
	imprint := sha256.Sum256(value)
	if token, err = RequestTimestamp(ctx, pki.TSAURL(), imprint[:]); err != nil {
		f.Fatal(err)
	}
	if cms, err = AddSignatureTimestamp(der, token); err != nil {
		f.Fatal(err)
	}
	if ocsp, err = fetchOCSP(ctx, pki.Leaf, pki.Intermediate); err != nil || ocsp == nil {
		f.Fatalf("fetching an OCSP response: %v", err)
	}
	return pki, cms, token, ocsp, pki.CRL(pki.Intermediate)
}

// FuzzPAdESParsers feeds the parsers of SignedData, timestamp tokens, OCSP
// responses and CRLs that signing and verifying read from a PDF or the
// network, and checks none panics.
func FuzzPAdESParsers(f *testing.F) {
	pki, cms, token, ocsp, crl := padesSeeds(f)
	for _, seed := range [][]byte{cms, token, ocsp, crl} {
		f.Add(seed)
		f.Add(seed[:len(seed)/2])
	}
	digest := sha256.Sum256([]byte("signed bytes"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		if _, err := SignatureValue(data); err == nil {
			_, _ = AddSignatureTimestamp(data, token) //nolint:errcheck // Only a panic fails the target.
		}
		_ = CheckCAdES(data)                                                        //nolint:errcheck // Only a panic fails the target.
		_, _ = VerifyTimestampToken(data, digest[:])                                //nolint:errcheck // Only a panic fails the target.
		_, _ = signatureTimestamp(data, data, false)                                //nolint:errcheck // Only a panic fails the target.
		_, _ = signatureTimestamp(data, data, true)                                 //nolint:errcheck // Only a panic fails the target.
		_ = checkOCSP(data, pki.Leaf, pki.Intermediate)                             //nolint:errcheck // Only a panic fails the target.
		_ = checkCRL(data, pki.Leaf, pki.Intermediate)                              //nolint:errcheck // Only a panic fails the target.
		_, _ = CollectValidationData(context.Background(), data, &ValidationData{}) //nolint:errcheck // Only a panic fails the target.
	})
}
