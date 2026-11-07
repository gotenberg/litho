// Package csv converts a comma-separated values file (RFC 4180) to the
// composition model as LibreOffice Calc opens one through Gotenberg's
// LibreOffice route: it reads fields separated by commas, semicolons or
// tabs alike, quoted with double quotes, in UTF-8, detects numbers,
// percentages, currency amounts, booleans, dates and times as Calc's
// Standard column type does in the en-US locale, and fills one sheet,
// named after nothing (a CSV prints without a header), its columns sized
// as Calc sizes them after an import, into a sheet.Document, which
// sheet.Convert paginates and lays out as LibreOffice prints a
// spreadsheet. LibreOffice, as Gotenberg's LibreOffice route prints a
// document, is the reference wherever the specification leaves room or
// LibreOffice departs from it; see [Convert].
package csv
