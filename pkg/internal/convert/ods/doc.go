// Package ods converts an OpenDocument spreadsheet, an ODS or flat FODS file (ODF 1.3 Part 3, 9, the table elements; Part 2 for the package) to the composition model: it reads the package, the styles and data styles, the tables with their columns, rows, cells, spans, page layouts, headers, footers and frames into a sheet.Document, which
// sheet.Convert paginates and lays out as LibreOffice prints a
// spreadsheet. LibreOffice, as Gotenberg's LibreOffice route prints a
// document, is the reference wherever the specification leaves room or
// LibreOffice departs from it; see [Convert].
package ods
