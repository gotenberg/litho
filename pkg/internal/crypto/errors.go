package crypto

import "errors"

// Sentinel errors for encryption-related failure states.
var (
	// ErrInvalidPassword indicates the supplied password does not decrypt the document.
	ErrInvalidPassword = errors.New("invalid password")

	// ErrUnsupportedEncryption indicates the encryption dictionary uses an
	// algorithm or revision that this library does not implement.
	ErrUnsupportedEncryption = errors.New("unsupported encryption dictionary")

	// ErrPasswordRejected indicates an AES-256 password SASLprep (RFC 4013)
	// rejects, which ISO 32000-2, 7.6.4.3.3 forbids a new document to be
	// keyed to. The wrapped saslprep error says why.
	ErrPasswordRejected = errors.New("password rejected by SASLprep")
)
