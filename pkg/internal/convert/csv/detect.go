package csv

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// kind is the format Calc's Standard column type gives a detected value
// in the en-US locale, with "detect special numbers" on.
type kind uint8

// The detected kinds.
//
// # Corpus evidence
//
//   - litho: a CSV of one value per row, converted by the image, prints
//     123, -1.5, "+3" as 3, "007" as 7, "1,234.56" as 1234.56, ".5" as
//     0.5, "(5)" and "5-" as -5, "1.5E+03" and "1e5" as 1.50E+03 and
//     1.00E+05, "15%" and "5 %" as 15.00% and 5.00%, "$1234.50", "-$5"
//     and "$-5" as $1,234.50 and -$5.00, "2026-01-10" and "2026-1-5" as
//     ISO dates, "1/10/2026", "01/10/26", "Jan 10, 2026", "January 10,
//     2026", "10-Jan-2026" and "Jan 2026" as 01/10/26 and 01/01/26, "1/10"
//     and "1/2" in the current year, "12:30" and "13:45" as 12:30:00 PM
//     and 01:45:00 PM, "25:10" as 25:10:00, "12:30:45.5" as 12:30:45.50,
//     "12/31/2026 10:00" as 12/31/26 10:00 AM, "2026-01-10 13:45" as
//     2026-01-10 13:45:00, "2026-01-10T13:45:30" as itself, TRUE and false
//     as TRUE and FALSE, a quoted "42" and " 12 " as numbers; "12,34",
//     "1 000", "2026/01/10", "10.01.2026", "31/12/2026", "1-Jan", "€5",
//     "0x10", "Infinity", "NaN", "Monday" and "-(5)" as text.
const (
	kText kind = iota
	kGeneral
	kBool
	kPercent
	kCurrency
	kScientific
	kDateUS
	kDateISO
	kDateTimeISO
	kDateTimeISOT
	kDateTimeUS
	kTime
	kElapsed
	kTimeFraction
)

// formatCodes are the SpreadsheetML codes of the kinds, as Calc's en-US
// standard formats of each type write them.
var formatCodes = [...]string{
	kText: "General", kGeneral: "General", kBool: "General", kPercent: "0.00%",
	kCurrency: "[$$-409]#,##0.00;-[$$-409]#,##0.00", kScientific: "0.00E+00",
	kDateUS: "mm/dd/yy", kDateISO: "yyyy-mm-dd", kDateTimeISO: "yyyy-mm-dd hh:mm:ss",
	kDateTimeISOT: `yyyy-mm-dd"T"hh:mm:ss`, kDateTimeUS: "mm/dd/yy hh:mm AM/PM",
	kTime: "hh:mm:ss AM/PM", kElapsed: "[hh]:mm:ss", kTimeFraction: "hh:mm:ss.00",
}

// detected is a field read as Calc's Standard column type reads it.
type detected struct {
	kind  kind
	value float64
}

// detect reads a field as Calc's Standard column type does with the
// en-US locale and "detect special numbers" on (the route's token): a
// number, a percentage, a currency amount in dollars, a scientific
// number, a boolean, a date, a time or both; anything else is text.
// year is the year a date without one takes: Calc takes the current
// year, the reader the print date's.
func detect(field string, year int) detected {
	s := strings.TrimSpace(field)
	if s == "" {
		return detected{}
	}
	switch strings.ToUpper(s) {
	case "TRUE":
		return detected{kind: kBool, value: 1}
	case "FALSE":
		return detected{kind: kBool, value: 0}
	}
	if d, ok := detectNumber(s); ok {
		return d
	}
	if d, ok := detectDateTime(s, year); ok {
		return d
	}
	return detected{}
}

// detectNumber reads a number in the en-US syntax: a sign before (or a
// minus after), parentheses for a negative, a dollar sign before the
// digits or the sign, digits grouped by thousands with commas or not, a
// decimal point, an exponent, a percent sign after.
func detectNumber(s string) (detected, bool) {
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg, s = true, strings.TrimSpace(s[1:len(s)-1])
	}
	k := kGeneral
	if p, ok := strings.CutSuffix(s, "%"); ok {
		k, s = kPercent, strings.TrimSpace(p)
	}
	sign := func() bool {
		if v, ok := strings.CutPrefix(s, "-"); ok {
			s = v
			return true
		}
		if v, ok := strings.CutPrefix(s, "+"); ok {
			s = v
		}
		return false
	}
	if !neg {
		neg = sign()
	}
	if v, ok := strings.CutPrefix(s, "$"); ok {
		if k != kGeneral {
			return detected{}, false
		}
		k, s = kCurrency, v
		if !neg {
			neg = sign()
		}
	}
	if v, ok := strings.CutSuffix(s, "-"); ok && !neg {
		neg, s = true, v
	}
	mant, exp, hasExp := strings.Cut(strings.ToLower(s), "e")
	if hasExp {
		if k != kGeneral {
			return detected{}, false
		}
		k = kScientific
		e := strings.TrimPrefix(strings.TrimPrefix(exp, "+"), "-")
		if e == "" || strings.Trim(e, "0123456789") != "" {
			return detected{}, false
		}
	}
	intPart, frac, hasFrac := strings.Cut(mant, ".")
	if hasFrac && strings.Trim(frac, "0123456789") != "" || intPart == "" && frac == "" {
		return detected{}, false
	}
	if strings.Contains(intPart, ",") {
		groups := strings.Split(intPart, ",")
		if hasExp || len(groups[0]) < 1 || len(groups[0]) > 3 {
			return detected{}, false
		}
		for _, g := range groups[1:] {
			if len(g) != 3 {
				return detected{}, false
			}
		}
		intPart = strings.Join(groups, "")
	}
	if strings.Trim(intPart, "0123456789") != "" {
		return detected{}, false
	}
	num := intPart + "." + frac
	if intPart == "" {
		num = "0." + frac
	}
	if hasExp {
		num += "e" + exp
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(num, "."), 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return detected{}, false
	}
	if neg {
		v = -v
	}
	if v == 0 {
		v = 0
	}
	if k == kPercent {
		v /= 100
	}
	return detected{kind: k, value: v}, true
}

// monthNames are the en-US month names and their abbreviations.
var monthNames = map[string]int{
	"jan": 1, "january": 1, "feb": 2, "february": 2, "mar": 3, "march": 3, "apr": 4, "april": 4,
	"may": 5, "jun": 6, "june": 6, "jul": 7, "july": 7, "aug": 8, "august": 8, "sep": 9, "sept": 9,
	"september": 9, "oct": 10, "october": 10, "nov": 11, "november": 11, "dec": 12, "december": 12,
}

// detectDateTime reads a date, a time, or a date and a time: ISO 8601
// dates "2026-01-10" with a time after a space or a "T"; the en-US M/D/Y
// order "1/10/2026", "01/10/26", "1/10"; the month names "Jan 10, 2026",
// "January 10 2026", "10-Jan-2026", "Jan 2026"; times "12:30",
// "1:30 PM", "12:30:45.5", "25:10".
func detectDateTime(s string, year int) (detected, bool) {
	lower := strings.ToLower(s)
	if t, k, ok := detectTime(lower); ok {
		return detected{kind: k, value: t}, true
	}
	datePart, timePart, sep := lower, "", ""
	if i := strings.IndexByte(lower, ' '); i > 0 && (isoLike(lower[:i]) || strings.Contains(lower[:i], "/")) {
		datePart, timePart, sep = lower[:i], strings.TrimSpace(lower[i+1:]), " "
	} else if i := strings.IndexByte(lower, 't'); i > 0 && isoLike(lower[:i]) {
		datePart, timePart, sep = lower[:i], lower[i+1:], "t"
	}
	var day float64
	var k kind
	switch {
	case isoDate(datePart, &day):
		k = kDateISO
		if timePart != "" {
			k = kDateTimeISO
			if sep == "t" {
				k = kDateTimeISOT
			}
		}
	case usDate(datePart, year, &day):
		k = kDateUS
		if timePart != "" {
			k = kDateTimeUS
		}
	default:
		if timePart == "" && namedDate(lower, &day) {
			return detected{kind: kDateUS, value: day}, true
		}
		return detected{}, false
	}
	if timePart != "" {
		t, _, ok := detectTime(timePart)
		if !ok || t >= 1 {
			return detected{}, false
		}
		day += t
	}
	return detected{kind: k, value: day}, true
}

// isoLike reports a string that starts as an ISO 8601 date: four digits
// and a hyphen.
func isoLike(s string) bool {
	return len(s) > 5 && isDigit(s[0]) && isDigit(s[1]) && isDigit(s[2]) && isDigit(s[3]) && s[4] == '-'
}

// isDigit reports an ASCII digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// serial returns Calc's serial number of a date, days since 1899-12-30,
// and whether the date exists.
func serial(y, m, d int) (float64, bool) {
	if y < 1 || y > 9999 || m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d {
		return 0, false
	}
	return math.Round(t.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24), true
}

// atoi parses a run of 1 to n ASCII digits.
func atoi(s string, n int) (int, bool) {
	if s == "" || len(s) > n {
		return 0, false
	}
	v := 0
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return 0, false
		}
		v = v*10 + int(s[i]-'0')
	}
	return v, true
}

// isoDate reads "YYYY-M-D".
func isoDate(s string, day *float64) bool {
	f := strings.Split(s, "-")
	if len(f) != 3 || len(f[0]) != 4 {
		return false
	}
	y, ok1 := atoi(f[0], 4)
	m, ok2 := atoi(f[1], 2)
	d, ok3 := atoi(f[2], 2)
	if !ok1 || !ok2 || !ok3 {
		return false
	}
	v, ok := serial(y, m, d)
	*day = v
	return ok
}

// fullYear reads a two-digit year in Calc's default window, 1930 to
// 2029.
func fullYear(y, digits int) int {
	if digits > 2 {
		return y
	}
	if y < 30 {
		return 2000 + y
	}
	return 1900 + y
}

// usDate reads "M/D/Y" or "M/D", the en-US order.
func usDate(s string, year int, day *float64) bool {
	f := strings.Split(s, "/")
	if len(f) < 2 || len(f) > 3 {
		return false
	}
	m, ok1 := atoi(f[0], 2)
	d, ok2 := atoi(f[1], 2)
	if !ok1 || !ok2 {
		return false
	}
	y := year
	if len(f) == 3 {
		v, ok := atoi(f[2], 4)
		if !ok || len(f[2]) == 3 {
			return false
		}
		y = fullYear(v, len(f[2]))
	}
	v, ok := serial(y, m, d)
	*day = v
	return ok
}

// namedDate reads a date with a month name: "Jan 10, 2026", "January 10
// 2026", "10-Jan-2026", "10 Jan 2026", "Jan 2026".
func namedDate(s string, day *float64) bool {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '-' })
	var m, d, y, yDigits int
	switch {
	case len(f) == 3 && monthNames[f[0]] > 0:
		m = monthNames[f[0]]
		var ok bool
		if d, ok = atoi(f[1], 2); !ok {
			return false
		}
		if y, ok = atoi(f[2], 4); !ok {
			return false
		}
		yDigits = len(f[2])
	case len(f) == 3 && monthNames[f[1]] > 0:
		m = monthNames[f[1]]
		var ok bool
		if d, ok = atoi(f[0], 2); !ok {
			return false
		}
		if y, ok = atoi(f[2], 4); !ok {
			return false
		}
		yDigits = len(f[2])
	case len(f) == 2 && monthNames[f[0]] > 0 && len(f[1]) == 4:
		m, d = monthNames[f[0]], 1
		var ok bool
		if y, ok = atoi(f[1], 4); !ok {
			return false
		}
		yDigits = 4
	default:
		return false
	}
	if yDigits == 3 {
		return false
	}
	v, ok := serial(fullYear(y, yDigits), m, d)
	*day = v
	return ok
}

// detectTime reads "H:MM", "H:MM:SS" and "H:MM:SS.f", with "AM" or "PM",
// "A" or "P" after: the fraction of a day and the kind its display takes.
// Hours past 23 without AM or PM are a duration.
func detectTime(s string) (float64, kind, bool) {
	ampm := ""
	for _, suf := range []string{"am", "pm", "a", "p"} {
		if v, ok := strings.CutSuffix(s, suf); ok {
			ampm, s = suf[:1], strings.TrimSpace(v)
			break
		}
	}
	f := strings.Split(s, ":")
	if len(f) < 2 || len(f) > 3 {
		return 0, 0, false
	}
	h, ok1 := atoi(f[0], 4)
	mi, ok2 := atoi(f[1], 2)
	if !ok1 || !ok2 || len(f[1]) != 2 || mi > 59 {
		return 0, 0, false
	}
	sec := 0.0
	frac := false
	if len(f) == 3 {
		whole, fr, hasFr := strings.Cut(f[2], ".")
		sv, ok := atoi(whole, 2)
		if !ok || len(whole) != 2 || sv > 59 {
			return 0, 0, false
		}
		sec = float64(sv)
		if hasFr {
			if fr == "" || strings.Trim(fr, "0123456789") != "" || len(fr) > 9 {
				return 0, 0, false
			}
			v, _ := strconv.ParseFloat("0."+fr, 64)
			sec += v
			frac = true
		}
	}
	k := kTime
	switch {
	case ampm != "":
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		h %= 12
		if ampm == "p" {
			h += 12
		}
	case frac:
		k = kTimeFraction
	case h > 23:
		k = kElapsed
	}
	return (float64(h)*3600 + float64(mi)*60 + sec) / 86400, k, true
}

// display returns the text Calc shows for v in the format of kind k: the
// string the column width is measured on.
func display(k kind, v float64) string {
	switch k {
	case kBool:
		if v != 0 {
			return "TRUE"
		}
		return "FALSE"
	case kPercent:
		return strconv.FormatFloat(v*100, 'f', 2, 64) + "%"
	case kCurrency:
		s := "$" + grouped(math.Abs(v))
		if v < 0 {
			s = "-" + s
		}
		return s
	case kScientific:
		return strings.ToUpper(strconv.FormatFloat(v, 'E', 2, 64))
	case kDateUS, kDateISO, kDateTimeISO, kDateTimeISOT, kDateTimeUS, kTime, kElapsed, kTimeFraction:
		return dateText(k, v)
	case kText, kGeneral:
	}
	return general(v)
}

// grouped writes |v| with two decimals and thousands commas.
func grouped(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	ip, fp, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range ip {
		if i > 0 && (len(ip)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String() + "." + fp
}

// general writes v as Calc's Standard format does without a width
// limit: 15 significant digits, the exponent form with three exponent
// digits for the large and the small.
func general(v float64) string {
	if v == 0 {
		return "0"
	}
	s := strconv.FormatFloat(v, 'g', 15, 64)
	mant, exp, ok := strings.Cut(s, "e")
	if !ok {
		return s
	}
	if strings.Contains(mant, ".") {
		mant = strings.TrimRight(strings.TrimRight(mant, "0"), ".")
	}
	sign := exp[:1]
	digits := strings.TrimLeft(exp[1:], "0")
	for len(digits) < 3 {
		digits = "0" + digits
	}
	return mant + "E" + sign + digits
}

// dateText writes the serial number v in a date or time kind's format.
func dateText(k kind, v float64) string {
	day := math.Floor(v)
	secs := math.Round((v - day) * 86400 * 100)
	t := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).Add(time.Duration(day) * 24 * time.Hour)
	cs := int(secs)
	h, m, s, c := cs/360000, cs/6000%60, cs/100%60, cs%100
	ampm := func() string {
		hh := h % 12
		if hh == 0 {
			hh = 12
		}
		suffix := "AM"
		if h >= 12 {
			suffix = "PM"
		}
		return fmt.Sprintf("%02d:%02d:%02d %s", hh, m, s, suffix)
	}
	switch k {
	case kDateUS:
		return fmt.Sprintf("%02d/%02d/%02d", int(t.Month()), t.Day(), t.Year()%100)
	case kDateISO:
		return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
	case kDateTimeISO:
		return fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d", t.Year(), int(t.Month()), t.Day(), h, m, s)
	case kDateTimeISOT:
		return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", t.Year(), int(t.Month()), t.Day(), h, m, s)
	case kDateTimeUS:
		hh := h % 12
		if hh == 0 {
			hh = 12
		}
		suffix := "AM"
		if h >= 12 {
			suffix = "PM"
		}
		return fmt.Sprintf("%02d/%02d/%02d %02d:%02d %s", int(t.Month()), t.Day(), t.Year()%100, hh, m, suffix)
	case kTime:
		return ampm()
	case kElapsed:
		return fmt.Sprintf("%02d:%02d:%02d", int(day)*24+h, m, s)
	case kTimeFraction, kText, kGeneral, kBool, kPercent, kCurrency, kScientific:
	}
	return fmt.Sprintf("%02d:%02d:%02d.%02d", h, m, s, c)
}
