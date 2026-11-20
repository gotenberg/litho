package models

// Signature represents a cryptographic signature found in the document.
// Concurrent reads of a value are safe; a write needs synchronization.
type Signature struct {
	// FieldName is the fully qualified name of the signature field.
	FieldName string `json:"fieldName"`
	// ByteRange is the signature's /ByteRange: the offset and length, in bytes,
	// of each of the two file ranges the signature covers (ISO 32000-1,
	// 12.8.1).
	ByteRange [4]int64 `json:"byteRange"`
	// Contents is the signature's /Contents: the raw bytes of the PKCS#7 or
	// CMS container, not hex-encoded.
	Contents string `json:"contents"`
	// DocumentHash is the hex digest of the bytes ByteRange covers, taken with
	// the digest algorithm the signature declares. It is empty when ByteRange
	// names bytes the file does not hold.
	DocumentHash string `json:"documentHash"`
	// SigningTime is the signature's /M entry as the file writes it, a PDF
	// date string, or empty when it has none.
	SigningTime string `json:"signingTime,omitempty"`
	// MDPPolicy is the /P permission of the signature's DocMDP or FieldMDP
	// transform (ISO 32000-1, 12.8.1, Tables 252 and 253), or 0 when it has none.
	MDPPolicy int `json:"mdpPolicy,omitempty"`
	// SubFilter is the signature dictionary's /SubFilter, such as
	// adbe.pkcs7.detached, ETSI.CAdES.detached or, for a document timestamp,
	// ETSI.RFC3161 (ISO 32000-2, 12.8.3 and 12.8.5).
	SubFilter string `json:"subFilter,omitempty"`
}

// SignatureValidation holds the result of validating a PDF signature's
// X.509 certificate chain against a set of trusted root certificates.
// Concurrent reads of a value are safe; a write needs synchronization.
type SignatureValidation struct {
	// FieldName is the fully qualified name of the signature field.
	FieldName string `json:"fieldName"`
	// HashValid reports that the signature verifies over the bytes its
	// /ByteRange covers, independent of trust in the certificate chain.
	HashValid bool `json:"hashValid"`
	// CertChainValid reports that the signer certificate chains to a trusted
	// root, checked at the signing time when one is known.
	CertChainValid bool `json:"certChainValid"`
	// SignerCN is the common name of the signer certificate.
	SignerCN string `json:"signerCN"`
	// Issuer is the common name of the signer certificate's issuer.
	Issuer string `json:"issuer"`
	// NotBefore is the start of the signer certificate's validity, in RFC 3339
	// form.
	NotBefore string `json:"notBefore"`
	// NotAfter is the end of the signer certificate's validity, in RFC 3339
	// form.
	NotAfter string `json:"notAfter"`
	// SigningTime is the signature's /M entry as the file writes it, or empty
	// when it has none.
	SigningTime string `json:"signingTime,omitempty"`
	// TimestampValid reports that the signing time falls within the signer
	// certificate's validity. It is false when no signing time is known.
	TimestampValid bool `json:"timestampValid"`
	// ErrorMessage says why a check failed, or is empty when none did.
	ErrorMessage string `json:"errorMessage,omitempty"`

	// LTV / PAdES fields

	// HasDSS reports that the document carries a Document Security Store.
	HasDSS bool `json:"hasDSS,omitempty"`
	// HasTimestamp reports that the signature holds an RFC 3161 timestamp
	// token.
	HasTimestamp bool `json:"hasTimestamp,omitempty"`
	// TimestampTime is the time the timestamp token states, in RFC 3339 form.
	TimestampTime string `json:"timestampTime,omitempty"`
	// TimestampIssuer is the common name of the timestamp authority's
	// certificate.
	TimestampIssuer string `json:"timestampIssuer,omitempty"`
	// TimestampError says why the RFC 3161 token found does not verify over
	// what it timestamps; its time is then not trusted as the signing time.
	TimestampError string `json:"timestampError,omitempty"`
	// SubFilter is the signature dictionary's /SubFilter.
	SubFilter string `json:"subFilter,omitempty"`
	// LTVEnabled reports that the document holds enough data to validate the
	// signature offline: a Document Security Store with revocation data and a
	// timestamp that verifies.
	LTVEnabled bool `json:"ltvEnabled,omitempty"`
	// RevocationOK reports that the embedded OCSP responses or CRLs show the
	// signer certificate was not revoked at the signing time.
	RevocationOK bool `json:"revocationOK,omitempty"`
	// RevocationInfo describes the revocation outcome, or says that the
	// Document Security Store holds no OCSP or CRL data.
	RevocationInfo string `json:"revocationInfo,omitempty"`

	// MDP (Modification Detection and Prevention) fields

	// MDPPolicy is the /P permission of the signature's DocMDP or FieldMDP
	// transform, or 0 when it has none.
	MDPPolicy int `json:"mdpPolicy,omitempty"`
	// MDPValid reports that the changes made after signing comply with
	// MDPPolicy.
	MDPValid bool `json:"mdpValid,omitempty"`
	// MDPInfo explains the MDP outcome.
	MDPInfo string `json:"mdpInfo,omitempty"`
}

// DSSInfo represents the Document Security Store (/DSS) extracted from
// the document catalog. It contains the embedded validation materials
// needed for Long-Term Validation (LTV).
// Concurrent reads of a value are safe; a write needs synchronization.
type DSSInfo struct {
	// CertCount is the number of certificates in the store.
	CertCount int `json:"certCount"`
	// CRLCount is the number of certificate revocation lists in the store.
	CRLCount int `json:"crlCount"`
	// OCSPCount is the number of OCSP responses in the store.
	OCSPCount int `json:"ocspCount"`
	// VRICount is the number of per-signature validation entries in the
	// store.
	VRICount int `json:"vriCount"`
}
