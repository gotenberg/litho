// Package numfmt formats spreadsheet values by number format codes as
// LibreOffice Calc shows them. A code is in the SpreadsheetML syntax
// (ECMA-376 Part 1, 18.8.31 numFmt and 18.8.30 the built-in formats),
// which Calc reads and writes for its own number formats too and which
// an OpenDocument data style translates to (ODF 1.3 Part 3, 16.29).
//
// [Parse] reads a code once; [Format.Apply] formats a value by it, and
// [Format.Fit] formats it for a cell of a given width, as Calc narrows a
// General number and shows "###" for a number that does not fit. The
// output is Calc's for the en-US locale, the only one: month and day names
// in English, "." the decimal separator, "," the thousands separator,
// TRUE and FALSE for booleans, and the null date 1899-12-30 for serial
// numbers.
//
// What a code can say:
//
//   - Up to four sections separated by ";": positive, negative, zero and
//     text, or two sections chosen by conditions such as [>=100].
//   - Colours [Black] [Blue] [Cyan] [Green] [Magenta] [Red] [White]
//     [Yellow], LibreOffice's [Brown] and [Grey], and [Color n] from the
//     56-colour workbook palette.
//   - Locale tags [$-409], which are ignored, and currency tags [$€-407],
//     which show their symbol.
//   - Literal text in quotes, backslash escapes, and the characters that
//     stand for themselves ($ - + / ( ) : ! ^ & ' ~ { } < > = and space).
//   - "_x", replaced by the spaces LibreOffice inserts for the width of
//     x, and "*x", which repeats x to fill the cell: [Result.Fill] marks
//     where, and the caller, who knows the cell width, repeats it.
//   - The digit placeholders 0 # ?, the decimal point, the thousands
//     separator, scaling commas, percent, scientific notation E+00 and
//     E-00, and fractions such as # ?/? and 0/8.
//   - Dates and times: yy yyyy m mm mmm mmmm mmmmm d dd ddd dddd h hh m
//     mm s ss, fractions of a second, AM/PM and A/P, and the elapsed times
//     [h] [mm] [ss]; m is the minute after an hour or before a second, as
//     LibreOffice and Excel decide, and the month otherwise.
//   - @, the text of a text value, and General, Calc's standard format.
//
// Not supported: the calendar and numeral system tags ([~buddhist],
// [NatNum1], [DBNum1]), which are ignored, other locales, the era and
// quarter keywords, and Japanese and Chinese date keywords.
package numfmt
