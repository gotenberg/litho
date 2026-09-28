package signature

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	_ "crypto/md5" //nolint:gosec // Registers MD5 so legacy signatures can be verified, not produced.
	"crypto/rsa"
	_ "crypto/sha1" //nolint:gosec // Registers SHA-1 so legacy signatures can be verified, not produced.
	_ "crypto/sha256"
	_ "crypto/sha512"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"math/big"
)

// CMS / PKCS#7 algorithm OIDs.
var (
	oidSHA1   = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA224 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 4}
	oidSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	oidMD5    = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 5}

	oidRSASSAPSS = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}

	oidAttrMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
)

// algorithmIdentifier is the minimal AlgorithmIdentifier needed to read a
// digest or signature OID.
type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

// issuerAndSerialNumber identifies a signer certificate by its issuer
// distinguished name and serial number (RFC 5652 §5.3, the version-1
// SignerIdentifier).
type issuerAndSerialNumber struct {
	Issuer       asn1.RawValue
	SerialNumber *big.Int
}

// hashForOID maps a digest AlgorithmIdentifier to its [crypto.Hash].
func hashForOID(raw asn1.RawValue) (crypto.Hash, error) {
	var ai algorithmIdentifier
	if _, err := asn1.Unmarshal(raw.FullBytes, &ai); err != nil {
		return 0, fmt.Errorf("%w: parsing digest algorithm: %v", ErrSignatureMalformed, err)
	}
	return hashForObjectID(ai.Algorithm)
}

// hashForObjectID maps a digest algorithm OID to its [crypto.Hash].
func hashForObjectID(oid asn1.ObjectIdentifier) (crypto.Hash, error) {
	switch {
	case oid.Equal(oidSHA256):
		return crypto.SHA256, nil
	case oid.Equal(oidSHA384):
		return crypto.SHA384, nil
	case oid.Equal(oidSHA512):
		return crypto.SHA512, nil
	case oid.Equal(oidSHA224):
		return crypto.SHA224, nil
	case oid.Equal(oidSHA1):
		return crypto.SHA1, nil
	case oid.Equal(oidMD5):
		return crypto.MD5, nil
	}
	return 0, fmt.Errorf("%w: digest OID %v", ErrUnsupportedSignatureAlgo, oid)
}

// Digest-to-SignatureAlgorithm tables, keyed by [crypto.Hash], for each
// supported public-key scheme.
var (
	rsaPKCS1ByHash = map[crypto.Hash]x509.SignatureAlgorithm{
		crypto.SHA1:   x509.SHA1WithRSA,
		crypto.SHA256: x509.SHA256WithRSA,
		crypto.SHA384: x509.SHA384WithRSA,
		crypto.SHA512: x509.SHA512WithRSA,
		crypto.MD5:    x509.MD5WithRSA,
	}
	rsaPSSByHash = map[crypto.Hash]x509.SignatureAlgorithm{
		crypto.SHA256: x509.SHA256WithRSAPSS,
		crypto.SHA384: x509.SHA384WithRSAPSS,
		crypto.SHA512: x509.SHA512WithRSAPSS,
	}
	ecdsaByHash = map[crypto.Hash]x509.SignatureAlgorithm{
		crypto.SHA1:   x509.ECDSAWithSHA1,
		crypto.SHA256: x509.ECDSAWithSHA256,
		crypto.SHA384: x509.ECDSAWithSHA384,
		crypto.SHA512: x509.ECDSAWithSHA512,
	}
)

// signatureAlgorithm picks the [x509.SignatureAlgorithm] from the signer's
// public-key type and the digest the SignerInfo declared. The signature
// AlgorithmIdentifier is consulted only to detect RSASSA-PSS; otherwise
// the key type plus digest fully determine the scheme, which covers both
// the generic key OIDs (rsaEncryption, id-ecPublicKey) and the combined
// OIDs (sha256WithRSAEncryption, ecdsa-with-SHA256, ...).
func signatureAlgorithm(raw asn1.RawValue, pub any, hash crypto.Hash) (x509.SignatureAlgorithm, error) {
	isPSS := false
	var ai algorithmIdentifier
	if _, err := asn1.Unmarshal(raw.FullBytes, &ai); err == nil {
		isPSS = ai.Algorithm.Equal(oidRSASSAPSS)
	}

	var table map[crypto.Hash]x509.SignatureAlgorithm
	switch pub.(type) {
	case *rsa.PublicKey:
		if isPSS {
			table = rsaPSSByHash
		} else {
			table = rsaPKCS1ByHash
		}
	case *ecdsa.PublicKey:
		table = ecdsaByHash
	case ed25519.PublicKey:
		return x509.PureEd25519, nil
	default:
		return 0, fmt.Errorf("%w: key type %T", ErrUnsupportedSignatureAlgo, pub)
	}

	if alg, ok := table[hash]; ok {
		return alg, nil
	}
	return 0, fmt.Errorf("%w: key type %T with digest %v", ErrUnsupportedSignatureAlgo, pub, hash)
}

// reTagSet returns a copy of a DER [0] IMPLICIT element re-tagged as a
// universal SET (0x31). RFC 5652 §5.4 requires verifying the signature
// over the SignedAttributes encoded with the SET OF tag, not the IMPLICIT
// [0] tag carried in the SignerInfo. The content and length octets are
// identical; only the leading tag byte changes.
func reTagSet(fullBytes []byte) []byte {
	if len(fullBytes) == 0 {
		return nil
	}
	out := make([]byte, len(fullBytes))
	copy(out, fullBytes)
	out[0] = 0x31 // universal, constructed, SET OF
	return out
}

// signedAttrValue returns the first value of the signed attribute with the
// given OID. The attributes live in the [0] IMPLICIT content of
// SignedAttributes; each Attribute is a SEQUENCE the parser walks in turn.
func signedAttrValue(signedAttrs asn1.RawValue, oid asn1.ObjectIdentifier) ([]byte, bool) {
	rest := signedAttrs.Bytes
	for len(rest) > 0 {
		var a attribute
		var err error
		rest, err = asn1.Unmarshal(rest, &a)
		if err != nil {
			return nil, false
		}
		if !a.Type.Equal(oid) {
			continue
		}
		var val []byte
		if _, err := asn1.Unmarshal(a.Values.Bytes, &val); err != nil {
			return nil, false
		}
		return val, true
	}
	return nil, false
}

// matchSignerCert resolves the signing certificate referenced by a
// SignerInfo's SignerIdentifier from the certificate set embedded in the
// PKCS#7 block. It handles both the version-1 IssuerAndSerialNumber form
// and the version-3 [0] SubjectKeyIdentifier form, and falls back to the
// sole certificate when only one is present.
func matchSignerCert(sid asn1.RawValue, certs []*x509.Certificate) *x509.Certificate {
	var ias issuerAndSerialNumber
	if _, err := asn1.Unmarshal(sid.FullBytes, &ias); err == nil && ias.SerialNumber != nil {
		for _, c := range certs {
			if c.SerialNumber.Cmp(ias.SerialNumber) == 0 && bytes.Equal(c.RawIssuer, ias.Issuer.FullBytes) {
				return c
			}
		}
	}
	// Version-3 SignerIdentifier: [0] SubjectKeyIdentifier (an OCTET
	// STRING carried directly in the context-tagged value).
	if sid.Class == asn1.ClassContextSpecific && sid.Tag == 0 && len(sid.Bytes) > 0 {
		for _, c := range certs {
			if len(c.SubjectKeyId) > 0 && bytes.Equal(c.SubjectKeyId, sid.Bytes) {
				return c
			}
		}
	}
	if len(certs) == 1 {
		return certs[0]
	}
	return nil
}

// verifyCMSSignature verifies a detached PKCS#7/CMS signature (RFC 5652)
// over content, the concatenated bytes of the signature's ByteRange. It
// resolves the signer certificate from the embedded set, recomputes the
// digest with the algorithm the SignerInfo declares, checks it against the
// signed messageDigest attribute, and verifies the signature value against
// the signer's public key. Returns the matched signer certificate so the
// caller can pin chain validation to it.
//
// Returns [ErrSignatureMalformed] when the structure cannot be parsed,
// [ErrSignatureInvalid] when the digest or signature does not verify, and
// [ErrUnsupportedSignatureAlgo] for algorithms the verifier does not
// implement.
func verifyCMSSignature(der, content []byte, certs []*x509.Certificate) (*x509.Certificate, error) {
	var ci contentInfo
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return nil, fmt.Errorf("%w: ContentInfo: %v", ErrSignatureMalformed, err)
	}
	var sd signedDataFull
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, fmt.Errorf("%w: SignedData: %v", ErrSignatureMalformed, err)
	}
	if len(sd.SignerInfos) == 0 {
		return nil, fmt.Errorf("%w: no SignerInfo present", ErrSignatureMalformed)
	}
	si := sd.SignerInfos[0]

	signer := matchSignerCert(si.SID, certs)
	if signer == nil {
		return nil, fmt.Errorf("%w: signer certificate not found", ErrSignatureMalformed)
	}

	hash, err := hashForOID(si.DigestAlgorithm)
	if err != nil {
		return signer, err
	}
	if !hash.Available() {
		return signer, fmt.Errorf("%w: digest %v not compiled in", ErrUnsupportedSignatureAlgo, hash)
	}

	h := hash.New()
	h.Write(content)
	contentDigest := h.Sum(nil)

	signedPayload := content
	if len(si.SignedAttrs.Bytes) > 0 {
		// With signed attributes the signature is over the attributes,
		// and the messageDigest attribute must equal the content digest.
		md, ok := signedAttrValue(si.SignedAttrs, oidAttrMessageDigest)
		if !ok {
			return signer, fmt.Errorf("%w: missing messageDigest attribute", ErrSignatureMalformed)
		}
		if !bytes.Equal(md, contentDigest) {
			return signer, fmt.Errorf("%w: messageDigest does not match the signed byte range", ErrSignatureInvalid)
		}
		signedPayload = reTagSet(si.SignedAttrs.FullBytes)
	}

	sigAlg, err := signatureAlgorithm(si.SignatureAlgorithm, signer.PublicKey, hash)
	if err != nil {
		return signer, err
	}
	if err := signer.CheckSignature(sigAlg, signedPayload, si.Signature); err != nil {
		return signer, fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	}
	return signer, nil
}

// cmsDigestHash returns the digest algorithm declared by the first
// SignerInfo of a PKCS#7/CMS blob, or [crypto.SHA256] when it cannot be
// determined. It lets the informational document hash be computed with the
// algorithm the signature actually uses instead of a hardcoded one.
func cmsDigestHash(der []byte) crypto.Hash {
	var ci contentInfo
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return crypto.SHA256
	}
	var sd signedDataFull
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil || len(sd.SignerInfos) == 0 {
		return crypto.SHA256
	}
	if h, err := hashForOID(sd.SignerInfos[0].DigestAlgorithm); err == nil && h.Available() {
		return h
	}
	return crypto.SHA256
}
