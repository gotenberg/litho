package numfmt

import (
	"math"
	"strconv"
	"strings"
)

// figureSpace is what a "?" placeholder without a digit shows.
const figureSpace = " "

// maxExponentDigits caps the exponent placeholders honoured. Anti-DoS
// guard, not spec-mandated: a double's exponent has three digits.
const maxExponentDigits = 8

// maxDecimalPlaces caps the decimal placeholders honoured. Anti-DoS
// guard, not spec-mandated: a double carries 17 significant digits, and
// LibreOffice writes zeros past the fifteenth.
const maxDecimalPlaces = 64

// mantissa locates the parts of a number section.
type mantissa struct {
	// dot is the index of the decimal point, exp of the exponent, both
	// len(toks) when absent.
	dot, exp int
	// intDigits and decDigits count the placeholders before and after the
	// point.
	intDigits, decDigits int
	// group reports a thousands separator between integer placeholders;
	// scale counts the scaling commas.
	group bool
	scale int
	// role marks each comma: 0 literal, 1 grouping, 2 scaling.
	role    []uint8
	percent int
	// intHash reports a '#' among the integer placeholders.
	intHash bool
}

// analyse returns the layout of the number section's tokens.
func (s *section) analyse() mantissa {
	m := mantissa{dot: len(s.toks), exp: len(s.toks), role: make([]uint8, len(s.toks))}
	for i, t := range s.toks {
		if t.kind == tExp && m.exp == len(s.toks) {
			m.exp = i
		}
	}
	for i := 0; i < m.exp; i++ {
		if s.toks[i].kind == tDot {
			m.dot = i
			break
		}
	}
	for i, t := range s.toks {
		switch {
		case t.kind == tDigit && i < m.dot:
			m.intDigits++
			if t.text == "#" {
				m.intHash = true
			}
		case t.kind == tDigit && i > m.dot && i < m.exp:
			m.decDigits++
		case t.kind == tPercent:
			m.percent++
		}
	}
	for i := 0; i < m.exp; i++ {
		if s.toks[i].kind != tComma {
			continue
		}
		before, after := false, false
		for j := 0; j < i; j++ {
			before = before || s.toks[j].kind == tDigit
		}
		for j := i + 1; j < m.exp; j++ {
			after = after || s.toks[j].kind == tDigit
		}
		switch {
		case before && after && i < m.dot:
			m.role[i] = 1
			m.group = true
		case before && !after:
			m.role[i] = 2
			m.scale++
		}
	}
	return m
}

// fixed formats x by a section of digit placeholders, in scientific
// notation when it has an exponent.
func (s *section) fixed(x float64, first bool) Result {
	neg := x < 0 && first
	x = math.Abs(x)
	m := s.analyse()
	for range min(m.percent, 4) {
		x *= 100
	}
	for range min(m.scale, 8) {
		x /= 1000
	}
	dec := min(m.decDigits, maxDecimalPlaces)
	e := 0
	if m.exp < len(s.toks) && x != 0 {
		n := max(m.intDigits, 1)
		step := 1
		e0 := int(math.Floor(math.Log10(x)))
		if m.intDigits > 1 && m.intHash {
			// Engineering notation: the exponent is a multiple of the
			// integer placeholders.
			step = m.intDigits
			e = floorDiv(e0, step) * step
		} else {
			e = e0 - (n - 1)
		}
		limit := math.Pow(10, float64(n))
		if step > 1 {
			limit = math.Pow(10, float64(step))
		}
		if roundTo(x/math.Pow(10, float64(e)), dec) >= limit {
			e += step
		}
		x /= math.Pow(10, float64(e))
	}
	str := formatFloat(x, fmtF, dec, false)
	ip, dp, _ := strings.Cut(str, ".")
	ip = strings.TrimLeft(ip, "0")
	if neg && strings.Trim(ip+dp, "0") == "" {
		neg = false
	}
	var b strings.Builder
	r := Result{Numeric: true}
	if neg {
		b.WriteByte('-')
	}
	s.writeInt(&b, &r, ip, 0, m.dot, m.role, m.group)
	if m.dot < len(s.toks) {
		s.writeDecimals(&b, &r, dp, m)
	}
	if m.exp < len(s.toks) {
		t := s.toks[m.exp]
		b.WriteByte('E')
		if e < 0 {
			b.WriteByte('-')
		} else if t.text == "+" {
			b.WriteByte('+')
		}
		nd := 0
		for i := m.exp + 1; i < len(s.toks); i++ {
			if s.toks[i].kind == tDigit {
				nd++
			}
		}
		es := strconv.Itoa(absInt(e))
		for len(es) < min(nd, maxExponentDigits) {
			es = "0" + es
		}
		b.WriteString(es)
		for i := m.exp + 1; i < len(s.toks); i++ {
			s.writeOther(&b, &r, s.toks[i])
		}
	}
	r.Text = b.String()
	return r
}

// writeInt writes the tokens [from, to) of the section, the integer
// part, with the digits ip spread over the placeholders from the right,
// the excess with the first, grouped by three when group is set.
func (s *section) writeInt(b *strings.Builder, r *Result, ip string, from, to int, role []uint8, group bool) {
	n := 0
	for i := from; i < to; i++ {
		if s.toks[i].kind == tDigit {
			n++
		}
	}
	var out strings.Builder
	k := 0
	for i := from; i < to; i++ {
		t := s.toks[i]
		switch {
		case t.kind == tDigit:
			// The k-th placeholder from the left shows the digit at
			// len(ip) - (n - k), the first one every digit before it too.
			pos := len(ip) - (n - k)
			switch {
			case k == 0 && pos >= 0:
				out.WriteString(ip[:pos+1])
			case pos >= 0:
				out.WriteByte(ip[pos])
			case t.text == "0":
				out.WriteByte('0')
			case t.text == "?":
				out.WriteString(figureSpace)
			}
			k++
		case t.kind == tComma && role != nil && role[i] != 0:
		default:
			if out.Len() > 0 {
				writeGrouped(b, out.String(), group)
				out.Reset()
			}
			s.writeOther(b, r, t)
		}
	}
	if out.Len() > 0 {
		writeGrouped(b, out.String(), group)
	}
}

// writeGrouped writes the digits run d, with a thousands separator
// between each group of three digits from the right when group is set.
func writeGrouped(b *strings.Builder, d string, group bool) {
	if !group {
		b.WriteString(d)
		return
	}
	digits := 0
	for _, c := range d {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	seen := 0
	for _, c := range d {
		if c >= '0' && c <= '9' {
			if seen > 0 && (digits-seen)%3 == 0 {
				b.WriteByte(',')
			}
			seen++
		}
		b.WriteRune(c)
	}
}

// writeDecimals writes the decimal point and the tokens after it up to
// the exponent, the decimal digits dp over the placeholders: trailing
// zeros of '#' placeholders dropped, of '?' ones shown as figure spaces.
// The point is dropped too when no decimal shows (LibreOffice writes 3
// for 3 in #.##).
func (s *section) writeDecimals(b *strings.Builder, r *Result, dp string, m mantissa) {
	type place struct {
		tok   int
		digit byte
		show  string
	}
	var places []place
	k := 0
	for i := m.dot + 1; i < m.exp; i++ {
		if s.toks[i].kind == tDigit {
			d := byte('0')
			if k < len(dp) {
				d = dp[k]
			}
			places = append(places, place{tok: i, digit: d, show: string(d)})
			k++
		}
	}
	trailing := true
	shown := false
	for j := len(places) - 1; j >= 0; j-- {
		p := &places[j]
		ph := s.toks[p.tok].text
		if trailing && p.digit == '0' && ph != "0" {
			if ph == "#" {
				p.show = ""
			} else {
				p.show = figureSpace
				shown = true
			}
			continue
		}
		trailing = false
		shown = true
	}
	if shown || m.decDigits == 0 {
		b.WriteByte('.')
	}
	pi := 0
	for i := m.dot + 1; i < m.exp; i++ {
		t := s.toks[i]
		if t.kind == tDigit {
			b.WriteString(places[pi].show)
			pi++
			continue
		}
		if t.kind == tComma && m.role[i] != 0 {
			continue
		}
		s.writeOther(b, r, t)
	}
}

// writeOther writes a token that is not a placeholder.
func (s *section) writeOther(b *strings.Builder, r *Result, t token) {
	switch t.kind {
	case tLit:
		b.WriteString(t.text)
	case tPercent:
		b.WriteByte('%')
	case tComma:
		b.WriteByte(',')
	case tFill:
		r.setFill(t, b.Len())
	case tSlash:
		b.WriteByte('/')
	case tDot:
		b.WriteByte('.')
	case tAt:
	case tGeneral:
		b.WriteString("General")
	case tDigit, tExp, tDate, tAMPM, tAP, tSecFrac:
	}
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// fraction formats x by a fraction section: an optional integer part,
// the numerator's placeholders, the bar and the denominator's
// placeholders or fixed digits.
func (s *section) fraction(x float64, first bool) Result {
	neg := x < 0 && first
	x = math.Abs(x)
	slash := -1
	for i, t := range s.toks {
		if t.kind == tSlash {
			slash = i
			break
		}
	}
	// The numerator is the run of placeholders right before the bar.
	numFrom := slash
	for numFrom > 0 && s.toks[numFrom-1].kind == tDigit {
		numFrom--
	}
	// The denominator is the run of placeholders or digits after it.
	denTo := slash + 1
	fixedDen := ""
	denPlaces := ""
	rest := ""
scan:
	for ; denTo < len(s.toks); denTo++ {
		t := s.toks[denTo]
		switch {
		case t.kind == tDigit:
			denPlaces += t.text
		case t.kind == tLit && t.text != "" && t.text[0] >= '0' && t.text[0] <= '9' && denPlaces == "":
			n := 0
			for n < len(t.text) && t.text[n] >= '0' && t.text[n] <= '9' {
				n++
			}
			fixedDen += t.text[:n]
			if n < len(t.text) {
				rest = t.text[n:]
				denTo++
				break scan
			}
		default:
			break scan
		}
	}
	if fixedDen != "" {
		fixedDen += strings.Map(func(r rune) rune {
			if r == '0' {
				return r
			}
			return -1
		}, denPlaces)
		denPlaces = ""
	}
	hasInt := false
	for i := 0; i < numFrom; i++ {
		hasInt = hasInt || s.toks[i].kind == tDigit
	}
	ip, fr := 0.0, x
	if hasInt {
		ip = math.Floor(x)
		fr = x - ip
	}
	var num, den int64
	if fixedDen != "" {
		d, err := strconv.ParseInt(fixedDen, 10, 64)
		if err != nil || d <= 0 {
			d = 1
		}
		den = d
		num = int64(math.Floor(fr*float64(d) + 0.5))
	} else {
		maxDen := int64(1)
		for range min(len(denPlaces), 7) {
			maxDen *= 10
		}
		num, den = approximate(fr, maxDen-1)
	}
	if hasInt && num == den && den > 0 {
		ip++
		num = 0
	}
	if neg && ip == 0 && num == 0 {
		neg = false
	}
	r := Result{Numeric: true}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	ipStr := strconv.FormatFloat(ip, 'f', 0, 64)
	if ip == 0 {
		ipStr = ""
		if num == 0 {
			ipStr = "0"
		}
	}
	if hasInt {
		s.writeInt(&b, &r, ipStr, 0, numFrom, nil, false)
	}
	if hasInt && num == 0 {
		// A whole number leaves the fraction blank.
		width := 0
		for i := numFrom; i < denTo; i++ {
			switch s.toks[i].kind {
			case tDigit, tSlash:
				width++
			case tLit:
				width += len(s.toks[i].text)
			case tDot, tComma, tPercent, tExp, tDate, tAMPM, tAP, tSecFrac, tAt, tFill, tGeneral:
			}
		}
		b.WriteString(strings.Repeat(" ", width))
	} else {
		ns := strconv.FormatInt(num, 10)
		var nb strings.Builder
		s.writeInt(&nb, &r, ns, numFrom, slash, nil, false)
		b.WriteString(nb.String())
		b.WriteByte('/')
		ds := strconv.FormatInt(den, 10)
		b.WriteString(ds)
		for i := len(ds); i < len(denPlaces); i++ {
			switch denPlaces[i] {
			case '?':
				b.WriteString(figureSpace)
			case '0':
				b.WriteByte('0')
			}
		}
	}
	b.WriteString(rest)
	for i := denTo; i < len(s.toks); i++ {
		s.writeOther(&b, &r, s.toks[i])
	}
	r.Text = b.String()
	return r
}

// approximate returns the fraction closest to x in [0, 1] whose
// denominator is at most maxDen, by the convergents and semiconvergents
// of its continued fraction.
func approximate(x float64, maxDen int64) (int64, int64) {
	if maxDen < 1 {
		maxDen = 1
	}
	if x <= 0 {
		return 0, 1
	}
	p0, q0, p1, q1 := int64(0), int64(1), int64(1), int64(0)
	v := x
	bestP, bestQ := int64(math.Floor(x+0.5)), int64(1)
	bestErr := math.Abs(x - float64(bestP))
	for range 64 {
		a := math.Floor(v)
		if a > 1e12 {
			break
		}
		ai := int64(a)
		p2, q2 := ai*p1+p0, ai*q1+q0
		if q2 > maxDen {
			// The largest semiconvergent within the bound.
			k := (maxDen - q0) / q1
			if k > 0 {
				ps, qs := k*p1+p0, k*q1+q0
				if math.Abs(x-float64(ps)/float64(qs)) < bestErr {
					bestP, bestQ = ps, qs
				}
			}
			break
		}
		if e := math.Abs(x - float64(p2)/float64(q2)); e < bestErr {
			bestP, bestQ, bestErr = p2, q2, e
		}
		p0, q0, p1, q1 = p1, q1, p2, q2
		f := v - a
		if f < 1e-12 {
			break
		}
		v = 1 / f
	}
	return bestP, bestQ
}
