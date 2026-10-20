// Command gen-iccv2 generates the version 2 ICC profiles of
// pkg/internal/graphics/icc: the grey profile profiles/sgray-v2.icc and the
// CMYK printer profile profiles/cgats001-cmyk-v2.icc.
//
// Remediation replaces an output intent's destination profile it cannot keep
// with a stock profile of the same data colour space (ISO 19005-1, 6.2.2 and
// ISO 19005-2, 6.2.3), and PDF/A-1 admits only a version 2 profile there.
//
// The stock grey profile is a monochrome display profile (ICC.1:2001-04,
// 6.3.1.2) whose tone curve is the sRGB transfer function and whose media
// white is the sRGB white, both from IEC 61966-2-1: a grey level reads as the
// sRGB colour of three equal components, the reading litho, PDF.js and lcms
// give DeviceGray. The generator computes every byte from those published
// constants and the ICC.1:2001-04 layout.
//
// The stock CMYK profile is an output profile (ICC.1:2001-04, 6.3.1.3) of the
// CGATS TR 001 (SWOP) printing condition over the Lab connection space. Its
// AToB tables are the AToB0 table of profiles/CGATS001Compat-v2-micro.icc, the
// CC0 1.0 profile litho reads DeviceCMYK through, so a CMYK colour reads
// through the stock profile exactly as through litho's default; its BToA
// tables invert that characterisation by least squares within the 300 per
// cent SWOP ink limit, with a medium grey-component replacement, and its
// gamut table marks the Lab colours the inversion cannot reach.
//
// Both profiles copy no vendor profile, their copyright tags dedicate them to
// the public domain under CC0 1.0, as profiles/README.md records, and every
// run writes the same bytes on every architecture.
//
// Usage:
//
//	go run ./cmd/gen-iccv2              # write into pkg/internal/graphics/icc/profiles
//	go run ./cmd/gen-iccv2 --out DIR    # write into DIR, which holds CGATS001Compat-v2-micro.icc
//	go generate ./pkg/internal/graphics/icc/...
package main
