package slides

// The codes of the warnings the builder records (compose.Warning.Code).
const (
	// CodeGeometry is a geometry drawn as its bounding rectangle.
	CodeGeometry = "slides-geometry"
	// CodePattern is a pattern fill drawn approximately.
	CodePattern = "slides-pattern"
	// CodePicture is a picture, a picture fill or a picture bullet that
	// has nothing to draw.
	CodePicture = "slides-picture"
	// CodeTooManyShapes is a slide or a group with more shapes than the
	// builder draws.
	CodeTooManyShapes = "slides-too-many-shapes"
	// CodeGroupDepth is a group nested deeper than the builder follows.
	CodeGroupDepth = "slides-group-depth"
	// CodeText is a text body that could not be laid out.
	CodeText = "slides-text"
	// CodeNumbering is a number format the builder does not know, drawn
	// as Arabic numbers with a full stop.
	CodeNumbering = "slides-numbering"
	// CodeVertical is a vertical text mode drawn approximately.
	CodeVertical = "slides-vertical"
	// CodeTable is a table cell or row the builder leaves out.
	CodeTable = "slides-table"
	// CodeEffect is a shadow, glow or soft edge drawn approximately or
	// not at all.
	CodeEffect = "slides-effect"
)
