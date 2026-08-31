package parser

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// parseWithDecryption parses src with a decryption callback that flips every
// bit, so a decrypted string is trivially distinguishable from one left alone.
func parseWithDecryption(t *testing.T, src string) ast.PdfDict {
	t.Helper()

	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader([]byte(src)), int64(len(src)), 0))
	p := NewParser(lex).WithDecryption(func(b []byte) ([]byte, error) {
		out := make([]byte, len(b))
		for i, c := range b {
			out[i] = ^c
		}
		return out, nil
	})

	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("parsing %q: %v", src, err)
	}
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("parsed %T, want a dict", obj)
	}
	return dict
}

// TestSignatureContentsIsNotDecrypted pins that the signature value survives an
// encrypted file untouched.
//
// ISO 32000-1, 7.6.2 exempts a signature dictionary's /Contents from the
// document's string encryption, because the digest is computed over the file
// with that hole excised and the bytes go in as they are. Decrypting them makes
// noise of the PKCS#7 blob, and under AES it also eats the leading 16-byte
// initialisation vector: pdfjs xfa_issue14071.pdf carries a 16288-byte
// signature that litho read back as 16272 bytes of garbage, so no signature on
// an encrypted document could ever verify.
func TestSignatureContentsIsNotDecrypted(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		src  string
	}{
		{"typed", "<< /Type /Sig /ByteRange [0 1 2 3] /Contents <414243> >>"},
		// /Type is optional in ISO 32000-1, Table 252, so the /ByteRange
		// pairing has to be enough on its own.
		{"untyped", "<< /ByteRange [0 1 2 3] /Contents <414243> >>"},
		{"timestamp", "<< /Type /DocTimeStamp /Contents <414243> >>"},
		// /Type follows /Contents here, so the decision cannot be made at the
		// moment the string is read.
		{"type after contents", "<< /Contents <414243> /Type /Sig >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dict := parseWithDecryption(t, tc.src)
			got, ok := dict["Contents"].(ast.PdfString)
			if !ok {
				t.Fatalf("/Contents is %T, want a string", dict["Contents"])
			}
			if string(got) != "ABC" {
				t.Errorf("/Contents = %q, want %q: the signature value was decrypted", got, "ABC")
			}
		})
	}
}

// TestNonSignatureContentsIsStillDecrypted is the control: /Contents on an
// annotation is ordinary text (ISO 32000-1, Table 168) and is encrypted like
// any other string, so the exemption must not reach it.
func TestNonSignatureContentsIsStillDecrypted(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		src  string
	}{
		{"annotation", "<< /Type /Annot /Subtype /Text /Contents <414243> >>"},
		{"untyped", "<< /Contents (ABC) >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dict := parseWithDecryption(t, tc.src)
			got, ok := dict["Contents"].(ast.PdfString)
			if !ok {
				t.Fatalf("/Contents is %T, want a string", dict["Contents"])
			}
			if string(got) != "\xbe\xbd\xbc" {
				t.Errorf("/Contents = %q, want the decrypted bytes: an annotation's text is encrypted", got)
			}
		})
	}
}

// TestPageContentsIsUnaffected pins that a /Contents that is not a string at
// all still parses normally: on a page it is a stream reference or an array of
// them (ISO 32000-1, Table 30).
func TestPageContentsIsUnaffected(t *testing.T) {
	t.Parallel()

	dict := parseWithDecryption(t, "<< /Type /Page /Contents [4 0 R 5 0 R] >>")
	arr, ok := dict["Contents"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Contents is %T, want an array", dict["Contents"])
	}
	if len(arr) != 2 {
		t.Errorf("/Contents holds %d entries, want 2", len(arr))
	}
}
