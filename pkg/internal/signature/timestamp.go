package signature

import (
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"time"
)

// RFC 3161 Time-Stamp Token ASN.1 structures.
// A timestamp token is a CMS SignedData wrapping a TSTInfo content type.

// tstInfo is the RFC 3161 TSTInfo structure (the timestamped content).
type tstInfo struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint asn1.RawValue
	SerialNumber   asn1.RawValue
	GenTime        time.Time `asn1:"generalized"`
	// Remaining fields (accuracy, ordering, nonce, extensions) are optional
	// and not needed for basic timestamp extraction.
}

// timestampResult holds the parsed timestamp token data.
type timestampResult struct {
	Time       time.Time
	IssuerCN   string
	SignerCert *x509.Certificate
}

// parseTimestampToken extracts the timestamp from an RFC 3161 Time-Stamp Token
// embedded in a CMS SignedData structure. The token is typically found as an
// unsigned attribute in a PDF signature's PKCS#7 block, or as a standalone
// /DocTimeStamp signature.
func parseTimestampToken(der []byte) (*timestampResult, error) {
	// The timestamp token is a CMS ContentInfo → SignedData → EncapContentInfo.
	var ci contentInfo
	_, err := asn1.Unmarshal(der, &ci)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp ContentInfo: %w", err)
	}

	var sd signedData
	_, err = asn1.Unmarshal(ci.Content.Bytes, &sd)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp SignedData: %w", err)
	}

	// The encapsulated content is a TSTInfo.
	tstInfoDER, err := extractEncapContent(sd.EncapContentInfo)
	if err != nil {
		return nil, err
	}

	var tst tstInfo
	_, err = asn1.Unmarshal(tstInfoDER, &tst)
	if err != nil {
		return nil, fmt.Errorf("failed to parse TSTInfo: %w", err)
	}

	result := &timestampResult{
		Time: tst.GenTime,
	}

	// Extract the TSA certificate if embedded.
	if len(sd.Certificates.Bytes) > 0 {
		certs, err := x509.ParseCertificates(sd.Certificates.Bytes)
		if err == nil && len(certs) > 0 {
			result.SignerCert = certs[0]
			result.IssuerCN = certs[0].Subject.CommonName
		}
	}

	return result, nil
}

// encapContentInfo is the CMS EncapsulatedContentInfo.
type encapContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

// extractEncapContent extracts the DER-encoded content from an
// EncapsulatedContentInfo raw value.
func extractEncapContent(raw asn1.RawValue) ([]byte, error) {
	var eci encapContentInfo
	_, err := asn1.Unmarshal(raw.FullBytes, &eci)
	if err != nil {
		return nil, fmt.Errorf("failed to parse EncapsulatedContentInfo: %w", err)
	}

	if len(eci.Content.Bytes) == 0 {
		return nil, fmt.Errorf("EncapsulatedContentInfo has no content")
	}

	// The content is wrapped in an OCTET STRING.
	var content []byte
	_, err = asn1.Unmarshal(eci.Content.Bytes, &content)
	if err != nil {
		// Try using the raw bytes directly (some implementations don't
		// wrap in an additional OCTET STRING).
		return eci.Content.Bytes, nil
	}

	return content, nil
}

// extractTimestampFromSignature attempts to extract an RFC 3161 timestamp
// token from the unsigned attributes of a PKCS#7 SignedData structure.
// Returns nil if no timestamp attribute is found.
func extractTimestampFromSignature(der []byte) *timestampResult {
	var ci contentInfo
	_, err := asn1.Unmarshal(der, &ci)
	if err != nil {
		return nil
	}

	// Parse SignedData with full signer info to access unsigned attributes.
	var sd signedDataFull
	_, err = asn1.Unmarshal(ci.Content.Bytes, &sd)
	if err != nil {
		return nil
	}

	// Look through signer infos for timestamp unsigned attributes.
	for _, si := range sd.SignerInfos {
		for _, attr := range si.UnsignedAttrs {
			// OID 1.2.840.113549.1.9.16.2.14 = id-smime-aa-timeStampToken
			if attr.Type.Equal(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 14}) {
				if len(attr.Values.Bytes) > 0 {
					ts, err := parseTimestampToken(attr.Values.Bytes)
					if err == nil {
						return ts
					}
				}
			}
		}
	}

	return nil
}

// signedDataFull extends signedData to include SignerInfos for timestamp extraction.
type signedDataFull struct {
	Version          int
	DigestAlgorithms asn1.RawValue
	EncapContentInfo asn1.RawValue
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue `asn1:"optional,tag:1"`
	SignerInfos      []signerInfo  `asn1:"set"`
}

// signerInfo is a CMS SignerInfo (RFC 5652 §5.3).
type signerInfo struct {
	Version            int
	SID                asn1.RawValue
	DigestAlgorithm    asn1.RawValue
	SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm asn1.RawValue
	Signature          []byte
	UnsignedAttrs      []attribute `asn1:"optional,tag:1,set"`
}

// attribute is a CMS Attribute (type + values).
type attribute struct {
	Type   asn1.ObjectIdentifier
	Values asn1.RawValue `asn1:"set"`
}
