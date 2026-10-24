package pdf

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// cmykRenderPDF is a 100 by 20 page of five 20-unit squares, left to right: a
// DeviceCMYK fill, a DeviceGray black, a DeviceRGB red, a Separation over
// DeviceCMYK at full tint, and a DeviceCMYK black at constant alpha 0.5.
func cmykRenderPDF() []byte {
	content := "0.2 0.4 0.6 0.8 k 0 0 20 20 re f " +
		"0 g 20 0 20 20 re f " +
		"1 0 0 rg 40 0 20 20 re f " +
		"/Spot cs 1 scn 60 0 20 20 re f " +
		"/Half gs 0 0 0 1 k 80 0 20 20 re f"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 20] /Contents 4 0 R " +
			"/Resources << /ColorSpace << /Spot [/Separation /Gold /DeviceCMYK << /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0.3 0.9 0.1] /N 1 >>] >> " +
			"/ExtGState << /Half << /ca 0.5 >> >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

// cmykAt returns pixel (x, y) of a CMYK render.
func cmykAt(t *testing.T, img image.Image, x, y int) color.CMYK {
	t.Helper()
	c, ok := img.(*image.CMYK)
	if !ok {
		t.Fatalf("RenderPage returned %T, want *image.CMYK", img)
	}
	return c.CMYKAt(x, y)
}

// inkBytes returns the 8-bit inks of the link from sRGB to the CMYK profile
// dst for the sRGB colour rgb, as the renderer rounds them: relative
// colorimetric with black point compensation, the renderer's CMYK links.
func inkBytes(t *testing.T, dst []byte, rgb []float64) color.CMYK {
	t.Helper()
	sp, err := icc.Parse(icc.SRGBProfile())
	if err != nil {
		t.Fatal(err)
	}
	dp, err := icc.Parse(dst)
	if err != nil {
		t.Fatal(err)
	}
	l, err := icc.NewLink(sp, dp, icc.TransformOptions{Intent: icc.RelativeColorimetric, BlackPointCompensation: true})
	if err != nil {
		t.Fatal(err)
	}
	v := make([]float64, l.OutputChannels())
	l.Convert(rgb, v)
	b := func(f float64) uint8 { return uint8(math.Round(math.Min(math.Max(f, 0), 1) * 255)) }
	return color.CMYK{C: b(v[0]), M: b(v[1]), Y: b(v[2]), K: b(v[3])}
}

// nearCMYK reports whether two inks differ by at most one level a channel.
func nearCMYK(a, b color.CMYK) bool {
	d := func(x, y uint8) bool { return x-y <= 1 || y-x <= 1 }
	return d(a.C, b.C) && d(a.M, b.M) && d(a.Y, b.Y) && d(a.K, b.K)
}

// TestRenderPageCMYK draws the device colours through unchanged, DeviceGray
// as black ink alone, a spot over DeviceCMYK as its tint transform's output,
// an RGB colour through the device profile, and constant alpha as half the
// ink over paper; the image is the size of the RGB render.
func TestRenderPageCMYK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openLimited(t, cmykRenderPDF(), Limits{})
	img, err := r.RenderPage(ctx, 1, WithRenderDPI(72), WithRenderColorModel(RenderCMYK))
	if err != nil {
		t.Fatal(err)
	}
	rgb, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != rgb.Bounds() {
		t.Errorf("CMYK render %v, RGB render %v", img.Bounds(), rgb.Bounds())
	}
	for _, tc := range []struct {
		name string
		x    int
		want color.CMYK
	}{
		{"DeviceCMYK", 10, color.CMYK{C: 51, M: 102, Y: 153, K: 204}},
		{"DeviceGray", 30, color.CMYK{K: 255}},
		{"DeviceRGB", 50, inkBytes(t, icc.CMYKPrinterProfile(), []float64{1, 0, 0})},
		{"Separation", 70, color.CMYK{M: 77, Y: 230, K: 26}},
		{"half alpha", 90, color.CMYK{K: 128}},
	} {
		if got := cmykAt(t, img, tc.x, 10); !nearCMYK(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestRenderPageCMYKBackground leaves unpainted paper without ink, over a
// transparent background as over the default white one.
func TestRenderPageCMYKBackground(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] >>",
	})
	r := openLimited(t, data, Limits{})
	for _, bg := range []color.Color{color.White, color.Transparent, nil} {
		img, err := r.RenderPage(ctx, 1, WithRenderDPI(72), WithRenderColorModel(RenderCMYK), WithRenderBackground(bg))
		if err != nil {
			t.Fatal(err)
		}
		if got := cmykAt(t, img, 5, 5); got != (color.CMYK{}) {
			t.Errorf("background %v: paper %v, want no ink", bg, got)
		}
	}
}

// TestRenderPageCMYKProfile converts an RGB colour into the profile
// WithRenderCMYKProfile gives, and still passes DeviceCMYK through.
func TestRenderPageCMYKProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profile, err := os.ReadFile(filepath.Join("..", "internal", "graphics", "icc", "testdata", "v4-cmyk-lut.icc"))
	if err != nil {
		t.Fatal(err)
	}
	r := openLimited(t, cmykRenderPDF(), Limits{})
	img, err := r.RenderPage(ctx, 1, WithRenderDPI(72), WithRenderColorModel(RenderCMYK), WithRenderCMYKProfile(profile))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cmykAt(t, img, 10, 10), (color.CMYK{C: 51, M: 102, Y: 153, K: 204}); got != want {
		t.Errorf("DeviceCMYK %v, want %v", got, want)
	}
	if got, want := cmykAt(t, img, 50, 10), inkBytes(t, profile, []float64{1, 0, 0}); !nearCMYK(got, want) {
		t.Errorf("DeviceRGB red %v, want %v", got, want)
	}
}

// TestRenderPageCMYKOutputIntent draws for the document's CMYK output intent
// by default: an RGB colour lands where the link into the intent's profile
// puts it.
func TestRenderPageCMYKOutputIntent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profile, err := os.ReadFile(filepath.Join("..", "internal", "graphics", "icc", "testdata", "v4-cmyk-lut.icc"))
	if err != nil {
		t.Fatal(err)
	}
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (x) /DestOutputProfile 5 0 R >>] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Contents 4 0 R >>",
		"<< /Length 22 >>\nstream\n1 0 0 rg 0 0 10 10 re f\nendstream",
		fmt.Sprintf("<< /N 4 /Length %d >>\nstream\n%s\nendstream", len(profile), profile),
	})
	r := openLimited(t, data, Limits{})
	img, err := r.RenderPage(ctx, 1, WithRenderDPI(72), WithRenderColorModel(RenderCMYK))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cmykAt(t, img, 5, 5), inkBytes(t, profile, []float64{1, 0, 0}); !nearCMYK(got, want) {
		t.Errorf("DeviceRGB red %v, want %v", got, want)
	}
}

// TestRenderPageCMYKRefusals refuses a colour model no constant names, a
// profile litho cannot read CMYK through, and a profile for an RGB render.
func TestRenderPageCMYKRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openLimited(t, cmykRenderPDF(), Limits{})
	for name, opts := range map[string][]RenderOption{
		"model 7":         {WithRenderColorModel(RenderColorModel(7))},
		"garbage profile": {WithRenderColorModel(RenderCMYK), WithRenderCMYKProfile([]byte("not a profile"))},
		"RGB profile":     {WithRenderColorModel(RenderCMYK), WithRenderCMYKProfile(icc.SRGBProfile())},
		"profile for RGB": {WithRenderCMYKProfile(icc.CMYKPrinterProfile())},
	} {
		if _, err := r.RenderPage(ctx, 1, opts...); !errors.Is(err, ErrInvalidRenderOptions) {
			t.Errorf("%s: %v, want ErrInvalidRenderOptions", name, err)
		}
	}
}

// TestRenderPageCMYKDeterministic draws a CMYK page twice, concurrently,
// byte for byte alike.
func TestRenderPageCMYKDeterministic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openLimited(t, cmykRenderPDF(), Limits{})
	imgs := make([]image.Image, 2)
	errs := make([]error, 2)
	done := make(chan int)
	for i := range imgs {
		go func() {
			imgs[i], errs[i] = r.RenderPage(ctx, 1, WithRenderDPI(50), WithRenderColorModel(RenderCMYK))
			done <- i
		}()
	}
	<-done
	<-done
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	a, b := imgs[0].(*image.CMYK), imgs[1].(*image.CMYK) //nolint:forcetypeassert // checked by the errors above.
	if string(a.Pix) != string(b.Pix) {
		t.Error("two CMYK renders of one page differ")
	}
}
