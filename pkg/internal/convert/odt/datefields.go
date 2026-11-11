package odt

import (
	"math"
	"regexp"
	"strconv"
	"time"

	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// Default codes of a date or time field that names no data style.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 (the pinned gotenberg/gotenberg:9.0.0
//     image) prints an en-US text:date without a data style as
//     11/11/26 and a text:time as 19:35:20.
const (
	defaultDateCode = "mm/dd/yy"
	defaultTimeCode = "hh:mm:ss"
)

// maxAdjustDays bounds the shift a text:date-adjust or text:time-adjust
// applies, in days: 3,660,000, about 10,000 years. Anti-DoS guard, not
// spec-mandated: past it the date leaves the range a data style prints,
// and the shift is ignored.
const maxAdjustDays = 3_660_000

// durationRE matches an XML Schema duration (ODF 1.3, Part 3, 19.369 and
// 19.375 take one): sign, years, months, days, hours, minutes, seconds.
var durationRE = regexp.MustCompile(`^(-)?P(?:(\d{1,9})Y)?(?:(\d{1,9})M)?(?:(\d{1,9})D)?(?:T(?:(\d{1,9})H)?(?:(\d{1,9})M)?(?:(\d{1,9})(?:\.\d+)?S)?)?$`)

// dateField returns what a text:date or text:time (ODF 1.3, Part 3, 7.3.2
// and 7.3.3) shows on the conversion date: that date shifted by its
// text:date-adjust or text:time-adjust, in its data style, or a default
// one; false for a field text:fixed holds or without a conversion date,
// which shows the content the document saved. Writer updates such a
// field to the moment it prints the document.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 prints a text:date and a text:time
//     that are not fixed on the moment of the conversion, a date adjusted
//     by P1D a day later, and a fixed one as saved; a text:print-date or
//     text:creation-date shows the document's metadata, not that moment.
func (b *builder) dateField(c *odf.Element) (string, bool) {
	if b.date.IsZero() || c.A(odf.NSText, "fixed") == "true" {
		return "", false
	}
	t := b.date
	code := defaultDateCode
	adjust := c.A(odf.NSText, "date-adjust")
	if c.Name.Local == "time" {
		code, adjust = defaultTimeCode, c.A(odf.NSText, "time-adjust")
	}
	if adjust != "" {
		t = adjusted(t, adjust)
	}
	v := numfmt.Value{Kind: numfmt.Number, Number: serial(t)}
	if s, ok := b.formatValue(c.A(odf.NSStyle, "data-style-name"), v); ok {
		return s, true
	}
	f, err := numfmt.Parse(code)
	if err != nil {
		return "", false
	}
	return f.Apply(v).Text, true
}

// adjusted returns t shifted by the XML Schema duration d, t itself for
// a duration it does not read or one past [maxAdjustDays].
func adjusted(t time.Time, d string) time.Time {
	m := durationRE.FindStringSubmatch(d)
	if m == nil {
		return t
	}
	var n [6]int
	for i := range n {
		if m[i+2] != "" {
			// The pattern matched at most nine digits, which always parse.
			n[i], _ = strconv.Atoi(m[i+2])
		}
	}
	if days := n[0]*366 + n[1]*31 + n[2] + n[3]/24 + n[4]/1440 + n[5]/86400; days > maxAdjustDays {
		return t
	}
	sign := 1
	if m[1] == "-" {
		sign = -1
	}
	t = t.AddDate(sign*n[0], sign*n[1], sign*n[2])
	return t.Add(time.Duration(sign) * (time.Duration(n[3])*time.Hour + time.Duration(n[4])*time.Minute + time.Duration(n[5])*time.Second))
}

// serialNudge is added to a serial's time of day, in seconds: a serial
// of this century holds its time of day to about 0.6 microseconds, and a
// data style showing whole seconds cuts the time to the second, so the
// nudge keeps the binary error from cutting a whole second short.
const serialNudge = 2e-6

// serial returns the wall-clock date and time t reads in its own location
// as days since [nullDate].
func serial(t time.Time) float64 {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	days := math.Round(day.Sub(nullDate).Hours() / 24)
	secs := float64(t.Hour()*3600+t.Minute()*60+t.Second()) + float64(t.Nanosecond())/1e9
	return days + (secs+serialNudge)/86400
}
