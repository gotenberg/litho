package css

// The fields of [Style] that no CSS property sets, which the Office
// converters fill from their document models and the layout engine
// honours: [Style.TabStops], [Style.MinLineHeight] and [Style.TextScale].

// TabStop is one tab stop of a block container (ECMA-376 Part 1,
// 17.3.1.37; ODF 1.3 Part 3, 17.8): a tab character in a line of the
// container advances to the next stop past the line's current position.
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
	// TabBar draws a vertical rule at the stop across the line and leaves
	// the text where it is: the tab advances nothing.
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
