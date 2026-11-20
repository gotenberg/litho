package models

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestImageMeta_JSONRoundTrip(t *testing.T) {
	in := ImageMeta{
		Filename:         "img.jpg",
		Width:            800,
		Height:           600,
		ColorSpace:       "DeviceRGB",
		BitsPerComponent: 8,
		Open: func(_ context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("data")), nil
		},
	}
	assertRoundTripExceptOpen(t, in, func(v *ImageMeta) { v.Open = nil })
}
