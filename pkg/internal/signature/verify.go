package signature

import (
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// PKCS#7 / CMS ASN.1 structures (RFC 5652).
// We only need enough to extract the embedded X.509 certificates.

// contentInfo is the top-level CMS ContentInfo.
type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,tag:0"`
}

// signedData is the CMS SignedData structure.
type signedData struct {
	Version          int
	DigestAlgorithms asn1.RawValue
	EncapContentInfo asn1.RawValue
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
}

// parsePKCS7Certificates extracts X.509 certificates from a raw PKCS#7 / CMS
// DER-encoded byte block. Returns the parsed certificates or an error.
func parsePKCS7Certificates(der []byte) ([]*x509.Certificate, error) {
	var ci contentInfo
	_, err := asn1.Unmarshal(der, &ci)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS#7 ContentInfo: %w", err)
	}

	var sd signedData
	_, err = asn1.Unmarshal(ci.Content.Bytes, &sd)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS#7 SignedData: %w", err)
	}

	// The certificates field is a SET OF Certificate (implicit tag 0).
	// Each certificate is a SEQUENCE that x509.ParseCertificate can handle.
	if len(sd.Certificates.Bytes) == 0 {
		return nil, fmt.Errorf("no certificates found in PKCS#7 block")
	}

	return x509.ParseCertificates(sd.Certificates.Bytes)
}

// parsePDFDate parses a PDF date string (D:YYYYMMDDHHmmSSOHH'mm') into a time.Time.
func parsePDFDate(s string) (time.Time, error) {
	s = strings.TrimPrefix(s, "D:")

	// Try common PDF date formats from most to least specific.
	formats := []string{
		"20060102150405-07'00'",
		"20060102150405+07'00'",
		"20060102150405Z",
		"20060102150405",
		"200601021504",
		"2006010215",
		"20060102",
	}

	// Normalize timezone offset format: +05'30' → +0530
	normalized := strings.ReplaceAll(s, "'", "")

	for _, layout := range formats {
		normLayout := strings.ReplaceAll(layout, "'", "")
		if t, err := time.Parse(normLayout, normalized); err == nil {
			return t, nil
		}
	}

	// Try ISO 8601 as fallback.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unable to parse PDF date: %q", s)
}

// VerifySignatures extracts all digital signatures from the document and
// validates each signer's X.509 certificate chain. When a Document Security
// Store (DSS) is present, it uses the embedded OCSP responses, CRLs, and
// timestamp tokens for Long-Term Validation (LTV), allowing offline
// verification of signatures whose certificates have since expired.
func VerifySignatures(doc *document.Document, roots *x509.CertPool) ([]models.SignatureValidation, error) {
	sigs, err := ExtractSignatures(doc)
	if err != nil {
		return nil, err
	}
	if len(sigs) == 0 {
		return nil, nil
	}

	// Extract DSS for LTV validation (best-effort).
	dss, _ := extractDSS(doc)

	var results []models.SignatureValidation

	for _, sig := range sigs {
		result := models.SignatureValidation{
			FieldName:   sig.FieldName,
			SigningTime: sig.SigningTime,
		}

		if dss != nil {
			result.HasDSS = true
		}

		// Validate the document hash.
		result.HashValid = sig.DocumentHash != "" && sig.ByteRange[1] > 0

		// Decode the PKCS#7 contents from hex.
		der, err := hex.DecodeString(fmt.Sprintf("%x", []byte(sig.Contents)))
		if err != nil {
			// Contents might already be raw DER bytes (not hex-encoded string).
			der = []byte(sig.Contents)
		}

		certs, err := parsePKCS7Certificates(der)
		if err != nil {
			result.Error = fmt.Sprintf("failed to parse certificates: %v", err)
			results = append(results, result)
			continue
		}

		if len(certs) == 0 {
			result.Error = "no certificates found in signature"
			results = append(results, result)
			continue
		}

		// The first certificate is typically the signer (leaf) certificate.
		leaf := certs[0]
		result.SignerCN = leaf.Subject.CommonName
		result.Issuer = leaf.Issuer.CommonName
		result.NotBefore = leaf.NotBefore.Format(time.RFC3339)
		result.NotAfter = leaf.NotAfter.Format(time.RFC3339)

		// Build an intermediate pool from signature certs + DSS certs.
		intermediates := x509.NewCertPool()
		for _, cert := range certs[1:] {
			intermediates.AddCert(cert)
		}
		if dss != nil {
			for _, cert := range dss.Certs {
				intermediates.AddCert(cert)
			}
		}

		// Determine the effective signing time: prefer a cryptographic
		// timestamp over the self-declared /M entry.
		var effectiveSignTime time.Time

		// Try to extract an RFC 3161 timestamp from the signature.
		tsResult := extractTimestampFromSignature(der)
		if tsResult != nil {
			result.HasTimestamp = true
			result.TimestampTime = tsResult.Time.Format(time.RFC3339)
			result.TimestampIssuer = tsResult.IssuerCN
			effectiveSignTime = tsResult.Time
		} else if sig.SigningTime != "" {
			// Fall back to the /M entry.
			if t, err := parsePDFDate(sig.SigningTime); err == nil {
				effectiveSignTime = t
			}
		}

		// Verify the certificate chain. For LTV with a known signing time,
		// pin verification to that time so expired certs can still validate.
		opts := x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		}
		if !effectiveSignTime.IsZero() {
			opts.CurrentTime = effectiveSignTime
		}

		_, err = leaf.Verify(opts)
		if err == nil {
			result.CertChainValid = true
		} else {
			result.Error = fmt.Sprintf("certificate chain validation failed: %v", err)
		}

		// Validate signing time against certificate validity.
		if !effectiveSignTime.IsZero() {
			result.TimestampValid = !effectiveSignTime.Before(leaf.NotBefore) && !effectiveSignTime.After(leaf.NotAfter)
		}

		// Revocation checking via embedded OCSP/CRL data.
		if dss != nil && !effectiveSignTime.IsZero() {
			// Merge DSS-level and VRI-level revocation data.
			allOCSPs := dss.OCSPs
			allCRLs := dss.CRLs
			for _, vri := range dss.VRI {
				allOCSPs = append(allOCSPs, vri.OCSPs...)
				allCRLs = append(allCRLs, vri.CRLs...)
			}

			if len(allOCSPs) > 0 || len(allCRLs) > 0 {
				revOK, revInfo := checkRevocation(leaf, effectiveSignTime, allOCSPs, allCRLs)
				result.RevocationOK = revOK
				result.RevocationInfo = revInfo
			} else {
				result.RevocationInfo = "no OCSP or CRL data in DSS"
			}
		}

		// Determine LTV status: the document has enough embedded data for
		// full offline validation if it has a DSS with revocation info and
		// a cryptographic timestamp.
		result.LTVEnabled = result.HasDSS && result.HasTimestamp &&
			(result.RevocationOK || result.RevocationInfo != "")

		// Evaluate MDP (Modification Detection and Prevention) policy.
		evaluateMDP(doc, sig, &result)

		results = append(results, result)
	}

	return results, nil
}

// GetDSSInfo returns a summary of the Document Security Store contents.
// Returns nil if no /DSS dictionary exists.
func GetDSSInfo(doc *document.Document) (*models.DSSInfo, error) {
	dss, err := extractDSS(doc)
	if err != nil {
		return nil, err
	}
	if dss == nil {
		return nil, nil
	}

	info := &models.DSSInfo{
		CertCount: len(dss.Certs),
		CRLCount:  len(dss.CRLs),
		OCSPCount: len(dss.OCSPs),
		VRICount:  len(dss.VRI),
	}

	return info, nil
}
