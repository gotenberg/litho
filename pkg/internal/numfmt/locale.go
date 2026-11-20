package numfmt

import (
	"slices"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

//go:generate go run ../../../cmd/gen-locales -root ../../..

// localeData is what a locale decides of a formatted value, as LibreOffice's
// locale data states it (i18npool/source/localedata/data, one file a
// locale; cmd/gen-locales names the element each field comes from).
type localeData struct {
	tag models.Locale
	// lcid is the Windows locale identifier a [$-LCID] tag names the
	// locale with.
	lcid uint16
	// decimal and group are the decimal and thousands separators;
	// dateSep, timeSep and time100Sep the separators of the parts of a
	// date, a time and a second.
	decimal, group, dateSep, timeSep, time100Sep string
	// decimalAlt is a second decimal separator typed text may use, ""
	// for none.
	decimalAlt string
	// am and pm are what AM/PM writes.
	am, pm string
	// trueWord and falseWord name the booleans.
	trueWord, falseWord string
	// indian reports digits grouped by three then by two (12,34,567).
	indian bool
	// currency is the default currency's symbol.
	currency string
	// calendar is the unoid of the default calendar when it is not the
	// Gregorian one, which numfmt does not follow.
	calendar string
	// months, genitive and partitive are the abbreviated and full month
	// names, genitive and partitive nil when they are the names they fall
	// back on; days the day names from Sunday.
	months, genitive, partitive *[12][2]string
	// narrow are the narrow month names, nil for the full names' first
	// letters.
	narrow *[12]string
	days   *[7][2]string
	// codes are the locale's built-in formats in SpreadsheetML syntax,
	// "" for one the locale states with a keyword numfmt does not read.
	codes *[numCodes]string
	// acceptance are the date acceptance patterns of the CSV reader.
	acceptance []string
}

// lcidLocale maps a Windows locale identifier to an index of [locales].
type lcidLocale struct {
	id  uint16
	loc uint16
}

// The built-in formats of a locale, by i18npool's NumberFormatIndex: the
// order of cmd/gen-locales' codeOrder.
const (
	CodeScientific             = iota // SCIENTIFIC_000E00, 0.00E+00
	CodePercentInt                    // PERCENT_INT, 0%
	CodePercent                       // PERCENT_DEC2, 0.00%
	CodeCurrencyInt                   // CURRENCY_1000INT
	CodeCurrency                      // CURRENCY_1000DEC2
	CodeCurrencyIntRed                // CURRENCY_1000INT_RED
	CodeCurrencyRed                   // CURRENCY_1000DEC2_RED
	CodeDateShort                     // DATE_SYSTEM_SHORT
	CodeDateLong                      // DATE_SYSTEM_LONG
	CodeDateDDMMYY                    // DATE_SYS_DDMMYY
	CodeDateDDMMYYYY                  // DATE_SYS_DDMMYYYY
	CodeDateISO                       // DATE_DIN_YYYYMMDD
	CodeTimeHHMM                      // TIME_HHMM
	CodeTimeHHMMSS                    // TIME_HHMMSS
	CodeTimeHHMMAMPM                  // TIME_HHMMAMPM
	CodeTimeHHMMSSAMPM                // TIME_HHMMSSAMPM
	CodeTimeElapsed                   // TIME_HH_MMSS, [HH]:MM:SS
	CodeTimeMMSS00                    // TIME_MMSS00
	CodeTimeElapsed00                 // TIME_HH_MMSS00
	CodeDateTime                      // DATETIME_SYSTEM_SHORT_HHMM
	CodeDateTimeDDMMYYYYHHMMSS        // DATETIME_SYS_DDMMYYYY_HHMMSS
	CodeDateTimeDDMMYYYYHHMM          // DATETIME_SYS_DDMMYYYY_HHMM
	CodeDate                          // the default date format
	CodeTime                          // the default time format
	CodeDateTimeDefault               // the default date and time format
	CodePercentDefault                // the default percent format
	CodeScientificDefault             // the default scientific format
	CodeCurrencyDefault               // the default currency, two decimals or its own
	CodeCurrencyDefaultInt            // the default currency without decimals
	numCodes
)

// lookup returns the data of loc, nil for a locale the table lacks and
// for the zero Locale.
func lookup(loc models.Locale) *localeData {
	i := sort.Search(len(locales), func(i int) bool { return locales[i].tag >= loc })
	if i < len(locales) && locales[i].tag == loc {
		return &locales[i]
	}
	return nil
}

// Known reports whether loc is a locale of the table, one of the Locale
// constants of models.
func Known(loc models.Locale) bool {
	return lookup(loc) != nil
}

// IsDefault reports whether loc formats as the default, en-US: the zero
// Locale or en-US itself.
func IsDefault(loc models.Locale) bool {
	return loc == "" || loc == models.LocaleEnUS
}

// Code returns the built-in format code c of loc in SpreadsheetML syntax,
// "" for a locale the table lacks and for a format the locale states with
// a keyword numfmt does not read.
func Code(loc models.Locale, c int) string {
	d := lookup(loc)
	if d == nil || c < 0 || c >= numCodes {
		return ""
	}
	return d.codes[c]
}

// Separators returns the decimal and thousands separators of loc, "." and
// "," for the default and a locale the table lacks.
func Separators(loc models.Locale) (decimal, group string) {
	d := lookup(loc)
	if d == nil {
		return ".", ","
	}
	return d.decimal, d.group
}

// LocaleOf returns the locale of the language tag lang ("de-DE", "es_ES",
// "hu"): the locale of that tag, in any case, else, for a tag that names
// a language alone or a country the table lacks, the locale of that
// language in its own country (de-DE for de), else the first of that
// language by tag; "" for an empty tag or a language the table lacks.
func LocaleOf(lang string) models.Locale {
	l := strings.ReplaceAll(strings.TrimSpace(lang), "_", "-")
	if l == "" {
		return ""
	}
	for i := range locales {
		if strings.EqualFold(string(locales[i].tag), l) {
			return locales[i].tag
		}
	}
	prefix, _, _ := strings.Cut(l, "-")
	prefix = strings.ToLower(prefix)
	if d := lookup(models.Locale(prefix + "-" + strings.ToUpper(prefix))); d != nil {
		return d.tag
	}
	for i := range locales {
		if p, _, _ := strings.Cut(string(locales[i].tag), "-"); p == prefix {
			return locales[i].tag
		}
	}
	return ""
}

// localeOfLCID returns the locale a Windows locale identifier names, its
// sort and its high bits ignored (MS-LCID, 2.2), nil for one the table
// lacks.
func localeOfLCID(id uint64) *localeData {
	id &= 0xFFFF
	i := sort.Search(len(lcidLocales), func(i int) bool { return uint64(lcidLocales[i].id) >= id })
	if i < len(lcidLocales) && uint64(lcidLocales[i].id) == id {
		return &locales[lcidLocales[i].loc]
	}
	return nil
}

// LCID returns the Windows locale identifier a [$-LCID] tag names loc
// with, false for a locale the table lacks.
func LCID(loc models.Locale) (uint64, bool) {
	d := lookup(loc)
	if d == nil {
		return 0, false
	}
	return uint64(d.lcid), true
}

// LCIDOf returns the Windows locale identifier of the language tag lang
// ("hu-HU"), for a reader that writes a locale into a format code as a
// [$-LCID] tag; false for a locale whose separators are en-US's or that
// the table lacks.
func LCIDOf(lang string) (uint64, bool) {
	d := lookup(LocaleOf(lang))
	if d == nil || d.decimal == "." && d.group == "," {
		return 0, false
	}
	return uint64(d.lcid), true
}

// WithLocale returns f formatting in loc: its separators, its names and
// its system formats, a section's own [$-LCID] tag still naming that
// section's locale. It returns f itself for the default locale and for a
// locale the table lacks, and a copy otherwise.
func (f *Format) WithLocale(loc models.Locale) *Format {
	d := lookup(loc)
	if f == nil || d == nil || IsDefault(loc) {
		return f
	}
	out := *f
	out.sections = make([]*section, len(f.sections))
	for i, s := range f.sections {
		c := *s
		if c.system != 0 {
			code := d.codes[CodeDateLong]
			if c.system == sysTime {
				code = d.codes[CodeTime]
			}
			if ns, err := scanSection(code); code != "" && err == nil {
				ns.color, ns.op, ns.limit, ns.explicitCond = c.color, c.op, c.limit, c.explicitCond
				ns.excel, ns.system = c.excel, c.system
				c = *ns
			}
		}
		if !c.tagged {
			c.loc = d
			c.toks = slices.Clone(c.toks)
			c.plainSpaces()
		}
		out.sections[i] = &c
	}
	return &out
}

// LocaleInfo is what a reader of typed text needs of a locale.
type LocaleInfo struct {
	// Decimal and Group are the separators; Indian reports digits
	// grouped by three then two.
	Decimal, Group string
	Indian         bool
	// DecimalAlt is a second decimal separator typed text may use, ""
	// for none.
	DecimalAlt string
	// Currency is the default currency's symbol.
	Currency string
	// AM and PM are the time markers, True and False the boolean words.
	AM, PM, True, False string
	// DateSep, TimeSep and Time100Sep separate the parts of a date, of a
	// time and the fraction of a second.
	DateSep, TimeSep, Time100Sep string
	// Months are the abbreviated and full month names, Genitive and
	// Partitive their other cases, nil when the same.
	Months              [12][2]string
	Genitive, Partitive *[12][2]string
	// Acceptance are the date acceptance patterns ("D.M.", "M/D").
	Acceptance []string
	// Order is the order of day, month and year in the locale's short
	// date: "DMY", "MDY" or "YMD".
	Order string
}

// Info returns what loc states for a reader of typed text, false for a
// locale the table lacks.
func Info(loc models.Locale) (LocaleInfo, bool) {
	d := lookup(loc)
	if d == nil {
		return LocaleInfo{}, false
	}
	return LocaleInfo{
		Decimal: d.decimal, DecimalAlt: d.decimalAlt, Group: d.group, Indian: d.indian, Currency: d.currency,
		AM: d.am, PM: d.pm, True: d.trueWord, False: d.falseWord, DateSep: d.dateSep, TimeSep: d.timeSep, Time100Sep: d.time100Sep,
		Months: *d.months, Genitive: d.genitive, Partitive: d.partitive,
		Acceptance: d.acceptance, Order: dateOrder(d.codes[CodeDateDDMMYYYY]),
	}, true
}

// dateOrder returns the order of the day, month and year keywords of a
// date code, "DMY" when it does not say.
func dateOrder(code string) string {
	var out []byte
	for i := 0; i < len(code); i++ {
		c := code[i] &^ 0x20
		switch c {
		case '"':
			if j := strings.IndexByte(code[i+1:], '"'); j >= 0 {
				i += j + 1
			}
		case 'D', 'M', 'Y':
			if !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	if len(out) != 3 {
		return "DMY"
	}
	return string(out)
}
