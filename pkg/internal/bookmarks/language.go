package bookmarks

import (
	"regexp"
	"unicode"
)

// scriptKind classifies a piece of text by dominant script.
type scriptKind int

const (
	scriptLatin scriptKind = iota
	scriptCJK
	scriptCyrillic
	scriptArabic
	scriptDevanagari
	scriptThai
	scriptOther
)

// ScriptName returns a stable string identifier for a [scriptKind] so
// that pkg/pdf (which cannot name [scriptKind] directly) can translate
// a [HeadingCandidate.Script] value into its own public enum.
func ScriptName(k scriptKind) string {
	switch k {
	case scriptLatin:
		return "Latin"
	case scriptCJK:
		return "CJK"
	case scriptCyrillic:
		return "Cyrillic"
	case scriptArabic:
		return "Arabic"
	case scriptDevanagari:
		return "Devanagari"
	case scriptThai:
		return "Thai"
	case scriptOther:
		return "Other"
	}
	return "Other"
}

// detectScript returns the dominant script of the given text by counting
// characters in each Unicode script range. Used to choose language-specific
// heading patterns and adjust the body-size threshold.
func detectScript(text string) scriptKind {
	if text == "" {
		return scriptOther
	}
	var counts [7]int
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r),
			unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
			counts[scriptCJK]++
		case unicode.Is(unicode.Cyrillic, r):
			counts[scriptCyrillic]++
		case unicode.Is(unicode.Arabic, r), unicode.Is(unicode.Hebrew, r):
			counts[scriptArabic]++
		case unicode.Is(unicode.Devanagari, r):
			counts[scriptDevanagari]++
		case unicode.Is(unicode.Thai, r):
			counts[scriptThai]++
		case unicode.Is(unicode.Latin, r):
			counts[scriptLatin]++
		default:
			counts[scriptOther]++
		}
	}
	maxIdx := 0
	for i := 1; i < len(counts); i++ {
		if counts[i] > counts[maxIdx] {
			maxIdx = i
		}
	}
	return scriptKind(maxIdx)
}

// Language-specific heading patterns. These match the START of a line and
// indicate the line is likely a heading regardless of font size.
//
//nolint:gochecknoglobals // immutable lookup tables
var (
	// CJK chapter markers: 第一章, 第十二節, 第3部 etc.
	reCJKChapter = regexp.MustCompile(`^第[一二三四五六七八九十百千万0-9０-９]+[章節部編集卷]`)
	// Korean chapter: 제1장, 제 5 절
	reKoreanChapter = regexp.MustCompile(`^제[\s\x{00a0}]*[0-9０-９]+[\s\x{00a0}]*[장절편부]`)
	// Russian: Глава, Раздел, Часть, Параграф
	reRussianChapter = regexp.MustCompile(`^(?i)(глава|раздел|часть|параграф)[\s\x{00a0}]+\d+`)
	// Arabic: الفصل (chapter), الباب (section)
	reArabicChapter = regexp.MustCompile(`^(الفصل|الباب)[\s\x{00a0}]+`)
)

// matchLanguagePattern returns true if the text matches a language-specific
// heading pattern.
func matchLanguagePattern(text string, script scriptKind) bool {
	switch script {
	case scriptCJK:
		return reCJKChapter.MatchString(text) || reKoreanChapter.MatchString(text)
	case scriptCyrillic:
		return reRussianChapter.MatchString(text)
	case scriptArabic:
		return reArabicChapter.MatchString(text)
	case scriptLatin, scriptDevanagari, scriptThai, scriptOther:
		return false
	}
	return false
}

// scriptFontSizeRatio returns the recommended MinFontSizeRatio override for
// the given script. CJK and similar scripts often use smaller heading-to-body
// ratios than Latin. Returns 0 if no override applies (use default).
func scriptFontSizeRatio(script scriptKind) float64 {
	switch script {
	case scriptCJK:
		return 1.10 // CJK headings often only 10% larger
	case scriptThai, scriptDevanagari:
		return 1.12
	case scriptLatin, scriptCyrillic, scriptArabic, scriptOther:
		return 0
	}
	return 0
}
