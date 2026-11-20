package numfmt

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/detmath"
)

// maxSerialDays bounds the serial numbers a date section formats, about
// 27,000 years either side of the null date. Anti-DoS guard, not
// spec-mandated: Calc's dates end in the year 32767.
const maxSerialDays = 1e7

// maxSecondDigits caps the digits of a fraction of a second shown: a
// nanosecond. Anti-DoS guard, not spec-mandated: a double holds a time of
// day to about ten microseconds.
const maxSecondDigits = 9

// maxElapsedDigits caps the width an elapsed time keyword ([hh], [mm],
// [ss]) pads its count to. Anti-DoS guard, not spec-mandated: a code
// writes two or three letters, and nine digits hold any count of hours
// within maxSerialDays.
const maxElapsedDigits = 9

// nullDate is Calc's day 0 (ECMA-376 Part 1, 18.17.4.1, the 1900 date
// system as Calc counts it, without the 1900 leap day).
var nullDate = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

var monthNames = [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

var dayNames = [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// The cases a month name takes (svl zformat.cxx, ImpUseMonthCase).
const (
	caseNominative = iota + 1
	caseGenitive
	casePartitive
)

// monthCase returns the case a section writes its month names in: the
// nominative when a literal other than a space or a comma follows a month
// name, or one that does not end with a space precedes it, or no day of
// the month is near it; the genitive when the day follows the month; the
// partitive when it precedes it (LibreOffice's rule, which a locale with
// genitive or partitive names follows).
func (s *section) monthCase() int {
	monthSeen, daySeen := false, false
	for i, t := range s.toks {
		if t.kind != tDate {
			continue
		}
		switch {
		case t.key == kDay && t.n <= 2:
			if monthSeen {
				return caseGenitive
			}
			daySeen = true
		case t.key == kMonth && t.n >= 3:
			next := i+1 < len(s.toks) && s.toks[i+1].kind == tLit && s.toks[i+1].text != "" &&
				s.toks[i+1].text[0] != ' ' && s.toks[i+1].text[0] != ','
			prev := i > 0 && s.toks[i-1].kind == tLit && s.toks[i-1].text != "" &&
				s.toks[i-1].text[len(s.toks[i-1].text)-1] != ' '
			switch {
			case next || prev:
				return caseNominative
			case daySeen:
				return casePartitive
			}
			monthSeen = true
		}
	}
	return caseNominative
}

// monthName returns the name of month mo (1 to 12) a keyword of n letters
// writes, in the section's locale and case.
func (s *section) monthName(mo time.Month, n int) string {
	full, abbr := monthNames[mo-1], monthNames[mo-1][:3]
	if loc := s.loc; loc != nil {
		names := loc.months
		switch s.monthCase() {
		case casePartitive:
			if loc.partitive != nil {
				names = loc.partitive
				break
			}
			fallthrough
		case caseGenitive:
			if loc.genitive != nil {
				names = loc.genitive
			}
		}
		abbr, full = names[mo-1][0], names[mo-1][1]
	}
	switch n {
	case 3:
		return abbr
	case 4:
		return full
	}
	if s.loc != nil && s.loc.narrow != nil && s.loc.narrow[mo-1] != "" {
		return s.loc.narrow[mo-1]
	}
	// The narrow name is the full name's first character.
	r, _ := utf8.DecodeRuneInString(full)
	return string(r)
}

// time100Sep returns the separator of the fraction of a second.
func (s *section) time100Sep() string {
	if s.loc != nil {
		return s.loc.time100Sep
	}
	return "."
}

// dayName returns the name of weekday a keyword of n letters writes.
func (s *section) dayName(wd time.Weekday, n int) string {
	if loc := s.loc; loc != nil {
		if n == 3 {
			return loc.days[wd][0]
		}
		return loc.days[wd][1]
	}
	if n == 3 {
		return dayNames[wd][:3]
	}
	return dayNames[wd]
}

// excelLeapDay is the serial number of the 1900 date system's February
// 29, 1900, a day Excel counts and the calendar does not (ECMA-376 Part
// 1, 18.17.4.1).
const excelLeapDay = 60

// calendarDay returns the date of the serial day days: from Calc's null
// date, or under Excel's rules from January 0, 1900, February 29, 1900
// included, a serial number from 61 on reading as Calc's.
func calendarDay(days int64, excel bool) (int, time.Month, int, time.Weekday) {
	if excel && days >= 0 && days <= excelLeapDay {
		// Excel's day 1 is a Sunday, January 1, 1900.
		wd := time.Weekday((days + 6) % 7) // fits int: a remainder of 7, days is not negative
		switch days {
		case 0:
			return 1900, time.January, 0, wd
		case excelLeapDay:
			return 1900, time.February, 29, wd
		}
		d := time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(days)) // fits int: at most excelLeapDay
		return d.Year(), d.Month(), d.Day(), wd
	}
	d := nullDate.AddDate(0, 0, int(days)) // fits int: |days| is at most maxSerialDays, checked by the caller
	return d.Year(), d.Month(), d.Day(), d.Weekday()
}

// approxFloor returns the floor of v rounded to 15 significant digits,
// which absorbs the binary error of a decimal value (rtl::math::approxFloor).
func approxFloor(v float64) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 15, 64), 64)
	if err != nil {
		return math.Floor(v)
	}
	return math.Floor(r)
}

// date formats the serial number x by a date or time section. A time
// that shows whole seconds is cut to the second, one that shows fractions
// of a second or no seconds is rounded.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf125099.ods, 03:23:59.999999665 in [HH]:MM:SS
//     prints 03:23:59, and 11:23:59.999999665 in HH:MM prints 11:24.
func (s *section) date(x float64, first bool) Result {
	r := Result{Numeric: true}
	hasDate, elapsed, ampm, seconds := false, false, false, false
	frac := 0
	for _, t := range s.toks {
		switch t.kind {
		case tDate:
			switch t.key {
			case kYear, kMonth, kDay:
				hasDate = true
			case kElapsedHour, kElapsedMinute:
				elapsed = true
			case kElapsedSecond:
				elapsed, seconds = true, true
			case kSecond:
				seconds = true
			case kHour, kMinute:
			}
		case tAMPM, tAP:
			ampm = true
		case tSecFrac:
			frac = max(frac, min(t.n, maxSecondDigits))
		case tLit, tDigit, tDot, tComma, tPercent, tExp, tSlash, tAt, tFill, tGeneral:
		}
	}
	if s.excel && x < 0 {
		return Result{Numeric: true, Fill: '#', Hash: true}
	}
	neg := false
	if !hasDate && x < 0 {
		neg = first
		x = -x
	}
	if math.Abs(x) > maxSerialDays {
		r.Text = "###"
		return r
	}
	scale := detmath.Pow(10, float64(frac))
	// A time that shows whole seconds is cut to the second, as a clock
	// reads, the value first rounded to 15 significant digits; one that
	// shows fractions of a second, or no seconds, is rounded to the
	// precision it keeps (SvNumberformat::ImpGetTimeOutput).
	whole := func(v float64) float64 {
		if seconds && frac == 0 && !s.excel {
			return approxFloor(v)
		}
		return math.Floor(v + 0.5)
	}
	var days, total int64
	var sub float64
	if elapsed {
		t := whole(float64(x*86400*scale)) / scale
		total = int64(t)
		sub = t - float64(total)
	} else {
		d := math.Floor(x)
		t := whole(float64((x-d)*86400*scale)) / scale
		if t >= 86400 {
			d++
			t -= 86400
		}
		days, total = int64(d), int64(t)
		sub = t - float64(total)
	}
	if neg && total == 0 && sub == 0 {
		neg = false
	}
	year, month, mday, weekday := calendarDay(days, s.excel)
	hour, minute, sec := total/3600, total%3600/60, total%60
	if !elapsed {
		hour %= 24
	}
	pm := hour >= 12
	h12 := hour % 12
	if h12 == 0 {
		h12 = 12
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	pad := func(v int64, n int) {
		str := strconv.FormatInt(v, 10)
		for len(str) < n {
			str = "0" + str
		}
		b.WriteString(str)
	}
	elapsedHour := false
	elapsedMinute := false
	for _, t := range s.toks {
		switch t.kind {
		case tLit:
			b.WriteString(t.text)
		case tFill:
			r.setFill(t, b.Len())
		case tAMPM:
			am, pmText := "AM", "PM"
			if s.loc != nil {
				am, pmText = s.loc.am, s.loc.pm
			}
			if pm {
				b.WriteString(pmText)
			} else {
				b.WriteString(am)
			}
		case tAP:
			if pm {
				b.WriteByte('p')
			} else {
				b.WriteByte('a')
			}
		case tSecFrac:
			ds := strconv.FormatFloat(sub, 'f', min(t.n, maxSecondDigits), 64)
			if i := strings.IndexByte(ds, '.'); i >= 0 {
				b.WriteString(s.time100Sep() + ds[i+1:])
			} else {
				b.WriteString(s.time100Sep() + strings.Repeat("0", min(t.n, maxSecondDigits)))
			}
		case tDate:
			switch t.key {
			case kYear:
				y := int64(year)
				if t.n <= 2 {
					pad(y%100, 2)
				} else {
					pad(y, 4)
				}
			case kMonth:
				mo := month
				switch t.n {
				case 1:
					pad(int64(mo), 1)
				case 2:
					pad(int64(mo), 2)
				default:
					b.WriteString(s.monthName(mo, t.n))
				}
			case kDay:
				switch t.n {
				case 1, 2:
					pad(int64(mday), t.n)
				default:
					b.WriteString(s.dayName(weekday, t.n))
				}
			case kHour:
				h := hour
				if ampm {
					h = h12
				}
				pad(h, min(t.n, 2))
			case kMinute:
				mi := minute
				if elapsedMinute {
					mi = total / 60
				} else if !elapsedHour && elapsed {
					mi = total % 3600 / 60
				}
				pad(mi, min(t.n, 2))
			case kSecond:
				pad(sec, min(t.n, 2))
			case kElapsedHour:
				elapsedHour = true
				pad(total/3600, min(t.n, maxElapsedDigits))
			case kElapsedMinute:
				elapsedMinute = true
				pad(total/60, min(t.n, maxElapsedDigits))
			case kElapsedSecond:
				pad(total, min(t.n, maxElapsedDigits))
			}
		case tDigit, tDot, tComma, tPercent, tExp, tSlash, tAt, tGeneral:
		}
	}
	r.Text = b.String()
	return r
}
