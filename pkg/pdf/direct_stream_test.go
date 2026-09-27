package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// directStreamPDF builds a one-page document holding two streams inline, which
// ISO 32000-1, 7.3.8.1 forbids and litho's parser accepts: a form XObject
// written directly in the page's /XObject resources, and a content stream
// written directly in its /Contents array, which draws the form. Each stream
// shows its own text, so an output that lost or garbled one reads without it.
func directStreamPDF() []byte {
	inline := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	form := inline("/Type /XObject /Subtype /Form /BBox [0 0 300 50] /Resources << /Font << /F1 5 0 R >> >>",
		"BT /F1 12 Tf 0 10 Td (drawn by the direct form) Tj ET")
	content := inline("", "q 1 0 0 1 72 600 cm /Fm0 Do Q BT /F1 12 Tf 72 650 Td (shown by the direct content) Tj ET")
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> /XObject << /Fm0 " +
			form + " >> >> /Contents [4 0 R " + content + "] >>",
		inline("", "BT /F1 12 Tf 72 700 Td (shown by the indirect content) Tj ET"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// directStreamTexts is the text [directStreamPDF] shows, one per stream.
var directStreamTexts = []string{"drawn by the direct form", "shown by the direct content", "shown by the indirect content"}

// streamsInsideObjects counts the streams data holds inside another object,
// its trailer included, reading it with password.
func streamsInsideObjects(t *testing.T, data []byte, password string) int {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	if doc.IsEncrypted {
		if err := doc.Authenticate(password); err != nil {
			t.Fatalf("authenticating the output: %v", err)
		}
	}
	var count func(obj ast.PdfObject, top bool) int
	count = func(obj ast.PdfObject, top bool) int {
		n := 0
		switch v := obj.(type) {
		case ast.PdfStream:
			if !top {
				n++
			}
			for _, e := range v.Dict {
				n += count(e, false)
			}
		case ast.PdfDict:
			for _, e := range v {
				n += count(e, false)
			}
		case ast.PdfArray:
			for _, e := range v {
				n += count(e, false)
			}
		}
		return n
	}
	direct := count(doc.Index.Trailer, false)
	for num, entry := range doc.XRefEntries() {
		if entry.Free || num == 0 {
			continue
		}
		gen := entry.Generation
		if entry.ObjStmNum != 0 {
			gen = 0
		}
		obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: num, GenNum: gen})
		if err != nil {
			t.Fatalf("resolving object %d of the output: %v", num, err)
		}
		direct += count(obj, true)
	}
	return direct
}

// TestDirectStream_SourceReads is the control for the write tests below: litho
// reads the text of every stream the source holds inline, so text missing from
// an output was lost by the write.
func TestDirectStream_SourceReads(t *testing.T) {
	t.Parallel()
	data := directStreamPDF()
	if n := streamsInsideObjects(t, data, ""); n != 2 {
		t.Fatalf("the source holds %d streams inside another object, want 2", n)
	}
	assertDirectStreamText(t, data, "", 1)
}

// assertDirectStreamText fails unless every page of data shows the text of all
// three streams of [directStreamPDF].
func assertDirectStreamText(t *testing.T, data []byte, password string, pages int) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithPassword(password))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	n, err := r.PageCount(ctx)
	if err != nil || n != pages {
		t.Fatalf("PageCount = %d, %v; want %d", n, err, pages)
	}
	for p := 1; p <= n; p++ {
		text, err := r.ExtractText(ctx, p)
		if err != nil {
			t.Fatalf("ExtractText(%d): %v", p, err)
		}
		for _, want := range directStreamTexts {
			if !strings.Contains(text, want) {
				t.Errorf("page %d lost %q: %q", p, want, text)
			}
		}
	}
}

// TestDirectStream_EveryWritePathWritesItIndirect pins ISO 32000-1, 7.3.8.1 on
// every path that writes a document: each stream the source holds inline goes
// out as an indirect object with a reference in its place, and the page still
// shows what it drew. The encrypted path moves the streams before encryption
// keys their data to an object number, so its text reads back only if that
// order holds. AES-128 derives each object's key from its number (ISO 32000-1,
// 7.6.2, Algorithm 1), where AES-256 uses the file key for every object and
// would read back either way.
func TestDirectStream_EveryWritePathWritesItIndirect(t *testing.T) {
	t.Parallel()
	encryption := EncryptConfig{UserPassword: "u", OwnerPassword: "o", Algorithm: EncryptAES128}
	for _, tc := range []struct {
		name     string
		password string
		pages    int
		write    func(ctx context.Context, r, r2 *Reader, dst *bytes.Buffer) error
	}{
		{name: "full rewrite", pages: 1, write: func(ctx context.Context, r, _ *Reader, dst *bytes.Buffer) error {
			return writeDirectStream(ctx, NewWriter(r, WithFullRewrite()), dst, nil)
		}},
		{name: "object streams", pages: 1, write: func(ctx context.Context, r, _ *Reader, dst *bytes.Buffer) error {
			return writeDirectStream(ctx, NewWriter(r, WithFullRewrite(), WithObjectStreams(), WithDedup()), dst, nil)
		}},
		{name: "incremental update", pages: 1, write: func(ctx context.Context, r, _ *Reader, dst *bytes.Buffer) error {
			return writeDirectStream(ctx, NewWriter(r), dst, func(w *Writer) error { return w.RotatePages(90, 1) })
		}},
		{name: "encryption", password: "u", pages: 1, write: func(ctx context.Context, r, _ *Reader, dst *bytes.Buffer) error {
			return writeDirectStream(ctx, NewWriter(r, WithEncryption(encryption)), dst, nil)
		}},
		{name: "remediation", pages: 1, write: func(ctx context.Context, r, _ *Reader, dst *bytes.Buffer) error {
			return writeDirectStream(ctx, NewWriter(r), dst, func(w *Writer) error {
				_, err := w.RemediatePDFA(ctx, models.PDFA2b)
				return err
			})
		}},
		{name: "split", pages: 1, write: func(ctx context.Context, r, _ *Reader, dst *bytes.Buffer) error {
			return Split(ctx, r, []int{1}, dst)
		}},
		{name: "merge", pages: 2, write: func(ctx context.Context, r, r2 *Reader, dst *bytes.Buffer) error {
			return Merge(ctx, []*Reader{r, r2}, dst)
		}},
		{name: "insert", pages: 2, write: func(ctx context.Context, r, r2 *Reader, dst *bytes.Buffer) error {
			return writeDirectStream(ctx, NewWriter(r), dst, func(w *Writer) error { return w.InsertPages(0, r2, 1) })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data := directStreamPDF()
			open := func() *Reader {
				r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				t.Cleanup(func() {
					if err := r.Close(); err != nil {
						t.Errorf("Close: %v", err)
					}
				})
				return r
			}
			var out bytes.Buffer
			if err := tc.write(ctx, open(), open(), &out); err != nil {
				t.Fatalf("write: %v", err)
			}
			if n := streamsInsideObjects(t, out.Bytes(), tc.password); n != 0 {
				t.Errorf("the output holds %d streams inside another object", n)
			}
			assertDirectStreamText(t, out.Bytes(), tc.password, tc.pages)
		})
	}
}

// writeDirectStream queues edit on w, when there is one, writes w to dst and closes it.
func writeDirectStream(ctx context.Context, w *Writer, dst *bytes.Buffer, edit func(*Writer) error) (err error) {
	defer func() {
		if closeErr := w.Close(); err == nil {
			err = closeErr
		}
	}()
	if edit != nil {
		if err := edit(w); err != nil {
			return err
		}
	}
	return w.WriteTo(ctx, dst)
}
