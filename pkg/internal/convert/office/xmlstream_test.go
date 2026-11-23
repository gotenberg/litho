package office

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// decodeAll decodes every token of in, marking each, and returns the
// decoder's last error, nil at the end of the input.
func decodeAll(in *TokenReader) error {
	d := xml.NewDecoder(in)
	for {
		if _, err := d.RawToken(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		in.Mark()
	}
}

// TestTokenReader_CapsOneToken pins the cap on one token: a document of
// many tokens each under it reads to its end however long it is, and one
// token past it fails with limits.ErrInputTooLarge before the decoder holds it.
func TestTokenReader_CapsOneToken(t *testing.T) {
	t.Parallel()
	long := "<a>" + strings.Repeat("<b>12345678</b>", 1000) + "</a>"
	if err := decodeAll(NewTokenReader(strings.NewReader(long), 64)); err != nil {
		t.Fatalf("many small tokens: %v", err)
	}
	for name, doc := range map[string]string{
		"character data": "<a>" + strings.Repeat("x", 65) + "</a>",
		"white space":    "<?xml version=\"1.0\"?>" + strings.Repeat(" ", 200) + "<a/>",
		"attribute":      `<a v="` + strings.Repeat("x", 100) + `"/>`,
		"comment":        "<a><!--" + strings.Repeat("x", 100) + "--></a>",
	} {
		if err := decodeAll(NewTokenReader(strings.NewReader(doc), 64)); !errors.Is(err, limits.ErrInputTooLarge) {
			t.Errorf("%s: %v, want limits.ErrInputTooLarge", name, err)
		}
	}
}

// TestTokenReader_CharsetInput checks a charset reader reading the
// TokenReader through Read: the cap still applies.
func TestTokenReader_CharsetInput(t *testing.T) {
	t.Parallel()
	in := NewTokenReader(strings.NewReader(strings.Repeat("y", 100)), 10)
	buf := make([]byte, 64)
	n, err := in.Read(buf)
	if n != 10 || err != nil {
		t.Fatalf("Read = %d, %v; want 10 bytes", n, err)
	}
	if _, err := in.Read(buf); !errors.Is(err, limits.ErrInputTooLarge) {
		t.Fatalf("Read past the cap = %v", err)
	}
	in.Mark()
	if n, err := in.Read(buf); n != 10 || err != nil {
		t.Fatalf("Read after Mark = %d, %v", n, err)
	}
}

// utf16 encodes s as UTF-16 in the byte order big says, its byte order
// mark first.
func utf16(s string, big bool) []byte {
	out := []byte{0xFF, 0xFE}
	if big {
		out = []byte{0xFE, 0xFF}
	}
	for _, r := range s {
		hi, lo := byte(r>>8), byte(r) //nolint:gosec // the two bytes of a code unit of the BMP
		if big {
			out = append(out, hi, lo)
		} else {
			out = append(out, lo, hi)
		}
	}
	return out
}

// TestTokenReader_DecodesUTF16 checks that a stream opening with a UTF-16
// byte order mark reads as UTF-8 without the mark (XML 1.0, 4.3.3), in
// both byte orders, and that a stream without one is read as it is.
func TestTokenReader_DecodesUTF16(t *testing.T) {
	t.Parallel()
	const doc = `<?xml version="1.0"?><a>été</a>`
	for _, big := range []bool{false, true} {
		got, err := io.ReadAll(NewTokenReader(strings.NewReader(string(utf16(doc, big))), 0))
		if err != nil || string(got) != doc {
			t.Errorf("big endian %t: %q, %v; want %q", big, got, err, doc)
		}
	}
	for _, plain := range []string{doc, "", "<"} {
		got, err := io.ReadAll(NewTokenReader(strings.NewReader(plain), 0))
		if err != nil || string(got) != plain {
			t.Errorf("%q read as %q, %v", plain, got, err)
		}
	}
}
