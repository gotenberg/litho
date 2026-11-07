// Package chart holds the model of a chart embedded in an Office document
// and draws it as LibreOffice draws one. A reader fills a [Chart] from a
// DrawingML chart part (ECMA-376 Part 1, 21.2) or an OpenDocument chart
// document (ODF 1.3 Part 3, 11); the spreadsheet and presentation
// converters place it as a picture whose fragments this package returns.
package chart
