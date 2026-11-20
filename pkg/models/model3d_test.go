package models

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestModel3D_JSONRoundTrip(t *testing.T) {
	in := Model3D{
		Name:    "cube",
		Subtype: "U3D",
		Open: func(_ context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("u3d")), nil
		},
	}
	assertRoundTripExceptOpen(t, in, func(v *Model3D) { v.Open = nil })
}
