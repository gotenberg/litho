// Command gen-shape-presets generates the preset geometries the shape
// package of pkg/internal/shape draws.
//
// A DrawingML shape names its geometry ("rightArrow", "star5") without
// defining it when the geometry is one of the presets of ECMA-376 Part 1,
// 20.1.10.56 ST_ShapeType, which the standard's electronic annex defines
// in presetShapeDefinitions.xml (Annex D, inside
// OfficeOpenXML-DrawingMLGeometries.zip): each preset's adjustments,
// guide formulas, text rectangle and paths, in the custom geometry syntax
// of 20.1.9.8. The generator downloads LibreOffice core's copy of that
// file at a pinned commit, which restores the upArrow the annex lacks,
// and refuses a file whose SHA-256 differs from the pinned digest. It
// resolves every name of a preset as 20.1.9.11 says (an earlier guide, an
// adjustment or a built-in), refuses a preset with a name that resolves
// to none of them, defined twice, or past the shape package's caps, and
// writes the presets as Go data: guides as operators over resolved
// operands, paths as commands, keyed by name.
//
// Usage:
//
//	go run ./cmd/gen-shape-presets                 # download the definitions
//	go run ./cmd/gen-shape-presets --src FILE.xml  # read a local copy
//	go generate ./pkg/internal/shape/...
package main
