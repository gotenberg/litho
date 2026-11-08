package chart

import (
	"math"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/numfmt"
)

// MaxPoints caps the points of one series a reader keeps. Anti-DoS guard,
// not spec-mandated: Excel draws at most 32,000 points a series and 4,000
// in a two-dimensional chart, and a point costs a few path operations.
const MaxPoints = 32_000

// MaxSeries caps the series of one chart a reader keeps. Anti-DoS guard,
// not spec-mandated: Excel allows 255 series a chart.
const MaxSeries = 255

// ValueKind says what a [Value] holds.
type ValueKind uint8

// The value kinds.
const (
	// Empty is a missing point: a gap in a line, no bar.
	Empty ValueKind = iota
	// Number is a numeric value.
	Number
	// Text is a string, such as a category label.
	Text
)

// Value is one cached point of a series: a number, a string, or nothing.
type Value struct {
	Kind ValueKind
	// Number is the value of a Number; Text the string of a Text, or the
	// number as the cache spelled it.
	Number float64
	Text   string
}

// Num returns a numeric value.
func Num(v float64) Value { return Value{Kind: Number, Number: v} }

// Str returns a string value.
func Str(s string) Value { return Value{Kind: Text, Text: s} }

// Data is the data of one dimension of a series: its cached values, the
// range they come from, and their number format.
type Data struct {
	// Ref is the range reference the file names, as written:
	// "Sheet1!$B$2:$B$5" in SpreadsheetML, "Sheet1.B2:Sheet1.B5" in
	// OpenDocument, "" for literal data.
	Ref string
	// Values are the cached points, in order. A file whose cache is missing
	// leaves it nil and the renderer asks [Chart.Resolver].
	Values []Value
	// Cached reports that the file held a cache, even an empty one.
	Cached bool
	// Format is the number format code of the cache, "" for General.
	Format string
}

// Resolver returns the values of the range ref names, nil when it cannot.
// A spreadsheet converter supplies one to fill series whose file holds no
// cache.
type Resolver func(ref string) []Value

// Len returns the number of points.
func (d Data) Len() int { return len(d.Values) }

// number returns the numeric value of point i and whether it is one: a
// Text that parses as a number counts.
func (d Data) number(i int) (float64, bool) {
	if i < 0 || i >= len(d.Values) {
		return 0, false
	}
	v := d.Values[i]
	switch v.Kind {
	case Number:
		if math.IsNaN(v.Number) || math.IsInf(v.Number, 0) {
			return 0, false
		}
		return v.Number, true
	case Text:
		f, err := strconv.ParseFloat(v.Text, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case Empty:
	}
	return 0, false
}

// label returns point i as a category label, formatted with the data's
// format and loc's separators when it is a number.
func (d Data) label(i int, loc numfmt.Locale) string {
	if i < 0 || i >= len(d.Values) {
		return ""
	}
	v := d.Values[i]
	switch v.Kind {
	case Text:
		return v.Text
	case Number:
		return formatLabel(d.Format, v.Number, loc)
	case Empty:
	}
	return ""
}

// labelAs returns point i as a category label, a number formatted with
// code when not empty, as an axis with its own number format shows its
// numeric categories, else with the data's format.
func (d Data) labelAs(i int, code string, loc numfmt.Locale) string {
	if code != "" && i >= 0 && i < len(d.Values) && d.Values[i].Kind == Number {
		return formatLabel(code, d.Values[i].Number, loc)
	}
	return d.label(i, loc)
}

// numeric reports whether every non-empty point is a number.
func (d Data) numeric() bool {
	n := 0
	for _, v := range d.Values {
		switch v.Kind {
		case Text:
			return false
		case Number:
			n++
		case Empty:
		}
	}
	return n > 0
}
