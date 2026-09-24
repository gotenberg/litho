package xsd

import "strings"

// temporalKind names one of the eight date/time primitives of XML
// Schema Part 2, 3.2.7 to 3.2.14. They share the dateTime value model
// and differ only in which fields their literals carry.
type temporalKind int

const (
	kindDateTime temporalKind = iota
	kindTime
	kindDate
	kindGYearMonth
	kindGYear
	kindGMonthDay
	kindGDay
	kindGMonth
)

// temporalKindOf maps a built-in local name onto its [temporalKind].
func temporalKindOf(local string) (temporalKind, bool) {
	switch local {
	case "dateTime":
		return kindDateTime, true
	case "time":
		return kindTime, true
	case "date":
		return kindDate, true
	case "gYearMonth":
		return kindGYearMonth, true
	case "gYear":
		return kindGYear, true
	case "gMonthDay":
		return kindGMonthDay, true
	case "gDay":
		return kindGDay, true
	case "gMonth":
		return kindGMonth, true
	}
	return 0, false
}

// form names the lexical form of k for diagnostics.
func (k temporalKind) form() string {
	switch k {
	case kindDateTime:
		return "-?CCYY-MM-DDThh:mm:ss"
	case kindTime:
		return "hh:mm:ss"
	case kindDate:
		return "-?CCYY-MM-DD"
	case kindGYearMonth:
		return "-?CCYY-MM"
	case kindGYear:
		return "-?CCYY"
	case kindGMonthDay:
		return "--MM-DD"
	case kindGDay:
		return "---DD"
	case kindGMonth:
		return "--MM"
	}
	return ""
}

// maxTemporalDigits caps the digits of a year that litho places on a
// timeline, and of each integer component of a duration. It is an
// anti-overflow guard, not a limit XML Schema imposes: Part 2, 3.2.7.1
// and 3.2.6.1 leave both unbounded. The conformance note of 3.2.6,
// which 3.2.7 and 3.2.9 to 3.2.11 extend to their years, lets a
// processor document a limit on year digits if it supports at least
// four.
//
// Nine digits keep every timeline computation inside int64. A year
// below 10^9 lies about 3.2e16 seconds from the origin, and adding a
// duration whose six components each stay below 10^9 moves it by less
// than 3.5e16 more, against the 9.2e18 int64 holds. Real documents use
// four-digit years. A literal beyond the cap is still a literal, so the
// lexical checks accept it and a range facet passes it unjudged rather
// than guess an order.
const maxTemporalDigits = 9

const (
	secondsPerDay = 86400

	// timezoneWindow is the span of Part 2, 3.2.7.4 within which a
	// timezoned and a non-timezoned value are incomparable: fourteen
	// hours, the largest timezone magnitude 3.2.7.3 admits.
	timezoneWindow = 14 * 3600

	// referenceYear and referenceMonth fill the fields a left-truncated
	// literal omits. Per 3.2.12 gMonthDay values compare "in an
	// arbitrary leap year", and per 3.2.13 gDay values "in an arbitrary
	// month that has 31 days". 1972 and January satisfy both, and
	// time (3.2.8: "using an arbitrary date") and gMonth leave the
	// choice free.
	referenceYear  = 1972
	referenceMonth = 1
)

// temporalValue is a parsed date/time literal. The fields a kind's
// literal omits hold the starting instant of its period on the
// reference date: per 3.2.10 to 3.2.14 the truncated kinds order by
// their starting instants.
type temporalValue struct {
	// year is astronomical. Version 1.0 has no year 0 (3.2.7, Note), so
	// the lexical -0001, 1 BCE, is year 0 and -0002 is year -1.
	year                             int64
	month, day, hour, minute, second int
	// frac holds the fractional-second digits with trailing zeros
	// trimmed, so equal fractions compare equal as strings.
	frac      string
	timezoned bool
	// offset is the timezone offset in minutes, zero when not timezoned.
	offset int
	// beyondLimit marks a year longer than maxTemporalDigits.
	beyondLimit bool
}

// parseTemporal parses value as a literal of kind. It returns a
// non-empty reason when value is not one.
//
// The lexical spaces are those of Part 2: dateTime '-'? yyyy '-' mm
// '-' dd 'T' hh ':' mm ':' ss ('.' s+)? zzzzzz? (3.2.7.1), its left
// truncation for time (3.2.8.1), its right truncations for date,
// gYearMonth and gYear (3.2.9.1, 3.2.10.1, 3.2.11.1), and --MM-DD,
// ---DD and --MM (3.2.12.1, 3.2.13.1, 3.2.14.1). Each takes an
// optional timezone, written as 3.2.7.3 describes.
//
// # Corpus evidence
//
//   - All corpora: the 159 files that embed XML hold no element typed
//     xs:date, xs:dateTime or xs:time, so the lexical checks built on
//     this parser leave every XSD verdict unchanged.
func parseTemporal(value string, kind temporalKind) (temporalValue, string) {
	tv := temporalValue{year: referenceYear, month: referenceMonth, day: 1}
	leap := true // The reference year, 1972, is a leap year.

	body, tz := splitTimezone(value)
	c := lexCursor{s: body, ok: true}
	switch kind {
	case kindDateTime, kindDate, kindGYearMonth, kindGYear:
		var reason string
		if leap, reason = c.year(&tv); reason != "" {
			return tv, reason
		}
		if kind != kindGYear {
			c.literal("-")
			tv.month = c.twoDigits()
		}
		if kind == kindDateTime || kind == kindDate {
			c.literal("-")
			tv.day = c.twoDigits()
		}
	case kindGMonthDay:
		c.literal("--")
		tv.month = c.twoDigits()
		c.literal("-")
		tv.day = c.twoDigits()
	case kindGDay:
		c.literal("---")
		tv.day = c.twoDigits()
	case kindGMonth:
		c.literal("--")
		tv.month = c.twoDigits()
	case kindTime:
	}
	if kind == kindDateTime {
		c.literal("T")
	}
	if kind == kindDateTime || kind == kindTime {
		c.timeOfDay(&tv)
	}
	if !c.done() {
		return tv, "expected " + kind.form() + " lexical form"
	}

	// Per 3.2.7, each field is limited by the next-higher one: the day
	// cannot be 29 in February 2002.
	switch {
	case tv.month < 1 || tv.month > 12:
		return tv, "month out of range"
	case tv.day < 1 || tv.day > daysInMonth(tv.month, leap):
		return tv, "day out of range for its month"
	case tv.hour == 24 && (tv.minute != 0 || tv.second != 0 || tv.frac != ""):
		// Per 3.2.7.1, hour 24 is allowed only as 24:00:00.
		return tv, "hour 24 with non-zero minutes or seconds"
	case tv.hour > 24 || tv.minute > 59 || tv.second > 60:
		// A second of 60 is a leap second, which Appendix E.1 carries
		// into the next minute.
		return tv, "hour, minute or second out of range"
	}
	if kind == kindTime && tv.hour == 24 {
		// Per 3.2.8.2, midnight has the one value 00:00:00. A dateTime
		// keeps hour 24, which 3.2.7.1 makes the first instant of the
		// next day, and the timeline arithmetic carries it there.
		tv.hour = 0
	}

	if tz != "" {
		offset, ok := parseTimezoneOffset(tz)
		if !ok {
			return tv, "timezone offset out of range"
		}
		tv.timezoned, tv.offset = true, offset
	}
	return tv, ""
}

// parseTemporalValue adapts [parseTemporal] to [compareRange].
func parseTemporalValue(s string, kind temporalKind) (temporalValue, rangeStatus) {
	tv, reason := parseTemporal(s, kind)
	switch {
	case reason != "":
		return tv, rangeBadValue
	case tv.beyondLimit:
		return tv, rangeBeyondLimit
	}
	return tv, rangeCompared
}

// parseTimezoneOffset reads a suffix [splitTimezone] split off. Per
// Part 2, 3.2.7.3 it is Z or (+|-)hh:mm, the hour magnitude at most 14,
// the minutes at most 59, and the minutes 00 when the hour is 14.
func parseTimezoneOffset(tz string) (int, bool) {
	if tz == "Z" {
		return 0, true
	}
	if len(tz) != len("+hh:mm") || !allDigits(tz[1:3]) || tz[3] != ':' || !allDigits(tz[4:6]) {
		return 0, false
	}
	hours := int(tz[1]-'0')*10 + int(tz[2]-'0')
	minutes := int(tz[4]-'0')*10 + int(tz[5]-'0')
	if hours > 14 || minutes > 59 || (hours == 14 && minutes != 0) {
		return 0, false
	}
	offset := hours*60 + minutes
	if tz[0] == '-' {
		offset = -offset
	}
	return offset, true
}

// daysInMonth returns the length of month in a year that is leap or
// not, per the Gregorian rule Appendix E's maximumDayInMonthFor states.
func daysInMonth(month int, leap bool) int {
	switch month {
	case 4, 6, 9, 11:
		return 30
	case 2:
		if leap {
			return 29
		}
		return 28
	}
	return 31
}

// lexCursor walks a literal left to right. The first mismatch clears
// ok and turns every later step into a no-op, so callers check ok once.
type lexCursor struct {
	s  string
	ok bool
}

func (c *lexCursor) literal(prefix string) {
	if c.ok && strings.HasPrefix(c.s, prefix) {
		c.s = c.s[len(prefix):]
		return
	}
	c.ok = false
}

func (c *lexCursor) twoDigits() int {
	if !c.ok || len(c.s) < 2 || !allDigits(c.s[:2]) {
		c.ok = false
		return 0
	}
	n := int(c.s[0]-'0')*10 + int(c.s[1]-'0')
	c.s = c.s[2:]
	return n
}

// digits consumes the longest run of ASCII digits, possibly empty.
func (c *lexCursor) digits() string {
	if !c.ok {
		return ""
	}
	i := 0
	for i < len(c.s) && c.s[i] >= '0' && c.s[i] <= '9' {
		i++
	}
	run := c.s[:i]
	c.s = c.s[i:]
	return run
}

func (c *lexCursor) done() bool { return c.ok && c.s == "" }

// year consumes '-'? yyyy into tv and reports whether the year is leap.
// A non-empty reason names a year numeral the shape check alone would
// accept. Per Part 2, 3.2.7.1 the numeral has four or more digits, no
// leading zero when longer than four, takes no plus sign, and is never
// 0000 (also D.3.2).
func (c *lexCursor) year(tv *temporalValue) (bool, string) {
	negative := c.ok && strings.HasPrefix(c.s, "-")
	if negative {
		c.s = c.s[1:]
	}
	run := c.digits()
	switch {
	case len(run) < len("yyyy"):
		c.ok = false
		return false, ""
	case len(run) > len("yyyy") && run[0] == '0':
		return false, "year with more than four digits has a leading zero"
	case run == "0000":
		return false, "year 0000 is not allowed"
	}

	// The last four digits fix the year modulo 10000, a multiple of 400,
	// and so its leap status even beyond the cap. Leap years follow the
	// proleptic Gregorian rule on the astronomical year, under which
	// 1 BCE is leap, as the 3.2.7 Note says of it.
	last4 := digitsValue(run[len(run)-4:])
	mod400 := last4 % 400
	if negative {
		mod400 = floorMod(1-last4, 400)
	}
	leap := mod400%4 == 0 && (mod400%100 != 0 || mod400 == 0)

	if len(run) > maxTemporalDigits {
		tv.beyondLimit = true
		return leap, ""
	}
	tv.year = digitsValue(run)
	if negative {
		tv.year = 1 - tv.year
	}
	return leap, ""
}

// timeOfDay consumes hh ':' mm ':' ss ('.' s+)? into tv.
func (c *lexCursor) timeOfDay(tv *temporalValue) {
	tv.hour = c.twoDigits()
	c.literal(":")
	tv.minute = c.twoDigits()
	c.literal(":")
	tv.second = c.twoDigits()
	if c.ok && strings.HasPrefix(c.s, ".") {
		c.s = c.s[1:]
		run := c.digits()
		if run == "" {
			// Per 3.2.7.1 the period is followed by s+, one digit or more.
			c.ok = false
		}
		tv.frac = strings.TrimRight(run, "0")
	}
}

// digitsValue returns the value of an all-digit string no longer than
// [maxTemporalDigits], so the result cannot overflow.
func digitsValue(run string) int64 {
	var n int64
	for i := 0; i < len(run); i++ {
		n = n*10 + int64(run[i]-'0')
	}
	return n
}

// instant is a point on a timeline: whole seconds plus a non-negative
// fraction whose digits, trailing zeros trimmed, compare as strings.
type instant struct {
	sec  int64
	frac string
}

func (a instant) cmp(b instant) int {
	switch {
	case a.sec < b.sec:
		return -1
	case a.sec > b.sec:
		return 1
	}
	return strings.Compare(a.frac, b.frac)
}

func (a instant) shift(sec int64) instant { return instant{sec: a.sec + sec, frac: a.frac} }

// timeline places tv on its timeline, the timeOnTimeline of Part 2,
// 3.2.7, converted to UTC for a timezoned value. The origin is
// 1970-01-01 rather than 0001-01-01, which 3.2.7 allows: any
// interpretation isomorphic to its timeline is acceptable.
func (tv temporalValue) timeline() instant {
	sec := daysFromCivil(tv.year, tv.month, tv.day)*secondsPerDay +
		int64(tv.hour)*3600 + int64(tv.minute)*60 + int64(tv.second) -
		int64(tv.offset)*60
	return instant{sec: sec, frac: tv.frac}
}

// compareTemporal orders two values of one date/time kind per Part 2,
// 3.2.7.4. Values on one timeline, both timezoned or both not, order by
// their position on it. Across the two timelines they order only when
// more than fourteen hours apart, and are incomparable otherwise: a
// non-timezoned value may carry any offset from +14:00 to -14:00.
func compareTemporal(a, b temporalValue) order {
	ta, tb := a.timeline(), b.timeline()
	if a.timezoned == b.timezoned {
		return orderOf(ta.cmp(tb))
	}
	switch {
	case ta.cmp(tb.shift(timezoneWindow)) > 0:
		return orderGreater
	case ta.cmp(tb.shift(-timezoneWindow)) < 0:
		return orderLess
	}
	return orderIncomparable
}

// daysFromCivil counts the days from 1970-01-01 to the proleptic
// Gregorian date y-m-d, y astronomical. The closed form replaces the
// month-by-month loop of Appendix E, whose iteration count a crafted
// duration would otherwise control.
func daysFromCivil(y int64, m, d int) int64 {
	if m <= 2 {
		y--
	}
	era := floorDiv(y, 400)
	yearOfEra := y - era*400
	dayOfYear := (153*int64((m+9)%12)+2)/5 + int64(d) - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear
	return era*146097 + dayOfEra - 719468
}

// floorDiv is the fQuotient of Appendix E: the greatest integer not
// above a/b, for b > 0.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b < 0 {
		q--
	}
	return q
}

// floorMod is the modulo of Appendix E, a - floorDiv(a, b)*b, for b > 0.
func floorMod(a, b int64) int64 { return a - floorDiv(a, b)*b }

// durationValue is a parsed duration literal (Part 2, 3.2.6.1). The
// sign applies to every component.
type durationValue struct {
	negative                                     bool
	years, months, days, hours, minutes, seconds int64
	// frac holds the fractional-second digits with trailing zeros trimmed.
	frac string
	// beyondLimit marks a component longer than maxTemporalDigits.
	beyondLimit bool
}

// parseDuration parses value as a duration literal. It returns a
// non-empty reason when value is not one. Per Part 2, 3.2.6.1 the form
// is '-'? P nY nM nD T nH nM nS: every component optional but one
// present, the designators in that order, T present exactly when a
// time component follows, and only the seconds taking a fraction, with
// a digit after the period.
func parseDuration(value string) (durationValue, string) {
	var dv durationValue
	const reasonForm = "expected -?PnYnMnDTnHnMnS lexical form"
	s := value
	if rest, ok := strings.CutPrefix(s, "-"); ok {
		dv.negative, s = true, rest
	}
	body, ok := strings.CutPrefix(s, "P")
	if !ok {
		return dv, reasonForm
	}
	datePart, timePart, hasTime := strings.Cut(body, "T")
	switch {
	case datePart == "" && timePart == "":
		return dv, "no duration component"
	case hasTime && timePart == "":
		return dv, "T with no time component"
	}
	if !dv.scanComponents(datePart, "YMD", [3]*int64{&dv.years, &dv.months, &dv.days}) ||
		!dv.scanComponents(timePart, "HMS", [3]*int64{&dv.hours, &dv.minutes, &dv.seconds}) {
		return dv, reasonForm
	}
	return dv, ""
}

// scanComponents consumes a run of nD components whose designators
// appear in order in designators, storing each into the matching
// target. Only the last designator, the seconds, takes a fraction.
func (dv *durationValue) scanComponents(part, designators string, targets [3]*int64) bool {
	next := 0
	for part != "" {
		c := lexCursor{s: part, ok: true}
		run := c.digits()
		frac := ""
		if strings.HasPrefix(c.s, ".") {
			c.s = c.s[1:]
			if frac = c.digits(); frac == "" {
				return false
			}
		}
		if run == "" || c.s == "" {
			return false
		}
		i := strings.IndexByte(designators[next:], c.s[0])
		if i < 0 {
			return false
		}
		i += next
		if frac != "" {
			if designators[i] != 'S' {
				return false
			}
			dv.frac = strings.TrimRight(frac, "0")
		}
		if run = strings.TrimLeft(run, "0"); len(run) > maxTemporalDigits {
			dv.beyondLimit = true
		} else {
			*targets[i] = digitsValue(run)
		}
		next = i + 1
		part = c.s[1:]
	}
	return true
}

// parseDurationValue adapts [parseDuration] to [compareRange].
func parseDurationValue(s string) (durationValue, rangeStatus) {
	dv, reason := parseDuration(s)
	switch {
	case reason != "":
		return dv, rangeBadValue
	case dv.beyondLimit:
		return dv, rangeBeyondLimit
	}
	return dv, rangeCompared
}

// addTo returns the instant s + d for s = year-month-01T00:00:00Z, per
// Part 2, Appendix E: the year and month components first, then the day
// pinned into the resulting month (a no-op, as every reference of
// 3.2.6.2 starts on day 1), then the day and time components, whose
// carries into the month and year the day count absorbs.
func (d durationValue) addTo(year int64, month int) instant {
	sign := int64(1)
	if d.negative {
		sign = -1
	}
	months := int64(month-1) + sign*d.months
	endYear := year + sign*d.years + floorDiv(months, 12)
	endMonth := int(floorMod(months, 12)) + 1
	sec := daysFromCivil(endYear, endMonth, 1)*secondsPerDay +
		sign*(d.days*secondsPerDay+d.hours*3600+d.minutes*60+d.seconds)
	frac := d.frac
	if d.negative && frac != "" {
		// -(n + 0.f) is -(n + 1) + (1 - 0.f): keeping the fraction
		// non-negative lets instants compare field by field.
		sec--
		frac = complementFraction(frac)
	}
	return instant{sec: sec, frac: frac}
}

// complementFraction returns the digits of 1 - 0.f for a fraction f
// with no trailing zero. That last digit is non-zero, so no borrow
// crosses it and the result keeps a non-zero last digit.
func complementFraction(f string) string {
	b := []byte(f)
	last := len(b) - 1
	for i := range last {
		b[i] = '9' - (b[i] - '0')
	}
	b[last] = '0' + 10 - (b[last] - '0')
	return string(b)
}

// compareDuration orders two durations per Part 2, 3.2.6.2: x < y when
// s + x < s + y for each of four reference dateTimes s, and the pair is
// incomparable when the references disagree, as for P1M and P30D.
// Version 1.0 defines only the strict order; when all four sums
// coincide the durations are equal, as XML Schema 1.1 Part 2, 3.3.6.1
// states, so P1D equals PT24H.
func compareDuration(a, b durationValue) order {
	references := [...]struct {
		year  int64
		month int
	}{{1696, 9}, {1697, 2}, {1903, 3}, {1903, 7}}
	first := a.addTo(references[0].year, references[0].month).cmp(b.addTo(references[0].year, references[0].month))
	for _, ref := range references[1:] {
		if a.addTo(ref.year, ref.month).cmp(b.addTo(ref.year, ref.month)) != first {
			return orderIncomparable
		}
	}
	return orderOf(first)
}
