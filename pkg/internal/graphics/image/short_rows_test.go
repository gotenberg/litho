package image

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeImage_ShortOfOneRowReportsTheStreamLength pins the refusal of data
// that holds no whole row. It once padded the data to the whole image before
// refusing, so the message reported the padded length, 192 bytes for this 8x8
// RGB image, instead of the 5 the stream holds.
func TestDecodeImage_ShortOfOneRowReportsTheStreamLength(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, filteredImagePDF("/ColorSpace /DeviceRGB /BitsPerComponent 8", []byte("short")))
	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil {
		t.Fatal("DecodeImage accepted data short of one row")
	}
	if !strings.Contains(err.Error(), "got 5 bytes") {
		t.Errorf("refusal does not report the stream's 5 bytes: %v", err)
	}
}

// TestPadShortSamples_NoRowIsNotPadded pins that data short of one row comes
// back as it is, so the refusal allocates nothing, while data holding a row is
// still padded to the whole image.
func TestPadShortSamples_NoRowIsNotPadded(t *testing.T) {
	t.Parallel()

	data := []byte{1, 2, 3}
	got, rows := padShortSamples(data, 8, 8, 3, 8)
	if rows != 0 || len(got) != len(data) {
		t.Errorf("short of a row: %d bytes, %d rows; want %d bytes, 0 rows", len(got), rows, len(data))
	}

	got, rows = padShortSamples(make([]byte, 30), 8, 8, 3, 8)
	if rows != 1 || len(got) != 8*8*3 {
		t.Errorf("one row: %d bytes, %d rows; want %d bytes, 1 row", len(got), rows, 8*8*3)
	}
}
