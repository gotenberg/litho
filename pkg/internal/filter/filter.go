package filter

import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
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

// ErrDecompressionBomb is returned when a stream exceeds maxDecompressedSize.
var ErrDecompressionBomb = fmt.Errorf("decompressed stream exceeds %d MB limit", maxDecompressedSize>>20)

// limitedCopy copies from r to buf up to maxDecompressedSize bytes.
// Returns ErrDecompressionBomb if the limit is exceeded.
func limitedCopy(buf *bytes.Buffer, r io.Reader) error {
	limited := io.LimitReader(r, maxDecompressedSize+1)
	_, err := io.Copy(buf, limited)
	if buf.Len() > maxDecompressedSize {
		return ErrDecompressionBomb
	}
	return err
}

// bufPool reuses bytes.Buffer instances across stream decompression calls
// to reduce GC pressure from repeated allocations.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// DecodeStream decodes a PDF stream based on its /Filter dictionary entry.
// It supports applying multiple filters sequentially if an array is provided.
// /DecodeParms are extracted and passed per filter for predictor processing.
func DecodeStream(stream ast.PdfStream) ([]byte, error) {
	filterObj, hasFilter := stream.Dict["Filter"]
	if !hasFilter {
		return stream.Bytes()
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

	// Extract per-filter DecodeParms. The value can be a single dict (applied
	// to the single filter) or an array of dicts matching the filter array.
	parms := extractDecodeParms(stream.Dict, len(filters))

	data, err := stream.Bytes()
	if err != nil {
		return nil, err
	}
	for i, f := range filters {
		decoded, err := applyDecodeFilter(data, f, parms[i])
		if err != nil {
			return nil, err
		}
		data = decoded
	}

	return data, nil
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
func applyDecodeFilter(data []byte, filterName ast.PdfName, parms ast.PdfDict) ([]byte, error) {
	switch filterName {
	case "FlateDecode", "Fl":
		decoded, err := decodeFlateDecode(data)
		if err != nil {
			return nil, err
		}
		return applyPredictor(decoded, extractPredictorParams(parms))

	case "LZWDecode", "LZW":
		decoded, err := decodeLZW(data, parms)
		if err != nil {
			return nil, err
		}
		return applyPredictor(decoded, extractPredictorParams(parms))

	case "ASCIIHexDecode", "AHx":
		return decodeASCIIHex(data)

	case "ASCII85Decode", "A85":
		return decodeASCII85(data)

	case "CCITTFaxDecode", "CCF":
		return decodeCCITTFax(data, parms)

	case "BrotliDecode":
		return decodeBrotli(data)

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

// ApplyDecodeFilter applies a single decoding filter to the given byte slice.
// This is the public API that does not accept DecodeParms (for backward compatibility).
func ApplyDecodeFilter(data []byte, filterName ast.PdfName) ([]byte, error) {
	return applyDecodeFilter(data, filterName, nil)
}

func decodeFlateDecode(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	copyErr := limitedCopy(buf, r)
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

func decodeLZW(data []byte, parms ast.PdfDict) ([]byte, error) {
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
	err := limitedCopy(buf, r)
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

func decodeCCITTFax(data []byte, parms ast.PdfDict) ([]byte, error) {
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
	err := limitedCopy(buf, r)
	if err != nil {
		return nil, fmt.Errorf("ccitt decode: %w", err)
	}
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

func decodeBrotli(data []byte) ([]byte, error) {
	r := brotli.NewReader(bytes.NewReader(data))
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	err := limitedCopy(buf, r)
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
func NewStreamReader(stream ast.PdfStream) (io.ReadCloser, error) {
	filterObj, hasFilter := stream.Dict["Filter"]
	if !hasFilter {
		data, err := stream.Bytes()
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

	// Fast path: single Flate or LZW filter without predictor — true streaming.
	if len(filters) == 1 && extractPredictorParams(parms[0]) == nil {
		switch filters[0] {
		case "FlateDecode", "Fl":
			raw, err := stream.Bytes()
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
			raw, err := stream.Bytes()
			if err != nil {
				return nil, err
			}
			return io.NopCloser(newLZWReader(bytes.NewReader(raw), earlyChange)), nil
		case "BrotliDecode":
			raw, err := stream.Bytes()
			if err != nil {
				return nil, err
			}
			return io.NopCloser(brotli.NewReader(bytes.NewReader(raw))), nil
		}
	}

	// Slow path: decode fully and wrap in a reader.
	data, err := DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// EncodeStream creates a new PdfStream by applying the specified filter.
func EncodeStream(data []byte, filter ast.PdfName) (ast.PdfStream, error) {
	encoded, err := ApplyEncodeFilter(data, filter)
	if err != nil {
		return ast.PdfStream{}, err
	}

	dict := make(ast.PdfDict)
	dict["Filter"] = filter
	dict["Length"] = ast.PdfInt(len(encoded))

	return ast.NewStream(dict, encoded), nil
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
