package signature

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"math/big"
	"slices"
	"time"
)

// TimestampInfo is what a verified TimeStampToken states.
type TimestampInfo struct {
	// Time is the TSTInfo genTime.
	Time time.Time
	// Nonce is the TSTInfo nonce, nil when it carries none.
	Nonce *big.Int
	// Signer is the TSA certificate that signed the token.
	Signer *x509.Certificate
	// Certificates are the certificates the token carries.
	Certificates []*x509.Certificate
}

// VerifyTimestampToken checks that token is an RFC 3161 TimeStampToken over
// the SHA-256 digest and returns what it states (see [verifyTimestampToken]).
// A token imprinting another algorithm is refused with [ErrSignatureInvalid]:
// litho asks for SHA-256.
func VerifyTimestampToken(token, digest []byte) (*TimestampInfo, error) {
	return verifyTimestampToken(token, func(hash crypto.Hash, imprint []byte) error {
		if hash != crypto.SHA256 {
			return fmt.Errorf("%w: the token imprints a %v digest, not the SHA-256 requested", ErrSignatureInvalid, hash)
		}
		if !bytes.Equal(imprint, digest) {
			return fmt.Errorf("%w: the token imprints another digest than the one timestamped", ErrSignatureInvalid)
		}
		return nil
	})
}

// verifyTimestampOver checks that token is an RFC 3161 TimeStampToken over
// data, hashed with the algorithm its imprint names, and returns what it
// states (see [verifyTimestampToken]).
func verifyTimestampOver(token, data []byte) (*TimestampInfo, error) {
	return verifyTimestampToken(token, func(hash crypto.Hash, imprint []byte) error {
		if !hash.Available() {
			return fmt.Errorf("%w: digest %v not compiled in", ErrUnsupportedSignatureAlgo, hash)
		}
		h := hash.New()
		h.Write(data)
		if !bytes.Equal(imprint, h.Sum(nil)) {
			return fmt.Errorf("%w: the token imprints another digest than the timestamped data's", ErrSignatureInvalid)
		}
		return nil
	})
}

// signatureTimestampToken returns the first id-aa-signatureTimeStampToken
// unsigned attribute of the first SignerInfo of der (RFC 3161, Appendix A),
// nil when it holds none.
func signatureTimestampToken(der []byte) []byte {
	var ci contentInfo
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return nil
	}
	var sd signedDataFull
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil || len(sd.SignerInfos) == 0 {
		return nil
	}
	for _, attr := range sd.SignerInfos[0].UnsignedAttrs {
		if attr.Type.Equal(oidAttrTimeStampToken) && len(attr.Values.Bytes) > 0 {
			return attr.Values.Bytes
		}
	}
	return nil
}

// signatureTimestamp returns the timestamp of the signature whose /Contents
// is der and whose /ByteRange covers content, nil for a signature that
// carries none. The timestamp of a document timestamp is der itself, a token
// over content (ISO 32000-2, 12.8.5); that of another signature is the token
// in its SignerInfo's unsigned attributes, over its signature value (RFC
// 3161, Appendix A). A token that does not verify is returned as it reads,
// beside the error that says why.
func signatureTimestamp(der, content []byte, docTimestamp bool) (*TimestampInfo, error) {
	token := der
	if !docTimestamp {
		if token = signatureTimestampToken(der); token == nil {
			return nil, nil
		}
	}
	unverified := func(err error) (*TimestampInfo, error) {
		parsed, perr := parseTimestampToken(token)
		if perr != nil {
			return nil, err
		}
		return &TimestampInfo{Time: parsed.Time, Signer: parsed.SignerCert}, err
	}
	data := content
	if !docTimestamp {
		value, err := SignatureValue(der)
		if err != nil {
			return unverified(fmt.Errorf("finding the timestamped signature value: %w", err))
		}
		data = value
	} else if content == nil {
		return unverified(fmt.Errorf("%w: the document timestamp's /ByteRange covers no bytes of the file", ErrSignatureInvalid))
	}
	info, err := verifyTimestampOver(token, data)
	if err != nil {
		return unverified(err)
	}
	return info, nil
}

// verifyTimestampToken checks that token is an RFC 3161 TimeStampToken whose
// imprint checkImprint accepts, and returns what it states: a CMS SignedData
// whose eContentType is id-ct-TSTInfo (RFC 3161, 2.4.2), and whose SignerInfo
// verifies against a certificate the token carries, that certificate
// allowing timestamping alone as its extended key usage (RFC 3161, 2.3). It
// returns [ErrSignatureMalformed] for a token that does not parse and
// [ErrSignatureInvalid] for one that does not verify.
func verifyTimestampToken(token []byte, checkImprint func(hash crypto.Hash, imprint []byte) error) (*TimestampInfo, error) {
	var ci contentInfo
	if _, err := asn1.Unmarshal(token, &ci); err != nil {
		return nil, fmt.Errorf("%w: TimeStampToken: %v", ErrSignatureMalformed, err)
	}
	if !ci.ContentType.Equal(oidCMSSignedData) {
		return nil, fmt.Errorf("%w: the TimeStampToken is a %v, not a SignedData", ErrSignatureMalformed, ci.ContentType)
	}
	var sd signedDataFull
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, fmt.Errorf("%w: TimeStampToken SignedData: %v", ErrSignatureMalformed, err)
	}
	var eci encapContentInfo
	if _, err := asn1.Unmarshal(sd.EncapContentInfo.FullBytes, &eci); err != nil {
		return nil, fmt.Errorf("%w: TimeStampToken EncapsulatedContentInfo: %v", ErrSignatureMalformed, err)
	}
	if !eci.ContentType.Equal(oidTSTInfo) {
		return nil, fmt.Errorf("%w: the token's content is a %v, not a TSTInfo", ErrSignatureMalformed, eci.ContentType)
	}
	var tstDER []byte
	if _, err := asn1.Unmarshal(eci.Content.Bytes, &tstDER); err != nil {
		return nil, fmt.Errorf("%w: the token's eContent: %v", ErrSignatureMalformed, err)
	}
	var tst tstInfoFull
	if _, err := asn1.Unmarshal(tstDER, &tst); err != nil {
		return nil, fmt.Errorf("%w: TSTInfo: %v", ErrSignatureMalformed, err)
	}
	hash, err := hashForObjectID(tst.MessageImprint.HashAlgorithm.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("the token's imprint: %w", err)
	}
	if err := checkImprint(hash, tst.MessageImprint.HashedMessage); err != nil {
		return nil, err
	}

	var certs []*x509.Certificate
	if len(sd.Certificates.Bytes) > 0 {
		if certs, err = x509.ParseCertificates(sd.Certificates.Bytes); err != nil {
			return nil, fmt.Errorf("%w: the token's certificates: %v", ErrSignatureMalformed, err)
		}
	}
	signer, err := verifyCMSSignature(token, tstDER, certs)
	if err != nil {
		return nil, fmt.Errorf("the token's signature: %w", err)
	}
	if !slices.Equal(signer.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}) {
		return nil, fmt.Errorf("%w: the TSA certificate %q does not restrict its key to timestamping (RFC 3161, 2.3)", ErrSignatureInvalid, signer.Subject.CommonName)
	}
	return &TimestampInfo{Time: tst.GenTime, Nonce: tst.Nonce, Signer: signer, Certificates: certs}, nil
}
