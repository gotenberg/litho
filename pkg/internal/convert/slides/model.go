package slides

import (
	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// Document is a presentation as its reader found it. A reader fills every
// field a file gives a value for, resolving masters, layouts and
// placeholders into each shape, and leaves the rest zero; the builder
// reads the model and never changes it.
type Document struct {
	// Meta is the document information written to the PDF.
	Meta compose.Metadata
	// Width and Height are the slide size in points (ECMA-376 Part 1,
	// 19.2.1.36 sldSz; ODF fo:page-width and fo:page-height of the master
	// pages' page layout), both positive.
	Width, Height float64
	// NotesWidth and NotesHeight are the notes page size in points
	// (19.2.1.22 notesSz; the notes master's page layout), both positive
	// when the file has one, else 0 for the builder's default.
	NotesWidth, NotesHeight float64
	// NotesSlideBox and NotesTextBox are the boxes of the notes master
	// (19.3.1.27 notesMaster, the sldImg and body placeholders; ODF
	// presentation:notes with its page-thumbnail and notes frames) the
	// slide picture and the notes text take on a notes page, in notes page
	// points; both zero for the builder's default layout.
	NotesSlideBox, NotesTextBox compose.Rect
	// NotesShapes are the notes master's other shapes, drawn on every
	// notes page: the slide number, date and footer placeholders when they
	// show, and its decorations.
	NotesShapes []*Shape
	// Slides lists the slides in show order. A hidden slide is listed and
	// not printed unless the options ask for it.
	Slides []*Slide
}

// Slide is one slide (ECMA-376 Part 1, 19.3.1.38 sld; ODF 1.3 Part 3, 10.2.4
// draw:page).
type Slide struct {
	// Name is what the slide's outline entry shows, as Impress writes the
	// bookmarks of the format: the title placeholder's text of a
	// PresentationML slide, the draw:name of an OpenDocument page; "Slide
	// 3" when the file gives none.
	Name string
	// Title is the text of the slide's title placeholder, "" for none: the
	// slide's heading for the structure tree and the fields.
	Title string
	// Hidden marks a slide the show skips (19.3.1.38 show false; ODF
	// presentation:visibility hidden).
	Hidden bool
	// Background paints the whole slide first: the slide's own, or the
	// layout's or the master's the reader resolved.
	Background Fill
	// Shapes are the shapes drawn on the slide in stacking order, the
	// master's and the layout's shapes that show through first, as the
	// reader merged them.
	Shapes []*Shape
	// Notes is the text of the slide's notes, by paragraph, for a notes
	// page; nil for none.
	Notes []Paragraph
}

// Shape is one shape of a slide. Kind says which fields apply; Box,
// Rotation, FlipH, FlipV, Name and Href apply to every kind.
type Shape struct {
	Kind Kind
	// Box is the shape's bounding box in slide points, before Rotation and
	// the flips, which turn the shape about the box's centre (ECMA-376
	// Part 1, 20.1.7.6 xfrm; ODF svg:x, svg:y, svg:width, svg:height and
	// draw:transform, resolved by the reader to a box and an angle).
	Box compose.Rect
	// Rotation turns the shape clockwise by this many degrees about the
	// box's centre; FlipH and FlipV mirror it about the centre first.
	Rotation     float64
	FlipH, FlipV bool
	// Geometry is the outline of a KindGeometry shape, nil for a
	// rectangle.
	Geometry *shape.Geometry
	// Fill paints the inside of a KindGeometry shape; Line strokes its
	// outline. A connector is a KindGeometry shape of a line preset with
	// Line set.
	Fill Fill
	Line Line
	// Text is the text body of a KindGeometry shape, nil for none.
	Text *TextBody
	// Picture is the picture of a KindPicture shape.
	Picture *layout.Picture
	// Chart is the chart of a KindChart shape.
	Chart *chart.Chart
	// Table is the table of a KindTable shape.
	Table *Table
	// Kids are the shapes of a KindGroup, in stacking order, with boxes in the
	// group's child coordinate space, which Child maps onto Box
	// (20.1.7.6 chOff and chExt); a zero Child is Box itself.
	Kids  []*Shape
	Child compose.Rect
	// Href is the hyperlink the shape carries, "" for none.
	Href string
	// Jump is the 1-based show position of the slide the shape links to
	// (ECMA-376 Part 1, 19.3.1.19 hlinkClick with the ppaction://hlinkshowjump
	// and hlinksldjump actions; ODF presentation:event-listener), 0 for
	// none: the builder writes a link to that slide's page.
	Jump int
	// Name is the shape's name, for warnings.
	Name string
}

// Kind is the kind of a shape.
type Kind uint8

// The shape kinds.
const (
	// KindGeometry is a shape drawn from its geometry with its fill and
	// line, with a text body when it has one: an autoshape, a text box, a
	// placeholder, a line or a connector.
	KindGeometry Kind = iota
	// KindPicture is a raster or vector picture, cropped by the reader.
	KindPicture
	// KindChart is a chart the chart package draws.
	KindChart
	// KindTable is a table of text cells.
	KindTable
	// KindGroup is a group of shapes.
	KindGroup
)

// Fill is how an area is painted (ECMA-376 Part 1, 20.1.8 the fill
// properties; ODF 1.3 Part 3, 20.182 draw:fill).
type Fill struct {
	// Kind says which fields apply; FillNone paints nothing.
	Kind FillKind
	// Color is the colour of a FillSolid, and the foreground of a
	// FillPattern.
	Color css.Color
	// Gradient is the gradient of a FillGradient.
	Gradient *Gradient
	// Picture is the picture of a FillPicture, stretched over the area
	// unless Tile is set.
	Picture *layout.Picture
	// Tile repeats a FillPicture at its natural size instead of
	// stretching it.
	Tile bool
	// Background is the background colour of a FillPattern.
	Background css.Color
	// Pattern names a FillPattern's preset (20.1.10.51 ST_PresetPatternVal:
	// "pct50", "dkUpDiag", ...; ODF draw:hatch styles mapped onto one).
	Pattern string
}

// FillKind is the kind of a fill.
type FillKind uint8

// The fill kinds.
const (
	FillNone FillKind = iota
	FillSolid
	FillGradient
	FillPicture
	FillPattern
)

// Gradient is a gradient fill (ECMA-376 Part 1, 20.1.8.33 gradFill; ODF
// 1.3 Part 3, 16.40.1 draw:gradient).
type Gradient struct {
	// Stops are the colour stops by rising Position, at least two.
	Stops []Stop
	// Angle is the direction of a linear gradient in degrees, clockwise,
	// 0 pointing right (20.1.8.41 lin ang); Radial makes it a radial
	// gradient instead, Angle ignored.
	Angle  float64
	Radial bool
	// CenterX and CenterY are the centre of a radial gradient as fractions
	// of the box's width and height (20.1.8.46 path with fillToRect; ODF
	// draw:cx and draw:cy), 0.5 each for the middle: the reader resolves
	// them. Border is the share of the gradient's run, in [0, 1), painted
	// in the first stop's colour before the stops begin (ODF draw:border);
	// 0 for none.
	CenterX, CenterY float64
	Border           float64
}

// Stop is one colour stop of a gradient.
type Stop struct {
	// Position is the stop's place in [0, 1].
	Position float64
	Color    css.Color
}

// Line is how an outline is stroked (ECMA-376 Part 1, 20.1.2.2.24 ln; ODF
// 1.3 Part 3, 20.187 draw:stroke and the svg:stroke-* properties). A zero
// Width strokes nothing.
type Line struct {
	// Width is the stroke width in points.
	Width float64
	// Hairline draws the thinnest line the output allows, whatever Width
	// says (ODF svg:stroke-width 0; a DrawingML line of width 0): a PDF
	// zero-width line.
	Hairline bool
	// Color is the stroke colour.
	Color css.Color
	// Dash is the dash pattern in points, dash then gap, repeated; nil for
	// a solid line. The reader resolves a preset dash (20.1.10.48
	// ST_PresetLineDashVal) to the lengths Impress draws at Width.
	Dash []float64
	// Cap is the line cap; Join the join.
	Cap  Cap
	Join Join
	// Head and Tail are the arrowheads at the line's start and end.
	Head, Tail Arrow
}

// Cap is a line cap (ECMA-376 Part 1, 20.1.10.31 ST_LineCap).
type Cap uint8

// The line caps.
const (
	CapFlat Cap = iota
	CapRound
	CapSquare
)

// Join is a line join (ECMA-376 Part 1, 20.1.8.52 to 20.1.8.54).
type Join uint8

// The line joins.
const (
	JoinRound Join = iota
	JoinBevel
	JoinMiter
)

// Arrow is an arrowhead (ECMA-376 Part 1, 20.1.8.37 headEnd and 20.1.8.57
// tailEnd; ODF draw:marker-start and draw:marker-end mapped onto the
// DrawingML kinds).
type Arrow struct {
	// Kind is the arrowhead kind (20.1.10.33 ST_LineEndType), ArrowNone
	// for none.
	Kind ArrowKind
	// Width and Length are the arrowhead's size relative to the line
	// width: 1 for the medium size, 0.5 small, 2 large (20.1.10.32 and
	// 20.1.10.34).
	Width, Length float64
}

// ArrowKind is an arrowhead kind.
type ArrowKind uint8

// The arrowhead kinds.
const (
	ArrowNone ArrowKind = iota
	ArrowTriangle
	ArrowStealth
	ArrowDiamond
	ArrowOval
	ArrowOpen
)

// TextBody is the text of a shape and how it sits in the shape (ECMA-376
// Part 1, 21.1.2.1.1 bodyPr; ODF 1.3 Part 3, 20.300 fo:padding, 20.386
// draw:textarea-vertical-align and the related properties).
type TextBody struct {
	// Paragraphs are the paragraphs, in order; an empty paragraph keeps
	// its line.
	Paragraphs []Paragraph
	// Insets are the distances from the text rectangle to the text,
	// indexed by [css.Side].
	Insets [4]float64
	// Anchor places the text vertically in the text rectangle.
	Anchor Anchor
	// Wrap wraps the lines at the text rectangle's width; false lets them
	// run on, the box growing as Autofit says.
	Wrap bool
	// Autofit says what happens when the text does not fit the box.
	Autofit Autofit
	// Clip cuts the text at the text rectangle instead of letting it
	// overflow (ODF loext:overflow-behavior clip); false lets it run past.
	Clip bool
	// FontScale and SpacingReduction are what a file saved for
	// AutofitShrink (21.1.2.1.2 normAutofit fontScale and lnSpcReduction),
	// as fractions of 1: 0.8 and 0.2; 0 when the file saved none, and the
	// builder computes the fit itself.
	FontScale, SpacingReduction float64
	// Columns is the number of text columns, 1 or 0 for one; ColumnGap
	// the space between them in points.
	Columns   int
	ColumnGap float64
	// Vertical lays the text out vertically (21.1.2.1.1 vert: "vert",
	// "vert270", "eaVert"; ODF style:writing-mode tb-rl), as
	// css.WritingMode names the modes.
	Vertical css.WritingMode
	// RightToLeft is the body's base direction.
	RightToLeft bool
	// FontLineSpacing makes a line as tall as its faces' ascent plus
	// descent, as Impress lays an OpenDocument text out unless the
	// paragraph's style:font-independent-line-spacing is true (ODF 1.3
	// Part 3, 20.330); false, the default, makes every line 1.2 times its
	// largest size tall with the baseline the size below the line's top,
	// whatever the face, as Impress lays a PresentationML text out.
	FontLineSpacing bool
	// MaxParagraphSpacing makes the space between two paragraphs the
	// larger of the first's SpaceAfter and the second's SpaceBefore, as
	// Impress stacks OpenDocument paragraphs; false, the default, adds the
	// two, as it stacks PresentationML paragraphs.
	MaxParagraphSpacing bool
}

// Anchor is the vertical placement of a text body (ECMA-376 Part 1,
// 20.1.10.60 ST_TextAnchoringType).
type Anchor uint8

// The anchors.
const (
	AnchorTop Anchor = iota
	AnchorMiddle
	AnchorBottom
)

// Autofit is what a text body does with text that does not fit (ECMA-376
// Part 1, 21.1.2.1.2 normAutofit, 21.1.2.1.3 noAutofit, 21.1.2.1.4
// spAutoFit; ODF draw:fit-to-size and draw:auto-grow-height).
type Autofit uint8

// The autofit kinds.
const (
	// AutofitNone lets the text overflow the box.
	AutofitNone Autofit = iota
	// AutofitShrink shrinks the text until it fits, as Impress scales the
	// font size and the spacing down.
	AutofitShrink
	// AutofitGrow grows the box's height to the text.
	AutofitGrow
)

// Paragraph is one paragraph of a text body (ECMA-376 Part 1, 21.1.2.2.6 p
// with 21.1.2.2.7 pPr; ODF text:p with its paragraph style).
type Paragraph struct {
	// Runs are the paragraph's runs, in order; an empty slice is an empty
	// paragraph, whose line takes Style's size.
	Runs []Run
	// Style is the run style an empty paragraph's line and the bullet
	// default to (21.1.2.2.3 endParaRPr).
	Style RunStyle
	// Align is the horizontal alignment.
	Align css.TextAlign
	// Level is the list level, 0 for the first.
	Level int
	// MarginLeft is the indent of the paragraph from the text rectangle's
	// left edge in points; Indent is the first line's offset from it,
	// negative for a hanging indent where the bullet sits (21.1.2.2.7
	// marL and indent).
	MarginLeft, Indent float64
	// MarginRight is the indent from the right edge in points.
	MarginRight float64
	// Bullet is the paragraph's bullet or number, nil for none.
	Bullet *Bullet
	// LineSpacing is the line spacing as a fraction of the single spacing
	// (1 for single) when LineSpacingPoints is 0, else LineSpacingPoints
	// is the exact spacing in points (21.1.2.2.5 lnSpc with spcPct or
	// spcPts).
	LineSpacing       float64
	LineSpacingPoints float64
	// SpaceBefore and SpaceAfter are the spacing around the paragraph in
	// points; SpaceBeforePct and SpaceAfterPct their percentage-of-size
	// forms when a file gives those instead, as fractions of 1.
	SpaceBefore, SpaceAfter       float64
	SpaceBeforePct, SpaceAfterPct float64
	// RightToLeft is the paragraph's base direction.
	RightToLeft bool
	// TabStops are the paragraph's tab stop positions in points from the
	// text rectangle's left edge.
	TabStops []float64
}

// Bullet is a paragraph's bullet or number (ECMA-376 Part 1, 21.1.2.4 the
// bullet properties; ODF 1.3 Part 3, 16.31 text:list-level-style-bullet
// and 16.32 text:list-level-style-number).
type Bullet struct {
	// Kind says which fields apply.
	Kind BulletKind
	// Char is the bullet character of a BulletChar, with Families the
	// typefaces it draws in (empty: the paragraph's first run's).
	Char     string
	Families []string
	// Format is the number format of a BulletNumber (21.1.10.62
	// ST_TextAutonumberScheme: "arabicPeriod", "alphaLcParenR",
	// "romanUcPeriod", ...; an ODF style:num-format with its prefix and
	// suffix mapped onto one). StartAt, when not 0, restarts the level's
	// count at that number for this paragraph, as Impress numbers a
	// paragraph whose file states a start (21.1.2.4.1 buAutoNum startAt);
	// 0 continues the count.
	Format  string
	StartAt int
	// Picture is the picture of a BulletPicture.
	Picture *layout.Picture
	// Color is the bullet colour; the zero Color takes the text's.
	Color css.Color
	// Scale is the bullet size as a fraction of the text size, 1 when 0;
	// Points the bullet size in points when a file sets that instead.
	Scale  float64
	Points float64
}

// BulletKind is a bullet kind.
type BulletKind uint8

// The bullet kinds.
const (
	BulletNone BulletKind = iota
	BulletChar
	BulletNumber
	BulletPicture
)

// Run is a span of text with one style (ECMA-376 Part 1, 21.1.2.3.8 r,
// 21.1.2.3.1 br, 21.1.2.3.6 fld; ODF text:span and the text fields).
type Run struct {
	// Text is the run's text; a Break run has none.
	Text string
	// Style is the run's style.
	Style RunStyle
	// Break ends the line (21.1.2.3.1 br; ODF text:line-break).
	Break bool
	// Field, when not FieldNone, is what the run shows, Text being the
	// text the file saved for it.
	Field Field
	// Format is the number format code (SpreadsheetML syntax, as numfmt
	// reads it) a FieldDate or FieldTime run shows the print date in, ""
	// for the format Impress gives a field of that kind by default.
	Format string
	// Href is the run's hyperlink, "" for none.
	Href string
}

// RunStyle is the style of a run (ECMA-376 Part 1, 21.1.2.3.9 rPr; ODF
// 1.3 Part 3, 16.29.x text properties), resolved by the reader through
// the placeholder's, the layout's, the master's and the theme's styles.
type RunStyle struct {
	// Families lists the typefaces in order of preference: the Latin,
	// then the East Asian and complex script faces, theme fonts resolved.
	Families []string
	// Size is the font size in points, positive.
	Size float64
	// Bold and Italic select the face.
	Bold, Italic bool
	// Underline and Strike decorate the text; Strike draws one line.
	Underline bool
	Strike    bool
	// Color is the text colour, black when zero.
	Color css.Color
	// Spacing is the extra space between characters in points.
	Spacing float64
	// Script raises or lowers the text, Baseline for neither.
	Script Script
	// Caps draws the text in capitals: CapsAll or CapsSmall.
	Caps Caps
	// Lang is the run's language as a BCP 47 tag, "" for the document's.
	Lang string
}

// Script is a vertical text position.
type Script uint8

// The vertical text positions.
const (
	Baseline Script = iota
	Superscript
	Subscript
)

// Caps is a capitals transform (ECMA-376 Part 1, 20.1.10.64
// ST_TextCapsType).
type Caps uint8

// The capitals transforms.
const (
	CapsNone Caps = iota
	CapsAll
	CapsSmall
)

// Field is a text field (ECMA-376 Part 1, 21.1.2.3.6 fld type; ODF 1.3
// Part 3, 7.3 and 10.9.2 the presentation fields). FieldDate and FieldTime
// show office.Options.PrintDate when it is set, else the text the file
// saved.
type Field uint8

// The fields.
const (
	FieldNone Field = iota
	// FieldSlideNumber is the slide's number in the show.
	FieldSlideNumber
	// FieldSlideCount is the number of slides printed.
	FieldSlideCount
	// FieldDate is the date; FieldTime the time.
	FieldDate
	FieldTime
	// FieldSlideName is the slide's name; FieldTitle the document title;
	// FieldAuthor the author; FieldFile the file name, which shows
	// nothing.
	FieldSlideName
	FieldTitle
	FieldAuthor
	FieldFile
)

// Table is a table shape (ECMA-376 Part 1, 21.1.3.13 tbl; ODF 1.3 Part 3,
// 9.1.2 table:table in a draw:frame).
type Table struct {
	// ColumnWidths are the column widths in points.
	ColumnWidths []float64
	// Rows are the rows, each with its height in points and its cells, one
	// per column: a cell a span covers is listed with Covered set.
	Rows []TableRow
}

// TableRow is one row of a table.
type TableRow struct {
	Height float64
	Cells  []TableCell
}

// TableCell is one cell of a table (ECMA-376 Part 1, 21.1.3.16 tc; ODF
// table:table-cell).
type TableCell struct {
	// Text is the cell's text, nil for an empty cell.
	Text *TextBody
	// Fill paints the cell; Borders stroke its sides, indexed by
	// [css.Side], a zero Width for none.
	Fill    Fill
	Borders [4]Line
	// ColSpan and RowSpan are the cells the cell spans, 1 for its own
	// alone; Covered marks a cell another cell's span covers.
	ColSpan, RowSpan int
	Covered          bool
}
