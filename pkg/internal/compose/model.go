package compose

import "image"

// Document is a laid-out document: its pages in order, document-wide
// metadata, the outline and, when the converter kept the semantics of its
// input, the structure tree the fragments hang from.
type Document struct {
	// Pages holds every page in reading order. A document needs at least
	// one page.
	Pages []*Page
	// Metadata is what the Info dictionary and the XMP packet carry.
	Metadata Metadata
	// Outline is the bookmark tree, empty for none.
	Outline []Outline
	// Structure is the root of the structure tree (ISO 32000-1, 14.7), nil
	// for an untagged document. Its role is Document. A fragment whose
	// Parent is nil while Structure is set is written as an artifact.
	Structure *StructElem
	// Warnings lists what the converter could not render faithfully, in
	// the order it met them. A caller surfaces them so a reader learns why
	// a page differs from the source instead of guessing.
	Warnings []Warning
}

// Metadata is the document information a converter knows from its input.
// Dates are left out on purpose: the output must not depend on when it was
// made.
type Metadata struct {
	Title    string
	Author   string
	Subject  string
	Keywords string
	// Creator names the application that made the source, when known.
	Creator string
	// Lang is the document's natural language as a BCP 47 tag, "" for
	// unknown.
	Lang string
}

// Warning is one construct of the input the converter did not render as the
// source meant it.
type Warning struct {
	// Code is a stable, machine-readable identifier in kebab-case, such as
	// "unsupported-css-property" or "font-fallback".
	Code string
	// Message explains the construct and what was done instead.
	Message string
	// Page is the 1-based page the construct lands on, 0 when it has none.
	Page int
}

// Page is one output page and what is painted on it.
type Page struct {
	// Width and Height are the page size in points. Both are positive.
	Width, Height float64
	// Content lists the fragments in paint order: a later fragment covers
	// an earlier one.
	Content []Fragment
	// Links are the clickable regions of the page.
	Links []Link
	// TrimBox and BleedBox, when set, are the page's trim and bleed boxes
	// in page coordinates (ISO 32000-1, 14.11.2): where the printed sheet
	// is cut, and how far the content painted past the cut reaches. Each
	// lies within the page, the trim box within the bleed box. nil leaves
	// the box out, which a reader takes as the media box.
	TrimBox, BleedBox *Rect
}

// Fragment is something painted on a page: a [Text] run, an [Image], a
// [Path] or a [Group]. The set is closed.
type Fragment interface {
	fragment()
}

// Point is a position in page or group coordinates.
type Point struct {
	X, Y float64
}

// Rect is an axis-aligned rectangle given by its top-left corner and size.
type Rect struct {
	X, Y, Width, Height float64
}

// Matrix is an affine transform [A B C D E F] mapping (x, y) to
// (A*x + C*y + E, B*x + D*y + F), the PDF convention (ISO 32000-1, 8.3.3).
type Matrix struct {
	A, B, C, D, E, F float64
}

// Identity is the transform that changes nothing.
var Identity = Matrix{A: 1, D: 1}

// Color is an sRGB colour with straight (not premultiplied) alpha. Every
// channel runs from 0 to 1; A is 1 for opaque. A fully transparent colour
// paints nothing and is refused: omit the fragment instead. That rule also
// catches the zero value, which would otherwise paint invisible text.
type Color struct {
	R, G, B, A float64
}

// Black is opaque black.
var Black = Color{A: 1}

// Face is one font program a [Text] run draws with.
type Face struct {
	// Program is a single sfnt program, TrueType or OpenType with CFF
	// outlines. A collection is refused: extract the face first.
	Program []byte
	// Key identifies the program for deduplication: two faces with the
	// same key are embedded once. A converter derives it from the file
	// path and face index, or from a hash of the program.
	Key string
}

// Glyph is one positioned glyph of a [Text] run.
type Glyph struct {
	// GID is the glyph index in the run's face.
	GID uint16
	// X and Y place the glyph origin relative to the run's origin, in
	// points, y downward. The converter has already applied advances,
	// kerning and positioning offsets.
	X, Y float64
	// Text is the characters this glyph stands for, for text extraction
	// and search: a cluster's characters on its first glyph, "" on a glyph
	// that continues the cluster or stands for nothing.
	Text string
}

// Text is a run of glyphs from one face at one size and colour.
type Text struct {
	Face *Face
	// Size is the font size in points.
	Size float64
	// Color fills the glyphs.
	Color Color
	// Origin is the baseline origin of the run on the page.
	Origin Point
	// Glyphs are the run's glyphs in paint order.
	Glyphs []Glyph
	// Parent is the structure element the run belongs to, nil for an
	// artifact or an untagged document.
	Parent *StructElem
	// Stroke, when positive, also strokes the glyph outlines in the run's
	// colour with a line of this width in points, centred on the outline:
	// text rendering mode 2, fill then stroke (ISO 32000-1, 9.3.6). A
	// converter sets it to embolden a face that has no bold, as browsers
	// synthesise bold. Zero fills only. It is at most [Text.Size].
	Stroke float64
	// Skew slants the glyphs by Skew points to the right per point above
	// the baseline, a shear about each glyph's baseline origin in the text
	// matrix (ISO 32000-1, 9.4.2). A converter sets it to slant a face that
	// has no italic, as browsers synthesise italic with a skew of 0.25.
	// Zero draws upright; a negative value leans left. Its magnitude is at
	// most 1.
	Skew float64
	// HScale scales the glyphs horizontally about their origins, the text
	// state's horizontal scaling (ISO 32000-1, 9.3.4): the glyph shapes
	// and the positions along the baseline are both scaled, so a converter
	// lays the run out at the scaled advances and writes the unscaled
	// positions. 1 and 0 draw at the natural width. It lies in [MinHScale,
	// MaxHScale].
	HScale float64
}

// The bounds of [Text.HScale] (ISO 32000-1, 9.3.4 puts no bound on Tz; the
// Office formats scale text between 1% and 600%).
const (
	MinHScale = 0.01
	MaxHScale = 6
)

// ImageSource is the pixels of an [Image], shared by every placement of it.
// Exactly one of Image and JPEG is set.
type ImageSource struct {
	// Image is a decoded picture, written lossless. Its alpha, when any,
	// becomes a soft mask.
	Image image.Image
	// JPEG is a JPEG file kept as it is, written as a DCTDecode stream.
	JPEG []byte
}

// Image places a picture on the page, scaled to fill Rect.
type Image struct {
	Rect   Rect
	Source *ImageSource
	// Parent is the structure element the image belongs to, usually a
	// Figure, nil for an artifact or an untagged document.
	Parent *StructElem
}

// OpKind is the kind of a [PathOp].
type OpKind uint8

// The path construction operators (ISO 32000-1, 8.5.2).
const (
	// MoveTo starts a subpath at P[0].
	MoveTo OpKind = iota
	// LineTo draws a straight segment to P[0].
	LineTo
	// CurveTo draws a cubic Bézier through control points P[0] and P[1] to
	// P[2].
	CurveTo
	// ClosePath closes the current subpath.
	ClosePath
)

// PathOp is one path construction step.
type PathOp struct {
	Kind OpKind
	P    [3]Point
}

// FillRule chooses how a path's interior is decided.
type FillRule uint8

// The fill rules (ISO 32000-1, 8.5.3.3.2 and 8.5.3.3.3).
const (
	NonZero FillRule = iota
	EvenOdd
)

// Fill paints a path's interior with a flat colour or, when Shading is set,
// a smooth colour transition.
type Fill struct {
	// Color is the flat colour. It is ignored, and may be the zero value,
	// when Shading is set.
	Color Color
	Rule  FillRule
	// Shading, when set, paints the interior instead of Color.
	Shading *Shading
}

// ShadingKind is the geometry of a [Shading].
type ShadingKind uint8

// The shading geometries.
const (
	// Axial varies the colour along the line from From to To and keeps it
	// constant on every line perpendicular to it (ISO 32000-1, 8.7.4.5.3,
	// type 2 shading).
	Axial ShadingKind = iota
	// Radial varies the colour across the circles that blend the circle
	// of centre From and radius FromRadius into the circle of centre To
	// and radius ToRadius (ISO 32000-1, 8.7.4.5.4, type 3 shading).
	Radial
)

// ColorStop is one colour of a [Shading] at one point of its parameter.
type ColorStop struct {
	// Offset runs from 0, at From, to 1, at To.
	Offset float64
	// Color is the colour at Offset. Unlike a flat colour it may be fully
	// transparent.
	Color Color
}

// Shading is a smooth colour transition, axial or radial. Between two
// stops every channel, alpha included, interpolates linearly and
// independently: the colour is not premultiplied by its alpha, so a
// converter whose source interpolates premultiplied colours adds stops to
// follow that curve. Two stops at one offset make a sharp edge. Past
// either end the shading extends over the whole interior of the path it
// fills, with the colour the stops reach at that end: the last stop at
// offset 0 before From, the first stop at offset 1 past To.
type Shading struct {
	Kind ShadingKind
	// From and To are the start and end points: the ends of the axis of an
	// Axial shading, the centres of the start and end circles of a Radial
	// one. They are in the shading's own coordinates, which Transform maps
	// to the path's. An Axial shading needs them apart.
	From, To Point
	// FromRadius and ToRadius are the radii of a Radial shading's circles,
	// in the shading's coordinates, non-negative and not both zero. An
	// Axial shading ignores them. Two equal circles are refused.
	FromRadius, ToRadius float64
	// Stops holds at least two stops in non-decreasing Offset order, the
	// first at 0 and the last at 1. At least one stop is not fully
	// transparent.
	Stops []ColorStop
	// Transform maps the shading's coordinates to the coordinates of the
	// path it fills; it must be invertible. The zero value is replaced by
	// [Identity]. A Radial shading under a transform that scales x and y
	// apart paints ellipses.
	Transform Matrix
}

// LineCap is the shape at the end of an open stroked subpath.
type LineCap uint8

// The line cap styles (ISO 32000-1, 8.4.3.3).
const (
	ButtCap LineCap = iota
	RoundCap
	SquareCap
)

// LineJoin is the shape at the corner of a stroked path.
type LineJoin uint8

// The line join styles (ISO 32000-1, 8.4.3.4).
const (
	MiterJoin LineJoin = iota
	RoundJoin
	BevelJoin
)

// Stroke paints a path's outline.
type Stroke struct {
	Color Color
	// Width is the line width in points. Zero asks for the thinnest line
	// the device can draw.
	Width float64
	Cap   LineCap
	Join  LineJoin
	// Miter is the miter limit, 10 when zero.
	Miter float64
	// Dash is the dash array in points, empty for a solid line.
	Dash []float64
	// DashPhase is the distance into the dash pattern at which to start.
	DashPhase float64
}

// Path is a vector shape in page coordinates, filled, stroked or both. A
// path with neither Fill nor Stroke paints nothing.
type Path struct {
	Ops    []PathOp
	Fill   *Fill
	Stroke *Stroke
	// Parent is the structure element the path belongs to, nil for an
	// artifact, which is what decoration almost always is.
	Parent *StructElem
}

// Group paints its content under a transform, a clip and a constant
// opacity. Nested groups compose. A group with Opacity 1, no Clip and the
// identity transform is a plain sequence.
type Group struct {
	// Transform maps the group's coordinates to its parent's. The zero
	// value is replaced by [Identity].
	Transform Matrix
	// Clip restricts painting to the interior of the path, in the group's
	// coordinates, nil for none. Only the path's Ops and the fill rule of
	// its Fill, when set, matter.
	Clip *Path
	// Opacity applies to the group as a whole, from 0 to 1. Zero is
	// replaced by 1, so a converter that wants nothing painted omits the
	// group.
	Opacity float64
	Content []Fragment
}

func (*Text) fragment()  {}
func (*Image) fragment() {}
func (*Path) fragment()  {}
func (*Group) fragment() {}

// Dest is a position in the document a link or an outline entry goes to.
type Dest struct {
	// Page is the 1-based page number.
	Page int
	// X and Y are the position on the page, in page coordinates, that the
	// viewer scrolls to the top-left corner of its window.
	X, Y float64
}

// Link is a clickable region. Exactly one of URI and Dest is set.
type Link struct {
	Rect Rect
	// URI is an external target.
	URI string
	// Dest is an internal target.
	Dest *Dest
	// Parent is the Link structure element the region belongs to, nil for
	// an untagged document.
	Parent *StructElem
}

// Outline is one bookmark and its children.
type Outline struct {
	Title string
	Dest  Dest
	Kids  []Outline
}

// StructElem is one node of the structure tree. Fragments point at the
// element they belong to; the element's content on each page is written in
// paint order as marked-content sequences.
type StructElem struct {
	// Role is a standard structure type (ISO 32000-1, 14.8.4): Document,
	// Part, Sect, H1 to H6, P, L, LI, Lbl, LBody, Table, TR, TH, TD,
	// Figure, Link, Span, Code, Quote, Note, Caption.
	Role string
	// Lang overrides the document language for this subtree, "" for none.
	Lang string
	// Alt describes a Figure or a Link for a reader who cannot see it.
	Alt string
	// ActualText replaces the subtree's content for extraction, "" for
	// none.
	ActualText string
	// Kids are the child elements in logical reading order.
	Kids []*StructElem
}
