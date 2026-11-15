package typeset

import "github.com/gotenberg/litho/pkg/internal/warn"

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
	// Code names the kind of construct, one of the closed set of package
	// warn.
	Code warn.Code
	// Effect says what the construct's fault cost the output: content
	// left out, or content printed differently. It is never the zero
	// Effect.
	Effect warn.Effect
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
	// Fields are the interactive form fields of the page, in the order a
	// reader tabs through them.
	Fields []Field
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
	// Order places the run among the content of Parent on its page in
	// logical order, which a converter that paints out of that order
	// sets: an element's content on a page is read in increasing Order,
	// in paint order among equals (ISO 32000-1, 14.8.2.3.1). Zero
	// everywhere keeps paint order.
	Order int
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
	// Invisible draws the glyphs neither filled nor stroked, text
	// rendering mode 3 (ISO 32000-1, 9.3.6): the run stays in the page as
	// text, extracted and tagged, while something else draws what it
	// looks like, as an OCR layer under a scan does. Color, alpha
	// included, is then not painted. An invisible run has no Stroke.
	Invisible bool
	// HScale scales the glyphs horizontally about their origins, the text
	// state's horizontal scaling (ISO 32000-1, 9.3.4): the glyph shapes
	// and the positions along the baseline are both scaled, so a converter
	// lays the run out at the scaled advances and writes the unscaled
	// positions. 1 and 0 draw at the natural width. It lies in [MinHScale,
	// MaxHScale].
	HScale float64
	// Upright turns each glyph a quarter turn counter-clockwise about its
	// origin, which X and Y still place: the run belongs to a vertical
	// line laid out across the content of a group the page turns a
	// quarter turn clockwise (CSS Writing Modes 4, 7.3), and its glyphs
	// stand upright on the page (5.1). Each glyph is then placed by its
	// own text matrix; Skew does not apply, and HScale narrows each glyph
	// along its own width alone, X and Y placing it unscaled, as a
	// combined run of CSS text-combine-upright asks (9.1).
	Upright bool
	// Mirror reflects each glyph across the x axis of the run's space
	// through its origin, y to -y: the run belongs to a vertical-lr line
	// laid out across the content of a group the page reflects across its
	// diagonal (CSS Writing Modes 4, 3.1), and its glyphs read unreflected
	// on the page.
	Mirror bool
	// Palette, for a run whose face draws layered colour glyphs (OpenType,
	// COLR and CPAL), fills each layer with the entry of its palette
	// index; nil fills them with the face's first palette. A layer whose
	// index lies past the slice is drawn in Color, as the face's text
	// colour entry is. A face without layered glyphs ignores it.
	Palette []Color
	// Plain draws every glyph of a colour face from the program's outline,
	// in Color, never from its bitmap or its layers: no Type 3 font and no
	// glyph image is written for the run. A glyph of a program without
	// outlines is blank, of its advance. A face without colour glyphs
	// ignores it.
	Plain bool
}

// The bounds of [Text.HScale] (ISO 32000-1, 9.3.4 puts no bound on Tz; the
// Office formats scale text between 1% and 600%).
const (
	MinHScale = 0.01
	MaxHScale = 6
)

// ImageSource is the picture of an [Image] in the form the file stores it,
// shared by every placement of it: a JPEG file kept as it is, or 8-bit
// samples already Flate-compressed. [Write] copies these bytes into the
// file untouched, one image XObject per distinct source pointer.
//
// A converter builds a source with [EncodeImage] or [JPEGImage] when it
// meets the picture and drops the decoded pixels, so a document holds no
// decoded picture: a conversion keeps one picture decoded at a time, the
// one it is encoding. A converter that needs the pixels again, to crop or
// recolour them, calls [ImageSource.Decode] and encodes the result as a
// source of its own.
//
// Under a context carrying a [PictureStore], the constructors may keep the
// bytes in the store's temp file instead: JPEG, Samples and Alpha are then
// nil, [ImageSource.IsJPEG] tells the kind, and [ImageSource.Decode] and
// [Write] read the bytes back from the file.
//
// The constructors set the fields and nothing changes them after: a source
// is safe for concurrent reads.
type ImageSource struct {
	// Width and Height are the picture's size in pixels, both positive.
	Width, Height int
	// JPEG is a JPEG file written as a DCTDecode stream. When it is set,
	// Gray, Samples and Alpha are unset.
	JPEG []byte
	// Gray is true when Samples holds one DeviceGray sample a pixel, false
	// when it holds three, DeviceRGB.
	Gray bool
	// Samples is the zlib stream (RFC 1950), the format FlateDecode reads
	// (ISO 32000-1, 7.4.4), of the picture's 8-bit samples, row by row from
	// the top, colours straight, not premultiplied.
	Samples []byte
	// Alpha is the zlib stream of the picture's 8-bit alpha, one sample a
	// pixel, written as a DeviceGray soft mask (ISO 32000-1, 11.6.5.3); nil
	// when every pixel is opaque.
	Alpha []byte

	// stored is where a [PictureStore] keeps the bytes, nil when the
	// fields above hold them.
	stored *storedPicture
}

// Image places a picture on the page, scaled to fill Rect.
type Image struct {
	Rect   Rect
	Source *ImageSource
	// Parent is the structure element the image belongs to, usually a
	// Figure, nil for an artifact or an untagged document.
	Parent *StructElem
	// Order places the image among the content of Parent on its page in
	// logical order, which a converter that paints out of that order
	// sets: an element's content on a page is read in increasing Order,
	// in paint order among equals (ISO 32000-1, 14.8.2.3.1). Zero
	// everywhere keeps paint order.
	Order int
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
// a smooth colour transition, or, when Tiling is set, a tiling pattern. At
// most one of Shading and Tiling is set.
type Fill struct {
	// Color is the flat colour. It is ignored, and may be the zero value,
	// when Shading or Tiling is set.
	Color Color
	Rule  FillRule
	// Shading, when set, paints the interior instead of Color.
	Shading *Shading
	// Tiling, when set, paints the interior instead of Color.
	Tiling *Tiling
}

// Tiling is a coloured tiling pattern of constant spacing (ISO 32000-1,
// 8.7.3.1, PatternType 1, PaintType 1, TilingType 1): the content of one
// cell, painted once in the file and repeated in rows and columns by the
// reader over the interior it fills, as a background image or gradient
// repeated over a box is.
//
// The cell paints as one isolated transparency group, whose result the
// path's opacity and blend mode composite as a whole (11.6.7): a cell whose
// fragments overlap shows the overlap as a cell drawn alone does. A reader
// paints the cells at the device's resolution, so a cell much smaller than
// a device pixel shows its average colour or nothing.
type Tiling struct {
	// Content draws one cell, in the cell's coordinates: its top-left
	// corner at the origin, y down. What it paints outside the cell's
	// Width by Height is clipped. Its fragments hang from no structure
	// element: the pattern is an artifact.
	Content []Fragment
	// Width and Height are the size of the cell, positive.
	Width, Height float64
	// XStep and YStep are the distances between the origins of two
	// neighbouring cells across and down, positive: a step past the cell's
	// size leaves a gap between cells, a smaller one overlaps them.
	XStep, YStep float64
	// Transform maps the cell's coordinates to the coordinates the path
	// is in: one cell lands where it maps the origin, the others at
	// multiples of the steps from it. It is invertible. The zero value is
	// replaced by [Identity].
	Transform Matrix
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
	// Conic varies the colour with the angle about From: offset 0 on the
	// ray from From through To, the offset growing clockwise on the page
	// to 1 a full turn round. It is a function-based shading (ISO
	// 32000-1, 8.7.4.5.2, type 1) whose PostScript calculator function
	// (7.10.5) takes the angle with atan; the radii are ignored.
	Conic
)

// ColorStop is one colour of a [Shading] at one point of its parameter.
type ColorStop struct {
	// Offset runs from 0, at From, to 1, at To.
	Offset float64
	// Color is the colour at Offset. Unlike a flat colour it may be fully
	// transparent.
	Color Color
}

// Shading is a smooth colour transition, axial, radial or conic. Between two
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
	// Order places the path among the content of Parent on its page in
	// logical order, which a converter that paints out of that order
	// sets: an element's content on a page is read in increasing Order,
	// in paint order among equals (ISO 32000-1, 14.8.2.3.1). Zero
	// everywhere keeps paint order.
	Order int
}

// Group paints its content under a transform, a clip, a constant opacity
// and a blend mode. Nested groups compose. A group with Opacity 1, no
// Clip, the identity transform, [BlendNormal] and no Transparency is a
// plain sequence.
//
// Without Transparency, Opacity and Blend apply to each fragment of the
// content on its own (ISO 32000-1, 11.6.4.4 and 11.3.5): two overlapping
// fragments at opacity 0.5 show the overlap darker, and a blend mode
// blends each fragment with what lies beneath it, earlier fragments of
// the group included. With Transparency, the content is painted as one
// transparency group (11.4), and Opacity, Blend and the group's mask
// apply to the group's result as a whole: the overlap of two opaque
// fragments at opacity 0.5 shows as one translucent shape, which is what
// CSS `opacity` and SVG group opacity mean.
type Group struct {
	// Transform maps the group's coordinates to its parent's. The zero
	// value is replaced by [Identity].
	Transform Matrix
	// Clip restricts painting to the interior of the path, in the group's
	// coordinates, nil for none. Only the path's Ops and the fill rule of
	// its Fill, when set, matter.
	Clip *Path
	// Opacity is the constant alpha of the content, from 0 to 1. Zero is
	// replaced by 1, so a converter that wants nothing painted omits the
	// group.
	Opacity float64
	// Blend is the blend mode the content composites with. [BlendNormal]
	// leaves the enclosing group's blend mode in force, as an Opacity of 1
	// leaves its alpha: plain painting over outside every blending group.
	// Inside a transparency group's content the enclosing mode is Normal
	// again (ISO 32000-1, 11.6.6).
	Blend BlendMode
	// Transparency, when set, paints the content as one transparency
	// group, nil for none. It costs a form XObject in the file and, in a
	// renderer, a raster of the group's area.
	Transparency *TransparencyGroup
	Content      []Fragment
}

// BlendMode is how a colour painted composites with the backdrop beneath
// it: the sixteen blend modes of ISO 32000-1, 11.3.5, Tables 136 and 137,
// which CSS Compositing 1 names alike. Every version a conversion writes,
// PDF 1.4 on, holds them, and PDF/A-2 and later allow these and no other
// (ISO 19005-2, 6.2.10). The deprecated Compatible is not one of them.
type BlendMode uint8

// The blend modes. The separable ones (Table 136) blend each colour
// component on its own; the non-separable ones (Table 137) blend the hue,
// saturation and luminosity of the colour as a whole.
const (
	// BlendNormal paints the source over the backdrop.
	BlendNormal BlendMode = iota
	BlendMultiply
	BlendScreen
	BlendOverlay
	BlendDarken
	BlendLighten
	BlendColorDodge
	BlendColorBurn
	BlendHardLight
	BlendSoftLight
	BlendDifference
	BlendExclusion
	BlendHue
	BlendSaturation
	BlendColor
	BlendLuminosity
)

// GroupSpace is the colour space a transparency group composites its
// content in: the CS entry of its group attributes (ISO 32000-1, 11.6.6,
// Table 147).
type GroupSpace uint8

// The colour spaces of a transparency group.
const (
	// SpaceInherit leaves CS out: an isolated group then composites in the
	// space of the page's group or of the device, a non-isolated one in
	// its parent's (11.6.6).
	SpaceInherit GroupSpace = iota
	// SpaceRGB composites in DeviceRGB, which the model's sRGB colours are
	// written in.
	SpaceRGB
	// SpaceGray composites in DeviceGray: the colours of the content enter
	// the group as their grey.
	SpaceGray
)

// TransparencyGroup is how a [Group] composites as one transparency group
// (ISO 32000-1, 11.4 and 11.6.6). The zero value is a non-isolated,
// non-knockout group in its parent's colour space.
type TransparencyGroup struct {
	// Isolated composites the content over a fully transparent backdrop
	// rather than over what the page holds beneath the group (11.4.5), so
	// a blend mode inside the group blends only with the group's own
	// content: CSS `isolation: isolate`.
	Isolated bool
	// Knockout composites each fragment of the content with the group's
	// backdrop alone rather than with the fragments painted before it, so
	// a later fragment replaces an earlier one where it covers it
	// (11.4.6).
	Knockout bool
	// Space is the colour space the group composites in.
	Space GroupSpace
	// Mask, when set, masks the group's result by a soft mask, nil for
	// none.
	Mask *SoftMask
}

// MaskKind is how a [SoftMask] derives its mask values from its content
// (ISO 32000-1, 11.6.5.2, Table 144, entry S).
type MaskKind uint8

// The kinds of a soft mask.
const (
	// MaskLuminosity takes the luminosity of the content painted over
	// black: white keeps, black masks out, and where the content paints
	// nothing it masks out. CSS `mask-mode: luminance` and an SVG mask.
	MaskLuminosity MaskKind = iota
	// MaskAlpha takes the alpha of the content: where it paints opaque
	// keeps, where it paints nothing masks out. CSS `mask-mode: alpha`
	// and a CSS mask image.
	MaskAlpha
)

// SoftMask is a mask drawn by fragments: a soft mask dictionary whose
// group is the content (ISO 32000-1, 11.6.5.2). The mask's backdrop is
// black and its transfer function the identity: neither is supported
// otherwise.
type SoftMask struct {
	Kind MaskKind
	// Content draws the mask in the coordinates of the group it masks. It
	// is painted only to derive the mask, never shown, so its fragments
	// hang from no structure element.
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
	// Elem is the structure element the destination shows, nil for none.
	// A PDF 2.0 file names it as a structure destination beside the page
	// position (ISO 32000-2, 12.3.2.3), which ISO 14289-2, 8.8 asks of
	// every destination inside the document; an earlier version writes the
	// page position alone. It belongs to the document's structure tree.
	Elem *StructElem
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
	// Contents describes the link for a reader who cannot see it: the
	// annotation's /Contents (ISO 32000-1, 12.5.2, Table 164), which ISO
	// 14289-1, 7.18.5 requires of every link. A converter sets it to the
	// text the link shows, or to what its source says the link is for.
	// Empty, the Parent's Alt stands in, and with neither the entry is
	// left out.
	Contents string
}

// Outline is one bookmark and its children.
type Outline struct {
	Title string
	Dest  Dest
	Kids  []Outline
	// Closed starts the entry with its children hidden; the zero value
	// shows them.
	Closed bool
}

// StructElem is one node of the structure tree. Fragments point at the
// element they belong to; the element's content on each page is written as
// marked-content sequences, listed by the fragments' Order and in paint
// order among equals.
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
	// ColSpan and RowSpan are the columns and rows a TH or TD cell spans
	// (ISO 32000-1, 14.8.5.7, Table 349), 0 or 1 for one. With them the
	// rows of a table whose cells merge count the same columns, as ISO
	// 14289-1, 7.2 requires. Other roles ignore them. Each is at most
	// [MaxCellSpan].
	ColSpan, RowSpan int
	// Scope says which cells a TH cell heads (Table 349), [ScopeNone] to
	// leave the attribute out. Other roles ignore it.
	Scope TableScope
	// Numbering is the numbering system of the labels of an L element
	// (ISO 32000-1, 14.8.5.5, Table 347), [NumberingUnset] to leave the
	// attribute out. Other roles ignore it.
	Numbering ListNumbering
	// PrintField describes the non-interactive form field a Form element
	// shows. The backend writes it on a Form element that holds no
	// widget annotation, in a PDF 1.7 or PDF 2.0 file: the attributes are
	// PDF 1.7 ones, and a Form element holding its field's widget keeps
	// to that widget (ISO 32000-1, 14.8.4.5, Table 340). Other roles
	// ignore it.
	PrintField PrintField
	// Kids are the child elements in logical reading order.
	Kids []*StructElem
}

// MaxCellSpan bounds [StructElem.ColSpan] and [StructElem.RowSpan].
// Anti-DoS guard, not spec-mandated: a span is an integer attribute a
// reader multiplies into a table grid, and no sheet or page holds more
// cells across than a spreadsheet's 16,384 columns or down than its
// 1,048,576 rows.
const MaxCellSpan = 1 << 20

// TableScope says which cells a header cell heads: the Scope attribute of a
// TH element (ISO 32000-1, 14.8.5.7, Table 349).
type TableScope uint8

// The scopes of a header cell.
const (
	// ScopeNone leaves the attribute out.
	ScopeNone TableScope = iota
	// ScopeRow heads the cells of the cell's row.
	ScopeRow
	// ScopeColumn heads the cells of the cell's column.
	ScopeColumn
	// ScopeBoth heads the cells of both.
	ScopeBoth
)

// ListNumbering is the numbering system of a list's labels: the
// ListNumbering attribute of an L element. The values past
// [NumberingLowerAlpha] are those PDF 2.0 adds (ISO 32000-2, 14.8.5.5,
// Table 382); a file of an earlier version writes the nearest value its
// version defines (ISO 32000-1, Table 347).
type ListNumbering uint8

// The numbering systems of a list.
const (
	// NumberingUnset leaves the attribute out.
	NumberingUnset ListNumbering = iota
	// NumberingNone is a list whose items carry no label.
	NumberingNone
	// NumberingDisc, NumberingCircle and NumberingSquare are bullets of
	// that shape.
	NumberingDisc
	NumberingCircle
	NumberingSquare
	// NumberingDecimal is Arabic numerals.
	NumberingDecimal
	// NumberingUpperRoman and NumberingLowerRoman are Roman numerals.
	NumberingUpperRoman
	NumberingLowerRoman
	// NumberingUpperAlpha and NumberingLowerAlpha are letters.
	NumberingUpperAlpha
	NumberingLowerAlpha
	// NumberingOrdered is an ordered list numbered another way, such as
	// with the numerals of another script.
	NumberingOrdered
	// NumberingUnordered is an unordered list labelled another way, such
	// as with a picture or a dash.
	NumberingUnordered
	// NumberingDescription is a list of terms and their descriptions.
	NumberingDescription
)

// PrintField describes the non-interactive form field a Form element
// shows: its PrintField attributes (ISO 32000-1, 14.8.5.6, Table 348; ISO
// 32000-2, 14.8.5.6). A Form element with them holds the content that
// represents the field, where one without holds only the widget annotation
// of an interactive field (ISO 32000-1, 14.8.4.5, Table 340). The zero
// value says nothing.
type PrintField struct {
	// Role is the kind of field, [PrintRoleNone] to leave every
	// attribute out.
	Role PrintRole
	// Checked is the state of a check box or a radio button,
	// [CheckUnset] to leave the attribute out, which a reader takes as
	// off. Other roles ignore it.
	Checked CheckState
	// Desc describes the field, as a field's alternate name does
	// ([Field.Description]); "" leaves it out. It holds at most
	// [MaxFieldText] bytes of UTF-8.
	Desc string
}

// PrintRole is the kind of a non-interactive form field: the Role
// attribute of the PrintField owner (ISO 32000-1, Table 348).
type PrintRole uint8

// The kinds of a non-interactive form field.
const (
	// PrintRoleNone leaves the PrintField attributes out.
	PrintRoleNone PrintRole = iota
	// PrintRadio is a radio button, rb.
	PrintRadio
	// PrintCheckBox is a check box, cb.
	PrintCheckBox
	// PrintPushButton is a push button, pb.
	PrintPushButton
	// PrintText is a field whose value shows as text, tv: a text field, a
	// drop-down list or a combo box.
	PrintText
	// PrintList is a list box: lb in a PDF 2.0 file (ISO 32000-2, Table
	// 383), tv in an older one, whose Table 348 has no list role.
	PrintList
)

// CheckState is the state of a check box or a radio button: the checked
// attribute of the PrintField owner (ISO 32000-1, Table 348).
type CheckState uint8

// The states of a check box or a radio button.
const (
	// CheckUnset leaves the attribute out.
	CheckUnset CheckState = iota
	// CheckOn is on.
	CheckOn
	// CheckOff is off.
	CheckOff
	// CheckNeutral is neither on nor off.
	CheckNeutral
)
