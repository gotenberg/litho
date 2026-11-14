package css

// WritingMode is the writing-mode property (CSS Writing Modes 4, 3.1): the
// direction lines stack in and the direction text runs along them. It is
// inherited.
//
// The layout engine lays a box of a vertical mode out as an orthogonal
// flow (CSS Writing Modes 4, 7.3): the lines of a horizontal layout turned
// a quarter turn, which sets the text sideways, as Word and Writer draw a
// table cell or a frame of vertical text (ECMA-376 Part 1, 17.4.72
// textDirection; ODF 1.3 Part 3, 20.394 style:writing-mode). Under the
// browser rules, [TextOrientation] turns characters of vertical-rl and
// vertical-lr back upright.
type WritingMode uint8

// The writing-mode values.
const (
	// HorizontalTB stacks horizontal lines top to bottom, the initial value.
	HorizontalTB WritingMode = iota
	// VerticalRL stacks vertical lines right to left, text top to bottom.
	VerticalRL
	// VerticalLR stacks vertical lines left to right, text top to bottom.
	VerticalLR
	// SidewaysRL sets the text turned a quarter turn clockwise, lines
	// stacking right to left: WordprocessingML tbRl.
	SidewaysRL
	// SidewaysLR sets the text turned a quarter turn counter-clockwise,
	// reading bottom to top, lines stacking left to right:
	// WordprocessingML btLr, ODF bt-lr.
	SidewaysLR
)

// Vertical reports a vertical writing mode, whose lines are vertical.
func (m WritingMode) Vertical() bool { return m != HorizontalTB }

// TextOrientation is the text-orientation property (CSS Writing Modes 4,
// 5.1): how the characters of a vertical line stand. It is inherited and
// applies to text in vertical-rl and vertical-lr alone: the sideways
// writing modes and the horizontal one set every character sideways or
// upright as a horizontal line does.
type TextOrientation uint8

// The text-orientation values.
const (
	// OrientMixed, the initial value, sets the characters whose Unicode
	// Vertical_Orientation is U, Tu or Tr upright and the others
	// sideways (UAX #50, 5).
	OrientMixed TextOrientation = iota
	// OrientUpright sets every character upright, and the line's bidi
	// direction left to right.
	OrientUpright
	// OrientSideways sets every character sideways, as a horizontal line
	// turned a quarter turn clockwise.
	OrientSideways
)

// TextCombine is the text-combine-upright property (CSS Writing Modes 4,
// 9.1): text set as one upright character in a vertical line, a
// tate-chu-yoko. It is inherited.
type TextCombine uint8

// The text-combine-upright values.
const (
	// CombineNone, the initial value, combines nothing.
	CombineNone TextCombine = iota
	// CombineAll sets the text of each inline box of the element, in a
	// vertical line, as one character one em high.
	CombineAll
)
