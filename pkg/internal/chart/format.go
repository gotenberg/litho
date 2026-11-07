package chart

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// maxFormatCode caps the length of a number format code formatLabel reads.
// Anti-DoS guard, not spec-mandated: real codes stay under a hundred
// characters, and a label is formatted once per tick or point.
const maxFormatCode = 256

// formatLabel formats v with the number format code code, the
// SpreadsheetML syntax Calc also uses for its own formats (ECMA-376 Part 1,
// 18.8.31 numFmt). It covers the codes charts carry: General, digit
// placeholders with decimals and thousands separators, percentages,
// scientific notation, literal text, the sections for positive, negative
// and zero values, and the date and time codes. Colours and conditions in
// brackets are ignored; a currency in brackets keeps its symbol.
func formatLabel(code string, v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}
	if len(code) > maxFormatCode {
		code = code[:maxFormatCode]
	}
	sections := splitSections(code)
	sec, neg := sections[0], false
	switch {
	case v < 0 && len(sections) > 1:
		sec, v = sections[1], -v
	case v == 0 && len(sections) > 2:
		sec = sections[2]
	case v < 0:
		neg = true
		v = -v
	}
	sec = strings.TrimSpace(sec)
	if sec == "" || strings.EqualFold(sec, "general") || strings.EqualFold(sec, "standard") || sec == "@" {
		if neg {
			return "-" + formatGeneral(v)
		}
		return formatGeneral(v)
	}
	out := formatSection(sec, v)
	if neg && strings.ContainsAny(out, "123456789") {
		out = "-" + out
	}
	return out
}

// formatGeneral formats v as the General format does: up to ten
// significant digits, scientific notation for very large and very small
// magnitudes.
func formatGeneral(v float64) string {
	if v == 0 {
		return "0"
	}
	a := math.Abs(v)
	if a >= 1e11 || a < 1e-9 {
		s := strconv.FormatFloat(v, 'E', 5, 64)
		mant, exp, _ := strings.Cut(s, "E")
		if strings.Contains(mant, ".") {
			mant = strings.TrimRight(strings.TrimRight(mant, "0"), ".")
		}
		sign := exp[:1]
		digits := strings.TrimLeft(exp[1:], "0")
		if len(digits) < 2 {
			digits = strings.Repeat("0", 2-len(digits)) + digits
		}
		return mant + "E" + sign + digits
	}
	s := strconv.FormatFloat(v, 'g', 10, 64)
	if strings.ContainsAny(s, "eE") {
		f, _ := strconv.ParseFloat(s, 64)
		s = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return s
}

// splitSections splits a format code at its unquoted semicolons.
func splitSections(code string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == '"':
			quoted = !quoted
		case c == '\\' && !quoted && i+1 < len(code):
			b.WriteByte(c)
			i++
			c = code[i]
		case c == ';' && !quoted:
			out = append(out, b.String())
			b.Reset()
			continue
		}
		b.WriteByte(c)
	}
	return append(out, b.String())
}

// token is one piece of a format section: literal text or a placeholder
// run.
type token struct {
	lit string
	// code is a run of placeholder characters, "" for a literal.
	code string
}

// tokenize splits a section into literals and codes, resolving quotes,
// escapes and brackets.
func tokenize(sec string) []token {
	var toks []token
	var code strings.Builder
	flush := func() {
		if code.Len() > 0 {
			toks = append(toks, token{code: code.String()})
			code.Reset()
		}
	}
	lit := func(s string) {
		flush()
		toks = append(toks, token{lit: s})
	}
	for i := 0; i < len(sec); i++ {
		c := sec[i]
		switch c {
		case '"':
			j := strings.IndexByte(sec[i+1:], '"')
			if j < 0 {
				lit(sec[i+1:])
				return toks
			}
			lit(sec[i+1 : i+1+j])
			i += j + 1
		case '\\':
			if i+1 < len(sec) {
				lit(sec[i+1 : i+2])
				i++
			}
		case '[':
			j := strings.IndexByte(sec[i:], ']')
			if j < 0 {
				return toks
			}
			inner := sec[i+1 : i+j]
			if strings.HasPrefix(inner, "$") {
				sym, _, _ := strings.Cut(inner[1:], "-")
				lit(sym)
			}
			i += j
		case '_', '*':
			// _x leaves the width of x, *x repeats x: a space either way.
			if i+1 < len(sec) {
				i++
			}
			if c == '_' {
				lit(" ")
			}
		case '0', '#', '?', '.', ',', '%', 'E', 'e', '+', '-', 'y', 'Y', 'm', 'M', 'd', 'D', 'h', 'H', 's', 'S', ':', '/', 'A', 'a', 'P', 'p':
			if c == '-' || c == '+' || c == ':' || c == '/' {
				if code.Len() == 0 || !isNumericCode(code.String()) || c == ':' || c == '/' {
					lit(string(c))
					continue
				}
			}
			code.WriteByte(c)
		default:
			lit(string(c))
		}
	}
	flush()
	return toks
}

// isNumericCode reports a code run of digit placeholders, which a sign may
// follow in an exponent.
func isNumericCode(s string) bool {
	return strings.ContainsAny(s, "Ee")
}

// isDateCode reports a section whose placeholders are dates or times.
func isDateCode(toks []token) bool {
	for _, t := range toks {
		if strings.ContainsAny(t.code, "yYdDhHsS") {
			return true
		}
		if strings.ContainsAny(t.code, "mM") && !strings.ContainsAny(t.code, "0#?") {
			return true
		}
	}
	return false
}

// formatSection formats the non-negative v with one section.
func formatSection(sec string, v float64) string {
	toks := tokenize(sec)
	if isDateCode(toks) {
		return formatDate(toks, v)
	}
	var num strings.Builder
	for _, t := range toks {
		num.WriteString(t.code)
	}
	nc := num.String()
	if strings.Contains(nc, "%") {
		v *= 100 * math.Pow(100, float64(strings.Count(nc, "%")-1))
	}
	formatted := formatNumber(strings.ReplaceAll(nc, "%", ""), v)
	var b strings.Builder
	placed := false
	for _, t := range toks {
		if t.code == "" {
			b.WriteString(t.lit)
			continue
		}
		if !placed {
			b.WriteString(formatted)
			placed = true
		}
		b.WriteString(strings.Repeat("%", strings.Count(t.code, "%")))
	}
	if !placed {
		return b.String()
	}
	return b.String()
}

// formatNumber formats v with the digit placeholders of code.
func formatNumber(code string, v float64) string {
	if code == "" {
		return ""
	}
	if i := strings.IndexAny(code, "Ee"); i >= 0 {
		return formatScientific(code[:i], code[i+1:], v)
	}
	intPart, frac, _ := strings.Cut(code, ".")
	// Trailing commas scale by a thousand each.
	for strings.HasSuffix(intPart, ",") {
		intPart = intPart[:len(intPart)-1]
		v /= 1000
	}
	grouping := strings.Contains(intPart, ",")
	decimals := strings.Count(frac, "0") + strings.Count(frac, "#") + strings.Count(frac, "?")
	minDecimals := strings.Count(frac, "0")
	s := strconv.FormatFloat(roundHalfUp(v, decimals), 'f', decimals, 64)
	ip, fp, _ := strings.Cut(s, ".")
	for len(fp) > minDecimals && strings.HasSuffix(fp, "0") {
		fp = fp[:len(fp)-1]
	}
	minInt := strings.Count(intPart, "0")
	if ip == "0" && minInt == 0 {
		ip = ""
	}
	for len(ip) < minInt {
		ip = "0" + ip
	}
	if grouping {
		ip = group(ip)
	}
	if fp == "" && !strings.Contains(code, ".") {
		return ip
	}
	if fp == "" && minDecimals == 0 {
		return ip + "."
	}
	return ip + "." + fp
}

// group inserts thousands separators into the digits s.
func group(s string) string {
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// formatScientific formats v as mantissa and exponent codes.
func formatScientific(mant, exp string, v float64) string {
	sign := ""
	if strings.HasPrefix(exp, "+") {
		sign = "+"
	}
	exp = strings.TrimLeft(exp, "+-")
	e := 0
	if v != 0 {
		e = int(math.Floor(math.Log10(v)))
	}
	m := v / math.Pow(10, float64(e))
	ms := formatNumber(mant, m)
	if strings.HasPrefix(ms, "10") && !strings.HasPrefix(mant, "00") {
		e++
		ms = formatNumber(mant, m/10)
	}
	es := strconv.Itoa(abs(e))
	for len(es) < len(exp) {
		es = "0" + es
	}
	if e < 0 {
		sign = "-"
	}
	return ms + "E" + sign + es
}

// abs returns the magnitude of n.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// serialEpoch is day 0 of the 1900 date system as spreadsheets count it,
// past the phantom 29 February 1900 (ECMA-376 Part 1, 18.17.4.1).
var serialEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// maxSerial bounds the date serials formatted as dates: 31 December 9999.
const maxSerial = 2958465

var (
	monthNames = [...]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	dayNames   = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
)

// formatDate formats the serial v with date and time codes.
func formatDate(toks []token, v float64) string {
	if v < 0 || v > maxSerial {
		return formatGeneral(v)
	}
	days := math.Floor(v)
	secs := math.Round((v - days) * 86400)
	t := serialEpoch.AddDate(0, 0, int(days)).Add(time.Duration(secs) * time.Second)
	ampm := false
	for _, tk := range toks {
		if strings.Contains(strings.ToUpper(tk.code), "AM/PM") || strings.Contains(strings.ToUpper(tk.code), "A/P") {
			ampm = true
		}
	}
	var b strings.Builder
	lastHour := false
	for _, tk := range toks {
		if tk.code == "" {
			b.WriteString(tk.lit)
			continue
		}
		c := tk.code
		for len(c) > 0 {
			up := strings.ToUpper(c)
			n := 1
			for n < len(c) && strings.EqualFold(c[n:n+1], c[:1]) {
				n++
			}
			switch {
			case strings.HasPrefix(up, "AM/PM"):
				if t.Hour() < 12 {
					b.WriteString("AM")
				} else {
					b.WriteString("PM")
				}
				n = 5
			case strings.HasPrefix(up, "A/P"):
				if t.Hour() < 12 {
					b.WriteString("A")
				} else {
					b.WriteString("P")
				}
				n = 3
			case up[0] == 'Y':
				if n <= 2 {
					b.WriteString(pad2(t.Year() % 100))
				} else {
					b.WriteString(strconv.Itoa(t.Year()))
				}
			case up[0] == 'M' && lastHour:
				b.WriteString(pad(t.Minute(), n))
			case up[0] == 'M':
				switch {
				case n >= 5:
					b.WriteString(monthNames[t.Month()-1][:1])
				case n == 4:
					b.WriteString(monthNames[t.Month()-1])
				case n == 3:
					b.WriteString(monthNames[t.Month()-1][:3])
				default:
					b.WriteString(pad(int(t.Month()), n))
				}
			case up[0] == 'D':
				switch {
				case n >= 4:
					b.WriteString(dayNames[t.Weekday()])
				case n == 3:
					b.WriteString(dayNames[t.Weekday()][:3])
				default:
					b.WriteString(pad(t.Day(), n))
				}
			case up[0] == 'H':
				h := t.Hour()
				if ampm {
					h %= 12
					if h == 0 {
						h = 12
					}
				}
				b.WriteString(pad(h, n))
			case up[0] == 'S':
				b.WriteString(pad(t.Second(), n))
			case up[0] == '0' || up[0] == '.':
				// Fractions of seconds are not drawn.
			default:
				b.WriteString(c[:n])
			}
			lastHour = up[0] == 'H' || (lastHour && (up[0] == 'M'))
			if n > len(c) {
				n = len(c)
			}
			c = c[n:]
		}
	}
	return b.String()
}

// pad formats n with at least width digits, width 1 or 2.
func pad(n, width int) string {
	if width >= 2 {
		return pad2(n)
	}
	return strconv.Itoa(n)
}

// pad2 formats n with two digits.
func pad2(n int) string {
	if n < 10 && n >= 0 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// roundHalfUp rounds the non-negative v to decimals places, halves away
// from zero as spreadsheets round, not to even.
func roundHalfUp(v float64, decimals int) float64 {
	if decimals > 15 {
		return v
	}
	p := math.Pow(10, float64(decimals))
	r := math.Floor(v*p+0.5+1e-9) / p
	if math.IsInf(r, 0) || math.IsNaN(r) {
		return v
	}
	return r
}
