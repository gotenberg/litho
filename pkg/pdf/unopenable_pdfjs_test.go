package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// onePagePDF builds a one-page document whose page dictionary carries
// pageExtra and whose trailer carries trailerExtra. extra objects are numbered
// from 4.
func onePagePDF(pageExtra, trailerExtra string, extra ...string) []byte {
	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " + pageExtra + " >>",
	}, extra...)
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, trailerExtra, start)
	return b.Bytes()
}

// TestContentsNullIsAnEmptyPage pins that a page whose /Contents is the null
// object reads as a page with no /Contents (ISO 32000-1, 7.3.7), as pdf.js and
// poppler read the pdfjs file issue15803.pdf, rather than failing extraction
// with "invalid /Contents type".
func TestContentsNullIsAnEmptyPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name      string
		pageExtra string
		extra     []string
	}{
		{"direct null", "/Contents null", nil},
		{"reference to null", "/Contents 4 0 R", []string{"null"}},
		{"reference to a missing object", "/Contents 9 0 R", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := onePagePDF(tc.pageExtra, "", tc.extra...)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			if _, err := r.ExtractText(ctx, 1); err != nil && !errors.Is(err, ErrRequiresOCR) {
				t.Errorf("ExtractText: %v, want the empty-page outcome", err)
			}
		})
	}
}

// TestUnusableEncryptIsIgnored pins that a trailer /Encrypt that names no
// encryption dictionary leaves the document unencrypted, as pdf.js (XRef.parse
// keeps a cipher only for an /Encrypt that is a Dict) and poppler open it. The
// pdfjs file PDFBOX-4352-0.pdf carries the unparsable shape.
func TestUnusableEncryptIsIgnored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		value string
		extra []string
	}{
		{"unparsable object", "/Encrypt 4 0 R", []string{"E< /Filter /Standard /V 5 /R 6 >>"}},
		{"reference to null", "/Encrypt 4 0 R", []string{"null"}},
		{"reference to a missing object", "/Encrypt 9 0 R", nil},
		{"integer", "/Encrypt 7", nil},
		{"name", "/Encrypt /Standard", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := onePagePDF("", tc.value, tc.extra...)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			if r.IsEncrypted() {
				t.Errorf("IsEncrypted = true, want an unencrypted document")
			}
			if n, err := r.PageCount(ctx); err != nil || n != 1 {
				t.Errorf("PageCount = %d, %v, want 1", n, err)
			}
		})
	}
}

// TestShortKeyV4EncryptionDecrypts opens the pdfjs files whose /V 4 handler
// has a 40-bit key, which only Acrobat and pdf.js read (see the crypto
// package's objKeyBase). Skips when the corpus is not provisioned.
func TestShortKeyV4EncryptionDecrypts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, name := range []string{"issue19484_1.pdf", "issue19484_2.pdf"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("..", "..", "test", "integration", "testdata", "pdfjs", "pdfs", name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("corpus file not provisioned: %v", err)
			}
			r, err := OpenFile(ctx, path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer r.Close()
			text, err := r.ExtractText(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if !strings.Contains(text, "under-documented encryption scheme") {
				t.Errorf("page 1 text = %.80q, want the decrypted text", text)
			}
		})
	}
}
