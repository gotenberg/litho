package css

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// counterSystem is the system descriptor of a counter style (CSS Counter
// Styles 3, 3.1), with the algorithms of the complex predefined styles
// (CSS Counter Styles 3, 7).
type counterSystem uint8

const (
	systemSymbolic counterSystem = iota
	systemCyclic
	systemNumeric
	systemAlphabetic
	systemAdditive
	systemFixed
	systemExtends
	// systemChinese is the longhand Chinese algorithm (CSS Counter
	// Styles 3, 7.1.4.2) over the extended range of 7.1.5.
	systemChinese
	// systemKorean is the longhand Korean algorithm of 7.1.5.2.
	systemKorean
	// systemEthiopic is ethiopic-numeric (CSS Counter Styles 3, 7.2).
	systemEthiopic
)

// counterSystems maps the keywords of the system descriptor to their
// system. Immutable lookup table.
var counterSystems = map[string]counterSystem{
	"cyclic": systemCyclic, "numeric": systemNumeric, "alphabetic": systemAlphabetic,
	"symbolic": systemSymbolic, "additive": systemAdditive, "fixed": systemFixed, "extends": systemExtends,
}

// additiveSymbol is one tuple of additive-symbols: a weight and its
// symbol.
type additiveSymbol struct {
	weight int
	symbol string
}

// counterRange is one range of the range descriptor, bounds included;
// math.MinInt64 and math.MaxInt64 stand for infinite.
type counterRange struct{ lo, hi int64 }

// counterStyleRule holds the descriptors of an @counter-style rule (CSS
// Counter Styles 3, 3), each with whether the rule sets it.
type counterStyleRule struct {
	name      string
	system    counterSystem
	hasSystem bool
	// first is the first symbol value of a fixed system.
	first   int
	extends string

	negative    [2]string
	hasNegative bool
	prefix      string
	hasPrefix   bool
	suffix      string
	hasSuffix   bool
	// ranges is nil for auto.
	ranges    []counterRange
	hasRange  bool
	pad       int
	padSymbol string
	hasPad    bool
	fallback  string
	hasFall   bool
	symbols   []string
	additive  []additiveSymbol
	// digits are the characters of a complex predefined style: the ten
	// digits, then the digit markers and the negative sign (see
	// [chineseText] and [koreanText]).
	digits []string

	cond *mediaCond
	// layer is the rule's cascade layer index in its sheet.
	layer int
}

// maxCounterSymbols bounds the symbols and additive tuples of one
// counter style: 1024. Anti-DoS guard, not spec-mandated: the predefined
// styles use at most 60.
const maxCounterSymbols = 1024

// maxCounterLength bounds a counter representation, in characters: a
// longer one draws in the fallback style, which CSS Counter Styles 3, 1.3
// allows past 60 characters. Anti-DoS guard: a symbolic or additive
// style writes a number of symbols proportional to the value.
const maxCounterLength = 60

// maxCounterPad bounds the pad descriptor's length: the representation
// it pads stays within maxCounterLength.
const maxCounterPad = maxCounterLength

// maxExtendsDepth bounds a chain of extends and fallback styles.
// Anti-DoS guard, not spec-mandated: a chain longer than the count of
// styles holds a cycle.
const maxExtendsDepth = 64

// fixedCounterNames are the counter style names an @counter-style rule
// cannot define (CSS Counter Styles 3, 3): the rule is invalid.
// Immutable lookup table.
var fixedCounterNames = map[string]bool{
	"none": true, "decimal": true, "disc": true, "square": true, "circle": true,
	"disclosure-open": true, "disclosure-closed": true,
}

// counterStyleName returns the name a <counter-style-name> ident stands
// for: the ident lowercased when it names a predefined style, which CSS
// Counter Styles 3, 2 matches ASCII case-insensitively, else as written.
func counterStyleName(ident string) string {
	if l := lower(ident); predefinedNames()[l] || l == "none" {
		return l
	}
	return ident
}

// parseCounterStyleRule parses the prelude and block of an @counter-style
// rule (CSS Counter Styles 3, 3). It reports false for a rule that
// defines nothing: a name that is not a <custom-ident> or that a rule
// cannot define, unless predefined, for the predefined styles' own rules.
// An invalid descriptor is dropped alone.
func parseCounterStyleRule(prelude, block []token, c *declCtx, predefined bool) (*counterStyleRule, bool) {
	p := significant(prelude)
	if len(p) != 1 || p[0].typ != tIdent || cssWideKeywords[lower(p[0].value)] || fixedCounterNames[lower(p[0].value)] && !predefined {
		return nil, false
	}
	r := &counterStyleRule{name: counterStyleName(p[0].value)}
	newListParser(block).declarations(func(it item) bool {
		if it.kind != itemDeclaration {
			return true
		}
		v := significant(it.value)
		if !r.descriptor(lower(it.name), v, c) {
			c.warn(warn.CSSInvalid, warn.Approximated, "@counter-style %s: the %s descriptor is invalid and was ignored", r.name, lower(it.name))
		}
		return true
	})
	return r, true
}

// cssWideKeywords are the names no <custom-ident> takes (CSS Values 4,
// 4.2). Immutable lookup table.
var cssWideKeywords = map[string]bool{"initial": true, "inherit": true, "unset": true, "revert": true, "revert-layer": true, "default": true}

// descriptor sets the descriptor name of r from v, reporting false for an
// invalid value. speak-as is parsed and not used: a PDF reader reads the
// marker's text.
func (r *counterStyleRule) descriptor(name string, v []token, c *declCtx) bool {
	switch name {
	case "system":
		return r.parseSystem(v)
	case "negative":
		if len(v) < 1 || len(v) > 2 {
			return false
		}
		var neg [2]string
		for i, t := range v {
			s, ok := symbolOf(t, c)
			if !ok {
				return false
			}
			neg[i] = s
		}
		r.negative, r.hasNegative = neg, true
	case "prefix", "suffix":
		if len(v) != 1 {
			return false
		}
		s, ok := symbolOf(v[0], c)
		if !ok {
			return false
		}
		if name == "prefix" {
			r.prefix, r.hasPrefix = s, true
		} else {
			r.suffix, r.hasSuffix = s, true
		}
	case "range":
		ranges, ok := parseCounterRange(v)
		if !ok {
			return false
		}
		r.ranges, r.hasRange = ranges, true
	case "pad":
		return r.parsePad(v, c)
	case "fallback":
		if len(v) != 1 || v[0].typ != tIdent || lower(v[0].value) == "none" {
			return false
		}
		r.fallback, r.hasFall = counterStyleName(v[0].value), true
	case "symbols":
		if len(v) == 0 || len(v) > maxCounterSymbols {
			return false
		}
		syms := make([]string, 0, len(v))
		for _, t := range v {
			s, ok := symbolOf(t, c)
			if !ok {
				return false
			}
			syms = append(syms, s)
		}
		r.symbols = syms
	case "additive-symbols":
		return r.parseAdditive(v, c)
	case "speak-as":
		return len(v) == 1 && v[0].typ == tIdent
	default:
		return false
	}
	return true
}

// parseSystem parses the system descriptor: a keyword, fixed with an
// optional first value, or extends with a counter style name.
func (r *counterStyleRule) parseSystem(v []token) bool {
	if len(v) == 0 || v[0].typ != tIdent {
		return false
	}
	sys, ok := counterSystems[lower(v[0].value)]
	if !ok {
		return false
	}
	switch sys { //nolint:exhaustive // the other systems take no argument
	case systemFixed:
		r.first = 1
		if len(v) == 2 {
			n, ok := parseInteger(v[1])
			if !ok {
				return false
			}
			r.first = n
		} else if len(v) > 2 {
			return false
		}
	case systemExtends:
		if len(v) != 2 || v[1].typ != tIdent || lower(v[1].value) == "none" {
			return false
		}
		r.extends = counterStyleName(v[1].value)
	default:
		if len(v) != 1 {
			return false
		}
	}
	r.system, r.hasSystem = sys, true
	return true
}

// parseCounterRange parses the range descriptor: auto, or a list of pairs
// of integers or infinite, each lower bound at most its upper bound.
func parseCounterRange(v []token) ([]counterRange, bool) {
	if k, ok := keyword(v); ok && k == "auto" {
		return nil, true
	}
	var out []counterRange
	for _, part := range splitCommas(v) {
		if len(part) != 2 || len(out) == maxCounterSymbols {
			return nil, false
		}
		var b [2]int64
		for i, t := range part {
			switch {
			case t.isIdent("infinite") && i == 0:
				b[i] = math.MinInt64
			case t.isIdent("infinite"):
				b[i] = math.MaxInt64
			default:
				n, ok := parseInteger(t)
				if !ok {
					return nil, false
				}
				b[i] = int64(n)
			}
		}
		if b[0] > b[1] {
			return nil, false
		}
		out = append(out, counterRange{lo: b[0], hi: b[1]})
	}
	return out, len(out) > 0
}

// parsePad parses the pad descriptor: a non-negative integer and a
// symbol, in either order.
func (r *counterStyleRule) parsePad(v []token, c *declCtx) bool {
	if len(v) != 2 {
		return false
	}
	n, sym := v[0], v[1]
	if _, ok := parseInteger(n); !ok {
		n, sym = sym, n
	}
	k, ok := parseInteger(n)
	if !ok || k < 0 {
		return false
	}
	s, ok := symbolOf(sym, c)
	if !ok {
		return false
	}
	r.pad, r.padSymbol, r.hasPad = min(k, maxCounterPad), s, true
	return true
}

// parseAdditive parses additive-symbols: a list of a non-negative weight
// and a symbol, in either order, the weights strictly decreasing.
func (r *counterStyleRule) parseAdditive(v []token, c *declCtx) bool {
	parts := splitCommas(v)
	if len(parts) == 0 || len(parts) > maxCounterSymbols {
		return false
	}
	out := make([]additiveSymbol, 0, len(parts))
	for _, part := range parts {
		if len(part) != 2 {
			return false
		}
		n, sym := part[0], part[1]
		if _, ok := parseInteger(n); !ok {
			n, sym = sym, n
		}
		w, ok := parseInteger(n)
		if !ok || w < 0 || len(out) > 0 && w >= out[len(out)-1].weight {
			return false
		}
		s, ok := symbolOf(sym, c)
		if !ok {
			return false
		}
		out = append(out, additiveSymbol{weight: w, symbol: s})
	}
	r.additive = out
	return true
}

// symbolOf returns a <symbol>: a string or an ident (CSS Counter Styles
// 3, 3.1). An image symbol is not supported.
func symbolOf(t token, c *declCtx) (string, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are no symbol
	case tString:
		return t.value, true
	case tIdent:
		return t.value, true
	case tURL, tFunction:
		c.warn(warn.CSSUnsupported, warn.Approximated, "an image counter symbol is not supported")
	}
	return "", false
}

// counterStyle is a counter style with every descriptor resolved (CSS
// Counter Styles 3, 3), extends applied.
type counterStyle struct {
	name      string
	system    counterSystem
	first     int
	negative  [2]string
	prefix    string
	suffix    string
	ranges    []counterRange
	pad       int
	padSymbol string
	fallback  string
	symbols   []string
	additive  []additiveSymbol
	digits    []string
}

// valid reports whether the rule defines a counter style: the symbols its
// system needs (CSS Counter Styles 3, 3.1.1 to 3.1.7; 3.8 and 3.9).
func (r *counterStyleRule) valid() bool {
	switch r.system {
	case systemCyclic, systemFixed, systemSymbolic:
		return len(r.symbols) >= 1
	case systemAlphabetic, systemNumeric:
		return len(r.symbols) >= 2
	case systemAdditive:
		return len(r.additive) >= 1
	case systemExtends:
		return r.symbols == nil && r.additive == nil
	case systemChinese, systemKorean, systemEthiopic:
		return true
	}
	return false
}

// CounterStyles resolves counter style names to the predefined styles of
// CSS Counter Styles 3, 6 and 7 and to a document's @counter-style rules,
// and spells counter values in them. It is safe for concurrent use.
type CounterStyles struct {
	rules map[string]*counterStyleRule
	mu    sync.Mutex
	cache map[string]*counterStyle
}

// newCounterStyles returns the counter styles of rules, the last rule of
// a name winning (CSS Counter Styles 3, 3).
func newCounterStyles(rules []*counterStyleRule) *CounterStyles {
	cs := &CounterStyles{rules: map[string]*counterStyleRule{}}
	for _, r := range rules {
		cs.rules[r.name] = r
	}
	return cs
}

// Defines reports whether a document rule defines name, so that a
// predefined style litho also draws itself is redefined.
func (c *CounterStyles) Defines(name string) bool {
	if c == nil {
		return false
	}
	r, ok := c.rules[name]
	return ok && r.valid()
}

// Known reports whether name is a counter style: a predefined one or one
// a rule defines. An unknown name draws as decimal.
func (c *CounterStyles) Known(name string) bool {
	return c.Defines(name) || predefinedCounterStyles()[name] != nil || isSymbolsName(name)
}

// Counter spells v in the counter style name, for counter() and
// counters() (CSS Counter Styles 3, 1.3): no prefix nor suffix, decimal
// for an unknown name.
func (c *CounterStyles) Counter(name string, v int) string {
	if c == nil {
		c = noCounterRules
	}
	return c.represent(c.resolve(name), v, 0)
}

// Marker returns the text of a list item's marker numbered v in the
// counter style name (CSS Lists 3, 3.1 and CSS Counter Styles 3, 3.3 and
// 3.4): the style's prefix, the representation and its suffix.
func (c *CounterStyles) Marker(name string, v int) string {
	if c == nil {
		c = noCounterRules
	}
	s := c.resolve(name)
	return s.prefix + c.represent(s, v, 0) + s.suffix
}

// noCounterRules is the counter styles of a document without
// @counter-style rules, which a nil [CounterStyles] stands for.
var noCounterRules = newCounterStyles(nil)

// resolve returns the resolved style of name, decimal for an unknown one.
func (c *CounterStyles) resolve(name string) *counterStyle {
	if c == nil {
		c = noCounterRules
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resolveLocked(name, 0)
}

// resolveLocked resolves name at depth of an extends chain. c.mu is held.
func (c *CounterStyles) resolveLocked(name string, depth int) *counterStyle {
	if s, ok := c.cache[name]; ok {
		return s
	}
	if depth > maxExtendsDepth {
		return decimalStyle()
	}
	var r *counterStyleRule
	if dr, ok := c.rules[name]; ok && dr.valid() {
		r = dr
	}
	if pr := predefinedCounterStyles()[name]; r == nil && pr != nil && pr.valid() {
		r = pr
	}
	if r == nil && isSymbolsName(name) {
		r = symbolsRule(name)
	}
	if r == nil {
		return decimalStyle()
	}
	var s *counterStyle
	if r.system == systemExtends {
		// A style that extends itself, or a style of a cycle, extends
		// decimal (CSS Counter Styles 3, 3.1.8).
		if c.cache == nil {
			c.cache = map[string]*counterStyle{}
		}
		c.cache[name] = decimalStyle()
		base := *c.resolveLocked(r.extends, depth+1)
		s = &base
		s.name = name
	} else {
		s = &counterStyle{
			name: name, system: r.system, first: r.first, symbols: r.symbols, additive: r.additive, digits: r.digits,
			negative: [2]string{"-", ""}, suffix: ". ", fallback: "decimal",
		}
	}
	if r.hasNegative {
		s.negative = r.negative
	}
	if r.hasPrefix {
		s.prefix = r.prefix
	}
	if r.hasSuffix {
		s.suffix = r.suffix
	}
	if r.hasRange {
		s.ranges = r.ranges
	}
	if r.hasPad {
		s.pad, s.padSymbol = r.pad, r.padSymbol
	}
	if r.hasFall {
		s.fallback = r.fallback
	}
	if c.cache == nil {
		c.cache = map[string]*counterStyle{}
	}
	c.cache[name] = s
	return s
}

// decimalStyle returns the decimal style (CSS Counter Styles 3, 6.1).
func decimalStyle() *counterStyle {
	return &counterStyle{
		name: "decimal", system: systemNumeric, symbols: []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"},
		negative: [2]string{"-", ""}, suffix: ". ", fallback: "decimal",
	}
}

// inRange reports whether v lies in the range of s (CSS Counter Styles 3,
// 3.5), auto taking the range of the system.
func (s *counterStyle) inRange(v int) bool {
	if s.ranges == nil {
		switch s.system {
		case systemAlphabetic, systemSymbolic, systemEthiopic:
			return v >= 1
		case systemAdditive:
			return v >= 0
		case systemChinese, systemKorean:
			// The extended range of CSS Counter Styles 3, 7.1.5.
			return v > -1e16 && v < 1e16
		case systemCyclic, systemNumeric, systemFixed, systemExtends:
		}
		return true
	}
	for _, r := range s.ranges {
		if int64(v) >= r.lo && int64(v) <= r.hi {
			return true
		}
	}
	return false
}

// usesNegative reports whether s writes a negative sign (CSS Counter
// Styles 3, 3.2): the symbolic, alphabetic, numeric and additive systems.
func (s *counterStyle) usesNegative() bool {
	switch s.system {
	case systemSymbolic, systemAlphabetic, systemNumeric, systemAdditive:
		return true
	case systemCyclic, systemFixed, systemExtends, systemChinese, systemKorean, systemEthiopic:
	}
	return false
}

// represent generates the representation of v in s (CSS Counter Styles
// 3, 1.3), at depth of a fallback chain.
func (c *CounterStyles) represent(s *counterStyle, v int, depth int) string {
	fallback := func() string {
		if depth >= maxExtendsDepth || s.name == "decimal" && s.fallback == "decimal" {
			return strconv.Itoa(v)
		}
		return c.represent(c.resolve(s.fallback), v, depth+1)
	}
	if !s.inRange(v) {
		return fallback()
	}
	neg := v < 0 && s.usesNegative()
	abs := v
	if neg {
		abs = -v
	}
	rep, ok := s.initial(abs)
	if !ok {
		return fallback()
	}
	if s.pad > 0 {
		diff := s.pad - graphemeCount(rep)
		if neg {
			diff -= graphemeCount(s.negative[0]) + graphemeCount(s.negative[1])
		}
		if diff > 0 {
			rep = strings.Repeat(s.padSymbol, diff) + rep
		}
	}
	if neg {
		rep = s.negative[0] + rep + s.negative[1]
	}
	if utf8.RuneCountInString(rep) > maxCounterLength && depth < maxExtendsDepth && s.name != "decimal" {
		return fallback()
	}
	return rep
}

// graphemeCount counts the grapheme clusters of s, a character with the
// combining marks after it counting once.
func graphemeCount(s string) int {
	n := 0
	for _, r := range s {
		if !isCombining(r) {
			n++
		}
	}
	return n
}

// initial generates the initial representation of v, which is not
// negative when the style uses a negative sign, by the system's
// algorithm (CSS Counter Styles 3, 3.1). It reports false for a value
// the algorithm cannot represent.
func (s *counterStyle) initial(v int) (string, bool) {
	syms := s.symbols
	switch s.system {
	case systemCyclic:
		n := len(syms)
		return syms[((v-1)%n+n)%n], true
	case systemFixed:
		i := v - s.first
		if v < s.first || i >= len(syms) {
			return "", false
		}
		return syms[i], true
	case systemSymbolic:
		n := len(syms)
		reps := (v + n - 1) / n
		if v < 1 || reps > maxCounterLength {
			return "", false
		}
		return strings.Repeat(syms[(v-1)%n], reps), true
	case systemAlphabetic:
		n := len(syms)
		if v < 1 || n < 2 {
			return "", false
		}
		var parts []string
		for v > 0 {
			v--
			parts = append(parts, syms[v%n])
			v /= n
		}
		return joinBackward(parts), true
	case systemNumeric:
		n := len(syms)
		if n < 2 {
			return "", false
		}
		if v == 0 {
			return syms[0], true
		}
		var parts []string
		for v > 0 {
			parts = append(parts, syms[v%n])
			v /= n
		}
		return joinBackward(parts), true
	case systemAdditive:
		return additiveRep(s.additive, v)
	case systemChinese:
		return chineseText(s.digits, v), true
	case systemKorean:
		return koreanText(s.digits, v), true
	case systemEthiopic:
		return ethiopicText(v), true
	case systemExtends:
	}
	return "", false
}

// joinBackward joins parts in reverse order.
func joinBackward(parts []string) string {
	var b strings.Builder
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteString(parts[i])
	}
	return b.String()
}

// additiveRep is the additive algorithm (CSS Counter Styles 3, 3.1.6): 0
// is the symbol of weight 0, else the largest weights first; it fails
// when a remainder is left or the representation grows past
// maxCounterLength symbols.
func additiveRep(tuples []additiveSymbol, v int) (string, bool) {
	if v == 0 {
		if last := tuples[len(tuples)-1]; last.weight == 0 {
			return last.symbol, true
		}
		return "", false
	}
	var b strings.Builder
	count := 0
	for _, t := range tuples {
		if t.weight == 0 || v < t.weight {
			continue
		}
		reps := v / t.weight
		count += reps
		if count > maxCounterLength {
			return "", false
		}
		v -= reps * t.weight
		b.WriteString(strings.Repeat(t.symbol, reps))
		if v == 0 {
			return b.String(), true
		}
	}
	return "", false
}

// symbolsPrefix starts the name a symbols() function computes to: no
// <custom-ident> holds it.
const symbolsPrefix = "\x00symbols"

// isSymbolsName reports a name a symbols() function computes to.
func isSymbolsName(name string) bool { return strings.HasPrefix(name, symbolsPrefix) }

// symbolsName returns the name an anonymous style of symbols() computes
// to (CSS Counter Styles 3, 4): its system and symbols, NUL-separated.
func symbolsName(sys counterSystem, syms []string) string {
	return symbolsPrefix + "\x00" + strconv.Itoa(int(sys)) + "\x00" + strings.Join(syms, "\x00")
}

// symbolsRule returns the anonymous style a symbols() name stands for: its
// system and symbols, an empty prefix, a space as suffix and the initial
// value of every other descriptor (CSS Counter Styles 3, 4).
func symbolsRule(name string) *counterStyleRule {
	parts := strings.Split(strings.TrimPrefix(name, symbolsPrefix+"\x00"), "\x00")
	if len(parts) < 2 {
		return nil
	}
	sys, err := strconv.Atoi(parts[0])
	if err != nil || sys < 0 || sys > int(systemFixed) {
		return nil
	}
	r := &counterStyleRule{
		name: name, system: counterSystem(sys), first: 1, symbols: parts[1:],
		prefix: "", hasPrefix: true, suffix: " ", hasSuffix: true,
	}
	if !r.valid() {
		return nil
	}
	return r
}

// parseSymbolsFunction parses symbols() (CSS Counter Styles 3, 4): an
// optional system among cyclic, numeric, alphabetic, symbolic and fixed,
// symbolic by default, then strings; numeric and alphabetic need two. It
// returns the name the function computes to.
func parseSymbolsFunction(t token, c *declCtx) (string, bool) {
	args := significant(t.children)
	sys := systemSymbolic
	if len(args) > 0 && args[0].typ == tIdent {
		k, ok := counterSystems[lower(args[0].value)]
		if !ok || k == systemAdditive || k == systemExtends {
			return "", false
		}
		sys = k
		args = args[1:]
	}
	if len(args) == 0 || len(args) > maxCounterSymbols {
		return "", false
	}
	syms := make([]string, 0, len(args))
	for _, a := range args {
		if a.typ != tString {
			if a.typ == tURL || a.typ == tFunction {
				c.warn(warn.CSSUnsupported, warn.Approximated, "an image counter symbol is not supported")
			}
			return "", false
		}
		if strings.ContainsRune(a.value, 0) {
			return "", false
		}
		syms = append(syms, a.value)
	}
	if (sys == systemNumeric || sys == systemAlphabetic) && len(syms) < 2 {
		return "", false
	}
	return symbolsName(sys, syms), true
}
