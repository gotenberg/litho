package pkcs12

import "errors"

// Sentinel errors of [Decode].
var (
	// ErrMalformed indicates input that is not a well-formed PKCS #12 PFX, or
	// holds a bag, key or certificate that does not parse.
	ErrMalformed = errors.New("malformed PKCS #12 data")

	// ErrUnsupported indicates a well-formed construct this package does not
	// read: an unknown encryption or MAC algorithm, or a public-key mode.
	ErrUnsupported = errors.New("unsupported PKCS #12 construct")

	// ErrIncorrectPassword indicates the password does not verify the MAC,
	// or, in a store without one, does not decrypt the encrypted content.
	ErrIncorrectPassword = errors.New("incorrect PKCS #12 password")

	// ErrLimit indicates input past one of the package's anti-DoS caps: its
	// size, its nesting, an iteration count, or the number of key derivations.
	ErrLimit = errors.New("PKCS #12 data exceeds a limit")
)
