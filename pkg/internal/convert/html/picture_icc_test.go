package html

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/internal/imagetest"
)

// TestResources_KeepPictureProfiles checks that a PNG's iCCP profile and
// a JPEG's APP2 profile reach the picture as its ICCBased colour space,
// as the image file import keeps them, and that a version
// 4 profile is left out of a PDF 1.4 file (ISO 32000-1, Table 67).
func TestResources_KeepPictureProfiles(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	v4 := imagetest.ICCProfile("RGB ", 4)
	pngICC := imagetest.WithPNGChunks(buf.Bytes(), imagetest.PNGChunk{Type: "iCCP", Data: imagetest.ICCP(v4)})
	ycc := image.NewYCbCr(image.Rect(0, 0, 8, 8), image.YCbCrSubsampleRatio444)
	jpegICC := imagetest.JPEG(ycc, imagetest.JPEGOptions{Components: 3, ICC: v4})
	for _, tc := range []struct {
		name  string
		data  []byte
		pdf14 bool
		kept  bool
	}{
		{"png", pngICC, false, true},
		{"png at PDF 1.4", pngICC, true, false},
		{"jpeg", jpegICC, false, true},
		{"jpeg at PDF 1.4", jpegICC, true, false},
	} {
		r := newResources(fstest.MapFS{"p": {Data: tc.data}})
		r.pdf14 = tc.pdf14
		pic, err := r.picture(context.Background(), rootPrefix+"p")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var icc []byte
		if l := pic.Source.Layout; l != nil {
			icc = l.ICC
		}
		if kept := bytes.Equal(icc, v4); kept != tc.kept {
			t.Errorf("%s: profile kept %t, want %t", tc.name, kept, tc.kept)
		}
	}
}
