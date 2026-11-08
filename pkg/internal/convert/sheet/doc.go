// Package sheet lays a spreadsheet out as LibreOffice Calc prints one. It
// is the model the XLSX, ODS and CSV converters fill ([Document], a book of
// [Sheet] values whose cells carry a value, a number format and a style,
// each sheet with its page setup, headers and footers, merges and
// drawings) and the builder that paginates it: every sheet that is not
// hidden, in book order, its print ranges cut into pages by the sheet's
// paper, margins, scale or fit-to-pages, page order, repeated rows and
// columns and manual breaks, the cells formatted by their number formats
// and drawn with their fonts, fills, borders, alignment, wrapping and
// rotation, text overflowing into the empty cells beside it as Calc lets
// it, the headers and footers with their fields resolved per page, and
// the pictures and charts anchored to the cells.
//
// The readers know their file format and nothing of pages; the builder
// knows pages and nothing of any file format. A length anywhere in the
// model is in points; a row or column index is 0-based. LibreOffice Calc
// is the reference wherever the specifications (ECMA-376 Part 1, 18; ODF
// 1.3 Part 3, 9) leave room or LibreOffice departs from them; see
// [Convert].
package sheet
