package dictbreak

//go:generate go run ../../../cmd/gen-thai-dictionary -out thai_gen.go

// thaiWords is the Thai word list, nil when the build carries none.
var thaiWords *Dictionary

// The constants of the Thai engine, ICU's (ICU User Guide, Boundary
// Analysis; icu4c/source/common/dictbe.cpp, ThaiBreakEngine).
const (
	// thaiLookahead is how many words in a row are good enough: a word
	// that two more words follow is taken at once.
	thaiLookahead = 3
	// thaiRootCombineThreshold is the length from which a word takes in
	// none of the letters after it that no word matches.
	thaiRootCombineThreshold = 3
	// thaiPrefixCombineThreshold is the length of the start of a word
	// from which letters no word matches stay a word of their own: they
	// read as a misspelt word.
	thaiPrefixCombineThreshold = 3
	// thaiMinWordSpan is the shortest run divided: two words of two
	// letters.
	thaiMinWordSpan = 4
	// thaiPaiyannoi is U+0E2F THAI CHARACTER PAIYANNOI, the abbreviation
	// mark, and thaiMaiyamok U+0E46 THAI CHARACTER MAIYAMOK, the
	// repetition mark: both end the word before them.
	thaiPaiyannoi = 'ฯ'
	thaiMaiyamok  = 'ๆ'
)

// maxCandidates bounds the words tried at one place: the dictionary's
// words that start there, longest first. Complexity guard, not
// spec-mandated: ICU tries as many (POSSIBLE_WORD_LIST_MAX).
const maxCandidates = 20

// IsThai reports a character of the Thai script that breaks as class SA
// (UAX #14): every Thai letter, vowel and mark, the repetition and
// abbreviation marks; not U+0E3F THAI CURRENCY SYMBOL BAHT (PR), U+0E4F
// THAI CHARACTER FONGMAN (AL), the Thai digits (NU), nor U+0E5A ANGKHANKHU
// and U+0E5B KHOMUT (BA), which UAX #14 breaks by its own rules.
func IsThai(r rune) bool {
	return r >= 'ก' && r <= 'ฺ' || r >= 'เ' && r <= '๎'
}

// isThaiMark reports a Thai vowel or tone mark set above or below a
// letter (General_Category Mn): a word never starts on one.
func isThaiMark(r rune) bool {
	return r == 'ั' || r >= 'ิ' && r <= 'ฺ' || r >= '็' && r <= '๎'
}

// endsThaiWord reports a character a word may end on: any but U+0E31
// THAI CHARACTER MAI HAN-AKAT and the vowels written before their
// consonant, U+0E40 to U+0E44.
func endsThaiWord(r rune) bool {
	return IsThai(r) && r != 'ั' && (r < 'เ' || r > 'ไ')
}

// beginsThaiWord reports a character a word may start on: a consonant,
// U+0E01 to U+0E2E, or a vowel written before its consonant.
func beginsThaiWord(r rune) bool {
	return r >= 'ก' && r <= 'ฮ' || r >= 'เ' && r <= 'ไ'
}

// Thai appends to ends the word boundaries inside text, a run of
// characters [IsThai] holds, as offsets from its start in ascending
// order, and returns them: no boundary at 0 nor at len(text). It finds
// none without the Thai word list in the build, or in a run shorter than
// four characters.
func Thai(text []rune, ends []int) []int {
	return divideThai(thaiWords, text, ends)
}

// candidates holds the words of the dictionary that start at one place.
type candidates struct {
	// at is the place, -1 before the first lookup.
	at int
	// lens are the lengths of the words, shortest first; n of them.
	lens [maxCandidates]int
	n    int
	// cur is the word tried, mark the one kept; both index lens.
	cur, mark int
	// reach is how far the longest start of a word reaches from at.
	reach int
}

// lookup finds the words of d that start at text[at:], keeping the last
// lookup at the same place, and selects the longest as tried and kept.
// It returns how many there are.
func (c *candidates) lookup(d *Dictionary, text []rune, at int) int {
	if c.at != at {
		c.at = at
		c.n, c.reach = d.Prefixes(text[at:], c.lens[:])
	}
	c.cur = c.n - 1
	c.mark = c.cur
	return c.n
}

// end returns where the word tried ends.
func (c *candidates) end() int { return c.at + c.lens[c.cur] }

// backUp tries the next shorter word and reports whether there is one.
func (c *candidates) backUp() bool {
	if c.cur > 0 {
		c.cur--
		return true
	}
	return false
}

// divideThai is [Thai] with the dictionary d.
//
// At each place it looks up the words that start there. Of several, it
// keeps the longest that a second word follows, taking at once one that
// a second and a third follow; of none so followed, the longest. After a
// word shorter than [thaiRootCombineThreshold], or at a place no word
// starts, letters that start no word and do not read as the start of
// one are skipped to the next place a word may end before and start at,
// where a word starts, and joined to the word before them, or made a
// word of their own after none. A word takes the marks after it, and an
// abbreviation or repetition mark no word follows.
func divideThai(d *Dictionary, text []rune, ends []int) []int {
	n := len(text)
	if d == nil || n < thaiMinWordSpan {
		return ends
	}
	var words [thaiLookahead]candidates
	for i := range words {
		words[i].at = -1
	}
	slot := func(k int) *candidates { return &words[k%thaiLookahead] }
	found := 0
	first := len(ends)
	for pos := 0; pos < n; {
		length := 0
		w := slot(found)
		switch w.lookup(d, text, pos) {
		case 0:
		case 1:
			length = w.lens[w.mark]
			found++
		default:
			length = bestWord(d, text, words[:], found)
			found++
		}
		if pos+length < n && length < thaiRootCombineThreshold {
			next := slot(found)
			if next.lookup(d, text, pos+length) == 0 && (length == 0 || next.reach < thaiPrefixCombineThreshold) {
				skipped := skipNonWord(d, text, pos+length, slot(found+1))
				if length == 0 {
					found++
				}
				length += skipped
			}
		}
		for pos+length < n && isThaiMark(text[pos+length]) {
			length++
		}
		if pos+length < n && length > 0 && slot(found).lookup(d, text, pos+length) == 0 {
			length += suffixLength(text, pos+length)
		}
		if length == 0 {
			// Unreachable: a place no word starts skips a letter at least.
			length = 1
		}
		pos += length
		ends = append(ends, pos)
	}
	if len(ends) > first && ends[len(ends)-1] >= n {
		ends = ends[:len(ends)-1]
	}
	return ends
}

// bestWord returns the length of the word kept at the place
// words[found] was looked up at, among its several words: the longest a
// second word follows, at once one a third follows too, else the
// longest.
func bestWord(d *Dictionary, text []rune, words []candidates, found int) int {
	n := len(text)
	w := &words[found%thaiLookahead]
	second := &words[(found+1)%thaiLookahead]
	third := &words[(found+2)%thaiLookahead]
	if w.end() < n {
	search:
		for {
			if second.lookup(d, text, w.end()) > 0 {
				w.mark = w.cur
				if second.end() >= n {
					break search
				}
				for {
					if third.lookup(d, text, second.end()) > 0 {
						w.mark = w.cur
						break search
					}
					if !second.backUp() {
						break
					}
				}
			}
			if !w.backUp() {
				break
			}
		}
	}
	return w.lens[w.mark]
}

// skipNonWord returns how many letters from at belong to no word: up to
// the first place, before the end, between a letter a word may end on
// and one a word may start on, where a word of d starts, or to the end.
// It looks up with c.
func skipNonWord(d *Dictionary, text []rune, at int, c *candidates) int {
	n := len(text)
	chars := 0
	prev := text[at]
	for {
		chars++
		if at+chars >= n {
			return chars
		}
		r := text[at+chars]
		if endsThaiWord(prev) && beginsThaiWord(r) && c.lookup(d, text, at+chars) > 0 {
			return chars
		}
		prev = r
	}
}

// suffixLength returns how many of the characters at text[at:] join the
// word before them as its suffix: an abbreviation mark the word does not
// end on another such mark, then a repetition mark the word does not end
// on one.
func suffixLength(text []rune, at int) int {
	k := 0
	if text[at] == thaiPaiyannoi && text[at-1] != thaiPaiyannoi && text[at-1] != thaiMaiyamok {
		k++
	}
	if at+k < len(text) && text[at+k] == thaiMaiyamok && text[at+k-1] != thaiMaiyamok {
		k++
	}
	return k
}
