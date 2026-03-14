package filter

import (
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// DecodeStream decodes a PDF stream based on its /Filter dictionary entry.
// It supports applying multiple filters sequentially if an array is provided.
// /DecodeParms are extracted and passed per filter for predictor processing.
func DecodeStream(stream ast.PdfStream) ([]byte, error) {
	filterObj, hasFilter := stream.Dict["Filter"]
	if !hasFilter {
		return stream.Data, nil
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

	data := stream.Data
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
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r) //nolint:gosec // Need full read
	if closeErr := r.Close(); closeErr != nil {
		return nil, fmt.Errorf("failed to close zlib reader: %w", closeErr)
	}
	// Flate decode often gives unexpected EOF but correct data.
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeLZW(data []byte, parms ast.PdfDict) ([]byte, error) {
	// PDF spec: LZW uses MSB order and starts with 9-bit codes (litWidth=8).
	// /EarlyChange defaults to 1 (standard LZW behavior).
	earlyChange := 1
	if parms != nil {
		if ec, ok := parms["EarlyChange"]; ok {
			earlyChange = pdfInt(ec)
		}
	}

	order := lzw.MSB
	litWidth := 8

	// TODO: When earlyChange is 0, Go's compress/lzw (which defaults to EarlyChange=1)
	// may produce incorrect output. A custom decoder is needed for full correctness.
	_ = earlyChange

	r := lzw.NewReader(bytes.NewReader(data), order, litWidth)
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r) //nolint:gosec // Need full read
	if closeErr := r.Close(); closeErr != nil {
		return nil, fmt.Errorf("failed to close LZW reader: %w", closeErr)
	}
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf.Bytes(), nil
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

// EncodeStream creates a new PdfStream by applying the specified filter.
func EncodeStream(data []byte, filter ast.PdfName) (ast.PdfStream, error) {
	encoded, err := ApplyEncodeFilter(data, filter)
	if err != nil {
		return ast.PdfStream{}, err
	}

	dict := make(ast.PdfDict)
	dict["Filter"] = filter
	dict["Length"] = ast.PdfInt(len(encoded))

	return ast.PdfStream{
		Dict: dict,
		Data: encoded,
	}, nil
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

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFilter, filterName)
	}
}
