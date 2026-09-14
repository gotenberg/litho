package compliance

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// hybridXRefPDF builds a hybrid-reference file: a classic cross-reference table
// for readers that predate xref streams, plus an xref STREAM linked from the
// trailer's /XRefStm (ISO 32000-1, 7.5.8.4). Every entry is type 1, so nothing
// in the classic table betrays the stream.
func hybridXRefPDF(withXRefStm bool) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")

	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	// The xref stream itself, object 3. W = [1 3 1].
	const entrySize = 5
	entries := make([]byte, 4*entrySize)
	put := func(idx, typ int, field2 int, field3 int) {
		b := idx * entrySize
		entries[b] = byte(typ)
		entries[b+1] = byte(field2 >> 16)
		entries[b+2] = byte(field2 >> 8)
		entries[b+3] = byte(field2)
		entries[b+4] = byte(field3)
	}
	put(0, 0, 0, 255)
	put(1, 1, off1, 0)
	put(2, 1, off2, 0)

	off3 := buf.Len()
	put(3, 1, off3, 0)

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	_, _ = w.Write(entries)
	_ = w.Close()

	fmt.Fprintf(&buf, "3 0 obj\n<< /Type /XRef /Size 4 /W [1 3 1] /Root 1 0 R"+
		" /Filter /FlateDecode /Length %d >>\nstream\n", compressed.Len())
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	xrefOff := buf.Len()
	buf.WriteString("xref\n0 4\n0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", off1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off3)

	stm := ""
	if withXRefStm {
		stm = fmt.Sprintf(" /XRefStm %d", off3)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 4 /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF\n", stm, xrefOff)
	return buf.Bytes()
}

// TestXRefFormatHybridReference pins that a hybrid-reference file uses an xref
// stream, which ISO 19005-1 §6.1.4 forbids.
//
// Neither of litho's two earlier tests could see it: the classic table supplies
// every entry, so none is compressed, and the trailer is its own dictionary
// rather than the stream's. govdocs thread_400_400545.pdf is such a file, with
// `/Prev 219303 /XRefStm 218949` and a `40 0 obj << /Type /XRef ... >>`, and
// veraPDF answers containsXRefStream true for it while litho passed it.
func TestXRefFormatHybridReference(t *testing.T) {
	t.Parallel()

	load := func(t *testing.T, data []byte) *document.Document {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		return doc
	}

	t.Run("a hybrid trailer reports", func(t *testing.T) {
		t.Parallel()
		got := checkXRefFormat(load(t, hybridXRefPDF(true)), "PDF/A-1b")
		if !hasRule(got, Rule_19005_1_6_1_4_t03) {
			t.Errorf("a /XRefStm trailer must report §6.1.4 t3, got %v", got)
		}
	})

	t.Run("the same file without the link stays silent", func(t *testing.T) {
		t.Parallel()
		got := checkXRefFormat(load(t, hybridXRefPDF(false)), "PDF/A-1b")
		if hasRule(got, Rule_19005_1_6_1_4_t03) {
			t.Errorf("a classic table alone must not report §6.1.4 t3, got %v", got)
		}
	})

	t.Run("parts 2 and later permit xref streams", func(t *testing.T) {
		t.Parallel()
		for _, profile := range []string{"PDF/A-2b", "PDF/A-4"} {
			if got := checkXRefFormat(load(t, hybridXRefPDF(true)), profile); len(got) != 0 {
				t.Errorf("%s states no such restriction, got %v", profile, got)
			}
		}
	})
}
