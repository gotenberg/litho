package signature

import "errors"

// ErrMDPViolation indicates that a document was modified in a way that
// violates the DocMDP or FieldMDP policy declared by a signature.
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
