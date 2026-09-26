package parser

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
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

	t.Run("plain: exact /Length keeps its last byte", func(t *testing.T) {
		t.Parallel()
		// Same bytes as the encrypted case: the separator before "endstream"
		// survives, so /Length 6 stopped at the last data byte and that byte is
		// data even though it is a newline.
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
			t.Errorf("plain stream length = %d, want 6 (a counted newline is data)", len(b))
		}
	})

	t.Run("plain: folded EOL trimmed", func(t *testing.T) {
		t.Parallel()
		// No separator: "endstream" follows the counted bytes directly, so the
		// producer folded the marker into /Length. That is the case the trim
		// exists for, and it still runs.
		p := newParser("<< /Length 6 >>\nstream\nHello\nendstream")
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
			t.Errorf("plain stream length = %d, want 5 (folded newline trimmed)", len(b))
		}
	})
}

// TestParseStream_CountedNewlineTerminatesComment pins the reason a counted
// trailing newline matters: it can be the only thing ending a comment.
//
// Trimming a counted newline that ends a comment leaves the comment
// unterminated, and a comment runs to the end of the line (ISO 32000-1, 7.2.4),
// so on concatenation it swallows whatever opens the next part of a /Contents
// array.
//
// # Corpus evidence
//
//   - pdfjs annotation-as.pdf: splits a page's content into a /Contents array
//     whose first part is the single comment "% CANON_PFINF_TYPE2_TEXTON\n",
//     declared as /Length 27 with the separator written after it. Trimming
//     the counted newline swallowed the "q" that opened the next part. The
//     unbalanced q/Q pair moved the rest of the page out of view and
//     `litho split` produced a page whose text no reader could find.
func TestParseStream_CountedNewlineTerminatesComment(t *testing.T) {
	t.Parallel()

	const comment = "% CANON_PFINF_TYPE2_TEXTON\n"
	src := "<< /Length 27 >>\nstream\n" + comment + "\nendstream"

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
	if string(b) != comment {
		t.Errorf("stream = %q, want %q (the comment must keep its terminator)", b, comment)
	}
}

// TestParseStream_XRefStreamNotTrimmed pins that a cross-reference stream's
// exact /Length is preserved even when its binary entry table ends in a real
// 0x0A/0x0D byte. Trimming it (as for a plain textual stream) shortens the
// table, fails the xref parse, and forces a raw-scan rebuild that loses every
// compressed object.
//
// # Corpus evidence
//
//   - pdfjs comments.pdf: its 200-object ObjStm, holding the page-tree root,
//     vanished and 0 of 14 pages resolved.
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

// flateEndingIn returns zlib data (RFC 1950) whose last byte, the low byte of
// its Adler-32 checksum, is last. The payload varies until the checksum
// cooperates, which takes a few hundred tries at most.
func flateEndingIn(t *testing.T, last byte) []byte {
	t.Helper()
	for i := range 1 << 16 {
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		if _, err := fmt.Fprintf(zw, "BT /F1 12 Tf 72 712 Td (line %d) Tj ET", i); err != nil {
			t.Fatalf("compressing: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("closing: %v", err)
		}
		if b := buf.Bytes(); b[len(b)-1] == last {
			return b
		}
	}
	t.Fatalf("no payload yields zlib data ending in %#x", last)
	return nil
}

// completeZlib reports whether data holds a whole zlib stream, its checksum
// included: compress/zlib verifies the checksum once it reaches the end of the
// deflate data, and fails with io.ErrUnexpectedEOF when bytes of it are
// missing.
func completeZlib(data []byte) error {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, zr); err != nil {
		return err
	}
	return zr.Close()
}

// TestParseStream_FlateChecksumNotTrimmed pins that a FlateDecode stream keeps
// every byte its /Length counts when no end-of-line marker separates them from
// "endstream". The last one can be the final byte of the zlib checksum, and a
// CR or LF there is data, not the marker ISO 32000-1, 7.3.8.1 places before
// "endstream". Trimming it truncated the stream, and every copy litho wrote of
// it carried the truncated checksum: qpdf warns "input stream is complete but
// output may still be valid" on it.
//
// The folded case keeps its newline too: zlib data delimits itself, and no
// decoder reads a byte after the checksum.
//
// # Corpus evidence
//
//   - pdfcpu golang.pdf: objects 20 and 88 end their checksum in CR, directly
//     followed by "endstream".
//   - pdfcpu CenterOfWhy.pdf: object 661 ends its checksum in CR and folds an
//     LF into /Length, and the trim took the pair for one CRLF marker.
func TestParseStream_FlateChecksumNotTrimmed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"checksum ends in CR", flateEndingIn(t, '\r')},
		{"checksum ends in LF", flateEndingIn(t, '\n')},
		{"checksum ends in CR, folded LF", append(flateEndingIn(t, '\r'), '\n')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>\nstream\n%sendstream", len(tc.data), tc.data)
			obj, err := newParser(src).ParseObject(context.Background())
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
			if err := completeZlib(b); err != nil {
				t.Errorf("stream keeps %d of the %d bytes /Length counts and is no "+
					"longer a whole zlib stream: %v", len(b), len(tc.data), err)
			}
		})
	}
}

// TestParseStream_UnfilteredStreamStillTrimmed is the control that confines the
// change to FlateDecode: an unfiltered stream whose /Length folds in the
// newline before "endstream" still loses it.
func TestParseStream_UnfilteredStreamStillTrimmed(t *testing.T) {
	t.Parallel()
	obj, err := newParser("<< /Length 6 >>\nstream\n0 0 m\nendstream").ParseObject(context.Background())
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
	if string(b) != "0 0 m" {
		t.Errorf("stream = %q, want %q (the folded newline trimmed)", b, "0 0 m")
	}
}
