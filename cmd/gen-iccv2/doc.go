// Command gen-iccv2 generates the version 2 grey ICC profile of
// pkg/internal/graphics/icc, profiles/sgray-v2.icc.
//
// Remediation replaces an output intent's destination profile it cannot keep
// with a stock profile of the same data colour space (ISO 19005-1, 6.2.2 and
// ISO 19005-2, 6.2.3), and PDF/A-1 admits only a version 2 profile there. The
// stock grey profile is a monochrome display profile (ICC.1:2001-04, 6.3.1.2)
// whose tone curve is the sRGB transfer function and whose media white is the
// sRGB white, both from IEC 61966-2-1: a grey level reads as the sRGB colour
// of three equal components, the reading litho, PDF.js and lcms give
// DeviceGray. The generator computes every byte from those published
// constants and the ICC.1:2001-04 layout, copies no vendor profile, and writes
// the same bytes on every run.
//
// Usage:
//
//	go run ./cmd/gen-iccv2              # write pkg/internal/graphics/icc/profiles/sgray-v2.icc
//	go run ./cmd/gen-iccv2 --out DIR    # write DIR/sgray-v2.icc
//	go generate ./pkg/internal/graphics/icc/...
package main
