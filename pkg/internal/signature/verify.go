package signature

import (
	"context"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// permitsSigning reports whether a certificate's [x509.KeyUsage] bits
// allow digital-signature use. RFC 5280 §4.2.1.3 leaves the KeyUsage
// extension optional, so an absent extension (KeyUsage == 0) is
// treated as permissive for backwards compatibility with legacy PDF
// signing certs that omit it. When the extension is present, at least
// one of [x509.KeyUsageDigitalSignature] or
// [x509.KeyUsageContentCommitment] (the X.509 name for
// "non-repudiation") must be set.
func permitsSigning(ku x509.KeyUsage) bool {
	if ku == 0 {
		return true
	}
	const sigBits = x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment
	return ku&sigBits != 0
}

// PKCS#7 / CMS ASN.1 structures (RFC 5652). Only the subset needed to
// extract the embedded X.509 certificates is modeled here.

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
//
// The modification detection and prevention policy of each signature is
// evaluated independently of its cryptographic checks, so a signature whose
// PKCS#7 data does not parse still reports its MDP fields beside the
// ErrorMessage that says why.
//
// An RFC 3161 token sets the signing time the chain is validated at only once
// it verifies over what it timestamps: the signature value for the token in a
// SignerInfo's unsigned attributes (RFC 3161, Appendix A), the signed bytes
// for a document timestamp, whose /Contents is the token itself (ISO 32000-2,
// 12.8.5). A token that does not verify is reported with the reason in
// TimestampError, and the /M entry stands in for it.
//
// # Corpus evidence
//
//   - All corpora: 41 signatures in 5171 files, 11 carrying a token. 10
//     tokens verify; the eleventh, pdfjs bug854315.pdf's, embeds a
//     certificate crypto/x509 does not parse, and its chain verdict stays
//     what it was.
//   - pdfjs issue15813.pdf and issue8047.pdf: the two document timestamps,
//     whose HashValid was false while their token was checked as a
//     SignedData over the signed bytes, verify now.
func VerifySignatures(ctx context.Context, doc *document.Document, roots *x509.CertPool) ([]models.SignatureValidation, error) {
	sigs, err := ExtractSignatures(ctx, doc)
	if err != nil {
		return nil, err
	}
	if len(sigs) == 0 {
		return nil, nil
	}

	// Extract DSS for LTV validation (best-effort).
	dss, _ := extractDSS(ctx, doc)

	var results []models.SignatureValidation

	for _, sig := range sigs {
		result := models.SignatureValidation{
			FieldName:   sig.FieldName,
			SubFilter:   sig.SubFilter,
			SigningTime: sig.SigningTime,
		}

		if dss != nil {
			result.HasDSS = true
		}

		// The DocMDP or FieldMDP transform and its /P live in the signature
		// dictionary's /Reference (ISO 32000-1, 12.8.1, Tables 252 and 253),
		// not in the PKCS#7 data, and the policy is checked against the file's
		// revisions (12.8.2.2.2). Neither needs the signature to parse or
		// verify, so the policy is reported beside whatever the cryptographic
		// checks below conclude.
		evaluateMDP(ctx, doc, sig, &result)

		// /Contents in a signature dict is stored as a hex string in
		// the PDF, but the lexer has already decoded it into raw DER
		// bytes by the time this code reads sig.Contents.
		der := []byte(sig.Contents)

		certs, err := parsePKCS7Certificates(der)
		if err != nil {
			result.ErrorMessage = fmt.Sprintf("failed to parse certificates: %v", err)
			results = append(results, result)
			continue
		}

		if len(certs) == 0 {
			result.ErrorMessage = "no certificates found in signature"
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

		// The signed bytes are read once, for the digest check below and for
		// a document timestamp's imprint.
		var content []byte
		readFailed := false
		if sig.ByteRange[1] > 0 && sig.ByteRange[3] >= 0 &&
			byteRangeReadable(sig.ByteRange, doc.FileSize()) {
			chunk1, e1 := doc.ReadFileBytes(ctx, sig.ByteRange[0], sig.ByteRange[1])
			chunk2, e2 := doc.ReadFileBytes(ctx, sig.ByteRange[2], sig.ByteRange[3])
			if e1 != nil || e2 != nil {
				readFailed = true
			} else {
				content = make([]byte, 0, len(chunk1)+len(chunk2))
				content = append(content, chunk1...)
				content = append(content, chunk2...)
			}
		}

		// Determine the effective signing time: prefer a cryptographic
		// timestamp over the self-declared /M entry. Only a token that
		// verifies over what it timestamps sets it: a token copied from
		// another signature would otherwise pin the chain validation to a
		// time it does not vouch for.
		var effectiveSignTime time.Time
		docTimestamp := sig.SubFilter == "ETSI.RFC3161"
		ts, tsErr := signatureTimestamp(der, content, docTimestamp)
		if ts != nil {
			result.HasTimestamp = true
			result.TimestampTime = ts.Time.Format(time.RFC3339)
			if ts.Signer != nil {
				result.TimestampIssuer = ts.Signer.Subject.CommonName
			}
			if tsErr == nil {
				effectiveSignTime = ts.Time
			} else {
				result.TimestampError = tsErr.Error()
			}
		}
		if docTimestamp && ts != nil && ts.Signer != nil {
			// ISO 32000-2, 12.8.5: the token is the whole signature, and
			// the TSA signed it.
			leaf = ts.Signer
			result.SignerCN = leaf.Subject.CommonName
			result.Issuer = leaf.Issuer.CommonName
			result.NotBefore = leaf.NotBefore.Format(time.RFC3339)
			result.NotAfter = leaf.NotAfter.Format(time.RFC3339)
		}
		if effectiveSignTime.IsZero() && sig.SigningTime != "" {
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
			result.ErrorMessage = fmt.Sprintf("certificate chain validation failed: %v", err)
		}

		// Per RFC 5280, when the leaf cert carries a KeyUsage
		// extension it must permit signing. ExtKeyUsage stays
		// permissive (ExtKeyUsageAny) because PDF defines no required
		// EKU; tightening that field would reject many legitimate
		// signatures. KeyUsage is the orthogonal check.
		if result.CertChainValid && !permitsSigning(leaf.KeyUsage) {
			result.CertChainValid = false
			result.ErrorMessage = "certificate KeyUsage extension does not permit digital signatures"
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
		// a cryptographic timestamp that verifies.
		result.LTVEnabled = result.HasDSS && result.HasTimestamp && result.TimestampError == "" &&
			(result.RevocationOK || result.RevocationInfo != "")

		// Cryptographically verify the signature over the byte range it
		// covers. HashValid is true only when the SignerInfo's signed
		// messageDigest matches the recomputed digest of those bytes and
		// the signature value verifies against the signer's public key, or,
		// for a document timestamp, when its token imprints those bytes and
		// verifies (ISO 32000-2, 12.8.5). This is independent of
		// CertChainValid (trust) and of revocation: a signature can be
		// cryptographically valid yet chain-untrusted, or vice versa.
		switch {
		case readFailed:
			if result.ErrorMessage == "" {
				result.ErrorMessage = "could not read signed byte range"
			}
		case content == nil:
			// A /ByteRange naming bytes the file does not hold covers
			// nothing, so nothing verifies.
		case docTimestamp:
			result.HashValid = ts != nil && tsErr == nil
			if !result.HashValid && result.ErrorMessage == "" && tsErr != nil {
				result.ErrorMessage = tsErr.Error()
			}
		default:
			if _, verifyErr := verifyCMSSignature(der, content, certs); verifyErr != nil {
				if result.ErrorMessage == "" {
					result.ErrorMessage = verifyErr.Error()
				}
			} else {
				result.HashValid = true
			}
		}

		results = append(results, result)
	}

	return results, nil
}

// ResolveDSSInfo returns a summary of the Document Security Store contents.
// Returns nil if no /DSS dictionary exists.
func ResolveDSSInfo(ctx context.Context, doc *document.Document) (*models.DSSInfo, error) {
	dss, err := extractDSS(ctx, doc)
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
