package html

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// redPNG encodes a w by h opaque red PNG.
func redPNG(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 0xff, A: 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestVideo_TakesItsBox checks the replaced box of a video element (HTML
// Standard, 4.8.8 and 15.4.1): the natural size of its poster when the
// poster loads, else no natural size, which the layout sizes to the
// default object size, 300 by 150 CSS pixels; and that only what litho
// leaves undrawn is reported: the controls, and the frames of a video
// whose resource is named and that shows no poster.
func TestVideo_TakesItsBox(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"p.png": {Data: redPNG(t, 20, 10)}}
	for _, tc := range []struct {
		src     string
		natural bool
		w, h    float64
		warns   []string
	}{
		{`<video id=v poster=p.png></video>`, true, 20, 10, nil},
		{`<video id=v poster=p.png src=a.mp4><source src=b.webm>fallback</video>`, true, 20, 10, nil},
		{`<video id=v></video>`, false, 300, 150, nil},
		{`<video id=v poster=""></video>`, false, 300, 150, nil},
		{`<video id=v poster=p.png controls></video>`, true, 20, 10, []string{"controls"}},
		{`<video id=v src=a.mp4></video>`, false, 300, 150, []string{"frames"}},
		{`<video id=v><source src=a.mp4></video>`, false, 300, 150, []string{"frames"}},
		{`<video id=v poster=gone.png controls></video>`, false, 300, 150, []string{"gone.png", "controls"}},
	} {
		p := prep(t, tc.src+`<p>x`, Options{Resources: files})
		v := byID(t, p.root, "v")
		if v.Kind != layout.Replaced || v.Picture == nil {
			t.Errorf("%s: a %v node without a picture", tc.src, v.Kind)
			continue
		}
		pic := v.Picture
		if pic.Width != tc.w*css.Pt || pic.Height != tc.h*css.Pt || pic.NoNaturalWidth == tc.natural || pic.NoRatio == tc.natural {
			t.Errorf("%s: picture %v by %v, natural %v, want %v by %v px, natural %v", tc.src, pic.Width, pic.Height, !pic.NoNaturalWidth, tc.w, tc.h, tc.natural)
		}
		if treeText(p.root) != "x" {
			t.Errorf("%s: drew the text %q", tc.src, treeText(p.root))
		}
		var got []string
		for _, w := range p.warnings {
			if w.Code != warn.LimitReached {
				got = append(got, w.Message)
			}
		}
		if len(got) != len(tc.warns) {
			t.Errorf("%s: warnings %q, want ones about %q", tc.src, got, tc.warns)
			continue
		}
		for i, about := range tc.warns {
			if !strings.Contains(got[i], about) {
				t.Errorf("%s: warning %q, want one about %q", tc.src, got[i], about)
			}
		}
	}
}

// TestDraw_VideoPoster draws a video's poster fitted in its box as
// object-fit contain fits it, the user agent sheet's value (HTML Standard,
// 15.4.1: the largest size that keeps its ratio, centred), and filled
// under object-fit fill.
func TestDraw_VideoPoster(t *testing.T) {
	t.Parallel()

	o := Options{Resources: fstest.MapFS{"p.png": {Data: redPNG(t, 2, 2)}}}
	for _, tc := range []struct {
		style string
		want  string
	}{
		{"", "__RRRR__|__RRRR__|__RRRR__|__RRRR__"},
		{"object-fit: fill", "RRRRRRRR|RRRRRRRR|RRRRRRRR|RRRRRRRR"},
	} {
		src := `<body style="margin:0"><video poster=p.png style="display:block;width:8px;height:4px;` + tc.style + `"></video>`
		if got := strings.Join(drawPage(t, src, 8, 4, o), "|"); got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.style, got, tc.want)
		}
	}
}
