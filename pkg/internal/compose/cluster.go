package compose

import (
	"bytes"
	"fmt"
	"slices"
	"unicode/utf16"

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
// glyph claimed, in text order. A text extractor reading glyph by glyph
// then reads each character where it is drawn: one that gives a glyph
// whose text holds a nonspacing mark no advance, as PDF.js does, measures
// the next letter from the base letter rather than from before it, and no
// word gap opens inside the word.
//
// When the glyphs, in paint order, do not spell the cluster's text (a vowel
// sign drawn before the consonant it follows, a ligature of characters the
// cmap maps elsewhere, a glyph left without a character), the cluster is
// also marked as a span whose ActualText is the cluster's text (ISO
// 32000-1, 14.9.4), the text a conforming reader extracts in place of the
// glyphs'. A cluster past [maxClusterRunes] keeps its text on its first
// glyph, as the model gives it, inside such a span.
func clusterTexts(f *embed.Font, glyphs []Glyph) glyphTexts {
	out := glyphTexts{texts: make([]string, len(glyphs))}
	for start := 0; start < len(glyphs); {
		end := start + 1
		for end < len(glyphs) && glyphs[end].Text == "" {
			end++
		}
		text := glyphs[start].Text
		out.texts[start] = text
		if end-start > 1 && text != "" {
			if spread, whole := spreadCluster(f, glyphs[start:end], text); spread != nil {
				copy(out.texts[start:end], spread)
				if !whole {
					out.spans = append(out.spans, actualSpan{start: start, end: end, text: text})
				}
			} else {
				out.spans = append(out.spans, actualSpan{start: start, end: end, text: text})
			}
		}
		start = end
	}
	return out
}

// spreadCluster returns the text of each glyph of a cluster standing for
// text, and whether those texts, joined in paint order, spell text with no
// glyph left without a character. It returns nil past [maxClusterRunes].
func spreadCluster(f *embed.Font, glyphs []Glyph, text string) ([]string, bool) {
	runes := []rune(text)
	if len(runes) > maxClusterRunes {
		return nil, false
	}
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
	for i, t := range texts {
		out[i] = string(t)
		joined = append(joined, t...)
	}
	// A glyph left without a character maps to nothing a reader can rely
	// on, so the span's ActualText names the cluster then too.
	return out, string(joined) == text && !slices.Contains(out, "")
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
