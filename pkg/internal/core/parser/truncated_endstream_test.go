package parser

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// TestStreamLengthSurvivesTruncatedEndstream pins that a correct /Length is
// honoured when the end-of-stream keyword is damaged. Rejecting a truncated
// keyword makes the fallback scan find no "endstream" at all and return the
// rest of the file as stream data, so each stream swallows the bytes after it.
//
// # Corpus evidence
//
//   - pdfjs issue10004.pdf: writes every stream end as a truncated
//     "endstrea". Rejecting those made each stream swallow the bytes after it,
//     and remediation grew the file from 757 KB to 12.6 MB.
func TestStreamLengthSurvivesTruncatedEndstream(t *testing.T) {
	t.Parallel()
	const body = "HELLO-STREAM-BODY"
	for _, tc := range []struct {
		name   string
		marker string
	}{
		{"intact", "endstream"},
		{"truncated", "endstrea"},
		{"truncated to endstr", "endstr"},
		{"endobj only", "endobj"},
	} {
		src := []byte("<< /Length " + itoa(len(body)) + " >>\nstream\n" + body + "\r\n" + tc.marker +
			"\rendobj\n% trailing bytes that must NOT become stream data\n")
		p := NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(src), int64(len(src)), 0)))
		obj, err := p.ParseObject(context.Background())
		if err != nil {
			t.Errorf("%s: ParseObject: %v", tc.name, err)
			continue
		}
		st, ok := obj.(ast.PdfStream)
		if !ok {
			t.Errorf("%s: got %T, want ast.PdfStream", tc.name, obj)
			continue
		}
		raw, err := st.Bytes(context.Background())
		if err != nil {
			t.Errorf("%s: Bytes: %v", tc.name, err)
			continue
		}
		if string(raw) != body {
			t.Errorf("%s: stream body = %q (%d bytes), want %q", tc.name, string(raw), len(raw), body)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
