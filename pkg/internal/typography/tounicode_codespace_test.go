package typography

import (
	"context"
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
