package icc

import (
	_ "embed" // the built-in sRGB profile
	"slices"
)

// srgbProfile is a version 2.1 display profile of sRGB (IEC 61966-2-1), the
// one remediation has embedded for its output intents and blending spaces
// (profiles/README.md).
//
//go:embed profiles/srgb.icc
var srgbProfile []byte

// SRGBProfile returns a copy of litho's built-in sRGB profile, for a writer
// that states in sRGB the colour of samples it produced. It is a copy because
// the caller may hand the bytes to a stream it later edits.
func SRGBProfile() []byte {
	return slices.Clone(srgbProfile)
}
