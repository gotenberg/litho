package docx

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// maxLevels is the number of list levels WordprocessingML defines
// (ECMA-376 Part 1, 17.9.3: ilvl 0 to 8).
const maxLevels = 9

// level is one w:lvl of an abstract numbering (ECMA-376 Part 1, 17.9.6).
type level struct {
	start   int
	format  string
	text    string
	jc      string
	suffix  string
	legal   bool
	restart int // -1 for the default: after any higher level
	ppr     *paraProps
	rpr     *runProps
	set     bool
}

// abstractNum is a w:abstractNum (17.9.1): its levels and the style it
// links to.
type abstractNum struct {
	id           string
	levels       [maxLevels]level
	numStyleLink string
	counters     [maxLevels]int
	used         [maxLevels]bool
}

// numInstance is a w:num (17.9.15): the abstract numbering it instantiates
// and its level overrides.
type numInstance struct {
	abs       *abstractNum
	overrides [maxLevels]*level
	starts    [maxLevels]*int
	restarted [maxLevels]bool
}

// numbering is the numbering part, with the counters of the lists as the
// document numbers its paragraphs in order.
type numbering struct {
	abstract map[string]*abstractNum
	nums     map[int]*numInstance
}

// parseNumbering reads the numbering part's root, w:numbering.
func (d *document) parseNumbering(root *ooxml.Element) *numbering {
	n := &numbering{abstract: map[string]*abstractNum{}, nums: map[int]*numInstance{}}
	if root == nil {
		return n
	}
	for _, e := range root.Kids() {
		if !e.Is(w, "abstractNum") {
			continue
		}
		a := &abstractNum{id: attrOr(e, "abstractNumId")}
		a.numStyleLink = attrOr(e.Child(w, "numStyleLink"), "val")
		for _, l := range e.Kids() {
			if !l.Is(w, "lvl") {
				continue
			}
			i, ok := ooxml.Int(attrOr(l, "ilvl"))
			if !ok || i < 0 || i >= maxLevels {
				continue
			}
			a.levels[i] = d.parseLevel(l)
		}
		if _, dup := n.abstract[a.id]; !dup {
			n.abstract[a.id] = a
		}
	}
	for _, e := range root.Kids() {
		if !e.Is(w, "num") {
			continue
		}
		id, ok := ooxml.Int(attrOr(e, "numId"))
		if !ok {
			continue
		}
		ni := &numInstance{abs: n.abstract[attrOr(e.Child(w, "abstractNumId"), "val")]}
		for _, o := range e.Kids() {
			if !o.Is(w, "lvlOverride") {
				continue
			}
			i, ok := ooxml.Int(attrOr(o, "ilvl"))
			if !ok || i < 0 || i >= maxLevels {
				continue
			}
			if s, ok := ooxml.Int(attrOr(o.Child(w, "startOverride"), "val")); ok {
				ni.starts[i] = ptr(s)
			}
			if l := o.Child(w, "lvl"); l != nil {
				lv := d.parseLevel(l)
				ni.overrides[i] = &lv
			}
		}
		if _, dup := n.nums[id]; !dup {
			n.nums[id] = ni
		}
	}
	return n
}

// parseLevel reads a w:lvl element.
func (d *document) parseLevel(l *ooxml.Element) level {
	lv := level{start: 1, format: "decimal", suffix: "tab", restart: -1, set: true}
	if v, ok := ooxml.Int(attrOr(l.Child(w, "start"), "val")); ok {
		lv.start = v
	}
	if f := l.Child(w, "numFmt"); f != nil {
		lv.format = attrOr(f, "val")
	}
	if t := l.Child(w, "lvlText"); t != nil {
		lv.text = attrOr(t, "val")
	}
	lv.jc = attrOr(l.Child(w, "lvlJc"), "val")
	if s := l.Child(w, "suff"); s != nil {
		lv.suffix = attrOr(s, "val")
	}
	if lg := l.Child(w, "isLgl"); lg != nil {
		lv.legal = ooxml.Toggle(lg)
	}
	if v, ok := ooxml.Int(attrOr(l.Child(w, "lvlRestart"), "val")); ok {
		lv.restart = v
	}
	if p := l.Child(w, "pPr"); p != nil {
		lv.ppr = d.parseParaProps(p)
	}
	if r := l.Child(w, "rPr"); r != nil {
		lv.rpr = d.parseRunProps(r)
	}
	return lv
}

// instance returns the numbering instance numID, following an abstract
// numbering's numStyleLink to the numbering its style names (17.9.21);
// nil for numID 0, which removes numbering, or an unknown one.
func (d *document) instance(numID int) *numInstance {
	n := d.numbering
	ni := n.nums[numID]
	if ni == nil || ni.abs == nil {
		return nil
	}
	if ni.abs.numStyleLink == "" {
		return ni
	}
	st := d.styles.byID[ni.abs.numStyleLink]
	if st == nil || st.ppr == nil || st.ppr.numID == nil || *st.ppr.numID == numID {
		return ni
	}
	linked := n.nums[*st.ppr.numID]
	if linked == nil || linked.abs == nil || linked.abs.numStyleLink != "" {
		return ni
	}
	// The linked instance's abstract numbering numbers the paragraphs, with
	// this instance's overrides on top; the result replaces the instance.
	merged := *linked
	for i := range maxLevels {
		if ni.overrides[i] != nil {
			merged.overrides[i] = ni.overrides[i]
		}
		if ni.starts[i] != nil {
			merged.starts[i] = ni.starts[i]
		}
	}
	n.nums[numID] = &merged
	return &merged
}

// levelOf returns the level ilvl of instance ni, its override first.
func (ni *numInstance) levelOf(ilvl int) *level {
	if o := ni.overrides[ilvl]; o != nil {
		return o
	}
	return &ni.abs.levels[ilvl]
}

// next numbers the next paragraph of instance ni at level ilvl and returns
// its label and its ordinal (17.9.6: the counters of the abstract
// numbering continue across its instances; a level restarts after a
// higher one unless lvlRestart says otherwise; a startOverride restarts
// its level at the instance's first use).
func (ni *numInstance) next(ilvl int) (string, int) {
	a := ni.abs
	lv := ni.levelOf(ilvl)
	if s := ni.starts[ilvl]; s != nil && !ni.restarted[ilvl] {
		ni.restarted[ilvl] = true
		a.counters[ilvl] = *s - 1
		a.used[ilvl] = true
	}
	if !a.used[ilvl] {
		a.counters[ilvl] = lv.start - 1
		a.used[ilvl] = true
	}
	a.counters[ilvl]++
	for deeper := ilvl + 1; deeper < maxLevels; deeper++ {
		dl := ni.levelOf(deeper)
		if dl.restart == 0 || dl.restart > 0 && dl.restart-1 < ilvl {
			continue
		}
		a.used[deeper] = false
	}
	return ni.label(ilvl), a.counters[ilvl]
}

// label formats the lvlText of level ilvl: each %n stands for level n's
// counter in that level's format, or in decimal when ilvl's level is
// legal (17.9.11 lvlText, 17.9.4 isLgl).
func (ni *numInstance) label(ilvl int) string {
	lv := ni.levelOf(ilvl)
	if lv.format == "none" {
		return ""
	}
	var b strings.Builder
	t := lv.text
	for i := 0; i < len(t); i++ {
		if t[i] == '%' && i+1 < len(t) && t[i+1] >= '1' && t[i+1] <= '9' {
			n := int(t[i+1] - '1')
			i++
			if n > ilvl {
				continue
			}
			ref := ni.levelOf(n)
			v := ni.abs.counters[n]
			if !ni.abs.used[n] {
				v = ref.start
			}
			format := ref.format
			if lv.legal {
				format = "decimal"
			}
			b.WriteString(formatNumber(v, format))
			continue
		}
		b.WriteByte(t[i])
	}
	return b.String()
}

// romanNumerals are the additive symbols of roman numbers.
var romanNumerals = []struct {
	v int
	s string
}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}}

// formatNumber writes n in an ST_NumberFormat (ECMA-376 Part 1,
// 17.18.59); a format the converter does not write falls back to decimal.
func formatNumber(n int, format string) string {
	if s, ok := cjkFormat(n, format); ok {
		if s == "" {
			return strconv.Itoa(n)
		}
		return s
	}
	switch format {
	case "none":
		return ""
	case "bullet":
		return ""
	case "decimalZero":
		if n >= 0 && n < 10 {
			return "0" + strconv.Itoa(n)
		}
	case "upperRoman", "lowerRoman":
		if n > 0 && n < 4000 {
			var b strings.Builder
			for _, r := range romanNumerals {
				for n >= r.v {
					b.WriteString(r.s)
					n -= r.v
				}
			}
			if format == "lowerRoman" {
				return strings.ToLower(b.String())
			}
			return b.String()
		}
	case "upperLetter", "lowerLetter":
		// Word repeats the letter: 27 is AA, 53 is AAA.
		if n > 0 && n <= 780 {
			c := byte('A' + (n-1)%26)
			if format == "lowerLetter" {
				c += 'a' - 'A'
			}
			return strings.Repeat(string(c), (n-1)/26+1)
		}
	case "ordinal":
		return strconv.Itoa(n) + ordinalSuffix(n)
	case "cardinalText":
		if s := cardinalWords(n); s != "" {
			return strings.ToUpper(s[:1]) + s[1:]
		}
	case "ordinalText":
		if s := ordinalWords(n); s != "" {
			return strings.ToUpper(s[:1]) + s[1:]
		}
	case "decimalEnclosedCircle", "decimalEnclosedCircleChinese":
		if n >= 1 && n <= 20 {
			return string(rune(0x2460 + n - 1))
		}
	case "decimalEnclosedFullstop":
		if n >= 1 && n <= 20 {
			return string(rune(0x2488 + n - 1))
		}
	case "decimalEnclosedParen":
		if n >= 1 && n <= 20 {
			return string(rune(0x2474 + n - 1))
		}
	case "russianLower", "russianUpper":
		if n > 0 && n <= 29*28 {
			letters := []rune("абвгдежзиклмнопрстуфхцчшщэюя")
			s := strings.Repeat(string(letters[(n-1)%len(letters)]), (n-1)/len(letters)+1)
			if format == "russianUpper" {
				s = strings.ToUpper(s)
			}
			return s
		}
	}
	return strconv.Itoa(n)
}

// ordinalSuffix returns the English ordinal suffix of n.
func ordinalSuffix(n int) string {
	if m := n % 100; m >= 11 && m <= 13 {
		return "th"
	}
	switch n % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	}
	return "th"
}

var (
	ones = []string{
		"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
		"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen",
	}
	tens = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}
)

// cardinalWords spells 0 to 999 in English, "" beyond.
func cardinalWords(n int) string {
	switch {
	case n < 0 || n > 999:
		return ""
	case n < 20:
		return ones[n]
	case n < 100:
		if n%10 == 0 {
			return tens[n/10]
		}
		return tens[n/10] + "-" + ones[n%10]
	}
	s := ones[n/100] + " hundred"
	if n%100 != 0 {
		s += " " + cardinalWords(n%100)
	}
	return s
}

// ordinalWords spells the ordinal of 1 to 999 in English, "" beyond.
func ordinalWords(n int) string {
	s := cardinalWords(n)
	if s == "" || n == 0 {
		return ""
	}
	irregular := map[string]string{"one": "first", "two": "second", "three": "third", "five": "fifth", "eight": "eighth", "nine": "ninth", "twelve": "twelfth"}
	i := strings.LastIndexAny(s, " -")
	last := s[i+1:]
	switch {
	case irregular[last] != "":
		last = irregular[last]
	case strings.HasSuffix(last, "y"):
		last = last[:len(last)-1] + "ieth"
	default:
		last += "th"
	}
	return s[:i+1] + last
}

// symbolChars maps the code points of the Symbol font Word lists name in
// the private use area (U+F020 to U+F0FF, the font's byte plus 0xF000) to
// Unicode, for the characters list labels use.
var symbolChars = map[rune]rune{
	0xB7: '•', 0xA7: '♣', 0xA8: '♦', 0xA9: '♥', 0xAA: '♠', 0xD8: '¬', 0xAE: '→', 0xDE: '⇒', 0xB0: '°', 0x2D: '−', 0x2A: '∗',
}

// wingdingsChars maps Wingdings characters list labels use to Unicode.
var wingdingsChars = map[rune]rune{
	0xA7: '▪', 0xA8: '◻', 0x6E: '■', 0x6F: '□', 0x71: '❑', 0x75: '◆', 0x76: '❖', 0xD8: '➢', 0xFC: '✔', 0xFB: '✘',
	0x9F: '•', 0xA1: '○', 0x77: '⬥', 0xE0: '➔', 0xE8: '➔', 0x4A: '☺', 0x4C: '☹', 0xF0: '⇨',
}

// mapSymbolText returns s with the private-use characters of a symbol font
// mapped to Unicode, and whether the font family no longer applies: Symbol
// and Wingdings are not installed where LibreOffice runs Gotenberg, which
// draws their list bullets from its own symbol font.
func mapSymbolText(s, family string) (string, bool) {
	var table map[rune]rune
	switch strings.ToLower(family) {
	case "symbol":
		table = symbolChars
	case "wingdings", "wingdings 2", "wingdings 3":
		table = wingdingsChars
	}
	mapped := false
	out := []rune(s)
	for i, r := range out {
		b := r
		if r >= 0xF020 && r <= 0xF0FF {
			b = r - 0xF000
		}
		if table != nil {
			if u, ok := table[b]; ok {
				out[i], mapped = u, true
				continue
			}
		}
		if b != r {
			out[i], mapped = b, true
		}
	}
	return string(out), mapped || table != nil
}
