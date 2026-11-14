package text

import "unicode"

//go:generate go run ../../../cmd/gen-emoji-data -out emoji_gen.go

// The characters emoji sequences are built with (UTS #51, 1.4.5 and
// Annex A).
const (
	variationText  = '︎' // VS15, text presentation selector
	variationEmoji = '️' // VS16, emoji presentation selector
	zeroWidthJoin  = '‍'
	keycapMark     = '⃣' // COMBINING ENCLOSING KEYCAP
	tagCancel      = '\U000E007F'
)

// isRegionalIndicator reports whether r is one of the 26 regional
// indicator symbols a flag pairs (UTS #51, ED-14).
func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// isTag reports whether r is a tag character of an emoji tag sequence
// other than its cancel tag (UTS #51, ED-14b).
func isTag(r rune) bool { return r >= 0xE0020 && r <= 0xE007E }

// isKeycapBase reports whether r may start a keycap sequence (UTS #51,
// ED-14c): a digit, # or *.
func isKeycapBase(r rune) bool { return r >= '0' && r <= '9' || r == '#' || r == '*' }

// pictographic approximates the Extended_Pictographic property of Unicode
// Technical Standard #51 (Unicode Emoji, Annex A, emoji-data.txt) by the
// blocks that hold its characters: Miscellaneous Technical's pictographs,
// Miscellaneous Symbols, Dingbats, the arrows and shapes of Miscellaneous
// Symbols and Arrows, and the supplementary blocks from Mahjong Tiles to
// Symbols and Pictographs Extended-A, with the regional indicators that
// make flags. The approximation decides only what a character no face
// covers is drawn as, so a non-emoji character it takes in is drawn blank
// instead of as a box, and reported once with the emoji.
var pictographic = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x231A, Hi: 0x231B, Stride: 1},
		{Lo: 0x23E9, Hi: 0x23FA, Stride: 1},
		{Lo: 0x2600, Hi: 0x27BF, Stride: 1},
		{Lo: 0x2B05, Hi: 0x2B55, Stride: 1},
	},
	R32: []unicode.Range32{
		{Lo: 0x1F000, Hi: 0x1FAFF, Stride: 1},
	},
}

// isEmoji reports whether r is a pictographic character, an emoji
// presentation selector (U+FE0F) or an emoji modifier, which a colour emoji
// font draws and text faces mostly lack.
func isEmoji(r rune) bool {
	return r == variationEmoji || unicode.Is(pictographic, r)
}

// EmojiVariant is the font-variant-emoji value of a run (CSS Fonts 4,
// 6.12): which presentation an emoji character takes when no variation
// selector follows it.
type EmojiVariant uint8

// The font-variant-emoji values.
const (
	// EmojiNormal leaves the presentation to the user agent: a character
	// whose Emoji_Presentation property is Yes prefers a colour face where
	// the family list names none that covers it.
	EmojiNormal EmojiVariant = iota
	// EmojiText asks for text presentation.
	EmojiText
	// EmojiEmoji asks for emoji presentation of every Emoji character.
	EmojiEmoji
	// EmojiUnicode asks for the presentation the Emoji_Presentation
	// property gives.
	EmojiUnicode
)

// presentation is how an emoji sequence is drawn, which decides the face
// that draws it.
type presentation uint8

const (
	// presentNone is a sequence with no presentation of its own: its
	// face is picked as any character's.
	presentNone presentation = iota
	// presentDefaultEmoji is a sequence drawn as emoji by default: the
	// family list's faces come first, as for any character, then the
	// colour faces before the other installed faces.
	presentDefaultEmoji
	// presentEmoji is a sequence asked to draw as emoji: a face of the
	// family list draws it only if it is a colour face or maps the
	// sequence to an emoji glyph, and the colour faces come first.
	presentEmoji
	// presentText is a sequence asked to draw as text: a colour face draws
	// it only if no other face covers it or it maps the sequence to a
	// text glyph.
	presentText
)

// emojiSeq is one emoji sequence of a paragraph: its runes [start, end)
// and its presentation.
type emojiSeq struct {
	start, end int
	pres       presentation
}

// emojiSequences returns the emoji sequences of para, in order, each a
// single emoji character, a presentation sequence, a modifier sequence, a
// flag, a keycap or a tag sequence, or a ZWJ sequence of them (UTS #51,
// ED-14 to ED-17), with the presentation variant gives the run of each
// rune. A sequence whose presentation is none is left out, and so is one
// plain reports: a single character asked to draw as text, without a
// selector, that its run draws so already (see [layoutState.plainText]);
// plain may be nil. The sequences are appended to buf, which the caller
// may reuse from one paragraph to the next.
//
// The presentation follows CSS Fonts 4, 6.12: a variation selector wins
// (VS16 emoji, VS15 text); else font-variant-emoji emoji takes emoji for
// every Emoji character, text takes text, unicode takes the
// Emoji_Presentation property; normal draws an Emoji_Presentation
// character as emoji by default. A keycap without VS16 is text unless
// font-variant-emoji asks for emoji; a flag, a modifier sequence, a tag
// sequence and a ZWJ sequence take the presentation of their first
// character, emoji by default.
func emojiSequences(para []rune, variant func(i int) EmojiVariant, plain func(i int) bool, buf []emojiSeq) []emojiSeq {
	seqs := buf
	for i := 0; i < len(para); {
		end, vs, kind := emojiElement(para, i)
		if kind == elementNone {
			i++
			continue
		}
		multi := kind != elementSingle
		for end+1 < len(para) && para[end] == zeroWidthJoin {
			next, _, k := emojiElement(para, end+1)
			if k == elementNone {
				break
			}
			end, multi = next, true
		}
		p := presentationOf(para[i], vs, kind, multi, variant(i))
		if p == presentText && end == i+1 && plain != nil && plain(i) {
			p = presentNone
		}
		if p != presentNone {
			seqs = append(seqs, emojiSeq{start: i, end: end, pres: p})
		}
		i = end
	}
	return seqs
}

// The kinds of emoji element [emojiElement] reads.
const (
	elementNone = iota
	// elementSingle is one character with its selector, if any.
	elementSingle
	// elementKeycap is a keycap sequence.
	elementKeycap
	// elementCompound is a flag, or a character with modifiers or tags.
	elementCompound
)

// emojiElement reads the emoji element starting at para[i]: its end, the
// variation selector after its first character (0 for none), and its
// kind, elementNone when para[i] starts none.
func emojiElement(para []rune, i int) (int, rune, int) {
	r := para[i]
	n := len(para)
	j := i + 1
	if isRegionalIndicator(r) {
		if j < n && isRegionalIndicator(para[j]) {
			return j + 1, 0, elementCompound
		}
		return j, 0, elementSingle
	}
	if !unicode.Is(emojiChars, r) && !unicode.Is(extendedPictographic, r) {
		return i, 0, elementNone
	}
	var vs rune
	if j < n && (para[j] == variationEmoji || para[j] == variationText) {
		vs = para[j]
		j++
	}
	if isKeycapBase(r) {
		if j < n && para[j] == keycapMark {
			return j + 1, vs, elementKeycap
		}
		return j, vs, elementSingle
	}
	kind := elementSingle
	for j < n && unicode.Is(emojiModifier, para[j]) {
		j++
		kind = elementCompound
	}
	if j < n && isTag(para[j]) {
		k := j
		for k < n && isTag(para[k]) {
			k++
		}
		if k < n && para[k] == tagCancel {
			j, kind = k+1, elementCompound
		}
	}
	return j, vs, kind
}

// presentationOf returns the presentation of a sequence starting with r,
// whose first character carries the variation selector vs (0 for none),
// of element kind, joined with others when multi, in a run of
// font-variant-emoji v.
func presentationOf(r, vs rune, kind int, multi bool, v EmojiVariant) presentation {
	switch vs {
	case variationEmoji:
		return presentEmoji
	case variationText:
		return presentText
	}
	switch v { //nolint:exhaustive // normal and unicode follow the Emoji_Presentation property below
	case EmojiEmoji:
		return presentEmoji
	case EmojiText:
		return presentText
	}
	byDefault := unicode.Is(emojiPresentation, r) || multi || kind == elementCompound
	if kind == elementKeycap {
		byDefault = false
	}
	switch {
	case v == EmojiUnicode && byDefault:
		return presentEmoji
	case v == EmojiUnicode:
		return presentText
	case byDefault:
		return presentDefaultEmoji
	}
	return presentNone
}
