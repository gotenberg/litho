package woff

import "errors"

// ErrNotWOFF reports data that starts with neither the WOFF 1.0 nor the
// WOFF 2.0 signature.
var ErrNotWOFF = errors.New("not a WOFF or WOFF2 file")

// ErrInvalid reports a WOFF or WOFF2 file that breaks its format: a field
// out of range, a table outside the file, a stream that ends early, or a
// transform that does not reconstruct.
var ErrInvalid = errors.New("invalid WOFF file")

// ErrTooLarge reports a file past one of the decoders' caps.
var ErrTooLarge = errors.New("WOFF file past the decoder's bounds")
