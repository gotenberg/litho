package jbig2

import "errors"

// Sentinel errors for JBIG2 decoding failure states.
var (
	// ErrCorruptedData indicates the JBIG2 data stream is malformed.
	ErrCorruptedData = errors.New("jbig2: corrupted data")

	// ErrUnsupportedFeature indicates an unimplemented JBIG2 feature was encountered.
	ErrUnsupportedFeature = errors.New("jbig2: unsupported feature")

	// ErrInvalidHeader indicates a segment header contains invalid values.
	ErrInvalidHeader = errors.New("jbig2: invalid header value")
)
