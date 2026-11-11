package docx

import (
	"strconv"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// Default pictures of the date fields without a \@ switch.
//
// # Corpus evidence
//
//   - openxmlsdk/docx v2FxTestFiles/wordprocessing/field/simple-field:
//     Word caches DATE \* MERGEFORMAT as 2/15/2008.
//   - litho/docx fields-date-doc-info: Word caches a PRINTDATE of a
//     document never printed as 0/0/0000 0:00:00 AM.
//   - TIME's default is not measured: Word's en-US time, h:mm AM/PM.
const (
	defaultDatePicture      = "M/d/yyyy"
	defaultTimePicture      = "h:mm AM/PM"
	defaultPrintDatePicture = "M/d/yyyy h:mm:ss AM/PM"
)

// fieldLocked reports a field whose w:fldLock is on (17.16.18 fldChar,
// 17.16.19 fldSimple): its result is never updated.
func fieldLocked(e *ooxml.Element) bool {
	v, ok := e.Attr("fldLock")
	return ok && ooxml.OnOff(v, true)
}

// dateField returns what a date field shows on the conversion date,
// office.Options.PrintDate, formatted by its \@ picture or its default
// one; false without a conversion date and for any other field, which
// keeps its cached result. DATE and TIME (17.16.5.12 and 17.16.5.70) show
// the current date and time, the moment the document prints, and
// PRINTDATE (17.16.5.48) the moment it was last printed, which a
// conversion is. CREATEDATE and SAVEDATE (17.16.5.10 and 17.16.5.58) show
// when the document was created and saved, which a conversion does not
// change: they keep their cached result. A field with the \h or \s switch,
// a Hijri or Saka date, keeps its cached result too.
func (d *document) dateField(instr string) (string, bool) {
	if d.opts.PrintDate.IsZero() {
		return "", false
	}
	args := fieldArgs(instr)
	if len(args) == 0 {
		return "", false
	}
	var picture string
	switch strings.ToUpper(args[0]) {
	case "DATE":
		picture = defaultDatePicture
	case "TIME":
		picture = defaultTimePicture
	case "PRINTDATE":
		picture = defaultPrintDatePicture
	default:
		return "", false
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case `\h`, `\s`:
			return "", false
		case `\@`:
			if i+1 < len(args) {
				picture = args[i+1]
				i++
			}
		}
	}
	return formatPicture(d.opts.PrintDate, picture), true
}

// formatPicture formats t, read in its own location, by a date-time
// picture (17.16.4.1): d to dddd the day and its name, M to MMMM the month
// and its name, y and yy the year in two digits, yyy and more in four, h
// and hh the hour on a 12-hour clock, H and HH on a 24-hour one, m and mm
// the minutes, s and ss the seconds, AM/PM, am/pm, A/P and a/p the half of
// the day; text in single quotes as it stands, any other character too.
// Names are English.
func formatPicture(t time.Time, picture string) string {
	var b strings.Builder
	for i := 0; i < len(picture); {
		c := picture[i]
		n := 1
		for i+n < len(picture) && picture[i+n] == c {
			n++
		}
		switch c {
		case '\'':
			j := strings.IndexByte(picture[i+1:], '\'')
			if j < 0 {
				b.WriteString(picture[i+1:])
				return b.String()
			}
			b.WriteString(picture[i+1 : i+1+j])
			i += j + 2
			continue
		case 'd':
			switch {
			case n >= 4:
				b.WriteString(t.Weekday().String())
			case n == 3:
				b.WriteString(t.Weekday().String()[:3])
			default:
				pad(&b, t.Day(), n)
			}
		case 'M':
			switch {
			case n >= 4:
				b.WriteString(t.Month().String())
			case n == 3:
				b.WriteString(t.Month().String()[:3])
			default:
				pad(&b, int(t.Month()), n)
			}
		case 'y', 'Y':
			if n <= 2 {
				pad(&b, t.Year()%100, 2)
			} else {
				pad(&b, t.Year(), 4)
			}
		case 'h':
			h := t.Hour() % 12
			if h == 0 {
				h = 12
			}
			pad(&b, h, min(n, 2))
		case 'H':
			pad(&b, t.Hour(), min(n, 2))
		case 'm':
			pad(&b, t.Minute(), min(n, 2))
		case 's', 'S':
			pad(&b, t.Second(), min(n, 2))
		case 'A', 'a':
			if s, used, ok := half(t, picture[i:]); ok {
				b.WriteString(s)
				i += used
				continue
			}
			b.WriteString(picture[i : i+n])
		default:
			b.WriteString(picture[i : i+n])
		}
		i += n
	}
	return b.String()
}

// pad writes v with at least width digits.
func pad(b *strings.Builder, v, width int) {
	s := strconv.Itoa(v)
	for range width - len(s) {
		b.WriteByte('0')
	}
	b.WriteString(s)
}

// half returns the half of the day of t that the AM/PM, am/pm, A/P or a/p
// opening rest names, in its case, and the bytes it takes.
func half(t time.Time, rest string) (string, int, bool) {
	pm := t.Hour() >= 12
	for _, f := range []struct{ token, am, pm string }{
		{"AM/PM", "AM", "PM"}, {"am/pm", "am", "pm"}, {"A/P", "A", "P"}, {"a/p", "a", "p"},
	} {
		if strings.HasPrefix(rest, f.token) {
			if pm {
				return f.pm, len(f.token), true
			}
			return f.am, len(f.token), true
		}
	}
	return "", 0, false
}
