package filter

import (
	"bytes"
	"compress/flate"
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// whiteSpace holds the six white-space characters of ISO 32000-1, 7.2.2,
// Table 1, the ones [isWhiteSpace] reports.
const whiteSpace = "\x00\t\n\f\r "

// zlibChecksumLen is the length of the Adler-32 checksum that ends a zlib
// stream (RFC 1950, 2.2).
const zlibChecksumLen = 4

// StoredTail returns the bytes of the stream's stored data that its first
// filter never reads: what follows the filter's end-of-data marker, or what
// follows the point where its data is damaged. White space at either end is
// dropped, and nil means there is none, or that the filter's decoder cannot
// tell where its data ends.
//
// Decoding hides these bytes. ISO 32000-1, 7.4.2 and 7.4.3 end ASCIIHexDecode
// and ASCII85Decode data at their EOD markers, 7.4.4.2 ends LZWDecode data at
// its EOD code, 7.4.5 ends RunLengthDecode data at its EOD byte, and RFC 1950
// ends a zlib stream at its checksum, and every decoder stops there. The bytes
// past the end are still in the stream, so a file holds them for anyone who
// reads the stored data rather than the decoded one, and a copy of the stream
// carries them along. A check of decoded data alone cannot vouch for them.
//
// Only the first filter is measured: its input is the stored data a reader of
// the file sees. A later filter's leftover sits inside data the first filter
// still encodes. The image codecs, BrotliDecode and Crypt report none, since
// their decoders do not say how much of their input they read.
//
// limit bounds the decoded bytes read to find the end, as an anti-DoS guard:
// data that decodes to more reports nil, as unknown.
//
// # Corpus evidence
//
//   - All corpora: 1778 of the 163,291 filtered streams of the 5247 files
//     carry a tail, in 59 files. 1534 of them are one byte, mostly in
//     encrypted govdocs files whose /Length counts the end-of-line before
//     endstream, which decrypts to a stray byte.
//   - govdocs thread_400_400299.pdf: a /Length past endstream makes the tail
//     the text of the next object.
func StoredTail(ctx context.Context, stream ast.PdfStream, limit int64) ([]byte, error) {
	filters, err := streamFilterNames(stream.Dict)
	if err != nil || len(filters) == 0 {
		// An invalid /Filter fails decoding outright, and no filter at all
		// stores the data as it decodes: either way nothing is hidden.
		return nil, nil //nolint:nilerr // Decoding reports an invalid /Filter; there is no tail to measure.
	}
	data, err := stream.Bytes(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading stored stream data: %w", err)
	}
	parms := extractDecodeParms(stream.Dict, len(filters))
	tail, err := firstFilterTail(ctx, data, filters[0], parms[0], limit)
	if err != nil {
		return nil, err
	}
	tail = bytes.Trim(tail, whiteSpace)
	if len(tail) == 0 {
		return nil, nil
	}
	return tail, nil
}

// firstFilterTail returns the bytes of data the named filter does not read.
// The error is only ever the context's.
func firstFilterTail(ctx context.Context, data []byte, name ast.PdfName, parms ast.PdfDict, limit int64) ([]byte, error) {
	switch name {
	case "FlateDecode", "Fl":
		return flateTail(ctx, data, limit)
	case "LZWDecode", "LZW":
		earlyChange := 1
		if parms != nil {
			if ec, ok := parms["EarlyChange"]; ok {
				earlyChange = pdfInt(ec)
			}
		}
		src := bytes.NewReader(data)
		if !drained(ctx, newLZWReader(src, earlyChange), limit) {
			return nil, ctx.Err()
		}
		return data[len(data)-src.Len():], nil
	case "ASCIIHexDecode", "AHx":
		return asciiTail(data, ">", func(c byte) bool { _, ok := hexDigit(c); return ok }), nil
	case "ASCII85Decode", "A85":
		return asciiTail(data, "~>", func(c byte) bool { return (c >= '!' && c <= 'u') || c == 'z' }), nil
	case "RunLengthDecode", "RL":
		return runLengthTail(data), nil
	}
	return nil, nil
}

// flateTail returns what follows the zlib checksum, or the raw deflate data,
// of FlateDecode data. Damaged deflate data yields what follows the damage.
func flateTail(ctx context.Context, data []byte, limit int64) ([]byte, error) {
	body, framed, err := deflateData(data)
	if err != nil {
		// decodeFlateDecode fails on the same header, so the whole stream is
		// unread by decoding, and every byte of it is a tail.
		return data, nil //nolint:nilerr // The stored data no decoder reads is the tail.
	}
	// A bytes.Reader is an io.ByteReader, so the decompressor reads from it
	// directly, without buffering ahead, and what it leaves is what follows
	// the deflate data.
	src := bytes.NewReader(body)
	if !drained(ctx, flate.NewReader(src), limit) {
		return nil, ctx.Err()
	}
	rest := body[len(body)-src.Len():]
	if framed {
		rest = rest[min(zlibChecksumLen, len(rest)):]
	}
	return rest, nil
}

// drained reads r to its end or to its first error, and reports whether it
// got there within limit decoded bytes and before ctx was done.
func drained(ctx context.Context, r io.Reader, limit int64) bool {
	const chunk = 1 << 20
	var read int64
	for {
		if ctx.Err() != nil {
			return false
		}
		n, err := io.CopyN(io.Discard, r, chunk)
		read += n
		if read > limit {
			return false
		}
		if err != nil {
			// io.EOF ends the data, and any other error is where the damage
			// starts: the decoder reads nothing past either.
			return true
		}
	}
}

// asciiTail returns what follows the eod marker of an ASCII filter's data, or
// what follows its first character that is neither white space, eod, nor
// accepted by digit. Data with neither has no tail.
func asciiTail(data []byte, eod string, digit func(byte) bool) []byte {
	for i := range data {
		if bytes.HasPrefix(data[i:], []byte(eod)) {
			return data[i+len(eod):]
		}
		c := data[i]
		if isWhiteSpace(c) || digit(c) {
			continue
		}
		return data[i:]
	}
	return nil
}

// runLengthTail returns what follows the EOD byte of RunLengthDecode data
// (ISO 32000-1, 7.4.5). Data without one, or cut short, has no tail.
func runLengthTail(data []byte) []byte {
	const eodByte = 128
	for i := 0; i < len(data); {
		n := int(data[i])
		i++
		switch {
		case n < eodByte:
			i += n + 1
		case n > eodByte:
			i++
		default:
			return data[i:]
		}
	}
	return nil
}
