package compliance

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// zeroReader yields zero bytes without end, standing in for the body of a
// large attachment without allocating it.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// failingReader returns data, then err.
type failingReader struct {
	data []byte
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

// TestReadEmbeddedFile_HeaderDecides pins that an embedded file is judged a
// PDF or not from its header alone, and that only a PDF is read past it.
//
// The limit here stands in for maxEmbeddedFileSize, and each body is a lazy
// stream far longer than it, so a read of the whole file would never end.
func TestReadEmbeddedFile_HeaderDecides(t *testing.T) {
	t.Parallel()

	const limit = 8 << 10
	logger := slog.New(slog.DiscardHandler)
	lazy := func(prefix string) *countingReader {
		return &countingReader{r: io.MultiReader(strings.NewReader(prefix), zeroReader{})}
	}

	t.Run("large file that is not a PDF", func(t *testing.T) {
		t.Parallel()
		r := lazy("GIF89a")
		data, isPDF, tooLarge, err := readEmbeddedFile(context.Background(), r, limit, logger)
		if err != nil || isPDF || tooLarge || data != nil {
			t.Fatalf("readEmbeddedFile = (%d bytes, isPDF %v, tooLarge %v, %v), want (0, false, false, nil)",
				len(data), isPDF, tooLarge, err)
		}
		if r.n > embeddedHeaderScanLen {
			t.Errorf("read %d bytes to judge the header, want at most %d", r.n, embeddedHeaderScanLen)
		}
	})

	t.Run("large PDF", func(t *testing.T) {
		t.Parallel()
		r := lazy("%PDF-1.7\n")
		data, isPDF, tooLarge, err := readEmbeddedFile(context.Background(), r, limit, logger)
		if err != nil || !isPDF || !tooLarge || data != nil {
			t.Fatalf("readEmbeddedFile = (%d bytes, isPDF %v, tooLarge %v, %v), want (0, true, true, nil)",
				len(data), isPDF, tooLarge, err)
		}
		if r.n != limit+1 {
			t.Errorf("read %d bytes, want limit+1 = %d", r.n, limit+1)
		}
	})

	t.Run("PDF within the limit", func(t *testing.T) {
		t.Parallel()
		body := "%PDF-1.7\n" + strings.Repeat("x", limit-len("%PDF-1.7\n"))
		data, isPDF, tooLarge, err := readEmbeddedFile(context.Background(), strings.NewReader(body), limit, logger)
		if err != nil || !isPDF || tooLarge || string(data) != body {
			t.Fatalf("readEmbeddedFile = (%d bytes, isPDF %v, tooLarge %v, %v), want the whole %d-byte PDF",
				len(data), isPDF, tooLarge, err, len(body))
		}
	})

	t.Run("header past the scan window", func(t *testing.T) {
		t.Parallel()
		r := lazy(strings.Repeat(" ", embeddedHeaderScanLen) + "%PDF-1.7\n")
		_, isPDF, _, err := readEmbeddedFile(context.Background(), r, limit, logger)
		if err != nil || isPDF {
			t.Fatalf("readEmbeddedFile = (isPDF %v, %v), want a header past %d bytes not to count",
				isPDF, err, embeddedHeaderScanLen)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		t.Parallel()
		_, isPDF, _, err := readEmbeddedFile(context.Background(), strings.NewReader(""), limit, logger)
		if err != nil || isPDF {
			t.Fatalf("readEmbeddedFile = (isPDF %v, %v), want an empty file judged not a PDF", isPDF, err)
		}
	})

	t.Run("damaged PDF keeps what decoded", func(t *testing.T) {
		t.Parallel()
		body := "%PDF-1.7\n" + strings.Repeat("x", 2*embeddedHeaderScanLen)
		r := &failingReader{data: []byte(body), err: errors.New("corrupt deflate data")}
		data, isPDF, tooLarge, err := readEmbeddedFile(context.Background(), r, limit, logger)
		if err != nil || !isPDF || tooLarge || string(data) != body {
			t.Fatalf("readEmbeddedFile = (%d bytes, isPDF %v, tooLarge %v, %v), want the %d bytes before the error",
				len(data), isPDF, tooLarge, err, len(body))
		}
	})

	t.Run("stream that fails before any byte", func(t *testing.T) {
		t.Parallel()
		readErr := errors.New("corrupt deflate data")
		_, _, _, err := readEmbeddedFile(context.Background(), &failingReader{err: readErr}, limit, logger)
		if !errors.Is(err, readErr) {
			t.Fatalf("readEmbeddedFile error = %v, want %v", err, readErr)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, _, err := readEmbeddedFile(ctx, lazy("%PDF-1.7\n"), limit, logger)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("readEmbeddedFile error = %v, want context.Canceled", err)
		}
	})
}

// TestEmbeddedFileCompliance_LargeNonPDFStillFails pins ISO 19005-2, 6.8 on
// an attachment past maxEmbeddedFileSize. The whole file was decoded before
// its size was checked, and a file over the cap was skipped with no
// violation, so a large attachment that is not a PDF escaped the clause.
//
// The attachment is a FlateDecode stream of a GIF header and zeros that
// decodes past the cap. The check reads only its header, so the test never
// holds the decoded file.
func TestEmbeddedFileCompliance_LargeNonPDFStillFails(t *testing.T) {
	if testing.Short() {
		t.Skip("compresses 50 MB of zeros")
	}
	t.Parallel()

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := io.WriteString(w, "GIF89a"); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if _, err := io.CopyN(w, zeroReader{}, maxEmbeddedFileSize); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 5 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Names [(a.gif) 6 0 R] >>",
		"<< /Type /Filespec /F (a.gif) /UF (a.gif) /EF << /F 7 0 R >> >>",
		fmt.Sprintf("<< /Type /EmbeddedFile /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream",
			compressed.Len(), compressed.Bytes()),
	)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	for _, e := range viols {
		var v Violation
		if errors.As(e, &v) && v.Rule == Rule_19005_2_6_8_t05 {
			if !strings.Contains(v.Context, "not a PDF document") {
				t.Errorf("6.8 t5 fired for another reason: %s", v.Context)
			}
			return
		}
	}
	t.Errorf("no 6.8 t5 violation for a %d-byte attachment that is not a PDF", maxEmbeddedFileSize+len("GIF89a"))
}
