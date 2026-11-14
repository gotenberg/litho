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
	// emojiTextOnly asks for text presentation whatever selector follows
	// the character: see [Style.emojiVariant]. No CSS value maps to it.
	emojiTextOnly
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
// plain may be nil. A paragraph without a sequence, which is nearly every
// paragraph, allocates nothing.
//
// The presentation follows CSS Fonts 4, 6.12: a variation selector wins
// (VS16 emoji, VS15 text); else font-variant-emoji emoji takes emoji for
// every Emoji character, text takes text, unicode takes the
// Emoji_Presentation property; normal draws an Emoji_Presentation
// character as emoji by default. A keycap without VS16 is text unless
// font-variant-emoji asks for emoji; a flag, a modifier sequence, a tag
// sequence and a ZWJ sequence take the presentation of their first
// character, emoji by default.
func emojiSequences(para []rune, variant func(i int) EmojiVariant, plain func(i int) bool) []emojiSeq {
	var seqs []emojiSeq
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
	if v == emojiTextOnly {
		return presentText
	}
	switch vs {
	case variationEmoji:
		return presentEmoji
	case variationText:
		return presentText
	}
	switch v { //nolint:exhaustive // normal and unicode follow the Emoji_Presentation property below, emojiTextOnly returned above
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

// EmojiJoins reports whether Word and PowerPoint keep a and b, two
// characters next to each other, on one line where an emoji is involved.
// Office reads an emoji as UAX #14 reads a letter, class AL, where UAX
// #14 gives it class ID, EB, EM or RI: an emoji is an
// Extended_Pictographic character (UTS #51, Annex A) or a regional
// indicator, and a skin tone modifier or the emoji presentation selector
// ends one. So no line breaks between two emoji, two flags among them
// (LB28, AL × AL, where LB30a breaks between flags and LB30b and LB31
// between pictographs), between an emoji and a letter or digit either
// way (LB28 and LB23, AL × NU, NU × AL), nor after a closing parenthesis
// or bracket before an emoji (LB30, CP × AL). Han, kana and Hangul (ID,
// H2, H3) keep their break beside an emoji, as beside a letter (LB31).
// A line still breaks next to an emoji at a space, or inside a word no
// line holds. The pairs UAX #14 sets apart for AL and ID that no probe
// shows, an emoji before an opening bracket (LB30, AL × OP) or next to a
// prefix or postfix sign (LB24, LB23a), keep UAX #14's emoji classes.
//
// # Corpus evidence
//
//   - probe, docx: Word sets "Abcdef" and ten 36 point emoji on a line
//     468 points wide as "Abcdef" and seven emoji, then three and " Z",
//     and "A ", ten emoji, "abc" and ten emoji as "A", nine emoji, then
//     an emoji, "abc" and seven emoji, then three and " Z": no break
//     between a letter and an emoji either way, the word broken where the
//     line ends.
//   - probe, pptx: PowerPoint sets "A " and ten 36 point emoji in a box
//     285.6 points wide as "A", five emoji, then five and " Z", where a
//     break between two emoji would have set "A " and five; "Abcdef" and
//     ten emoji as "Abcdef" and three emoji, five, then two and " Z".
//   - probe, docx: Word sets "A ", twenty flags and " Z" as "A", then
//     the flags' forty regional indicators broken where the lines end:
//     no break between two flags. It sets "A (", ten emoji, ")", ten
//     emoji and " Z" as "A", "(" and nine emoji, an emoji, ")" and eight
//     emoji, then two and " Z": no break after "(" nor after ")". It sets
//     "A ", two Han characters, ten emoji, two Han characters and " Z" as
//     "A " and the two Han characters, nine emoji, then an emoji, the two
//     Han characters and " Z": a break between Han and an emoji.
//     PowerPoint was not probed on flags nor brackets.
func EmojiJoins(a, b rune) bool {
	endsEmoji := a == variationEmoji || unicode.Is(extendedPictographic, a) || unicode.Is(emojiModifier, a) || isRegionalIndicator(a)
	if unicode.Is(extendedPictographic, b) || isRegionalIndicator(b) {
		return endsEmoji || officeLetter(a) || closingPunct(a)
	}
	return endsEmoji && officeLetter(b)
}

// closingPunct reports whether r is of line break class CP (UAX #14,
// LineBreak.txt): the right parenthesis and the right square bracket.
func closingPunct(r rune) bool { return r == ')' || r == ']' }

// officeLetter reports whether r is a letter or digit of a script UAX #14
// breaks only at spaces: not Han, kana or Hangul, whose characters break
// between any two (classes ID, H2, H3), next to which an emoji keeps the
// opportunity UAX #14 gives it.
func officeLetter(r rune) bool {
	if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return false
	}
	return !unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}
