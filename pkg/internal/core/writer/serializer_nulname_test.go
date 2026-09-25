package writer

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// TestSerialize_NameHoldingNullIsRefused pins that a name holding NUL, which
// ISO 32000-1, 7.3.5 excludes from every name, is refused with
// [ErrNameHoldsNull] rather than written as "#00" or renamed by dropping the
// byte. The refusal names the offending name and writes nothing, whether the
// name is a value or a dictionary key.
func TestSerialize_NameHoldingNullIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		obj  ast.PdfObject
	}{
		{"value", ast.PdfName("F\x001")},
		{"leading", ast.PdfName("\x00")},
		{"key", ast.PdfDict{"Type": ast.PdfName("Font"), "F\x001": ast.PdfInt(1)}},
		{"nested", ast.PdfArray{ast.PdfInt(1), ast.PdfDict{"Font": ast.PdfDict{"F\x001": ast.PdfNull{}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			n, err := Serialize(context.Background(), &buf, tc.obj)
			if !errors.Is(err, ErrNameHoldsNull) {
				t.Fatalf("errors.Is(err, ErrNameHoldsNull) = false, err = %v, wrote %q", err, buf.String())
			}
			if strings.Contains(buf.String(), "#00") {
				t.Errorf("wrote the excluded character as #00: %q", buf.String())
			}
			if tc.name == "value" && (n != 0 || buf.Len() != 0) {
				t.Errorf("wrote %d bytes (reported %d) for a refused name, want 0", buf.Len(), n)
			}
		})
	}

	_, err := Serialize(context.Background(), &bytes.Buffer{}, ast.PdfName("F\x001"))
	if !strings.Contains(err.Error(), `"F\x001"`) {
		t.Errorf("the error does not name the refused name: %v", err)
	}
}

// TestSerializeIndirect_NameFromNullEscapeIsRefused pins the path a NUL takes
// into a name: the lexer decodes a "#00" escape in the source, as PDF.js and
// veraPDF do, and the writer then refuses the object instead of writing the
// escape back.
func TestSerializeIndirect_NameFromNullEscapeIsRefused(t *testing.T) {
	t.Parallel()

	src := []byte("<< /Font << /F#001 5 0 R >> >>")
	p := parser.NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(src), int64(len(src)), 0)))
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject: %v", err)
	}
	var buf bytes.Buffer
	if _, err := SerializeIndirect(context.Background(), &buf, 7, 0, obj); !errors.Is(err, ErrNameHoldsNull) {
		t.Fatalf("errors.Is(err, ErrNameHoldsNull) = false, err = %v, wrote %q", err, buf.String())
	}
}

// TestSerialize_NameEscapesRoundTrip pins that every byte other than NUL
// still survives a write and a read, escaped where ISO 32000-1, 7.3.5 wants
// its hexadecimal code.
func TestSerialize_NameEscapesRoundTrip(t *testing.T) {
	t.Parallel()

	var all []byte
	for b := 1; b < 256; b++ {
		all = append(all, byte(b))
	}
	for _, name := range []string{"#", "F#23", "A B", "()<>[]{}/%", "\x01\xff", string(all)} {
		got := roundTrip(t, ast.PdfName(name))
		if v, ok := got.(ast.PdfName); !ok || string(v) != name {
			t.Errorf("round-trip %q: got %v", name, got)
		}
	}
}
