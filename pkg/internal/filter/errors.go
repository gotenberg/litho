package filter

import (
	"errors"
	"fmt"
)

// Sentinel errors for stream filter failure states.
var (
	// ErrUnsupportedFilter indicates the stream uses a compression or
	// encoding filter that this library does not implement.
	ErrUnsupportedFilter = errors.New("unsupported filter")

	// ErrDecompressionBomb is returned when a stream exceeds maxDecompressedSize.
	ErrDecompressionBomb = fmt.Errorf("decompressed stream exceeds %d MB limit", maxDecompressedSize>>20)

	// ErrUnsupportedPredictor indicates a predictor configuration the
	// un-filter cannot reverse, such as a BitsPerComponent the TIFF
	// predictor code does not handle. Returning an error prevents
	// silently emitting corrupted samples.
	ErrUnsupportedPredictor = errors.New("unsupported predictor configuration")

	// ErrLZWNonLiteralAfterClear is returned when an LZW stream contains
	// a non-literal code immediately after a clear table code.
	ErrLZWNonLiteralAfterClear = errors.New("lzw: unexpected non-literal code after clear")

	// ErrLZWCodeOutOfRange is returned when an LZW stream contains a
	// code value that exceeds the current dictionary size.
	ErrLZWCodeOutOfRange = errors.New("lzw: code out of range")
)
