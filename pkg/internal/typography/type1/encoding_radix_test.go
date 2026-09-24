package type1

import "testing"

// radixVector is an encoding vector written with PostScript radix numbers
// (PostScript Language Reference, 3.2.2), such as "dup 8#40 /space put".
//
// # Corpus evidence
//
//   - govdocs thread_100_100480.pdf: its Universal News with Commercial Pi
//     program writes its vector this way, as in "dup 8#40 /space put".
var radixVector = []byte("%!PS-AdobeFont-1.0: Test\n/Encoding 256 array\n0 1 255 {1 index exch /.notdef put} for\n" +
	"dup 8#101 /A put\ndup 16#42 /B put\ndup 67 /C put\ndup 99#1 /bad put\nreadonly def\ncurrentfile eexec\n")

// TestParseEncodingPSRadixCodes pins that [ParseEncodingPS] reads radix
// indices, as veraPDF does, and skips a radix out of range.
func TestParseEncodingPSRadixCodes(t *testing.T) {
	t.Parallel()
	enc := ParseEncodingPS(radixVector)
	for code, want := range map[int]string{65: "A", 66: "B", 67: "C"} {
		if enc[code] != want {
			t.Errorf("code %d = %q, want %q", code, enc[code], want)
		}
	}
	if enc[1] != "" {
		t.Errorf("an out-of-range radix named code 1 %q", enc[1])
	}
}

// TestParseEncodingDecimalCodes pins that [ParseEncoding] reads decimal
// indices only, as pdf.js's Type1Parser does, so text extraction keeps
// agreeing with its pdf.js reference.
func TestParseEncodingDecimalCodes(t *testing.T) {
	t.Parallel()
	enc := ParseEncoding(radixVector)
	if enc[67] != "C" {
		t.Errorf("code 67 = %q, want C", enc[67])
	}
	if enc[65] != "" || enc[66] != "" {
		t.Errorf("radix entries were read: 65=%q 66=%q", enc[65], enc[66])
	}
}

// TestExtractAdvanceWidthDiv pins that a div before hsbw is evaluated, as a
// fractional advance such as "78 113889 100 div hsbw" (1138.89 units)
// requires.
//
// # Corpus evidence
//
//   - pdfjs tracemonkey.pdf: the circlecopyrt glyph of its CMSY7 subset sets
//     its advance as "78 113889 100 div hsbw", 1138.89 units.
func TestExtractAdvanceWidthDiv(t *testing.T) {
	t.Parallel()
	w, ok := extractAdvanceWidth([]byte{217, 255, 0, 1, 188, 225, 239, 12, 12, 13})
	if !ok || w != 1138.89 {
		t.Errorf("width = %v, %v; want 1138.89, true", w, ok)
	}
	if _, ok := extractAdvanceWidth([]byte{217, 239, 139, 12, 12, 13}); ok {
		t.Error("a division by zero produced a width")
	}
}
