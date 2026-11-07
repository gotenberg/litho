package text

import (
	gobidi "github.com/go-text/typesetting/bidi"
)

// The explicit directional formatting characters of UAX #9, 2.
const (
	LRE = '‪'
	RLE = '‫'
	PDF = '‬'
	LRO = '‭'
	RLO = '‮'
	LRI = '⁦'
	RLI = '⁧'
	FSI = '⁨'
	PDI = '⁩'
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

// resolveLevels resolves the embedding level of each character of the
// paragraph (UAX #9, X1 to I2) whose base direction is base, from the
// bidi text bidiInput built and the index of each character in it.
func resolveLevels(bidiText []rune, at []int, base Direction) []int8 {
	def := gobidi.LeftToRight
	baseLevel := int8(0)
	if base == RightToLeft {
		def, baseLevel = gobidi.RightToLeft, 1
	}
	lv := make([]int8, len(bidiText))
	for i := range lv {
		lv[i] = baseLevel
	}
	var bp gobidi.Paragraph
	res := bp.Segment(bidiText, def)
	for i := range res.NumRuns() {
		r := res.Run(i)
		for k := max(r.Start, 0); k < min(r.End, len(lv)); k++ {
			lv[k] = int8(r.Level)
		}
	}
	out := make([]int8, len(at))
	for i, k := range at {
		if k >= 0 && k < len(lv) {
			out[i] = lv[k]
		}
	}
	return out
}
