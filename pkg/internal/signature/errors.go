package signature

import "errors"

// ErrMDPViolation names a modification that violates the DocMDP or FieldMDP
// policy a signature declared.
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
