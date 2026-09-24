package image

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// unreadableBase is a base colour space whose family litho assigns no component
// count. It has to be an ARRAY: a bare name reaches resolveColorSpace's PdfName
// case and comes back verbatim, which imageComponents already rejects by name.
// Only an array family it does not recognise falls through the Indexed branch to
// its DeviceRGB default, which is the substitution under test.
const unreadableBase = "[/Bogus << >>]"

// TestDecodeImage_IndexedOverUnreadableBaseIsRefused pins that an indexed image
// litho cannot build a palette for is refused as such.
//
// It was not. resolveIndexedPalette returned one flag for two questions, "is the
// space indexed" and "did its colour table resolve", so a base with no known
// component count turned an indexed image into a direct one. resolveColorSpace
// then collapsed /Indexed to that base and defaulted it to DeviceRGB, and the
// decoder demanded three bytes for every pixel that holds one index. Complete
// data came back as "decoded data too short for RGB image: got 1560, want 4680",
// blaming the file for a colour space litho does not support.
//
// # Corpus evidence
//
//   - pdfjs issue10339_reduced.pdf, issue2761.pdf: 4 images in these 2
//     documents were refused this way, every one of them /Indexed over a /Lab
//     base.
func TestDecodeImage_IndexedOverUnreadableBaseIsRefused(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t,
		indexedImagePDF(unreadableBase, "<FF0000 00FF00 0000FF FFFFFF>", []byte{0, 1, 2, 3}))

	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil {
		t.Fatal("DecodeImage accepted an indexed image whose colour table cannot be read")
	}

	// The message has to name the cause. "too short" is the old wording and is
	// the specific lie being fixed: the data is complete, and asserting on the
	// message rather than on err != nil is what keeps the fix from regressing
	// into a differently-worded length complaint.
	if strings.Contains(err.Error(), "too short") {
		t.Errorf("refusal still blames the data length: %v", err)
	}
	if !strings.Contains(err.Error(), "Bogus") {
		t.Errorf("refusal does not name the base colour space: %v", err)
	}
}

// TestAppliesDecodeArray_IndexedOverUnreadableBase pins the same distinction in
// the exported predicate.
//
// AppliesDecodeArray documents that only an indexed image is excluded, but it
// asked resolveIndexedPalette, so an indexed image with an unreadable table
// answered "the /Decode array was applied" for pixels DecodeImage never returns.
// mutations.blankImageRegion reads this to decide whether dropping /Decode on
// re-encode is safe.
func TestAppliesDecodeArray_IndexedOverUnreadableBase(t *testing.T) {
	t.Parallel()

	ctx, doc, _ := imagePDF(t, indexedImagePDF(unreadableBase, "<FF0000>", []byte{0}))

	dict := ast.PdfDict{
		"ColorSpace": ast.PdfArray{
			ast.PdfName("Indexed"),
			ast.PdfArray{ast.PdfName("Bogus"), ast.PdfDict{}},
			ast.PdfInt(3),
			ast.PdfString("\xff\x00\x00"),
		},
	}
	if AppliesDecodeArray(ctx, doc, dict) {
		t.Error("AppliesDecodeArray says the array was folded into an indexed image litho refuses to decode")
	}
}
