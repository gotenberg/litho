package models

// TextElement represents a spatially positioned piece of text on a page.
// Concurrent reads of a value are safe; a write needs synchronization.
type TextElement struct {
	// Text is the characters the glyph maps to.
	Text string `json:"text"`
	// X is the horizontal position of the glyph origin in default user space,
	// in points. The origin sits on the baseline, lifted by any text rise.
	X float64 `json:"x"`
	// Y is the vertical position of the glyph origin, in the same space as X.
	Y float64 `json:"y"`
	// Width is the advance of the glyph in default user space, in points. It
	// leaves out character and word spacing, which sit in the gap between
	// elements.
	Width float64 `json:"width"`
	// FontSize is the font size as drawn, in points, once the text matrix and
	// the CTM scale it.
	FontSize float64 `json:"fontSize"`
	// SpaceWidth is the advance of the font's space glyph at the size and
	// horizontal scaling the element is drawn at, in the units of Width. It is
	// left out of the JSON form.
	SpaceWidth float64 `json:"-"`
	// Chunk numbers the run of the content stream the element comes from.
	// Elements that share a Chunk value come from one chain of show-text
	// operators, with no text object or text positioning operator between
	// them. The value is zero when omitted from the JSON form.
	Chunk int `json:"chunk,omitempty"`
	// MCID is the marked-content identifier of the innermost enclosing
	// marked-content sequence that carries one, or -1 when the text sits in
	// none.
	MCID int `json:"mcid"`
	// Hidden reports a glyph whose origin falls outside the clipping path the
	// page set with W or W*. A page that sets none never marks an element
	// hidden.
	Hidden bool `json:"hidden,omitempty"`
	// Invisible reports text drawn in rendering mode 3 or painted only in
	// white (ISO 32000-1, 9.3.6, Table 106).
	Invisible bool `json:"invisible,omitempty"`
	// Vertical reports a font in vertical writing mode (ISO 32000-1, 9.7.4.2).
	Vertical bool `json:"vertical,omitempty"`
	// Angle is the direction the glyph advances along, in radians, measured
	// from the positive X axis of default user space.
	Angle float64 `json:"angle,omitempty"`
	// InkRight reports that horizontal text inks the right of its advance, the side
	// a quarter turn clockwise from it, rather than the left. On the left, glyphs
	// rise to their ascenders as the rendering matrix of ISO 32000-1, 9.4.4 sets
	// them upright.
	//
	// A negative horizontal scaling, a mirrored text matrix and a mirrored CTM each
	// reflect the glyphs onto the right, and any two of them cancel. So does a Type
	// 3 font that draws its glyphs below its text-space baseline (9.6.5). A
	// negative font size turns the glyphs a half turn without moving them across
	// the advance, which Angle already carries.
	InkRight bool `json:"inkRight,omitempty"`
	// InkBox is the axis-aligned box [minX minY maxX maxY], in default user space,
	// holding the ink the element paints and its origin. Redaction tests it against
	// a region. It is all zeros on an element that does not come from extraction.
	//
	// A run's box is the union of its glyphs'. Each glyph's bounds are carried
	// through the text rendering matrix of ISO 32000-1, 9.4.4,
	// [Tfs×Th 0 0 Tfs 0 Trise] × Tm × CTM, so a skewed, turned or mirrored matrix
	// gives the box of the parallelogram it draws. The bounds depend on the glyph:
	//
	//   - A Type 3 glyph is bounded by what its description paints and the box its
	//     d1 states (9.6.5), mapped by /FontMatrix. A glyph drawn in pixels and
	//     shown at Tf 1 is bounded where it inks, however small its font size.
	//   - A Type 3 glyph whose description paints what no box holds, a shading over
	//     no clip of its own, is bounded by the clipping path it is shown under.
	//   - Any other glyph is bounded by its advance along the baseline and by the
	//     font's /FontBBox across it, never less than an ascender of one em and a
	//     descender of a fifth of one.
	//   - When the text rendering mode strokes the glyph (9.3.6, Table 106), the
	//     box grows by the reach of the pen: half the line width through the CTM,
	//     √2 times that under a projecting square cap (8.4.3.3). A miter join's tip
	//     at a sharp corner may reach past it (8.4.3.5).
	InkBox [4]float64 `json:"inkBox,omitzero"`
	// Redacted reports an element whose ink overlaps an unapplied /Redact
	// annotation. Only Reader.ExtractRedactedText sets it.
	Redacted bool `json:"redacted,omitempty"`
	// IsClipped reports a glyph whose origin falls outside the page's crop
	// box, the region a viewer shows (ISO 32000-1, 14.11.2). It stays false
	// when the crop box lies wholly outside the media box.
	IsClipped bool `json:"isClipped,omitempty"`
	// PatternType is "Tiling" or "Shading" when the glyph is painted with a
	// pattern of that type (ISO 32000-1, 8.7.3.1, Table 75), and empty
	// otherwise.
	PatternType string `json:"patternType,omitempty"`
	// Language is the BCP 47 tag of the nearest enclosing marked-content
	// sequence that carries a /Lang entry, or empty when none does.
	Language string `json:"language,omitempty"`
	// ExpansionText is the /E expansion of the nearest enclosing
	// marked-content sequence that carries one, the full form of an
	// abbreviation or acronym (ISO 32000-1, 14.9.5). It is empty when none
	// does.
	ExpansionText string `json:"expansionText,omitempty"`
}

// Line represents a stroked line segment on a page, in absolute page coordinates.
// Concurrent reads of a value are safe; a write needs synchronization.
type Line struct {
	// X1 is the x coordinate of the start point, in points.
	X1 float64 `json:"x1"`
	// Y1 is the y coordinate of the start point, in points.
	Y1 float64 `json:"y1"`
	// X2 is the x coordinate of the end point, in points.
	X2 float64 `json:"x2"`
	// Y2 is the y coordinate of the end point, in points.
	Y2 float64 `json:"y2"`
}
