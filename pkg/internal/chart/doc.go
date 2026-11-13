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
// labels fit, unless a manual layout places the plot area, the title or
// the legend. Value axes scale as LibreOffice's ScaleAutomatism does; bars
// take their width from the gap and overlap; pies start at the first slice
// angle; lines may be smoothed as natural cubic splines. Category labels
// break at spaces, or stay whole and turn by 45 degrees when they would
// overlap. A data table replaces the category labels under the plot area.
// A histogram series counts its values into bins first. A kind the
// renderer does not draw ([Unsupported]) keeps its axes and legend, and
// the reader's [Chart.Notice] says so over the chart's middle.
// Gradient and pattern fills paint through the shape package. Every text
// is shaped through the text package and emitted as [typeset.Text], so
// extraction reads it.
//
// A chart under [OfficeRules], as a DrawingML reader returns it, follows
// Office's rules where they are known. Its vertical primary value axis
// takes as many intervals as its labels fit end to end, each label the
// height of its line rounded to a whole pixel, and counts its ticks from
// its minimum. A two-dimensional cartesian chart puts its title, a legend
// above or below the plot area and the plot area at Office's distances,
// and its text lines are as high as their faces' ascent, descent and line
// gap. The rest keeps LibreOffice's rules.
//
// Trendlines fit their series by least squares, or average them, and
// enter the legend after their series. Error bars mark each point's error
// with a capped line.
//
// A three-dimensional bar, line, area or pie group with a [View3D] is
// drawn as a parallel projection, as Office draws a plot with right-angled
// axes: the front plane keeps its scale and the depth axis runs up and to
// the side by the sines of the view's angles. Bars are boxes, cylinders,
// cones or pyramids, lines ribbons and areas slabs, each in its row along
// the depth axis, painted back to front with their faces shaded by fixed
// factors; walls, floor and gridlines are projected, axis labels stay on
// the front plane. A pie is a tilted ellipse on a rim.
//
// Not drawn: perspective (a perspective view is drawn as a parallel
// projection), lighting, surface charts, a 3D chart's height percentage,
// trendlines and error bars of a 3D group, the equations of trendlines,
// high-low lines and up-down bars, leader lines, multi-level categories
// (their levels join on one label).
package chart
