package odf

import (
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/models"
)

// dataStyleKinds are the local names of the data style elements of ODF
// 1.3, Part 3, 16.29.
var dataStyleKinds = map[string]bool{
	"number-style": true, "percentage-style": true, "currency-style": true,
	"date-style": true, "time-style": true, "boolean-style": true, "text-style": true,
}

// maxDataStyleMaps bounds the style:map elements of one data style read:
// 8. Anti-DoS guard, not spec-mandated: a format code holds four sections
// at most (ECMA-376 Part 1, 18.8.31), and LibreOffice writes three maps
// at most.
const maxDataStyleMaps = 8

// maxFormatCode bounds the length of one translated format code: 1,024
// bytes. Anti-DoS guard, not spec-mandated: Excel caps a format code at
// 255 characters, and a data style with a long literal text past it is
// truncated.
const maxFormatCode = 1024

// DataStyles indexes the data styles of a document (ODF 1.3, Part 3,
// 16.29: number:number-style, percentage-style, currency-style,
// date-style, time-style, boolean-style and text-style) of both scopes,
// and translates one to the SpreadsheetML number format code it means
// (ECMA-376 Part 1, 18.8.31), the syntax Calc reads and writes for its own
// number formats. It is read-only once built and safe for concurrent
// reads.
type DataStyles struct {
	common map[string]*Element
	auto   [2]map[string]*Element
	// locale is the locale a style that names no language resolves what
	// it leaves open in: the system locale of LibreOffice, en-US when
	// unset.
	locale models.Locale
}

// WithLocale returns a copy of d whose styles that name no language take
// loc, LibreOffice's system locale, for what they leave to a locale: the
// default date format of a date style of automatic order, the decimal
// places of a currency style that states none, the system number format
// of a number style that states no integer digits. The copy shares d's
// index.
func (d *DataStyles) WithLocale(loc models.Locale) *DataStyles {
	c := *d
	c.locale = loc
	return &c
}

// styleLocale returns the locale of the data style el: the one its
// number:language and number:country name (ODF 1.3, Part 3, 19.351 and
// 19.348), else d's, else en-US.
func (d *DataStyles) styleLocale(el *Element) models.Locale {
	lang := el.A(NSNumber, "language")
	if c := el.A(NSNumber, "country"); lang != "" && c != "" {
		lang += "-" + c
	}
	if loc := numfmt.LocaleOf(lang); loc != "" {
		return loc
	}
	if d.locale != "" {
		return d.locale
	}
	return models.LocaleEnUS
}

// NewDataStyles indexes the data styles of p.
func NewDataStyles(p *Package) *DataStyles {
	if p.Flat {
		d := DataStylesOf(nil, p.Styles)
		d.auto[ScopeContent] = d.auto[ScopeStyles]
		return d
	}
	return DataStylesOf(p.Content, p.Styles)
}

// DataStylesOf indexes the data styles of a document given as its parsed
// content and styles documents, either nil for none: the chart document
// a spreadsheet embeds, whose data styles name the formats of its axes
// and labels, or a flat document's one tree passed as styles.
func DataStylesOf(content, styles *Element) *DataStyles {
	d := &DataStyles{common: map[string]*Element{}}
	for i := range d.auto {
		d.auto[i] = map[string]*Element{}
	}
	parts := []struct {
		el    *Element
		scope Scope
	}{{styles, ScopeStyles}, {content, ScopeContent}}
	for _, part := range parts {
		if part.el == nil {
			continue
		}
		d.add(Section(part.el, "styles"), d.common)
		d.add(Section(part.el, "automatic-styles"), d.auto[part.scope])
	}
	return d
}

// add records the data styles of sec in m, the first of a name winning.
func (d *DataStyles) add(sec *Element, m map[string]*Element) {
	if sec == nil {
		return
	}
	for _, el := range sec.Children {
		if el.Name.Space != NSNumber || !dataStyleKinds[el.Name.Local] {
			continue
		}
		name := el.A(NSStyle, "name")
		if _, dup := m[name]; !dup && name != "" {
			m[name] = el
		}
	}
}

// Element returns the data style name as scope sees it, the automatic
// one first, then the common one, then the other scope's automatic one:
// LibreOffice keeps one number formatter for both documents and resolves
// a cell's data style written among styles.xml's automatic styles,
// which ODF 1.3, Part 3, 16.4 does not let content.xml name. nil when
// none.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/formats.ods's cell style ce28 names N2, which only
//     styles.xml's automatic styles hold; Calc prints its 2 as 2.00.
func (d *DataStyles) Element(scope Scope, name string) *Element {
	if el := d.auto[scope][name]; el != nil {
		return el
	}
	if el := d.common[name]; el != nil {
		return el
	}
	return d.auto[1-scope][name]
}

// Code returns the SpreadsheetML format code of the data style name as
// scope sees it: "" for a name that resolves to none, "General" for
// LibreOffice's standard format (a number:number without decimal places,
// 16.29.3). The style:map conditions (16.3) become the code's sections,
// the conditional ones in brackets: "[>=0]" maps of the default kinds
// become the plain positive, negative and zero sections; a section's
// style:text-properties fo:color its colour code.
func (d *DataStyles) Code(scope Scope, name string) string {
	el := d.Element(scope, name)
	if el == nil {
		return ""
	}
	maps := el.All(NSStyle, "map")
	if len(maps) > maxDataStyleMaps {
		maps = maps[:maxDataStyleMaps]
	}
	type section struct{ cond, code string }
	var secs []section
	for _, m := range maps {
		sub := d.Element(scope, m.A(NSStyle, "apply-style-name"))
		if sub == nil {
			continue
		}
		cond, ok := condition(m.A(NSStyle, "condition"))
		if !ok {
			continue
		}
		secs = append(secs, section{cond, d.sectionCode(sub)})
	}
	main := d.sectionCode(el)
	if len(secs) == 0 && systemNumber(el) {
		if code := numfmt.FormatIndexCode(d.styleLocale(el), formatIndexNumberSystem); code != "" {
			main = code
		}
	}
	var b strings.Builder
	switch {
	case len(secs) == 0:
		b.WriteString(main)
	case len(secs) == 1 && secs[0].cond == ">=0":
		// value()>=0 then the rest: the positive and zero section, then the
		// negative one, as Calc writes its "negative in red" formats.
		b.WriteString(secs[0].code + ";" + main)
	case len(secs) == 2 && secs[0].cond == ">0" && secs[1].cond == "<0":
		b.WriteString(secs[0].code + ";" + secs[1].code + ";" + main)
	default:
		for _, s := range secs {
			b.WriteString("[" + s.cond + "]" + s.code + ";")
		}
		b.WriteString(main)
	}
	code := b.String()
	if len(code) > maxFormatCode {
		code = code[:maxFormatCode]
	}
	return code
}

// condition translates a style:condition, "value()>=0" (ODF 1.3, Part 3,
// 19.477), to the condition of a format code section, ">=0".
func condition(c string) (string, bool) {
	c = strings.ReplaceAll(strings.TrimSpace(c), " ", "")
	rest, ok := strings.CutPrefix(c, "value()")
	if !ok {
		return "", false
	}
	for _, op := range []string{">=", "<=", "!=", "<>", "=", ">", "<"} {
		if v, ok := strings.CutPrefix(rest, op); ok {
			if _, err := strconv.ParseFloat(v, 64); err != nil {
				return "", false
			}
			if op == "!=" {
				op = "<>"
			}
			return op + v, true
		}
	}
	return "", false
}

// excelColors names the colours a format code's colour section names
// (ECMA-376 Part 1, 18.8.31): the eight of the legacy palette's first
// entries, by their sRGB value, upper-case as LibreOffice writes them.
var excelColors = map[Color]string{
	{0, 0, 0}: "BLACK", {0, 0, 255}: "BLUE", {0, 255, 255}: "CYAN", {0, 255, 0}: "GREEN",
	{255, 0, 255}: "MAGENTA", {255, 0, 0}: "RED", {255, 255, 255}: "WHITE", {255, 255, 0}: "YELLOW",
}

// formatIndexNumberSystem is the i18n NumberFormatIndex NUMBER_SYSTEM.
const formatIndexNumberSystem = 5

// systemNumber reports a number style that LibreOffice reads as its
// locale's system number format (NUMBER_SYSTEM): one number element that
// states no number:min-integer-digits, and no text, colour or map beside
// it (SvXMLNumFormatContext::CreateAndInsert, the automatic integer
// digits).
//
// # Corpus evidence
//
//   - probe, ods: a number style of 3 decimal places and no integer
//     digits prints 1234.5 as 1,234.50 under en-US, 1.234,50 under de-DE
//     and 1 234,50 under fr-FR (TestDataStyle_CalcLocales in the ODS
//     converter).
func systemNumber(el *Element) bool {
	if el.Name.Local != "number-style" {
		return false
	}
	number := false
	for _, c := range el.Children {
		switch {
		case c.IsText():
		case c.Is(NSNumber, "number") && !number:
			if c.A(NSNumber, "min-integer-digits") != "" {
				return false
			}
			number = true
		case c.Is(NSStyle, "text-properties"):
			if _, ok := ParseColor(c.A(NSFO, "color")); ok {
				return false
			}
		case c.Is(NSStyle, "map"):
		default:
			return false
		}
	}
	return number
}

// sectionCode translates one data style element, without its maps, to a
// format code section.
func (d *DataStyles) sectionCode(el *Element) string {
	var b strings.Builder
	if c, ok := ParseColor(el.Child(NSStyle, "text-properties").A(NSFO, "color")); ok {
		if n, ok := excelColors[c]; ok {
			b.WriteString("[" + n + "]")
		}
	}
	kind := el.Name.Local
	if kind == "boolean-style" {
		return `"TRUE";"TRUE";"FALSE"`
	}
	if index, ok := defaultDateIndex(el); ok {
		if code := numfmt.FormatIndexCode(d.styleLocale(el), index); code != "" {
			return code
		}
	}
	// Minutes follow hours or precede seconds in a code; an isolated
	// minutes element of a time style is written as minutes by its
	// context alone, as Excel reads "mm" after "h".
	elapsed := el.A(NSNumber, "truncate-on-overflow") == "false"
	for _, c := range el.Children {
		if c.IsText() || c.Name.Space != NSNumber && !c.Is(NSLOExt, "fill-character") {
			continue
		}
		long := c.A(NSNumber, "style") == "long"
		switch c.Name.Local {
		case "number":
			places := -1
			if kind == "currency-style" {
				// A currency style that states no decimal places takes
				// those of its locale's default currency
				// (SvXMLNumFormatContext::AddNumber).
				places = numfmt.CurrencyDigits(d.styleLocale(el))
			}
			b.WriteString(numberCode(c, kind == "percentage-style", places))
		case "scientific-number":
			b.WriteString(scientificCode(c))
		case "fraction":
			b.WriteString(fractionCode(c))
		case "text":
			t := c.TextContent()
			if (kind == "date-style" || kind == "time-style") && (t == ":" || t == "/") {
				// The date and time separators, which LibreOffice
				// writes bare.
				b.WriteString(t)
				continue
			}
			b.WriteString(literal(t, kind == "percentage-style"))
		case "text-content":
			b.WriteString("@")
		case "currency-symbol":
			b.WriteString(currencyCode(c))
		case "fill-character":
			if r := []rune(c.TextContent()); len(r) > 0 {
				b.WriteString("*" + string(r[0]))
			}
		case "day":
			b.WriteString(pick(long, "dd", "d"))
		case "month":
			if c.A(NSNumber, "textual") == "true" {
				b.WriteString(pick(long, "mmmm", "mmm"))
			} else {
				b.WriteString(pick(long, "mm", "m"))
			}
		case "year":
			b.WriteString(pick(long, "yyyy", "yy"))
		case "day-of-week":
			b.WriteString(pick(long, "dddd", "ddd"))
		case "era":
			b.WriteString(pick(long, "gggg", "g"))
		case "quarter":
			b.WriteString(pick(long, `"1st quarter"`, `"Q1"`))
		case "hours":
			h := pick(long, "hh", "h")
			if elapsed && kind == "time-style" {
				h = "[" + h + "]"
				elapsed = false
			}
			b.WriteString(h)
		case "minutes":
			b.WriteString(pick(long, "mm", "m"))
		case "seconds":
			b.WriteString(pick(long, "ss", "s"))
			if n, err := intconv.Atoi(c.A(NSNumber, "decimal-places")); err == nil && n > 0 {
				b.WriteString("." + strings.Repeat("0", min(n, maxSecondDecimals)))
			}
		case "am-pm":
			b.WriteString("AM/PM")
		}
	}
	return b.String()
}

// maxSecondDecimals bounds the decimal places of the seconds written: 9,
// a nanosecond. Anti-DoS guard, not spec-mandated: a time value holds no
// finer part.
const maxSecondDecimals = 9

// dateLength is how a date style writes one part of a date or time
// (xmloff/source/style/xmlnumfi.cxx, SvXMLDateElementAttributes): absent,
// short, long, or a month's abbreviated or full name.
type dateLength uint8

const (
	dateNone dateLength = iota
	dateShort
	dateLong
	dateTextShort
	dateTextLong
	// dateAny matches any part but an absent one.
	dateAny
)

// defaultDate is a default date format of LibreOffice (aDefaultDateFormats
// of xmlnumfi.cxx): the i18n NumberFormatIndex of a locale's built-in
// format, the parts a date style holds to take it (day of the week, day,
// month, year, hours, minutes, seconds), and whether the style says
// number:format-source="language".
type defaultDate struct {
	index  int
	parts  [7]dateLength
	system bool
}

// defaultDates are LibreOffice's default date formats, in the order it
// tries them.
var defaultDates = []defaultDate{
	{18, [7]dateLength{dateNone, dateAny, dateAny, dateAny, dateNone, dateNone, dateNone}, true},
	{19, [7]dateLength{dateAny, dateAny, dateAny, dateAny, dateNone, dateNone, dateNone}, true},
	{34, [7]dateLength{dateNone, dateNone, dateLong, dateShort, dateNone, dateNone, dateNone}, false},
	{35, [7]dateLength{dateNone, dateLong, dateTextShort, dateNone, dateNone, dateNone, dateNone}, false},
	{21, [7]dateLength{dateNone, dateLong, dateLong, dateLong, dateNone, dateNone, dateNone}, false},
	{20, [7]dateLength{dateNone, dateLong, dateLong, dateShort, dateNone, dateNone, dateNone}, false},
	{22, [7]dateLength{dateNone, dateShort, dateTextShort, dateShort, dateNone, dateNone, dateNone}, false},
	{23, [7]dateLength{dateNone, dateShort, dateTextShort, dateLong, dateNone, dateNone, dateNone}, false},
	{25, [7]dateLength{dateNone, dateShort, dateTextLong, dateLong, dateNone, dateNone, dateNone}, false},
	{27, [7]dateLength{dateShort, dateShort, dateTextShort, dateShort, dateNone, dateNone, dateNone}, false},
	{29, [7]dateLength{dateShort, dateShort, dateTextLong, dateLong, dateNone, dateNone, dateNone}, false},
	{30, [7]dateLength{dateLong, dateShort, dateTextLong, dateLong, dateNone, dateNone, dateNone}, false},
	{50, [7]dateLength{dateNone, dateAny, dateAny, dateLong, dateAny, dateAny, dateNone}, false},
	{46, [7]dateLength{dateNone, dateAny, dateAny, dateAny, dateAny, dateAny, dateNone}, true},
	{47, [7]dateLength{dateNone, dateAny, dateAny, dateAny, dateAny, dateAny, dateAny}, false},
}

// defaultDateIndex returns the default date format a date style of
// number:automatic-order (ODF 1.3, Part 3, 19.340) stands for: when its
// parts are exactly those of one of LibreOffice's default date formats,
// LibreOffice takes that format of the style's locale, with that
// locale's order and separators, in place of the style's own
// (SvXMLNumFormatContext::CreateAndInsert). The texts between the parts
// and AM/PM decide nothing; an era, a quarter or a week makes the style
// none. ok is false for a style that stands for none, which is written as
// its elements come.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf162177_Eastersunday.fods's "dd.mm.yy" style of
//     automatic order prints 03/31/24, and sc/tdf125099.ods's
//     "dd-mm-yyyy" prints 04/17/2019.
//   - probe, ods: under 37 system locales, 23 date styles of automatic
//     order (TestDataStyle_CalcLocales in the ODS converter): "dd.mm.yyyy"
//     prints 12/09/2025 under en-US, 09.12.2025 under de-DE, 2025/12/09
//     under ja-JP; "d.m.yy", which matches no default format, prints
//     9.12.25 under every locale, and with format-source="language" the
//     locale's short date (12/9/25, 09.12.25); a style of de-DE prints
//     09.12.2025 under every locale.
func defaultDateIndex(el *Element) (int, bool) {
	if el.Name.Local != "date-style" || el.A(NSNumber, "automatic-order") != "true" {
		return 0, false
	}
	var parts [7]dateLength
	for _, c := range el.Children {
		if c.IsText() || c.Name.Space != NSNumber {
			continue
		}
		length := dateShort
		if c.A(NSNumber, "style") == "long" {
			length = dateLong
		}
		var part int
		switch c.Name.Local {
		case "day-of-week":
			part = 0
		case "day":
			part = 1
		case "month":
			part = 2
			if c.A(NSNumber, "textual") == "true" {
				length += dateTextShort - dateShort
			}
		case "year":
			part = 3
		case "hours":
			part = 4
		case "minutes":
			part = 5
		case "seconds":
			part = 6
		case "text", "am-pm", "text-content", "fill-character":
			continue
		default:
			return 0, false
		}
		parts[part] = length
	}
	system := el.A(NSNumber, "format-source") == "language"
	for _, d := range defaultDates {
		if d.system != system {
			continue
		}
		match := true
		for i, want := range d.parts {
			if parts[i] != want && (want != dateAny || parts[i] == dateNone) {
				match = false
				break
			}
		}
		if match {
			return d.index, true
		}
	}
	return 0, false
}

// pick returns a when cond holds, else b.
func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// maxDigits bounds the digits of one number element written: 30.
// Anti-DoS guard, not spec-mandated: a double holds 17 significant
// digits.
const maxDigits = 30

// attrInt returns the non-negative integer attribute space:local of el,
// at most maxDigits, and whether it is present.
func attrInt(el *Element, space, local string) (int, bool) {
	n, err := intconv.Atoi(el.A(space, local))
	if err != nil || n < 0 {
		return 0, false
	}
	return min(n, maxDigits), true
}

// integerCode writes the integer digits of a number element: n zeros,
// grouped by thousands with a comma when grouping is set ("#,##0").
func integerCode(n int, grouping bool) string {
	digits := strings.Repeat("0", n)
	if !grouping {
		if n == 0 {
			return "#"
		}
		return digits
	}
	if len(digits) < 4 {
		digits = strings.Repeat("#", 4-len(digits)) + digits
	}
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// decimalCode writes the decimal part: min required digits as zeros, the
// rest up to places as "#".
func decimalCode(places, minPlaces int) string {
	if places <= 0 {
		return ""
	}
	minPlaces = min(minPlaces, places)
	return "." + strings.Repeat("0", minPlaces) + strings.Repeat("#", places-minPlaces)
}

// minDecimals returns the minimal decimal places of a number element: the
// ODF 1.3 number:min-decimal-places, else LibreOffice's earlier
// loext:min-decimal-places, else all of places.
func minDecimals(el *Element, places int) int {
	if n, ok := attrInt(el, NSNumber, "min-decimal-places"); ok {
		return n
	}
	if n, ok := attrInt(el, NSLOExt, "min-decimal-places"); ok {
		return n
	}
	return places
}

// numberCode translates number:number (ODF 1.3, Part 3, 16.29.3). A
// number without decimal places and nothing else is the General format,
// unless defaultPlaces, the decimal places of a currency style's locale,
// is not negative: it then takes those. The embedded texts (16.29.4) are
// written among the integer digits.
func numberCode(el *Element, percent bool, defaultPlaces int) string {
	places, hasPlaces := attrInt(el, NSNumber, "decimal-places")
	if !hasPlaces && defaultPlaces >= 0 {
		places, hasPlaces = defaultPlaces, true
	}
	ints, _ := attrInt(el, NSNumber, "min-integer-digits")
	grouping := el.A(NSNumber, "grouping") == "true"
	factor := el.A(NSNumber, "display-factor")
	embedded := el.All(NSNumber, "embedded-text")
	if len(embedded) > maxEmbeddedTexts {
		embedded = embedded[:maxEmbeddedTexts]
	}
	if !hasPlaces && !grouping && ints <= 1 && factor == "" && len(embedded) == 0 && !percent {
		return "General"
	}
	code := integerCode(ints, grouping)
	if len(embedded) > 0 {
		code = embed(code, embedded)
	}
	code += decimalCode(places, minDecimals(el, places))
	if f, err := strconv.ParseFloat(factor, 64); err == nil {
		for ; f >= 1000-1e-9 && len(code) < maxFormatCode; f /= 1000 {
			code += ","
		}
	}
	return code
}

// maxEmbeddedTexts bounds the number:embedded-text elements of one number
// element written: 64. Anti-DoS guard, not spec-mandated: each insertion
// copies the code written so far, so the work grows with their square,
// and a format code of [maxFormatCode] bytes holds far fewer.
const maxEmbeddedTexts = 64

// embed inserts the number:embedded-text elements into the integer
// digits code, each at its number:position, the digits counted from the
// decimal point.
func embed(code string, texts []*Element) string {
	digits := []rune(code)
	for _, text := range slices.Backward(texts) {
		pos, ok := attrInt(text, NSNumber, "position")
		if !ok {
			continue
		}
		at, seen := len(digits), 0
		for at > 0 && seen < pos {
			at--
			if digits[at] != ',' {
				seen++
			}
		}
		lit := []rune(literal(text.TextContent(), false))
		digits = append(digits[:at], append(lit, digits[at:]...)...)
	}
	return string(digits)
}

// scientificCode translates number:scientific-number (ODF 1.3, Part 3,
// 16.29.6): "0.00E+00".
func scientificCode(el *Element) string {
	places, _ := attrInt(el, NSNumber, "decimal-places")
	ints, _ := attrInt(el, NSNumber, "min-integer-digits")
	code := integerCode(max(ints, 1), el.A(NSNumber, "grouping") == "true")
	if iv, ok := attrInt(el, NSNumber, "exponent-interval"); ok && iv > 1 && iv > len(code) {
		code = strings.Repeat("#", iv-len(code)) + code
	}
	code += decimalCode(places, minDecimals(el, places))
	exp, ok := attrInt(el, NSNumber, "min-exponent-digits")
	if !ok {
		exp = 2
	}
	// number:forced-exponent-sign false shows the exponent's sign only
	// when it is negative (ODF 1.4, 19.348.2; LibreOffice since 7.6).
	sign := "E+"
	if el.A(NSNumber, "forced-exponent-sign") == "false" || el.A(NSLOExt, "forced-exponent-sign") == "false" {
		sign = "E-"
	}
	return code + sign + strings.Repeat("0", max(exp, 1))
}

// fractionCode translates number:fraction (ODF 1.3, Part 3, 16.29.7):
// "# ?/?", "0/8".
func fractionCode(el *Element) string {
	var b strings.Builder
	if ints, ok := attrInt(el, NSNumber, "min-integer-digits"); ok {
		if ints == 0 {
			b.WriteString("# ")
		} else {
			b.WriteString(integerCode(ints, el.A(NSNumber, "grouping") == "true") + " ")
		}
	}
	num, ok := attrInt(el, NSNumber, "min-numerator-digits")
	if !ok || num == 0 {
		num = 1
	}
	b.WriteString(strings.Repeat("?", num) + "/")
	if dv, ok := attrInt(el, NSNumber, "denominator-value"); ok && dv > 0 {
		b.WriteString(strconv.Itoa(dv))
		return b.String()
	}
	den, ok := attrInt(el, NSNumber, "min-denominator-digits")
	if !ok || den == 0 {
		den = 1
	}
	if mx, ok := attrInt(el, NSNumber, "max-denominator-value"); ok && mx > 0 {
		den = max(den, len(strconv.Itoa(mx)))
	}
	b.WriteString(strings.Repeat("?", den))
	return b.String()
}

// lcids maps a language and country to the Windows locale identifier a
// currency tag carries ([MS-LCID], 2.2), for the locales LibreOffice's
// currency table lists most.
var lcids = map[string]int{
	"en-US": 0x409, "en-GB": 0x809, "en-AU": 0xc09, "en-CA": 0x1009, "en-IE": 0x1809, "en-IN": 0x4009,
	"de-DE": 0x407, "de-AT": 0xc07, "de-CH": 0x807, "fr-FR": 0x40c, "fr-BE": 0x80c, "fr-CA": 0xc0c,
	"fr-CH": 0x100c, "it-IT": 0x410, "es-ES": 0xc0a, "es-MX": 0x80a, "nl-NL": 0x413, "nl-BE": 0x813,
	"pt-PT": 0x816, "pt-BR": 0x416, "sv-SE": 0x41d, "da-DK": 0x406, "nb-NO": 0x414, "fi-FI": 0x40b,
	"pl-PL": 0x415, "cs-CZ": 0x405, "hu-HU": 0x40e, "ru-RU": 0x419, "uk-UA": 0x422, "tr-TR": 0x41f,
	"el-GR": 0x408, "ja-JP": 0x411, "zh-CN": 0x804, "zh-TW": 0x404, "ko-KR": 0x412, "he-IL": 0x40d,
	"ar-SA": 0x401, "hi-IN": 0x439, "th-TH": 0x41e,
}

// currencyCode translates number:currency-symbol (ODF 1.3, Part 3,
// 16.29.10) to a currency tag, "[$€-407]", the locale written when the
// element names a known one.
func currencyCode(el *Element) string {
	sym := strings.NewReplacer("]", "", "[", "").Replace(el.TextContent())
	tag := el.A(NSNumber, "language") + "-" + el.A(NSNumber, "country")
	if id, ok := lcids[tag]; ok {
		return "[$" + sym + "-" + strings.ToUpper(strconv.FormatInt(int64(id), 16)) + "]"
	}
	return "[$" + sym + "]"
}

// literal writes text as format code literal characters as LibreOffice's
// XLSX export writes them: one character escaped with a backslash, more
// in quotes. In a percentage style a "%" stays a percent sign, which
// scales the value.
func literal(text string, percent bool) string {
	var b strings.Builder
	var run []rune
	flush := func() {
		switch len(run) {
		case 0:
		case 1:
			b.WriteString(`\` + string(run))
		default:
			b.WriteString(`"` + strings.ReplaceAll(string(run), `"`, `\"`) + `"`)
		}
		run = run[:0]
	}
	for _, r := range text {
		if r == '%' && percent {
			flush()
			b.WriteRune(r)
			continue
		}
		run = append(run, r)
	}
	flush()
	return b.String()
}
