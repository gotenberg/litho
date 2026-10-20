package icc

import (
	_ "embed" // the stock CMYK printer profile
	"slices"
)

// cmykPrinterProfile is litho's version 2.4 CMYK output profile of the CGATS
// TR 001 (SWOP) printing condition, which cmd/gen-iccv2 generates from the
// built-in CMYK profile's characterisation (profiles/README.md).
//
//go:embed profiles/cgats001-cmyk-v2.icc
var cmykPrinterProfile []byte

// CMYKPrinterProfile returns a copy of litho's stock CMYK printer profile, for
// a writer that needs a version 2 output profile of the CMYK data colour
// space. A CMYK colour reads through it as through the built-in profile
// [BuiltinCMYK] transforms, the reading DeviceCMYK gets when nothing names
// another. It is a copy because the caller may hand the bytes to a stream it
// later edits.
func CMYKPrinterProfile() []byte {
	return slices.Clone(cmykPrinterProfile)
}
