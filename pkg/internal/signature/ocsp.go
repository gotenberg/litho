package signature

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha1" //nolint:gosec // RFC 6960 CertIDs name issuers by SHA-1 hashes; nothing is signed with it.
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"
)

// maxOCSPResponseBytes bounds the body of an OCSP responder's answer.
// Anti-DoS guard, not spec-mandated: a response for one certificate holds a
// few statuses, a signature and at most the responder's certificate chain,
// a few kilobytes.
const maxOCSPResponseBytes = 1 << 20

// oidOCSPBasic is id-pkix-ocsp-basic (RFC 6960, 4.2.1).
var oidOCSPBasic = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 1}

// ocspCertID is the CertID of RFC 6960, 4.1.1.
type ocspCertID struct {
	HashAlgorithm  pkix.AlgorithmIdentifier
	IssuerNameHash []byte
	IssuerKeyHash  []byte
	SerialNumber   *big.Int
}

// ocspRequest is the OCSPRequest of RFC 6960, 4.1.1, for one certificate and
// unsigned.
type ocspRequest struct {
	TBSRequest struct {
		RequestList []struct {
			ReqCert ocspCertID
		}
	}
}

// subjectPublicKeyBits returns the subjectPublicKey BIT STRING contents of
// cert, the key RFC 6960, 4.1.1 hashes into a CertID.
func subjectPublicKeyBits(cert *x509.Certificate) ([]byte, error) {
	var spki struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(cert.RawSubjectPublicKeyInfo, &spki); err != nil {
		return nil, fmt.Errorf("%w: the public key of %q: %v", ErrSignatureMalformed, cert.Subject.CommonName, err)
	}
	return spki.PublicKey.Bytes, nil
}

// buildOCSPRequest returns the DER OCSPRequest asking for the status of cert,
// issued by issuer, with the SHA-1 CertID RFC 5019, 2.1.1 has responders
// accept.
func buildOCSPRequest(cert, issuer *x509.Certificate) ([]byte, error) {
	key, err := subjectPublicKeyBits(issuer)
	if err != nil {
		return nil, err
	}
	nameHash := sha1.Sum(issuer.RawSubject) //nolint:gosec // RFC 6960, 4.1.1 CertID.
	keyHash := sha1.Sum(key)                //nolint:gosec // RFC 6960, 4.1.1 CertID.
	var req ocspRequest
	req.TBSRequest.RequestList = []struct{ ReqCert ocspCertID }{{ReqCert: ocspCertID{
		HashAlgorithm:  pkix.AlgorithmIdentifier{Algorithm: oidSHA1, Parameters: asn1.NullRawValue},
		IssuerNameHash: nameHash[:],
		IssuerKeyHash:  keyHash[:],
		SerialNumber:   cert.SerialNumber,
	}}}
	der, err := asn1.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encoding the OCSP request: %w", err)
	}
	return der, nil
}

// fetchOCSP asks the responders cert names in its authority information
// access (RFC 5280, 4.2.2.1) for its status, and returns the first response
// [checkOCSP] accepts. It returns [ErrValidationData] when none answers
// usably, and [ErrCertificateRevoked] when one reports cert revoked.
func fetchOCSP(ctx context.Context, cert, issuer *x509.Certificate) ([]byte, error) {
	req, err := buildOCSPRequest(cert, issuer)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, server := range cert.OCSPServer {
		body, err := post(ctx, server, "application/ocsp-request", req, maxOCSPResponseBytes)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			errs = append(errs, err)
			continue
		}
		if err := checkOCSP(body, cert, issuer); err != nil {
			if errors.Is(err, ErrCertificateRevoked) {
				return nil, err
			}
			errs = append(errs, fmt.Errorf("the answer of %s: %w", redacted(server), err))
			continue
		}
		return body, nil
	}
	if len(errs) == 0 {
		return nil, nil
	}
	return nil, fmt.Errorf("%w: no OCSP responder of %q answered usably%s", ErrValidationData, cert.Subject.CommonName, because(errs))
}

// checkOCSP returns nil when der is a successful basic OCSP response (RFC
// 6960, 4.2.1) that the issuer of cert, or a responder it delegated OCSP
// signing to (RFC 6960, 4.2.2.2), signed, and that reports cert good. It
// returns [ErrCertificateRevoked] when the response reports cert revoked, and
// [ErrValidationData] for any other response that does not vouch for cert.
func checkOCSP(der []byte, cert, issuer *x509.Certificate) error {
	var resp ocspResponseASN1
	if _, err := asn1.Unmarshal(der, &resp); err != nil {
		return fmt.Errorf("%w: no OCSPResponse: %v", ErrValidationData, err)
	}
	if resp.Status != 0 {
		return fmt.Errorf("%w: OCSP response status %d, not successful (RFC 6960, 4.2.1)", ErrValidationData, resp.Status)
	}
	var respBytes ocspResponseBytesASN1
	if _, err := asn1.Unmarshal(resp.Bytes.Bytes, &respBytes); err != nil {
		return fmt.Errorf("%w: OCSP ResponseBytes: %v", ErrValidationData, err)
	}
	if !respBytes.ResponseType.Equal(oidOCSPBasic) {
		return fmt.Errorf("%w: OCSP response type %v, not id-pkix-ocsp-basic", ErrValidationData, respBytes.ResponseType)
	}
	var basic basicOCSPResponseASN1
	if _, err := asn1.Unmarshal(respBytes.Response, &basic); err != nil {
		return fmt.Errorf("%w: BasicOCSPResponse: %v", ErrValidationData, err)
	}
	var data responseDataASN1
	if _, err := asn1.Unmarshal(basic.TBSResponseData.FullBytes, &data); err != nil {
		return fmt.Errorf("%w: OCSP ResponseData: %v", ErrValidationData, err)
	}

	responder, err := ocspResponder(basic, data, issuer)
	if err != nil {
		return err
	}
	alg, err := x509SignatureAlgorithm(basic.SignatureAlg, responder.PublicKey)
	if err != nil {
		return fmt.Errorf("%w: the OCSP response's signature: %w", ErrValidationData, err)
	}
	if err := responder.CheckSignature(alg, basic.TBSResponseData.FullBytes, basic.Signature.RightAlign()); err != nil {
		return fmt.Errorf("%w: the OCSP response's signature does not verify: %v", ErrValidationData, err)
	}

	key, err := subjectPublicKeyBits(issuer)
	if err != nil {
		return err
	}
	for _, raw := range data.Responses {
		var single singleResponseASN1
		if _, err := asn1.Unmarshal(raw.FullBytes, &single); err != nil {
			continue
		}
		if !certIDNames(single.CertID, cert, issuer, key) {
			continue
		}
		// RFC 6960, 4.2.1: certStatus is good [0], revoked [1] or unknown [2].
		switch single.CertStatus.Tag {
		case 0:
			return nil
		case 1:
			// RevokedInfo opens with its revocationTime.
			var at time.Time
			if _, err := asn1.UnmarshalWithParams(single.CertStatus.Bytes, &at, "generalized"); err == nil {
				return fmt.Errorf("%w: OCSP reports %q revoked on %s", ErrCertificateRevoked, cert.Subject.CommonName, at.Format(time.RFC3339))
			}
			return fmt.Errorf("%w: OCSP reports %q revoked", ErrCertificateRevoked, cert.Subject.CommonName)
		default:
			return fmt.Errorf("%w: OCSP reports the status of %q unknown", ErrValidationData, cert.Subject.CommonName)
		}
	}
	return fmt.Errorf("%w: the OCSP response has no status for %q", ErrValidationData, cert.Subject.CommonName)
}

// ocspResponder returns the certificate that signed an OCSP response: issuer
// itself, or a certificate the response carries that issuer issued for OCSP
// signing (RFC 6960, 4.2.2.2), whichever the responderID names.
func ocspResponder(basic basicOCSPResponseASN1, data responseDataASN1, issuer *x509.Certificate) (*x509.Certificate, error) {
	candidates := []*x509.Certificate{issuer}
	if len(basic.Certs.Bytes) > 0 {
		var raws []asn1.RawValue
		if _, err := asn1.Unmarshal(basic.Certs.FullBytes, &raws); err == nil {
			for _, raw := range raws {
				if c, err := x509.ParseCertificate(raw.FullBytes); err == nil {
					candidates = append(candidates, c)
				}
			}
		}
	}
	for _, c := range candidates {
		if !responderIDNames(data.ResponderID, c) {
			continue
		}
		if c == issuer || bytes.Equal(c.Raw, issuer.Raw) {
			return c, nil
		}
		if c.CheckSignatureFrom(issuer) == nil && slices.Contains(c.ExtKeyUsage, x509.ExtKeyUsageOCSPSigning) {
			return c, nil
		}
	}
	return nil, fmt.Errorf("%w: the OCSP response is signed by neither %q nor a responder it delegated to", ErrValidationData, issuer.Subject.CommonName)
}

// responderIDNames reports whether the ResponderID id, byName [1] or byKey
// [2] (RFC 6960, 4.2.1), names cert.
func responderIDNames(id asn1.RawValue, cert *x509.Certificate) bool {
	switch id.Tag {
	case 1:
		return bytes.Equal(id.Bytes, cert.RawSubject)
	case 2:
		var hash []byte
		if _, err := asn1.Unmarshal(id.Bytes, &hash); err != nil {
			return false
		}
		key, err := subjectPublicKeyBits(cert)
		if err != nil {
			return false
		}
		sum := sha1.Sum(key) //nolint:gosec // RFC 6960, 4.2.1 KeyHash.
		return bytes.Equal(hash, sum[:])
	}
	return false
}

// certIDNames reports whether id names cert, issued by issuer whose public
// key bits are issuerKey (RFC 6960, 4.1.1).
func certIDNames(id certIDASN1, cert, issuer *x509.Certificate, issuerKey []byte) bool {
	if id.SerialNumber == nil || id.SerialNumber.Cmp(cert.SerialNumber) != 0 {
		return false
	}
	hash, err := hashForOID(id.HashAlgorithm)
	if err != nil || !hash.Available() {
		return false
	}
	return bytes.Equal(id.IssuerNameHash, digestOf(hash, issuer.RawSubject)) &&
		bytes.Equal(id.IssuerKeyHash, digestOf(hash, issuerKey))
}

// digestOf returns the hash of data.
func digestOf(hash crypto.Hash, data []byte) []byte {
	h := hash.New()
	h.Write(data)
	return h.Sum(nil)
}

// Signature algorithm OIDs of the OCSP responses and CRLs litho checks (RFC
// 8017, Appendix C; RFC 5758, 3.2; RFC 8410, 3).
var (
	oidSHA1WithRSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
	oidSHA256WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidSHA384WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidSHA512WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidECDSAWithSHA1   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 1}
	oidECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
	oidEd25519         = asn1.ObjectIdentifier{1, 3, 101, 112}
)

// x509SignatureAlgorithm maps a signature AlgorithmIdentifier to the
// [x509.SignatureAlgorithm] that checks it. RSASSA-PSS takes its digest from
// the key type and the parameters' hash, as [signatureAlgorithm] reads them.
func x509SignatureAlgorithm(raw asn1.RawValue, pub any) (x509.SignatureAlgorithm, error) {
	var ai algorithmIdentifier
	if _, err := asn1.Unmarshal(raw.FullBytes, &ai); err != nil {
		return 0, fmt.Errorf("%w: signature algorithm: %v", ErrSignatureMalformed, err)
	}
	for _, m := range []struct {
		oid asn1.ObjectIdentifier
		alg x509.SignatureAlgorithm
	}{
		{oidSHA1WithRSA, x509.SHA1WithRSA},
		{oidSHA256WithRSA, x509.SHA256WithRSA},
		{oidSHA384WithRSA, x509.SHA384WithRSA},
		{oidSHA512WithRSA, x509.SHA512WithRSA},
		{oidECDSAWithSHA1, x509.ECDSAWithSHA1},
		{oidECDSAWithSHA256, x509.ECDSAWithSHA256},
		{oidECDSAWithSHA384, x509.ECDSAWithSHA384},
		{oidECDSAWithSHA512, x509.ECDSAWithSHA512},
		{oidEd25519, x509.PureEd25519},
	} {
		if ai.Algorithm.Equal(m.oid) {
			return m.alg, nil
		}
	}
	if ai.Algorithm.Equal(oidRSASSAPSS) {
		// RFC 4055, 3.1: the parameters name the hash, SHA-1 when absent.
		var params struct {
			Hash algorithmIdentifier `asn1:"optional,explicit,tag:0"`
		}
		hash := crypto.SHA1
		if _, err := asn1.Unmarshal(ai.Parameters.FullBytes, &params); err == nil && params.Hash.Algorithm != nil {
			h, err := hashForObjectID(params.Hash.Algorithm)
			if err != nil {
				return 0, err
			}
			hash = h
		}
		if alg, ok := rsaPSSByHash[hash]; ok {
			return alg, nil
		}
		return 0, fmt.Errorf("%w: RSASSA-PSS over %v", ErrUnsupportedSignatureAlgo, hash)
	}
	return 0, fmt.Errorf("%w: signature algorithm %v with key %T", ErrUnsupportedSignatureAlgo, ai.Algorithm, pub)
}
