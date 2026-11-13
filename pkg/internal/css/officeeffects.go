package css

// OfficeEffects are the effects an Office document draws around a shape,
// a picture or a run of text as bitmaps: the members of a DrawingML
// effect list (ECMA-376 Part 1, 20.1.8.26 effectLst) and their
// WordprocessingML text counterparts (MS-DOCX, 2.6.1 w14:glow,
// w14:shadow, w14:reflection). Lengths are in points, angles in degrees
// clockwise from the x axis with y downward, opacities and positions
// fractions from 0 to 1. A nil member draws nothing.
type OfficeEffects struct {
	// Shadow is the outer shadow (20.1.8.45 outerShdw).
	Shadow *OfficeShadow
	// Glow is the glow (20.1.8.32 glow).
	Glow *OfficeGlow
	// Reflection is the reflection (20.1.8.50 reflection).
	Reflection *OfficeReflection
	// SoftEdge is the radius of the soft edge (20.1.8.53 softEdge), 0
	// for none.
	SoftEdge float64
	// InnerShadow is the inner shadow (20.1.8.40 innerShdw): its Color,
	// Blur, Dist and Dir; its other fields are unused.
	InnerShadow *OfficeShadow
	// Blur is the blur of what the source paints (20.1.8.15 blur).
	Blur *OfficeBlur
	// FillOverlay is a colour blended over what the source paints
	// (20.1.8.29 fillOverlay).
	FillOverlay *OfficeOverlay
	// Rules names the program whose rules draw the effects: the zero
	// value, [OfficeWord], for a WordprocessingML or SpreadsheetML
	// document, [OfficePowerPoint] for a PresentationML one.
	Rules OfficeRules
}

// OfficeBlur blurs what a source paints over Radius points; Grow lets the
// blur reach past the source's bounds, which otherwise cut it.
type OfficeBlur struct {
	Radius float64
	Grow   bool
}

// OfficeOverlay blends Color over what a source paints, where it paints,
// by Blend.
type OfficeOverlay struct {
	Color Color
	Blend OfficeBlend
}

// OfficeBlend is how an [OfficeOverlay] blends (ECMA-376 Part 1,
// 20.1.10.11 ST_BlendMode).
type OfficeBlend uint8

// The overlay blends.
const (
	OfficeOver OfficeBlend = iota
	OfficeMultiply
	OfficeScreen
	OfficeDarken
	OfficeLighten
)

// OfficeRules names the Microsoft Office program whose rules draw a
// document's effects: each program prints its own documents, and Word
// and PowerPoint draw some effects apart.
type OfficeRules uint8

// The programs whose rules draw effects. Excel's drawings take Word's.
const (
	OfficeWord OfficeRules = iota
	OfficePowerPoint
)

// OfficeShadow is an outer shadow: a copy of the silhouette scaled by SX
// and SY and skewed by KX and KY about the point of the bounds Align
// names, blurred by Blur, moved Dist along Dir, in Color.
type OfficeShadow struct {
	Color      Color
	Blur, Dist float64
	Dir        float64
	SX, SY     float64
	KX, KY     float64
	Align      RectAlign
}

// OfficeGlow is a glow: a halo of Radius around the silhouette in Color.
type OfficeGlow struct {
	Color  Color
	Radius float64
}

// OfficeReflection is a reflection: the content scaled by SX and SY
// (SY negative mirrors it) and skewed about the point of the bounds Align
// names, moved Dist along Dir, blurred by Blur, its opacity fading from
// StartAlpha at StartPos to EndAlpha at EndPos of its extent along
// FadeDir.
type OfficeReflection struct {
	Blur                 float64
	StartAlpha, EndAlpha float64
	StartPos, EndPos     float64
	Dist, Dir, FadeDir   float64
	SX, SY               float64
	KX, KY               float64
	Align                RectAlign
}

// RectAlign is a point of a rectangle, its corners, edge midpoints or
// centre (ECMA-376 Part 1, 20.1.10.53 ST_RectAlignment), as fractions of
// its width and height from its top-left corner.
type RectAlign struct {
	X, Y float64
}

// OfficeTextPaint paints the glyphs of a run otherwise than filled in
// the run's colour: unfilled, filled with a linear gradient, outlined
// (MS-DOCX, 2.6.1 w14:textFill and w14:textOutline; ECMA-376 Part 1,
// 17.3.2.23 outline). The glyphs stay text, drawn invisible under the
// paint.
type OfficeTextPaint struct {
	// NoFill leaves the glyphs unfilled.
	NoFill bool
	// Gradient, two stops or more in increasing Pos, fills the glyphs
	// along Angle degrees, clockwise from the x axis, over the run's box.
	Gradient []OfficeStop
	Angle    float64
	// Outline strokes the glyphs' outlines, nil for none.
	Outline *OfficeOutline
}

// OfficeStop is a gradient stop: a colour at a fraction of the way.
type OfficeStop struct {
	Pos   float64
	Color Color
}

// OfficeOutline is a stroke of Width points in Color.
type OfficeOutline struct {
	Color Color
	Width float64
}
