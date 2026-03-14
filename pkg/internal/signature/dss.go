package signature

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// dssData holds the raw validation materials extracted from the Document
// Security Store (/DSS) dictionary in the catalog.
type dssData struct {
	Certs []*x509.Certificate
	CRLs  []*pkix.CertificateList
	OCSPs []ocspBasicResponse
	VRI   map[string]*vriEntry
}

// vriEntry holds per-signature Validation Related Information.
type vriEntry struct {
	Certs []*x509.Certificate
	CRLs  []*pkix.CertificateList
	OCSPs []ocspBasicResponse
}

// ocspBasicResponse holds the parsed fields we need from an OCSP BasicResponse.
type ocspBasicResponse struct {
	ProducedAt  time.Time
	Responses   []singleResponse
	SignerCerts []*x509.Certificate
}

// singleResponse is one certificate status entry inside an OCSP response.
type singleResponse struct {
	SerialNumber *big.Int
	Status       int // 0=good, 1=revoked, 2=unknown
	ThisUpdate   time.Time
	NextUpdate   time.Time
}

// extractDSS reads the /DSS dictionary from the document catalog and parses
// all embedded certificates, CRLs, and OCSP responses.
func extractDSS(doc *document.Document) (*dssData, error) {
	catalog, err := document.GetCatalog(doc)
	if err != nil {
		return nil, err
	}

	dssObj, ok := catalog.Dict["DSS"]
	if !ok {
		return nil, nil
	}

	dssResolved, err := resolveObject(doc, dssObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /DSS: %w", err)
	}

	dssDict, ok := dssResolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("/DSS is not a dictionary")
	}

	dss := &dssData{
		VRI: make(map[string]*vriEntry),
	}

	dss.Certs = parseDSSCerts(doc, dssDict)
	dss.CRLs = parseDSSCRLs(doc, dssDict)
	dss.OCSPs = parseDSSOCSPs(doc, dssDict)

	// /VRI — dictionary keyed by uppercase hex SHA-1 of the signature value,
	// each value is a dictionary with optional /Cert, /CRL, /OCSP arrays.
	if vriObj, ok := dssDict["VRI"]; ok {
		vriResolved, err := resolveObject(doc, vriObj)
		if err == nil {
			if vriDict, ok := vriResolved.(ast.PdfDict); ok {
				for key, valObj := range vriDict {
					entryResolved, err := resolveObject(doc, valObj)
					if err != nil {
						continue
					}
					entryDict, ok := entryResolved.(ast.PdfDict)
					if !ok {
						continue
					}

					entry := &vriEntry{
						Certs: parseDSSCerts(doc, entryDict),
						CRLs:  parseDSSCRLs(doc, entryDict),
						OCSPs: parseDSSOCSPs(doc, entryDict),
					}
					dss.VRI[string(key)] = entry
				}
			}
		}
	}

	return dss, nil
}

// resolveDSSStreamArray resolves a named array from a dictionary and decodes
// each stream entry, returning the raw DER bytes of each stream.
func resolveDSSStreamArray(doc *document.Document, dict ast.PdfDict, keys ...ast.PdfName) [][]byte {
	var arrObj ast.PdfObject
	var found bool
	for _, key := range keys {
		if obj, ok := dict[key]; ok {
			arrObj = obj
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	arrResolved, err := resolveObject(doc, arrObj)
	if err != nil {
		return nil
	}

	arr, ok := arrResolved.(ast.PdfArray)
	if !ok {
		return nil
	}

	var results [][]byte
	for _, streamRef := range arr {
		streamResolved, err := resolveObject(doc, streamRef)
		if err != nil {
			continue
		}
		stream, ok := streamResolved.(ast.PdfStream)
		if !ok {
			continue
		}
		data, err := filter.DecodeStream(stream)
		if err != nil {
			continue
		}
		results = append(results, data)
	}
	return results
}

// parseDSSCerts extracts X.509 certificates from /Certs or /Cert arrays.
func parseDSSCerts(doc *document.Document, dict ast.PdfDict) []*x509.Certificate {
	var certs []*x509.Certificate
	for _, data := range resolveDSSStreamArray(doc, dict, "Certs", "Cert") {
		cert, err := x509.ParseCertificate(data)
		if err == nil {
			certs = append(certs, cert)
		}
	}
	return certs
}

// parseDSSCRLs extracts Certificate Revocation Lists from /CRLs or /CRL arrays.
func parseDSSCRLs(doc *document.Document, dict ast.PdfDict) []*pkix.CertificateList {
	var crls []*pkix.CertificateList
	for _, data := range resolveDSSStreamArray(doc, dict, "CRLs", "CRL") {
		crl, err := x509.ParseCRL(data) //nolint:staticcheck // x509.ParseCRL is deprecated but pkix.CertificateList is still the standard type
		if err == nil {
			crls = append(crls, crl)
		}
	}
	return crls
}

// parseDSSOCSPs extracts OCSP responses from /OCSPs or /OCSP arrays.
func parseDSSOCSPs(doc *document.Document, dict ast.PdfDict) []ocspBasicResponse {
	var responses []ocspBasicResponse
	for _, data := range resolveDSSStreamArray(doc, dict, "OCSPs", "OCSP") {
		resp, err := parseOCSPResponse(data)
		if err == nil {
			responses = append(responses, resp)
		}
	}
	return responses
}

// --- OCSP ASN.1 parsing (minimal, RFC 6960) ---

// OCSPResponse ::= SEQUENCE { responseStatus ENUMERATED, responseBytes [0] EXPLICIT ... }
type ocspResponseASN1 struct {
	Status asn1.Enumerated
	Bytes  asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

// ResponseBytes ::= SEQUENCE { responseType OID, response OCTET STRING }
type ocspResponseBytesASN1 struct {
	ResponseType asn1.ObjectIdentifier
	Response     []byte
}

// BasicOCSPResponse ::= SEQUENCE { tbsResponseData, signatureAlgorithm, signature, certs [0] OPTIONAL }
type basicOCSPResponseASN1 struct {
	TBSResponseData asn1.RawValue
	SignatureAlg    asn1.RawValue
	Signature       asn1.BitString
	Certs           asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

// ResponseData ::= SEQUENCE { version [0] EXPLICIT, responderID, producedAt, responses SEQUENCE OF ... }
type responseDataASN1 struct {
	Raw         asn1.RawContent
	Version     asn1.RawValue   `asn1:"explicit,optional,tag:0"`
	ResponderID asn1.RawValue   // CHOICE: byName [1] or byKey [2]
	ProducedAt  time.Time       `asn1:"generalized"`
	Responses   []asn1.RawValue //nolint:unused // used by asn1.Unmarshal
}

// SingleResponse ::= SEQUENCE { certID, certStatus, thisUpdate, nextUpdate [0] OPTIONAL }
type singleResponseASN1 struct {
	CertID     certIDASN1
	CertStatus asn1.RawValue
	ThisUpdate time.Time     `asn1:"generalized"`
	NextUpdate asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

// CertID ::= SEQUENCE { hashAlgorithm, issuerNameHash, issuerKeyHash, serialNumber }
type certIDASN1 struct {
	HashAlgorithm  asn1.RawValue
	IssuerNameHash []byte
	IssuerKeyHash  []byte
	SerialNumber   *big.Int
}

// parseOCSPResponse parses a DER-encoded OCSPResponse (RFC 6960).
func parseOCSPResponse(der []byte) (ocspBasicResponse, error) {
	var resp ocspResponseASN1
	_, err := asn1.Unmarshal(der, &resp)
	if err != nil {
		return ocspBasicResponse{}, fmt.Errorf("failed to parse OCSPResponse: %w", err)
	}

	if resp.Status != 0 { // 0 = successful
		return ocspBasicResponse{}, fmt.Errorf("OCSP response status: %d (not successful)", resp.Status)
	}

	if len(resp.Bytes.Bytes) == 0 {
		return ocspBasicResponse{}, fmt.Errorf("OCSP response has no response bytes")
	}

	var respBytes ocspResponseBytesASN1
	_, err = asn1.Unmarshal(resp.Bytes.Bytes, &respBytes)
	if err != nil {
		return ocspBasicResponse{}, fmt.Errorf("failed to parse OCSP ResponseBytes: %w", err)
	}

	var basicResp basicOCSPResponseASN1
	_, err = asn1.Unmarshal(respBytes.Response, &basicResp)
	if err != nil {
		return ocspBasicResponse{}, fmt.Errorf("failed to parse BasicOCSPResponse: %w", err)
	}

	var respData responseDataASN1
	_, err = asn1.Unmarshal(basicResp.TBSResponseData.FullBytes, &respData)
	if err != nil {
		return ocspBasicResponse{}, fmt.Errorf("failed to parse ResponseData: %w", err)
	}

	result := ocspBasicResponse{
		ProducedAt: respData.ProducedAt,
	}

	for _, rawSingle := range respData.Responses {
		var single singleResponseASN1
		_, err := asn1.Unmarshal(rawSingle.FullBytes, &single)
		if err != nil {
			continue
		}

		sr := singleResponse{
			SerialNumber: single.CertID.SerialNumber,
			ThisUpdate:   single.ThisUpdate,
		}

		// CertStatus is a CHOICE: good [0] IMPLICIT NULL, revoked [1], unknown [2]
		sr.Status = single.CertStatus.Tag

		// NextUpdate is optional.
		if len(single.NextUpdate.Bytes) > 0 {
			var nextUpdate time.Time
			if _, err := asn1.Unmarshal(single.NextUpdate.Bytes, &nextUpdate); err == nil {
				sr.NextUpdate = nextUpdate
			}
		}

		result.Responses = append(result.Responses, sr)
	}

	// Extract signer certificates if present.
	if len(basicResp.Certs.Bytes) > 0 {
		certs, err := x509.ParseCertificates(basicResp.Certs.Bytes)
		if err == nil {
			result.SignerCerts = certs
		}
	}

	return result, nil
}

// resolveObject dereferences an indirect reference via the document's xref table.
// If obj is not an indirect reference it is returned as-is.
func resolveObject(doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ref)
	}
	return obj, nil
}

// checkRevocation checks whether a certificate was revoked at the given time
// using the embedded OCSP responses and CRLs.
func checkRevocation(cert *x509.Certificate, sigTime time.Time, ocsps []ocspBasicResponse, crls []*pkix.CertificateList) (ok bool, info string) {
	// Try OCSP first (preferred over CRL).
	for _, resp := range ocsps {
		for _, sr := range resp.Responses {
			if sr.SerialNumber == nil || cert.SerialNumber == nil {
				continue
			}
			if sr.SerialNumber.Cmp(cert.SerialNumber) != 0 {
				continue
			}

			// Found an OCSP response for this certificate.
			switch sr.Status {
			case 0: // good
				if !sigTime.Before(sr.ThisUpdate) && (sr.NextUpdate.IsZero() || !sigTime.After(sr.NextUpdate)) {
					return true, fmt.Sprintf("OCSP: good (produced %s)", resp.ProducedAt.Format(time.RFC3339))
				}
				return true, fmt.Sprintf("OCSP: good (but signing time %s outside validity window %s\u2013%s)",
					sigTime.Format(time.RFC3339), sr.ThisUpdate.Format(time.RFC3339), sr.NextUpdate.Format(time.RFC3339))
			case 1: // revoked
				return false, "OCSP: certificate was revoked"
			default: // unknown
				return false, "OCSP: certificate status unknown"
			}
		}
	}

	// Fall back to CRL: check all CRLs for the certificate's serial number.
	crlChecked := false
	for _, crl := range crls {
		crlChecked = true
		for _, revoked := range crl.TBSCertList.RevokedCertificates {
			if revoked.SerialNumber.Cmp(cert.SerialNumber) == 0 {
				if revoked.RevocationTime.After(sigTime) {
					return true, fmt.Sprintf("CRL: certificate revoked on %s (after signing time %s)",
						revoked.RevocationTime.Format(time.RFC3339), sigTime.Format(time.RFC3339))
				}
				return false, fmt.Sprintf("CRL: certificate revoked on %s (before signing time %s)",
					revoked.RevocationTime.Format(time.RFC3339), sigTime.Format(time.RFC3339))
			}
		}
	}

	if crlChecked {
		return true, "CRL: certificate not revoked"
	}

	return false, "no OCSP or CRL data available for this certificate"
}
