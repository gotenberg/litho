package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// stampDoc builds a one-page document to stamp.
func stampDoc(t *testing.T) (context.Context, *document.Document, []*document.Page, *writer.ObjAllocator) {
	t.Helper()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	return ctx, doc, pages, writer.NewObjAllocator(doc.HighestObjectNumber())
}

// stampedContent returns every content stream the stamp queued.
func stampedContent(t *testing.T, ctx context.Context, alloc *writer.ObjAllocator) string {
	t.Helper()

	var all strings.Builder
	for _, obj := range alloc.Pending() {
		stream, ok := obj.(ast.PdfStream)
		if !ok {
			continue
		}
		raw, err := stream.Bytes(ctx)
		if err != nil {
			continue
		}
		all.Write(raw)
	}
	return all.String()
}

// TestStampTextWritesWinAnsiBytes pins that a stamp drawn with a standard font
// is written in the encoding that font declares.
//
// A standard font is emitted with /WinAnsiEncoding, so a viewer decodes one byte
// per character. Writing the Go string's UTF-8 bytes instead put two on the page
// for every accented character: stamping "Café" produced "CafÃ©" in poppler and
// in litho's own text extraction, which is the opposite of what the code
// intended when it chose that encoding "so extended Latin characters render
// correctly".
func TestStampTextWritesWinAnsiBytes(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := stampDoc(t)
	if err := StampText(ctx, alloc, doc, pages, TextStampOptions{
		Text:     "Café",
		FontSize: 12,
	}, nil); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	content := stampedContent(t, ctx, alloc)
	// Build the bytes rather than spelling them as escapes: an escape written in
	// the source is at the mercy of whatever wrote the source.
	winAnsi := string([]byte{'C', 'a', 'f', 0xE9})
	utf8Bytes := string([]byte{0xC3, 0xA9})
	if !strings.Contains(content, winAnsi) {
		t.Errorf("the stamp was not written in WinAnsi: %q", content)
	}
	if strings.Contains(content, utf8Bytes) {
		t.Errorf("the stamp reached the content stream as raw UTF-8: %q", content)
	}
}

// TestStampTextRefusesUnencodableText pins that text a standard font cannot
// express is reported rather than stamped as mojibake.
//
// litho used to write the UTF-8 bytes anyway, and both poppler and litho read
// the result back as "Ð¡Ð£Ð’ÐžÐ ÐžÐ’".
func TestStampTextRefusesUnencodableText(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := stampDoc(t)
	err := StampText(ctx, alloc, doc, pages, TextStampOptions{
		Text:     "СУВОРОВ",
		FontSize: 12,
	}, nil)
	if err == nil {
		t.Fatal("stamping text the font cannot express reported success")
	}
	if !strings.Contains(err.Error(), "-font") {
		t.Errorf("the error does not say how to stamp the text: %v", err)
	}
}

// TestStampTextKeepsASCII is the control: ordinary text is unaffected.
func TestStampTextKeepsASCII(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := stampDoc(t)
	if err := StampText(ctx, alloc, doc, pages, TextStampOptions{
		Text:     "PlainStamp",
		FontSize: 12,
	}, nil); err != nil {
		t.Fatalf("StampText: %v", err)
	}
	if content := stampedContent(t, ctx, alloc); !strings.Contains(content, "(PlainStamp) Tj") {
		t.Errorf("the ASCII stamp was not drawn: %q", content)
	}
}

// TestStampTextEscapesDelimiters is the control for the literal string itself:
// a value carrying parentheses must not end the string early.
func TestStampTextEscapesDelimiters(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := stampDoc(t)
	if err := StampText(ctx, alloc, doc, pages, TextStampOptions{
		Text:     "a(b)c",
		FontSize: 12,
	}, nil); err != nil {
		t.Fatalf("StampText: %v", err)
	}
	if content := stampedContent(t, ctx, alloc); !strings.Contains(content, `(a\(b\)c) Tj`) {
		t.Errorf("the parentheses were not escaped: %q", content)
	}
}
