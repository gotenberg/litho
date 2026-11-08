package ods

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// calcExt is the namespace of LibreOffice's Calc extensions, whose
// calcext:value-type tells an error value from a number.
const calcExt = "urn:org:documentfoundation:names:experimental:calc:xmlns:calcext:1.0"

// The caps of one cell's text.
const (
	// maxCellText bounds the bytes of one cell's text: 32,767, Calc's and
	// Excel's limit on a cell's characters, read as bytes. Anti-DoS
	// guard, not spec-mandated.
	maxCellText = 32767
	// maxSpaces bounds the spaces one text:s writes: 1,024. Anti-DoS
	// guard, not spec-mandated: a cell indents its text by a few.
	maxSpaces = 1024
	// maxCellRuns bounds the rich text runs of one cell: 256. Anti-DoS
	// guard, not spec-mandated.
	maxCellRuns = 256
	// maxTextDepth bounds the nesting of spans and links read in a cell:
	// 16. Anti-DoS guard, not spec-mandated.
	maxTextDepth = 16
)

// cell reads the value, text, style and link of a table:table-cell (ODF
// 1.3, Part 3, 9.1.4) and whether it is listed: it holds a value or text,
// or a style that shows on paper without one.
func (r *reader) cell(el *odf.Element) (sheet.Cell, bool) {
	c := sheet.Cell{Style: r.cellStyle(el.A(odf.NSTable, "style-name"))}
	tc := r.cellText(el, c.Style)
	c.Href = tc.href
	c.Value = value(el, tc.text)
	if len(tc.runs) > 1 || len(tc.runs) == 1 && tc.runs[0].Font != nil {
		c.Runs = tc.runs
	}
	if c.Value.Kind != sheet.Text && c.Value.Kind != sheet.Empty {
		c.Runs = nil
	}
	// Calc recalculates an OpenDocument spreadsheet as it loads it,
	// whatever program wrote it: every formula is kept for the builder
	// to compute (ODF 1.3 Part 3, 19.643 table:formula).
	//
	// # Corpus evidence
	//
	//   - LibreOffice 26.8.2.2: a cell =[.A1]+2 saved with the result 5
	//     prints 4, A1 holding 2, whether LibreOffice, Gnumeric or no
	//     program is named as the file's generator.
	if f := el.A(odf.NSTable, "formula"); f != "" && len(f) <= maxFormulaLen {
		c.Formula = f
	}
	return c, c.Value.Kind != sheet.Empty || visible(c.Style) || c.Formula != ""
}

// maxFormulaLen caps the formula text kept for a cell. Anti-DoS guard,
// not spec-mandated: Calc's formulas run to a few thousand characters.
const maxFormulaLen = 8192

// visible reports a style that draws on an empty cell: a fill, a border
// or a diagonal, which Calc prints as part of the used range.
func visible(st *sheet.Style) bool {
	if st == nil {
		return false
	}
	if st.Fill.Color.A > 0 || st.DiagonalUp || st.DiagonalDown {
		return true
	}
	for _, b := range st.Border {
		if b.Line != sheet.LineNone {
			return true
		}
	}
	return false
}

// value reads the cell's value by office:value-type (ODF 1.3, Part 3,
// 19.385) and the value attributes (19.384 to 19.391), shown is the text
// the cell's paragraphs hold. A date or time becomes Calc's serial number,
// days since 1899-12-30; calcext:value-type error the error text.
func value(el *odf.Element, shown string) sheet.Value {
	vt := el.A(odf.NSOffice, "value-type")
	if el.A(calcExt, "value-type") == "error" {
		return sheet.Value{Kind: sheet.Error, Text: shown}
	}
	num := func(s string) (float64, bool) {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
	}
	switch vt {
	case "float", "percentage", "currency":
		if v, ok := num(el.A(odf.NSOffice, "value")); ok {
			return sheet.Value{Kind: sheet.Number, Number: v, Text: shown}
		}
	case "date":
		if v, ok := dateSerial(el.A(odf.NSOffice, "date-value")); ok {
			return sheet.Value{Kind: sheet.Number, Number: v, Text: shown}
		}
	case "time":
		if v, ok := durationDays(el.A(odf.NSOffice, "time-value")); ok {
			return sheet.Value{Kind: sheet.Number, Number: v, Text: shown}
		}
	case "boolean":
		// A number stands for true unless it is zero, as Calc reads the
		// boolean-value of older files.
		b := strings.TrimSpace(el.A(odf.NSOffice, "boolean-value"))
		v := 0.0
		if n, err := strconv.ParseFloat(b, 64); b == "true" || err == nil && n != 0 {
			v = 1
		}
		return sheet.Value{Kind: sheet.Bool, Number: v, Text: shown}
	case "string":
		if s, ok := el.Lookup(odf.NSOffice, "string-value"); ok {
			return sheet.Value{Kind: sheet.Text, Text: s}
		}
		return sheet.Value{Kind: sheet.Text, Text: shown}
	}
	if shown != "" {
		return sheet.Value{Kind: sheet.Text, Text: shown}
	}
	return sheet.Value{}
}

// nullDate is Calc's day 0, 1899-12-30, from which its serial numbers
// count (ODF 1.3, Part 3, 9.4.2, table:null-date's default).
var nullDate = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// dateSerial parses an office:date-value (ODF 1.3, Part 3, 19.385, the
// XML Schema date or dateTime) to days since [nullDate].
func dateSerial(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		t, err := time.Parse(layout, strings.TrimSuffix(s, "Z"))
		if err != nil {
			continue
		}
		return t.Sub(nullDate).Hours() / 24, true
	}
	return 0, false
}

// maxDurationDays bounds a time value: 1,000,000 days. Anti-DoS guard,
// not spec-mandated: it keeps the number finite.
const maxDurationDays = 1_000_000

// durationDays parses an office:time-value, an XML Schema duration
// "PT12H30M00S" (ODF 1.3, Part 3, 19.389), to a fraction of days, signed.
func durationDays(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	rest, ok := strings.CutPrefix(s, "P")
	if !ok || rest == "" {
		return 0, false
	}
	var days float64
	inTime := false
	for rest != "" {
		if rest[0] == 'T' {
			inTime = true
			rest = rest[1:]
			continue
		}
		i := 0
		for i < len(rest) && (rest[i] >= '0' && rest[i] <= '9' || rest[i] == '.') {
			i++
		}
		if i == 0 || i == len(rest) {
			return 0, false
		}
		v, err := strconv.ParseFloat(rest[:i], 64)
		if err != nil {
			return 0, false
		}
		switch unit := rest[i]; {
		case unit == 'Y' && !inTime:
			days += v * 365
		case unit == 'M' && !inTime:
			days += v * 30
		case unit == 'D' && !inTime:
			days += v
		case unit == 'H' && inTime:
			days += v / 24
		case unit == 'M' && inTime:
			days += v / 1440
		case unit == 'S' && inTime:
			days += v / 86400
		default:
			return 0, false
		}
		if days > maxDurationDays {
			return 0, false
		}
		rest = rest[i+1:]
	}
	if neg {
		days = -days
	}
	return days, true
}

// cellText is a cell's text as its paragraphs give it.
type cellText struct {
	text string
	runs []sheet.Run
	href string
}

// textBuilder collects a cell's text and runs.
type textBuilder struct {
	r     *reader
	b     strings.Builder
	runs  []sheet.Run
	href  string
	space bool
	base  sheet.Font
}

// cellText reads the text:p children of a cell (ODF 1.3, Part 3, 9.1.4
// and 5.1.3): the paragraphs joined by line feeds, with the spans
// (6.1.7), links (6.1.8), spaces (6.1.3), tabs (6.1.4), line breaks
// (6.1.5) and the fields' shown text; white space collapsed (6.1.2).
func (r *reader) cellText(el *odf.Element, st *sheet.Style) cellText {
	tb := &textBuilder{r: r}
	switch {
	case st != nil:
		tb.base = st.Font
	default:
		tb.base = r.doc.Default.Font
	}
	first := true
	for _, p := range el.Children {
		if !p.Is(odf.NSText, "p") && !p.Is(odf.NSText, "h") {
			continue
		}
		if !first {
			tb.add("\n", nil)
		}
		first = false
		tb.space = true
		tb.inline(p, nil, 0)
	}
	return cellText{text: tb.b.String(), runs: tb.runs, href: tb.href}
}

// add appends s in the font f (nil for the cell's), merging it into the
// last run when the font is the same.
func (tb *textBuilder) add(s string, f *sheet.Font) {
	if s == "" || tb.b.Len()+len(s) > maxCellText {
		return
	}
	tb.b.WriteString(s)
	if n := len(tb.runs); n > 0 && sameFont(tb.runs[n-1].Font, f) {
		tb.runs[n-1].Text += s
		return
	}
	if len(tb.runs) >= maxCellRuns {
		tb.runs[len(tb.runs)-1].Text += s
		return
	}
	tb.runs = append(tb.runs, sheet.Run{Text: s, Font: f})
}

// sameFont reports two run fonts that draw alike.
func sameFont(a, b *sheet.Font) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Size == b.Size && a.Bold == b.Bold && a.Italic == b.Italic && a.Underline == b.Underline &&
		a.Strike == b.Strike && a.Color == b.Color && a.Script == b.Script && strings.Join(a.Families, ",") == strings.Join(b.Families, ",")
}

// inline reads the inline content of a paragraph or span in font f.
func (tb *textBuilder) inline(el *odf.Element, f *sheet.Font, depth int) {
	if depth > maxTextDepth {
		return
	}
	for _, c := range el.Children {
		if c.IsText() {
			tb.chars(c.Text, f)
			continue
		}
		switch {
		case c.Is(odf.NSText, "span"):
			tb.inline(c, tb.r.spanFont(tb.base, f, c.A(odf.NSText, "style-name")), depth+1)
		case c.Is(odf.NSText, "a"):
			if tb.href == "" {
				tb.href = c.A(odf.NSXLink, "href")
			}
			tb.inline(c, f, depth+1)
		case c.Is(odf.NSText, "s"):
			n := 1
			if v, err := strconv.Atoi(c.A(odf.NSText, "c")); err == nil && v > 1 {
				n = min(v, maxSpaces)
			}
			tb.add(strings.Repeat(" ", n), f)
			tb.space = false
		case c.Is(odf.NSText, "tab"):
			tb.add("\t", f)
			tb.space = false
		case c.Is(odf.NSText, "line-break"):
			tb.add("\n", f)
			tb.space = true
		case c.Is(odf.NSOffice, "annotation"), c.Is(odf.NSOffice, "annotation-end"):
			tb.r.warn("comments", "cell comments are not printed")
		case c.Name.Space == odf.NSDraw, c.Is(odf.NSText, "ruby-text"):
			// A ruby's annotation is drawn above its base, which Calc does
			// not print in a cell (ODF 1.3, Part 3, 6.4).
		default:
			// A field (text:sheet-name, text:date, text:page-number...)
			// shows the text the file stores.
			tb.inline(c, f, depth+1)
		}
	}
}

// chars appends character data with its white space collapsed (ODF 1.3,
// Part 3, 6.1.2): runs of spaces, tabs and line ends are one space, none
// at a paragraph's start.
func (tb *textBuilder) chars(s string, f *sheet.Font) {
	var b strings.Builder
	for _, ch := range s {
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if !tb.space {
				b.WriteByte(' ')
				tb.space = true
			}
			continue
		}
		b.WriteRune(ch)
		tb.space = false
	}
	tb.add(b.String(), f)
}
