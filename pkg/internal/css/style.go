package css

// This file is the contract between the CSS cascade and the layout engine:
// every field of [Style] is a computed value (CSS 2.1, 6.1.2) in the units
// the layout engine works in. A later wave adds fields; it does not change
// the meaning of one that exists.

// Pt converts a CSS pixel count to points: 1px is 1/96 in and 1pt is 1/72
// in (CSS Values 4, 6.2).
const Pt = 0.75

// DimKind says what a [Dimension] holds.
type DimKind uint8

// The kinds of [Dimension]. Each property says which of them it takes.
const (
	// Auto is the auto keyword.
	Auto DimKind = iota
	// Points is an absolute length, resolved from any absolute or
	// font-relative unit.
	Points
	// Percent is a percentage of a reference the property names, kept as
	// written because the reference is a used value.
	Percent
	// None is the none keyword (max-width, max-height).
	None
	// Normal is the normal keyword (line-height, letter-spacing,
	// word-spacing).
	Normal
	// Number is a unitless number (line-height, tab-size).
	Number
)

// Dimension is a computed length, percentage, number or keyword.
type Dimension struct {
	Kind DimKind
	// Value is the length in points for Points, the fraction of the
	// reference for Percent (0.5 for 50%), the number for Number, and 0
	// otherwise.
	Value float64
}

// Pts is the Dimension of an absolute length in points.
func Pts(v float64) Dimension { return Dimension{Kind: Points, Value: v} }

// Color is an sRGB colour with straight alpha, every channel from 0 to 1.
// The zero value is transparent: it paints nothing. currentColor is already
// resolved to the element's colour.
type Color struct {
	R, G, B, A float64
}

// Transparent paints nothing.
var Transparent = Color{}

// Display is the display property (CSS 2.1, 9.2.4). flex, grid and the
// other CSS 3 values compute to Block until a later wave lays them out.
type Display uint8

// The display values.
const (
	Inline Display = iota
	Block
	InlineBlock
	ListItem
	Table
	InlineTable
	TableRowGroup
	TableHeaderGroup
	TableFooterGroup
	TableRow
	TableColumnGroup
	TableColumn
	TableCell
	TableCaption
	// DisplayNone generates no box.
	DisplayNone
)

// Position is the position property (CSS 2.1, 9.3.1). Sticky computes to
// Relative: paged output never scrolls.
type Position uint8

// The position values.
const (
	Static Position = iota
	Relative
	Absolute
	Fixed
)

// ZIndex is the z-index property (CSS 2.1, 9.9.1): auto, or the integer
// stack level of a positioned box's stacking context.
type ZIndex struct {
	// Auto reports z-index: auto, which creates no stacking context.
	Auto bool
	// Level is the stack level when Auto is false.
	Level int
}

// TransformKind says what a [TransformFunc] is (CSS Transforms 1, 13).
type TransformKind uint8

// The 2D transform functions.
const (
	// TransformMatrix is matrix(a, b, c, d, e, f): V holds a to f, e and f
	// in points.
	TransformMatrix TransformKind = iota
	// TransformTranslate is translate(X, Y), translateX and translateY.
	TransformTranslate
	// TransformScale is scale(V[0], V[1]), scaleX and scaleY.
	TransformScale
	// TransformRotate is rotate(V[0]), V[0] in radians, clockwise on the
	// page.
	TransformRotate
	// TransformSkew is skew(V[0], V[1]), skewX and skewY, in radians.
	TransformSkew
)

// TransformFunc is one function of a transform list (CSS Transforms 1, 13).
type TransformFunc struct {
	Kind TransformKind
	// X and Y are the offsets of a translation, Points or Percent of the
	// border box's width and height (CSS Transforms 1, 13.1).
	X, Y Dimension
	// V holds the numbers of the other functions.
	V [6]float64
}

// Float is the float property (CSS 2.1, 9.5.1).
type Float uint8

// The float values.
const (
	FloatNone Float = iota
	FloatLeft
	FloatRight
)

// Clear is the clear property (CSS 2.1, 9.5.2).
type Clear uint8

// The clear values.
const (
	ClearNone Clear = iota
	ClearLeft
	ClearRight
	ClearBoth
)

// Visibility is the visibility property (CSS 2.1, 11.2). Collapse behaves
// as Hidden outside tables.
type Visibility uint8

// The visibility values.
const (
	Visible Visibility = iota
	Hidden
	VisibilityCollapse
)

// Overflow is the overflow property (CSS 2.1, 11.1.1). On paged output
// scroll and auto behave as visible, so they compute to OverflowVisible.
type Overflow uint8

// The overflow values.
const (
	OverflowVisible Overflow = iota
	OverflowHidden
)

// BoxSizing is the box-sizing property (CSS UI 3, 4.1).
type BoxSizing uint8

// The box-sizing values.
const (
	ContentBox BoxSizing = iota
	BorderBox
)

// BoxEdge names an edge of the box model (CSS Backgrounds 3, 3.7 and 3.8).
type BoxEdge uint8

// The box edges.
const (
	BorderEdge BoxEdge = iota
	PaddingEdge
	ContentEdge
)

// Side indexes the four sides of a box in the order CSS shorthands list
// them.
type Side uint8

// The sides.
const (
	Top Side = iota
	Right
	Bottom
	Left
)

// Corner indexes the four corners of a box in the order border-radius lists
// them.
type Corner uint8

// The corners.
const (
	TopLeft Corner = iota
	TopRight
	BottomRight
	BottomLeft
)

// BorderStyle is a border-*-style value (CSS 2.1, 8.5.3).
type BorderStyle uint8

// The border styles.
const (
	BorderNone BorderStyle = iota
	BorderHidden
	Dotted
	Dashed
	Solid
	Double
	Groove
	Ridge
	Inset
	Outset
)

// Border is one side's border.
type Border struct {
	// Width is the computed width in points, snapped as a border width
	// (CSS Values 4, 6.1.1): a width under 1px rounds up to 1px, a wider one
	// down to a whole pixel. It keeps the specified length whatever Style
	// is, as Blink, Gecko and WebKit do (CSS Backgrounds 3, 4.3); the
	// used width is 0 when Style is BorderNone or BorderHidden, and the
	// layout engine applies that.
	Width float64
	Style BorderStyle
	Color Color
}

// Radius is one corner's border-radius: the horizontal and vertical radii,
// each Points or Percent of the border box's width and height respectively.
type Radius struct {
	H, V Dimension
}

// Repeat is one axis of background-repeat (CSS Backgrounds 3, 3.4).
type Repeat uint8

// The repeat values.
const (
	RepeatRepeat Repeat = iota
	RepeatNoRepeat
	RepeatSpace
	RepeatRound
)

// GradientKind is the shape of a [Gradient].
type GradientKind uint8

// The gradient shapes (CSS Images 3, 3.1 and 3.2).
const (
	LinearGradient GradientKind = iota
	RadialGradient
)

// RadialSize is the size of a radial gradient's ending shape (CSS Images
// 3, 3.2.1, <radial-extent>), or RadialExplicit for a size given as
// lengths.
type RadialSize uint8

// The radial sizes. The zero value is the default, farthest-corner.
const (
	FarthestCorner RadialSize = iota
	ClosestSide
	ClosestCorner
	FarthestSide
	// RadialExplicit takes the size from Gradient.Radius.
	RadialExplicit
)

// GradientStop is one colour stop or transition hint of a gradient's
// colour stop list (CSS Images 3, 3.4; transition hints from CSS Images
// 4, 3.5.3), in the order written. A stop written with two positions is
// two stops of one colour.
type GradientStop struct {
	// Color is the stop's colour, currentColor resolved. A hint has none.
	Color Color
	// Position is Auto for a stop written without one, else Points or
	// Percent of the gradient line's length for a linear gradient and of
	// the ending shape's horizontal radius for a radial one. The fixup of
	// CSS Images 3, 3.4.3 is left to the layout engine, which knows the
	// length.
	Position Dimension
	// Hint marks a transition hint: Position is set and Color unused.
	Hint bool
}

// Gradient is a computed <gradient> (CSS Images 3, 3): a linear or radial
// one, repeating or not. A conic gradient is not represented.
type Gradient struct {
	Kind      GradientKind
	Repeating bool
	// Angle is the direction of a linear gradient in degrees, clockwise
	// from toward the top (CSS Images 3, 3.1.1), when Corner is zero. "to
	// bottom", the default, is 180.
	Angle float64
	// Corner points a linear gradient at a corner of its box, horizontal
	// then vertical, -1 for left or top and +1 for right or bottom; zero
	// otherwise. Its angle depends on the box, so it stays a keyword.
	Corner [2]int8
	// Circle selects a circular ending shape over an elliptical one.
	Circle bool
	Size   RadialSize
	// Radius is an explicit size: a circle's radius in Radius[0], Points;
	// an ellipse's horizontal then vertical radius, Points or Percent of
	// the box's width and height.
	Radius [2]Dimension
	// Center is the centre of a radial gradient, horizontal then vertical,
	// Points or Percent of the box's width and height from its top-left
	// corner (CSS Images 3, 3.2.1, the at <position> clause).
	Center [2]Dimension
	// Stops holds at least one colour stop, hints only between two stops.
	Stops []GradientStop
}

// SizeKind says what a [BackgroundSize] holds.
type SizeKind uint8

// The background-size kinds.
const (
	// SizeExplicit uses Width and Height, each Auto, Points or Percent of
	// the background positioning area.
	SizeExplicit SizeKind = iota
	SizeCover
	SizeContain
)

// BackgroundSize is the background-size property (CSS Backgrounds 3, 3.9).
type BackgroundSize struct {
	Kind          SizeKind
	Width, Height Dimension
}

// Shadow is one shadow of box-shadow (CSS Backgrounds 3, 7.1).
type Shadow struct {
	// X and Y are the horizontal and vertical offsets, Blur the blur
	// radius, at least 0, and Spread the spread distance, all in points.
	X, Y, Blur, Spread float64
	Color              Color
	// Inset casts the shadow inside the padding box instead of outside
	// the border box.
	Inset bool
	// current records a colour given as currentColor, resolved again by
	// an element that inherits the shadow explicitly (CSS Color 4, 4.4).
	current bool
}

// ObjectFit is the object-fit property (CSS Images 3, 5.5).
type ObjectFit uint8

// The object-fit values.
const (
	FitFill ObjectFit = iota
	FitContain
	FitCover
	FitNone
	FitScaleDown
)

// Containment is the contain property's bit set (CSS Containment 2, 2).
type Containment uint8

// The containment bits. strict and content set several of them.
const (
	ContainSize Containment = 1 << iota
	ContainInlineSize
	ContainLayout
	ContainStyle
	ContainPaint
)

// FontStyle is the font-style property (CSS Fonts 4, 2.4). Oblique is
// treated as italic.
type FontStyle uint8

// The font-style values.
const (
	FontNormal FontStyle = iota
	FontItalic
	FontOblique
)

// FontVariant is the font-variant property (CSS 2.1, 15.5).
type FontVariant uint8

// The font-variant values.
const (
	VariantNormal FontVariant = iota
	SmallCaps
)

// TextAlign is the text-align property (CSS Text 3, 6.1). Start is the
// initial value: left in a left-to-right block, right in a right-to-left
// one.
type TextAlign uint8

// The text-align values.
const (
	AlignStart TextAlign = iota
	AlignEnd
	AlignLeft
	AlignRight
	AlignCenter
	AlignJustify
)

// TextDecoration is the text-decoration-line bit set (CSS Text Decoration
// 3, 2.1). It is not inherited; the layout engine propagates it to inline
// descendants as CSS 2.1, 16.3.1 asks.
type TextDecoration uint8

// The text-decoration-line bits.
const (
	Underline TextDecoration = 1 << iota
	Overline
	LineThrough
)

// TextTransform is the text-transform property (CSS Text 3, 2.1).
type TextTransform uint8

// The text-transform values.
const (
	TransformNone TextTransform = iota
	Capitalize
	Uppercase
	Lowercase
)

// WhiteSpace is the white-space property (CSS Text 3, 3).
type WhiteSpace uint8

// The white-space values.
const (
	WhiteSpaceNormal WhiteSpace = iota
	Pre
	Nowrap
	PreWrap
	PreLine
)

// WordBreak is the word-break property (CSS Text 3, 5.2).
type WordBreak uint8

// The word-break values.
const (
	WordBreakNormal WordBreak = iota
	BreakAll
	KeepAll
)

// OverflowWrap is the overflow-wrap property (CSS Text 3, 5.5). word-wrap
// is an alias.
type OverflowWrap uint8

// The overflow-wrap values.
const (
	OverflowWrapNormal OverflowWrap = iota
	BreakWord
	Anywhere
)

// AlignKind says what a [VerticalAlign] holds.
type AlignKind uint8

// The vertical-align keywords (CSS 2.1, 10.8.1) and the length form.
const (
	Baseline AlignKind = iota
	Sub
	Super
	AlignTop
	TextTop
	Middle
	AlignBottom
	TextBottom
	// AlignLength raises the box by Length, Points or Percent of the
	// element's line-height.
	AlignLength
)

// VerticalAlign is the vertical-align property.
type VerticalAlign struct {
	Kind   AlignKind
	Length Dimension
}

// Direction is the direction property (CSS Writing Modes 4, 2.1).
type Direction uint8

// The direction values.
const (
	LTR Direction = iota
	RTL
)

// UnicodeBidi is the unicode-bidi property (CSS Writing Modes 4, 2.2).
type UnicodeBidi uint8

// The unicode-bidi values.
const (
	BidiNormal UnicodeBidi = iota
	Embed
	Isolate
	BidiOverride
	IsolateOverride
	Plaintext
)

// ListStyleType is the list-style-type property (CSS 2.1, 12.5.1).
type ListStyleType uint8

// The list-style-type values.
const (
	Disc ListStyleType = iota
	Circle
	Square
	Decimal
	DecimalLeadingZero
	LowerRoman
	UpperRoman
	LowerAlpha
	UpperAlpha
	LowerGreek
	Armenian
	Georgian
	ListNone
)

// ListStylePosition is the list-style-position property (CSS 2.1, 12.5.1).
type ListStylePosition uint8

// The list-style-position values.
const (
	Outside ListStylePosition = iota
	Inside
)

// BorderCollapse is the border-collapse property (CSS 2.1, 17.6).
type BorderCollapse uint8

// The border-collapse values.
const (
	SeparateBorders BorderCollapse = iota
	CollapseBorders
)

// CaptionSide is the caption-side property (CSS 2.1, 17.4.1).
type CaptionSide uint8

// The caption-side values.
const (
	CaptionTop CaptionSide = iota
	CaptionBottom
)

// EmptyCells is the empty-cells property (CSS 2.1, 17.6.1.1).
type EmptyCells uint8

// The empty-cells values.
const (
	Show EmptyCells = iota
	Hide
)

// TableLayout is the table-layout property (CSS 2.1, 17.5.2).
type TableLayout uint8

// The table-layout values.
const (
	TableAuto TableLayout = iota
	TableFixed
)

// Break is a break-before or break-after value (CSS Fragmentation 3, 3.1).
// The page-break-* properties map onto it: always to Page.
type Break uint8

// The break-before and break-after values.
const (
	BreakAuto Break = iota
	BreakAvoid
	BreakAvoidPage
	Page
	LeftPage
	RightPage
)

// BreakInside is the break-inside value (CSS Fragmentation 3, 3.2).
type BreakInside uint8

// The break-inside values.
const (
	InsideAuto BreakInside = iota
	InsideAvoid
	InsideAvoidPage
)

// Style is the computed style of one element. The zero value is not a
// valid style: start from [Initial].
type Style struct {
	// Box generation.

	Display    Display
	Position   Position
	Float      Float
	Clear      Clear
	Visibility Visibility
	Overflow   Overflow
	BoxSizing  BoxSizing
	// Opacity runs from 0 to 1 (CSS Color 3, 3.2).
	Opacity float64
	// ZIndex is the stack level of a positioned box (CSS 2.1, 9.9.1).
	ZIndex ZIndex
	// Transform is the 2D transform list, nil for none (CSS Transforms 1,
	// 5); TransformOrigin holds its horizontal then vertical origin, each
	// Points or Percent of the border box (CSS Transforms 1, 6).
	Transform       []TransformFunc
	TransformOrigin [2]Dimension
	// OuterInline reports a box whose display was inline-level before CSS
	// 2.1, 9.7 blockified it: an absolutely positioned box's static
	// position is then where its inline box would have sat on the line
	// (CSS Position 3, 4.1).
	OuterInline bool

	// Box dimensions. Width and Height take Auto, Points or Percent;
	// MinWidth and MinHeight take Points or Percent; MaxWidth and MaxHeight
	// take None, Points or Percent. Top, Right, Bottom and Left are the
	// offsets of a positioned box, Auto, Points or Percent.

	Width, Height       Dimension
	MinWidth, MinHeight Dimension
	MaxWidth, MaxHeight Dimension
	Top, Right          Dimension
	Bottom, Left        Dimension
	// Margin takes Auto, Points or Percent a side; Padding takes Points or
	// Percent. Both are indexed by [Side].
	Margin  [4]Dimension
	Padding [4]Dimension
	// Border is indexed by [Side].
	Border [4]Border
	// BorderRadius is indexed by [Corner].
	BorderRadius [4]Radius

	// Background (CSS Backgrounds 3). A single layer.

	BackgroundColor Color
	// BackgroundImage is the resolved URL of the image, "" for none. A
	// gradient computes to "" and sets BackgroundGradient instead.
	BackgroundImage string
	// BackgroundGradient is the gradient the image is, nil for none, a
	// URL or a gradient litho does not paint (a conic one, reported by
	// the cascade as a warning). It is shared and never mutated.
	BackgroundGradient *Gradient
	// BackgroundRepeat holds the horizontal then the vertical repeat.
	BackgroundRepeat [2]Repeat
	// BackgroundPosition holds the horizontal then the vertical offset,
	// each Points or Percent: a percentage p puts the image's p point over
	// the area's p point (CSS Backgrounds 3, 3.6).
	BackgroundPosition [2]Dimension
	BackgroundSize     BackgroundSize
	BackgroundOrigin   BoxEdge
	BackgroundClip     BoxEdge

	// Decoration around the box.

	// BoxShadow lists the shadows in the order written, the first painted
	// on top; nil for none (CSS Backgrounds 3, 7.1).
	BoxShadow []Shadow
	// Outline is the outline's width, style and colour (CSS UI 3, 5): the
	// width computed as a border width is, the style auto computed to
	// Solid. The used width is 0 when the style is BorderNone.
	Outline Border
	// OutlineOffset is the outline-offset in points (CSS UI 3, 5.5).
	OutlineOffset float64
	// Contain is the contain property (CSS Containment 2, 2). The engine
	// reads ContainPaint, which stops the body's background reaching the
	// canvas (CSS Backgrounds 3, 2.11.2).
	Contain Containment

	// Replaced content.

	// ObjectFit sizes a replaced element's picture in its content box
	// (CSS Images 3, 5.5).
	ObjectFit ObjectFit
	// ObjectPosition places the picture in the content box, as
	// BackgroundPosition places a background (CSS Images 3, 5.6).
	ObjectPosition [2]Dimension

	// Colour and font.

	Color Color
	// FontFamilies is the family list as written, generic names included;
	// the text stack resolves them.
	FontFamilies []string
	// FontSize is the computed size in points.
	FontSize float64
	// FontWeight is the numeric weight, 100 to 900.
	FontWeight  int
	FontStyle   FontStyle
	FontVariant FontVariant
	// LineHeight takes Normal, Points or Number (CSS 2.1, 10.8.1): a
	// percentage has become Points, a number stays a factor of the
	// font size of each descendant.
	LineHeight Dimension

	// Text.

	TextAlign      TextAlign
	TextDecoration TextDecoration
	// TextIndent takes Points or Percent of the containing block's width.
	TextIndent    Dimension
	TextTransform TextTransform
	// LetterSpacing and WordSpacing take Normal or Points.
	LetterSpacing Dimension
	WordSpacing   Dimension
	WhiteSpace    WhiteSpace
	WordBreak     WordBreak
	OverflowWrap  OverflowWrap
	// TabSize takes Number (a count of spaces) or Points.
	TabSize       Dimension
	VerticalAlign VerticalAlign
	Direction     Direction
	UnicodeBidi   UnicodeBidi

	// Lists.

	ListStyleType     ListStyleType
	ListStylePosition ListStylePosition
	// ListStyleImage is the resolved URL of the marker image, "" for none.
	ListStyleImage string

	// Tables.

	BorderCollapse BorderCollapse
	// BorderSpacing holds the horizontal then the vertical spacing in
	// points.
	BorderSpacing [2]float64
	CaptionSide   CaptionSide
	EmptyCells    EmptyCells
	TableLayout   TableLayout

	// Fragmentation (CSS Fragmentation 3).

	BreakBefore Break
	BreakAfter  Break
	BreakInside BreakInside
	// Orphans and Widows are the minimum line counts, at least 1.
	Orphans int
	Widows  int

	// currentColor records the colour longhands whose computed value is
	// the currentColor keyword (CSS Color 4, 4.4): the exported field
	// holds it resolved, and an element that inherits it explicitly
	// resolves it again against its own color.
	currentColor colorSlots
}

// colorSlots is a bit set of the colour longhands other than color.
type colorSlots uint8

// The colour longhands that can hold currentColor.
const (
	slotBackground colorSlots = 1 << iota
	slotBorderTop
	slotBorderRight
	slotBorderBottom
	slotBorderLeft
	slotOutline
)

// Initial returns the initial value of every property (CSS 2.1, appendix
// F), with the root's font size at medium, 16px.
func Initial() *Style {
	return &Style{
		Opacity:            1,
		ZIndex:             ZIndex{Auto: true},
		TransformOrigin:    [2]Dimension{{Kind: Percent, Value: 0.5}, {Kind: Percent, Value: 0.5}},
		MinWidth:           Pts(0),
		MinHeight:          Pts(0),
		MaxWidth:           Dimension{Kind: None},
		MaxHeight:          Dimension{Kind: None},
		Margin:             [4]Dimension{Pts(0), Pts(0), Pts(0), Pts(0)},
		Padding:            [4]Dimension{Pts(0), Pts(0), Pts(0), Pts(0)},
		BackgroundPosition: [2]Dimension{{Kind: Percent}, {Kind: Percent}},
		BackgroundOrigin:   PaddingEdge,
		ObjectPosition:     [2]Dimension{{Kind: Percent, Value: 0.5}, {Kind: Percent, Value: 0.5}},
		Color:              Color{A: 1},
		FontFamilies:       []string{"serif"},
		FontSize:           16 * Pt,
		FontWeight:         400,
		LineHeight:         Dimension{Kind: Normal},
		TextIndent:         Pts(0),
		LetterSpacing:      Dimension{Kind: Normal},
		WordSpacing:        Dimension{Kind: Normal},
		TabSize:            Dimension{Kind: Number, Value: 8},
		Orphans:            2,
		Widows:             2,
	}
}

// Orientation is the orientation part of the @page size descriptor (CSS
// Page 3, 5.1).
type Orientation uint8

// The orientation values.
const (
	OrientationAuto Orientation = iota
	Portrait
	Landscape
)

// PageStyle is the computed style of a page box (CSS Page 3).
type PageStyle struct {
	// Width and Height are the page size in points from the size
	// descriptor, both 0 when it is auto: the caller's paper applies.
	Width, Height float64
	// Orientation flips the caller's paper when Width and Height are 0.
	Orientation Orientation
	// Margin is indexed by [Side], Auto for a side the rules leave to the
	// caller, Points or Percent of the page size otherwise.
	Margin [4]Dimension
	// Background is the page box's background (CSS Page 3, page
	// backgrounds and painting order): a style whose Background fields
	// hold what the @page rules set, every other field initial. nil when
	// no rule sets a background property.
	Background *Style
}
