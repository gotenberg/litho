package semantics

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

const samplePLS = `<?xml version="1.0" encoding="UTF-8"?>
<lexicon version="1.0" xmlns="http://www.w3.org/2005/01/pronunciation-lexicon"
  alphabet="ipa" xml:lang="en-US">
  <lexeme>
    <grapheme>tomato</grapheme>
    <phoneme>tÉ™ËˆmeÉªtoÊŠ</phoneme>
  </lexeme>
</lexicon>`

// buildPronunciationPDF constructs a minimal PDF with a /PronunciationLexicon
// entry in the catalog pointing to a FlateDecode stream containing PLS XML.
func buildPronunciationPDF(plsXML string) ([]byte, error) {
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write([]byte(plsXML)); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	compData := compressed.Bytes()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")

	// Object 1: PLS stream
	off1 := buf.Len()
	buf.WriteString(fmt.Sprintf("1 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n", len(compData)))
	buf.Write(compData)
	buf.WriteString("\nendstream\nendobj\n")

	// Object 2: Pages (empty)
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	// Object 3: Catalog with /PronunciationLexicon
	off3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Catalog /Pages 2 0 R /PronunciationLexicon [1 0 R] >>\nendobj\n")

	xrefOff := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off1))
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off2))
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off3))
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size 4 /Root 3 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOff))

	return buf.Bytes(), nil
}

func TestExtractPronunciationLexicons_Found(t *testing.T) {
	pdfData, err := buildPronunciationPDF(samplePLS)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
	if err != nil {
		t.Fatal(err)
	}

	lexicons, err := ExtractPronunciationLexicons(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(lexicons) != 1 {
		t.Fatalf("expected 1 lexicon, got %d", len(lexicons))
	}

	if lexicons[0].XML != samplePLS {
		t.Errorf("XML content mismatch:\ngot:  %q\nwant: %q", lexicons[0].XML, samplePLS)
	}
}

func TestExtractPronunciationLexicons_NoCatalogEntry(t *testing.T) {
	// PDF with no /PronunciationLexicon in catalog.
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Catalog /Pages 1 0 R >>\nendobj\n")
	xrefOff := buf.Len()
	buf.WriteString("xref\n0 3\n")
	buf.WriteString("0000000000 65535 f \n")
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off1))
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off2))
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size 3 /Root 2 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOff))

	pdfData := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
	if err != nil {
		t.Fatal(err)
	}

	lexicons, err := ExtractPronunciationLexicons(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if lexicons != nil {
		t.Errorf("expected nil for no /PronunciationLexicon, got %d", len(lexicons))
	}
}

func TestExtractPronunciationLexicons_MultipleLexicons(t *testing.T) {
	pls1 := `<?xml version="1.0"?><lexicon version="1.0"><lexeme><grapheme>a</grapheme></lexeme></lexicon>`
	pls2 := `<?xml version="1.0"?><lexicon version="1.0"><lexeme><grapheme>b</grapheme></lexeme></lexicon>`

	var comp1, comp2 bytes.Buffer
	w1 := zlib.NewWriter(&comp1)
	_, _ = w1.Write([]byte(pls1))
	_ = w1.Close()
	w2 := zlib.NewWriter(&comp2)
	_, _ = w2.Write([]byte(pls2))
	_ = w2.Close()
	c1, c2 := comp1.Bytes(), comp2.Bytes()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")

	off1 := buf.Len()
	buf.WriteString(fmt.Sprintf("1 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n", len(c1)))
	buf.Write(c1)
	buf.WriteString("\nendstream\nendobj\n")

	off2 := buf.Len()
	buf.WriteString(fmt.Sprintf("2 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n", len(c2)))
	buf.Write(c2)
	buf.WriteString("\nendstream\nendobj\n")

	off3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	off4 := buf.Len()
	buf.WriteString("4 0 obj\n<< /Type /Catalog /Pages 3 0 R /PronunciationLexicon [1 0 R 2 0 R] >>\nendobj\n")

	xrefOff := buf.Len()
	buf.WriteString("xref\n0 5\n")
	buf.WriteString("0000000000 65535 f \n")
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off1))
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off2))
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off3))
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off4))
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size 5 /Root 4 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOff))

	pdfData := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
	if err != nil {
		t.Fatal(err)
	}

	lexicons, err := ExtractPronunciationLexicons(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(lexicons) != 2 {
		t.Fatalf("expected 2 lexicons, got %d", len(lexicons))
	}
	if lexicons[0].XML != pls1 {
		t.Errorf("first lexicon mismatch")
	}
	if lexicons[1].XML != pls2 {
		t.Errorf("second lexicon mismatch")
	}
}
