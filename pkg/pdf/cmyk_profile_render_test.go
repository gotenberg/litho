package pdf

import (
	"testing"
)

// white is the 8-bit RGB of a white pixel.
var white = [3]uint32{255, 255, 255}

// TestRenderPage_DrawsDeviceCMYKThroughTheDefaultProfile pins that RenderPage
// paints a DeviceCMYK fill through the profile [WithDefaultCMYKProfile]
// names, and through the built-in one without it: a profile that reads every
// colour as white leaves the translucent cyan page white.
func TestRenderPage_DrawsDeviceCMYKThroughTheDefaultProfile(t *testing.T) {
	t.Parallel()

	data := translucentCyanPDF()
	if got := renderCentre(t, data); got == white {
		t.Error("the built-in profile drew the cyan fill white")
	}
	if got := renderCentre(t, data, WithDefaultCMYKProfile(whiteProfile(t))); got != white {
		t.Errorf("the caller's profile: centre is %v, want white", got)
	}
}

// TestFlattenTransparency_CompositesThroughTheDefaultProfile pins that
// FlattenTransparency composites DeviceCMYK through the Reader's default CMYK
// profile: the white profile flattens the translucent cyan page to white, and
// the built-in one keeps it cyan.
func TestFlattenTransparency_CompositesThroughTheDefaultProfile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		opts      []OpenOption
		wantWhite bool
	}{
		{"the built-in profile", nil, false},
		{"the caller's profile", []OpenOption{WithDefaultCMYKProfile(whiteProfile(t))}, true},
	} {
		w := NewWriter(openColorPDF(t, translucentCyanPDF(), tc.opts...))
		t.Cleanup(func() { _ = w.Close() })
		if _, err := w.FlattenTransparency(t.Context(), WithFlattenDPI(72)); err != nil {
			t.Fatalf("%s: FlattenTransparency: %v", tc.name, err)
		}
		if got := renderCentre(t, writeAll(t, w)); (got == white) != tc.wantWhite {
			t.Errorf("%s: the flattened centre is %v, want white %t", tc.name, got, tc.wantWhite)
		}
	}
}
