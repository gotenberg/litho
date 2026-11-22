package html

import (
	"math"
	"strconv"
	"strings"
	"time"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/convert/formfield"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// inputType returns the type of the input n, lower-cased and trimmed,
// "text" for none.
func inputType(n *dom.Node) string {
	typ, _ := attr(n, "type")
	if typ = strings.ToLower(strings.TrimSpace(typ)); typ == "" {
		return "text"
	}
	return typ
}

// formDisabled reports a disabled form control (HTML Standard,
// 4.10.18.5): one with a disabled attribute, or a descendant of a
// disabled fieldset outside its first legend.
func formDisabled(n *dom.Node) bool {
	if _, ok := attr(n, "disabled"); ok {
		return true
	}
	child := n
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == dom.ElementNode && p.Namespace == "" && p.DataAtom == atom.Fieldset {
			if _, ok := attr(p, "disabled"); ok && child != firstLegend(p) {
				return true
			}
		}
		child = p
	}
	return false
}

// firstLegend returns the first legend child of the fieldset f, nil for
// none.
func firstLegend(f *dom.Node) *dom.Node {
	for c := f.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == dom.ElementNode && c.Namespace == "" && c.DataAtom == atom.Legend {
			return c
		}
	}
	return nil
}

// formRole makes node the Form element of the control n printed as the
// non-interactive field of role (ISO 32000-1, 14.8.5.6, Table 348): its
// checked state for a check box or a radio button, its accessible
// description its aria-label, else its title, else its name.
func formRole(node *layout.Node, n *dom.Node, role typeset.PrintRole, checked bool) {
	node.Role = "Form"
	desc := ""
	for _, key := range []string{"aria-label", "title", "name"} {
		if v, ok := attr(n, key); ok {
			if desc = collapse(v); desc != "" {
				break
			}
		}
	}
	node.PrintField = typeset.PrintField{Role: role, Desc: formfield.Clip(desc)}
	if role == typeset.PrintCheckBox || role == typeset.PrintRadio {
		node.PrintField.Checked = typeset.CheckOff
		if checked {
			node.PrintField.Checked = typeset.CheckOn
		}
	}
}

// placeholderColor is the colour of a control's placeholder text, as
// Chromium's ::placeholder takes it under either colour scheme.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints the
//     placeholder of a text field, a textarea and a telephone field in
//     #757575, under color-scheme: dark too.
var placeholderColor = css.Color{R: 0x75 / 255.0, G: 0x75 / 255.0, B: 0x75 / 255.0, A: 1}

// placeholderStyle returns st coloured as a placeholder.
func placeholderStyle(st *css.Style) *css.Style {
	c := *st
	c.Color = placeholderColor
	return &c
}

// partStyle returns the style of a part of a control styled st, which
// the control's shadow tree holds in Chromium (a date field, a file
// control's button): st's text, with no box of its own, inline.
func partStyle(st *css.Style) *css.Style {
	c := *withoutBackground(st)
	zero := [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	c.Display = css.Inline
	c.Border, c.Outline, c.BoxShadow = [4]css.Border{}, css.Border{}, nil
	c.Margin, c.Padding = zero, zero
	c.Width, c.Height = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	c.Overflow = css.OverflowVisible
	c.Appearance = css.AppearanceNone
	c.AuthorBorder, c.AuthorBackground = false, false
	return &c
}

// parseNumber parses a floating-point number value (HTML Standard,
// 2.3.4.3), false for none.
func parseNumber(s string) (float64, bool) {
	s = strings.Trim(s, "\t\n\f\r ")
	if s == "" || strings.ContainsAny(s, "xXpP_") {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false
	}
	return v, true
}

// numberAttr returns the attribute key of n as a number, def when it is
// absent or invalid.
func numberAttr(n *dom.Node, key string, def float64) (float64, bool) {
	v, ok := attr(n, key)
	if !ok {
		return def, false
	}
	f, ok := parseNumber(v)
	if !ok {
		return def, false
	}
	return f, true
}

// rangeValue returns where the thumb of the range input n sits, from 0 to
// 1 (HTML Standard, 4.10.5.1.13): its value, the midpoint of its minimum
// (0) and its maximum (100) when it has none, clamped to them and to the
// nearest step (1) from the minimum.
func rangeValue(n *dom.Node) float64 {
	lo, _ := numberAttr(n, "min", 0)
	hi, _ := numberAttr(n, "max", 100)
	if hi < lo {
		hi = lo
	}
	v, ok := numberAttr(n, "value", 0)
	if !ok {
		v = lo + float64((hi-lo)/2)
	}
	v = min(max(v, lo), hi)
	step := 1.0
	if s, ok := attr(n, "step"); ok {
		if strings.EqualFold(strings.TrimSpace(s), "any") {
			step = 0
		} else if f, ok := parseNumber(s); ok && f > 0 {
			step = f
		}
	}
	if step > 0 {
		v = lo + float64(math.Floor((v-lo)/step+0.5)*step)
		for v > hi {
			v -= step
		}
	}
	if hi == lo {
		return 0
	}
	return (v - lo) / (hi - lo)
}

// progressValue returns how much of the progress bar n fills, from 0 to
// 1, or -1 for an indeterminate one (HTML Standard, 4.10.13): its value
// over its maximum, 1 when absent or not above 0, the value held between
// 0 and the maximum.
func progressValue(n *dom.Node) float64 {
	v, ok := numberAttr(n, "value", 0)
	if !ok {
		return -1
	}
	hi, _ := numberAttr(n, "max", 1)
	if hi <= 0 {
		hi = 1
	}
	return min(max(v, 0), hi) / hi
}

// meterValue returns how much of the meter n fills, from 0 to 1, and the
// region its value lies in (HTML Standard, 4.10.14): the minimum 0 and
// maximum 1 unless given, the value, the low and high boundaries and the
// optimum each clamped as the Standard clamps them.
func meterValue(n *dom.Node) (float64, layout.MeterLevel) {
	lo, _ := numberAttr(n, "min", 0)
	hi, _ := numberAttr(n, "max", 1)
	if hi < lo {
		hi = lo
	}
	clamp := func(v, a, b float64) float64 { return min(max(v, a), b) }
	v, _ := numberAttr(n, "value", 0)
	v = clamp(v, lo, hi)
	low, _ := numberAttr(n, "low", lo)
	low = clamp(low, lo, hi)
	high, _ := numberAttr(n, "high", hi)
	high = clamp(high, low, hi)
	opt, _ := numberAttr(n, "optimum", lo+float64((hi-lo)/2))
	opt = clamp(opt, lo, hi)
	level := layout.MeterOptimum
	switch {
	case opt < low:
		if v > high {
			level = layout.MeterEvenLessGood
		} else if v >= low {
			level = layout.MeterSuboptimal
		}
	case opt > high:
		if v < low {
			level = layout.MeterEvenLessGood
		} else if v <= high {
			level = layout.MeterSuboptimal
		}
	default:
		if v < low || v > high {
			level = layout.MeterSuboptimal
		}
	}
	if hi == lo {
		return 0, level
	}
	return (v - lo) / (hi - lo), level
}

// colorValue returns the colour the colour input n shows: its value when
// it is a valid simple colour, #rrggbb in either case, else black (HTML
// Standard, 4.10.5.1.15).
func colorValue(n *dom.Node) typeset.Color {
	v, _ := attr(n, "value")
	v = strings.Trim(v, "\t\n\f\r ")
	if len(v) != 7 || v[0] != '#' {
		return typeset.Black
	}
	x, err := strconv.ParseUint(v[1:], 16, 32)
	if err != nil {
		return typeset.Black
	}
	return typeset.Color{R: float64(x>>16&0xff) / 255, G: float64(x>>8&0xff) / 255, B: float64(x&0xff) / 255, A: 1}
}

// dateText returns the text a date or time input n of type typ shows and
// the text as wide as the widest it can show, as
// Chromium shows them in the en-US locale: an HTML conversion has no
// locale of its own, and Chromium formats these fields in its own
// locale, never the page's language. A value that does not parse shows
// the fields' placeholders.
//
// The value is formatted by number format codes over a serial date, the
// formatter and month names of the spreadsheet formats (numfmt).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1, its locale
//     en-US, prints 2026-11-22 as 11/22/2026, 1999-01-05 as 01/05/1999,
//     13:45 as 01:45 PM, 09:05:30 as 09:05:30 AM, 13:45:30.5 as
//     01:45:30.500 PM, 2026-11-22T13:45 as 11/22/2026, 01:45 PM, 2026-11
//     as November 2026, 2026-W47 as Week 47, 2026, an empty date as
//     mm/dd/yyyy and an empty time as --:-- --, under lang="fr" too; a
//     month field as wide as September.
func dateText(n *dom.Node, typ string) (shown, widest string) {
	v, _ := attr(n, "value")
	v = strings.TrimSpace(v)
	switch typ {
	case "date":
		if t, ok := parseDate(v); ok {
			shown = formatSerial(t, "mm/dd/yyyy")
		} else {
			shown = "mm/dd/yyyy"
		}
		return shown, shown
	case "time":
		t, code := parseTime(v)
		if code == "" {
			return "--:-- --", "--:-- --"
		}
		shown = formatSerial(time.Date(1899, 12, 30, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC), code)
		return shown, shown
	case "datetime-local":
		d, tm, ok := strings.Cut(v, "T")
		if !ok {
			d, tm, ok = strings.Cut(v, " ")
		}
		day, dok := parseDate(d)
		t, code := parseTime(tm)
		if !ok || !dok || code == "" {
			return "mm/dd/yyyy, --:-- --", "mm/dd/yyyy, --:-- --"
		}
		at := time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
		shown = formatSerial(at, "mm/dd/yyyy") + ", " + formatSerial(at, code)
		return shown, shown
	case "month":
		t, ok := parseDate(v + "-01")
		if !ok {
			return "--------- ----", "--------- ----"
		}
		shown = formatSerial(t, "mmmm yyyy")
		return shown, "September" + shown[strings.IndexByte(shown, ' '):]
	}
	// week: yyyy-Www.
	year, week, ok := strings.Cut(v, "-W")
	y, yerr := strconv.ParseUint(year, 10, 32)
	w, werr := strconv.ParseUint(week, 10, 8)
	if !ok || yerr != nil || werr != nil || len(year) < 4 || len(week) != 2 || y < 1 || w < 1 || w > 53 {
		return "Week --, ----", "Week --, ----"
	}
	shown = "Week " + week + ", " + year
	return shown, shown
}

// dateSegment is a field of a date or time input's text, or the literal
// text between two fields.
type dateSegment struct {
	text  string
	field bool
}

// dateSegments splits the text of a date or time input into its fields,
// the runs of letters, digits and hyphens but the word "Week", and the
// literal text between them.
func dateSegments(s string) []dateSegment {
	var out []dateSegment
	inField := func(r rune) bool {
		return r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
	}
	start := 0
	for start < len(s) {
		end := start
		field := inField(rune(s[start]))
		for end < len(s) && inField(rune(s[end])) == field {
			end++
		}
		seg := dateSegment{text: s[start:end], field: field && s[start:end] != "Week"}
		if n := len(out); n > 0 && !seg.field && !out[n-1].field {
			out[n-1].text += seg.text
		} else {
			out = append(out, seg)
		}
		start = end
	}
	return out
}

// countFields returns the number of fields of segs.
func countFields(segs []dateSegment) int {
	n := 0
	for _, s := range segs {
		if s.field {
			n++
		}
	}
	return n
}

// parseDate parses a valid date string (HTML Standard, 2.3.5.2),
// yyyy-mm-dd.
func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil && t.Year() >= 1
}

// parseTime parses a valid time string (HTML Standard, 2.3.5.4), hh:mm,
// hh:mm:ss or hh:mm:ss.sss, and returns it, the number format code that
// shows it as Chromium does; "" for none.
func parseTime(s string) (time.Time, string) {
	// Go reads a fraction after the seconds whatever the layout says: the
	// fraction is looked for first.
	for _, f := range []struct{ layout, code string }{
		{"15:04", "hh:mm AM/PM"},
		{"15:04:05", "hh:mm:ss AM/PM"},
		{"15:04:05.999999999", "hh:mm:ss.000 AM/PM"},
	} {
		if strings.Contains(s, ".") != strings.Contains(f.layout, ".") {
			continue
		}
		if t, err := time.Parse(f.layout, s); err == nil {
			return t, f.code
		}
	}
	return time.Time{}, ""
}

// secondsPerDay is the length of a day of a serial date.
const secondsPerDay = 24 * 60 * 60

// formatSerial formats t, read as a spreadsheet's serial date from
// 1899-12-30, by the number format code.
func formatSerial(t time.Time, code string) string {
	f, err := numfmt.Parse(code)
	if err != nil {
		return ""
	}
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	days := float64(t.Unix()-base.Unix())/secondsPerDay + float64(t.Nanosecond())/(secondsPerDay*1e9)
	return f.Apply(numfmt.Value{Kind: numfmt.Number, Number: days}).Text
}
