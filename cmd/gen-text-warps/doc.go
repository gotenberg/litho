// Command gen-text-warps generates the preset text warps the shape package
// of pkg/internal/shape fits a body's text to.
//
// A DrawingML body names the shape its text is warped to ("textArchUp",
// "textInflate") with prstTxWarp, one of the forty presets of ECMA-376
// Part 1, 20.1.10.76 ST_TextShapeType, which the standard's electronic
// annex defines in presetTextWarpDefinitions.xml (Annex D, inside
// OfficeOpenXML-DrawingMLGeometries.zip): each preset's adjustments,
// guide formulas and paths, in the custom geometry syntax of 20.1.9.8.
// The generator downloads the archive of ECMA-376 Part 1, fifth edition,
// from Ecma International, reads the definitions from the annex inside it,
// and refuses an archive or a definitions file whose SHA-256 differs from
// the pinned digest. It resolves every name of a preset as 20.1.9.11 says
// (an earlier guide, an adjustment or a built-in), refuses a preset with
// a name that resolves to none of them, defined twice, or past the shape
// package's caps, and writes the presets as Go data, keyed by name.
//
// The definitions are © Ecma International, published free of charge; the
// Ecma copyright notice allows derivative works that implement the
// standard, and the generated file carries the notice.
//
// Usage:
//
//	go run ./cmd/gen-text-warps                 # download the archive
//	go run ./cmd/gen-text-warps --src FILE.xml  # read presetTextWarpDefinitions.xml
//	go generate ./pkg/internal/shape/...
package main
