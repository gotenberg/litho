// Package csv converts a comma-separated values file (RFC 4180) as LibreOffice Calc opens one through Gotenberg's LibreOffice route to the composition model: it reads  fields separated by commas, semicolons or tabs, quoted with double quotes, in UTF-8, with numbers, dates and times detected:one sheet, named after nothing (a CSV prints without a header), its columns sized as Calc sizes them into a sheet.Document, which
// sheet.Convert paginates and lays out as LibreOffice prints a
// spreadsheet. LibreOffice, as Gotenberg's LibreOffice route prints a
// document, is the reference wherever the specification leaves room or
// LibreOffice departs from it; see [Convert].
package csv
