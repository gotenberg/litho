package writer

import (
	"context"
	"fmt"
	"io"
	"maps"
	"sort"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// Serialize writes a [ast.PdfObject] to w in spec-compliant PDF syntax.
// Streams write their raw bytes directly (content-preserving). Returns the
// number of bytes written.
func Serialize(ctx context.Context, w io.Writer, obj ast.PdfObject) (int64, error) {
	if obj == nil {
		n, err := io.WriteString(w, "null")
		return int64(n), err
	}
	switch v := obj.(type) {
	case ast.PdfNull:
		n, err := io.WriteString(w, "null")
		return int64(n), err
	case ast.PdfBool:
		if v {
			n, err := io.WriteString(w, "true")
			return int64(n), err
		}
		n, err := io.WriteString(w, "false")
		return int64(n), err
	case ast.PdfInt:
		n, err := io.WriteString(w, strconv.FormatInt(int64(v), 10))
		return int64(n), err
	case ast.PdfReal:
		n, err := io.WriteString(w, formatReal(float64(v)))
		return int64(n), err
	case ast.PdfName:
		return serializeName(w, v)
	case ast.PdfString:
		return serializeString(w, v)
	case ast.PdfArray:
		return serializeArray(ctx, w, v)
	case ast.PdfDict:
		return serializeDict(ctx, w, v)
	case ast.PdfIndirectRef:
		s := fmt.Sprintf("%d %d R", v.ObjectNum, v.GenNum)
		n, err := io.WriteString(w, s)
		return int64(n), err
	case ast.PdfStream:
		return serializeStream(ctx, w, v)
	default:
		return 0, fmt.Errorf("unsupported PDF object type %T", obj)
	}
}

// SerializeIndirect writes "N 0 obj\n<object>\nendobj\n" and returns the
// number of bytes written.
func SerializeIndirect(ctx context.Context, w io.Writer, objNum int, obj ast.PdfObject) (int64, error) {
	var total int64

	header := fmt.Sprintf("%d 0 obj\n", objNum)
	n, err := io.WriteString(w, header)
	total += int64(n)
	if err != nil {
		return total, err
	}

	n2, err := Serialize(ctx, w, obj)
	total += n2
	if err != nil {
		return total, err
	}

	n, err = io.WriteString(w, "\nendobj\n")
	total += int64(n)
	return total, err
}

// formatReal formats a float64 for PDF output, trimming unnecessary trailing
// zeros while preserving enough precision for PDF coordinates.
func formatReal(f float64) string {
	// Use enough precision for PDF coordinates (sub-point accuracy).
	s := strconv.FormatFloat(f, 'f', 6, 64)
	// Trim trailing zeros after the decimal point.
	if idx := len(s) - 1; s[idx] == '0' {
		for idx > 0 && s[idx] == '0' {
			idx--
		}
		if s[idx] == '.' {
			idx-- // remove the decimal point too if all zeros
		}
		s = s[:idx+1]
	}
	// A nonzero magnitude smaller than the 6-decimal grid rounds to "0",
	// silently dropping the value on round-trip. ISO 32000-1 Annex C
	// permits reals down to ~1.18e-38, so fall back to the shortest
	// exponent-free form (strconv 'f' with -1 precision never emits an
	// exponent, which PDF reals forbid) so small values survive.
	if f != 0 && (s == "0" || s == "-0") {
		s = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return s
}

// serializeName writes a PDF name with proper #XX hex-escaping for bytes
// outside printable ASCII or PDF delimiter characters.
func serializeName(w io.Writer, name ast.PdfName) (int64, error) {
	var buf []byte
	buf = append(buf, '/')
	for i := 0; i < len(string(name)); i++ {
		b := string(name)[i]
		if needsNameEscape(b) {
			buf = append(buf, '#')
			buf = append(buf, hexDigit(b>>4))
			buf = append(buf, hexDigit(b&0x0f))
		} else {
			buf = append(buf, b)
		}
	}
	n, err := w.Write(buf)
	return int64(n), err
}

// needsNameEscape returns true for bytes that must be hex-escaped in a PDF
// name token per ISO 32000 §7.3.5.
func needsNameEscape(b byte) bool {
	if b <= 0x20 || b >= 0x7f {
		return true // control chars, space, non-ASCII
	}
	switch b {
	case '#', '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true // PDF delimiters and the escape char itself
	}
	return false
}

func hexDigit(b byte) byte {
	const hex = "0123456789ABCDEF"
	return hex[b&0x0f]
}

// serializeString writes a PDF string. Uses literal form (...) for printable
// ASCII content, hex form <...> otherwise.
func serializeString(w io.Writer, s ast.PdfString) (int64, error) {
	data := []byte(s)

	// Check if all bytes are printable ASCII (and safe for literal form).
	useLiteral := true
	for _, b := range data {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			useLiteral = false
			break
		}
		if b > 0x7e {
			useLiteral = false
			break
		}
	}

	if useLiteral {
		return serializeLiteralString(w, data)
	}
	return serializeHexString(w, data)
}

func serializeLiteralString(w io.Writer, data []byte) (int64, error) {
	var buf []byte
	buf = append(buf, '(')
	for _, b := range data {
		switch b {
		case '(', ')', '\\':
			buf = append(buf, '\\', b)
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		case '\t':
			buf = append(buf, '\\', 't')
		default:
			buf = append(buf, b)
		}
	}
	buf = append(buf, ')')
	n, err := w.Write(buf)
	return int64(n), err
}

func serializeHexString(w io.Writer, data []byte) (int64, error) {
	buf := make([]byte, 0, 2*len(data)+2)
	buf = append(buf, '<')
	for _, b := range data {
		buf = append(buf, hexDigit(b>>4), hexDigit(b&0x0f))
	}
	buf = append(buf, '>')
	n, err := w.Write(buf)
	return int64(n), err
}

// serializeArray writes a PDF array with space-separated elements.
func serializeArray(ctx context.Context, w io.Writer, arr ast.PdfArray) (int64, error) {
	var total int64
	n, err := io.WriteString(w, "[")
	total += int64(n)
	if err != nil {
		return total, err
	}
	for i, elem := range arr {
		if err := ctx.Err(); err != nil {
			return total, fmt.Errorf("serializing array element %d: %w", i, err)
		}
		if i > 0 {
			n, err = io.WriteString(w, " ")
			total += int64(n)
			if err != nil {
				return total, err
			}
		}
		n2, err := Serialize(ctx, w, elem)
		total += n2
		if err != nil {
			return total, err
		}
	}
	n, err = io.WriteString(w, "]")
	total += int64(n)
	return total, err
}

// serializeDict writes a PDF dictionary with sorted keys for deterministic
// output.
func serializeDict(ctx context.Context, w io.Writer, d ast.PdfDict) (int64, error) {
	var total int64
	n, err := io.WriteString(w, "<<")
	total += int64(n)
	if err != nil {
		return total, err
	}

	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)

	for _, k := range keys {
		if err := ctx.Err(); err != nil {
			return total, fmt.Errorf("serializing dict key %q: %w", k, err)
		}
		v := d[ast.PdfName(k)]
		n, err = io.WriteString(w, " ")
		total += int64(n)
		if err != nil {
			return total, err
		}
		n2, err := serializeName(w, ast.PdfName(k))
		total += n2
		if err != nil {
			return total, err
		}
		n, err = io.WriteString(w, " ")
		total += int64(n)
		if err != nil {
			return total, err
		}
		n2, err = Serialize(ctx, w, v)
		total += n2
		if err != nil {
			return total, err
		}
	}

	n, err = io.WriteString(w, ">>")
	total += int64(n)
	return total, err
}

// serializeStream writes a PDF stream: dict, "stream\r\n", raw bytes,
// "\r\nendstream". The /Length key in the dict is updated to match the
// actual raw byte count.
func serializeStream(ctx context.Context, w io.Writer, s ast.PdfStream) (int64, error) {
	data, err := s.Bytes(ctx)
	if err != nil {
		return 0, fmt.Errorf("reading stream bytes: %w", err)
	}
	if data == nil {
		data = []byte{}
	}

	// Update /Length in the dict to match actual data.
	dict := make(ast.PdfDict, len(s.Dict)+1)
	maps.Copy(dict, s.Dict)
	dict["Length"] = ast.PdfInt(len(data))

	var total int64
	n2, err := serializeDict(ctx, w, dict)
	total += n2
	if err != nil {
		return total, err
	}

	n, err := io.WriteString(w, "\nstream\r\n")
	total += int64(n)
	if err != nil {
		return total, err
	}

	n, err = w.Write(data)
	total += int64(n)
	if err != nil {
		return total, err
	}

	n, err = io.WriteString(w, "\r\nendstream")
	total += int64(n)
	return total, err
}
