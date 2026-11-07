package text

import (
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
func bidiInput(runs []ParagraphRun, text []rune, runOf []int) ([]rune, []int) {
	out := make([]rune, 0, len(text))
	at := make([]int, len(text))
	for i, c := range text {
		r := -1
		if i < len(runOf) {
			r = runOf[i]
		}
		first := i == 0 || i-1 < len(runOf) && runOf[i-1] != r
		last := i == len(text)-1 || i+1 < len(runOf) && runOf[i+1] != r
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

// resolveLevels resolves the embedding level of each character of the
// paragraph (UAX #9, X1 to I2) whose base direction is base, from the
// bidi text bidiInput built and the index of each character in it.
//
// go-text's implementation of the algorithm reports each character's
// direction, not its level: the explicit levels come from X1 to X8 here,
// and a character whose direction differs from its explicit level's sits
// one level up (I1 and I2); one whose direction agrees sits on it, but for
// a number at an even level after a right-to-left strong character, which
// sits two levels up (W2, W4, W5, W7 and I1).
func resolveLevels(bidiText []rune, at []int, base Direction) []int8 {
	def := gobidi.LeftToRight
	baseLevel := 0
	if base == RightToLeft {
		def, baseLevel = gobidi.RightToLeft, 1
	}
	rtl := make([]bool, len(bidiText))
	for i := range rtl {
		rtl[i] = baseLevel == 1
	}
	var bp gobidi.Paragraph
	res := bp.Segment(bidiText, def)
	for i := range res.NumRuns() {
		r := res.Run(i)
		for k := max(r.Start, 0); k < min(r.End, len(rtl)); k++ {
			rtl[k] = !r.IsLeftToRight()
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
	return out
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
			if c == FSI {
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
	for i := range n {
		if cls[i] != bidi.ET || num[i] {
			continue
		}
		for j := i; j < n && cls[j] == bidi.ET; j++ {
			if j+1 < n && cls[j+1] == bidi.EN {
				num[i] = true
			}
		}
		for j := i; j >= 0 && cls[j] == bidi.ET; j-- {
			if j > 0 && cls[j-1] == bidi.EN {
				num[i] = true
			}
		}
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
