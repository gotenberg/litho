package page

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestImageTakesStateIntent checks ISO 32000-1, 8.6.5.8: an image with no
// /Intent is converted with the graphics state's rendering intent, set by ri
// or an ExtGState /RI, and an image's own /Intent overrides it. The profile's
// black is lifted, so the compensated intents draw it black and absolute
// colorimetric, which keeps the medium's black, draws it dark grey.
func TestImageTakesStateIntent(t *testing.T) {
	t.Parallel()
	profile := stream("/N 1", string(liftedGreyProfile()))
	image := func(own string) string {
		return stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 "+
			"/ColorSpace [/ICCBased 5 0 R]"+own, "\x00")
	}
	for _, tc := range []struct {
		name, content, own string
		grey               bool
	}{
		{"no ri", "", "", false},
		{"ri perceptual", "/Perceptual ri", "", false},
		{"ri absolute", "/AbsoluteColorimetric ri", "", true},
		{"ExtGState /RI absolute", "/Abs gs", "", true},
		{"own /Intent over ri", "/AbsoluteColorimetric ri", " /Intent /Perceptual", false},
		{"own absolute under ri perceptual", "/Perceptual ri", " /Intent /AbsoluteColorimetric", true},
		{"unrecognised own /Intent", "/AbsoluteColorimetric ri", " /Intent /Bogus", false},
		{"null own /Intent", "/AbsoluteColorimetric ri", " /Intent null", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			img, _ := draw(t, fixture{
				page: "/Resources << /XObject << /Im 6 0 R >> " +
					"/ExtGState << /Abs << /RI /AbsoluteColorimetric >> >> >>",
				content: "q " + tc.content + " q 30 0 0 30 10 60 cm /Im Do Q Q q 30 0 0 30 10 10 cm /Im Do Q",
				objs:    []string{profile, image(tc.own)},
			})
			got, after := at(t, img, 25, 25), at(t, img, 25, 75)
			// The same image follows Q, where an intent of its own still
			// applies.
			if tc.own == "" && !close8(after, black, 1) {
				t.Errorf("image after Q = %v, want compensated black", after)
			}
			if !tc.grey {
				if !close8(got, black, 1) {
					t.Errorf("image = %v, want compensated black", got)
				}
				return
			}
			if got.r < 80 || got.r != got.g || got.g != got.b {
				t.Errorf("image = %v, want the profile's own dark grey", got)
			}
		})
	}
}

// TestDefaultBlackPointKeepsInitialImageIntent checks that
// [Options.DefaultBlackPoint] reads an image with no /Intent under the
// initial intent, which compensates, whatever ri sets.
func TestDefaultBlackPointKeepsInitialImageIntent(t *testing.T) {
	t.Parallel()
	f := fixture{
		page:    "/Resources << /XObject << /Im 6 0 R >> >>",
		content: "/AbsoluteColorimetric ri q 30 0 0 30 10 60 cm /Im Do Q",
		objs: []string{stream("/N 1", string(liftedGreyProfile())), stream("/Type /XObject /Subtype /Image /Width 1 "+
			"/Height 1 /BitsPerComponent 8 /ColorSpace [/ICCBased 5 0 R]", "\x00")},
	}
	doc, pg := open(t, f.bytes(), limits.Limits{})
	opts := f.options()
	opts.DefaultBlackPoint = true
	c, _, err := Render(context.Background(), doc, pg, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	img := c.Image()
	if got := at(t, img, 25, 25); !close8(got, black, 1) {
		t.Errorf("image = %v, want compensated black", got)
	}
}

// TestInlineImageTakesStateIntent checks that an inline image, which has no
// /Intent of its own, takes the graphics state's rendering intent as an image
// XObject does (ISO 32000-1, 8.6.5.8 and 8.9.7).
func TestInlineImageTakesStateIntent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ri   string
		grey bool
	}{{"/Perceptual ri", false}, {"/AbsoluteColorimetric ri", true}} {
		t.Run(tc.ri, func(t *testing.T) {
			t.Parallel()
			img, _ := draw(t, fixture{
				page:    "/Resources << /ColorSpace << /CS0 [/ICCBased 5 0 R] >> >>",
				content: "q " + tc.ri + " 30 0 0 30 10 60 cm BI /W 1 /H 1 /BPC 8 /CS /CS0 ID \x00 EI Q",
				objs:    []string{stream("/N 1", string(liftedGreyProfile()))},
			})
			got := at(t, img, 25, 25)
			if grey := got.r >= 80 && got.r == got.g && got.g == got.b; grey != tc.grey || (!grey && !close8(got, black, 1)) {
				t.Errorf("inline image = %v, want grey %v", got, tc.grey)
			}
		})
	}
}
