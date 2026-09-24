package ioat

import "errors"

// Sentinel errors for [Reader] operations.
var (
	// ErrUnreadByte is returned when UnreadByte is called at the
	// beginning of the buffer with no byte to unread.
	ErrUnreadByte = errors.New("ioat.Reader: UnreadByte: at beginning of buffer")

	// ErrSeekInvalidWhence is returned when Seek is called with an
	// unsupported whence value.
	ErrSeekInvalidWhence = errors.New("ioat.Reader.Seek: invalid whence")

	// ErrSeekNegativePosition is returned when Seek would result in a
	// negative read position.
	ErrSeekNegativePosition = errors.New("ioat.Reader.Seek: negative position")
)
