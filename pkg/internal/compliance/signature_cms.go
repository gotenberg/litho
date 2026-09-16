package compliance

import (
	"encoding/asn1"
	"errors"
)

// maxCMSBlobSize bounds the /Contents blob litho will hand to the ASN.1
// decoder. Not spec-mandated: an anti-DoS guard. A CMS SignedData carrying a
// full certificate chain and timestamp runs to tens of kilobytes, so 1 MiB is
// far above any real signature while keeping a crafted /Contents from forcing
// an unbounded parse.
const maxCMSBlobSize = 1 << 20

// errCMSUnparseable reports a /Contents blob that is not CMS SignedData.
var errCMSUnparseable = errors.New("signature /Contents is not CMS SignedData")

// cmsContentInfo is the outer wrapper of RFC 5652, §3: the content is tagged
// [0] EXPLICIT, so the SignedData sits inside RawValue.Bytes.
type cmsContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,tag:0"`
}

// cmsSignedData mirrors RFC 5652, §5.1. Only the certificate set and the
// signer count are read, so everything else stays raw and unvalidated: the
// clause asks whether a certificate is present and how many signers there are,
// not whether the signature verifies.
type cmsSignedData struct {
	Version          int
	DigestAlgorithms asn1.RawValue
	EncapContentInfo asn1.RawValue
	Certificates     asn1.RawValue   `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue   `asn1:"optional,tag:1"`
	SignerInfos      []asn1.RawValue `asn1:"set"`
}

// cmsSignatureInfo reports what ISO 19005-2/3, 6.4.3 tests 2 and 3 ask of a
// signature's /Contents.
type cmsSignatureInfo struct {
	// HasCertificate mirrors veraPDF's signingCertificatePresent.
	HasCertificate bool
	// SignerCount mirrors veraPDF's SignerInfoCount.
	SignerCount int
}

// parseCMSSignature reads the certificate presence and signer count out of a
// signature's /Contents.
//
// The blob is attacker-controlled, so it goes through encoding/asn1, which
// reports a malformed structure as an error rather than reading past its
// input. A blob that does not parse yields errCMSUnparseable, and the caller
// reports both tests failing, which is what veraPDF does: with no SignedData
// there is no certificate and no signer.
func parseCMSSignature(blob []byte) (cmsSignatureInfo, error) {
	var info cmsSignatureInfo
	if len(blob) == 0 || len(blob) > maxCMSBlobSize {
		return info, errCMSUnparseable
	}

	// RFC 5652 allows BER, and a streaming signer writes indefinite lengths
	// that encoding/asn1 rejects: pdfjs issue16553.pdf signs with one, its
	// /Contents opening 30 80, and veraPDF finds the certificate and the one
	// signer it holds. Rewrite the lengths before decoding.
	der, err := berToDER(blob)
	if err != nil {
		return info, errCMSUnparseable
	}

	var outer cmsContentInfo
	if _, err := asn1.Unmarshal(der, &outer); err != nil {
		return info, errCMSUnparseable
	}

	var signed cmsSignedData
	if _, err := asn1.Unmarshal(outer.Content.Bytes, &signed); err != nil {
		return info, errCMSUnparseable
	}

	// The certificates field is [0] IMPLICIT and OPTIONAL. An absent field
	// leaves a zero RawValue; an empty set leaves a present field with no
	// content, and neither carries a certificate.
	info.HasCertificate = len(signed.Certificates.Bytes) > 0
	info.SignerCount = len(signed.SignerInfos)
	return info, nil
}
