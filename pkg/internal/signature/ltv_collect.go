package signature

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
)

// maxCRLBytes bounds the body of a CRL distribution point's answer. Anti-DoS
// guard, not spec-mandated: the CRLs of the largest public authorities stay
// in the tens of megabytes, and the CRL is embedded in the output whole.
const maxCRLBytes = 32 << 20

// maxIssuerCertBytes bounds the body of an authority information access
// caIssuers answer. Anti-DoS guard, not spec-mandated: it holds one
// certificate, or a few in a certs-only CMS (RFC 5280, 4.2.2.1).
const maxIssuerCertBytes = 1 << 20

// maxChainLength bounds the certificates walked from a signer up to its root.
// Anti-DoS guard, not spec-mandated: public hierarchies are three or four
// certificates deep, and the walk also stops at a certificate it met already.
const maxChainLength = 16

// ValidationData is the validation material a document security store holds
// (ISO 32000-2, 12.8.4.3): DER certificates, OCSP responses (RFC 6960, 4.2.1)
// and CRLs (RFC 5280, 5.1).
type ValidationData struct {
	Certs, OCSPs, CRLs [][]byte
}

// add appends each of values to *list that it does not hold already.
func add(list *[][]byte, values ...[]byte) {
	for _, v := range values {
		dup := false
		for _, have := range *list {
			dup = dup || bytes.Equal(have, v)
		}
		if !dup {
			*list = append(*list, v)
		}
	}
}

// CollectValidationData returns the validation data of the signature whose
// /Contents is der: the certificates of the signer's chain and of the chain
// of the timestamp authority that timestamped it, and for each certificate
// below a root an OCSP response or a CRL that vouches it is not revoked, what
// PAdES baseline B-LT asks a DSS to hold (ETSI EN 319 142-1, 6.3, Table 1;
// 5.4.2).
//
// Without supplied, it fetches: an issuer missing from the SignedData from
// the certificate's authority information access caIssuers URL, an OCSP
// response from its OCSP responders, and failing that a CRL from its CRL
// distribution points (RFC 5280, 4.2.1.13 and 4.2.2.1). With supplied it
// fetches nothing and draws from the certificates, OCSP responses and CRLs
// given. Either way an OCSP response counts only once its issuer or a
// responder it delegated to signed it (RFC 6960, 4.2.2.2), and a CRL once its
// issuer signed it.
//
// It returns [ErrCertificateRevoked] when a response or a CRL reports a
// certificate revoked, and [ErrValidationData] when a certificate's issuer
// cannot be found or nothing vouches for it.
func CollectValidationData(ctx context.Context, der []byte, supplied *ValidationData) (*ValidationData, error) {
	certs, err := parsePKCS7Certificates(der)
	if err != nil {
		return nil, fmt.Errorf("%w: the signature's certificates: %w", ErrValidationData, err)
	}
	signer, err := firstSigner(der, certs)
	if err != nil {
		return nil, err
	}
	pool := append([]*x509.Certificate(nil), certs...)
	leaves := []*x509.Certificate{signer}
	if token := signatureTimestampToken(der); token != nil {
		tsaCerts, err := parsePKCS7Certificates(token)
		if err == nil {
			pool = append(pool, tsaCerts...)
			if tsa, err := firstSigner(token, tsaCerts); err == nil {
				leaves = append(leaves, tsa)
			}
		}
	}
	if supplied != nil {
		for i, raw := range supplied.Certs {
			c, err := x509.ParseCertificate(raw)
			if err != nil {
				return nil, fmt.Errorf("%w: supplied certificate %d: %v", ErrValidationData, i+1, err)
			}
			pool = append(pool, c)
		}
	}

	out := &ValidationData{}
	crls := make(map[string][]byte)
	seen := make(map[string]bool)
	for _, leaf := range leaves {
		cur := leaf
		for range maxChainLength {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("collecting validation data: %w", err)
			}
			if seen[string(cur.Raw)] {
				break
			}
			seen[string(cur.Raw)] = true
			add(&out.Certs, cur.Raw)
			// RFC 5280, 6.1: a self-signed certificate is a trust anchor,
			// which no revocation data vouches for.
			if selfSigned(cur) {
				break
			}
			issuer, err := findIssuer(ctx, cur, &pool, supplied == nil)
			if err != nil {
				return nil, err
			}
			if err := vouch(ctx, out, cur, issuer, supplied, crls); err != nil {
				return nil, err
			}
			cur = issuer
		}
	}
	return out, nil
}

// firstSigner returns the certificate of der's first SignerInfo among certs.
func firstSigner(der []byte, certs []*x509.Certificate) (*x509.Certificate, error) {
	var ci contentInfo
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return nil, fmt.Errorf("%w: ContentInfo: %v", ErrSignatureMalformed, err)
	}
	var sd signedDataFull
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil || len(sd.SignerInfos) == 0 {
		return nil, fmt.Errorf("%w: no SignerInfo", ErrSignatureMalformed)
	}
	signer := matchSignerCert(sd.SignerInfos[0].SID, certs)
	if signer == nil {
		return nil, fmt.Errorf("%w: the signer's certificate is not in the SignedData", ErrValidationData)
	}
	return signer, nil
}

// selfSigned reports whether cert names itself its issuer and its own key
// verifies its signature.
func selfSigned(cert *x509.Certificate) bool {
	return bytes.Equal(cert.RawIssuer, cert.RawSubject) && cert.CheckSignatureFrom(cert) == nil
}

// findIssuer returns the certificate of *pool that issued cert, fetching it
// from cert's caIssuers URLs into *pool when fetch is set and the pool holds
// none.
func findIssuer(ctx context.Context, cert *x509.Certificate, pool *[]*x509.Certificate, fetch bool) (*x509.Certificate, error) {
	issuedBy := func(c *x509.Certificate) bool {
		return bytes.Equal(c.RawSubject, cert.RawIssuer) && cert.CheckSignatureFrom(c) == nil
	}
	for _, c := range *pool {
		if issuedBy(c) {
			return c, nil
		}
	}
	var errs []error
	if fetch {
		for _, u := range cert.IssuingCertificateURL {
			body, err := get(ctx, u, maxIssuerCertBytes)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				errs = append(errs, err)
				continue
			}
			// RFC 5280, 4.2.2.1: one DER certificate, or a certs-only CMS
			// message.
			found, err := x509.ParseCertificates(body)
			if err != nil {
				if found, err = parsePKCS7Certificates(body); err != nil {
					errs = append(errs, fmt.Errorf("the answer of %s holds no certificate", redacted(u)))
					continue
				}
			}
			*pool = append(*pool, found...)
			for _, c := range found {
				if issuedBy(c) {
					return c, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("%w: no issuer certificate for %q%s", ErrValidationData, cert.Subject.CommonName, because(errs))
}

// because returns ": " and errs joined, or "" when there are none.
func because(errs []error) string {
	if len(errs) == 0 {
		return ""
	}
	return ": " + errors.Join(errs...).Error()
}

// vouch adds to out an OCSP response or a CRL that vouches cert, issued by
// issuer, is not revoked, from supplied or, when it is nil, fetched.
// crls holds the CRLs fetched so far, by URL.
func vouch(ctx context.Context, out *ValidationData, cert, issuer *x509.Certificate, supplied *ValidationData, crls map[string][]byte) error {
	if supplied != nil {
		for _, o := range supplied.OCSPs {
			err := checkOCSP(o, cert, issuer)
			if errors.Is(err, ErrCertificateRevoked) {
				return err
			}
			if err == nil {
				add(&out.OCSPs, o)
				return nil
			}
		}
		for _, c := range supplied.CRLs {
			err := checkCRL(c, cert, issuer)
			if errors.Is(err, ErrCertificateRevoked) {
				return err
			}
			if err == nil {
				add(&out.CRLs, c)
				return nil
			}
		}
		return fmt.Errorf("%w: no supplied OCSP response or CRL vouches for %q", ErrValidationData, cert.Subject.CommonName)
	}

	var errs []error
	resp, err := fetchOCSP(ctx, cert, issuer)
	switch {
	case errors.Is(err, ErrCertificateRevoked) || ctx.Err() != nil:
		return err
	case err != nil:
		errs = append(errs, err)
	case resp != nil:
		add(&out.OCSPs, resp)
		return nil
	}
	for _, u := range cert.CRLDistributionPoints {
		// Certificates of one issuer share its CRL, which is fetched once:
		// a second copy, newer by a CRL number, would only bloat the store.
		body, ok := crls[u]
		if !ok {
			var err error
			if body, err = get(ctx, u, maxCRLBytes); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				errs = append(errs, err)
				continue
			}
			crls[u] = body
		}
		err = checkCRL(body, cert, issuer)
		if errors.Is(err, ErrCertificateRevoked) {
			return err
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("the CRL of %s: %w", redacted(u), err))
			continue
		}
		add(&out.CRLs, body)
		return nil
	}
	return fmt.Errorf("%w: no OCSP response or CRL vouches for %q%s", ErrValidationData, cert.Subject.CommonName, because(errs))
}

// checkCRL returns nil when der is a CRL issuer signed that does not list
// cert (RFC 5280, 5), [ErrCertificateRevoked] when it lists it, and
// [ErrValidationData] for anything else.
func checkCRL(der []byte, cert, issuer *x509.Certificate) error {
	crl, err := x509.ParseRevocationList(der)
	if err != nil {
		return fmt.Errorf("%w: no CRL: %v", ErrValidationData, err)
	}
	if !bytes.Equal(crl.RawIssuer, issuer.RawSubject) {
		return fmt.Errorf("%w: the CRL is not %q's", ErrValidationData, issuer.Subject.CommonName)
	}
	if err := crl.CheckSignatureFrom(issuer); err != nil {
		return fmt.Errorf("%w: the CRL's signature does not verify: %v", ErrValidationData, err)
	}
	for _, e := range crl.RevokedCertificateEntries {
		if e.SerialNumber != nil && e.SerialNumber.Cmp(cert.SerialNumber) == 0 {
			return fmt.Errorf("%w: the CRL of %q lists %q revoked on %s", ErrCertificateRevoked, issuer.Subject.CommonName, cert.Subject.CommonName, e.RevocationTime.Format("2006-01-02T15:04:05Z07:00"))
		}
	}
	return nil
}
