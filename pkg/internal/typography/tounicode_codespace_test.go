package typography

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// TestDecodeNextCode_IdentityCodeOutsideToUnicodeCodespace pins that an
// Identity-H font's two-byte code reads through the /ToUnicode entry for
// that code even when the CMap's codespace ranges leave the code out: the
// encoding CMap splits the string into codes (ISO 32000-1, 9.7.6.2), and
// /ToUnicode maps those codes (9.10.3). The CMap below declares <00> to <FF>
// one byte long and <0100> to <FFFF> two, so its own codespace would split
// <0029> into the one-byte <00> and a shifted rest.
func TestDecodeNextCode_IdentityCodeOutsideToUnicodeCodespace(t *testing.T) {
	t.Parallel()

	tu, err := cmap.Parse(context.Background(), []byte("begincmap\n"+
		"2 begincodespacerange\n<00> <FF>\n<0100> <FFFF>\nendcodespacerange\n"+
		"5 beginbfchar\n<0000> <FFFD>\n<0029> <0048>\n<0046> <0065>\n<004D> <006C>\n<0050> <006F>\nendbfchar\nendcmap\n"))
	if err != nil {
		t.Fatalf("cmap.Parse: %v", err)
	}
	font := &Font{Subtype: "Type0", ToUnicode: tu, identityEncoding: true}
	data := []byte{0x00, 0x29, 0x00, 0x46, 0x00, 0x4D, 0x00, 0x4D, 0x00, 0x50}
	var got string
	for len(data) > 0 {
		char, n, _, _ := decodeNextCode(font, data)
		if n <= 0 {
			t.Fatalf("decodeNextCode consumed %d bytes", n)
		}
		got += char
		data = data[n:]
	}
	if got != "Hello" {
		t.Fatalf("decoded %q, want %q", got, "Hello")
	}
}

// TestDecodeNextCode_IdentityControlCodeStaysTwoBytes pins that an
// Identity-H code in the C0 control range that the /ToUnicode CMap neither
// maps nor holds in its codespace reads as no character and consumes its
// two bytes, so the code after it stays aligned: govdocs
// thread_300_300034.pdf shows <0004> four times under a CMap whose only
// codespace range is <0006>, and read a byte at a time "<0004><0004>" came
// out "Ѐ".
func TestDecodeNextCode_IdentityControlCodeStaysTwoBytes(t *testing.T) {
	t.Parallel()

	tu, err := cmap.Parse(context.Background(), []byte("begincmap\n"+
		"1 begincodespacerange\n<0006> <0006>\nendcodespacerange\n"+
		"1 beginbfrange\n<0006> <0006> <0023>\nendbfrange\nendcmap\n"))
	if err != nil {
		t.Fatalf("cmap.Parse: %v", err)
	}
	font := &Font{Subtype: "Type0", ToUnicode: tu, identityEncoding: true}
	data := []byte{0x00, 0x04, 0x00, 0x04, 0x00, 0x06}
	var got []string
	for len(data) > 0 {
		char, n, _, _ := decodeNextCode(font, data)
		if n != 2 {
			t.Fatalf("decodeNextCode consumed %d bytes of % X, want 2", n, data)
		}
		got = append(got, char)
		data = data[n:]
	}
	if want := []string{"", "", "#"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("decoded %q, want %q", got, want)
	}
}

// TestDecodeNextCode_SimpleFontMappedPairStaysOneByte pins that a simple
// font's codes stay one byte each (ISO 32000-1, 9.6.1) when its /ToUnicode
// CMap declares a two-byte codespace and maps a byte pair of the string: the
// pair <4142> names no code of the font. Each byte reads its own entry, <41>
// or <0042> at the code's value, as PDF.js reads it, and a byte no entry maps
// takes the font's encoding.
func TestDecodeNextCode_SimpleFontMappedPairStaysOneByte(t *testing.T) {
	t.Parallel()

	tu, err := cmap.Parse(context.Background(), []byte("begincmap\n"+
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"+
		"3 beginbfchar\n<4142> <0078>\n<41> <0041>\n<0042> <0042>\nendbfchar\nendcmap\n"))
	if err != nil {
		t.Fatalf("cmap.Parse: %v", err)
	}
	font := &Font{Subtype: "TrueType", ToUnicode: tu, Encoding: map[byte]rune{'C': 'C'}}
	data := []byte("ABC")
	var got string
	for len(data) > 0 {
		char, n, skip, _ := decodeNextCode(font, data)
		if n != 1 || skip {
			t.Fatalf("code %q: consumed %d bytes, skip %v; want 1 byte, no skip", data[0], n, skip)
		}
		got += char
		data = data[n:]
	}
	if got != "ABC" {
		t.Errorf("decoded %q, want %q", got, "ABC")
	}
}
