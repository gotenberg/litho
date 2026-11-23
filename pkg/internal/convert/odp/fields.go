package odp

import (
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// fieldCode returns the format code a date or time field of data style
// name prints in: Impress shows a date or time field in one of its own
// formats, in the locale's words and order, and reads the data style
// only to choose which (ODF 1.3 Part 3, 7.3.2 and 7.3.3 leave a field's
// presentation to style:data-style-name; LibreOffice matches the style
// against the formats it writes for its fields). A style that matches
// none of them prints as itself.
//
// The formats, under the pinned image's locale, en-US: the numeric date
// of automatic order its short date (two-digit year), of fixed order
// with a four-digit year; the date of an abbreviated month name, of a
// full month name, and of the full day of the week too; the time of
// hours and minutes, of seconds too, each with AM/PM or not, hours of two
// digits.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/numfmt.odp prints dd.mm.yyyy of automatic order
//     11/12/26, of fixed order 11/12/2026, "d. MMM yyyy" Nov 12, 2026,
//     "d. MMMM yyyy" November 12, 2026, "NNNN, d. MMMM yyyy" Thursday,
//     November 12, 2026, HH:MM:SS AM/PM of either order 09:37:29 AM,
//     HH:MM 09:37 and HH:MM AM/PM 09:37 AM.
func (r *reader) fieldCode(scope odf.Scope, name string) string {
	el := r.data.Element(scope, name)
	if el == nil {
		return r.data.Code(scope, name)
	}
	if code, ok := impressFormat(el); ok {
		return code
	}
	return r.data.Code(scope, name)
}

// impressFormat returns the format code of the Impress field format the
// date or time style el matches ([reader.fieldCode]).
func impressFormat(el *odf.Element) (string, bool) {
	var weekday, day, month, year, hours, minutes, seconds, ampm, other bool
	var dayLong, monthLong, monthText, yearLong, weekdayLong bool
	for _, c := range el.Children {
		if c.IsText() || c.Name.Space != odf.NSNumber {
			continue
		}
		long := c.A(odf.NSNumber, "style") == "long"
		switch c.Name.Local {
		case "day-of-week":
			weekday, weekdayLong = true, long
		case "day":
			day, dayLong = true, long
		case "month":
			month, monthLong, monthText = true, long, c.A(odf.NSNumber, "textual") == "true"
		case "year":
			year, yearLong = true, long
		case "hours":
			hours = true
		case "minutes":
			minutes = true
		case "seconds":
			seconds = true
		case "am-pm":
			ampm = true
		case "text":
		default:
			other = true
		}
	}
	if other {
		return "", false
	}
	switch el.Name.Local {
	case "date-style":
		if hours || minutes || seconds || !day || !month || !year || !yearLong {
			return "", false
		}
		switch {
		case weekday && weekdayLong && monthText && monthLong:
			return "dddd, mmmm d, yyyy", true
		case weekday:
			return "", false
		case monthText && monthLong:
			return "mmmm d, yyyy", true
		case monthText:
			return "mmm d, yyyy", true
		case dayLong && monthLong && el.A(odf.NSNumber, "automatic-order") == "true":
			return "mm/dd/yy", true
		case dayLong && monthLong:
			return "mm/dd/yyyy", true
		}
	case "time-style":
		if !hours || !minutes {
			return "", false
		}
		code := "hh:mm"
		if seconds {
			code += ":ss"
		}
		if ampm {
			code += " AM/PM"
		}
		return code, true
	}
	return "", false
}
