// Package xlsx converts a SpreadsheetML workbook, an XLSX file (ECMA-376 Part 1, 18; Part 2 for the package) to the composition model: it reads the package, the shared strings, the styles with their number formats, the worksheets with their columns, rows, cells, merges, page setup, headers, footers and drawings into a sheet.Document, which
// sheet.Convert paginates and lays out as LibreOffice prints a
// spreadsheet. LibreOffice is the reference wherever the specification
// leaves room or LibreOffice departs from it; see [Convert].
package xlsx
