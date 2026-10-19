package icc

import (
	_ "embed" // the built-in grey profile
	"slices"
)

// sGrayProfile is litho's version 2.4 grey display profile, the sRGB tone
// curve and white of IEC 61966-2-1, which cmd/gen-iccv2 generates
// (profiles/README.md).
//
//go:embed profiles/sgray-v2.icc
var sGrayProfile []byte

// SGrayProfile returns a copy of litho's built-in grey profile, for a writer
// that needs a stock version 2 profile of the grey data colour space. A grey
// level reads through it as the sRGB colour of three equal components, the
// reading DeviceGray gets. It is a copy because the caller may hand the bytes
// to a stream it later edits.
func SGrayProfile() []byte {
	return slices.Clone(sGrayProfile)
}
