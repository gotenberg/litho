package xlsx

import (
	"regexp"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/models"
)

// builtinFormats maps the built-in number format ids (ECMA-376 Part 1,
// 18.8.30, the table of implied formats) onto the codes Excel shows them
// with when Office for the web prints a workbook: the locale-dependent ids
// read as that service's codes, 14 the short date MM/DD/YYYY, 15 and 16
// with a two-digit day, 20 and 21 a two-digit hour, 22 the short date and
// 24-hour time, 5 to 8, 42 and 44 the currency with the generic
// currency sign; the East Asian ids 27 to 36 and 50 to 58 as the short
// date or the time they stand for, as LibreOffice reads them (not
// measured). An id the table lacks reads as General.
//
// # Corpus evidence
//
//   - litho/xlsx: numfmt_builtin prints 1234.567 as 05/18/1903 for 14,
//     05/18/1903 13:36 for 22, 0 as 00-Jan-00 for 15, 00:00 for 20 and
//     01/00/1900 00:00 for 22, ¤1,235 for 5 and 6, ¤1,234.57 for 7 and 8,
//     and the sign ¤ in 42 and 44; dates_1900 prints 03/15/2023 for 14.
//   - poi/xlsx: 55927.xlsx prints 01/13/2012 for 14.
//   - libreoffice/xlsx: sc/tdf129985.xlsx prints its format 14 date as
//     MM/DD/YYYY.
var builtinFormats = map[int]string{
	0:  "General",
	1:  "0",
	2:  "0.00",
	3:  "#,##0",
	4:  "#,##0.00",
	5:  `"¤"#,##0_);\("¤"#,##0\)`,
	6:  `"¤"#,##0_);[Red]\("¤"#,##0\)`,
	7:  `"¤"#,##0.00_);\("¤"#,##0.00\)`,
	8:  `"¤"#,##0.00_);[Red]\("¤"#,##0.00\)`,
	9:  "0%",
	10: "0.00%",
	11: "0.00E+00",
	12: "# ?/?",
	13: "# ??/??",
	14: "mm/dd/yyyy",
	15: "dd-mmm-yy",
	16: "dd-mmm",
	17: "mmm-yy",
	18: "h:mm AM/PM",
	19: "h:mm:ss AM/PM",
	20: "hh:mm",
	21: "hh:mm:ss",
	22: "mm/dd/yyyy hh:mm",
	27: "m/d/yyyy",
	28: "m/d/yyyy",
	29: "m/d/yyyy",
	30: "m/d/yyyy",
	31: "m/d/yyyy",
	32: "h:mm:ss",
	33: "h:mm:ss",
	34: "h:mm:ss",
	35: "h:mm:ss",
	36: "m/d/yyyy",
	37: `#,##0_);\(#,##0\)`,
	38: `#,##0_);[Red]\(#,##0\)`,
	39: `#,##0.00_);\(#,##0.00\)`,
	40: `#,##0.00_);[Red]\(#,##0.00\)`,
	41: `_(* #,##0_);_(* \(#,##0\);_(* "-"_);_(@_)`,
	42: `_("¤"* #,##0_);_("¤"* \(#,##0\);_("¤"* "-"_);_(@_)`,
	43: `_(* #,##0.00_);_(* \(#,##0.00\);_(* "-"??_);_(@_)`,
	44: `_("¤"* #,##0.00_);_("¤"* \(#,##0.00\);_("¤"* "-"??_);_(@_)`,
	45: "mm:ss",
	46: "[h]:mm:ss",
	47: "mm:ss.0",
	48: "##0.0E+0",
	49: "@",
	50: "m/d/yyyy",
	51: "m/d/yyyy",
	52: "m/d/yyyy",
	53: "m/d/yyyy",
	54: "m/d/yyyy",
	55: "m/d/yyyy",
	56: "m/d/yyyy",
	57: "m/d/yyyy",
	58: "m/d/yyyy",
}

// localeBuiltin returns the code a built-in number format id takes under
// a locale other than en-US, "" for an id that keeps its code of
// [builtinFormats] and under en-US. The ids that stand for a format of the
// system take the locale's, as Calc's import maps them
// (sc/source/filter/oox/numberformatsbuffer.cxx, spBuiltinFormats_BASE):
// 9 and 10 the percent formats, 14 the short date with a four-digit year,
// 22 that date and the 24-hour time, 5 to 8, 42 and 44 the currency
// formats of the locale's default currency, red where Excel's are. The
// other ids keep their codes, the names, AM and PM they write being the
// locale's. Excel's own print of a workbook under another locale is not
// measured: these are Calc's.
//
// # Corpus evidence
//
//   - probe, xlsx: over 37 locales, 350 of 504 cases of ids 5 to 8, 10
//     and 14 to 22 print as Calc prints them (TestLocaleBuiltin_Calc):
//     2025-12-09 13:30 in id 14 as 09.12.2025 under de-DE and ru-RU,
//     09/12/2025 under fr-FR, 2025/12/09 under ja-JP, 09-12-2025 under
//     hi-IN; the others follow Calc's own table a locale.
func localeBuiltin(id int, loc models.Locale) string {
	if numfmt.IsDefault(loc) {
		return ""
	}
	red := func(code string) string {
		pos, neg, ok := strings.Cut(code, ";")
		if !ok {
			return code
		}
		return pos + ";[RED]" + neg
	}
	// Excel's currency formats show no decimals or two, whatever the
	// currency's.
	withDecimals := func(code string) string {
		return currencyNumber.ReplaceAllLiteralString(code, "#,##0.00")
	}
	switch id {
	case 9:
		return numfmt.Code(loc, numfmt.CodePercentInt)
	case 10:
		return numfmt.Code(loc, numfmt.CodePercent)
	case 14:
		return numfmt.Code(loc, numfmt.CodeDateDDMMYYYY)
	case 22:
		if d := numfmt.Code(loc, numfmt.CodeDateDDMMYYYY); d != "" {
			return d + " hh:mm"
		}
	case 5, 42:
		return numfmt.Code(loc, numfmt.CodeCurrencyDefaultInt)
	case 6:
		return red(numfmt.Code(loc, numfmt.CodeCurrencyDefaultInt))
	case 7, 44:
		return withDecimals(numfmt.Code(loc, numfmt.CodeCurrencyDefaultInt))
	case 8:
		return red(withDecimals(numfmt.Code(loc, numfmt.CodeCurrencyDefaultInt)))
	}
	return ""
}

// currencyNumber matches the number of a currency format code.
var currencyNumber = regexp.MustCompile(`#,##0(\.0+)?`)

// maxFormatCode caps a number format code's length. Anti-DoS guard, not
// spec-mandated: Excel refuses codes past 255 characters.
const maxFormatCode = 1024

// isDateFormat reports whether a format code shows a calendar date: a
// year, day or month-name placeholder (y, d, mmm) outside quoted text,
// escapes and bracketed sections (ECMA-376 Part 1, 18.8.31). A time-only
// or elapsed-time code is not a date.
func isDateFormat(code string) bool {
	lower := strings.ToLower(code)
	months := 0
	for i := 0; i < len(lower); i++ {
		switch c := lower[i]; c {
		case '"':
			if j := strings.IndexByte(lower[i+1:], '"'); j >= 0 {
				i += j + 1
			} else {
				return false
			}
		case '\\', '_', '*':
			i++
		case '[':
			if j := strings.IndexByte(lower[i:], ']'); j >= 0 {
				i += j
			}
		case 'y', 'd':
			return true
		case 'm':
			months++
			if months >= 3 {
				return true
			}
			continue
		case ';':
			return false
		}
		months = 0
	}
	return false
}
