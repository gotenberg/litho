// Package csv converts a comma-separated values file (RFC 4180) to the
// composition model as LibreOffice Calc opens one: it reads fields
// separated by commas, semicolons or tabs alike, quoted with double
// quotes, in UTF-8, detects numbers, percentages, currency amounts,
// booleans, dates and times as Calc's Standard column type does in the
// en-US locale, and fills one sheet, named Sheet1 and printed without a
// page header, its columns sized as Calc sizes them after an import, into
// a sheet.Document, which sheet.Convert paginates and lays out as a
// spreadsheet prints. LibreOffice is the reference wherever the
// specification leaves room or LibreOffice departs from it; see
// [Convert].
package csv
