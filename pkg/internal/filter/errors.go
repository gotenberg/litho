package filter

import "errors"

// Sentinel errors for stream filter failure states.
var (
	// ErrUnsupportedFilter indicates the stream uses a compression or
	// encoding filter that this library does not implement.
	ErrUnsupportedFilter = errors.New("unsupported filter")

	// ErrDecompressionBomb is returned, wrapped in an error naming the filter
	// and the budget, when a filter's output would exceed the stream budget
	// (limits.Limits.MaxStreamBytes). pkg/pdf re-exports it as
	// pdf.ErrStreamTooLarge.
	ErrDecompressionBomb = errors.New("decoded stream exceeds the stream size limit")

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
	// marker, which ISO 32000-1, 7.4.2 makes an error. A stream decode returns
	// it only when nothing decoded before that character: otherwise the bytes
	// before it are kept and the stream is reported incomplete.
	ErrInvalidASCIIHex = errors.New("asciihex: invalid data")

	// ErrInvalidASCII85 is returned when ASCII85Decode data holds a character
	// or a character sequence ISO 32000-1, 7.4.3 makes an error: a character
	// outside the encoding, a group whose value exceeds 2^32 - 1, a z inside
	// a group, or a final partial group of one character. A stream decode
	// returns it only when no group decoded before the damage: otherwise the
	// bytes before it are kept and the stream is reported incomplete.
	ErrInvalidASCII85 = errors.New("ascii85: invalid data")

	// errDamagedData marks the error a FlateDecode, LZWDecode or BrotliDecode
	// filter returns alongside the bytes it decoded before damaged data:
	// corrupt data, or data that ends before its final block. A
	// CCITTFaxDecode filter returns it alongside the rows before a row in a
	// coding its decoder lacks, whose rest it cannot read. The filter chain
	// keeps those bytes and reports the stream incomplete (see
	// keepsDamagedPrefix), so the error reaches no caller. When nothing decoded
	// before the damage the filter returns the damage's own error instead.
	errDamagedData = errors.New("damaged data")

	// errPluginDeclined marks, alongside [ErrUnsupportedFilter], a stream a
	// plugin declined for a filter litho has no built-in decoder for. It
	// fails the stream where a bare ErrUnsupportedFilter skips the filter.
	errPluginDeclined = errors.New("declined by its plugin, and litho has no built-in decoder for it")

	// errUnsettled is returned by a codec probe (see codecProbes) that cannot
	// tell from the headers whether the decoder refuses the stream, and by
	// probeChain for a chain the probes cannot settle: only decoding tells.
	errUnsettled = errors.New("only decoding tells whether the decoder refuses the stream")
)
