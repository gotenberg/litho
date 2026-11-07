package css

// WritingMode is the writing-mode property (CSS Writing Modes 4, 3.1): the
// direction lines stack in and the direction text runs along them. It is
// inherited.
//
// The layout engine lays a box of a vertical mode out as an orthogonal
// flow (CSS Writing Modes 4, 7.3) whose text it sets sideways: the lines
// of a horizontal layout turned a quarter turn, as Word and Writer draw a
// table cell or a frame of vertical text (ECMA-376 Part 1, 17.4.72
// textDirection; ODF 1.3 Part 3, 20.394 style:writing-mode).
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
