package signature

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// OIDs of the RFC 3161 time-stamp protocol and of the CMS attribute a
// signature timestamp travels in.
var (
	// oidTSTInfo is id-ct-TSTInfo, the eContentType of a TimeStampToken (RFC
	// 3161, 2.4.2).
	oidTSTInfo = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
	// oidAttrTimeStampToken is id-aa-signatureTimeStampToken (RFC 3161,
	// Appendix A), the unsigned attribute of a SignerInfo that holds a token
	// over its signature value.
	oidAttrTimeStampToken = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 14}
)

// maxTSAResponseBytes bounds the body of a timestamp authority's response.
// Anti-DoS guard, not spec-mandated: a TimeStampResp holds a status and one
// token, the TSA's certificates included, a few kilobytes in practice, and a
// server streaming more is not answering the request.
const maxTSAResponseBytes = 1 << 20

// timeStampReq is the TimeStampReq of RFC 3161, 2.4.1, without the policy
// and extensions litho does not ask for.
type timeStampReq struct {
	Version        int
	MessageImprint messageImprint
	Nonce          *big.Int
	CertReq        bool
}

// messageImprint is the MessageImprint of RFC 3161, 2.4.1.
type messageImprint struct {
	HashAlgorithm pkix.AlgorithmIdentifier
	HashedMessage []byte
}

// timeStampResp is the TimeStampResp of RFC 3161, 2.4.2.
type timeStampResp struct {
	Status         pkiStatusInfo
	TimeStampToken asn1.RawValue `asn1:"optional"`
}

// pkiStatusInfo is the PKIStatusInfo of RFC 3161, 2.4.2.
type pkiStatusInfo struct {
	Status       int
	StatusString []asn1.RawValue `asn1:"optional"`
	FailInfo     asn1.BitString  `asn1:"optional"`
}

// text returns the PKIFreeText of the status, its UTF8Strings joined by
// newlines.
func (s pkiStatusInfo) text() string {
	lines := make([]string, 0, len(s.StatusString))
	for _, v := range s.StatusString {
		lines = append(lines, string(v.Bytes))
	}
	return strings.Join(lines, "\n")
}

// accuracy is the Accuracy of RFC 3161, 2.4.2.
type accuracy struct {
	Seconds int `asn1:"optional"`
	Millis  int `asn1:"optional,tag:0"`
	Micros  int `asn1:"optional,tag:1"`
}

// tstInfoFull is the whole TSTInfo of RFC 3161, 2.4.2.
type tstInfoFull struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint messageImprint
	SerialNumber   *big.Int
	GenTime        time.Time     `asn1:"generalized"`
	Accuracy       accuracy      `asn1:"optional"`
	Ordering       bool          `asn1:"optional"`
	Nonce          *big.Int      `asn1:"optional"`
	TSA            asn1.RawValue `asn1:"optional,explicit,tag:0"`
	Extensions     asn1.RawValue `asn1:"optional,tag:1"`
}

// RequestTimestamp asks the RFC 3161 timestamp authority at tsaURL for a
// token over the SHA-256 digest, over HTTP as RFC 3161, 3.4 describes, and
// returns the TimeStampToken once [VerifyTimestampToken] accepts it with the
// nonce the request carried. It returns [ErrTimestampAuthority] for an
// unreachable authority, one that refuses the request, and one whose answer
// does not verify.
func RequestTimestamp(ctx context.Context, tsaURL string, digest []byte) ([]byte, error) {
	if len(digest) != sha256.Size {
		return nil, fmt.Errorf("%w: a %d-byte digest is not a SHA-256", ErrTimestampAuthority, len(digest))
	}
	// RFC 3161, 2.4.1: the nonce ties the response to this request, so a
	// replayed response for another request is refused.
	nonce, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return nil, fmt.Errorf("drawing the timestamp nonce: %w", err)
	}
	req, err := asn1.Marshal(timeStampReq{
		Version:        1,
		MessageImprint: messageImprint{HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: oidSHA256}, HashedMessage: digest},
		Nonce:          nonce,
		// The TSA's certificate in the token lets a verifier check it
		// without looking the TSA up (RFC 3161, 2.4.1).
		CertReq: true,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding the timestamp request: %w", err)
	}
	body, err := post(ctx, tsaURL, "application/timestamp-query", req, maxTSAResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTimestampAuthority, err)
	}

	var resp timeStampResp
	if rest, err := asn1.Unmarshal(body, &resp); err != nil || len(rest) > 0 {
		return nil, fmt.Errorf("%w: %s answered with no TimeStampResp", ErrTimestampAuthority, redacted(tsaURL))
	}
	// RFC 3161, 2.4.2: granted (0) and grantedWithMods (1) carry a token,
	// every other status refuses the request.
	if resp.Status.Status != 0 && resp.Status.Status != 1 {
		return nil, fmt.Errorf("%w: %s refused the request with status %d %q", ErrTimestampAuthority, redacted(tsaURL), resp.Status.Status, resp.Status.text())
	}
	token := resp.TimeStampToken.FullBytes
	if len(token) == 0 {
		return nil, fmt.Errorf("%w: %s granted the request with no token", ErrTimestampAuthority, redacted(tsaURL))
	}
	info, err := VerifyTimestampToken(token, digest)
	if err != nil {
		return nil, fmt.Errorf("%w: the token of %s: %w", ErrTimestampAuthority, redacted(tsaURL), err)
	}
	if info.Nonce == nil || info.Nonce.Cmp(nonce) != 0 {
		return nil, fmt.Errorf("%w: the token of %s does not carry the request's nonce (RFC 3161, 2.4.2)", ErrTimestampAuthority, redacted(tsaURL))
	}
	// RFC 3161, 2.3: the TSA's certificate carries the timeStamping extended
	// key usage in a critical extension, and verifiers such as openssl ts
	// refuse a token whose certificate does not.
	if !criticalExtKeyUsage(info.Signer) {
		return nil, fmt.Errorf("%w: the TSA certificate %q of %s does not mark its extended key usage critical (RFC 3161, 2.3)", ErrTimestampAuthority, info.Signer.Subject.CommonName, redacted(tsaURL))
	}
	return token, nil
}

// oidExtKeyUsage is the extended key usage extension (RFC 5280, 4.2.1.12).
var oidExtKeyUsage = asn1.ObjectIdentifier{2, 5, 29, 37}

// criticalExtKeyUsage reports whether cert marks its extended key usage
// extension critical.
func criticalExtKeyUsage(cert *x509.Certificate) bool {
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oidExtKeyUsage) {
			return ext.Critical
		}
	}
	return false
}
