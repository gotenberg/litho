package writer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestWriteXRefTable_EntryWidth pins ISO 32000-1, 7.5.4: every cross-reference
// entry occupies exactly 20 bytes including its end-of-line marker. A 21-byte
// entry leaves the reader one byte out of step from the second entry onward,
// which strict readers report as an invalid entry for object 1.
func TestWriteXRefTable_EntryWidth(t *testing.T) {
	records := []XRefRecord{
		{ObjNum: 0, Offset: 0, Generation: 65535, Free: true},
		{ObjNum: 1, Offset: 15, Generation: 0},
		{ObjNum: 2, Offset: 284, Generation: 0},
		{ObjNum: 3, Offset: 1234567890, Generation: 65535},
	}
	trailer := ast.PdfDict{
		"Size": ast.PdfInt(4),
		"Root": ast.PdfIndirectRef{ObjectNum: 1, GenNum: 0},
	}

	var buf bytes.Buffer
	if _, _, err := WriteXRefTable(context.Background(), &buf, records, trailer); err != nil {
		t.Fatalf("WriteXRefTable: %v", err)
	}

	out := buf.Bytes()
	start := bytes.Index(out, []byte("0 4\n"))
	if start < 0 {
		t.Fatalf("missing subsection header, got:\n%s", out)
	}
	start += len("0 4\n")
	end := bytes.Index(out, []byte("trailer"))
	if end < 0 {
		t.Fatalf("missing trailer keyword, got:\n%s", out)
	}

	body := out[start:end]
	if len(body) != 20*len(records) {
		t.Fatalf("entry region is %d bytes for %d entries (%.1f each), want %d",
			len(body), len(records), float64(len(body))/float64(len(records)), 20*len(records))
	}

	for i := range records {
		entry := body[20*i : 20*(i+1)]
		if got := len(entry); got != 20 {
			t.Errorf("entry %d: %d bytes, want 20", i, got)
		}
		// Each entry must be self-delimiting: no EOL byte may leak into the
		// next entry's leading offset field.
		if strings.ContainsAny(string(entry[:18]), "\r\n") {
			t.Errorf("entry %d begins with an end-of-line byte, entries are misaligned: %q", i, entry)
		}
		if entry[18] != '\r' || entry[19] != '\n' {
			t.Errorf("entry %d does not end with CRLF: %q", i, entry)
		}
		if kind := entry[17]; kind != 'n' && kind != 'f' {
			t.Errorf("entry %d type keyword is %q, want 'n' or 'f'", i, kind)
		}
	}
}
