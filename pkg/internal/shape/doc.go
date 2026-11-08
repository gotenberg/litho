// Package shape draws the geometry of a drawing shape: a preset of
// DrawingML (ECMA-376 Part 1, 20.1.9.18 prstGeom and 20.1.10.56
// ST_ShapeType, the preset shape definitions of Part 1, Annex D) or of
// OpenDocument's enhanced geometry (ODF 1.3 Part 3, 19.145 draw:type), with
// its adjustment values, or a custom geometry of guide formulas and path
// commands (20.1.9.8 custGeom; ODF draw:enhanced-geometry with its
// draw:equation and draw:enhanced-path), fitted to a shape's box. A
// [Geometry] is what a reader hands over; [Paths] returns the outlines to
// fill and stroke and [TextRect] the box the shape's text is laid out in.
//
// The 187 presets are generated from LibreOffice's copy of Annex D
// (cmd/gen-shape-presets) and evaluated as LibreOffice's custom shape
// engine evaluates them: guides in order, arcs by their visible angles,
// polygons filled even-odd. [Fill] and [Stroke] paint an outline with
// plain fill and line values: solid, gradient, picture and pattern fills,
// dashes in line widths ([PresetDash]) and arrowheads at a line's ends.
// The presentation, text and spreadsheet converters share it for every
// shape they draw.
//
// Everything in the package is a pure function of its arguments and safe
// for concurrent use.
package shape
