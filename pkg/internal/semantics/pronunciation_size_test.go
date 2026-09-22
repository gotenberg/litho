package semantics

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// buildLexiconsPDF generalises buildPronunciationPDF to several entries, so the
// bound can be driven across the array rather than from one oversized stream.
// Objects 1..N are the PLS streams, N+1 is /Pages and N+2 the catalog.
func buildLexiconsPDF(t *testing.T, xmls []string) []byte {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, 0, len(xmls)+2)

	for i, xml := range xmls {
		var comp bytes.Buffer
		w := zlib.NewWriter(&comp)
		if _, err := w.Write([]byte(xml)); err != nil {
			t.Fatalf("zlib write: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("zlib close: %v", err)
		}
		offs = append(offs, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n", i+1, comp.Len())
		buf.Write(comp.Bytes())
		buf.WriteString("\nendstream\nendobj\n")
	}

	pagesNum := len(xmls) + 1
	offs = append(offs, buf.Len())
	fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n", pagesNum)

	refs := make([]string, 0, len(xmls))
	for i := range xmls {
		refs = append(refs, fmt.Sprintf("%d 0 R", i+1))
	}
	catalogNum := len(xmls) + 2
	offs = append(offs, buf.Len())
	fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /Catalog /Pages %d 0 R /PronunciationLexicon [%s] >>\nendobj\n",
		catalogNum, pagesNum, strings.Join(refs, " "))

	xrefOff := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", catalogNum+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		catalogNum+1, catalogNum, xrefOff)

	return buf.Bytes()
}

func lexiconDoc(t *testing.T, xmls []string) *document.Document {
	t.Helper()
	data := buildLexiconsPDF(t, xmls)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestExtractPronunciationLexicons_BoundsTotal pins
// maxPronunciationLexiconBytes across the array.
//
// Each entry here is well under the limit on its own; only their sum exceeds
// it. Every entry decodes under its own decompression cap, so before the bound
// the total grew with the number of entries a file named: measured on 20
// compressed streams, 328 KB of input decoded to 160 MB.
func TestExtractPronunciationLexicons_BoundsTotal(t *testing.T) {
	const each = 4 << 20 // three of these pass 10 MB, one alone does not

	lexicon := strings.Repeat("a", each)
	doc := lexiconDoc(t, []string{lexicon, lexicon, lexicon})

	got, err := ExtractPronunciationLexicons(context.Background(), doc)
	if err == nil {
		t.Fatalf("got %d lexicons with no error; three entries of %d bytes must exceed the %d byte bound",
			len(got), each, maxPronunciationLexiconBytes)
	}
	if got != nil {
		t.Errorf("returned %d lexicons alongside the error", len(got))
	}
	if !strings.Contains(err.Error(), "exceed") {
		t.Errorf("error = %q, want it to name the limit it hit", err)
	}
}

// TestExtractPronunciationLexicons_AcceptsALargeLexicon is the control. The
// bound test would pass just as well on a build that refused every lexicon.
//
// No corpus file carries /PronunciationLexicon at all, so there is no measured
// real maximum to sit under. 1 MB is far past the kilobytes a real PLS document
// runs to, and must still be accepted.
func TestExtractPronunciationLexicons_AcceptsALargeLexicon(t *testing.T) {
	const size = 1 << 20

	doc := lexiconDoc(t, []string{strings.Repeat("b", size)})

	got, err := ExtractPronunciationLexicons(context.Background(), doc)
	if err != nil {
		t.Fatalf("refused a %d byte lexicon: %v", size, err)
	}
	if len(got) != 1 || len(got[0].XML) != size {
		t.Errorf("got %d lexicons, want 1 of %d bytes", len(got), size)
	}
}
