package text

import "unicode"

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
	return r == '️' || unicode.Is(pictographic, r)
}
