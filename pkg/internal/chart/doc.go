// Package chart holds the model of a chart embedded in an Office document
// and draws it as LibreOffice draws one. A reader fills a [Chart] from a
// DrawingML chart part (ECMA-376 Part 1, 21.2) or an OpenDocument chart
// document (ODF 1.3 Part 3, 11); the spreadsheet and presentation
// converters place it as a picture whose fragments [Render] returns.
//
// # The model
//
// A [Chart] is format-neutral: the readers resolve what a file leaves to
// its application, the automatic colours ([Chart.Palette]), line widths and
// text properties, so the renderer needs no knowledge of either format.
// The data are the caches the files carry; a series without one keeps its
// range reference, which a [Resolver] can fill.
//
// # The drawing
//
// [Render] follows LibreOffice's automatic chart layout (chart2's
// ChartView): the title at the top, the legend at its side sized from its
// entries, and the plot area in the space left, shrunk until its axis
// labels fit. Value axes scale as LibreOffice's ScaleAutomatism does; bars
// take their width from the gap and overlap; pies start at the first slice
// angle; lines may be smoothed as natural cubic splines. Every text is
// shaped through the text package and emitted as [compose.Text], so
// extraction reads it.
//
// Not drawn: 3D perspective (a 3D chart draws flat), trendlines, error
// bars, data tables, gradients and patterns (a fill takes one colour),
// manual layouts, leader lines.
package chart
