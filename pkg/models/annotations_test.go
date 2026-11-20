package models

import "testing"

func TestAnnotation_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Annotation{
		Subtype:    "Link",
		Rect:       [4]float64{0, 0, 100, 100},
		Contents:   "click me",
		URI:        "https://example.com",
		DestPage:   5,
		QuadPoints: [][8]float64{{0, 0, 100, 0, 0, 100, 100, 100}},
	})
}

// ImageMeta, EmbeddedFile, and Model3D carry a lazy Open closure that cannot
// round-trip through JSON. The dedicated tests below verify every other
// field survives and that Open is reset to nil on the decode side.
