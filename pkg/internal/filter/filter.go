package filter

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"encoding/ascii85"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"golang.org/x/image/ccitt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// maxDecompressedSize caps the output of any single stream decompression to
// 256 MB. Prevents decompression bomb attacks where a small compressed stream
// expands into gigabytes.
const maxDecompressedSize = 256 << 20 // 256 MB

// limitedCopy copies from r to buf in 1 MB chunks up to maxDecompressedSize.
// Context is checked between chunks so callers can cancel long decompression.
func limitedCopy(ctx context.Context, buf *bytes.Buffer, r io.Reader) error {
	const chunk = 1 << 20 // 1 MB
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := io.CopyN(buf, r, chunk)
		if buf.Len() > maxDecompressedSize {
			return ErrDecompressionBomb
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if buf.Len() > 0 {
				// Best-effort: a truncated or corrupt stream yields
				// usable partial data, so return what decoded with a nil
				// error. The caller cannot currently distinguish a fully
				// decoded stream from a salvaged partial one; surfacing
				// that is deferred to the strict/lenient read mode.
				return nil
			}
			return err
		}
	}
}

// bufPool reuses bytes.Buffer instances across stream decompression calls
// to reduce GC pressure from repeated allocations.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Decodable reports whether the built-in decoders can handle the named
// stream filter without a plugin. It mirrors the decodable cases of
// applyDecodeFilter: JBIG2Decode and JPXDecode are excluded because the
// built-in implementations return ErrUnsupportedFilter (they need the
// jbig2 / jpx plugins), and unknown filters are excluded. Keep this in
// sync with applyDecodeFilter.
func Decodable(name ast.PdfName) bool {
	switch name {
	case "FlateDecode", "Fl",
		"LZWDecode", "LZW",
		"ASCIIHexDecode", "AHx",
		"ASCII85Decode", "A85",
		"CCITTFaxDecode", "CCF",
		"BrotliDecode",
		"RunLengthDecode", "RL",
		"DCTDecode", "DCT",
		"Crypt":
		return true
	}
	return false
}

// DecodeStream decodes a PDF stream based on its /Filter dictionary entry.
// It supports applying multiple filters sequentially if an array is provided.
// /DecodeParms are extracted and passed per filter for predictor processing.
// Context is checked between decompression chunks to allow cancellation.
// Decoding is best-effort: an unsupported filter in the chain is skipped.
// Use [DecodeStreamChecked] when the caller needs to know whether decoding
// was complete.
func DecodeStream(ctx context.Context, stream ast.PdfStream) ([]byte, error) {
	data, _, err := DecodeStreamChecked(ctx, stream)
	return data, err
}

// DecodeStreamChecked is [DecodeStream] that also reports whether decoding
// was complete. incomplete is true when an unsupported filter in the chain
// was skipped, leaving the returned bytes still encoded by that filter, so
// a caller must not treat them as fully decoded. The byte and error
// contract matches DecodeStream (best-effort), so callers that ignore
// incomplete behave exactly as before.
func DecodeStreamChecked(ctx context.Context, stream ast.PdfStream) (data []byte, incomplete bool, err error) {
	filters, err := streamFilterNames(stream.Dict)
	if err != nil {
		return nil, false, err
	}

	// Extract per-filter DecodeParms. The value can be a single dict (applied
	// to the single filter) or an array of dicts matching the filter array.
	parms := extractDecodeParms(stream.Dict, len(filters))

	data, err = stream.Bytes(ctx)
	if err != nil {
		return nil, false, err
	}
	for i, f := range filters {
		decoded, derr := applyDecodeFilter(ctx, data, f, parms[i])
		if derr != nil {
			if errors.Is(derr, ErrUnsupportedFilter) {
				// Best-effort: skip an unsupported filter and continue
				// with the current data so text extraction still works
				// when only part of a chain is supported (e.g. a plugin
				// is not loaded). The returned bytes are then only
				// partially decoded (still encoded by the skipped filter);
				// incomplete records that so a caller can avoid treating
				// them as fully decoded.
				incomplete = true
				continue
			}
			return nil, incomplete, derr
		}
		data = decoded
	}

	return data, incomplete, nil
}

// streamFilterNames returns the stream's /Filter entries in the order they
// apply. A missing or null /Filter yields none: per ISO 32000-1, 7.3.9, a null
// value is equivalent to the key being absent, so the data is unencoded. qpdf
// emits an explicit null on cross-reference streams, and rejecting it would
// make the whole document unreadable.
func streamFilterNames(dict ast.PdfDict) ([]ast.PdfName, error) {
	filterObj, ok := dict["Filter"]
	if !ok {
		return nil, nil
	}
	switch f := filterObj.(type) {
	case ast.PdfName:
		return []ast.PdfName{f}, nil
	case ast.PdfArray:
		names := make([]ast.PdfName, 0, len(f))
		for _, item := range f {
			if name, isName := item.(ast.PdfName); isName {
				names = append(names, name)
			}
		}
		return names, nil
	case ast.PdfNull:
		return nil, nil
	default:
		return nil, fmt.Errorf("invalid type for /Filter: %T", filterObj)
	}
}

// DecodeStreamUpTo decodes stream through its filter chain, stopping before
// the first stop filter, and returns the bytes that filter itself receives.
// found reports whether stop appears in the chain; when it does not, the
// returned bytes are the fully decoded stream.
//
// A check that judges an embedded image format reads the input to that
// format's filter, not its output: the JP2 boxes ISO 19005-2, 6.2.8.3 inspects
// are what JPXDecode consumes. [ast.PdfStream.Bytes] returns the raw bytes,
// which are that input only when the format filter is the whole chain, so a
// /Filter of [/FlateDecode /JPXDecode] handed the checks a compressed buffer
// that matched no signature and every JPEG2000 violation behind an outer
// filter went unreported.
//
// Unlike [DecodeStreamChecked], a filter before stop that cannot be decoded is
// an error rather than a skip: its output is the only route to stop's input,
// so continuing would judge the wrong bytes.
func DecodeStreamUpTo(ctx context.Context, stream ast.PdfStream, stop ast.PdfName) (data []byte, found bool, err error) {
	filters, err := streamFilterNames(stream.Dict)
	if err != nil {
		return nil, false, err
	}
	parms := extractDecodeParms(stream.Dict, len(filters))

	data, err = stream.Bytes(ctx)
	if err != nil {
		return nil, false, err
	}
	for i, f := range filters {
		if f == stop {
			return data, true, nil
		}
		decoded, derr := applyDecodeFilter(ctx, data, f, parms[i])
		if derr != nil {
			return nil, false, fmt.Errorf("decoding %s before %s: %w", f, stop, derr)
		}
		data = decoded
	}
	return data, false, nil
}

// extractDecodeParms returns a slice of DecodeParms dicts, one per filter.
// Missing entries are nil.
func extractDecodeParms(dict ast.PdfDict, numFilters int) []ast.PdfDict {
	result := make([]ast.PdfDict, numFilters)

	dpObj, ok := dict["DecodeParms"]
	if !ok {
		return result
	}

	switch dp := dpObj.(type) {
	case ast.PdfDict:
		// Single dict applies to the first (or only) filter.
		if numFilters > 0 {
			result[0] = dp
		}
	case ast.PdfArray:
		for i, item := range dp {
			if i >= numFilters {
				break
			}
			if d, ok := item.(ast.PdfDict); ok {
				result[i] = d
			}
		}
	}

	return result
}

// applyDecodeFilter applies a single decoding filter with optional DecodeParms.
func applyDecodeFilter(ctx context.Context, data []byte, filterName ast.PdfName, parms ast.PdfDict) ([]byte, error) {
	switch filterName {
	case "FlateDecode", "Fl":
		decoded, err := decodeFlateDecode(ctx, data)
		if err != nil {
			return nil, err
		}
		return applyPredictor(decoded, extractPredictorParams(parms))

	case "LZWDecode", "LZW":
		decoded, err := decodeLZW(ctx, data, parms)
		if err != nil {
			return nil, err
		}
		return applyPredictor(decoded, extractPredictorParams(parms))

	case "ASCIIHexDecode", "AHx":
		return decodeASCIIHex(data)

	case "ASCII85Decode", "A85":
		return decodeASCII85(data)

	case "CCITTFaxDecode", "CCF":
		return decodeCCITTFax(ctx, data, parms)

	case "BrotliDecode":
		return decodeBrotli(ctx, data)

	case "RunLengthDecode", "RL":
		return decodeRunLength(data)

	case "DCTDecode", "DCT":
		return decodeDCT(data, parms)

	case "JPXDecode":
		return decodeJPX(data)

	case "JBIG2Decode":
		return decodeJBIG2(data, parms)

	case "Crypt":
		return decodeCrypt(data, parms)

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFilter, filterName)
	}
}

func decodeFlateDecode(ctx context.Context, data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		// Fallback: try raw deflate (no zlib header). Some corrupt PDFs
		// have damaged headers but valid deflate data.
		r = flate.NewReader(bytes.NewReader(data))
		// Verify we can read at least some data.
		probe := make([]byte, 1)
		if _, probeErr := r.Read(probe); probeErr != nil {
			r.Close()
			return nil, err // return original zlib error
		}
		// Reset: re-create reader to include the probed byte.
		r.Close()
		r = flate.NewReader(bytes.NewReader(data))
	}
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	copyErr := limitedCopy(ctx, buf, r)
	closeErr := r.Close()

	// Tolerate read errors that leave valid partial data. Damaged PDFs
	// frequently have truncated zlib streams or bad checksums, but the
	// decompressed bytes up to that point are usable.
	if copyErr != nil && copyErr != io.EOF && copyErr != io.ErrUnexpectedEOF {
		// If we got some data, use it despite the error.
		if buf.Len() == 0 {
			return nil, copyErr
		}
	}

	// Tolerate close errors (checksum mismatches) when we have data.
	if closeErr != nil && buf.Len() == 0 {
		return nil, fmt.Errorf("failed to close zlib reader: %w", closeErr)
	}

	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

func decodeLZW(ctx context.Context, data []byte, parms ast.PdfDict) ([]byte, error) {
	// PDF spec: LZW uses MSB-first order, literal width 8 (codes 0–255),
	// ClearTable=256, EOD=257. /EarlyChange defaults to 1.
	earlyChange := 1
	if parms != nil {
		if ec, ok := parms["EarlyChange"]; ok {
			earlyChange = pdfInt(ec)
		}
	}

	r := newLZWReader(bytes.NewReader(data), earlyChange)
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	err := limitedCopy(ctx, buf, r)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

func decodeASCIIHex(data []byte) ([]byte, error) {
	val := string(data)
	idx := strings.Index(val, ">")
	if idx != -1 {
		val = val[:idx]
	}

	val = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, val)

	if len(val)%2 != 0 {
		val += "0"
	}
	decoded, err := hex.DecodeString(val)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func decodeASCII85(data []byte) ([]byte, error) {
	// PDF ASCII85 streams end with the EOD marker "~>". Strip it (and any
	// trailing garbage) before passing to Go's decoder, which doesn't
	// understand the PDF-specific marker.
	if idx := bytes.Index(data, []byte("~>")); idx != -1 {
		data = data[:idx]
	}

	// Strip whitespace that PDF generators may inject between groups.
	data = bytes.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, data)

	dst := make([]byte, 4*len(data))
	ndst, _, err := ascii85.Decode(dst, data, true)
	if err != nil {
		return nil, fmt.Errorf("ascii85 decode: %w", err)
	}
	return dst[:ndst], nil
}

func decodeCCITTFax(ctx context.Context, data []byte, parms ast.PdfDict) ([]byte, error) {
	// Extract parameters from /DecodeParms (PDF spec Table 11).
	k := 0          // /K: <0 = Group 4, 0 = Group 3 1D, >0 = Group 3 mixed
	columns := 1728 // /Columns: image width in pixels
	rows := 0       // /Rows: image height (0 = unknown)
	blackIs1 := false
	align := false

	if parms != nil {
		if v, ok := parms["K"]; ok {
			k = pdfInt(v)
		}
		if v, ok := parms["Columns"]; ok {
			columns = pdfInt(v)
		}
		if v, ok := parms["Rows"]; ok {
			rows = pdfInt(v)
		}
		if v, ok := parms["BlackIs1"]; ok {
			switch b := v.(type) {
			case ast.PdfBool:
				blackIs1 = bool(b)
			case ast.PdfInt:
				blackIs1 = int(b) != 0
			}
		}
		if v, ok := parms["EncodedByteAlign"]; ok {
			switch b := v.(type) {
			case ast.PdfBool:
				align = bool(b)
			case ast.PdfInt:
				align = int(b) != 0
			}
		}
	}

	// Map PDF /K to ccitt.SubFormat.
	sf := ccitt.Group3
	if k < 0 {
		sf = ccitt.Group4
	}

	height := rows
	if height <= 0 {
		height = ccitt.AutoDetectHeight
	}

	opts := &ccitt.Options{
		Align:  align,
		Invert: blackIs1,
	}

	r := ccitt.NewReader(bytes.NewReader(data), ccitt.MSB, sf, columns, height, opts)
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	err := limitedCopy(ctx, buf, r)
	if err != nil {
		return nil, fmt.Errorf("ccitt decode: %w", err)
	}
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

func decodeBrotli(ctx context.Context, data []byte) ([]byte, error) {
	r := brotli.NewReader(bytes.NewReader(data))
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	err := limitedCopy(ctx, buf, r)
	if err != nil {
		return nil, fmt.Errorf("brotli decode: %w", err)
	}
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// NewStreamReader returns an io.ReadCloser that lazily decodes a PDF stream
// through its filter pipeline. For single FlateDecode or LZWDecode filters
// without predictor parameters, the decoded data is streamed without full
// buffering. For other filter configurations, the stream is decoded eagerly
// into a buffer on the first call.
//
// The caller must close the returned reader when done.
func NewStreamReader(ctx context.Context, stream ast.PdfStream) (io.ReadCloser, error) {
	filterObj, hasFilter := stream.Dict["Filter"]
	if !hasFilter {
		data, err := stream.Bytes(ctx)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	var filters []ast.PdfName
	switch f := filterObj.(type) {
	case ast.PdfName:
		filters = append(filters, f)
	case ast.PdfArray:
		for _, item := range f {
			if name, ok := item.(ast.PdfName); ok {
				filters = append(filters, name)
			}
		}
	default:
		return nil, fmt.Errorf("invalid type for /Filter: %T", filterObj)
	}

	parms := extractDecodeParms(stream.Dict, len(filters))

	// Fast path: single Flate or LZW filter without predictor, true streaming.
	if len(filters) == 1 && extractPredictorParams(parms[0]) == nil {
		switch filters[0] {
		case "FlateDecode", "Fl":
			raw, err := stream.Bytes(ctx)
			if err != nil {
				return nil, err
			}
			return zlib.NewReader(bytes.NewReader(raw))
		case "LZWDecode", "LZW":
			earlyChange := 1
			if parms[0] != nil {
				if ec, ok := parms[0]["EarlyChange"]; ok {
					earlyChange = pdfInt(ec)
				}
			}
			raw, err := stream.Bytes(ctx)
			if err != nil {
				return nil, err
			}
			return io.NopCloser(newLZWReader(bytes.NewReader(raw), earlyChange)), nil
		case "BrotliDecode":
			raw, err := stream.Bytes(ctx)
			if err != nil {
				return nil, err
			}
			return io.NopCloser(brotli.NewReader(bytes.NewReader(raw))), nil
		}
	}

	// Slow path: decode fully and wrap in a reader.
	data, err := DecodeStream(ctx, stream)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// ApplyEncodeFilter applies a single encoding filter to the byte slice.
func ApplyEncodeFilter(data []byte, filterName ast.PdfName) ([]byte, error) {
	switch filterName {
	case "FlateDecode", "Fl":
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil

	case "ASCIIHexDecode", "AHx":
		encoded := hex.EncodeToString(data)
		return []byte(encoded + ">"), nil

	case "BrotliDecode":
		var buf bytes.Buffer
		w := brotli.NewWriter(&buf)
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFilter, filterName)
	}
}
