package filter

import "errors"

// Sentinel errors for stream filter failure states.
var (
	// ErrUnsupportedFilter indicates the stream uses a compression or
	// encoding filter that this library does not implement.
	ErrUnsupportedFilter = errors.New("unsupported filter")
)
