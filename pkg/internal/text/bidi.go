package text

import (
	"context"
	"unicode"

	gobidi "github.com/go-text/typesetting/bidi"
	"github.com/go-text/typesetting/di"
	"golang.org/x/text/unicode/bidi"
)

// The explicit directional formatting characters of UAX #9, 2.
const (
	LRE = '\u202a'
	RLE = '\u202b'
	PDF = '\u202c'
	LRO = '\u202d'
	RLO = '\u202e'
	LRI = '\u2066'
	RLI = '\u2067'
	FSI = '\u2068'
	PDI = '\u2069'
)

// maxBidiControls caps the explicit formatting characters one run opens
// or closes: 8. CSS opens at most two per inline box (isolate-override),
// and UAX #9 caps the embedding depth at 125 anyway (BD2). Anti-DoS
// guard, not spec-mandated.
const maxBidiControls = 8

// isOpener reports an explicit formatting character that opens an
// embedding, an override or an isolate.
func isOpener(r rune) bool {
	switch r {
	case LRE, RLE, LRO, RLO, LRI, RLI, FSI:
		return true
	}
	return false
}

// bidiInput returns the text the embedding levels resolve on: text with
// the explicit formatting characters each run opens and closes around its
// characters, an override run's LRO or RLO and PDF among them, and the
// index of every character of text in it.
func bidiInput(runs []ParagraphRun, text []rune, runOf []uint16) ([]rune, []int) {
	out := make([]rune, 0, len(text))
	at := make([]int, len(text))
	for i, c := range text {
		r := -1
		if i < len(runOf) {
			r = int(runOf[i])
		}
		first := i == 0 || i-1 < len(runOf) && int(runOf[i-1]) != r
		last := i == len(text)-1 || i+1 < len(runOf) && int(runOf[i+1]) != r
		if first && r >= 0 && r < len(runs) {
			out = appendControls(out, runs[r].Open, true)
			switch runs[r].Override {
			case LeftToRight:
				out = append(out, LRO)
			case RightToLeft:
				out = append(out, RLO)
			case DirectionAuto:
			}
		}
		at[i] = len(out)
		if r >= 0 && r < len(runs) && runs[r].Object != nil && runs[r].Object.Tab {
			c = '\t'
		}
		out = append(out, c)
		if last && r >= 0 && r < len(runs) {
			if runs[r].Override != DirectionAuto {
				out = append(out, PDF)
			}
			out = appendControls(out, runs[r].Close, false)
		}
	}
	return out, at
}

// appendControls appends the openers, or the closers, of cs to out, at
// most maxBidiControls of them.
func appendControls(out, cs []rune, open bool) []rune {
	for i, c := range cs {
		if i >= maxBidiControls {
			break
		}
		if isOpener(c) == open && (open || c == PDF || c == PDI) {
			out = append(out, c)
		}
	}
	return out
}

// maxDepth is the deepest embedding level of UAX #9 (BD2).
const maxDepth = 125

// maxBidiRunes caps the characters litho hands go-text's bidi algorithm
// in one call. Anti-DoS guard, not spec-mandated: go-text's
// bidi.Paragraph.Segment takes no context and is quadratic on some runs,
// a run of terminators (W5), of European numbers after a right-to-left
// letter (W7) or of numbers and separators, and of bracket pairs (N0). At
// 32,768 characters the worst of them takes under a second, so the time
// grows with the text's length, not its square, and a context check comes
// between two pieces. The value holds the longest text Excel lets a cell
// hold, 32,767 characters, in one piece. A paragraph of no
// right-to-left character, nearly every one, never calls the algorithm
// for its levels.
//
// A longer text is resolved, and itemized, in pieces of at most
// maxBidiRunes characters, each cut after a space of its last
// [bidiCutSlack] characters when it has one: each piece is a paragraph of
// its own to the implicit rules (UAX #9, W1 to I2), so a character next
// to a cut may take another direction than in the whole paragraph, and
// shaping restarts at each cut.
//
// # Corpus evidence
//
//   - probe, txt: a Hebrew letter then 100,000 European digits took 4.5 s
//     in Segment; 80,000 characters of "1," after a Hebrew letter took
//     2.7 s, of "(a)" 0.76 s; 100,000 characters of Hebrew words took
//     8 ms (Apple silicon).
//   - probe, txt: at 32,768 characters the digits took 0.72 s, "1," 0.68 s,
//     "#" 0.28 s, "(a)" 0.23 s on a loaded machine; the levels of 2^20
//     characters of "(a)" after a Hebrew letter took 5.2 s in pieces.
//   - litho/xlsx: text_overflow holds a cell of 32,767 characters, which
//     pieces of 16,384 cut.
const maxBidiRunes = 1 << 15

// bidiCutSlack is how far back from [maxBidiRunes] a cut looks for a
// space.
const bidiCutSlack = 1024

// bidiCut returns the end of the piece of text[start:end] that starts at
// start: end when the rest fits [maxBidiRunes], else just after the last
// space of the piece's last [bidiCutSlack] characters, else before the
// character that starts a grapheme closest to maxBidiRunes.
func bidiCut(text []rune, start, end int) int {
	if end-start <= maxBidiRunes {
		return end
	}
	limit := start + maxBidiRunes
	for i := limit - 1; i >= limit-bidiCutSlack; i-- {
		if unicode.IsSpace(text[i]) {
			return i + 1
		}
	}
	cut := limit
	for cut > start+1 && continuesGrapheme(text[cut]) {
		cut--
	}
	return cut
}

// needsBidi reports whether the paragraph text of base direction base
// may hold a right-to-left level: a right-to-left base, or a character
// of class R, AL or AN, or an explicit right-to-left embedding, override
// or isolate. Without one every character resolves to an even level, left
// to right (UAX #9, X1 to I2), and go-text's algorithm has nothing to say.
func needsBidi(text []rune, base Direction) bool {
	if base == RightToLeft {
		return true
	}
	for _, c := range text {
		switch c {
		case RLE, RLO, RLI:
			return true
		}
		if c < 0x0590 {
			// Below Hebrew no character is of class R, AL or AN.
			continue
		}
		p, _ := bidi.LookupRune(c)
		if cl := p.Class(); cl == bidi.R || cl == bidi.AL || cl == bidi.AN {
			return true
		}
	}
	return false
}

// resolveLevels resolves the embedding level of each character of the
// paragraph (UAX #9, X1 to I2) whose base direction is base, from the
// bidi text bidiInput built and the index of each character in it. ctx's
// error stops it between two pieces of [maxBidiRunes].
//
// go-text's implementation of the algorithm reports each character's
// direction, not its level: the explicit levels come from X1 to X8 here,
// and a character whose direction differs from its explicit level's sits
// one level up (I1 and I2); one whose direction agrees sits on it, but for
// a number at an even level after a right-to-left strong character, which
// sits two levels up (W2, W4, W5, W7 and I1).
func resolveLevels(ctx context.Context, bidiText []rune, at []int, base Direction) ([]int8, error) {
	def := gobidi.LeftToRight
	baseLevel := 0
	if base == RightToLeft {
		def, baseLevel = gobidi.RightToLeft, 1
	}
	rtl := make([]bool, len(bidiText))
	for i := range rtl {
		rtl[i] = baseLevel == 1
	}
	if needsBidi(bidiText, base) {
		var bp gobidi.Paragraph
		for start := 0; start < len(bidiText); {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			end := bidiCut(bidiText, start, len(bidiText))
			res := bp.Segment(bidiText[start:end], def)
			for i := range res.NumRuns() {
				r := res.Run(i)
				for k := max(r.Start, 0); k < min(r.End, end-start); k++ {
					rtl[start+k] = !r.IsLeftToRight()
				}
			}
			start = end
		}
	}
	explicit, overridden := explicitLevels(bidiText, baseLevel)
	numeric := numericRuns(bidiText, explicit)
	out := make([]int8, len(at))
	for i, k := range at {
		if k < 0 || k >= len(bidiText) {
			continue
		}
		e := explicit[k]
		lv := e
		switch {
		case rtl[k] != (e%2 == 1):
			lv = e + 1
		case e%2 == 0 && !overridden[k] && numeric[k]:
			lv = e + 2
		}
		out[i] = int8(min(lv, maxDepth+1)) //nolint:gosec // at most 126
	}
	resetSeparators(bidiText, at, out, baseLevel)
	return out, nil
}

// resetSeparators applies the first rules of UAX #9, L1 to levels, the
// level of each character of bidiText at its index in at: a segment
// separator, a tab among them, takes the paragraph's level baseLevel, and
// so does the white space before it, so that the text on either side of
// it orders as separate segments. The white space at the end of a line,
// which L1 resets too, hangs or is removed as CSS Text 3, 4.1 says.
func resetSeparators(bidiText []rune, at []int, levels []int8, baseLevel int) {
	base := int8(baseLevel) //nolint:gosec // 0 or 1
	for i := len(at) - 1; i >= 0; i-- {
		k := at[i]
		if k < 0 || k >= len(bidiText) || !segmentSeparator(bidiText[k]) {
			continue
		}
		levels[i] = base
		for j := i - 1; j >= 0; j-- {
			if at[j] < 0 || at[j] >= len(bidiText) || !separatorSpace(bidiText[at[j]]) {
				break
			}
			levels[j] = base
		}
	}
}

// segmentSeparator reports a character of bidi class S (UAX #9, Table 4):
// the tab, U+000B and U+001F.
func segmentSeparator(r rune) bool {
	return r == '\t' || r == '\u000b' || r == '\u001f'
}

// separatorSpace reports white space L1 resets before a segment
// separator: a character of class WS, an isolate control or a
// formatting character of class BN.
func separatorSpace(r rune) bool {
	switch r {
	case LRI, RLI, FSI, PDI:
		return true
	}
	p, _ := bidi.LookupRune(r)
	cl := p.Class()
	return cl == bidi.WS || cl == bidi.BN
}

// explicitLevels applies rules X1 to X8 of UAX #9 to text, a paragraph of
// base level base: the embedding level of each character, and whether a
// directional override is in force on it. An isolate's initiator and its
// PDI take the level outside it; the other formatting characters, the
// level they set.
func explicitLevels(text []rune, base int) ([]int, []bool) {
	type entry struct {
		level    int
		override bool
		isolate  bool
	}
	stack := []entry{{level: base}}
	overflowIsolates, overflowEmbeddings, validIsolates := 0, 0, 0
	levels := make([]int, len(text))
	over := make([]bool, len(text))
	for i, c := range text {
		top := stack[len(stack)-1]
		levels[i], over[i] = top.level, top.override
		switch c {
		case RLE, LRE, RLO, LRO, RLI, LRI, FSI:
			isolate := c == RLI || c == LRI || c == FSI
			rtl := c == RLE || c == RLO || c == RLI
			// An overflow counter makes the initiator fail whatever its
			// direction (X5c, X6a), so an FSI resolves its own only when it
			// may open: the scan up to its PDI then runs for at most
			// maxDepth unclosed initiators, not for every one of the text.
			if c == FSI && overflowIsolates == 0 && overflowEmbeddings == 0 {
				rtl = paragraphDirection(isolated(text[i+1:])) == di.DirectionRTL
			}
			if isolate {
				over[i] = false
			}
			l := nextEven(top.level)
			if rtl {
				l = nextOdd(top.level)
			}
			if l <= maxDepth && overflowIsolates == 0 && overflowEmbeddings == 0 {
				if isolate {
					validIsolates++
				}
				stack = append(stack, entry{level: l, override: c == RLO || c == LRO, isolate: isolate})
				if !isolate {
					levels[i] = l
				}
				continue
			}
			if isolate {
				overflowIsolates++
			} else if overflowIsolates == 0 {
				overflowEmbeddings++
			}
		case PDI:
			if overflowIsolates > 0 {
				overflowIsolates--
				continue
			}
			if validIsolates == 0 {
				continue
			}
			overflowEmbeddings = 0
			for len(stack) > 1 && !stack[len(stack)-1].isolate {
				stack = stack[:len(stack)-1]
			}
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			validIsolates--
			levels[i], over[i] = stack[len(stack)-1].level, stack[len(stack)-1].override
		case PDF:
			switch {
			case overflowIsolates > 0:
			case overflowEmbeddings > 0:
				overflowEmbeddings--
			case !top.isolate && len(stack) > 1:
				stack = stack[:len(stack)-1]
			}
		}
	}
	return levels, over
}

// nextEven and nextOdd return the least even and odd levels above l.
func nextEven(l int) int { return l + 2 - l%2 }
func nextOdd(l int) int  { return l + 1 + l%2 }

// isolated returns text up to the PDI that matches an isolate initiator
// just before it, for the direction of an FSI (UAX #9, X5c).
func isolated(text []rune) []rune {
	depth := 0
	for i, c := range text {
		switch c {
		case RLI, LRI, FSI:
			depth++
		case PDI:
			if depth == 0 {
				return text[:i]
			}
			depth--
		}
	}
	return text
}

// numericRuns reports the characters that resolve to a number at the
// level they sit on after a right-to-left strong character, or to an
// Arabic number anywhere (UAX #9, W2, W4, W5 and W7): the European digits
// and the separators and terminators that join them, which then sit two
// levels above an even level (I1).
func numericRuns(text []rune, levels []int) []bool {
	n := len(text)
	cls := make([]bidi.Class, n)
	for i, c := range text {
		p, _ := bidi.LookupRune(c)
		cls[i] = p.Class()
	}
	isNum := func(c bidi.Class) bool { return c == bidi.EN || c == bidi.AN }
	num := make([]bool, n)
	for i := range n {
		num[i] = isNum(cls[i])
	}
	// W4: a single separator between two numbers; W5: terminators next to
	// European numbers.
	for i := 1; i+1 < n; i++ {
		if (cls[i] == bidi.CS || cls[i] == bidi.ES) && num[i-1] && num[i+1] && levels[i-1] == levels[i+1] {
			num[i] = true
		}
	}
	// A sequence of terminators joins the European number on either side
	// of it as a whole, so each sequence is tested once at its ends.
	for i := 0; i < n; {
		if cls[i] != bidi.ET {
			i++
			continue
		}
		j := i
		for j < n && cls[j] == bidi.ET {
			j++
		}
		if (i > 0 && cls[i-1] == bidi.EN) || (j < n && cls[j] == bidi.EN) {
			for k := i; k < j; k++ {
				num[k] = true
			}
		}
		i = j
	}
	out := make([]bool, n)
	strong := -1 // the class of the last strong character at the level
	strongLevel := -1
	for i := range n {
		c := cls[i]
		if levels[i] != strongLevel {
			strong, strongLevel = -1, levels[i]
		}
		switch {
		case c == bidi.L:
			strong = int(bidi.L)
		case c == bidi.R || c == bidi.AL:
			strong = int(bidi.R)
		case num[i]:
			// W2 turns a European number after an Arabic letter into an
			// Arabic number, W7 one after a left-to-right letter into one;
			// an Arabic number stays.
			out[i] = cls[i] == bidi.AN || strong == int(bidi.R)
			if !isNum(c) {
				// A separator or terminator follows its numbers.
				out[i] = i > 0 && out[i-1] || i+1 < n && isNum(cls[i+1]) && (strong == int(bidi.R) || cls[i+1] == bidi.AN)
			}
		}
	}
	return out
}
