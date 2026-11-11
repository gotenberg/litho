package sheet

import (
	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// Document is a spreadsheet as its reader found it: the sheets in book
// order and what the book carries as a whole. A reader fills every field
// a file gives a value for and leaves the rest zero; the builder reads the
// model and never changes it.
type Document struct {
	// Meta is the document information written to the PDF.
	Meta compose.Metadata
	// Default is the style a cell, row or column without one draws with:
	// the book's default font and the General format (ECMA-376 Part 1,
	// 18.8.11, the Normal cell style; ODF 1.3 Part 3, 16.5, the Default
	// cell style).
	Default Style
	// Sheets lists the sheets in book order. A hidden sheet is listed and
	// not printed.
	Sheets []*Sheet
	// ExcelRules lays the book out as Microsoft Excel prints a workbook
	// rather than as Calc prints a spreadsheet, where the two differ: a
	// row with a Height keeps it whatever its text needs, and a row
	// without one grows to fit wrapped text alone, a default row a line,
	// since Excel prints a row at its stored height and fits a row without
	// one to its lines as it opens a workbook. The reader sets the
	// lengths, paddings and page setups in
	// Excel's terms; the flag covers what the builder decides. The XLSX
	// reader sets it; the OpenDocument and CSV readers leave it false.
	ExcelRules bool
}

// Sheet is one worksheet (ECMA-376 Part 1, 18.3; ODF 1.3 Part 3, 9.1.2,
// table:table).
type Sheet struct {
	// Name is the sheet's name, what the sheet-name field of a header
	// shows.
	Name string
	// Hidden marks a sheet that is not printed (ECMA-376 Part 1, 18.2.19
	// state hidden or veryHidden; ODF table:display false).
	Hidden bool
	// RightToLeft lays the columns out from the right edge (ECMA-376 Part
	// 1, 18.3.1.87 rightToLeft; ODF style:writing-mode rl-tb on the table).
	RightToLeft bool
	// HideZeros leaves a cell whose value is the number zero blank when
	// the sheet is printed, as the reader finds the file asks for (ODF
	// style:print on the page layout without zero-values; a workbook's
	// showZeros view setting when LibreOffice honours it in print).
	HideZeros bool
	// DefaultColumnWidth is the width of a column no Column covers, and
	// DefaultRowHeight the height of a row no Row lists or whose Height is
	// 0; both positive.
	DefaultColumnWidth, DefaultRowHeight float64
	// Columns are the column ranges with a width, a style or a hidden flag
	// of their own, by rising First, without overlap.
	Columns []Column
	// Rows are the rows holding cells or a height, a style or a hidden
	// flag of their own, by rising Index.
	Rows []Row
	// Merges are the merged ranges (ECMA-376 Part 1, 18.3.1.55; ODF
	// table:number-columns-spanned and number-rows-spanned): the top-left
	// cell of each holds the content and the style of the whole.
	Merges []Range
	// Page is the sheet's page setup, every field resolved by the reader
	// to what the file or the application's default gives.
	Page PageSetup
	// HeaderFooter is what the page margins show.
	HeaderFooter HeaderFooter
	// Drawings are the pictures and charts anchored on the sheet, in
	// drawing order.
	Drawings []Drawing
	// Conditional are the sheet's conditional formats, in the order the
	// file lists them; a rule earlier in the list, or of a lower Priority
	// when both set one, wins over a later one for a cell both match.
	Conditional []Conditional
	// Pivots are the sheet's pivot tables, whose output the builder
	// computes from their source cells and writes over the sheet's cells,
	// as Calc rebuilds a pivot table when it loads a workbook.
	Pivots []Pivot
}

// Conditional is one conditional format: the ranges it applies to and
// its rules (ECMA-376 Part 1, 18.3.1.18 conditionalFormatting and 18.3.1.10
// cfRule; ODF 1.3 Part 3, 9.4.6 calcext:conditional-format and the
// style:map of a cell style).
type Conditional struct {
	// Ranges are the cells the rules apply to.
	Ranges []Range
	// Rules are the rules, each with the style it applies when it holds.
	Rules []Rule
}

// Rule is one rule of a conditional format. The builder evaluates the
// kinds whose operands are the cell's own value and constants, and
// reports the rest.
type Rule struct {
	// Kind says how the rule is evaluated.
	Kind RuleKind
	// Priority orders the rules of a sheet, the lowest winning (18.3.1.10
	// priority); 0 for a file that gives none, when list order decides.
	Priority int
	// StopIfTrue ends the evaluation of lower-priority rules for a cell
	// the rule holds for.
	StopIfTrue bool
	// Operator is the comparison of a RuleCellIs ("lessThan", "between",
	// "equal", ... as 18.18.15 ST_ConditionalFormattingOperator spells
	// them) and of a RuleText ("containsText", "beginsWith", "endsWith",
	// "notContains").
	Operator string
	// Values are the constant operands: one or two numbers or strings for
	// a RuleCellIs, the text of a RuleText, the thresholds of a scale, a
	// threshold's text "=" and a formula for one the builder computes,
	// relative to the format's first cell.
	Values []Value
	// Formula is the formula of a RuleExpression, or a non-constant
	// operand, as the file writes it; the builder does not evaluate it.
	Formula string
	// Style is the differential style the rule applies: the fields set
	// (a non-zero Font.Color, Font.Bold, a Fill.Color, a Border side, a
	// Format) override the cell's; nil for a rule without one.
	Style *Style
	// Scale is the colour scale of a RuleColorScale, by rising position:
	// the colours of the minimum, the midpoint when three, and the
	// maximum, whose thresholds Values hold as numbers, "min" and "max"
	// standing for the range's extremes when Values is empty.
	Scale []css.Color
	// Bar is the colour of a RuleDataBar; BarNegative the colour of a bar
	// below the axis, the zero Color for red; BarSolid paints the bar flat
	// instead of as a gradient to transparent; BarMinLength and
	// BarMaxLength are the shortest and longest bars as fractions of the
	// cell width, 0.1 and 0.9 when both are 0 (ECMA-376 Part 1, 18.3.1.28
	// dataBar and its x14 extension; ODF calcext:data-bar).
	Bar                        css.Color
	BarNegative                css.Color
	BarSolid                   bool
	BarMinLength, BarMaxLength float64
	// BarAxis places the zero of a RuleDataBar's bars (MS-XLSX x14:dataBar
	// axisPosition; ODF calcext:axis-position).
	BarAxis BarAxis
	// Icons is the icon set of a RuleIconSet by its SpreadsheetML name
	// ("3Arrows", "4Rating", "5Quarters", ...; ECMA-376 Part 1, 18.18.42
	// ST_IconSetType, which ODF's calcext:icon-set-type spells alike), its
	// thresholds in Values as numbers, the lowest first, one per icon: a
	// cell shows the icon of the highest threshold its number reaches,
	// the first icon below them all.
	Icons string
	// IconsAbove, when set, lists per threshold whether the number must
	// pass it rather than reach it (MS-XLSX x14:cfvo gte false; ECMA-376
	// Part 1, 18.3.1.11 cfvo gte).
	IconsAbove []bool
	// IconsReversed shows the icons in the reverse order; IconsOnly hides
	// the cell's value (18.3.1.49 iconSet reverse and showValue; ODF
	// calcext:show-value).
	IconsReversed, IconsOnly bool
	// IconsCustom, when not empty, names the icon of each threshold as
	// another set's icon, "3Flags:2", "" keeping the set's own and
	// "NoIcons:0" showing none (MS-XLSX x14:cfIcon).
	IconsCustom []string
}

// BarAxis is where a data bar's zero sits in its cells
// (ScDataBarFormatData::meAxisPosition).
type BarAxis uint8

// The data bar axes.
const (
	// BarAxisAuto puts zero where it falls between the thresholds, at the
	// left edge when both are positive.
	BarAxisAuto BarAxis = iota
	// BarAxisMiddle puts zero halfway, each side as long as the larger
	// magnitude, the bars at most BarMaxLength of their side.
	BarAxisMiddle
	// BarAxisNone draws every bar from the left edge, from BarMinLength
	// of the cell at the lower threshold to BarMaxLength at the upper.
	BarAxisNone
)

// RuleKind is the kind of a conditional formatting rule (ECMA-376 Part 1,
// 18.18.12 ST_CfType).
type RuleKind uint8

// The rule kinds.
const (
	// RuleCellIs compares the cell's value to Values with Operator.
	RuleCellIs RuleKind = iota
	// RuleText tests the cell's text with Operator against Values[0].
	RuleText
	// RuleExpression holds when Formula is true: not evaluated, reported.
	RuleExpression
	// RuleColorScale paints the cell's fill from Scale by where its value
	// sits between the thresholds.
	RuleColorScale
	// RuleDataBar paints a bar of Bar's colour across the cell, as long
	// as the value's share of the range's maximum.
	RuleDataBar
	// RuleIconSet shows an icon of the set Icons at the cell's left by
	// where its number sits among the thresholds.
	RuleIconSet
	// RuleDuplicates and RuleUniques hold for a value seen elsewhere in
	// the range, or nowhere else.
	RuleDuplicates
	RuleUniques
	// RuleBlanks and RuleNoBlanks hold for an empty cell, or a filled one.
	RuleBlanks
	RuleNoBlanks
	// RuleTop holds for the Values[0] largest values of the range,
	// RuleBottom the smallest; the reader resolves a percent variant to a
	// count.
	RuleTop
	RuleBottom
	// RuleAboveAverage and RuleBelowAverage compare to the range's mean.
	RuleAboveAverage
	RuleBelowAverage
)

// Column is a range of columns sharing a width, a style or a hidden flag
// (ECMA-376 Part 1, 18.3.1.13 col; ODF 1.3 Part 3, 9.1.6 table:table-column
// with number-columns-repeated).
type Column struct {
	// First and Last are the 0-based indexes of the range, inclusive.
	First, Last int
	// Width is the column width in points, 0 for the sheet's default.
	Width float64
	// Hidden marks columns that are not printed.
	Hidden bool
	// Style, when not nil, is the style the range's cells without one
	// take.
	Style *Style
}

// Row is one row (ECMA-376 Part 1, 18.3.1.73; ODF 1.3 Part 3, 9.1.3
// table:table-row).
type Row struct {
	// Index is the 0-based row index.
	Index int
	// Height is the row height in points, 0 for the sheet's default.
	Height float64
	// CustomHeight marks a height the user set (ECMA-376 Part 1, 18.3.1.73
	// customHeight; ODF style:use-optimal-row-height false): the row keeps
	// it. Otherwise the builder may grow the row to fit wrapped text, as
	// Calc computes an optimal height.
	CustomHeight bool
	// Hidden marks a row that is not printed.
	Hidden bool
	// Style, when not nil, is the style the row's cells without one take,
	// before a column's.
	Style *Style
	// Cells are the row's cells with a value or a style, by rising Column.
	Cells []Cell
}

// Cell is one cell (ECMA-376 Part 1, 18.3.1.4 c; ODF 1.3 Part 3, 9.1.4
// table:table-cell).
type Cell struct {
	// Column is the 0-based column index.
	Column int
	// Value is the cell's value, the one its file stores: a formula's
	// cached result, never a computation.
	Value Value
	// Runs, when not empty, is the cell's rich text, whose concatenation
	// is Value.Text: each run draws with its own font over the cell's.
	Runs []Run
	// Style, when not nil, is the cell's own style; nil takes the row's,
	// then the column's, then the document's default.
	Style *Style
	// Href is the hyperlink the cell carries, "" for none (ECMA-376 Part
	// 1, 18.3.1.47; ODF text:a in the cell).
	Href string
	// MarkedInvalid circles the cell in red, as Calc's detective marks a
	// value its validity rule refuses (ODF 1.3 Part 3, 9.2.13
	// table:highlighted-range with table:marked-invalid true).
	MarkedInvalid bool
	// Formula is the cell's formula when Calc computes it as it loads the
	// file, as the file writes it (ECMA-376 Part 1, 18.3.1.40 f, of a cell
	// saved without its result, or of every cell of a workbook Calc
	// recalculates whole; ODF 1.3 Part 3, 19.643 table:formula, of every
	// cell, Calc recalculating an OpenDocument spreadsheet whole), the
	// office.RecalculateAsNeeded rule; the reader keeps every formula
	// under office.RecalculateAlways and none under
	// office.RecalculateNever. The builder computes the arithmetic, the
	// comparisons and SUM, AVERAGE, MIN, MAX and COUNT over the cells and
	// ranges of the book, and keeps Value for what it does not compute.
	Formula string
}

// Run is a span of rich text in a cell (ECMA-376 Part 1, 18.4.4 r; ODF
// text:span).
type Run struct {
	// Text is the span's text.
	Text string
	// Font, when not nil, overrides the cell font for the span.
	Font *Font
}

// Value is a cell value.
type Value struct {
	// Kind says which field holds the value.
	Kind Kind
	// Number holds a Number value, a date or time as its serial number
	// (days since the document's null date, Calc's 1899-12-30 unless the
	// reader converts; ECMA-376 Part 1, 18.17.4.1), a Bool as 0 or 1.
	Number float64
	// Text holds a Text value, an Error value's code ("#DIV/0!"), and the
	// text a cell shows for a Number or Bool when the file stores it.
	Text string
}

// Kind is the kind of a cell value.
type Kind uint8

// The kinds of cell values (ECMA-376 Part 1, 18.18.11 ST_CellType; ODF 1.3
// Part 3, 19.385 office:value-type).
const (
	// Empty is a cell without a value: it may still carry a style.
	Empty Kind = iota
	// Number is a numeric value, including dates, times, percentages and
	// currencies, which their format distinguishes.
	Number
	// Text is a string value.
	Text
	// Bool is a boolean value.
	Bool
	// Error is an error value.
	Error
)

// Range is a rectangle of cells, every index 0-based and inclusive.
type Range struct {
	FirstRow, FirstColumn, LastRow, LastColumn int
}

// Style is a cell style: what a cell draws with, resolved by the reader
// from the file's style tables (ECMA-376 Part 1, 18.8.45 xf; ODF 1.3 Part
// 3, 16.5 table-cell style). A zero field means the document default's.
type Style struct {
	// Font draws the cell's text.
	Font Font
	// Fill paints the cell's background.
	Fill Fill
	// Border is the cell's border on each side, indexed by [css.Side].
	Border [4]Border
	// Diagonal is the border drawn across the cell when DiagonalUp or
	// DiagonalDown is set, or both (ECMA-376 Part 1, 18.8.4; ODF
	// style:diagonal-bl-tr and diagonal-tl-br).
	Diagonal Border
	// DiagonalUp draws Diagonal from the bottom-left to the top-right
	// corner; DiagonalDown from the top-left to the bottom-right.
	DiagonalUp, DiagonalDown bool
	// Align places the text in the cell.
	Align Alignment
	// Padding is the inset between the border and the text on each side,
	// indexed by [css.Side] (ODF fo:padding; for a workbook, the inset Calc
	// gives every imported cell, which the reader sets).
	Padding [4]float64
	// Format is the cell's number format, as a format code in the
	// SpreadsheetML syntax (ECMA-376 Part 1, 18.8.31 numFmt), which Calc
	// reads and writes for its own number formats too; "" and "General"
	// are the General format. An ODF data style is written as the code it
	// means.
	Format string
}

// Font is a cell or run font (ECMA-376 Part 1, 18.8.22; ODF 1.3 Part 3,
// 16.29.x text properties).
type Font struct {
	// Families lists the typefaces in order of preference: the Western,
	// then the Asian and complex script faces an ODF style names, or the
	// one face a workbook font names; empty takes the document default's.
	Families []string
	// Size is the font size in points, 0 for the document default's.
	Size float64
	// Bold and Italic select the face.
	Bold, Italic bool
	// Underline is the underline kind.
	Underline Underline
	// Strike strikes the text through.
	Strike bool
	// Color is the text colour; the zero Color, transparent, draws the
	// document default's, black when none.
	Color css.Color
	// Script raises or lowers the text as a superscript or subscript.
	Script Script
}

// Underline is an underline kind (ECMA-376 Part 1, 18.18.86 ST_UnderlineValues).
type Underline uint8

// The underline kinds. The accounting kinds span the cell width and skip
// descenders as Calc draws them.
const (
	NoUnderline Underline = iota
	Single
	Double
	SingleAccounting
	DoubleAccounting
)

// Script is a vertical text position (ECMA-376 Part 1, 18.18.33
// ST_VerticalAlignRun).
type Script uint8

// The vertical text positions.
const (
	Baseline Script = iota
	Superscript
	Subscript
)

// Fill is a cell background (ECMA-376 Part 1, 18.8.20 fill; ODF
// fo:background-color).
type Fill struct {
	// Color is the colour painted over the whole cell; the zero Color,
	// transparent, paints nothing. A pattern fill is read as the colour
	// its pattern shows most, as Calc imports one.
	Color css.Color
}

// Border is one border line (ECMA-376 Part 1, 18.8.4 border; ODF 1.3 Part
// 3, 20.176 fo:border).
type Border struct {
	// Line is the line style; LineNone draws nothing.
	Line Line
	// Width is the line's total thickness in points, positive when Line is
	// not LineNone: the reader resolves a workbook's named widths (thin,
	// medium, thick, hair) to the points Calc draws them at.
	Width float64
	// Gap is the space between the two lines of a LineDouble border, in
	// points, 0 for a third of Width; the lines share the rest equally
	// (ODF style:border-line-width inner, distance, outer).
	Gap float64
	// Color is the line colour.
	Color css.Color
}

// Line is a border line style (ECMA-376 Part 1, 18.18.3 ST_BorderStyle;
// ODF 1.3 Part 3, 20.176, the CSS border styles with Calc's dash patterns).
type Line uint8

// The border line styles.
const (
	LineNone Line = iota
	LineSolid
	LineDashed
	LineDotted
	LineDashDot
	LineDashDotDot
	LineDouble
)

// Alignment places a cell's text (ECMA-376 Part 1, 18.8.1 alignment; ODF
// 1.3 Part 3, 20.216 fo:text-align, 20.386 style:vertical-align, 20.351
// style:rotation-angle and the related properties).
type Alignment struct {
	// Horizontal is the horizontal alignment.
	Horizontal HAlign
	// Vertical is the vertical alignment.
	Vertical VAlign
	// Wrap wraps the text in the cell's width.
	Wrap bool
	// Shrink shrinks the text to the cell's width instead of overflowing
	// or clipping.
	Shrink bool
	// Indent is the indent from the aligned edge in points, the reader
	// having resolved a workbook's indent levels.
	Indent float64
	// Rotation turns the text counter-clockwise by this many degrees, in
	// [-90, 90]; 0 is horizontal. A workbook's 91 to 180 rotates
	// clockwise and reads as negative.
	Rotation float64
	// Stacked writes the characters one under the other, upright
	// (ECMA-376 Part 1, 18.8.1 textRotation 255; ODF style:direction ttb).
	Stacked bool
	// RightToLeft lays the text out right to left; false lets the text's
	// first strong character decide (ECMA-376 Part 1, 18.8.1 readingOrder
	// 2; ODF style:writing-mode rl-tb on the cell).
	RightToLeft bool
}

// HAlign is a horizontal alignment (ECMA-376 Part 1, 18.18.40
// ST_HorizontalAlignment).
type HAlign uint8

// The horizontal alignments. HGeneral aligns text left and numbers,
// booleans and errors right, as Calc does.
const (
	HGeneral HAlign = iota
	HLeft
	HCenter
	HRight
	// HFill repeats the text to fill the cell.
	HFill
	HJustify
	// HCenterContinuous centres the text across this cell and the empty
	// cells to its right that share the alignment.
	HCenterContinuous
	// HDistributed justifies every line, the last one too.
	HDistributed
)

// VAlign is a vertical alignment (ECMA-376 Part 1, 18.18.88
// ST_VerticalAlignment).
type VAlign uint8

// The vertical alignments. Calc aligns to the bottom by default.
const (
	VBottom VAlign = iota
	VTop
	VMiddle
	VJustify
	VDistributed
)

// PageSetup is a sheet's print setup (ECMA-376 Part 1, 18.3.1.63
// pageSetup, 18.3.1.62 pageMargins, 18.3.1.70 printOptions, 18.3.1.74
// rowBreaks and 18.3.1.14 colBreaks, the defined names _xlnm.Print_Area
// and _xlnm.Print_Titles; ODF 1.3 Part 3, 16.9 page layout, 9.1.2
// table:print-ranges, 9.1.6 table:table-header-columns and 9.1.3
// table:table-header-rows, and fo:break-before on rows and columns). The
// reader fills every field: what the file does not say takes the
// application's default, since the builder knows no format's defaults.
type PageSetup struct {
	// Width and Height are the printed page's size in points, after
	// orientation; both positive.
	Width, Height float64
	// Margin is the distance from each page edge to the cells, indexed by
	// [css.Side]: a workbook's margins; an OpenDocument page's margin,
	// plus its header or footer with the spacing to the body on the sides
	// that have one.
	Margin [4]float64
	// HeaderTop is the distance from the top edge to the header's top, and
	// FooterBottom from the bottom edge to the footer's bottom (ECMA-376
	// Part 1, 18.3.1.62 header and footer; ODF fo:margin-top of the page
	// and fo:margin-bottom). The header grows down from HeaderTop and the
	// footer up from FooterBottom, within Margin.
	HeaderTop, FooterBottom float64
	// Scale is the print scale in percent, 100 the natural size, and 0
	// when FitWidth and FitHeight size the sheet instead (ECMA-376 Part 1,
	// 18.3.1.63 scale and fitToPage; ODF style:scale-to,
	// style:scale-to-pages, style:scale-to-X and style:scale-to-Y).
	Scale int
	// FitWidth and FitHeight, read when Scale is 0, are how many pages
	// across and down the sheet is shrunk to fit, 0 for as many as it
	// takes on that axis.
	FitWidth, FitHeight int
	// Order is the order the pages of a sheet wider and taller than one
	// page are numbered in.
	Order PageOrder
	// FirstPageNumber is the number of the sheet's first page, 0 to
	// continue from the previous sheet, or 1 for the first (ECMA-376 Part
	// 1, 18.3.1.63 firstPageNumber with useFirstPageNumber; ODF
	// style:first-page-number).
	FirstPageNumber int
	// GridLines prints the grid between the cells; Headings the row
	// numbers and column letters (ECMA-376 Part 1, 18.3.1.70 gridLines
	// and headings; ODF style:print "grid" and "headers").
	GridLines, Headings bool
	// CenterHorizontally and CenterVertically centre the cells of a page in
	// its area (ECMA-376 Part 1, 18.3.1.70 horizontalCentered and
	// verticalCentered; ODF style:table-centering).
	CenterHorizontally, CenterVertically bool
	// PrintAreas are the ranges printed, in order, each starting on a new
	// page; empty prints the used range: the cells with a value, a fill
	// or a border, and the drawings.
	PrintAreas []Range
	// RepeatRows and RepeatColumns, when not nil, are the rows repeated at
	// the top and the columns repeated at the left of every page of the
	// sheet (print titles); their row or column indexes alone are read.
	RepeatRows, RepeatColumns *Range
	// RowBreaks and ColumnBreaks are the 0-based indexes of the rows and
	// columns a manual page break starts, rising.
	RowBreaks, ColumnBreaks []int
}

// PageOrder is the order the pages of one sheet are numbered in
// (ECMA-376 Part 1, 18.18.51 ST_PageOrder; ODF style:print-page-order).
type PageOrder uint8

// The page orders.
const (
	// DownThenOver numbers the pages down the first band of columns,
	// then the next band: Calc's default.
	DownThenOver PageOrder = iota
	// OverThenDown numbers the pages across the first band of rows, then
	// the next band.
	OverThenDown
)

// HeaderFooter is what a sheet's pages show in their top and bottom
// margins (ECMA-376 Part 1, 18.3.1.46 headerFooter; ODF 1.3 Part 3, 16.11
// style:header and style:footer with their regions). A zero value shows
// nothing.
type HeaderFooter struct {
	// Odd is the header and footer of every page, unless Even or First
	// applies.
	Odd Margins
	// Even applies to even pages when DifferentOddEven is set.
	Even Margins
	// First applies to the first page when DifferentFirst is set.
	First Margins
	// DifferentOddEven and DifferentFirst enable Even and First.
	DifferentOddEven, DifferentFirst bool
	// HeaderMinHeight and FooterMinHeight are the least height of the
	// header and the footer in points, 0 for the content's own (ODF
	// fo:min-height); the margins the reader set already make room.
	HeaderMinHeight, FooterMinHeight float64
	// GrowHeader and GrowFooter make the header's and the footer's
	// heights follow their content, as Calc's dynamic heights do (ODF
	// fo:min-height; a workbook header whose font leaves room in its
	// margin, LibreOffice's HeaderIsDynamic): the tallest variant's
	// content plus HeaderSpacing, when taller than the room the margin
	// leaves (Margin less HeaderTop), moves the cells down; a footer
	// moves them up alike. HeaderSpacing and FooterSpacing are the space
	// between the header and the cells and between the cells and the
	// footer (ODF fo:margin-bottom of the header, fo:margin-top of the
	// footer; LibreOffice's HeaderBodyDistance).
	GrowHeader, GrowFooter       bool
	HeaderSpacing, FooterSpacing float64
	// ScaleWithDoc is a workbook's scaleWithDoc flag (ECMA-376 Part 1,
	// 18.3.1.46), kept for the record: Calc scales the header and footer
	// text with the sheet's print scale whatever it says, and so does the
	// builder.
	ScaleWithDoc bool
}

// Margins is one header and one footer, each three parts.
type Margins struct {
	// Header and Footer are the left, centre and right parts, in that
	// order.
	Header, Footer [3][]Span
}

// Span is a piece of a header or footer part: text, or a field resolved
// when the page is drawn (ECMA-376 Part 1, 18.3.1.46, the & codes; ODF
// 1.3 Part 3, 7.3 and 7.5, the text fields).
type Span struct {
	// Text is the span's text when Field is FieldNone.
	Text string
	// Field, when not FieldNone, is what the span shows.
	Field Field
	// Font, when not nil, overrides the header font (Calc's default header
	// style) for the span.
	Font *Font
}

// Field is a header or footer field.
type Field uint8

// The header and footer fields. FieldDate and FieldTime show
// office.Options.PrintDate, nothing when it is the zero time.
const (
	FieldNone Field = iota
	// FieldPage is the page number.
	FieldPage
	// FieldPages is the document's page count.
	FieldPages
	// FieldSheet is the sheet name.
	FieldSheet
	// FieldFile is the file name, which a converter does not know: it
	// shows nothing.
	FieldFile
	// FieldPath is the file path, shown as FieldFile.
	FieldPath
	// FieldTitle is the document title.
	FieldTitle
	// FieldDate is the print date.
	FieldDate
	// FieldTime is the print time.
	FieldTime
)

// Drawing is a picture or a chart placed on a sheet (ECMA-376 Part 1,
// 20.5.2 the spreadsheet drawing anchors; ODF 1.3 Part 3, 10.4 draw:frame
// in a table:shapes element or a cell). Exactly one of Picture and Chart
// is set.
type Drawing struct {
	// From is the cell the drawing's top-left corner sits in, with the
	// offset from that cell's corner. When To is set it places the
	// bottom-right corner alike (twoCellAnchor); else Width and Height
	// size the drawing (oneCellAnchor); with Absolute set, From is ignored
	// and X and Y place the corner from the sheet's origin
	// (absoluteAnchor, and ODF shapes outside a cell).
	From Anchor
	// To, when not nil, is the bottom-right corner's anchor.
	To *Anchor
	// Width and Height size the drawing in points when To is nil.
	Width, Height float64
	// Absolute places the drawing by X and Y.
	Absolute bool
	// X and Y are the top-left corner from the sheet's origin, in points,
	// when Absolute is set.
	X, Y float64
	// Picture is a raster or vector picture.
	Picture *layout.Picture
	// Chart is a chart the chart package draws.
	Chart *chart.Chart
	// Name is the drawing's name, for warnings.
	Name string
	// Alt describes the drawing for a reader who cannot see it, as the
	// source states it (ECMA-376 Part 1, 20.5.2.8 cNvPr descr, else title;
	// ODF 1.3 Part 3, 10.3.17 svg:desc, else 10.3.16 svg:title), "" when it
	// states nothing. A picture or a chart is a Figure that carries it; a
	// chart without one takes its title. A page whose only content is
	// drawings then has an element an outline entry or a link can name.
	Alt string
	// Decorative marks a drawing the source says carries no meaning (the
	// decorative flag of Office 2019, adec:decorative): it is drawn as an
	// artifact, outside the structure tree.
	Decorative bool
	// Shape marks a drawing of shapes alone, geometry, lines and text
	// boxes with no picture or chart among them (ECMA-376 Part 1, 20.5.2.29
	// sp, 20.5.2.13 cxnSp, 20.5.2.17 grpSp; ODF 1.3 Part 3, 10.3 drawing
	// shapes). One without an Alt is drawn as an artifact, as a slide's
	// and a text document's undescribed shapes are; one with an Alt is a
	// Figure that carries it.
	Shape bool
	// Unbounded lets the offsets of From and To run past their cells, and
	// below 0 to the cells above and left, as Calc reads the anchors of an
	// OpenDocument spreadsheet (ODF 1.3 Part 3, 19.663 table:end-x and
	// table:end-y); otherwise an offset stops at its cell's edges, as Calc
	// reads a workbook's anchors (ECMA-376 Part 1, 20.5.2.19 colOff and
	// rowOff).
	Unbounded bool
}

// Anchor is a cell with an offset from its top-left corner.
type Anchor struct {
	// Row and Column are the cell's 0-based indexes.
	Row, Column int
	// DX and DY are the offset into the cell in points, at least 0 unless
	// the drawing is Unbounded.
	DX, DY float64
}

// Pivot is a pivot table: a summary of a source range of cells by the
// distinct values of some of its columns (ECMA-376 Part 1, 18.10.1.73
// pivotTableDefinition with its cache, 18.10.1.67 pivotCacheDefinition;
// ODF 1.3 Part 3, 9.6.2 table:data-pilot-table). The reader resolves the
// file's fields to the source's columns and Calc's settings; the builder
// computes the output from the source cells and writes it at Row and
// Column after emptying Clear, laid out, captioned, framed and styled as
// Calc's DataPilot output is.
type Pivot struct {
	// Name is the pivot table's name, for warnings.
	Name string
	// Row and Column are the output's top-left cell, page fields
	// included.
	Row, Column int
	// Clear is the area the file saved the output in, emptied of values
	// and styles before the output is written; nil for none.
	Clear *Range
	// SourceSheet is the index in Document.Sheets of the sheet the source
	// range is on, Source the range: its first row names the fields, every
	// other row is a record.
	SourceSheet int
	Source      Range
	// Rows, Columns and Pages are the row, column and page fields in
	// order; Data the data fields. A Rows or Columns entry whose Field is
	// PivotDataLayout places the data fields' names on that axis.
	Rows, Columns, Pages []PivotField
	Data                 []PivotData
	// RowGrand adds a grand total row below the rows when there are row
	// fields, ColumnGrand a grand total column right of the columns when
	// there are column fields (Calc's RowGrand and ColumnGrand).
	RowGrand, ColumnGrand bool
	// HideHeader leaves out the header row above the column fields.
	HideHeader bool
	// FilterButton adds a row with a filter button above the page fields.
	FilterButton bool
	// ExpandCollapse indents the members of the row fields that have
	// children by one step, the room of Calc's expand and collapse
	// buttons.
	ExpandCollapse bool
}

// PivotDataLayout is the Field of the data layout field, the field whose
// members are the data fields.
const PivotDataLayout = -1

// PivotField is a row, column or page field of a pivot table (ECMA-376
// Part 1, 18.10.1.69 pivotField; ODF 1.3 Part 3, 9.6.7
// table:data-pilot-field).
type PivotField struct {
	// Field is the 0-based column of the source range, or
	// PivotDataLayout.
	Field int
	// Name, when not "", names the source column by its label instead of
	// Field, as Calc finds a workbook's cache field in its source: the
	// labels made unique as Calc's cache makes them ("Data" taken first,
	// a repeated label numbered from 2, case ignored), a name no label
	// matches leaving the field out.
	Name string
	// Caption is the field's caption, "" for its name.
	Caption string
	// Layout is how the field's members and subtotals are laid out.
	Layout PivotLayout
	// Subtotals are the subtotal functions of the field's members, in
	// order; PivotAuto alone uses each data field's own function, nil
	// shows no subtotal.
	Subtotals []PivotFunction
	// AddEmpty adds an empty line after each member.
	AddEmpty bool
	// ShowEmpty shows the members without data too.
	ShowEmpty bool
	// Sort orders the members; Order lists them for PivotManual, the
	// members it leaves out following in ascending order.
	Sort  PivotSort
	Order []Value
	// Hidden are the members not shown, whose records are left out.
	Hidden []Value
	// Selected, for a page field, is the member whose records alone are
	// summarised; nil for all.
	Selected *Value
}

// PivotData is a data field: a source column summarised by a function
// (ECMA-376 Part 1, 18.10.1.30 dataField; ODF table:data-pilot-field with
// table:orientation data).
type PivotData struct {
	// Field is the 0-based column of the source range; Name, when not
	// "", names it as PivotField.Name does.
	Field int
	Name  string
	// Function summarises the field's values.
	Function PivotFunction
	// Caption is the field's caption, "" for Calc's "Sum - name".
	Caption string
}

// PivotLayout is the layout of a row or column field's members.
type PivotLayout uint8

// The pivot field layouts (com.sun.star.sheet.DataPilotFieldLayoutMode).
const (
	// PivotTabular starts a member's first child on its own row.
	PivotTabular PivotLayout = iota
	// PivotOutlineBottom gives each member a title row and its subtotals
	// rows below its children.
	PivotOutlineBottom
	// PivotOutlineTop gives each member a title row showing its subtotal.
	PivotOutlineTop
	// PivotCompact is PivotOutlineTop with the next field's members in the
	// same column, indented.
	PivotCompact
)

// PivotFunction is a summary function (ECMA-376 Part 1, 18.18.16
// ST_DataConsolidateFunction; ODF table:function).
type PivotFunction uint8

// The summary functions.
const (
	// PivotAuto is a subtotal in each data field's own function.
	PivotAuto PivotFunction = iota
	PivotSum
	// PivotCount counts the non-empty values, PivotCountNums the numbers.
	PivotCount
	PivotAverage
	PivotMax
	PivotMin
	PivotProduct
	PivotCountNums
	PivotStdDev
	PivotStdDevP
	PivotVar
	PivotVarP
	PivotMedian
)

// PivotSort is the order of a field's members.
type PivotSort uint8

// The member orders.
const (
	// PivotManual lists the members in Order, then the rest ascending.
	PivotManual PivotSort = iota
	PivotAscending
	PivotDescending
)
