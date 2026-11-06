package compose

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// textString encodes s as a PDF text string (ISO 32000-1, 7.9.2.2): as is
// when every byte is printable ASCII, which PDFDocEncoding maps to itself,
// else as UTF-16BE behind the byte order mark FE FF, which carries every
// character. The serializer escapes the delimiters a literal string needs.
//
// It is local rather than document.EncodePdfString so that compose stays
// free of the parsed-document package; it gives up the shorter
// PDFDocEncoding form of Latin-1 text, which costs bytes, never fidelity.
func textString(s string) ast.PdfString {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7E {
			ascii = false
			break
		}
	}
	if ascii {
		return ast.PdfString(s)
	}
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2+2*len(units))
	out = append(out, 0xFE, 0xFF)
	for _, u := range units {
		out = append(out, byte(u>>8), byte(u&0xFF)) //nolint:gosec // G115: a uint16 shifted or masked to 8 bits fits a byte
	}
	return ast.PdfString(out)
}

// uriString returns uri as the 7-bit ASCII string a URI action takes (ISO
// 32000-1, 12.6.4.7, Table 206), percent-encoding every byte outside
// printable ASCII as RFC 3986 does for a URI's octets.
func uriString(uri string) ast.PdfString {
	var b strings.Builder
	for i := 0; i < len(uri); i++ {
		c := uri[i]
		if c <= 0x20 || c >= 0x7F {
			fmt.Fprintf(&b, "%%%02X", c)
			continue
		}
		b.WriteByte(c)
	}
	return ast.PdfString(b.String())
}

// flate returns data zlib-compressed, the format the FlateDecode filter
// reads (ISO 32000-1, 7.4.4). Writing to a bytes.Buffer cannot fail, so
// the errors zlib returns are propagated only for form.
func flate(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	return buf.Bytes(), nil
}

// flateStream returns data as a FlateDecode stream carrying dict.
func flateStream(dict ast.PdfDict, data []byte) (ast.PdfStream, error) {
	z, err := flate(data)
	if err != nil {
		return ast.PdfStream{}, err
	}
	dict["Filter"] = ast.PdfName("FlateDecode")
	return ast.NewStream(dict, z), nil
}

// precision is the number of decimals a content-stream number keeps: a
// ten-thousandth of a point is far below any device's resolution, and the
// shortest exact form of a float64 sum would otherwise run to seventeen
// digits.
const precision = 1e4

// num formats f as a content-stream number, rounded to [precision], in the
// decimal form ISO 32000-1, 7.3.3 requires.
func num(f float64) string {
	return writer.FormatReal(round(f))
}

// round rounds f to [precision], turning a negative zero into zero.
func round(f float64) float64 {
	r := math.Round(f*precision) / precision
	if r == 0 {
		return 0
	}
	return r
}

// pdfReal returns f rounded as a PDF real object.
func pdfReal(f float64) ast.PdfReal {
	return ast.PdfReal(round(f))
}
