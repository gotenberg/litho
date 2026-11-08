// Command gen-xlsx-tablestyles generates the preset table styles the XLSX
// reader of pkg/internal/convert/xlsx applies to a worksheet's tables.
//
// A workbook names a table's style ("TableStyleMedium2") without defining
// it when the style is one of the presets of ECMA-376 Part 1, 18.8.40,
// which the standard's electronic annex lists in presetTableStyles.xml
// (inside OfficeOpenXML-SpreadsheetMLStyles.zip). The generator downloads
// the annex of ECMA-376 Part 1, 5th edition, refuses an archive whose
// SHA-256 differs from the pinned digest, and writes each TableStyle preset
// (the PivotStyle presets are left out) as its differential formats and
// style elements, whitespace removed, in a Go map keyed by name.
//
// Usage:
//
//	go run ./cmd/gen-xlsx-tablestyles                 # download the annex
//	go run ./cmd/gen-xlsx-tablestyles --src FILE.zip  # read a local copy
//	go generate ./pkg/internal/convert/xlsx/...
package main
