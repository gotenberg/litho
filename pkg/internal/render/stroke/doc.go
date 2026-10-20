// Package stroke turns a path and the stroke parameters of the graphics state
// into a device-space outline that the rasterizer fills with the nonzero
// winding rule, which is how ISO 32000-1, 8.5.3.2 defines the S operator.
//
// [Outline] strokes in user space and then maps the result through the CTM,
// so the pen is a circle of the line width in user space and an ellipse in
// device space under a non-uniform CTM (ISO 32000-1, 8.4.3.2). Curves are
// flattened first, in user space, to a tolerance derived from the device
// tolerance and the largest stretch of the CTM, so the device-space error
// stays within the tolerance. Joins between the flattened pieces of one curve
// are round: the offset of a smooth curve is the envelope of the pen, so the
// outline stays within the tolerance however wide the line.
//
// The outline of one polyline is one or two closed contours. On the outer
// side of a corner the contour follows the join style; on the inner side it
// pivots through the corner point. That pivot leaves small loops, but the
// winding number of the contour equals the sum of the windings of one
// rectangle per segment, one wedge per join and one cap per end, all with the
// same orientation, so the nonzero rule fills exactly their union. The
// output holds only MoveTo, LineTo and Close steps.
//
// Work is linear in the number of flattened points plus the points of the
// round joins and caps; every count sized by the input is capped by a named
// constant and reported as [ErrTooComplex] when exceeded.
//
// Functions in this package keep no state between calls and are safe for
// concurrent use.
package stroke
