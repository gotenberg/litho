package layout

import (
	"strings"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// tailoredBreak reports a soft wrap opportunity before character i of p
// that the line-break property opens where UAX #14, as the text stack
// follows it, finds none (CSS Text 3, 5.3):
//
//   - normal and auto, Chromium's default, and loose break before the
//     small kana and the prolonged sound mark (UAX #14 class CJ resolved
//     to ID, LB1, which the text stack already does), and, in Chinese or
//     Japanese text, before the hyphens U+2010, U+2013, U+301C and U+30A0;
//   - loose also breaks before the iteration marks (々, 〻, ゝ, ゞ, ヽ, ヾ),
//     the inseparable characters (‥, …), the centred punctuation (・, ：,
//     ；, U+203C, U+2047 to U+2049, ！, ？), before a postfix after an
//     ideograph (%, ¢, °, ℃...) and after a prefix before one ($, £, ¥,
//     ₩...).
//
// A break never follows an opening bracket or quote, nor touches a space,
// which UAX #14 already handles.
func (p *ifcPara) tailoredBreak(i int) bool {
	lb := p.styleAt(i).LineBreak
	if lb == css.LineBreakStrict || lb == css.LineBreakAnywhere {
		return false
	}
	a, b := p.p.Rune(i-1), p.p.Rune(i)
	if unicode.IsSpace(a) || unicode.IsSpace(b) || unicode.Is(unicode.Ps, a) || unicode.Is(unicode.Pi, a) || !p.p.GraphemeStart(i) {
		return false
	}
	if isConditionalJapaneseStarter(b) {
		return true
	}
	if isCJHyphen(b) && isChineseOrJapanese(p.infos[p.p.RunOf(i)].box.lang) {
		return true
	}
	if lb != css.LineBreakLoose {
		return false
	}
	switch {
	case strings.ContainsRune("々〻ゝゞヽヾ‥…・：；‼⁇⁈⁉！？", b):
		return true
	case strings.ContainsRune("%¢°‰′″℃℉€", b) && isCJKIdeographOrSymbol(a):
		return true
	case strings.ContainsRune("$£¥₩¤№", a) && isCJKIdeographOrSymbol(b):
		return true
	}
	return false
}

// strictBreak reports an opportunity before character i of p that
// line-break: strict removes: before a small kana or a prolonged sound
// mark, which UAX #14 resolves to NS then (class CJ, LB1), the text stack
// resolving it to ID as normal does.
func (p *ifcPara) strictBreak(i int) bool {
	return p.styleAt(i).LineBreak == css.LineBreakStrict && isConditionalJapaneseStarter(p.p.Rune(i))
}

// isConditionalJapaneseStarter reports a character of UAX #14 class CJ:
// the small hiragana and katakana and the prolonged sound marks.
func isConditionalJapaneseStarter(r rune) bool {
	switch {
	case strings.ContainsRune("ぁぃぅぇぉっゃゅょゎゕゖァィゥェォッャュョヮヵヶーｧｨｩｪｫｬｭｮｯｰ", r),
		r >= 0x31F0 && r <= 0x31FF: // katakana phonetic extensions
		return true
	}
	return false
}

// isCJHyphen reports the hyphens CSS Text 3, 5.3 lets normal and loose
// break before in Chinese and Japanese text.
func isCJHyphen(r rune) bool {
	return r == '‐' || r == '–' || r == '〜' || r == '゠'
}

// isChineseOrJapanese reports a language tag of Chinese or Japanese.
func isChineseOrJapanese(lang string) bool {
	l := strings.ToLower(lang)
	return l == "ja" || l == "zh" || strings.HasPrefix(l, "ja-") || strings.HasPrefix(l, "zh-")
}
