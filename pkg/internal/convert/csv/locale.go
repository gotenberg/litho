package csv

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/models"
)

// localeReader reads the fields of a CSV file as Calc's Standard column
// type does under a locale other than en-US (svl/source/numbers/
// zforfind.cxx, ImpSvNumberInputScan): numbers in the locale's
// separators, its currency, its boolean words, dates in the order of its
// date acceptance patterns and with its month names, times with its
// markers; a value takes the locale's built-in format of its kind.
type localeReader struct {
	loc  models.Locale
	info numfmt.LocaleInfo
	// formats are the formats of the kinds, parsed once.
	formats [kTimeFraction + 1]*numfmt.Format
}

// newLocaleReader returns the reader of loc, nil for the default locale,
// which [detect] reads.
func newLocaleReader(loc models.Locale) *localeReader {
	if numfmt.IsDefault(loc) {
		return nil
	}
	info, ok := numfmt.Info(loc)
	if !ok {
		return nil
	}
	r := &localeReader{loc: loc, info: info}
	for k := range r.formats {
		f, err := numfmt.Parse(r.code(kind(k)))
		if err != nil {
			f = numfmt.General
		}
		r.formats[k] = f.WithLocale(loc)
	}
	return r
}

// code returns the format code of kind k under the locale: its built-in
// format of that kind, en-US's where the locale states none.
func (r *localeReader) code(k kind) string {
	c := -1
	switch k {
	case kPercent:
		c = numfmt.CodePercentDefault
	case kCurrency:
		c = numfmt.CodeCurrencyDefault
	case kScientific:
		c = numfmt.CodeScientificDefault
	case kDateUS:
		c = numfmt.CodeDate
	case kDateISO:
		c = numfmt.CodeDateISO
	case kDateTimeUS:
		c = numfmt.CodeDateTimeDefault
	case kTime:
		c = numfmt.CodeTime
	case kElapsed:
		c = numfmt.CodeTimeElapsed
	case kTimeFraction:
		c = numfmt.CodeTimeElapsed00
	case kText, kGeneral, kBool, kDateTimeISO, kDateTimeISOT:
	}
	if c >= 0 {
		if code := numfmt.Code(r.loc, c); code != "" {
			return code
		}
	}
	return formatCodes[k]
}

// display returns the text the locale shows v in the format of kind k.
func (r *localeReader) display(k kind, v float64) string {
	val := numfmt.Value{Kind: numfmt.Number, Number: v}
	if k == kBool {
		val.Kind = numfmt.Bool
	}
	return r.formats[k].Apply(val).Text
}

// detect reads a field as Calc does under the locale. year is the year a
// date without one takes.
func (r *localeReader) detect(field string, year int) detected {
	s := strings.TrimSpace(field)
	if s == "" {
		return detected{}
	}
	switch {
	case strings.EqualFold(s, r.info.True):
		return detected{kind: kBool, value: 1}
	case strings.EqualFold(s, r.info.False):
		return detected{kind: kBool, value: 0}
	}
	if d, ok := r.number(s); ok {
		return d
	}
	if d, ok := r.dateTime(s, year); ok {
		return d
	}
	return detected{}
}

// spaceGroup reports a group separator a space stands for: a no-break
// space, for which Calc takes a plain space too (zforfind.cxx,
// GetThousandSep).
func (r *localeReader) spaceGroup() bool {
	return r.info.Group == " " || r.info.Group == " "
}

// number reads a number in the locale's syntax: a sign before or a minus
// after, parentheses for a negative, the currency symbol before or after,
// digits grouped by the group separator or not, the decimal separator, an
// exponent, a percent sign after.
func (r *localeReader) number(s string) (detected, bool) {
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg, s = true, strings.TrimSpace(s[1:len(s)-1])
	}
	k := kGeneral
	if p, ok := strings.CutSuffix(s, "%"); ok {
		k, s = kPercent, strings.TrimSpace(p)
	}
	sign := func() {
		if v, ok := strings.CutPrefix(s, "-"); ok {
			s, neg = strings.TrimSpace(v), true
		} else if v, ok := strings.CutPrefix(s, "+"); ok {
			s = strings.TrimSpace(v)
		}
	}
	if !neg {
		sign()
	}
	if cur := r.info.Currency; cur != "" {
		if v, ok := strings.CutPrefix(s, cur); ok {
			if k != kGeneral {
				return detected{}, false
			}
			k, s = kCurrency, strings.TrimSpace(v)
			if !neg {
				sign()
			}
		} else if v, ok := strings.CutSuffix(s, cur); ok {
			if k != kGeneral {
				return detected{}, false
			}
			k, s = kCurrency, strings.TrimSpace(v)
		}
	}
	if v, ok := strings.CutSuffix(s, "-"); ok && !neg {
		neg, s = true, v
	}
	mant, exp, hasExp := cutExponent(s)
	if hasExp {
		if k != kGeneral {
			return detected{}, false
		}
		k = kScientific
		e := strings.TrimPrefix(strings.TrimPrefix(exp, "+"), "-")
		if e == "" || strings.Trim(e, "0123456789") != "" {
			return detected{}, false
		}
	}
	intPart, frac, hasFrac := strings.Cut(mant, r.info.Decimal)
	if !hasFrac && r.info.DecimalAlt != "" {
		intPart, frac, hasFrac = strings.Cut(mant, r.info.DecimalAlt)
	}
	if hasFrac && strings.Trim(frac, "0123456789") != "" || intPart == "" && frac == "" {
		return detected{}, false
	}
	digits, ok := r.ungroup(intPart, hasExp)
	if !ok {
		return detected{}, false
	}
	num := digits + "." + frac
	if digits == "" {
		num = "0." + frac
	}
	if hasExp {
		num += "e" + exp
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(num, "."), 64)
	if err != nil || !numeric.Finite(v) {
		return detected{}, false
	}
	if neg {
		v = -v
	}
	if v == 0 {
		v = 0
	}
	if k == kPercent {
		v /= 100
	}
	return detected{kind: k, value: v}, true
}

// cutExponent cuts a number at its exponent letter.
func cutExponent(s string) (string, string, bool) {
	i := strings.IndexAny(s, "eE")
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
}

// ungroup returns the digits of an integer part written with the locale's
// group separators: each group after the first holds three digits, or two
// before the last in a locale that groups so (zforfind.cxx,
// GetThousandSep).
func (r *localeReader) ungroup(intPart string, hasExp bool) (string, bool) {
	seps := []string{r.info.Group}
	if r.spaceGroup() {
		seps = append(seps, " ")
	}
	var groups []string
	rest := intPart
	for {
		i, n := -1, 0
		for _, sep := range seps {
			if j := strings.Index(rest, sep); j >= 0 && (i < 0 || j < i) {
				i, n = j, len(sep)
			}
		}
		if i < 0 {
			groups = append(groups, rest)
			break
		}
		groups = append(groups, rest[:i])
		rest = rest[i+n:]
	}
	if len(groups) > 1 {
		if hasExp || groups[0] == "" {
			return "", false
		}
		for _, g := range groups[1:] {
			if len(g) != 3 && (!r.info.Indian || len(g) != 2) {
				return "", false
			}
		}
	}
	d := strings.Join(groups, "")
	if strings.Trim(d, "0123456789") != "" {
		return "", false
	}
	return d, true
}

// dateTime reads a date, a time, or a date and a time: ISO 8601 dates, the
// numeric dates the locale's acceptance patterns allow, dates with its
// month names in its order, and times with its markers.
func (r *localeReader) dateTime(s string, year int) (detected, bool) {
	if t, k, ok := r.time(s); ok {
		return detected{kind: k, value: t}, true
	}
	datePart, timePart := s, ""
	if i := strings.LastIndexByte(s, ' '); i > 0 {
		if _, _, ok := r.time(s[i+1:]); ok {
			datePart, timePart = strings.TrimSpace(s[:i]), s[i+1:]
		}
	}
	var day float64
	k := kDateUS
	switch {
	case isoDate(datePart, &day):
		k = kDateISO
		if timePart != "" {
			k = kDateTimeISO
		}
	case r.numericDate(datePart, year, &day), r.namedDate(datePart, &day):
		if timePart != "" {
			k = kDateTimeUS
		}
	default:
		return detected{}, false
	}
	if timePart != "" {
		t, _, _ := r.time(timePart)
		if t >= 1 {
			return detected{}, false
		}
		day += t
	}
	return detected{kind: k, value: day}, true
}

// numericDate reads a date of numbers as Calc does (zforfind.cxx,
// IsAcceptedDatePattern and GetDateRef): numbers whose separators are a
// pattern's, a trailing one included, in that pattern's order ("D.M."
// takes 13.1., "M/D" takes 1/13, "M/D/Y" takes 1/13/2026); a year after
// a pattern of two numbers, behind the locale's date separator, in the
// locale's own date order ("D.M." takes 1.2.2026 in de-DE).
func (r *localeReader) numericDate(s string, year int, day *float64) bool {
	nums, seps, ok := splitNumbers(s)
	if !ok || len(nums) < 2 || len(nums) > 3 {
		return false
	}
	order := ""
	for _, p := range r.info.Acceptance {
		pk, ps, ok := splitPattern(p)
		if !ok || seps[0] != ps[0] {
			continue
		}
		switch {
		case len(nums) == 2 && len(pk) == 2 && slices.Equal(seps, ps):
			order = pk
		case len(nums) == 3 && len(pk) == 3 && slices.Equal(seps, ps):
			order = pk
		case len(nums) == 3 && len(pk) == 2 && len(seps) == 2 && len(ps) <= 2 && seps[1] == r.info.DateSep && (len(ps) == 1 || ps[1] == seps[1]):
			order = r.info.Order
		default:
			continue
		}
		break
	}
	if order == "" {
		return false
	}
	d, m, y, yDigits := 0, 0, year, 4
	for i, n := range nums {
		v, ok := atoi(n, 4)
		if !ok {
			return false
		}
		switch order[i] {
		case 'D':
			if len(n) > 2 {
				return false
			}
			d = v
		case 'M':
			if len(n) > 2 {
				return false
			}
			m = v
		case 'Y':
			y, yDigits = v, len(n)
		}
	}
	if yDigits == 3 {
		return false
	}
	v, ok := serial(fullYear(y, yDigits), m, d)
	if ok {
		*day = v
	}
	return ok
}

// splitNumbers splits a date of numbers into its numbers and the
// separators between them; a trailing separator is kept.
func splitNumbers(s string) ([]string, []string, bool) {
	var nums, seps []string
	i := 0
	for i < len(s) {
		j := i
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		if j == i {
			return nil, nil, false
		}
		nums = append(nums, s[i:j])
		if j == len(s) {
			break
		}
		k := j
		for k < len(s) && !isDigit(s[k]) {
			k++
		}
		seps = append(seps, s[j:k])
		i = k
	}
	return nums, seps, len(nums) > 0
}

// splitPattern splits an acceptance pattern into its letters and its
// separators.
func splitPattern(p string) (string, []string, bool) {
	var letters strings.Builder
	var seps []string
	sep := ""
	for _, c := range p {
		switch c {
		case 'D', 'M', 'Y':
			if letters.Len() > 0 {
				seps = append(seps, sep)
			}
			sep = ""
			letters.WriteRune(c)
		default:
			sep += string(c)
		}
	}
	if sep != "" {
		seps = append(seps, sep)
	}
	return letters.String(), seps, letters.Len() >= 2
}

// namedDate reads a date with a month name of the locale: day, month and
// year, "13. Januar 2026" or "13 janv. 2026", in a locale whose dates do
// not put the month first; month, day and year, "Jan 13, 2026", in one
// that does. A day followed by a full stop is a day only where the full
// stop separates dates.
func (r *localeReader) namedDate(s string, day *float64) bool {
	f := strings.FieldsFunc(s, func(c rune) bool { return c == ' ' || c == ',' })
	if len(f) != 3 {
		return false
	}
	dTok, mTok, yTok := f[0], f[1], f[2]
	if r.info.Order == "MDY" {
		mTok, dTok = f[0], f[1]
	}
	if d, ok := strings.CutSuffix(dTok, "."); ok && r.info.DateSep == "." {
		dTok = d
	}
	m := r.month(mTok)
	d, ok1 := atoi(dTok, 2)
	y, ok2 := atoi(yTok, 4)
	if m == 0 || !ok1 || !ok2 || len(yTok) == 3 {
		return false
	}
	v, ok := serial(fullYear(y, len(yTok)), m, d)
	if ok {
		*day = v
	}
	return ok
}

// month returns the month (1 to 12) a word names in any of the locale's
// cases, abbreviated or full, in any letter case; 0 for none.
func (r *localeReader) month(w string) int {
	if w == "" || !utf8.ValidString(w) || strings.IndexFunc(w, unicode.IsLetter) < 0 {
		return 0
	}
	lists := []*[12][2]string{&r.info.Months, r.info.Genitive, r.info.Partitive}
	for _, names := range lists {
		if names == nil {
			continue
		}
		for i, n := range names {
			if strings.EqualFold(w, n[0]) || strings.EqualFold(w, n[1]) {
				return i + 1
			}
		}
	}
	return 0
}

// time reads "H:MM", "H:MM:SS" and "H:MM:SS<sep>f" in the locale's
// separators, with its AM or PM marker after: the fraction of a day and
// its kind. Hours past 23 without a marker are a duration.
func (r *localeReader) time(s string) (float64, kind, bool) {
	ampm := ""
	lower := strings.ToLower(s)
	for _, m := range []struct{ word, tag string }{{r.info.AM, "a"}, {r.info.PM, "p"}} {
		w := strings.ToLower(m.word)
		if w != "" && strings.HasSuffix(lower, w) {
			ampm, s = m.tag, strings.TrimSpace(s[:len(s)-len(w)])
			break
		}
	}
	f := strings.Split(s, r.info.TimeSep)
	if len(f) < 2 || len(f) > 3 {
		return 0, 0, false
	}
	h, ok1 := atoi(f[0], 4)
	mi, ok2 := atoi(f[1], 2)
	if !ok1 || !ok2 || len(f[1]) != 2 || mi > 59 {
		return 0, 0, false
	}
	sec, frac := 0.0, false
	if len(f) == 3 {
		whole, fr, hasFr := strings.Cut(f[2], r.info.Time100Sep)
		if !hasFr && r.info.DecimalAlt != "" {
			whole, fr, hasFr = strings.Cut(f[2], r.info.DecimalAlt)
		}
		sv, ok := atoi(whole, 2)
		if !ok || len(whole) != 2 || sv > 59 {
			return 0, 0, false
		}
		sec = float64(sv)
		if hasFr {
			if fr == "" || strings.Trim(fr, "0123456789") != "" || len(fr) > 9 {
				return 0, 0, false
			}
			v, err := strconv.ParseFloat("0."+fr, 64)
			if err != nil {
				return 0, 0, false
			}
			sec += v
			frac = true
		}
	}
	k := kTime
	switch {
	case ampm != "":
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		h %= 12
		if ampm == "p" {
			h += 12
		}
	case frac:
		k = kTimeFraction
	case h > 23:
		k = kElapsed
	}
	return (float64(float64(h)*3600) + float64(float64(mi)*60) + sec) / 86400, k, true
}

// hasYear reports a date field that names its year: three numbers, or a
// month name and two.
func (r *localeReader) hasYear(f string) bool {
	nums, _, ok := splitNumbers(strings.TrimSpace(f))
	return !ok || len(nums) != 2
}
