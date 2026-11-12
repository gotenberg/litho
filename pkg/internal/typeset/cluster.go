package typeset

import (
	"bytes"
	"fmt"
	"slices"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/gotenberg/litho/pkg/internal/typography/embed"
)

// maxClusterRunes bounds the characters of a cluster the backend spreads
// over its glyphs: past it the cluster keeps its text on its first glyph.
// Anti-DoS guard, not spec-mandated: the claim below is quadratic in the
// cluster, and a shaped cluster spans a few characters (an Indic conjunct
// with its vowel signs stays under 16).
const maxClusterRunes = 64

// glyphTexts is what the content writer writes for the glyphs of a run: the
// text each glyph's code maps to in the ToUnicode CMap, and the clusters
// whose glyphs do not spell their text in paint order, which it wraps in an
// /ActualText span.
type glyphTexts struct {
	texts []string
	spans []actualSpan
}

// actualSpan is a cluster of glyphs [start, end) that stands for text as a
// whole.
type actualSpan struct {
	start, end int
	text       string
}

// clusterTexts spreads the text of each cluster of glyphs over the glyphs
// that draw it. A cluster is a glyph carrying text and the glyphs after it
// carrying none ([Glyph.Text]).
//
// Each glyph of a cluster of more than one takes the characters of the
// cluster the font's cmap maps to it, so a base letter maps to the letter
// and a combining mark to the mark, and each glyph that no character maps
// to, a contextual form or a ligature's part, takes the next character no
// glyph claimed, in text order. A character no glyph of its own draws,
// whose compatibility decomposition the cluster draws in part (Thai SARA
// AM as NIKHAHIT and SARA AA, a precomposed letter as its base and its
// mark), stands for its decomposition's characters there. A text
// extractor reading glyph by glyph then reads each character where it is
// drawn: one that gives a glyph whose text holds a nonspacing mark no
// advance, as PDF.js does, measures the next letter from the base letter
// rather than from before it, and no word gap opens inside the word.
//
// Every glyph drawn maps to text: ISO 19005-1, 6.3.8, ISO 19005-2,
// 6.2.11.7.2, ISO 14289-1, 7.21.7 and ISO 14289-2, 8.4.5.8 ask a Unicode
// value of every character code a page renders, an ActualText span
// covering it or not. A glyph
// left without a character of its cluster, as a dotted circle a shaper
// draws under a mark with no base or the second part of a Khmer vowel,
// takes the character the font's cmap maps to it, what it draws, and
// U+FFFD, the replacement character, when the cmap maps none; never 0,
// U+FEFF or U+FFFE, which the same clauses refuse.
//
// When the glyphs, in paint order, do not spell the cluster's text (a vowel
// sign drawn before the consonant it follows, a ligature of characters the
// cmap maps elsewhere, a decomposed character, a glyph left without a
// character), the cluster is also marked as a span whose ActualText is the
// cluster's text (ISO 32000-1, 14.9.4), the text a conforming reader
// extracts in place of the glyphs': the cluster's text is read once, in
// order. A cluster past [maxClusterRunes] keeps its text on its first
// glyph, as the model gives it, inside such a span. Glyphs that start a
// run without text stand for no character of the model and take what
// they draw, with no span.
func clusterTexts(f *embed.Font, glyphs []Glyph) glyphTexts {
	out := glyphTexts{texts: make([]string, len(glyphs))}
	for start := 0; start < len(glyphs); {
		end := start + 1
		for end < len(glyphs) && glyphs[end].Text == "" {
			end++
		}
		text := glyphs[start].Text
		switch {
		case text == "":
			for i := start; i < end; i++ {
				out.texts[i] = drawnText(f, glyphs[i].GID)
			}
		case end-start == 1:
			out.texts[start] = text
		default:
			spread, whole := spreadCluster(f, glyphs[start:end], text)
			if spread == nil {
				out.texts[start] = text
				for i := start + 1; i < end; i++ {
					out.texts[i] = drawnText(f, glyphs[i].GID)
				}
			} else {
				copy(out.texts[start:end], spread)
			}
			if !whole {
				out.spans = append(out.spans, actualSpan{start: start, end: end, text: text})
			}
		}
		start = end
	}
	return out
}

// spreadCluster returns the text of each glyph of a cluster standing for
// text, none empty, and whether those texts, joined in paint order, spell
// text with no glyph left without a character. It returns nil past
// [maxClusterRunes].
func spreadCluster(f *embed.Font, glyphs []Glyph, text string) ([]string, bool) {
	runes := []rune(text)
	if len(runes) > maxClusterRunes {
		return nil, false
	}
	runes = drawnParts(f, glyphs, runes)
	claim := make([]int, len(runes)) // the glyph claiming each character, -1 for none
	for k := range claim {
		claim[k] = -1
	}
	claimed := make([]bool, len(glyphs))
	for i, g := range glyphs {
		for k, r := range runes {
			if claim[k] >= 0 {
				continue
			}
			if gid, ok := f.GID(r); ok && gid == g.GID {
				claim[k], claimed[i] = i, true
				break
			}
		}
	}
	// A glyph the cmap maps no character of the cluster to takes the next
	// character no glyph claimed, in text order.
	k := 0
	for i := range glyphs {
		if claimed[i] {
			continue
		}
		for k < len(runes) && claim[k] >= 0 {
			k++
		}
		if k == len(runes) {
			break
		}
		claim[k], claimed[i] = i, true
	}
	texts := make([][]rune, len(glyphs))
	last := 0
	for k, r := range runes {
		i := claim[k]
		if i < 0 {
			// A character left over joins the glyph of the character
			// before it: the glyph that draws it with that one.
			i = last
		}
		texts[i] = append(texts[i], r)
		last = i
	}
	out := make([]string, len(glyphs))
	var joined []rune
	leftover := false
	for i, t := range texts {
		joined = append(joined, t...)
		if len(t) == 0 {
			leftover = true
			out[i] = drawnText(f, glyphs[i].GID)
			continue
		}
		out[i] = string(t)
	}
	// A glyph left without a character maps to what it draws, which the
	// cluster's text does not hold there, so the span's ActualText names
	// the cluster then too.
	return out, string(joined) == text && !leftover
}

// drawnParts returns runes, the characters of a cluster drawn by glyphs,
// with each character no glyph of its own draws, and whose compatibility
// decomposition (UAX #15) holds a character the font's cmap maps to one
// of glyphs, replaced by that decomposition. The result stays within
// [maxClusterRunes]: a decomposition that would pass it is left whole.
func drawnParts(f *embed.Font, glyphs []Glyph, runes []rune) []rune {
	drawn := func(r rune) bool {
		gid, ok := f.GID(r)
		if !ok {
			return false
		}
		for _, g := range glyphs {
			if g.GID == gid {
				return true
			}
		}
		return false
	}
	var out []rune // nil until a character expands
	n := len(runes)
	var enc [utf8.UTFMax]byte
	for k, r := range runes {
		var parts []rune
		// A character in NFKD form has no decomposition: testing that on
		// its bytes spares a string for every character of the cluster.
		if r >= utf8.RuneSelf && !norm.NFKD.IsNormal(enc[:utf8.EncodeRune(enc[:], r)]) && !drawn(r) {
			parts = []rune(norm.NFKD.String(string(r)))
		}
		if len(parts) < 2 || n+len(parts)-1 > maxClusterRunes || !slices.ContainsFunc(parts, drawn) {
			if out != nil {
				out = append(out, r)
			}
			continue
		}
		if out == nil {
			out = append(make([]rune, 0, maxClusterRunes), runes[:k]...)
		}
		out = append(out, parts...)
		n += len(parts) - 1
	}
	if out == nil {
		return runes
	}
	return out
}

// drawnText returns the text a glyph left without a character of the
// model maps to: the character the font's cmap maps to it, or U+FFFD, the
// replacement character, when it maps none or one ISO 19005-2,
// 6.2.11.7.2 refuses in a ToUnicode CMap (0, U+FEFF, U+FFFE).
func drawnText(f *embed.Font, gid uint16) string {
	r, ok := f.Rune(gid)
	if !ok || r <= 0 || r == 0xFEFF || r == 0xFFFE || !utf8.ValidRune(r) {
		return string(utf8.RuneError)
	}
	return string(r)
}

// writeActualText opens a marked-content span whose ActualText is text
// (ISO 32000-1, 14.9.4), as a hex string of UTF-16BE behind its byte order
// mark, the text-string form that needs no escaping (7.9.2.2).
func writeActualText(buf *bytes.Buffer, text string) {
	buf.WriteString("/Span <</ActualText <FEFF")
	for _, u := range utf16.Encode([]rune(text)) {
		fmt.Fprintf(buf, "%04X", u)
	}
	buf.WriteString(">>> BDC\n")
}
