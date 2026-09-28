package signature

import "errors"

// ErrMDPViolation names a modification that violates the DocMDP or FieldMDP
// policy a signature declared. An update that only adds validation data or a
// document timestamp is not a modification (ISO 32000-2, 12.8.2.2, Table
// 257), removing or reordering pages or changing what a signed page draws is
// one at every P, changing a catalog entry that form filling and signing do not need is one
// from P=2 up, and adding or removing an annotation other than a widget, adding
// a field other than a signature field or changing what a field's actions run
// is one at P=2. Rewriting the document metadata is one at every P unless the
// update also makes a change the policy permits.
//
// NOTHING RETURNS IT, and a caller must not reach for [errors.Is] against it.
// MDP is evaluated rather than refused: evaluateMDP reports every outcome
// through the result it is given, so a violation surfaces as
// models.SignatureValidation.MDPValid false beside an
// models.SignatureValidation.MDPInfo naming the policy and the offending
// object ("P=2 violation: object 12 (Annot:Text) is an annotation, which P=2
// permits only as a form field widget"). That verdict reaches callers through
// Reader.VerifySignatures, which returns the validations rather than an error,
// because one document can carry several signatures and a policy breach in one
// is a finding about that signature rather than a failure of the call.
//
// Kept rather than deleted: unused code here may be deliberate, and removing it
// is a maintainer's call. Wiring it instead would change a live verification
// path from reporting into refusing, which is a design decision rather than a
// repair.
var ErrMDPViolation = errors.New("MDP policy violation: unauthorized modification after signing")

// Signature-verification sentinels.
var (
	// ErrSignatureMalformed indicates the PKCS#7/CMS structure could not
	// be parsed far enough to verify (missing SignerInfo, unresolvable
	// signer certificate, absent messageDigest attribute).
	ErrSignatureMalformed = errors.New("malformed PKCS#7/CMS signature")

	// ErrSignatureInvalid indicates the signature did not verify: either
	// the signed messageDigest does not match the document byte range, or
	// the signature value failed against the signer's public key.
	ErrSignatureInvalid = errors.New("signature verification failed")

	// ErrUnsupportedSignatureAlgo indicates the signature uses a digest or
	// public-key algorithm the verifier does not implement.
	ErrUnsupportedSignatureAlgo = errors.New("unsupported signature algorithm")
)

// Signer-construction sentinels, returned by [NewCMSSigner].
var (
	// ErrSignerKeyUnsupported indicates a private key [CMSSigner] cannot sign
	// with: a key type other than RSA or ECDSA, or ECDSA on a curve other than
	// P-256, P-384 or P-521.
	ErrSignerKeyUnsupported = errors.New("unsupported signing key")

	// ErrSignerKeyMismatch indicates a signing key and certificate that do not
	// belong together: the key is not the private half of the certificate's
	// public key, or one of them is missing.
	ErrSignerKeyMismatch = errors.New("signing key does not match the certificate")

	// ErrSignerCertificateUsage indicates a certificate whose key usage
	// extension excludes signing (RFC 5280, 4.2.1.3).
	ErrSignerCertificateUsage = errors.New("certificate key usage does not allow signing")
)

// ErrNotCAdES indicates a SignedData that cannot be the CAdES signature of a
// PAdES baseline signature (ETSI EN 319 142-1, 6.3), returned by
// [CheckCAdES].
var ErrNotCAdES = errors.New("signature is not a PAdES baseline CAdES signature")
