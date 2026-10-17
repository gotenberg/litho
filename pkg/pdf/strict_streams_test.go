package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// salvagedPDF returns a one-page document whose content stream is FlateDecode
// data with a wrong zlib checksum, and whose image /Im0, a 2x1 grey image, is
// FlateDecode data without its checksum. Both decode whole, salvaged. The
// image /Im1 is sound.
func salvagedPDF(t *testing.T) []byte {
	t.Helper()
	deflate := func(plain []byte) []byte {
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		if _, err := w.Write(plain); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	content := deflate([]byte("BT /F1 12 Tf 72 700 Td (Salvaged text) Tj ET q 2 0 0 1 0 0 cm /Im0 Do /Im1 Do Q"))
	content[len(content)-1] ^= 0xFF
	noSum := deflate([]byte{10, 200})
	noSum = noSum[:len(noSum)-4]
	image := func(data []byte) string {
		return streamObj("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode", data)
	}
	return limitsPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
			" /Resources << /Font << /F1 5 0 R >> /XObject << /Im0 6 0 R /Im1 7 0 R >> >> >>",
		streamObj("/Filter /FlateDecode", content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		image(noSum),
		image(deflate([]byte{10, 200})),
	)
}

// TestSalvagedStreams_ListsWithoutChangingReads pins that a Reader opened by
// default reads salvaged streams as it did before, text and image alike, and
// that [Reader.SalvagedStreams] lists them, and only them.
func TestSalvagedStreams_ListsWithoutChangingReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := salvagedPDF(t)
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	text, err := r.ExtractText(ctx, 1)
	if err != nil || !strings.Contains(text, "Salvaged text") {
		t.Errorf("ExtractText = %q, %v; want the salvaged text", text, err)
	}
	for _, name := range []string{"Im0", "Im1"} {
		if _, err := r.DecodeImage(ctx, 1, name); err != nil {
			t.Errorf("DecodeImage(%s) = %v", name, err)
		}
	}
	got, err := r.SalvagedStreams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []models.StreamDiagnostic{
		{ObjectNum: 4, Filters: []string{"FlateDecode"}},
		{ObjectNum: 6, Filters: []string{"FlateDecode"}},
	}
	if !slices.EqualFunc(got, want, func(a, b models.StreamDiagnostic) bool {
		return a.ObjectNum == b.ObjectNum && slices.Equal(a.Filters, b.Filters)
	}) {
		t.Errorf("SalvagedStreams = %+v, want %+v", got, want)
	}
}

// TestWithStrictStreams_RefusesSalvagedStreams pins that a Reader opened with
// [WithStrictStreams] refuses each salvaged stream with [ErrDamagedStream],
// and still reads the sound image.
func TestWithStrictStreams_RefusesSalvagedStreams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := salvagedPDF(t)
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithStrictStreams())
	if err != nil {
		t.Fatal(err)
	}
	if text, err := r.ExtractText(ctx, 1); !errors.Is(err, ErrDamagedStream) {
		t.Errorf("ExtractText = %q, %v; want ErrDamagedStream", text, err)
	}
	if _, err := r.DecodeImage(ctx, 1, "Im0"); !errors.Is(err, ErrDamagedStream) {
		t.Errorf("DecodeImage(Im0) = %v, want ErrDamagedStream", err)
	}
	if _, err := r.DecodeImage(ctx, 1, "Im1"); err != nil {
		t.Errorf("DecodeImage(Im1) = %v", err)
	}
	if got, err := r.SalvagedStreams(ctx); err != nil || len(got) != 2 {
		t.Errorf("SalvagedStreams = %+v, %v; want both streams listed", got, err)
	}
}

// TestWithStrictStreams_WholeDocumentReads pins that a document without
// salvaged data reads the same under [WithStrictStreams].
func TestWithStrictStreams_WholeDocumentReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := imagePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithStrictStreams())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.DecodeImage(ctx, 1, "Im0"); err != nil {
		t.Errorf("DecodeImage = %v", err)
	}
	if got, err := r.SalvagedStreams(ctx); err != nil || len(got) != 0 {
		t.Errorf("SalvagedStreams = %v, %v; want none", got, err)
	}
}
