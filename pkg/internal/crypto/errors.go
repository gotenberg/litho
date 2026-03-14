package crypto

import "errors"

// Sentinel errors for encryption-related failure states.
var (
	// ErrInvalidPassword indicates the supplied password does not decrypt the document.
	ErrInvalidPassword = errors.New("invalid password")

	// ErrUnsupportedEncryption indicates the encryption dictionary uses an
	// algorithm or revision that this library does not implement.
	ErrUnsupportedEncryption = errors.New("unsupported encryption dictionary")
)
