package image

import (
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeImage_JBIG2WithGlobals decodes the JBIG2Decode example of ISO
// 32000-1, 7.4.7 as the image XObject the standard lays out: a DeviceGray
// 1-bit image whose DecodeParms name a JBIG2Globals stream by reference.
//
// Every layer has to hold for the picture to come out right: the document
// resolving the reference inside DecodeParms, the filter decoding the page
// against the global symbol dictionary and inverting it, and the 1-bit path
// reading 0 as black. The expected picture is the jbig2dec 0.20 decode of the
// same stream, with its foreground pixels black.
func TestDecodeImage_JBIG2WithGlobals(t *testing.T) {
	t.Parallel()

	page, errP := os.ReadFile("../../filter/jbig2/testdata/iso32000_jbig2_page.jb2")
	globals, errG := os.ReadFile("../../filter/jbig2/testdata/iso32000_jbig2_globals.jb2")
	golden, errW := os.ReadFile("../../filter/jbig2/testdata/iso32000_jbig2.pbm")
	if errP != nil || errG != nil || errW != nil {
		t.Fatalf("reading the example: %v %v %v", errP, errG, errW)
	}
	golden = golden[len("P4\n52 66\n"):]

	ctx, doc, pg := imagePDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 52 66] /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 52 /Height 66 /ColorSpace /DeviceGray"+
			" /BitsPerComponent 1 /Filter /JBIG2Decode /DecodeParms << /JBIG2Globals 5 0 R >>"+
			" /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(globals), globals),
	})

	img, err := DecodeImage(ctx, doc, pg, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 52 || b.Dy() != 66 {
		t.Fatalf("bounds = %v, want 52x66", b)
	}
	wrong := 0
	for y := range 66 {
		for x := range 52 {
			foreground := golden[y*7+x/8]>>(7-x%8)&1 == 1
			r, g, b, _ := img.At(x, y).RGBA()
			black := r>>8 < 128 && g>>8 < 128 && b>>8 < 128
			if foreground != black {
				wrong++
			}
		}
	}
	if wrong != 0 {
		t.Errorf("%d of %d pixels disagree with the foreground being black", wrong, 52*66)
	}
}
