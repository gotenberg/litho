package css

// The fields of [Style] that no CSS property sets, which the Office
// converters fill from their document models and the layout engine
// honours: [Style.TabStops], [Style.MinLineHeight] and [Style.TextScale].

// TabStop is one tab stop of a block container (ECMA-376 Part 1,
// 17.3.1.37; ODF 1.3 Part 3, 17.8): a tab character in a line of the
// container advances to the next stop past the line's current position.
//
// A container whose [Style.TabStops] is not nil lays every tab out as
// LibreOffice does for WordprocessingML and ODF:
//
//   - The tab goes to the first stop, bar stops aside, whose Pos lies
//     past the tab's position, measured from the start edge of the
//     container's content box (its end edge in a right-to-left
//     container), however close; past the last stop, to the next
//     multiple of [Style.TabSize] past both, a left stop.
//   - The text after the tab, up to the next tab or the paragraph's end,
//     is aligned on the stop as Align says. Text too wide to align that
//     way starts where the tab started: the tab takes no room.
//   - A stop past the line's end makes the tab fill the rest of the line;
//     what follows goes to the next line.
//   - When the first word after the tab does not fit on the line beside
//     it, the line breaks before the tab, which advances again on the
//     next line, unless it starts the line.
//   - A justified line stretches only its text after its last tab.
//
// The tab characters must survive white space processing: the container
// sets a [WhiteSpace] that preserves them, pre-wrap as the Office formats
// keep every space.
type TabStop struct {
	// Pos is the stop's position in points from the start edge of the
	// containing block's content box, the edge the paragraph's indent is
	// measured from. It is finite and at least 0.
	Pos float64
	// Align says what the text after the tab, up to the next tab or the
	// end of the line, aligns on the stop.
	Align TabAlign
	// Leader fills the room the tab leaves, from the tab's start to the
	// stop, with the leader's character repeated.
	Leader TabLeader
}

// TabAlign is the alignment of a [TabStop].
type TabAlign uint8

// The tab stop alignments.
const (
	// TabLeft starts the text at the stop.
	TabLeft TabAlign = iota
	// TabCenter centres the text on the stop.
	TabCenter
	// TabRight ends the text at the stop.
	TabRight
	// TabDecimal puts the first decimal separator of the text, else its
	// end, at the stop.
	TabDecimal
	// TabBar draws a vertical rule at the stop across every line box of
	// the container, in the container's colour; no tab advances to it.
	TabBar
)

// TabLeader is the fill of a [TabStop].
type TabLeader uint8

// The tab leaders.
const (
	// TabLeaderNone leaves the room blank.
	TabLeaderNone TabLeader = iota
	// TabLeaderDots fills it with full stops.
	TabLeaderDots
	// TabLeaderHyphens fills it with hyphens.
	TabLeaderHyphens
	// TabLeaderUnderscore fills it with low lines.
	TabLeaderUnderscore
	// TabLeaderMiddleDots fills it with middle dots.
	TabLeaderMiddleDots
)

// LineRule selects how the line height of a block container sizes its
// line boxes and places their baseline. The Office formats space lines
// differently from CSS: [LineRuleCSS] is CSS's rule, the others are
// Writer's, which LibreOffice applies to WordprocessingML and ODF alike
// (ECMA-376 Part 1, 17.3.1.33 spacing lineRule; ODF 1.3 Part 3, 20.315
// fo:line-height). The rule is the block container's; the inline boxes of
// its lines share one baseline under an Office rule, whatever their own
// line-height.
type LineRule uint8

// The line rules.
const (
	// LineRuleCSS sizes each inline box by its line-height with the
	// leading split above and below its glyphs (CSS 2.1, 10.8.1).
	LineRuleCSS LineRule = iota
	// LineRuleExact makes every line box exactly LineHeight tall, a
	// length (WordprocessingML lineRule exact, ODF fo:line-height as a
	// length): the baseline sits four fifths of the height below the line
	// top, whatever the glyphs, which overflow a line too short for them.
	LineRuleExact
	// LineRuleMultiple makes every line box LineHeight, a number, times
	// its natural height (WordprocessingML lineRule auto, ODF
	// fo:line-height as a percentage). The natural height is the tallest
	// ascent plus line gap over the line's glyph runs and atomic inlines
	// above the shared baseline, plus the deepest descent below it. At 1
	// and above the baseline keeps its natural place and the room added
	// goes below the glyphs; below 1 the baseline sits four fifths of the
	// shrunk height below the line top.
	LineRuleMultiple
)

// DecorationCloneAll gives each fragment of a box its own borders,
// padding, radii, shadows and background at every break, page breaks
// included, as Writer draws a bordered paragraph across a page break: the
// lines leave room for the bottom edges before each cut and start below
// the top edges after it. [DecorationClone] clones at column breaks alone,
// as Chromium prints. No CSS value sets it.
const DecorationCloneAll = DecorationClone + 1
