package document

import (
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// EncodePdfString encodes s as a PDF text string.
//
// Per ISO 32000-1, 7.9.2.2 a text string is either PDFDocEncoding or UTF-16BE
// with a leading byte order mark, and nothing else. Writing a Go string's raw
// UTF-8 bytes satisfies neither: a conforming reader sees no BOM, decodes the
// bytes as PDFDocEncoding, and renders "RIL’s" as "RILa€™s".
// litho read its own output back correctly only because [DecodePdfString]
// tolerates stray UTF-8, so the damage was invisible in-tool and real
// everywhere else.
//
// PDFDocEncoding is preferred when every rune is representable, because it is
// half the size and what most producers emit. Anything else, including a rune
// that would land on a byte Table D.2 leaves undefined, forces UTF-16BE: that
// form can carry every rune, so falling back is never lossy. A bookmark title
// holding U+000B took this route, and encoding it as the undefined byte 0x0B
// had silently dropped the character on the way back in.
func EncodePdfString(s string) ast.PdfString {
	if b, ok := encodePdfDocEncoding(s); ok {
		return ast.PdfString(b)
	}
	return ast.PdfString(encodeUTF16BE(s))
}

// EncodePdfDocString renders s in PDFDocEncoding, reporting whether every rune
// was representable.
//
// Use it for the byte-oriented strings that are NOT text strings and so may not
// take the UTF-16BE form: a file specification's /F is one (ISO 32000-1,
// 7.11.2), where the Unicode spelling belongs in the companion /UF. Callers
// keep their own bytes when the answer is false.
func EncodePdfDocString(s string) (ast.PdfString, bool) {
	b, ok := encodePdfDocEncoding(s)
	if !ok {
		return "", false
	}
	return ast.PdfString(b), true
}

// encodePdfDocEncoding renders s in PDFDocEncoding, reporting whether every
// rune was representable.
func encodePdfDocEncoding(s string) ([]byte, bool) {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		b, ok := pdfDocByte(r)
		if !ok {
			return nil, false
		}
		out = append(out, b)
	}
	return out, true
}

// pdfDocByte returns the PDFDocEncoding byte for r.
//
// The mapping is the inverse of the decode path: printable ASCII and
// U+00A0-U+00FF pass through, HT/LF/FF/CR are the four low controls Table D.2
// defines, and [pdfDocEncoding] covers the bytes whose meaning differs from
// Latin-1. Every other rune is reported unrepresentable, which is the safe
// answer: the caller then writes UTF-16BE, which can carry anything.
func pdfDocByte(r rune) (byte, bool) {
	switch {
	case r >= 0x20 && r <= 0x7E:
		return byte(r), true
	case r == '\t' || r == '\n' || r == '\f' || r == '\r':
		return byte(r), true
	case r >= 0xA0 && r <= 0xFF:
		return byte(r), true
	}
	for b, mapped := range pdfDocEncoding {
		if mapped == r {
			return b, true
		}
	}
	return 0, false
}

// encodeUTF16BE renders s as UTF-16BE preceded by the byte order mark ISO
// 32000-1, 7.9.2.2 requires for the form.
func encodeUTF16BE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2+2*len(units))
	out = append(out, utf16BEBom0, utf16BEBom1)
	for _, u := range units {
		// Both halves are masked to 8 bits before the conversion, so neither
		// can overflow a byte.
		out = append(out, byte((u>>8)&0xFF), byte(u&0xFF))
	}
	return out
}
