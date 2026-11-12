package css

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// This file parses the properties of grid layout (CSS Grid 1, 7 and 8)
// and the justify-items and justify-self alignment properties that grid
// items take (CSS Box Alignment 3, 6). Subgrid (CSS Grid 2) and masonry
// are not parsed: subgrid makes a template declaration invalid.

// TrackKind says what a [TrackBreadth] holds.
type TrackKind uint8

// The kinds of [TrackBreadth] (CSS Grid 1, 7.2.1).
const (
	// TrackAuto is the auto keyword.
	TrackAuto TrackKind = iota
	// TrackLength is a length, Value in points.
	TrackLength
	// TrackPercent is a percentage of the grid container's content box
	// size in the track's axis, Value the fraction (0.5 for 50%).
	TrackPercent
	// TrackFlex is a flexible length, Value in fr (CSS Grid 1, 7.2.4).
	TrackFlex
	// TrackMinContent and TrackMaxContent are the min-content and
	// max-content keywords.
	TrackMinContent
	TrackMaxContent
)

// TrackBreadth is a track sizing function (CSS Grid 1, 7.2.1).
type TrackBreadth struct {
	Kind  TrackKind
	Value float64
}

// TrackSize is a <track-size> (CSS Grid 1, 7.2.1): its minimum and maximum
// track sizing functions, both Min and Max for a single breadth, Min auto
// for a flexible one (a lone 1fr is minmax(auto, 1fr)). FitContent reports
// fit-content(Max), Max a length or percentage: minmax(auto, max-content)
// clamped at Max (CSS Grid 1, 7.2.4).
type TrackSize struct {
	Min, Max   TrackBreadth
	FitContent bool
}

// TrackList is a grid-template-rows or grid-template-columns value (CSS
// Grid 1, 7.2): the explicit tracks and the line names around them, the
// zero value being none. Names holds one entry per line, len(Sizes)+1, the
// names a <line-names> gives it in source order. Auto is the auto-repeated
// part of an <auto-track-list>, nil for none: it then sits before
// Sizes[Auto.At], and Names holds one more entry, Names[Auto.At] being the
// names written before the repeat() and Names[Auto.At+1] those after it.
// Integer repeat() is expanded (7.2.3.1).
type TrackList struct {
	Names [][]string
	Sizes []TrackSize
	Auto  *TrackRepeat
}

// TrackRepeat is the auto-repeated part of a track list (CSS Grid 1,
// 7.2.3.2): its tracks, the names inside the repeat(), len(Sizes)+1 of
// them, and where it sits.
type TrackRepeat struct {
	// Fit reports auto-fit, which collapses the empty repeated tracks;
	// else auto-fill.
	Fit   bool
	At    int
	Names [][]string
	Sizes []TrackSize
}

// Expand returns the line names and the track sizes of the list with the
// auto-repeated tracks repeated n times (CSS Grid 1, 7.2.3.2): adjacent
// line names combine, the names of a repeat() boundary joining those
// before and after it. auto reports the index of the first repeated track,
// -1 when there is no auto repeat.
func (l TrackList) Expand(n int) (names [][]string, sizes []TrackSize, auto int) {
	if l.Auto == nil {
		names = make([][]string, len(l.Sizes)+1)
		for i := range names {
			if i < len(l.Names) {
				names[i] = l.Names[i]
			}
		}
		return names, slices.Clone(l.Sizes), -1
	}
	r := l.Auto
	at := min(max(r.At, 0), len(l.Sizes))
	name := func(i int) []string {
		if i >= 0 && i < len(l.Names) {
			return l.Names[i]
		}
		return nil
	}
	rname := func(i int) []string {
		if i >= 0 && i < len(r.Names) {
			return r.Names[i]
		}
		return nil
	}
	join := func(a, b []string) []string {
		if len(a) == 0 {
			return b
		}
		if len(b) == 0 {
			return a
		}
		return append(slices.Clip(a), b...)
	}
	for i := range at {
		names = append(names, name(i))
		sizes = append(sizes, l.Sizes[i])
	}
	auto = len(sizes)
	if n <= 0 {
		names = append(names, join(name(at), name(at+1)))
		auto = -1
	} else {
		cur := name(at)
		for range n {
			names = append(names, join(cur, rname(0)))
			for j, s := range r.Sizes {
				sizes = append(sizes, s)
				if j < len(r.Sizes)-1 {
					names = append(names, rname(j+1))
				}
			}
			cur = rname(len(r.Sizes))
		}
		names = append(names, join(cur, name(at+1)))
	}
	for i := at; i < len(l.Sizes); i++ {
		sizes = append(sizes, l.Sizes[i])
		names = append(names, name(i+2))
	}
	return names, sizes, auto
}

// GridAreas is a grid-template-areas value (CSS Grid 1, 7.3): the
// implicit grid it defines, Rows by Cols, and its named areas sorted by
// name.
type GridAreas struct {
	Rows, Cols int
	Areas      []GridArea
}

// GridArea is a named grid area: the 0-based lines it spans in each axis,
// end lines exclusive.
type GridArea struct {
	Name                   string
	Row0, Row1, Col0, Col1 int
}

// Area returns the area called name, false for none.
func (g *GridAreas) Area(name string) (GridArea, bool) {
	if g == nil {
		return GridArea{}, false
	}
	i, ok := slices.BinarySearchFunc(g.Areas, name, func(a GridArea, n string) int { return strings.Compare(a.Name, n) })
	if !ok {
		return GridArea{}, false
	}
	return g.Areas[i], true
}

// GridLine is a grid-placement property's value (CSS Grid 1, 8.3): auto
// as the zero value; an integer line N, counting only the lines called
// Name when Name is set; a lone Name (N 0); or a span of N lines, or of N
// lines called Name, with Span.
type GridLine struct {
	Span bool
	N    int
	Name string
}

// IsAuto reports the auto keyword.
func (g GridLine) IsAuto() bool { return !g.Span && g.N == 0 && g.Name == "" }

// GridAutoFlow is grid-auto-flow (CSS Grid 1, 7.7): row, the zero value,
// or column, with the dense bit.
type GridAutoFlow uint8

// The grid-auto-flow bits.
const (
	// GridFlowColumn places items column by column.
	GridFlowColumn GridAutoFlow = 1 << iota
	// GridFlowDense fills holes left earlier in the grid.
	GridFlowDense
)

// MaxGridTracks bounds the explicit tracks a track list expands to in one
// axis, integer repeats included, and the tracks the layout creates.
// Anti-DoS guard, not spec-mandated: repeat(1000000, 1px) would otherwise
// allocate a million tracks per element; real templates use tens.
const MaxGridTracks = 1000

// MaxGridLine bounds the magnitude of a line number in a grid-placement
// property, as Chromium clamps it. Anti-DoS guard, not spec-mandated: the
// implicit grid would otherwise reach any size an integer names (CSS
// Grid 1, 8.3.1 lets implementations clamp).
const MaxGridLine = 1000

// isGridContainer reports a grid container's display value.
func isGridContainer(d Display) bool { return d == Grid || d == InlineGrid }

// breadthSpec is a specified track breadth.
type breadthSpec struct {
	kind TrackKind
	l    length
	fr   float64
}

// trackSpec is a specified track size.
type trackSpec struct {
	min, max breadthSpec
	fit      bool
}

// trackListSpec is a specified track list, its lengths not yet computed.
type trackListSpec struct {
	names [][]string
	sizes []trackSpec
	auto  *repeatSpec
}

// repeatSpec is a specified auto repeat.
type repeatSpec struct {
	fit   bool
	at    int
	names [][]string
	sizes []trackSpec
}

// parseBreadth parses a <track-breadth> (CSS Grid 1, 7.2.1): a
// non-negative length or percentage, a non-negative flexible length when
// flex, min-content, max-content or auto.
func parseBreadth(t token, flex bool) (breadthSpec, bool) {
	if t.typ == tIdent {
		switch lower(t.value) {
		case "auto":
			return breadthSpec{kind: TrackAuto}, true
		case "min-content":
			return breadthSpec{kind: TrackMinContent}, true
		case "max-content":
			return breadthSpec{kind: TrackMaxContent}, true
		}
		return breadthSpec{}, false
	}
	if t.typ == tDimension && eqFold(t.value, "fr") {
		if !flex || !finite(t.num) || t.num < 0 {
			return breadthSpec{}, false
		}
		return breadthSpec{kind: TrackFlex, fr: min(t.num, maxFlexFactor)}, true
	}
	l, ok := parseLength(t, allowPct|nonNeg)
	if !ok {
		return breadthSpec{}, false
	}
	if l.hasPct && !l.hasLen {
		return breadthSpec{kind: TrackPercent, l: l}, true
	}
	return breadthSpec{kind: TrackLength, l: l}, true
}

// parseTrackSize parses a <track-size> (CSS Grid 1, 7.2.1).
func parseTrackSize(t token) (trackSpec, bool) {
	if t.typ == tFunction {
		args := splitCommas(significant(t.children))
		switch lower(t.value) {
		case "minmax":
			if len(args) != 2 || len(args[0]) != 1 || len(args[1]) != 1 {
				return trackSpec{}, false
			}
			lo, ok := parseBreadth(args[0][0], false)
			if !ok {
				return trackSpec{}, false
			}
			hi, ok := parseBreadth(args[1][0], true)
			if !ok {
				return trackSpec{}, false
			}
			return trackSpec{min: lo, max: hi}, true
		case "fit-content":
			if len(args) != 1 || len(args[0]) != 1 {
				return trackSpec{}, false
			}
			l, ok := parseLength(args[0][0], allowPct|nonNeg)
			if !ok {
				return trackSpec{}, false
			}
			b := breadthSpec{kind: TrackLength, l: l}
			if l.hasPct && !l.hasLen {
				b.kind = TrackPercent
			}
			return trackSpec{min: breadthSpec{kind: TrackAuto}, max: b, fit: true}, true
		}
	}
	b, ok := parseBreadth(t, true)
	if !ok {
		return trackSpec{}, false
	}
	if b.kind == TrackFlex {
		return trackSpec{min: breadthSpec{kind: TrackAuto}, max: b}, true
	}
	return trackSpec{min: b, max: b}, true
}

// fixed reports a <fixed-size> (CSS Grid 1, 7.2.3): a fixed breadth (a
// length or percentage), a minmax() with a fixed minimum, or one with a
// fixed maximum and an inflexible minimum.
func (s trackSpec) fixed() bool {
	fixedB := func(b breadthSpec) bool { return b.kind == TrackLength || b.kind == TrackPercent }
	if s.fit {
		return false
	}
	return fixedB(s.min) || fixedB(s.max) && s.min.kind != TrackFlex
}

// parseLineNames parses a <line-names> block (CSS Grid 1, 7.2): custom
// identifiers but span and auto.
func parseLineNames(t token) ([]string, bool) {
	var out []string
	for _, c := range t.children {
		switch c.typ { //nolint:exhaustive // anything else is invalid
		case tWhitespace:
		case tIdent:
			if !customIdent(c.value) || eqFold(c.value, "span") || eqFold(c.value, "auto") {
				return nil, false
			}
			out = append(out, c.value)
		default:
			return nil, false
		}
	}
	return out, true
}

// customIdent reports an ident valid as a <custom-ident>: no CSS-wide
// keyword nor default (CSS Values 4, 4.2).
func customIdent(s string) bool {
	switch lower(s) {
	case "initial", "inherit", "unset", "default", "revert", "revert-layer":
		return false
	}
	return s != ""
}

// trackListParser accumulates a track list.
type trackListParser struct {
	out     trackListSpec
	pending []string
	// last reports that the last component was a track, so a
	// <line-names> may follow.
	names bool
}

// addNames records a <line-names> block before the next track.
func (p *trackListParser) addNames(n []string) bool {
	if p.names {
		return false
	}
	p.pending = append(p.pending, n...)
	p.names = true
	return true
}

// addTrack records a track after the pending names, and drops it past
// [MaxGridTracks].
func (p *trackListParser) addTrack(s trackSpec) {
	if len(p.out.sizes)+repeatLen(p.out.auto) >= MaxGridTracks {
		return
	}
	p.out.names = append(p.out.names, p.pending)
	p.out.sizes = append(p.out.sizes, s)
	p.pending, p.names = nil, false
}

func repeatLen(r *repeatSpec) int {
	if r == nil {
		return 0
	}
	return len(r.sizes)
}

// parseTrackList parses grid-template-rows or grid-template-columns (CSS
// Grid 1, 7.2): none, a <track-list> or an <auto-track-list>. explicit
// rejects repeat() with auto-fill and auto-fit, for the
// <explicit-track-list> of grid-template.
func parseTrackList(v []token, explicit bool) (trackListSpec, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return trackListSpec{}, true
	}
	if len(v) == 0 {
		return trackListSpec{}, false
	}
	var p trackListParser
	for _, t := range v {
		switch {
		case t.typ == tLBracket:
			n, ok := parseLineNames(t)
			if !ok || !p.addNames(n) {
				return trackListSpec{}, false
			}
		case t.typ == tFunction && eqFold(t.value, "repeat"):
			if !p.repeat(t, explicit) {
				return trackListSpec{}, false
			}
		default:
			s, ok := parseTrackSize(t)
			if !ok {
				return trackListSpec{}, false
			}
			p.addTrack(s)
		}
	}
	if len(p.out.sizes) == 0 && p.out.auto == nil {
		return trackListSpec{}, false
	}
	p.out.names = append(p.out.names, p.pending)
	if p.out.auto != nil {
		// An <auto-track-list> holds <fixed-size> tracks alone (7.2.3).
		for _, s := range p.out.sizes {
			if !s.fixed() {
				return trackListSpec{}, false
			}
		}
	}
	return p.out, true
}

// repeat parses a repeat() into the list (CSS Grid 1, 7.2.3): an integer
// count expands at once, auto-fill and auto-fit once per list.
func (p *trackListParser) repeat(t token, explicit bool) bool {
	args := splitCommas(significant(t.children))
	if len(args) != 2 || len(args[0]) != 1 || len(args[1]) == 0 {
		return false
	}
	var inner trackListParser
	for _, c := range args[1] {
		switch c.typ { //nolint:exhaustive // a track size otherwise
		case tLBracket:
			n, ok := parseLineNames(c)
			if !ok || !inner.addNames(n) {
				return false
			}
		default:
			s, ok := parseTrackSize(c)
			if !ok {
				return false
			}
			inner.out.names = append(inner.out.names, inner.pending)
			inner.out.sizes = append(inner.out.sizes, s)
			inner.pending, inner.names = nil, false
			if len(inner.out.sizes) > MaxGridTracks {
				return false
			}
		}
	}
	if len(inner.out.sizes) == 0 {
		return false
	}
	inner.out.names = append(inner.out.names, inner.pending)
	c := args[0][0]
	if c.typ == tIdent {
		k := lower(c.value)
		if explicit || p.out.auto != nil || k != "auto-fill" && k != "auto-fit" {
			return false
		}
		for _, s := range inner.out.sizes {
			if !s.fixed() {
				return false
			}
		}
		// The names before the repeat stay on their own line entry.
		p.out.names = append(p.out.names, p.pending)
		p.pending, p.names = nil, false
		p.out.auto = &repeatSpec{fit: k == "auto-fit", at: len(p.out.sizes), names: inner.out.names, sizes: inner.out.sizes}
		return true
	}
	n, ok := parseInteger(c)
	if !ok || n < 1 {
		return false
	}
	n = min(n, MaxGridTracks)
	k := len(inner.out.sizes)
	for range n {
		if len(p.out.sizes)+repeatLen(p.out.auto)+k > MaxGridTracks {
			break
		}
		for j, s := range inner.out.sizes {
			names := inner.out.names[j]
			if j == 0 {
				names = append(slices.Clip(p.pending), names...)
			}
			p.out.names = append(p.out.names, names)
			p.out.sizes = append(p.out.sizes, s)
			p.pending = nil
		}
		p.pending = slices.Clone(inner.out.names[k])
	}
	// A <line-names> may follow a repeat(), joining its trailing names.
	p.names = false
	return true
}

// computeBreadth computes a specified breadth.
func (s *styler) computeBreadth(b breadthSpec) TrackBreadth {
	switch b.kind { //nolint:exhaustive // the keywords have no value
	case TrackLength:
		return TrackBreadth{Kind: TrackLength, Value: max(b.l.points(s.lc), 0)}
	case TrackPercent:
		return TrackBreadth{Kind: TrackPercent, Value: max(clampLen(b.l.pct/100), 0)}
	case TrackFlex:
		return TrackBreadth{Kind: TrackFlex, Value: b.fr}
	}
	return TrackBreadth{Kind: b.kind}
}

func (s *styler) computeTrack(t trackSpec) TrackSize {
	return TrackSize{Min: s.computeBreadth(t.min), Max: s.computeBreadth(t.max), FitContent: t.fit}
}

func (s *styler) computeTracks(l []trackSpec) []TrackSize {
	if len(l) == 0 {
		return nil
	}
	out := make([]TrackSize, len(l))
	for i, t := range l {
		out[i] = s.computeTrack(t)
	}
	return out
}

// computeTrackList computes a specified track list.
func (s *styler) computeTrackList(l trackListSpec) TrackList {
	out := TrackList{Names: l.names, Sizes: s.computeTracks(l.sizes)}
	if l.auto != nil {
		out.Auto = &TrackRepeat{Fit: l.auto.fit, At: l.auto.at, Names: l.auto.names, Sizes: s.computeTracks(l.auto.sizes)}
	}
	if len(out.Sizes) == 0 && out.Auto == nil {
		return TrackList{}
	}
	return out
}

// trackListProp defines grid-template-rows or grid-template-columns.
func trackListProp(name string, f func(*Style) *TrackList) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			if hasSubgrid(v) {
				return nil, false
			}
			l, ok := parseTrackList(v, false)
			return l, ok
		},
		compute: func(s *styler, st *Style, v any) {
			if l, ok := v.(trackListSpec); ok {
				*f(st) = s.computeTrackList(l)
			}
		},
		copyFrom: field(f),
	}
}

// hasSubgrid reports a subgrid value, which CSS Grid 2 defines and litho
// does not lay out: the declaration is invalid and the cascade falls back.
func hasSubgrid(v []token) bool {
	return len(v) > 0 && v[0].isIdent("subgrid")
}

// autoTracksProp defines grid-auto-rows or grid-auto-columns: one or more
// <track-size> (CSS Grid 1, 7.6).
func autoTracksProp(name string, f func(*Style) *[]TrackSize) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			l, ok := parseAutoTracks(v)
			return l, ok
		},
		compute: func(s *styler, st *Style, v any) {
			if l, ok := v.([]trackSpec); ok {
				*f(st) = s.computeTracks(l)
			}
		},
		copyFrom: field(f),
	}
}

func parseAutoTracks(v []token) ([]trackSpec, bool) {
	if len(v) == 0 || len(v) > MaxGridTracks {
		return nil, false
	}
	out := make([]trackSpec, 0, len(v))
	for _, t := range v {
		s, ok := parseTrackSize(t)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// parseAreas parses grid-template-areas strings (CSS Grid 1, 7.3): every
// string a row of the same number of cell tokens, every named area a
// rectangle.
func parseAreas(strs []string) (*GridAreas, bool) {
	if len(strs) == 0 || len(strs) > MaxGridTracks {
		return nil, false
	}
	var rows [][]string
	for _, s := range strs {
		cells, ok := areaCells(s)
		if !ok || len(cells) == 0 || len(cells) > MaxGridTracks {
			return nil, false
		}
		if len(rows) > 0 && len(cells) != len(rows[0]) {
			return nil, false
		}
		rows = append(rows, cells)
	}
	g := &GridAreas{Rows: len(rows), Cols: len(rows[0])}
	seen := map[string]int{}
	for r, row := range rows {
		for c, name := range row {
			if name == "" {
				continue
			}
			i, ok := seen[name]
			if !ok {
				// The area starts here: it extends right and down while
				// the name repeats.
				c1 := c
				for c1 < len(row) && row[c1] == name {
					c1++
				}
				r1 := r
				for r1 < len(rows) && rows[r1][c] == name {
					r1++
				}
				seen[name] = len(g.Areas)
				g.Areas = append(g.Areas, GridArea{Name: name, Row0: r, Row1: r1, Col0: c, Col1: c1})
				continue
			}
			a := g.Areas[i]
			if r < a.Row0 || r >= a.Row1 || c < a.Col0 || c >= a.Col1 {
				return nil, false
			}
		}
	}
	// Every cell of each area's rectangle must hold its name.
	for _, a := range g.Areas {
		for r := a.Row0; r < a.Row1; r++ {
			for c := a.Col0; c < a.Col1; c++ {
				if rows[r][c] != a.Name {
					return nil, false
				}
			}
		}
	}
	slices.SortFunc(g.Areas, func(a, b GridArea) int { return strings.Compare(a.Name, b.Name) })
	return g, true
}

// areaCells splits a grid-template-areas string into its cell tokens
// (CSS Grid 1, 7.3.1): a name, "" for a null cell token (a run of full
// stops); a trash token makes the string invalid.
func areaCells(s string) ([]string, bool) {
	var out []string
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f':
			i += n
		case r == '.':
			for i < len(s) && s[i] == '.' {
				i++
			}
			out = append(out, "")
		case isNameRune(r):
			j := i
			for j < len(s) {
				r2, n2 := utf8.DecodeRuneInString(s[j:])
				if !isNameRune(r2) {
					break
				}
				j += n2
			}
			out = append(out, s[i:j])
			i = j
		default:
			return nil, false
		}
	}
	return out, true
}

// isNameRune reports a name code point (CSS Syntax 3, 4.2).
func isNameRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r >= 0x80
}

// areasProp defines grid-template-areas: none or one string per row.
func areasProp() propDef {
	return propDef{
		name: "grid-template-areas",
		parse: func(v []token, _ *declCtx) (any, bool) {
			if k, ok := keyword(v); ok && k == "none" {
				return (*GridAreas)(nil), true
			}
			strs := make([]string, 0, len(v))
			for _, t := range v {
				if t.typ != tString {
					return nil, false
				}
				strs = append(strs, t.value)
			}
			g, ok := parseAreas(strs)
			return g, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if g, ok := v.(*GridAreas); ok {
				st.GridTemplateAreas = g
			}
		},
		copyFrom: field(func(s *Style) **GridAreas { return &s.GridTemplateAreas }),
	}
}

// parseAutoFlow parses grid-auto-flow (CSS Grid 1, 7.7): [ row | column ]
// || dense.
func parseAutoFlow(v []token) (GridAutoFlow, bool) {
	if len(v) < 1 || len(v) > 2 {
		return 0, false
	}
	var f GridAutoFlow
	var dir, dense bool
	for _, t := range v {
		if t.typ != tIdent {
			return 0, false
		}
		switch lower(t.value) {
		case "row":
			if dir {
				return 0, false
			}
			dir = true
		case "column":
			if dir {
				return 0, false
			}
			dir = true
			f |= GridFlowColumn
		case "dense":
			if dense {
				return 0, false
			}
			dense = true
			f |= GridFlowDense
		default:
			return 0, false
		}
	}
	return f, true
}

// parseGridLine parses a <grid-line> (CSS Grid 1, 8.3): auto, a
// <custom-ident>, [ <integer> && <custom-ident>? ], or [ span && [
// <integer> || <custom-ident> ] ]. The integer is not 0, positive with
// span, and clamped to [MaxGridLine].
func parseGridLine(v []token) (GridLine, bool) {
	if len(v) < 1 || len(v) > 3 {
		return GridLine{}, false
	}
	if k, ok := keyword(v); ok && k == "auto" {
		return GridLine{}, true
	}
	var g GridLine
	var hasN, hasName bool
	for i, t := range v {
		switch {
		case t.isIdent("span"):
			// span comes first or last, never between the others.
			if g.Span || i != 0 && i != len(v)-1 {
				return GridLine{}, false
			}
			g.Span = true
		case t.typ == tIdent:
			if hasName || !customIdent(t.value) || t.isIdent("auto") {
				return GridLine{}, false
			}
			g.Name, hasName = t.value, true
		default:
			n, ok := parseInteger(t)
			if !ok || hasN || n == 0 {
				return GridLine{}, false
			}
			g.N, hasN = max(min(n, MaxGridLine), -MaxGridLine), true
		}
	}
	if g.Span {
		if !hasN && !hasName || g.N < 0 {
			return GridLine{}, false
		}
		if !hasN {
			g.N = 1
		}
	}
	if !g.Span && !hasN && !hasName {
		return GridLine{}, false
	}
	return g, true
}

// gridLineProp defines a grid-placement longhand.
func gridLineProp(name string, f func(*Style) *GridLine) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			g, ok := parseGridLine(v)
			return g, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if g, ok := v.(GridLine); ok {
				*f(st) = g
			}
		},
		copyFrom: field(f),
	}
}

// parseJustify parses justify-items (self false) or justify-self (CSS Box
// Alignment 3, 6.1 and 6.2): the align-items or align-self values with
// left and right; legacy, alone or with left, right or center, computes to
// normal for justify-items.
func parseJustify(v []token, self bool) (BoxAlign, bool) {
	set := alignItems
	if self {
		set = alignSelf
	}
	words := make([]string, 0, 2)
	for _, t := range v {
		if t.typ != tIdent {
			return 0, false
		}
		words = append(words, lower(t.value))
	}
	if !self && slices.Contains(words, "legacy") {
		switch {
		case len(words) == 1:
			return BoxNormal, true
		case len(words) == 2:
			other := words[0]
			if other == "legacy" {
				other = words[1]
			}
			if other == "left" || other == "right" || other == "center" {
				return BoxNormal, true
			}
		}
		return 0, false
	}
	if len(words) >= 1 && len(words) <= 2 {
		pos := words[len(words)-1]
		if pos == "left" || pos == "right" {
			a := BoxLeft
			if pos == "right" {
				a = BoxRight
			}
			switch {
			case len(words) == 1:
				return a, true
			case words[0] == "safe":
				return a | BoxSafe, true
			case words[0] == "unsafe":
				return a, true
			}
			return 0, false
		}
	}
	return parseAlign(v, set, false)
}

// justifyProp defines justify-items or justify-self.
func justifyProp(name string, f func(*Style) *BoxAlign, self bool) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			a, ok := parseJustify(v, self)
			return a, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if a, ok := v.(BoxAlign); ok {
				*f(st) = a
			}
		},
		copyFrom: field(f),
	}
}

// gridProps returns the longhands of grid layout.
func gridProps() map[propID]propDef {
	return map[propID]propDef{
		pGridTemplateRows:    trackListProp("grid-template-rows", func(s *Style) *TrackList { return &s.GridTemplateRows }),
		pGridTemplateColumns: trackListProp("grid-template-columns", func(s *Style) *TrackList { return &s.GridTemplateColumns }),
		pGridTemplateAreas:   areasProp(),
		pGridAutoRows:        autoTracksProp("grid-auto-rows", func(s *Style) *[]TrackSize { return &s.GridAutoRows }),
		pGridAutoColumns:     autoTracksProp("grid-auto-columns", func(s *Style) *[]TrackSize { return &s.GridAutoColumns }),
		pGridAutoFlow: {
			name: "grid-auto-flow",
			parse: func(v []token, _ *declCtx) (any, bool) {
				f, ok := parseAutoFlow(v)
				return f, ok
			},
			compute: func(_ *styler, st *Style, v any) {
				if f, ok := v.(GridAutoFlow); ok {
					st.GridAutoFlow = f
				}
			},
			copyFrom: field(func(s *Style) *GridAutoFlow { return &s.GridAutoFlow }),
		},
		pGridRowStart:    gridLineProp("grid-row-start", func(s *Style) *GridLine { return &s.GridRowStart }),
		pGridRowEnd:      gridLineProp("grid-row-end", func(s *Style) *GridLine { return &s.GridRowEnd }),
		pGridColumnStart: gridLineProp("grid-column-start", func(s *Style) *GridLine { return &s.GridColumnStart }),
		pGridColumnEnd:   gridLineProp("grid-column-end", func(s *Style) *GridLine { return &s.GridColumnEnd }),
		pJustifyItems:    justifyProp("justify-items", func(s *Style) *BoxAlign { return &s.JustifyItems }, false),
		pJustifySelf:     justifyProp("justify-self", func(s *Style) *BoxAlign { return &s.JustifySelf }, true),
	}
}

// splitSlash splits v at its top-level / delimiters.
func splitSlash(v []token) [][]token {
	out := [][]token{nil}
	for _, t := range v {
		if t.isDelim('/') {
			out = append(out, nil)
			continue
		}
		out[len(out)-1] = append(out[len(out)-1], t)
	}
	return out
}

// lineShorthand expands grid-row or grid-column (CSS Grid 1, 8.4): a
// start line and an optional end line, which defaults to the start line
// when that is a lone <custom-ident>, else to auto.
func lineShorthand(start, end propID) shorthandDef {
	return shorthandDef{
		longhands: []propID{start, end},
		expand: func(v []token, _ *declCtx) ([]entry, bool) {
			parts := splitSlash(v)
			if len(parts) > 2 {
				return nil, false
			}
			s, ok := parseGridLine(parts[0])
			if !ok {
				return nil, false
			}
			e := identOrAuto(s)
			if len(parts) == 2 {
				if e, ok = parseGridLine(parts[1]); !ok {
					return nil, false
				}
			}
			return []entry{{start, s}, {end, e}}, true
		},
	}
}

// identOrAuto returns g when it is a lone <custom-ident>, else auto: the
// value an omitted placement longhand takes from the one before it.
func identOrAuto(g GridLine) GridLine {
	if !g.Span && g.N == 0 && g.Name != "" {
		return g
	}
	return GridLine{}
}

// expandGridArea expands grid-area (CSS Grid 1, 8.4): row-start /
// column-start / row-end / column-end.
func expandGridArea(v []token, _ *declCtx) ([]entry, bool) {
	parts := splitSlash(v)
	if len(parts) > 4 {
		return nil, false
	}
	var l [4]GridLine
	for i, p := range parts {
		g, ok := parseGridLine(p)
		if !ok {
			return nil, false
		}
		l[i] = g
	}
	if len(parts) < 2 {
		l[1] = identOrAuto(l[0])
	}
	if len(parts) < 3 {
		l[2] = identOrAuto(l[0])
	}
	if len(parts) < 4 {
		l[3] = identOrAuto(l[1])
	}
	return []entry{{pGridRowStart, l[0]}, {pGridColumnStart, l[1]}, {pGridRowEnd, l[2]}, {pGridColumnEnd, l[3]}}, true
}

// parseTemplate parses a grid-template value (CSS Grid 1, 7.4): none,
// <rows> / <columns>, or the ASCII-art form of rows with area strings and
// optional explicit columns. It returns the rows, the columns and the
// areas.
func parseTemplate(v []token) (trackListSpec, trackListSpec, *GridAreas, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return trackListSpec{}, trackListSpec{}, nil, true
	}
	hasString := false
	for _, t := range v {
		if t.typ == tString {
			hasString = true
		}
	}
	parts := splitSlash(v)
	if !hasString {
		if len(parts) != 2 || hasSubgrid(parts[0]) || hasSubgrid(parts[1]) {
			return trackListSpec{}, trackListSpec{}, nil, false
		}
		rows, ok := parseTrackList(parts[0], false)
		if !ok {
			return trackListSpec{}, trackListSpec{}, nil, false
		}
		cols, ok := parseTrackList(parts[1], false)
		if !ok {
			return trackListSpec{}, trackListSpec{}, nil, false
		}
		return rows, cols, nil, true
	}
	if len(parts) > 2 {
		return trackListSpec{}, trackListSpec{}, nil, false
	}
	var cols trackListSpec
	if len(parts) == 2 {
		var ok bool
		if cols, ok = parseTrackList(parts[1], true); !ok {
			return trackListSpec{}, trackListSpec{}, nil, false
		}
	}
	// [ <line-names>? <string> <track-size>? <line-names>? ]+: a row's
	// trailing names and the next row's leading ones share a line.
	var rows trackListSpec
	var strs []string
	var carry []string
	toks := parts[0]
	auto := trackSpec{min: breadthSpec{kind: TrackAuto}, max: breadthSpec{kind: TrackAuto}}
	for i := 0; i < len(toks); {
		var lead []string
		if toks[i].typ == tLBracket {
			n, ok := parseLineNames(toks[i])
			if !ok {
				return trackListSpec{}, trackListSpec{}, nil, false
			}
			lead, i = n, i+1
		}
		if i >= len(toks) || toks[i].typ != tString {
			return trackListSpec{}, trackListSpec{}, nil, false
		}
		strs = append(strs, toks[i].value)
		i++
		rows.names = append(rows.names, append(slices.Clip(carry), lead...))
		size := auto
		if i < len(toks) && toks[i].typ != tString && toks[i].typ != tLBracket {
			if toks[i].typ == tFunction && eqFold(toks[i].value, "repeat") {
				return trackListSpec{}, trackListSpec{}, nil, false
			}
			sz, ok := parseTrackSize(toks[i])
			if !ok {
				return trackListSpec{}, trackListSpec{}, nil, false
			}
			size, i = sz, i+1
		}
		rows.sizes = append(rows.sizes, size)
		carry = nil
		if i < len(toks) && toks[i].typ == tLBracket {
			n, ok := parseLineNames(toks[i])
			if !ok {
				return trackListSpec{}, trackListSpec{}, nil, false
			}
			carry, i = n, i+1
		}
	}
	if len(strs) == 0 {
		return trackListSpec{}, trackListSpec{}, nil, false
	}
	rows.names = append(rows.names, carry)
	areas, ok := parseAreas(strs)
	if !ok {
		return trackListSpec{}, trackListSpec{}, nil, false
	}
	return rows, cols, areas, true
}

// expandTemplate expands grid-template.
func expandTemplate(v []token, _ *declCtx) ([]entry, bool) {
	rows, cols, areas, ok := parseTemplate(v)
	if !ok {
		return nil, false
	}
	return []entry{{pGridTemplateRows, rows}, {pGridTemplateColumns, cols}, {pGridTemplateAreas, areas}}, true
}

// expandGrid expands grid (CSS Grid 1, 7.8): a grid-template, or
// <rows> / [ auto-flow && dense? ] <auto-columns>?, or [ auto-flow &&
// dense? ] <auto-rows>? / <columns>. The longhands it does not set reset
// to their initial values; the gaps keep theirs, as Chromium expands it.
func expandGrid(v []token, _ *declCtx) ([]entry, bool) {
	reset := []entry{
		{pGridAutoRows, wideInitial}, {pGridAutoColumns, wideInitial}, {pGridAutoFlow, wideInitial},
	}
	if rows, cols, areas, ok := parseTemplate(v); ok {
		return append(reset, entry{pGridTemplateRows, rows}, entry{pGridTemplateColumns, cols}, entry{pGridTemplateAreas, areas}), true
	}
	parts := splitSlash(v)
	if len(parts) != 2 {
		return nil, false
	}
	// flowOf reads the leading [ auto-flow && dense? ] of a half.
	flowOf := func(l []token) (GridAutoFlow, []token, bool) {
		var f GridAutoFlow
		seen := false
		i := 0
		for ; i < len(l) && i < 2; i++ {
			switch {
			case l[i].isIdent("auto-flow") && !seen:
				seen = true
			case l[i].isIdent("dense") && f&GridFlowDense == 0:
				f |= GridFlowDense
			default:
				return f, l[i:], seen
			}
		}
		return f, l[i:], seen
	}
	if f, rest, ok := flowOf(parts[1]); ok {
		rows, ok := parseTrackList(parts[0], false)
		if !ok || hasSubgrid(parts[0]) {
			return nil, false
		}
		var auto any = wideInitial
		if len(rest) > 0 {
			l, ok := parseAutoTracks(rest)
			if !ok {
				return nil, false
			}
			auto = l
		}
		return []entry{
			{pGridTemplateRows, rows},
			{pGridTemplateColumns, trackListSpec{}},
			{pGridTemplateAreas, (*GridAreas)(nil)},
			{pGridAutoFlow, f | GridFlowColumn},
			{pGridAutoColumns, auto},
			{pGridAutoRows, wideInitial},
		}, true
	}
	if f, rest, ok := flowOf(parts[0]); ok {
		cols, ok := parseTrackList(parts[1], false)
		if !ok || hasSubgrid(parts[1]) {
			return nil, false
		}
		var auto any = wideInitial
		if len(rest) > 0 {
			l, ok := parseAutoTracks(rest)
			if !ok {
				return nil, false
			}
			auto = l
		}
		return []entry{
			{pGridTemplateRows, trackListSpec{}},
			{pGridTemplateColumns, cols},
			{pGridTemplateAreas, (*GridAreas)(nil)},
			{pGridAutoFlow, f},
			{pGridAutoRows, auto},
			{pGridAutoColumns, wideInitial},
		}, true
	}
	return nil, false
}

// gridShorthands returns the shorthands of grid layout.
func gridShorthands() map[string]shorthandDef {
	tmpl := []propID{pGridTemplateRows, pGridTemplateColumns, pGridTemplateAreas}
	return map[string]shorthandDef{
		"grid-row":      lineShorthand(pGridRowStart, pGridRowEnd),
		"grid-column":   lineShorthand(pGridColumnStart, pGridColumnEnd),
		"grid-area":     {longhands: []propID{pGridRowStart, pGridColumnStart, pGridRowEnd, pGridColumnEnd}, expand: expandGridArea},
		"grid-template": {longhands: tmpl, expand: expandTemplate},
		"grid": {
			longhands: append(slices.Clone(tmpl), pGridAutoRows, pGridAutoColumns, pGridAutoFlow),
			expand:    expandGrid,
		},
	}
}
