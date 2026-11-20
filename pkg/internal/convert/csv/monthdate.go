package csv

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// monthNamer is what a reader of dates with a month name needs of a
// locale: its month names and the orders and separators of its dates.
type monthNamer struct {
	// names are the month names in the order Calc tries them, each with
	// its month (1 to 12), negative for an abbreviation, which a full stop
	// may follow.
	names []monthName
	// order and longOrder are the orders of day, month and year of the
	// short and the long date ("DMY", "MDY", "YMD", "YDM").
	order, longOrder string
	// dateSep and decimal are the date and decimal separators; longDaySep
	// what a long date writes after its day.
	dateSep, decimal, longDaySep string
}

// monthName is a month name and its month, negative for an
// abbreviation.
type monthName struct {
	name  string
	month int
}

// newMonthNamer returns the namer of a locale of language lang whose
// month names are months, with genitive and partitive the names of the
// other cases, nil when the same: Calc tries, month after month, the
// genitive full and abbreviated names, the partitive ones, then the
// nominative ones (zforfind.cxx, GetMonth), and takes MRZ for MÄR in
// German and SEP for SEPT, and the reverse, in every language.
func newMonthNamer(lang string, months *[12][2]string, genitive, partitive *[12][2]string, order, longOrder, dateSep, decimal, longDaySep string) *monthNamer {
	n := &monthNamer{order: order, longOrder: longOrder, dateSep: dateSep, decimal: decimal, longDaySep: longDaySep}
	for i := range 12 {
		for _, l := range []*[12][2]string{genitive, partitive, months} {
			if l == nil {
				continue
			}
			n.names = append(n.names, monthName{l[i][1], i + 1}, monthName{l[i][0], -(i + 1)})
		}
		abbr := strings.ToUpper(months[i][0])
		switch {
		case i == 2 && lang == "de" && abbr == "MÄR":
			n.names = append(n.names, monthName{"MRZ", -3})
		case i == 2 && lang == "de" && abbr == "MRZ":
			n.names = append(n.names, monthName{"MÄR", -3})
		case i == 8 && abbr == "SEPT":
			n.names = append(n.names, monthName{"SEP", -9})
		case i == 8 && abbr == "SEP":
			n.names = append(n.names, monthName{"SEPT", -9})
		}
	}
	return n
}

// match returns the month whose name s starts with as a word, the first
// Calc tries, and the name's length in bytes; 0 for none. A name is a
// word when what follows it is no letter and no ASCII digit
// (StringContainsWord).
func (n *monthNamer) match(s string) (int, int) {
	for _, mn := range n.names {
		if mn.name == "" {
			continue
		}
		l := prefixLen(s, utf8.RuneCountInString(mn.name))
		if l < 0 || !strings.EqualFold(s[:l], mn.name) {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(s[l:]); l < len(s) && (unicode.IsLetter(r) || r >= '0' && r <= '9') {
			continue
		}
		return mn.month, l
	}
	return 0, 0
}

// prefixLen returns the length in bytes of the first n runes of s, -1
// when s holds fewer.
func prefixLen(s string, n int) int {
	i := 0
	for range n {
		if i >= len(s) {
			return -1
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return i
}

// monthPart is a part of a date with a month name: a number, or the
// month, with the separator before it.
type monthPart struct {
	sep   string
	num   string
	month int
	// dot reports a month name that ends with a full stop.
	dot bool
}

// date reads a date with a month name as Calc's input scan does
// (svl/source/numbers/zforfind.cxx, ImpSvNumberInputScan: ScanStartString,
// ScanMidString, GetDateRef), for one month name and at most two numbers.
// Where the month stands decides the rest:
//
//   - first ("Jan 13 2026", "Jan 2026"): a day then a year; one number is
//     a day of 1 to 32 under a month-first or year-first short date, else
//     a year; a comma after the day only where the long date writes one
//     there, month first;
//   - between two numbers ("13 Jan 2026", "2026 Jan 13"): with a hyphen
//     on both sides, day first unless the first number has three digits or
//     more; else by the long date's order, day first or year first, the
//     short date's deciding when the long one puts the month first, the
//     date being none when both do;
//   - last, after one number ("13 Jan"): a day under a day-first short
//     date, a year under a year-first one, none under a month-first one.
//
// A month alone, a name after the year and the day, and a slash beside the
// name are not dates. A missing year is year, a missing day the first.
//
// # Corpus evidence
//
//   - probe, csv: 26 forms under 37 locales (TestDetect_CalcNamedDates):
//     13 Jan 2026 is text under en-US, whose long and short dates put the
//     month first, and 13.01.26 under de-DE; 13-Jan-2026 is a date under
//     both; 2026 Jan 13 is a date under ko-KR, hu-HU, lt-LT, sv-SE,
//     en-ZA and fr-CA, whose long or short dates put the year first; Jan
//     13, 2026 is a date under en-US alone; Jan 13 is January 13 under
//     en-US and January 1, 2013 under de-DE.
func (n *monthNamer) date(s string, year int, day *float64) bool {
	parts, ok := n.parts(s)
	if !ok {
		return false
	}
	at, nums := -1, []monthPart{}
	for i, p := range parts {
		if p.month != 0 {
			at = i
			continue
		}
		nums = append(nums, p)
	}
	if at < 0 || len(nums) == 0 || len(nums) > 2 {
		return false
	}
	month := parts[at].month
	if month < 0 {
		month = -month
	}
	for _, p := range parts {
		if strings.Contains(p.sep, "/") {
			return false
		}
	}
	d, y, yDigits := 1, year, 4
	setDay := func(p monthPart) bool {
		v, ok := atoi(p.num, 2)
		d = v
		return ok
	}
	setYear := func(p monthPart) bool {
		v, ok := atoi(p.num, 4)
		y, yDigits = v, len(p.num)
		return ok
	}
	ok = true
	switch {
	case at == 0 && len(nums) == 1:
		if !n.afterMonth(parts[0], parts[1].sep, "-") {
			return false
		}
		v, isNum := atoi(nums[0].num, 4)
		switch {
		case isNum && (n.order == "MDY" || n.order == "YMD") && len(nums[0].num) <= 2 && v > 0 && v <= 32:
			d = v
		case n.order == "DMY" || n.order == "MDY" || n.order == "YMD":
			ok = setYear(nums[0])
		default:
			return false
		}
	case at == 0:
		if !n.afterMonth(parts[0], parts[1].sep, "") || !n.daySep(parts[2].sep) {
			return false
		}
		ok = setDay(nums[0]) && setYear(nums[1])
	case at == 1 && len(nums) == 2:
		before, after := parts[1].sep, parts[2].sep
		if !n.beforeMonth(before) || !n.afterMonth(parts[1], after, "-") {
			return false
		}
		order := ""
		if before == "-" && after == "-" {
			order = dashOrder(nums[0].num, nums[1].num)
		}
		if order == "" {
			order = n.middleOrder(nums[0].num, nums[1].num)
		}
		switch order {
		case "DMY":
			ok = setDay(nums[0]) && setYear(nums[1])
		case "YMD":
			ok = setYear(nums[0]) && setDay(nums[1])
		default:
			return false
		}
	case at == 1:
		if !n.beforeMonth(parts[1].sep) {
			return false
		}
		switch n.order {
		case "DMY":
			ok = setDay(nums[0])
		case "YMD":
			ok = setYear(nums[0])
		default:
			return false
		}
	default:
		return false
	}
	if !ok || yDigits == 3 || d == 0 {
		return false
	}
	v, ok := serial(fullYear(y, yDigits), month, d)
	if ok {
		*day = v
	}
	return ok
}

// parts splits s into its numbers and its month names, each with the
// separator before it; a full stop after an abbreviated name belongs to
// it. ok is false for a letter that starts no month name, for a separator
// at either end, and for two names.
func (n *monthNamer) parts(s string) ([]monthPart, bool) {
	var parts []monthPart
	sep := ""
	months := 0
	for i := 0; i < len(s); {
		if isDigit(s[i]) {
			j := i
			for j < len(s) && isDigit(s[j]) {
				j++
			}
			parts = append(parts, monthPart{sep: sep, num: s[i:j]})
			sep, i = "", j
			continue
		}
		if m, l := n.match(s[i:]); m != 0 {
			if len(parts) > 0 && sep == "" && parts[len(parts)-1].num != "" {
				// 13Jan: a name glued to a number is no date.
				return nil, false
			}
			months++
			i += l
			if m < 0 && i < len(s) && s[i] == '.' {
				// An abbreviation may take a full stop, one more if it
				// ends with one ("jan.." in nb-NO).
				i++
			}
			parts = append(parts, monthPart{sep: sep, month: m, dot: s[i-1] == '.'})
			sep = ""
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return nil, false
		}
		sep += s[i : i+size]
		i += size
	}
	if sep != "" || len(parts) == 0 || parts[0].sep != "" || months != 1 {
		return nil, false
	}
	return parts, true
}

// blankOr reports a separator of blanks, or exactly alt when alt is set.
func (n *monthNamer) blankOr(sep, alt string) bool {
	return strings.Trim(sep, " ") == "" && sep != "" || alt != "" && sep == alt
}

// afterMonth reports the separator a month name m may take before a
// number: blanks, alt, a hyphen where it separates dates, or nothing after
// a name that ends with a full stop ("13.Jan.2026").
func (n *monthNamer) afterMonth(m monthPart, sep, alt string) bool {
	return n.blankOr(sep, alt) || sep == "-" && n.dateSep == "-" || sep == "" && m.dot
}

// daySep reports the separator a date that starts with its month may
// write between the day and the year: blanks, the long date's own after
// the day where the long date puts the month first ("Jan 13, 2026"), or
// the date separator when it is a hyphen ("Jan-13-2026").
func (n *monthNamer) daySep(sep string) bool {
	switch {
	case n.blankOr(sep, ""):
		return true
	case n.longOrder == "MDY" && strings.TrimRight(sep, " ") == strings.TrimRight(n.longDaySep, " ") && n.longDaySep != "":
		return true
	case sep == "-" && n.dateSep == "-":
		return true
	}
	return false
}

// beforeMonth reports the separator a number may take before a month
// name: blanks, a hyphen, a full stop where it separates dates or
// decimals, alone, or a date separator of a full stop and blanks
// ("13. Januar").
func (n *monthNamer) beforeMonth(sep string) bool {
	switch {
	case n.blankOr(sep, "-"):
		return true
	case sep == "." && (n.dateSep == "." || n.decimal == "."):
		return true
	case strings.TrimRight(sep, " ") == "." && n.dateSep == ".":
		return true
	}
	return false
}

// dashOrder returns the order of a date written number, hyphen, month,
// hyphen, number (zforfind.cxx, MayBeMonthDate): "DMY" when the first
// number may be a day and the second may not, "YMD" for the reverse, "DMY"
// when both may; "" when neither may. A number of three digits or more is
// a year, one of 1 to 31 may be a day.
func dashOrder(a, b string) string {
	mayDay := func(s string) bool {
		v, ok := atoi(s, 2)
		return ok && v >= 1 && v <= 31
	}
	da, db := mayDay(a), mayDay(b)
	switch {
	case da && !db, da && db:
		return "DMY"
	case !da && db:
		return "YMD"
	}
	return ""
}

// middleOrder returns the order of a date whose month stands between two
// numbers (zforfind.cxx, GetMiddleMonthLongDateOrder): the long date's
// order when it is day first or year first, else the short date's when it
// is; a day-first long order under a year-first short one turns year
// first when only the second number may be a day, and the reverse; ""
// when neither order serves.
func (n *monthNamer) middleOrder(a, b string) string {
	day := func(s string) bool {
		v, ok := atoi(s, 2)
		return ok && v <= 31 && v > 0
	}
	switch o := n.longOrder; {
	case o != "DMY" && o != "YMD":
		if n.order == "DMY" || n.order == "YMD" {
			return n.order
		}
		return ""
	case o == "DMY" && n.order == "YMD" && !day(a) && day(b):
		return "YMD"
	case o == "YMD" && n.order == "DMY" && !day(b) && day(a):
		return "DMY"
	default:
		return o
	}
}
