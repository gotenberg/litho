package pdf

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
)

// TestConvert_DerivedAltText holds the contract of WithDerivedAltText: off,
// the default, writes the bytes a conversion without the option writes and
// leaves an undescribed picture without /Alt; on, the picture takes the
// caption of the figure it alone fills, and the page draws the same.
func TestConvert_DerivedAltText(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	src := `<figure><img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(buf.Bytes()) + `" width=40 height=40>` +
		`<figcaption>Quarterly sales</figcaption></figure>`
	convert := func(opts ...HTMLOption) []byte {
		t.Helper()
		var out bytes.Buffer
		// PDF 1.4 writes no object stream: the structure's text reads in
		// the file's bytes.
		all := append([]HTMLOption{WithConvertFontDirs(convertFontDir(t)), WithConvertVersion("1.4")}, opts...)
		if _, err := FromHTML(strings.NewReader(src), all...).WriteTo(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	plain, off, on := convert(), convert(WithDerivedAltText(false)), convert(WithDerivedAltText(true))
	if !bytes.Equal(plain, off) {
		t.Error("WithDerivedAltText(false) writes other bytes than no option")
	}
	if got := bytes.Count(plain, []byte("/Alt (Quarterly sales)")); got != 1 {
		t.Errorf("without the option %d /Alt entries name the caption, want the figure's alone", got)
	}
	if got := bytes.Count(on, []byte("/Alt (Quarterly sales)")); got != 2 {
		t.Errorf("with the option %d /Alt entries name the caption, want the figure's and the picture's", got)
	}
	render := func(data []byte) image.Image {
		t.Helper()
		r, err := Open(t.Context(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		img, err := r.RenderPage(t.Context(), 1, WithRenderDPI(36))
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	a, b := render(plain), render(on)
	if a.Bounds() != b.Bounds() {
		t.Fatalf("pages of %v and %v", a.Bounds(), b.Bounds())
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				t.Fatalf("the option draws pixel (%d, %d) otherwise", x, y)
			}
		}
	}
}
