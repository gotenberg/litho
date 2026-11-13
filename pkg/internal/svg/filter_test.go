package svg

import (
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// images returns the images d draws on a w by h pixel viewport, each as
// its pixel size and the start of the SHA-256 of its samples.
func images(t *testing.T, d *Document, w, h float64) []string {
	t.Helper()
	var out []string
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Image:
				img, err := v.Source.Decode(t.Context())
				if err != nil {
					t.Fatalf("Decode: %v", err)
				}
				n, ok := img.(*image.NRGBA)
				if !ok {
					t.Fatalf("image %T, want NRGBA", img)
				}
				sum := sha256.Sum256(n.Pix)
				out = append(out, fmt.Sprintf("%dx%d %x", n.Rect.Dx(), n.Rect.Dy(), sum[:8]))
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(d.Draw(context.Background(), typeset.Rect{Width: w * ptPerPx, Height: h * ptPerPx}))
	return out
}

func TestFilter_BlurDrawsAnImageOverTheRegion(t *testing.T) {
	img := draw(t, `<svg width="60" height="60"><filter id="f"><feGaussianBlur stdDeviation="3"/></filter>
		<rect x="20" y="20" width="20" height="20" fill="blue" filter="url(#f)"/></svg>`, 60, 60)
	if c := at(img, 30, 30); !near(c, [3]uint8{0, 0, 255}, 8) {
		t.Errorf("inside the square %v, want blue", c)
	}
	if c := at(img, 18, 30); c[2] != 255 || c[0] > 200 || c[0] < 40 {
		t.Errorf("2px out of the square %v, want a blurred blue", c)
	}
	if c := at(img, 5, 30); c != [3]uint8{255, 255, 255} {
		t.Errorf("past the filter region %v, want white", c)
	}
}

func TestFilter_NamingNothingHidesTheElement(t *testing.T) {
	img := draw(t, `<svg width="20" height="20"><rect width="20" height="20" fill="red" filter="url(#none)"/></svg>`, 20, 20)
	if c := at(img, 10, 10); c != [3]uint8{255, 255, 255} {
		t.Errorf("element of a missing filter %v, want nothing drawn", c)
	}
}

func TestFilter_FloodsTheRegionOfAnEmptyGroup(t *testing.T) {
	img := draw(t, `<svg width="20" height="20"><filter id="f" filterUnits="userSpaceOnUse" x="5" y="5" width="10" height="10">
		<feFlood flood-color="lime"/></filter><g filter="url(#f)"/></svg>`, 20, 20)
	if c := at(img, 10, 10); c != [3]uint8{0, 255, 0} {
		t.Errorf("inside the region %v, want lime", c)
	}
	if c := at(img, 2, 2); c != [3]uint8{255, 255, 255} {
		t.Errorf("outside the region %v, want white", c)
	}
}

func TestFilter_ColorInterpolation(t *testing.T) {
	// A slope of 0.5 halves grey 128 in sRGB to 64; in linearRGB, the
	// default (Filter Effects 1, 17), 128 is 55, halved 28, back in sRGB
	// 93.
	const src = `<svg width="20" height="20"><filter id="f" %s><feComponentTransfer>
		<feFuncR type="linear" slope="0.5"/><feFuncG type="linear" slope="0.5"/><feFuncB type="linear" slope="0.5"/>
		</feComponentTransfer></filter><rect width="20" height="20" fill="rgb(128,128,128)" filter="url(#f)"/></svg>`
	if c := at(draw(t, fmt.Sprintf(src, `color-interpolation-filters="sRGB"`), 20, 20), 10, 10); !near(c, [3]uint8{64, 64, 64}, 1) {
		t.Errorf("in sRGB %v, want 64", c)
	}
	if c := at(draw(t, fmt.Sprintf(src, ""), 20, 20), 10, 10); !near(c, [3]uint8{93, 93, 93}, 2) {
		t.Errorf("in linearRGB %v, want 93", c)
	}
}

func TestFilter_DropShadowOfAnyNameCase(t *testing.T) {
	// The HTML parser lowercases feDropShadow, an SVG 2 element its table
	// does not list.
	img := draw(t, `<svg width="40" height="40"><filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="40" height="40">
		<feDropShadow dx="10" dy="10" stdDeviation="0" flood-color="red"/></filter>
		<rect x="5" y="5" width="15" height="15" fill="blue" filter="url(#f)"/></svg>`, 40, 40)
	if c := at(img, 10, 10); !near(c, [3]uint8{0, 0, 255}, 8) {
		t.Errorf("the source %v, want blue", c)
	}
	if c := at(img, 27, 27); !near(c, [3]uint8{255, 0, 0}, 8) {
		t.Errorf("the shadow %v, want red", c)
	}
}

func TestFilter_LeftOutPrimitiveDrawsUnfiltered(t *testing.T) {
	d := parse(t, `<svg width="20" height="20"><filter id="f"><feTurbulence baseFrequency="0.1"/></filter>
		<rect width="20" height="20" fill="red" filter="url(#f)"/></svg>`)
	if c := at(drawDoc(t, d, 20, 20), 10, 10); c != [3]uint8{255, 0, 0} {
		t.Errorf("the element %v, want red, unfiltered", c)
	}
	found := false
	for _, w := range d.Warnings() {
		found = found || strings.Contains(w.Message, "feTurbulence")
	}
	if !found {
		t.Errorf("warnings %+v, want the left-out primitive named", d.Warnings())
	}
}

func TestFilter_SameBytesOnEveryArchitecture(t *testing.T) {
	d := parse(t, `<svg width="80" height="60"><filter id="f" x="-20%" y="-20%" width="160%" height="160%">
		<feGaussianBlur in="SourceAlpha" stdDeviation="2 1.5" result="b"/>
		<feOffset in="b" dx="3" dy="2" result="o"/>
		<feFlood flood-color="teal" flood-opacity="0.6"/>
		<feComposite in2="o" operator="in" result="s"/>
		<feColorMatrix in="SourceGraphic" type="hueRotate" values="40" result="h"/>
		<feMorphology in="h" operator="dilate" radius="1"/>
		<feBlend in2="s" mode="multiply"/>
		</filter><circle cx="40" cy="30" r="18" fill="orange" stroke="navy" stroke-width="3" filter="url(#f)"/></svg>`)
	got := images(t, d, 80, 60)
	want := []string{"173x173 c5c2d3eda38c26c8"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("filter images:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
