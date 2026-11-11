package office

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
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
// token past it fails with ErrInputTooLarge before the decoder holds it.
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
		if err := decodeAll(NewTokenReader(strings.NewReader(doc), 64)); !errors.Is(err, ErrInputTooLarge) {
			t.Errorf("%s: %v, want ErrInputTooLarge", name, err)
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
	if _, err := in.Read(buf); !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("Read past the cap = %v", err)
	}
	in.Mark()
	if n, err := in.Read(buf); n != 10 || err != nil {
		t.Fatalf("Read after Mark = %d, %v", n, err)
	}
}
