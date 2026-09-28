package pdf

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/signature"
)

// signerAdapter wraps a public [Signer] so it satisfies the internal
// [mutations.Signer] interface. The method sets are identical; this
// adapter exists only to bridge the two named interface types without
// leaking the internal one through the public API.
//
// at is the signing time the signature dictionary's /M records. It reaches the
// signer of [NewSigner] as the SignedData's signing-time, so the two agree.
type signerAdapter struct {
	inner Signer
	at    time.Time
}

// Sign returns the SignedData over digest. Under ETSI.CAdES.detached the
// signer of [NewSigner] writes the CAdES form, and the SignedData of any other
// [Signer] is refused with [ErrSignatureNotPAdES] when it is not one (see
// [WithPAdES]).
//
// With a timestamp authority, the SignedData's signature value is then
// timestamped (see [WithTimestampAuthority]).
func (a signerAdapter) Sign(ctx context.Context, digest []byte, params mutations.SignParams) ([]byte, error) {
	der, err := a.signedData(digest, params.SubFilter == mutations.SubFilterCAdESDetached)
	if err != nil || params.TimestampURL == "" {
		return der, err
	}
	return timestampSignature(ctx, der, params.TimestampURL)
}

// signedData returns the Signer's SignedData over digest, in its CAdES form
// under cades.
func (a signerAdapter) signedData(digest []byte, cades bool) ([]byte, error) {
	if s, ok := a.inner.(*builtinSigner); ok {
		return s.signAt(digest, a.at, cades)
	}
	der, err := a.inner.Sign(digest)
	if err != nil {
		return nil, err
	}
	if cades {
		if err := signature.CheckCAdES(der); err != nil {
			return nil, fmt.Errorf("the Signer's SignedData under ETSI.CAdES.detached: %w", err)
		}
	}
	return der, nil
}

// Certificate returns the signing certificate.
func (a signerAdapter) Certificate() *x509.Certificate { return a.inner.Certificate() }

// Chain returns the chain the signer of [NewSigner] was given, and nil for
// any other [Signer], whose chain the interface does not expose.
func (a signerAdapter) Chain() []*x509.Certificate {
	if s, ok := a.inner.(*builtinSigner); ok {
		return s.cms.Chain()
	}
	return nil
}

// EstimatedSize returns the Signer's estimate, and room for a timestamp token
// when params asks for one.
func (a signerAdapter) EstimatedSize(params mutations.SignParams) int {
	n := a.inner.EstimatedSize()
	if params.TimestampURL != "" {
		n += timestampTokenReserve
	}
	return n
}

// timestampTokenReserve is the room a signature's /Contents keeps for an
// RFC 3161 token before the authority answers. Not spec-mandated: a token
// holds a TSTInfo, a signature and the TSA's certificates, a few kilobytes,
// and a larger one makes [Writer.WriteTo] write the signature again with room
// for it rather than fail.
const timestampTokenReserve = 8192

// timestampSignature returns the SignedData der with an RFC 3161 token over
// its signature value, from the authority at tsaURL, in its SignerInfo's
// unsigned attributes (RFC 3161, Appendix A).
func timestampSignature(ctx context.Context, der []byte, tsaURL string) ([]byte, error) {
	value, err := signature.SignatureValue(der)
	if err != nil {
		return nil, fmt.Errorf("finding the signature value to timestamp: %w", err)
	}
	imprint := sha256.Sum256(value)
	token, err := signature.RequestTimestamp(ctx, tsaURL, imprint[:])
	if err != nil {
		return nil, fmt.Errorf("timestamping the signature: %w", err)
	}
	stamped, err := signature.AddSignatureTimestamp(der, token)
	if err != nil {
		return nil, fmt.Errorf("adding the signature timestamp: %w", err)
	}
	return stamped, nil
}

// signing returns the signer and the signature dictionary entries of a write,
// with the signing time [WithSigningTime] set or else now.
func (w *Writer) signing(signer Signer) (signerAdapter, mutations.SignatureInfo) {
	info := w.signOpts.info
	if info.Time.IsZero() {
		info.Time = time.Now()
	}
	return signerAdapter{inner: signer, at: info.Time}, info
}
