package parser

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestParseStream_EncryptedStreamNotTrimmed pins that a stream in an encrypted
// document keeps its exact /Length bytes. The trailing-EOL trim that helps
// plain producers (which fold the newline before "endstream" into /Length)
// must never run on an encrypted stream: /Length there is the exact
// IV+ciphertext count and the last byte is random ciphertext. Dropping one that
// happens to be CR or LF makes the ciphertext non-block-aligned, so AES
// decryption bails and the still-encrypted bytes reach the filter, a silent,
// IV-dependent content loss.
func TestParseStream_EncryptedStreamNotTrimmed(t *testing.T) {
	t.Parallel()

	// Six bytes of data whose last byte is a newline, mimicking a ciphertext
	// block that happens to end in 0x0A, followed by the EOL before "endstream".
	const src = "<< /Length 6 >>\nstream\nHello\n\nendstream"
	identity := func(b []byte) ([]byte, error) { return b, nil }

	t.Run("encrypted: full length kept", func(t *testing.T) {
		t.Parallel()
		p := newParser(src).WithDecryption(identity)
		obj, err := p.ParseObject(context.Background())
		if err != nil {
			t.Fatalf("ParseObject: %v", err)
		}
		s, ok := obj.(ast.PdfStream)
		if !ok {
			t.Fatalf("got %T, want ast.PdfStream", obj)
		}
		b, err := s.Bytes(context.Background())
		if err != nil {
			t.Fatalf("Bytes: %v", err)
		}
		if len(b) != 6 {
			t.Errorf("encrypted stream length = %d, want 6 (trailing newline must not be trimmed)", len(b))
		}
	})

	t.Run("plain: trailing EOL trimmed", func(t *testing.T) {
		t.Parallel()
		p := newParser(src)
		obj, err := p.ParseObject(context.Background())
		if err != nil {
			t.Fatalf("ParseObject: %v", err)
		}
		s, ok := obj.(ast.PdfStream)
		if !ok {
			t.Fatalf("got %T, want ast.PdfStream", obj)
		}
		b, err := s.Bytes(context.Background())
		if err != nil {
			t.Fatalf("Bytes: %v", err)
		}
		if len(b) != 5 {
			t.Errorf("plain stream length = %d, want 5 (trailing newline trimmed)", len(b))
		}
	})
}

// TestParseStream_XRefStreamNotTrimmed pins that a cross-reference stream's
// exact /Length is preserved even when its binary entry table ends in a real
// 0x0A/0x0D byte. Trimming it (as for a plain textual stream) shortens the
// table, fails the xref parse, and forces a raw-scan rebuild that loses every
// compressed object — comments.pdf's 200-object ObjStm, holding the page-tree
// root, vanished and 0 of 14 pages resolved.
func TestParseStream_XRefStreamNotTrimmed(t *testing.T) {
	t.Parallel()

	// Data "Hello\n" (6 bytes, last byte 0x0A), then the separator EOL before
	// "endstream" (ISO 32000-1 §7.3.8.1). A plain stream would trim to 5; a
	// cross-reference stream must keep all 6.
	const src = "<< /Type /XRef /Length 6 >>\nstream\nHello\n\nendstream"

	p := newParser(src)
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject: %v", err)
	}
	s, ok := obj.(ast.PdfStream)
	if !ok {
		t.Fatalf("got %T, want ast.PdfStream", obj)
	}
	b, err := s.Bytes(context.Background())
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if len(b) != 6 {
		t.Errorf("xref stream length = %d, want 6 (exact /Length must not be trimmed)", len(b))
	}
}
