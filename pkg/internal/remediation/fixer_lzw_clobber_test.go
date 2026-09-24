package remediation

import (
	"bytes"
	"compress/lzw"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// lzwBody returns payload compressed the way an LZWDecode stream carries it.
// The payload stays far below the 511 codes where the code width grows, so
// the /EarlyChange default of ISO 32000-1, Table 8 never comes into play.
func lzwBody(t *testing.T, payload string) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, lzw.MSB, 8)
	if _, err := w.Write([]byte(payload)); err != nil {
		t.Fatalf("lzw write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("lzw close: %v", err)
	}
	return buf.Bytes()
}

// lzwImagePDF builds a page drawing object 5, a 2x2 grey image whose body is
// compressed with filter and whose dictionary asks for /Interpolate true.
// ISO 19005-1, 6.2.4 forbids the interpolation and 6.1.10 the LZWDecode filter,
// so a PDF/A-1b run dispatches the images fixer and then the lzw fixer on the
// same object.
func lzwImagePDF(t *testing.T, filter string, body []byte) []byte {
	t.Helper()

	const content = "q 10 0 0 10 0 0 cm /Im0 Do Q\n"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R "+
			"/Resources << /XObject << /Im0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray "+
			"/BitsPerComponent 8 /Interpolate true /Filter /%s /Length %d >>\nstream\n%s\nendstream",
			filter, len(body), body),
	)
}

// TestFixLZW_KeepsTheImagesFixersPendingDictWrite pins that the LZW re-encoding
// starts from the stream the images fixer queued, not from the document.
//
// fixLZW visits every object in the file and re-encodes each LZWDecode stream
// at its own object number. It read each one with doc.Resolve, so a stream an
// earlier fixer had already rewritten went back out as the on-disk copy.
// images runs immediately before lzw in the registry and rewrites an image's
// DICTIONARY only, carrying the LZW body across untouched, so an LZW image
// with /Interpolate true is exactly the object both fixers write: the
// re-encoded image came back with /Interpolate true.
//
// The filter-case repair in the same loop has the same read, so the second
// case runs it over a /Flatedecode image.
//
// The fixpoint loop in pkg/pdf hides this in the output: the next round sees
// /Interpolate true again and repeats the repair. Only a unit test sees the
// first round discard it.
func TestFixLZW_KeepsTheImagesFixersPendingDictWrite(t *testing.T) {
	t.Parallel()

	const pixels = "\x00\x40\x80\xff"
	for _, tc := range []struct {
		name       string
		filter     string
		body       func(*testing.T) []byte
		wantFilter ast.PdfName
	}{
		{
			name:       "LZWDecode image",
			filter:     "LZWDecode",
			body:       func(t *testing.T) []byte { return lzwBody(t, pixels) },
			wantFilter: "FlateDecode",
		},
		{
			name:       "case-misspelled filter name",
			filter:     "Flatedecode",
			body:       func(t *testing.T) []byte { return flateBody(t, pixels) },
			wantFilter: "FlateDecode",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, lzwImagePDF(t, tc.filter, tc.body(t)))

			const imgObj = 5

			if _, err := fixImageIssues(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
				t.Fatalf("fixImageIssues: %v", err)
			}
			seeded, ok := pendingStream(t, alloc, imgObj)
			if !ok || seeded.Dict["Interpolate"] != ast.PdfBool(false) {
				t.Fatalf("control: images did not queue /Interpolate false at object %d, so there "+
					"is no earlier edit to lose; pending = %v", imgObj, alloc.Pending()[imgObj])
			}

			if _, err := fixLZW(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
				t.Fatalf("fixLZW: %v", err)
			}

			got, ok := pendingStream(t, alloc, imgObj)
			if !ok {
				t.Fatalf("object %d is no longer pending", imgObj)
			}
			if f := got.Dict["Filter"]; f != tc.wantFilter {
				t.Fatalf("/Filter is %v, want /%s: the repair did not rewrite the image, so the "+
					"check below proves nothing", f, tc.wantFilter)
			}
			if v := got.Dict["Interpolate"]; v != ast.PdfBool(false) {
				t.Errorf("/Interpolate is %v, want false: fixLZW rebuilt the image from the "+
					"document and discarded the images fixer's write", v)
			}
			decoded, err := doc.DecodeStream(ctx, got)
			if err != nil {
				t.Fatalf("decoding the rewritten image: %v", err)
			}
			if string(decoded) != pixels {
				t.Errorf("the image decodes to %q, want %q", decoded, pixels)
			}
		})
	}
}

// lzwContentPDF builds a page whose content, object 4, is LZWDecode-compressed
// and carries the undefined operator "xyz" that ISO 19005-2, 6.2.2 forbids and
// the unknown-ops fixer strips.
func lzwContentPDF(t *testing.T) []byte {
	t.Helper()

	body := lzwBody(t, "0 0 m 10 10 l S\nxyz\n")
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Filter /LZWDecode /Length %d >>\nstream\n%s\nendstream", len(body), body),
	)
}

// TestFixLZW_KeepsAnEarlierContentStreamRewrite pins that the LZW re-encoding
// leaves alone a content stream an earlier fixer already rewrote.
//
// The content-tier fixers ahead of lzw (unknown-ops, text-control, fonts and
// the others that edit page content) write the new content back UNFILTERED.
// fixLZW read the document's copy instead, found the original LZWDecode stream
// there, and re-encoded THAT at the same object number, so the page went out
// with the operator unknown-ops had just removed. Unlike the images case, the
// earlier fixer's whole edit is lost, not one key.
func TestFixLZW_KeepsAnEarlierContentStreamRewrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, lzwContentPDF(t))

	const contentObj = 4

	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if body := pendingContent(t, doc, alloc, contentObj); strings.Contains(body, "xyz") || body == "" {
		t.Fatalf("control: unknown-ops did not strip the operator at object %d, so there is no "+
			"earlier edit to lose; content = %q", contentObj, body)
	}

	if _, err := fixLZW(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixLZW: %v", err)
	}

	if body := pendingContent(t, doc, alloc, contentObj); strings.Contains(body, "xyz") {
		t.Errorf("the undefined operator is back in object %d: fixLZW re-encoded the document's "+
			"LZW stream over unknown-ops' rewrite; content = %q", contentObj, body)
	}
}

// pendingContent decodes the stream pending at objNum, whatever filter it
// carries, and fails the test when there is none.
func pendingContent(t *testing.T, doc *document.Document, alloc *writer.ObjAllocator, objNum int) string {
	t.Helper()

	s, ok := pendingStream(t, alloc, objNum)
	if !ok {
		t.Fatalf("object %d is not pending", objNum)
	}
	data, err := doc.DecodeStream(context.Background(), s)
	if err != nil {
		t.Fatalf("decoding object %d: %v", objNum, err)
	}
	return string(data)
}
