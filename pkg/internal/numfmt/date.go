package numfmt

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// maxSerialDays bounds the serial numbers a date section formats, about
// 27,000 years either side of the null date. Anti-DoS guard, not
// spec-mandated: Calc's dates end in the year 32767.
const maxSerialDays = 1e7

// nullDate is Calc's day 0 (ECMA-376 Part 1, 18.17.4.1, the 1900 date
// system as Calc counts it, without the 1900 leap day).
var nullDate = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

var monthNames = [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

var dayNames = [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// approxFloor returns the floor of v rounded to 15 significant digits,
// which absorbs the binary error of a decimal value (rtl::math::approxFloor).
func approxFloor(v float64) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 15, 64), 64)
	if err != nil {
		return math.Floor(v)
	}
	return math.Floor(r)
}

// date formats the serial number x by a date or time section.
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
			frac = max(frac, min(t.n, 9))
		case tLit, tDigit, tDot, tComma, tPercent, tExp, tSlash, tAt, tFill, tGeneral:
		}
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
	scale := math.Pow(10, float64(frac))
	// A time that shows whole seconds is cut to the second, as a clock
	// reads, the value first rounded to 15 significant digits; one that
	// shows fractions of a second, or no seconds, is rounded to the
	// precision it keeps (SvNumberformat::ImpGetTimeOutput).
	//
	// # Corpus evidence
	//
	//   - libreoffice/ods: sc/tdf125099.ods, 03:23:59.999999665 in [HH]:MM:SS prints
	//     03:23:59, and 11:23:59.999999665 in HH:MM prints 11:24.
	whole := func(v float64) float64 {
		if seconds && frac == 0 {
			return approxFloor(v)
		}
		return math.Floor(v + 0.5)
	}
	var days, total int64
	var sub float64
	if elapsed {
		t := whole(x*86400*scale) / scale
		total = int64(t)
		sub = t - float64(total)
	} else {
		d := math.Floor(x)
		t := whole((x-d)*86400*scale) / scale
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
	day := nullDate.AddDate(0, 0, int(days)) // fits int: |x| is at most maxSerialDays, checked above
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
			if pm {
				b.WriteString("PM")
			} else {
				b.WriteString("AM")
			}
		case tAP:
			if pm {
				b.WriteByte('p')
			} else {
				b.WriteByte('a')
			}
		case tSecFrac:
			ds := strconv.FormatFloat(sub, 'f', min(t.n, 9), 64)
			if i := strings.IndexByte(ds, '.'); i >= 0 {
				b.WriteString(ds[i:])
			} else {
				b.WriteString("." + strings.Repeat("0", min(t.n, 9)))
			}
		case tDate:
			switch t.key {
			case kYear:
				y := int64(day.Year())
				if t.n <= 2 {
					pad(y%100, 2)
				} else {
					pad(y, 4)
				}
			case kMonth:
				mo := day.Month()
				switch t.n {
				case 1:
					pad(int64(mo), 1)
				case 2:
					pad(int64(mo), 2)
				case 3:
					b.WriteString(monthNames[mo-1][:3])
				case 4:
					b.WriteString(monthNames[mo-1])
				default:
					b.WriteString(monthNames[mo-1][:1])
				}
			case kDay:
				switch t.n {
				case 1, 2:
					pad(int64(day.Day()), t.n)
				case 3:
					b.WriteString(dayNames[day.Weekday()][:3])
				default:
					b.WriteString(dayNames[day.Weekday()])
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
				pad(total/3600, min(t.n, 9))
			case kElapsedMinute:
				elapsedMinute = true
				pad(total/60, min(t.n, 9))
			case kElapsedSecond:
				pad(total, min(t.n, 9))
			}
		case tDigit, tDot, tComma, tPercent, tExp, tSlash, tAt, tGeneral:
		}
	}
	r.Text = b.String()
	return r
}
