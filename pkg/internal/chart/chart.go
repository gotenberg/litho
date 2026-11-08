package chart

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// Chart is one chart as a reader found it: its title, its legend, its plot
// area with the chart groups, their series and the axes, and the formats of
// every part. A reader resolves what the file leaves to the application, the
// automatic colours, line widths and text properties included, so the
// renderer draws the model without knowing the file format.
//
// A Chart is read-only once a reader returns it and safe for concurrent
// [Render] calls.
type Chart struct {
	// Title is the main title, nil for none.
	Title *Title
	// Subtitle is the OpenDocument subtitle drawn under the title, nil for
	// none (ODF 1.3 Part 3, 11.2.3 chart:subtitle).
	Subtitle *Title
	// Legend is the legend, nil for none.
	Legend *Legend
	// Groups are the chart groups of the plot area in drawing order: a bar
	// group and a line group share the plot area of a combined chart. The
	// first group decides the plot area's layout.
	Groups []*Group
	// Axes are the axes of the plot area. A group names its axes by index.
	Axes []*Axis
	// PlotLayout, when set, places the plot area where the file says
	// instead of in the space the title and legend leave.
	PlotLayout *ManualLayout
	// DataTable, when set, draws the series' values in a table under the
	// plot area, in place of the category axis's labels.
	DataTable *DataTable
	// Area is the chart area's fill and border, PlotArea the plot area's
	// (the wall of a two-dimensional chart).
	Area, PlotArea Frame
	// Palette lists the automatic colours of series and, in a group with
	// VaryColors, of points, used in order for an item with no fill of its
	// own.
	Palette []compose.Color
	// PaletteCycleTint darkens the automatic colours of the first cycles
	// through Palette and lightens those of the last, as Office does when
	// series outnumber its six accent colours (ECMA-376 Part 1, 21.2.2.202
	// style).
	PaletteCycleTint bool
	// Resolver, when set, supplies the values of a series whose file holds
	// no cache, from its range reference.
	Resolver Resolver
	// Locale is the separators the chart's numbers are written with, those
	// of the document's language, unless a number format names its own
	// locale; the zero Locale writes en-US's.
	Locale numfmt.Locale
	// Notice, when set, is a text drawn over the middle of the chart in
	// its frame, at most two thirds of the chart's width, over the plot
	// area and its axes: the reader's note that it does not draw the
	// chart's kind.
	Notice *Title
	// Warnings lists what the reader did not carry into the model.
	Warnings []compose.Warning
}

// Kind is the chart type of a [Group].
type Kind uint8

// The chart kinds.
const (
	// Bar draws a bar per point: vertical columns, or horizontal bars with
	// [Group.Horizontal] (ECMA-376 Part 1, 21.2.2.16 barChart).
	Bar Kind = iota
	// Line joins the points of a series with a line (21.2.2.97 lineChart).
	Line
	// Area fills under the line of a series (21.2.2.5 areaChart).
	Area
	// Pie draws the points of the first series as sectors (21.2.2.141
	// pieChart).
	Pie
	// Doughnut is a pie with a hole, each series a ring (21.2.2.50
	// doughnutChart).
	Doughnut
	// Scatter places points by their x and y values (21.2.2.161
	// scatterChart).
	Scatter
	// Bubble is a scatter chart whose points are circles sized by a third
	// value (21.2.2.20 bubbleChart).
	Bubble
	// Radar draws each series as a polygon around the centre (21.2.2.153
	// radarChart).
	Radar
	// Unsupported is a chart kind the renderer does not draw, as a
	// funnel or a box and whisker chart of Office 2016 (MS-ODRAWXML,
	// 2.24.3.53 ST_SeriesLayout): its axes are drawn, scaled to its last
	// series alone, its categories on the ticks, and no series; its
	// legend keys are circles.
	Unsupported
)

// String returns the kind's name, as warnings spell it.
func (k Kind) String() string {
	switch k {
	case Bar:
		return "bar"
	case Line:
		return "line"
	case Area:
		return "area"
	case Pie:
		return "pie"
	case Doughnut:
		return "doughnut"
	case Scatter:
		return "scatter"
	case Bubble:
		return "bubble"
	case Radar:
		return "radar"
	case Unsupported:
		return "unsupported"
	}
	return "unknown"
}

// Grouping says how the series of a bar, line or area group combine
// (ECMA-376 Part 1, 21.2.3.17 ST_Grouping; ODF 1.3 Part 3, 20.15
// chart:stacked and 20.14 chart:percentage).
type Grouping uint8

// The groupings.
const (
	// Clustered draws the series side by side, or over each other for
	// lines and areas: Office's "clustered" and "standard".
	Clustered Grouping = iota
	// Stacked adds each series to the ones before it.
	Stacked
	// PercentStacked stacks the series and scales each category to 100%.
	PercentStacked
)

// Group is one chart group of the plot area: series of one kind drawn
// against the same axes.
type Group struct {
	Kind Kind
	// Horizontal draws a bar group's bars along the x direction, the
	// category axis vertical (ECMA-376 Part 1, 21.2.2.17 barDir bar).
	Horizontal bool
	Grouping   Grouping
	// VaryColors gives every point of a single-series group its own
	// automatic colour, as pies do (21.2.2.227 varyColors).
	VaryColors bool
	// GapWidth is the space between two categories' bars as a percentage
	// of a bar's width, 150 by default (21.2.2.75 gapWidth). Overlap is how
	// much the bars of one category overlap, from -100 (a bar's width
	// apart) to 100 (on top of each other) (21.2.2.131 overlap).
	GapWidth, Overlap float64
	// HoleSize is a doughnut's hole diameter as a percentage of the outer
	// diameter (21.2.2.82 holeSize).
	HoleSize float64
	// FirstSliceAngle is the angle in degrees, clockwise from twelve
	// o'clock, at which a pie's first sector starts (21.2.2.68
	// firstSliceAng).
	FirstSliceAngle float64
	// CounterClockwise draws a pie's sectors counter-clockwise, as an
	// OpenDocument pie with a reversed angle axis does.
	CounterClockwise bool
	// BubbleScale is the size of the largest bubble as a percentage of the
	// default, 100 by default (21.2.2.21 bubbleScale).
	BubbleScale float64
	// Filled draws a radar group's polygons filled (21.2.3.37
	// ST_RadarStyle filled).
	Filled bool
	// ThreeD marks a group the file draws in three dimensions; it is
	// drawn as its two-dimensional kind.
	ThreeD bool
	// XAxis and YAxis index [Chart.Axes]: the category (or x value) axis
	// and the value axis. -1 for none, as a pie has.
	XAxis, YAxis int
	Series       []*Series
	// Labels are the data labels the group's series inherit.
	Labels Labels
}

// Series is one data series.
type Series struct {
	// Index orders the series for the automatic colours and markers
	// (ECMA-376 Part 1, 21.2.2.84 idx).
	Index int
	// Name is the series' name, as the legend shows it.
	Name string
	// Categories are the category labels, or the x values of a scatter or
	// bubble series; Values the values, or the y values; Sizes the bubble
	// sizes.
	Categories, Values, Sizes Data
	// Fill is the area fill of bars, areas, sectors and bubbles, nil for
	// the automatic colour.
	Fill *Paint
	// Border is the outline of bars, areas, sectors and bubbles, and the
	// line of a line, scatter or radar series. nil takes the automatic
	// line: none for filled series, a line in the automatic colour for
	// lines.
	Border *Stroke
	// Marker is the symbol drawn at each point of a line, scatter or radar
	// series.
	Marker Marker
	// Smooth draws a line series as a curve through its points.
	Smooth bool
	// Explosion moves a pie's sectors out from the centre by a percentage
	// of the radius (21.2.2.61 explosion).
	Explosion float64
	// Labels are the series' data labels.
	Labels Labels
	// Points override the format of single points.
	Points []Point
	// NoLegend leaves the series out of the legend.
	NoLegend bool
	// Trendlines are the regression curves drawn over the series.
	Trendlines []Trendline
	// Bins, when set, draws the series as a histogram: its values are
	// counted into bins, which become the series' categories and values
	// (MS-ODRAWXML, 2.24.3.5 CT_Binning).
	Bins *Binning
}

// TrendKind is the regression a [Trendline] fits (ECMA-376 Part 1,
// 21.2.3.51 ST_TrendlineType; ODF 1.3 Part 3, 20.86 chart:regression-type).
type TrendKind uint8

// The trendline kinds.
const (
	TrendLinear TrendKind = iota
	TrendExponential
	TrendLogarithmic
	TrendPolynomial
	TrendPower
	TrendMovingAverage
)

// MovingType is how a moving average places its points (ODF 1.3 Part 3,
// 20.86.1 chart:regression-moving-type).
type MovingType uint8

// The moving average types.
const (
	// MovingPrior averages each point with the points before it, at the
	// point's x.
	MovingPrior MovingType = iota
	// MovingCentral averages the same windows, placed at the middle
	// point's x.
	MovingCentral
	// MovingAveragedAbscissa places each average at the average x of its
	// window.
	MovingAveragedAbscissa
)

// Trendline is a regression curve fitted to a series' points: x is a
// scatter series' x value, or a category's 1-based index (ECMA-376 Part 1,
// 21.2.2.211 trendline; ODF 1.3 Part 3, 11.15 chart:regression-curve).
type Trendline struct {
	Kind TrendKind
	// Order is a polynomial's degree, 2 to 6.
	Order int
	// Period is a moving average's window, at least 2.
	Period int
	Moving MovingType
	// Forward and Backward extend the curve past the greatest and the
	// least x, in x units.
	Forward, Backward float64
	// Intercept, when HasIntercept, forces the curve through (0,
	// Intercept): a linear, polynomial or exponential fit.
	Intercept    float64
	HasIntercept bool
	// Name is the legend entry's text; "" takes the kind's name and the
	// series' ("Linear (Sales)").
	Name string
	// Line is the curve's line, nil for the series' colour.
	Line *Stroke
}

// Binning is how a histogram series counts its values into bins.
type Binning struct {
	// Width is the width of a bin, Count the number of bins; when both
	// are 0, the width follows Scott's rule: 3.5 standard deviations
	// over the cube root of the count of values.
	Width float64
	Count int
}

// Point is the format of one data point that differs from its series
// (ECMA-376 Part 1, 21.2.2.52 dPt; ODF 1.3 Part 3, 11.12 chart:data-point).
type Point struct {
	// Index is the point's 0-based index in the series.
	Index int
	// Fill and Border, when not nil, replace the series' or the automatic
	// ones.
	Fill   *Paint
	Border *Stroke
	// Marker, when HasMarker is set, replaces the series' marker.
	Marker    Marker
	HasMarker bool
	// Explosion, when HasExplosion is set, replaces the series' explosion.
	Explosion    float64
	HasExplosion bool
	// Labels, when HasLabels is set, replaces the series' data labels.
	Labels    Labels
	HasLabels bool
}

// Paint is a fill: a flat colour, a gradient or a pattern, or none.
type Paint struct {
	// None paints nothing.
	None bool
	// Color is the fill colour, and a gradient's or a pattern's
	// approximation where one colour is drawn (a line, a legend key's
	// border).
	Color compose.Color
	// Shape, when set, paints a gradient or a pattern instead of Color,
	// fitted to the bounding box of each area it fills (a bar, a sector,
	// the chart area), through [shape.Fill].
	Shape *shape.Paint
}

// Stroke is a line: its colour, width and dashes, or none.
type Stroke struct {
	// None draws no line.
	None bool
	// Color is the line colour.
	Color compose.Color
	// Width is the line width in points; zero draws the thinnest line.
	Width float64
	// Dash is the dash pattern in points, empty for solid.
	Dash []float64
}

// Frame is the fill and border of a rectangle of the chart: the chart
// area, the plot area, the title, the legend, a data label.
type Frame struct {
	// Fill is the background, nil for none.
	Fill *Paint
	// Border is the outline, nil for none.
	Border *Stroke
}

// TextStyle is the character format of a text of the chart.
type TextStyle struct {
	// Families lists the font families in preference order.
	Families []string
	// Size is the font size in points.
	Size float64
	Bold bool
	// Italic asks for an italic face.
	Italic bool
	// Color fills the glyphs.
	Color compose.Color
	// Rotation turns the text counter-clockwise by this many degrees.
	Rotation float64
}

// Title is a chart or axis title.
type Title struct {
	// Text is the title's text; a line feed breaks a line.
	Text  string
	Style TextStyle
	Frame Frame
	// Overlay lets the title sit over the plot area instead of taking
	// space from it (ECMA-376 Part 1, 21.2.2.132 overlay).
	Overlay bool
	// Layout, when set, puts the title's top-left corner where the file
	// says; its size stays automatic, and it still takes its space from
	// the plot area.
	Layout *ManualLayout
}

// DataTable is the table of the series' values a chart draws under its
// plot area: a header row of the categories, aligned with them, then a row
// a series, its name in a first column left of the plot area (ECMA-376
// Part 1, 21.2.2.54 dTable; ODF 1.3 Part 3, loext:data-table).
type DataTable struct {
	// HBorder, VBorder and Outline draw the lines between the rows, the
	// lines between the columns and the table's outline; Keys draws each
	// series' legend key before its name.
	HBorder, VBorder, Outline, Keys bool
	Style                           TextStyle
	// Fill is the background of each cell's text, nil for none.
	Fill *Paint
	// Line is the border lines' stroke, nil for none.
	Line *Stroke
}

// ManualLayout is where a file places a part of the chart, as fractions of
// the chart's rectangle from its top-left corner (ECMA-376 Part 1,
// 21.2.2.95 manualLayout in its edge mode; ODF 1.3 Part 3, 19.237 svg:x
// and 19.539 svg:width on a chart element).
type ManualLayout struct {
	// X and Y place the part's top-left corner.
	X, Y float64
	// Width and Height size it, 0 for its automatic size.
	Width, Height float64
	// Inner places a plot area's rectangle without its axis labels and
	// titles, which lie outside it (21.2.2.96 layoutTarget inner); else
	// the rectangle holds them.
	Inner bool
}

// rect returns the layout's rectangle in r, its size 0 when automatic.
func (l *ManualLayout) rect(r compose.Rect) compose.Rect {
	return compose.Rect{X: r.X + l.X*r.Width, Y: r.Y + l.Y*r.Height, Width: l.Width * r.Width, Height: l.Height * r.Height}
}

// LegendPosition is where the legend sits.
type LegendPosition uint8

// The legend positions (ECMA-376 Part 1, 21.2.3.24 ST_LegendPos; ODF 1.3
// Part 3, 19.15 chart:legend-position).
const (
	LegendRight LegendPosition = iota
	LegendLeft
	LegendTop
	LegendBottom
	// LegendTopRight sits in the top-right corner, as Office's tr.
	LegendTopRight
)

// Legend is the chart's legend.
type Legend struct {
	Position LegendPosition
	// Overlay lets the legend sit over the plot area.
	Overlay bool
	Style   TextStyle
	Frame   Frame
	// Layout, when set, puts the legend's top-left corner where the file
	// says, and its size when not 0; the plot area still leaves the room
	// the legend takes at its position.
	Layout *ManualLayout
	// Hidden lists legend entries, by index in the legend's order, that the
	// file deletes (ECMA-376 Part 1, 21.2.2.94 legendEntry delete).
	Hidden []int
}

// AxisKind is what an axis measures.
type AxisKind uint8

// The axis kinds (ECMA-376 Part 1, 21.2.2.25 catAx, 21.2.2.226 valAx,
// 21.2.2.39 dateAx, 21.2.2.175 serAx).
const (
	// CategoryAxis spaces the categories evenly.
	CategoryAxis AxisKind = iota
	// ValueAxis scales values linearly or logarithmically.
	ValueAxis
	// DateAxis spaces categories by their dates; it is drawn as a category
	// axis.
	DateAxis
	// SeriesAxis is the depth axis of a 3D chart; it is not drawn.
	SeriesAxis
)

// AxisPosition is the side of the plot area an axis is drawn on.
type AxisPosition uint8

// The axis positions (ECMA-376 Part 1, 21.2.3.3 ST_AxPos).
const (
	AxisBottom AxisPosition = iota
	AxisLeft
	AxisTop
	AxisRight
)

// Crossing is where an axis crosses the axis it is drawn against.
type Crossing uint8

// The crossings (ECMA-376 Part 1, 21.2.3.10 ST_Crosses).
const (
	// CrossAuto crosses at zero, or at the minimum when zero is out of
	// range.
	CrossAuto Crossing = iota
	// CrossMin crosses at the other axis' minimum.
	CrossMin
	// CrossMax crosses at the other axis' maximum.
	CrossMax
	// CrossValue crosses at Axis.CrossAt.
	CrossValue
)

// TickMark is how the ticks of an axis are drawn (ECMA-376 Part 1,
// 21.2.3.48 ST_TickMark).
type TickMark uint8

// The tick marks.
const (
	TickNone TickMark = iota
	TickOut
	TickIn
	TickCross
)

// LabelPosition is where an axis's tick labels sit (ECMA-376 Part 1,
// 21.2.3.47 ST_TickLblPos).
type LabelPosition uint8

// The tick label positions.
const (
	// LabelNextTo draws the labels beside the axis line.
	LabelNextTo LabelPosition = iota
	// LabelLow draws them at the low end of the crossing axis.
	LabelLow
	// LabelHigh draws them at the high end of the crossing axis.
	LabelHigh
	// LabelNone draws no labels.
	LabelNone
)

// Axis is one axis of the plot area.
type Axis struct {
	Kind AxisKind
	// Deleted hides the axis, its line, labels and title; its gridlines
	// still draw.
	Deleted  bool
	Position AxisPosition
	// Crosses is where this axis crosses its crossing axis, CrossAt the
	// value when Crosses is CrossValue.
	Crosses Crossing
	CrossAt float64
	// Min, Max, MajorUnit and MinorUnit fix the scale; NaN leaves a bound
	// automatic.
	Min, Max, MajorUnit, MinorUnit float64
	// Log is the logarithm base of a logarithmic scale, 0 for linear.
	Log float64
	// Reverse runs the axis from its maximum to its minimum.
	Reverse bool
	// Format is the number format code of the labels, "" or "General" for
	// the source's.
	Format string
	// Labels says where the tick labels go.
	Labels LabelPosition
	// MajorTicks and MinorTicks are the tick marks.
	MajorTicks, MinorTicks TickMark
	// MajorGrid and MinorGrid are the gridlines, nil for none.
	MajorGrid, MinorGrid *Stroke
	// Line is the axis line, nil for none.
	Line *Stroke
	// Style is the tick labels' character format.
	Style TextStyle
	// Title is the axis title, nil for none.
	Title *Title
	// NoBreak keeps each category label on one line: a label wider than
	// its category neither wraps nor breaks (LibreOffice's TextBreak off;
	// ODF 1.3 Part 3, 20.25 text:line-break false).
	NoBreak bool
	// Overlap lets the labels overlap rather than be rotated or thinned
	// out (ODF 1.3 Part 3, 20.21 chart:text-overlap).
	Overlap bool
	// BetweenCategories puts a category axis's categories between its tick
	// marks rather than on them (ECMA-376 Part 1, 21.2.2.32 crossBetween).
	BetweenCategories bool
}

// LabelPlacement is where a data label sits relative to its point
// (ECMA-376 Part 1, 21.2.3.13 ST_DLblPos).
type LabelPlacement uint8

// The data label placements.
const (
	// LabelAuto takes the kind's default: outside the end of a bar, right
	// of a line point, inside a pie's sector near its edge.
	LabelAuto LabelPlacement = iota
	LabelCenter
	LabelInsideEnd
	LabelInsideBase
	LabelOutsideEnd
	LabelAbove
	LabelBelow
	LabelLeft
	LabelRight
	// LabelBestFit places a pie's label inside its sector when it fits,
	// else outside with a leader line.
	LabelBestFit
)

// Labels are the data labels of a series or a point.
type Labels struct {
	// ShowValue, ShowCategory, ShowSeries and ShowPercent choose the parts
	// of a label; ShowKey draws the legend symbol before it.
	ShowValue, ShowCategory, ShowSeries, ShowPercent, ShowKey bool
	// Separator joins the parts; empty takes the kind's default, a line
	// break for pies and "; " otherwise.
	Separator string
	Placement LabelPlacement
	// Format is the number format code of the values, "" for the source's.
	Format string
	// PercentFormat is the number format code of the percentages, ""
	// for Format when the label shows no value, else "0%".
	PercentFormat string
	Style         TextStyle
	Frame         Frame
	// Custom, when not empty, is the label's own text, which replaces the
	// parts the flags choose (ECMA-376 Part 1, 21.2.2.47 dLbl's tx; ODF
	// 1.3 Part 3, loext:data-label's text:p).
	Custom []LabelField
}

// LabelField is one piece of a data label's own text: text as written,
// or a field the renderer fills.
type LabelField struct {
	Kind LabelFieldKind
	// Text is a FieldText's text, and what a field the renderer cannot
	// fill shows.
	Text string
}

// LabelFieldKind is what a [LabelField] holds (the field types of
// ECMA-376 Part 1, 21.1.2.2.4 fld as Office writes them in charts).
type LabelFieldKind uint8

// The label field kinds.
const (
	FieldText LabelFieldKind = iota
	FieldValue
	FieldCategory
	FieldSeries
	FieldPercent
	// FieldBreak breaks the line.
	FieldBreak
)

// Any reports labels that show something.
func (l Labels) Any() bool {
	return l.ShowValue || l.ShowCategory || l.ShowSeries || l.ShowPercent || len(l.Custom) > 0
}

// MarkerSymbol is the shape of a data point's marker. The standard shapes
// are LibreOffice's, in its order, which the automatic symbol cycles
// through by series.
type MarkerSymbol uint8

// The marker symbols.
const (
	// MarkerAuto takes the standard symbol of the series' index.
	MarkerAuto MarkerSymbol = iota
	MarkerNone
	MarkerSquare
	MarkerDiamond
	MarkerArrowDown
	MarkerArrowUp
	MarkerArrowRight
	MarkerArrowLeft
	MarkerBowTie
	MarkerSandglass
	MarkerCircle
	MarkerStar
	MarkerX
	MarkerPlus
	MarkerAsterisk
	MarkerHorizontalBar
	MarkerVerticalBar
)

// Marker is a data point's symbol.
type Marker struct {
	Symbol MarkerSymbol
	// Size is the symbol's width and height in points.
	Size float64
	// Fill and Border, nil for the series' automatic colour.
	Fill   *Paint
	Border *Stroke
}
