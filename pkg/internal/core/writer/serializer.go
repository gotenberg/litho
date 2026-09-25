package writer

import (
	"context"
	"fmt"
	"io"
	"maps"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// Serialize writes a [ast.PdfObject] to w in spec-compliant PDF syntax.
// Streams write their raw bytes directly (content-preserving). Returns the
// number of bytes written. A name holding a NUL byte has no conforming
// spelling (ISO 32000-1, 7.3.5), so it fails with [ErrNameHoldsNull].
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
		return 0, fmt.Errorf("%w: %T", ErrUnsupportedObject, obj)
	}
}

// SerializeIndirect writes "N G obj\n<object>\nendobj\n" and returns the
// number of bytes written.
//
// An indirect reference identifies its object by object number AND generation
// number (ISO 32000-1, 7.3.10), so a replacement written in an incremental
// update keeps the generation the file's references already name. Objects
// created fresh, and every object in a full rewrite, take generation 0.
func SerializeIndirect(ctx context.Context, w io.Writer, objNum, genNum int, obj ast.PdfObject) (int64, error) {
	var total int64

	header := fmt.Sprintf("%d %d obj\n", objNum, genNum)
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

// formatReal formats a float64 for PDF output as the shortest decimal that
// reads back as the same value.
//
// Precision 'f' with -1 is what keeps a copied value intact. A fixed six
// decimals rounds, so an object litho merely carried across came out holding a
// different number: a tint transform written .1000000015 was re-emitted as
// 0.1, and the copy then disagreed with the original the file still contained
// (ISO 19005-2, 6.2.4.4 test 2 compares the PDF objects, not what they
// compute). 'f' never emits an exponent, which a PDF real forbids
// (ISO 32000-1, 7.3.3), and it drops trailing zeros on its own.
func formatReal(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	// The shortest form of a whole number carries no decimal point, and a
	// magnitude the 64-bit range cannot hold then reads back as an
	// out-of-range integer rather than a real, so keep the point on those.
	if !strings.ContainsRune(s, '.') && (f >= math.MaxInt64 || f <= math.MinInt64) {
		s += ".0"
	}
	return s
}

// FormatReal renders a PDF real in the decimal form ISO 32000-1, 7.3.3
// requires: one or more digits with an optional sign and period, never an
// exponent. Content-stream writers outside this package need the same
// rendering, and Go's %g emits exponential form for small magnitudes, which a
// conforming reader rejects.
func FormatReal(f float64) string {
	return formatReal(f)
}

// SerializeName writes name to w in the spelling [Serialize] gives it and
// returns the number of bytes written. Content-stream writers outside this
// package serialise their operands one by one and need the same spelling: a
// name written raw ends at its first white space or delimiter, and a "#" in it
// reads back as the start of an escape (ISO 32000-1, 7.3.5). A name holding a
// NUL byte fails with [ErrNameHoldsNull].
func SerializeName(w io.Writer, name ast.PdfName) (int64, error) {
	return serializeName(w, name)
}

// serializeName writes a PDF name with proper #XX hex-escaping for bytes
// outside printable ASCII or PDF delimiter characters.
//
// A name holding a NUL byte is refused with [ErrNameHoldsNull] before a byte
// is written. ISO 32000-1, 7.3.5 builds a name from "any characters (8-bit
// values) except null (character code 0)", so no spelling of such a name
// conforms. It reaches the writer from a "#00" escape in the source, which
// the lexer decodes as PDF.js and veraPDF do. Writing "#00" back repeats the
// fault. Dropping the byte renames the name, which can collide with a name
// the same dictionary already holds, and orphans every reference still
// spelling it the old way, such as a resource name in a content stream the
// writer copies byte for byte. Refusing writes no invalid name and changes no
// other.
//
// # Corpus evidence
//
//   - All corpora: no file holds a NUL in a name of its trailer or of any
//     object its cross-reference data lists, so no full rewrite refuses.
func serializeName(w io.Writer, name ast.PdfName) (int64, error) {
	if strings.IndexByte(string(name), 0) >= 0 {
		return 0, fmt.Errorf("%w: %q", ErrNameHoldsNull, string(name))
	}
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
