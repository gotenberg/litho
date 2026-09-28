package signature

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"slices"
	"time"
)

// OIDs of the SignedData [CMSSigner] builds (RFC 5652, RFC 5754, RFC 5758,
// RFC 8017, RFC 5035).
var (
	oidCMSData            = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidCMSSignedData      = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidCMSAttrContentType = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidCMSAttrSigningTime = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
	oidCMSAttrSigningCert = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	oidCMSSHA256WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidCMSECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
)

// cmsFixedOverhead bounds the bytes of a SignedData [CMSSigner] writes beyond
// its certificates, its signature value and the two copies of the signer's
// issuer name: the ContentInfo, SignedData and SignerInfo headers, three
// algorithm identifiers and four signed attributes come to about 250 bytes.
// Not spec-mandated: it sizes the /Contents placeholder, and twice the need
// leaves room for longer length fields without bloating the output.
const cmsFixedOverhead = 512

// CMSSigner builds the detached CMS SignedData (RFC 5652, 5) a PDF signature
// holds in its /Contents (ISO 32000-1, 12.8.3.3.1; ISO 32000-2, 12.8.3.4.1):
// one SignerInfo over a SHA-256 digest, identified by issuer and serial number,
// with the content-type, signing-time and message-digest signed attributes RFC
// 5652, 11 defines and the ESS signing-certificate-v2 attribute (RFC 5035, 3)
// that CAdES (ETSI EN 319 122-1, 5.2.2.3) and PAdES require, and the signing
// certificate followed by its chain in the certificates field.
//
// It signs with an RSA key under PKCS #1 v1.5 (RFC 8017, 8.2) or an ECDSA key
// on P-256, P-384 or P-521 (RFC 5753), both over SHA-256, the digest the PDF
// signing flow hands it. It takes no timestamp: an RFC 3161 token needs a
// timestamp authority reached over the network.
//
// A CMSSigner is safe for concurrent use when its key is: it holds no mutable
// state, and each call builds its own SignedData.
type CMSSigner struct {
	cert     *x509.Certificate
	chain    []*x509.Certificate
	key      crypto.Signer
	sigAlg   []byte
	sigBytes int
}

// NewCMSSigner returns a [CMSSigner] signing with key as the subject of cert,
// the certificates of chain written after cert. It returns
// [ErrSignerKeyUnsupported] for a key other than RSA or ECDSA on a NIST curve,
// [ErrSignerKeyMismatch] when key is not the private half of cert's public key,
// and [ErrSignerCertificateUsage] when cert restricts its key to uses that
// exclude signing.
func NewCMSSigner(cert *x509.Certificate, key crypto.Signer, chain []*x509.Certificate) (*CMSSigner, error) {
	if cert == nil {
		return nil, fmt.Errorf("%w: no certificate", ErrSignerKeyMismatch)
	}
	if key == nil {
		return nil, fmt.Errorf("%w: no private key", ErrSignerKeyMismatch)
	}
	s := &CMSSigner{cert: cert, key: key}

	var err error
	switch pub := key.Public().(type) {
	case *rsa.PublicKey:
		s.sigAlg, err = asn1.Marshal(struct {
			Algorithm  asn1.ObjectIdentifier
			Parameters asn1.RawValue
		}{oidCMSSHA256WithRSA, asn1.NullRawValue})
		s.sigBytes = pub.Size()
	case *ecdsa.PublicKey:
		switch pub.Curve {
		case elliptic.P256(), elliptic.P384(), elliptic.P521():
		default:
			return nil, fmt.Errorf("%w: ECDSA on curve %s", ErrSignerKeyUnsupported, pub.Curve.Params().Name)
		}
		s.sigAlg, err = asn1.Marshal(struct{ Algorithm asn1.ObjectIdentifier }{oidCMSECDSAWithSHA256})
		// Ecdsa-Sig-Value is a SEQUENCE of two INTEGERs of the curve's
		// size, each one byte longer when its high bit is set (RFC 5753,
		// 7.2), plus the tag and length octets of all three.
		size := (pub.Curve.Params().BitSize + 7) / 8
		s.sigBytes = 2*(size+1) + 9
	default:
		return nil, fmt.Errorf("%w: %T", ErrSignerKeyUnsupported, pub)
	}
	if err != nil {
		return nil, fmt.Errorf("encoding the signature algorithm: %w", err)
	}

	pub, ok := key.Public().(interface{ Equal(crypto.PublicKey) bool })
	if !ok || !pub.Equal(cert.PublicKey) {
		return nil, fmt.Errorf("%w: the private key does not match the certificate of %q", ErrSignerKeyMismatch, cert.Subject.CommonName)
	}

	// RFC 5280, 4.2.1.3: a certificate that lists key usages allows its key
	// those uses alone, and a signature over a document needs
	// digitalSignature or nonRepudiation (contentCommitment).
	if cert.KeyUsage != 0 && cert.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageContentCommitment) == 0 {
		return nil, fmt.Errorf("%w: the certificate of %q allows neither digitalSignature nor nonRepudiation", ErrSignerCertificateUsage, cert.Subject.CommonName)
	}

	for i, c := range chain {
		if c == nil {
			return nil, fmt.Errorf("%w: chain certificate %d is nil", ErrSignerKeyMismatch, i+1)
		}
		if bytes.Equal(c.Raw, cert.Raw) || slices.ContainsFunc(s.chain, func(d *x509.Certificate) bool { return bytes.Equal(c.Raw, d.Raw) }) {
			continue
		}
		s.chain = append(s.chain, c)
	}
	return s, nil
}

// Certificate returns the signing certificate.
func (s *CMSSigner) Certificate() *x509.Certificate { return s.cert }

// EstimatedSize returns an upper bound on the bytes of a SignedData
// [CMSSigner.Sign] returns.
func (s *CMSSigner) EstimatedSize() int {
	n := len(s.cert.Raw) + s.sigBytes + 2*len(s.cert.RawIssuer) + cmsFixedOverhead
	for _, c := range s.chain {
		n += len(c.Raw)
	}
	return n
}

// Sign returns the DER-encoded ContentInfo of a detached SignedData over the
// content whose SHA-256 is digest, signed at the time at.
//
// With cades it returns the CAdES signature an ETSI.CAdES.detached signature
// dictionary holds (ISO 32000-2, 12.8.3.4; ETSI EN 319 142-1, PAdES baseline
// B-B): the same SignedData without the signing-time attribute, which ETSI EN
// 319 142-1, 5.2.1 and 6.3, Table 1 forbid there, the signature dictionary's
// /M carrying the claimed signing time instead. at is then unused.
func (s *CMSSigner) Sign(digest []byte, at time.Time, cades bool) ([]byte, error) {
	if len(digest) != sha256.Size {
		return nil, fmt.Errorf("%w: a %d-byte digest is not a SHA-256", ErrSignatureMalformed, len(digest))
	}
	attrs, err := s.signedAttributes(digest, at, cades)
	if err != nil {
		return nil, err
	}
	// RFC 5652, 5.4: the signature covers the DER encoding of the
	// SignedAttributes with the EXPLICIT SET OF tag, not the IMPLICIT [0] the
	// SignerInfo carries them under.
	attrSet, err := derWrap(asn1.ClassUniversal, asn1.TagSet, attrs)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(attrSet)
	sig, err := s.key.Sign(rand.Reader, h[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("signing the signed attributes: %w", err)
	}

	sha256Alg, err := asn1.Marshal(struct{ Algorithm asn1.ObjectIdentifier }{oidSHA256})
	if err != nil {
		return nil, fmt.Errorf("encoding the digest algorithm: %w", err)
	}
	sid, err := issuerSerial(s.cert, false)
	if err != nil {
		return nil, err
	}
	signedAttrs := slices.Clone(attrSet)
	signedAttrs[0] = 0xA0 // [0] IMPLICIT, constructed
	sigValue, err := asn1.Marshal(sig)
	if err != nil {
		return nil, fmt.Errorf("encoding the signature value: %w", err)
	}
	version, err := asn1.Marshal(1)
	if err != nil {
		return nil, fmt.Errorf("encoding the version: %w", err)
	}
	signerInfo, err := derWrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(version, sid, sha256Alg, signedAttrs, s.sigAlg, sigValue))
	if err != nil {
		return nil, err
	}

	digestAlgs, err := derWrap(asn1.ClassUniversal, asn1.TagSet, sha256Alg)
	if err != nil {
		return nil, err
	}
	dataType, err := asn1.Marshal(oidCMSData)
	if err != nil {
		return nil, fmt.Errorf("encoding the content type: %w", err)
	}
	// A detached signature carries its content type and no content (RFC
	// 5652, 5.2; ISO 32000-1, 12.8.3.3.1).
	encap, err := derWrap(asn1.ClassUniversal, asn1.TagSequence, dataType)
	if err != nil {
		return nil, err
	}
	raw := slices.Clone(s.cert.Raw)
	for _, c := range s.chain {
		raw = append(raw, c.Raw...)
	}
	certs, err := derWrap(asn1.ClassContextSpecific, 0, raw)
	if err != nil {
		return nil, err
	}
	signerInfos, err := derWrap(asn1.ClassUniversal, asn1.TagSet, signerInfo)
	if err != nil {
		return nil, err
	}
	signed, err := derWrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(version, digestAlgs, encap, certs, signerInfos))
	if err != nil {
		return nil, err
	}

	signedType, err := asn1.Marshal(oidCMSSignedData)
	if err != nil {
		return nil, fmt.Errorf("encoding the content type: %w", err)
	}
	content, err := derWrap(asn1.ClassContextSpecific, 0, signed)
	if err != nil {
		return nil, err
	}
	return derWrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(signedType, content))
}

// signedAttributes returns the content octets of the SignedAttributes: the
// DER encodings of its four Attributes, or three without the signing-time
// under cades (see [CMSSigner.Sign]), sorted as DER sorts a SET OF (X.690,
// 11.6).
func (s *CMSSigner) signedAttributes(digest []byte, at time.Time, cades bool) ([]byte, error) {
	contentType, err := asn1.Marshal(oidCMSData)
	if err != nil {
		return nil, fmt.Errorf("encoding the content type: %w", err)
	}
	// RFC 5652, 11.3: UTCTime for the years 1950 to 2049, GeneralizedTime
	// outside them, which encoding/asn1 picks by the year.
	signingTime, err := asn1.Marshal(at.UTC().Truncate(time.Second))
	if err != nil {
		return nil, fmt.Errorf("encoding the signing time: %w", err)
	}
	messageDigest, err := asn1.Marshal(digest)
	if err != nil {
		return nil, fmt.Errorf("encoding the message digest: %w", err)
	}
	signingCert, err := s.signingCertificateV2()
	if err != nil {
		return nil, err
	}

	type attr struct {
		oid   asn1.ObjectIdentifier
		value []byte
	}
	list := []attr{
		{oidCMSAttrContentType, contentType},
		{oidAttrMessageDigest, messageDigest},
		{oidCMSAttrSigningCert, signingCert},
	}
	if !cades {
		list = append(list, attr{oidCMSAttrSigningTime, signingTime})
	}
	var attrs [][]byte
	for _, a := range list {
		oid, err := asn1.Marshal(a.oid)
		if err != nil {
			return nil, fmt.Errorf("encoding attribute %v: %w", a.oid, err)
		}
		values, err := derWrap(asn1.ClassUniversal, asn1.TagSet, a.value)
		if err != nil {
			return nil, err
		}
		attr, err := derWrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(oid, values))
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, attr)
	}
	slices.SortFunc(attrs, bytes.Compare)
	return slices.Concat(attrs...), nil
}

// signingCertificateV2 returns the SigningCertificateV2 value of RFC 5035, 3:
// one ESSCertIDv2 naming the signing certificate by its SHA-256 hash, the
// default hashAlgorithm and therefore absent from DER, and by its issuer and
// serial number.
func (s *CMSSigner) signingCertificateV2() ([]byte, error) {
	certHash := sha256.Sum256(s.cert.Raw)
	hash, err := asn1.Marshal(certHash[:])
	if err != nil {
		return nil, fmt.Errorf("encoding the certificate hash: %w", err)
	}
	serial, err := issuerSerial(s.cert, true)
	if err != nil {
		return nil, err
	}
	certID, err := derWrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(hash, serial))
	if err != nil {
		return nil, err
	}
	certIDs, err := derWrap(asn1.ClassUniversal, asn1.TagSequence, certID)
	if err != nil {
		return nil, err
	}
	return derWrap(asn1.ClassUniversal, asn1.TagSequence, certIDs)
}

// issuerSerial returns the DER of cert's issuer and serial number: the CMS
// IssuerAndSerialNumber (RFC 5652, 10.2.4) or, with generalNames, the ESS
// IssuerSerial (RFC 5035, 4), which holds the issuer as a GeneralNames with
// one directoryName (RFC 5280, 4.2.1.6).
func issuerSerial(cert *x509.Certificate, generalNames bool) ([]byte, error) {
	issuer := cert.RawIssuer
	if generalNames {
		dirName, err := derWrap(asn1.ClassContextSpecific, 4, issuer)
		if err != nil {
			return nil, err
		}
		if issuer, err = derWrap(asn1.ClassUniversal, asn1.TagSequence, dirName); err != nil {
			return nil, err
		}
	}
	serial, err := asn1.Marshal(cert.SerialNumber)
	if err != nil {
		return nil, fmt.Errorf("encoding the serial number: %w", err)
	}
	return derWrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(issuer, serial))
}

// derWrap returns the DER encoding of a constructed element of the given
// class and tag holding content.
func derWrap(class, tag int, content []byte) ([]byte, error) {
	out, err := asn1.Marshal(asn1.RawValue{Class: class, Tag: tag, IsCompound: true, Bytes: content})
	if err != nil {
		return nil, fmt.Errorf("encoding a [%d %d] element: %w", class, tag, err)
	}
	return out, nil
}
