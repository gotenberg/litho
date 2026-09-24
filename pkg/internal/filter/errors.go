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

	// ErrInvalidASCIIHex is returned when ASCIIHexDecode data holds a
	// character that is neither a hexadecimal digit, white space, nor the EOD
	// marker, which ISO 32000-1, 7.4.2 makes an error.
	ErrInvalidASCIIHex = errors.New("asciihex: invalid data")

	// ErrInvalidASCII85 is returned when ASCII85Decode data holds a character
	// or a character sequence ISO 32000-1, 7.4.3 makes an error: a character
	// outside the encoding, a group whose value exceeds 2^32 - 1, a z inside
	// a group, or a final partial group of one character.
	ErrInvalidASCII85 = errors.New("ascii85: invalid data")

	// errPluginDeclined marks, alongside [ErrUnsupportedFilter], a stream a
	// plugin declined for a filter litho has no built-in decoder for. It
	// fails the stream where a bare ErrUnsupportedFilter skips the filter.
	errPluginDeclined = errors.New("declined by its plugin, and litho has no built-in decoder for it")
)
