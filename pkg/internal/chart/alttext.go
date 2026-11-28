package chart

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The bounds of a description [AltText] writes. Each is an anti-DoS guard
// and a courtesy to the reader, not spec-mandated: a chart's file may name
// thousands of series and categories, each as long as a cell holds, and a
// screen reader reads an alternate description whole, while a reader
// grasps a chart from its kind, its title, its first few series and its
// first and last category.
const (
	// maxAltSeriesNames is the number of series names a description
	// lists; the rest are counted.
	maxAltSeriesNames = 5
	// maxAltNameRunes is the length, in characters, of one name, label or
	// axis title in a description, past which it is cut with an ellipsis.
	maxAltNameRunes = 40
	// maxAltTitleRunes is the length of the chart's title in a
	// description.
	maxAltTitleRunes = 150
	// maxAltAxes is the number of axis titles a description names.
	maxAltAxes = 4
	// maxAltRunes is the length of a whole description, past which it is
	// cut at a word with an ellipsis.
	maxAltRunes = 600
)

// AltText returns a description of c for a reader who cannot see it,
// derived from what the model holds of it: an English sentence of fixed
// grammar, "" for a nil chart. It invents nothing: each part states what
// the file names, and a part the file leaves to the application (a
// placeholder title, a series name such as "Series1") is left out.
//
// The grammar, where [x] is optional and N a count written in digits:
//
//	description = [title ". "] kinds [", " series] [", " categories] *(", " axis)
//	kinds       = Kind " chart"                    one kind of chart
//	            | "Combination chart of " list     its kinds, in drawing order
//	            | "Chart"                          no kind the file states
//	series      = N " series" [" (" names ")"]
//	names       = name *(", " name) [" and " N " more"] [" and " N " unnamed"]
//	categories  = N (" category" | " categories") [" (" first [(" and " | " to ") last] ")"]
//	axis        = side " axis " axis-title
//
// A kind reads as Office names it: its grouping ("clustered", "stacked",
// "100% stacked") and its noun ("column" for vertical bars, "bar" for
// horizontal ones, "line", "area", "pie", "doughnut", "scatter",
// "bubble", "radar" or "filled radar", "stock", "surface", "histogram",
// "waterfall", "box and whisker", "funnel", "treemap", "sunburst"),
// "3-D" first for a three-dimensional group. The title ends with its own
// mark when it ends with one. Categories are counted and their first and
// last label named, as the category axis formats them, for every kind
// but scatter, bubble and histogram, whose points are values, not
// categories; a box and whisker chart counts each category once. A side is "horizontal", "vertical", "top" or "right", by
// where the axis is drawn, or "depth" for the series axis of a
// three-dimensional chart.
//
// Each name, label and axis title is cut at [maxAltNameRunes] characters,
// the title at [maxAltTitleRunes], the list of names at
// [maxAltSeriesNames] and the axes at [maxAltAxes]; the description is
// cut at [maxAltRunes] characters.
func AltText(c *Chart) string {
	if c == nil {
		return ""
	}
	c = live(c)
	var b strings.Builder
	if c.Title != nil && !c.Title.Auto {
		if t := cut(collapse(c.Title.Text), maxAltTitleRunes); t != "" {
			b.WriteString(t)
			if r, _ := utf8.DecodeLastRuneInString(t); r == '.' || r == '!' || r == '?' || r == ':' || r == '\u2026' {
				b.WriteString(" ")
			} else {
				b.WriteString(". ")
			}
		}
	}
	b.WriteString(capitalized(kindsPhrase(c)))
	if s := seriesPhrase(c); s != "" {
		b.WriteString(", ")
		b.WriteString(s)
	}
	if s := categoriesPhrase(c); s != "" {
		b.WriteString(", ")
		b.WriteString(s)
	}
	named := 0
	for _, a := range c.Axes {
		if a == nil || a.Deleted || a.Title == nil || a.Title.Auto {
			continue
		}
		t := cut(collapse(a.Title.Text), maxAltNameRunes)
		if t == "" {
			continue
		}
		if named == maxAltAxes {
			break
		}
		named++
		b.WriteString(", ")
		b.WriteString(axisSide(a))
		b.WriteString(" axis ")
		b.WriteString(t)
	}
	return cutWords(b.String(), maxAltRunes)
}

// kindsPhrase returns the kinds part of [AltText]'s grammar.
func kindsPhrase(c *Chart) string {
	var kinds []string
	add := func(k string) {
		for _, have := range kinds {
			if have == k {
				return
			}
		}
		kinds = append(kinds, k)
	}
	for _, g := range c.Groups {
		if g == nil {
			continue
		}
		if k := groupKind(g); k != "" {
			add(k)
		}
	}
	if c.Surface {
		add("surface")
	}
	switch len(kinds) {
	case 0:
		return "chart"
	case 1:
		return kinds[0] + " chart"
	}
	return "combination chart of " + joinAnd(kinds)
}

// groupKind returns the name of the kind g draws, "" for a kind the file
// states that the reader does not know ([Unsupported]).
func groupKind(g *Group) string {
	noun := ""
	grouped := false
	switch g.Kind {
	case Bar:
		if binnedGroup(g) {
			return "histogram"
		}
		noun, grouped = "column", true
		if g.Horizontal {
			noun = "bar"
		}
	case Line:
		noun, grouped = "line", true
		if g.Stock {
			noun, grouped = "stock", false
		}
	case Area:
		noun, grouped = "area", true
	case Pie:
		noun = "pie"
	case Doughnut:
		noun = "doughnut"
	case Scatter:
		noun = "scatter"
	case Bubble:
		noun = "bubble"
	case Radar:
		noun = "radar"
		if g.Filled {
			noun = "filled radar"
		}
	case Waterfall:
		return "waterfall"
	case BoxWhisker:
		return "box and whisker"
	case Funnel:
		return "funnel"
	case Treemap:
		return "treemap"
	case Sunburst:
		return "sunburst"
	case Surface:
		return "surface"
	case Unsupported:
		return ""
	}
	if grouped {
		switch g.Grouping {
		case Stacked:
			noun = "stacked " + noun
		case PercentStacked:
			noun = "100% stacked " + noun
		case Clustered:
			// A clustered bar names itself so, as Office does; a line or
			// an area of the standard grouping takes no word, and nor
			// does a three-dimensional bar whose series stand in rows.
			if g.Kind == Bar && (!g.ThreeD || !g.Rows) {
				noun = "clustered " + noun
			}
		}
	}
	if g.ThreeD {
		noun = "3-D " + noun
	}
	return noun
}

// binnedGroup reports whether g draws its series as histograms, counting
// their values into bins.
func binnedGroup(g *Group) bool {
	for _, s := range g.Series {
		if s != nil && s.Bins != nil {
			return true
		}
	}
	return false
}

// seriesPhrase returns the series part of [AltText]'s grammar, "" for a
// chart without series.
func seriesPhrase(c *Chart) string {
	total, unnamed := 0, 0
	var names []string
	for _, g := range c.Groups {
		if g == nil {
			continue
		}
		for _, s := range g.Series {
			if s == nil {
				continue
			}
			total++
			name := cut(collapse(s.Name), maxAltNameRunes)
			if s.Unnamed || name == "" {
				unnamed++
				continue
			}
			names = append(names, name)
		}
	}
	if total == 0 {
		return ""
	}
	out := strconv.Itoa(total) + " series"
	if len(names) == 0 {
		return out
	}
	listed := names[:min(len(names), maxAltSeriesNames)]
	var parts []string
	parts = append(parts, listed...)
	if more := len(names) - len(listed); more > 0 {
		parts = append(parts, strconv.Itoa(more)+" more")
	}
	if unnamed > 0 {
		parts = append(parts, strconv.Itoa(unnamed)+" unnamed")
	}
	return out + " (" + joinAnd(parts) + ")"
}

// categoriesPhrase returns the categories part of [AltText]'s grammar, ""
// for a chart whose first group plots values rather than categories, or
// that has none.
func categoriesPhrase(c *Chart) string {
	var first *Group
	for _, g := range c.Groups {
		if g != nil {
			first = g
			break
		}
	}
	if first == nil {
		return ""
	}
	switch first.Kind {
	case Scatter, Bubble:
		return ""
	case Bar, Line, Area, Pie, Doughnut, Radar, Unsupported, Waterfall, BoxWhisker, Funnel, Treemap, Sunburst, Surface:
	}
	if binnedGroup(first) {
		return ""
	}
	n := 0
	var labels Data
	for _, g := range c.Groups {
		if g == nil {
			continue
		}
		for _, s := range g.Series {
			if s == nil {
				continue
			}
			cats, vals := c.data(s.Categories), c.data(s.Values)
			n = max(n, min(max(cats.Len(), vals.Len()), MaxPoints))
			if labels.Len() == 0 && cats.Len() > 0 {
				labels = cats
			}
		}
	}
	// A label reads as the category axis shows it: in the axis's number
	// format, a source-linked axis's cells' format, else the data's.
	format := ""
	if a := c.axisAt(first.XAxis); a != nil {
		format = a.Format
		if format == "" && a.SourceLinked && c.FormatResolver != nil && labels.Ref != "" {
			format, _ = c.FormatResolver(labels.Ref)
		}
	}
	label := func(i int) string { return cut(collapse(labels.labelAs(i, format, c.Locale)), maxAltNameRunes) }
	m := min(labels.Len(), n)
	var head, tail string
	if m > 0 {
		head, tail = label(0), label(m-1)
	}
	if first.Kind == BoxWhisker && m > 0 {
		// A box and whisker chart draws a box a distinct category, the
		// values of one category gathered, as Render does.
		seen := map[string]bool{}
		for i := range m {
			if l := label(i); !seen[l] {
				seen[l] = true
				tail = l
			}
		}
		n, m = len(seen), len(seen)
	}
	if n == 0 {
		return ""
	}
	out := strconv.Itoa(n) + " categories"
	if n == 1 {
		out = "1 category"
	}
	if m == 0 {
		return out
	}
	switch {
	case head == "" || (m > 1 && tail == ""):
		return out
	case m == 1:
		return out + " (" + head + ")"
	case m == 2:
		return out + " (" + head + " and " + tail + ")"
	}
	return out + " (" + head + " to " + tail + ")"
}

// data returns d with its values filled by the chart's resolver when the
// file held no cache, as [Render] reads it.
func (c *Chart) data(d Data) Data {
	if d.Values == nil && !d.Cached && d.Ref != "" && c.Resolver != nil {
		vals := c.Resolver(d.Ref)
		if len(vals) > MaxPoints {
			vals = vals[:MaxPoints]
		}
		d.Values = vals
	}
	return d
}

// axisSide names the side of the plot area a is drawn on, as a reader
// sees it.
func axisSide(a *Axis) string {
	if a.Kind == SeriesAxis {
		return "depth"
	}
	switch a.Position {
	case AxisLeft:
		return "vertical"
	case AxisTop:
		return "top"
	case AxisRight:
		return "right"
	case AxisBottom:
	}
	return "horizontal"
}

// joinAnd joins parts as English lists them: "a", "a and b", "a, b and c".
func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// collapse returns s with its runs of white space, line breaks included,
// as single spaces, and none at either end.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// cut returns s cut to at most n characters, the last of them an
// ellipsis when it is cut.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i, count := 0, 0
	for j := range s {
		if count == n-1 {
			i = j
			break
		}
		count++
	}
	return strings.TrimRightFunc(s[:i], unicode.IsSpace) + "\u2026"
}

// cutWords returns s cut to at most n characters at the last space before
// the bound, an ellipsis ending it when it is cut.
func cutWords(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	short := cut(s, n)
	if i := strings.LastIndexByte(short, ' '); i > 0 {
		return strings.TrimRight(short[:i], ",;:") + "\u2026"
	}
	return short
}

// capitalized returns s with its first letter in upper case.
func capitalized(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// AltTextLang returns the language a structure element states for a
// description [AltText] writes in a document whose language is doc, a
// BCP 47 tag: "en" when doc is unknown or another language, "" when doc
// is English already and the element inherits it. ISO 14289-1, 7.2 asks
// that a change of natural language in an alternate description be
// stated; the Lang entry of the element that holds it states the
// language of its Alt (ISO 32000-1, 14.9.2).
func AltTextLang(doc string) string {
	primary, _, _ := strings.Cut(doc, "-")
	if strings.EqualFold(strings.TrimSpace(primary), "en") {
		return ""
	}
	return "en"
}
