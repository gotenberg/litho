package chart

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// altSeries returns a series named name, unnamed when name is "", over
// the categories cats with one value each.
func altSeries(name string, cats ...string) *Series {
	s := &Series{Name: name, Unnamed: name == ""}
	if name == "" {
		s.Name = "Series1"
	}
	for i, c := range cats {
		s.Categories.Values = append(s.Categories.Values, Str(c))
		s.Values.Values = append(s.Values.Values, Num(float64(i+1)))
	}
	return s
}

// TestAltText_StatesTheGrammar holds each case of the grammar AltText
// documents.
func TestAltText_StatesTheGrammar(t *testing.T) {
	quarters := []string{"Q1", "Q2", "Q3", "Q4"}
	cases := []struct {
		name string
		c    *Chart
		want string
	}{
		{"nil", nil, ""},
		{"no groups", &Chart{}, "Chart"},
		{"clustered column", &Chart{Groups: []*Group{{Kind: Bar, Series: []*Series{altSeries("North", quarters...), altSeries("South", quarters...)}}}},
			"Clustered column chart, 2 series (North and South), 4 categories (Q1 to Q4)"},
		{"horizontal stacked bar", &Chart{Groups: []*Group{{Kind: Bar, Horizontal: true, Grouping: Stacked, Series: []*Series{altSeries("A", "x", "y")}}}},
			"Stacked bar chart, 1 series (A), 2 categories (x and y)"},
		{"100% stacked area", &Chart{Groups: []*Group{{Kind: Area, Grouping: PercentStacked, Series: []*Series{altSeries("A", "x")}}}},
			"100% stacked area chart, 1 series (A), 1 category (x)"},
		{"line", &Chart{Groups: []*Group{{Kind: Line, Series: []*Series{altSeries("A", "x", "y", "z")}}}},
			"Line chart, 1 series (A), 3 categories (x to z)"},
		{"stock", &Chart{Groups: []*Group{{Kind: Line, Stock: true, Grouping: Stacked, Series: []*Series{altSeries("High", "Mon")}}}},
			"Stock chart, 1 series (High), 1 category (Mon)"},
		{"3-D rows", &Chart{Groups: []*Group{{Kind: Bar, ThreeD: true, Rows: true, Series: []*Series{altSeries("A", "x")}}}},
			"3-D column chart, 1 series (A), 1 category (x)"},
		{"3-D clustered", &Chart{Groups: []*Group{{Kind: Bar, ThreeD: true, Series: []*Series{altSeries("A", "x")}}}},
			"3-D clustered column chart, 1 series (A), 1 category (x)"},
		{"3-D pie", &Chart{Groups: []*Group{{Kind: Pie, ThreeD: true, Series: []*Series{altSeries("Share", "a", "b", "c")}}}},
			"3-D pie chart, 1 series (Share), 3 categories (a to c)"},
		{"doughnut", &Chart{Groups: []*Group{{Kind: Doughnut, Series: []*Series{altSeries("S", "a")}}}},
			"Doughnut chart, 1 series (S), 1 category (a)"},
		{"scatter has no categories", &Chart{Groups: []*Group{{Kind: Scatter, Series: []*Series{altSeries("Y", "1", "2")}}}},
			"Scatter chart, 1 series (Y)"},
		{"bubble", &Chart{Groups: []*Group{{Kind: Bubble, Series: []*Series{altSeries("Y", "1")}}}},
			"Bubble chart, 1 series (Y)"},
		{"filled radar", &Chart{Groups: []*Group{{Kind: Radar, Filled: true, Series: []*Series{altSeries("R", "a", "b")}}}},
			"Filled radar chart, 1 series (R), 2 categories (a and b)"},
		{"histogram counts no categories", &Chart{Groups: []*Group{{Kind: Bar, Series: []*Series{{Name: "Ages", Bins: &Binning{}, Values: Data{Values: []Value{Num(1), Num(2)}}}}}}},
			"Histogram chart, 1 series (Ages)"},
		{"office 2016 kinds", &Chart{Groups: []*Group{{Kind: Waterfall, Series: []*Series{altSeries("Cash", "a")}}}},
			"Waterfall chart, 1 series (Cash), 1 category (a)"},
		{"box and whisker", &Chart{Groups: []*Group{{Kind: BoxWhisker}}}, "Box and whisker chart"},
		{"box and whisker counts each category once", &Chart{Groups: []*Group{{Kind: BoxWhisker, Series: []*Series{altSeries("Scores", "A", "A", "B", "A", "C", "B")}}}},
			"Box and whisker chart, 1 series (Scores), 3 categories (A to C)"},
		{"funnel", &Chart{Groups: []*Group{{Kind: Funnel}}}, "Funnel chart"},
		{"treemap", &Chart{Groups: []*Group{{Kind: Treemap}}}, "Treemap chart"},
		{"sunburst", &Chart{Groups: []*Group{{Kind: Sunburst}}}, "Sunburst chart"},
		{"surface", &Chart{Surface: true}, "Surface chart"},
		{"unsupported kind", &Chart{Groups: []*Group{{Kind: Unsupported, Series: []*Series{altSeries("A", "x")}}}},
			"Chart, 1 series (A), 1 category (x)"},
		{"combination", &Chart{Groups: []*Group{
			{Kind: Bar, Series: []*Series{altSeries("Sales", "Jan", "Feb")}},
			{Kind: Line, Series: []*Series{altSeries("Margin", "Jan", "Feb")}},
			{Kind: Bar, Series: []*Series{altSeries("Costs", "Jan", "Feb")}},
		}}, "Combination chart of clustered column and line, 3 series (Sales, Margin and Costs), 2 categories (Jan and Feb)"},
		{"title first", &Chart{Title: &Title{Text: "Sales\nby  region"}, Groups: []*Group{{Kind: Pie, Series: []*Series{altSeries("2024", "N", "S")}}}},
			"Sales by region. Pie chart, 1 series (2024), 2 categories (N and S)"},
		{"title with its own mark", &Chart{Title: &Title{Text: "Why?"}, Groups: []*Group{{Kind: Line}}}, "Why? Line chart"},
		{"placeholder title left out", &Chart{Title: &Title{Text: "Chart Title", Auto: true}, Groups: []*Group{{Kind: Line}}}, "Line chart"},
		{"unnamed series counted", &Chart{Groups: []*Group{{Kind: Bar, Series: []*Series{altSeries("", "a"), altSeries("", "a")}}}},
			"Clustered column chart, 2 series, 1 category (a)"},
		{"named and unnamed", &Chart{Groups: []*Group{{Kind: Bar, Series: []*Series{altSeries("North", "a"), altSeries("", "a")}}}},
			"Clustered column chart, 2 series (North and 1 unnamed), 1 category (a)"},
		{"unlabelled categories counted", &Chart{Groups: []*Group{{Kind: Line, Series: []*Series{{Name: "A", Values: Data{Values: []Value{Num(1), Num(2), Num(3)}}}}}}},
			"Line chart, 1 series (A), 3 categories"},
		{"axis titles", &Chart{
			Groups: []*Group{{Kind: Bar, Series: []*Series{altSeries("A", "x")}}},
			Axes: []*Axis{
				{Kind: CategoryAxis, Position: AxisBottom, Title: &Title{Text: "Quarter"}},
				{Kind: ValueAxis, Position: AxisLeft, Title: &Title{Text: "Revenue (USD)"}},
				{Kind: ValueAxis, Position: AxisRight, Title: &Title{Text: "Margin"}},
				{Kind: ValueAxis, Position: AxisTop, Title: &Title{Text: "Axis Title", Auto: true}},
				{Kind: SeriesAxis, Title: &Title{Text: "Year"}},
				{Kind: ValueAxis, Position: AxisLeft, Deleted: true, Title: &Title{Text: "Hidden"}},
			},
		}, "Clustered column chart, 1 series (A), 1 category (x), horizontal axis Quarter, vertical axis Revenue (USD), right axis Margin, depth axis Year"},
	}
	for _, tc := range cases {
		if got := AltText(tc.c); got != tc.want {
			t.Errorf("%s: AltText = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestAltText_ResolvesDataWithoutCache reads the categories of a series
// whose file holds no cache through the chart's resolver, as Render does.
func TestAltText_ResolvesDataWithoutCache(t *testing.T) {
	c := &Chart{
		Groups: []*Group{{Kind: Bar, Series: []*Series{{Name: "A", Categories: Data{Ref: "S!A1:A3"}, Values: Data{Ref: "S!B1:B3"}}}}},
		Resolver: func(ref string) []Value {
			if ref == "S!A1:A3" {
				return []Value{Str("Mon"), Str("Tue"), Str("Wed")}
			}
			return []Value{Num(1), Num(2), Num(3)}
		},
	}
	if got, want := AltText(c), "Clustered column chart, 1 series (A), 3 categories (Mon to Wed)"; got != want {
		t.Errorf("AltText = %q, want %q", got, want)
	}
}

// TestAltText_CategoriesInTheAxisFormat names numeric categories as the
// category axis shows them: in its number format, else in the format of
// the source cells of an axis linked to them.
func TestAltText_CategoriesInTheAxisFormat(t *testing.T) {
	dates := Data{Ref: "S!A1:A2", Values: []Value{Num(34912), Num(34914)}}
	c := &Chart{
		Groups: []*Group{{Kind: Area, XAxis: 0, Series: []*Series{{Name: "A", Categories: dates, Values: Data{Values: []Value{Num(1), Num(2)}}}}}},
		Axes:   []*Axis{{Kind: DateAxis, Format: "yyyy-mm-dd"}},
	}
	if got, want := AltText(c), "Area chart, 1 series (A), 2 categories (1995-08-01 and 1995-08-03)"; got != want {
		t.Errorf("AltText = %q, want %q", got, want)
	}
	c.Axes[0].Format, c.Axes[0].SourceLinked = "", true
	c.FormatResolver = func(string) (string, bool) { return "d/m/yyyy", true }
	if got, want := AltText(c), "Area chart, 1 series (A), 2 categories (1/8/1995 and 3/8/1995)"; got != want {
		t.Errorf("source-linked: AltText = %q, want %q", got, want)
	}
}

// TestAltText_Bounded holds the bounds: names listed, the length of each,
// and the length of the whole.
func TestAltText_Bounded(t *testing.T) {
	long := strings.Repeat("word ", 40)
	g := &Group{Kind: Line}
	for i := range 9 {
		g.Series = append(g.Series, altSeries("S"+strconv.Itoa(i+1)+" "+long, long, "b", "c"+long))
	}
	c := &Chart{Title: &Title{Text: strings.Repeat("Title ", 100)}, Groups: []*Group{g}}
	for range 10 {
		c.Axes = append(c.Axes, &Axis{Kind: ValueAxis, Position: AxisLeft, Title: &Title{Text: long}})
	}
	got := AltText(c)
	if n := utf8.RuneCountInString(got); n > maxAltRunes {
		t.Errorf("AltText has %d characters, want at most %d", n, maxAltRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("AltText = %q, want it cut with an ellipsis", got)
	}
	g.Series = g.Series[:7]
	c.Title, c.Axes = nil, nil
	got = AltText(c)
	if !strings.Contains(got, "7 series (") || !strings.Contains(got, " and 2 more)") {
		t.Errorf("AltText = %q, want five names and two more", got)
	}
	_, list, _ := strings.Cut(got, "7 series (")
	list, _, _ = strings.Cut(list, " and 2 more)")
	for _, name := range strings.Split(list, ", ") {
		if n := utf8.RuneCountInString(name); n > maxAltNameRunes || !strings.HasSuffix(name, "\u2026") {
			t.Errorf("a name of %d characters, %q, in %q", n, name, got)
		}
	}
}

// TestAltTextLang states English on a description in a document of
// another language or of none, and inherits it in an English one.
func TestAltTextLang(t *testing.T) {
	for doc, want := range map[string]string{"": "en", "fr-FR": "en", "ja": "en", "en": "", "en-GB": "", "EN-us": ""} {
		if got := AltTextLang(doc); got != want {
			t.Errorf("AltTextLang(%q) = %q, want %q", doc, got, want)
		}
	}
}
