package docx

import (
	"strconv"
	"strings"
)

// runeRange returns the characters from lo to hi inclusive, each a string.
func runeRange(lo, hi rune) []string {
	out := make([]string, 0, hi-lo+1)
	for r := lo; r <= hi; r++ {
		out = append(out, string(r))
	}
	return out
}

// runes returns each of rs as a string.
func runes(rs ...rune) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

// repeatingSets are the alphabets of the ST_NumberFormat formats (ECMA-376
// Part 1, 17.18.59) that write the nth character of their set and, past
// the set's end, the same character once more for each pass through it,
// as lowerLetter does: the sets are the clause's, character for character.
//
// arabicAbjad's 27th and 28th characters are ظ (U+0638) and غ (U+063A),
// as the clause's example lists them, "…, ظ, غ, أأ", and Word prints
// them; the clause's character list swaps the two.
//
// # Corpus evidence
//
//   - litho/docx: numbering-formats prints in Word aiueo, aiueoFullWidth,
//     arabicAlpha, arabicAbjad, chosung, ganada, hindiVowels,
//     hindiConsonants and thaiLetters as these sets at 1 to 3, and
//     arabicAbjad at 27 to 29 as ظ, غ and أأ.
var repeatingSets = map[string][]string{
	"aiueo":          append(runeRange(0xFF71, 0xFF9C), runes(0xFF66, 0xFF9D)...),
	"aiueoFullWidth": runes(0x30A2, 0x30A4, 0x30A6, 0x30A8, 0x30AA, 0x30AB, 0x30AD, 0x30AF, 0x30B1, 0x30B3, 0x30B5, 0x30B7, 0x30B9, 0x30BB, 0x30BD, 0x30BF, 0x30C1, 0x30C4, 0x30C6, 0x30C8, 0x30CA, 0x30CB, 0x30CC, 0x30CD, 0x30CE, 0x30CF, 0x30D2, 0x30D5, 0x30D8, 0x30DB, 0x30DE, 0x30DF, 0x30E0, 0x30E1, 0x30E2, 0x30E4, 0x30E6, 0x30E8, 0x30E9, 0x30EA, 0x30EB, 0x30EC, 0x30ED, 0x30EF, 0x30F2, 0x30F3),
	"arabicAbjad":    runes(0x0623, 0x0628, 0x062C, 0x062F, 0x0647, 0x0648, 0x0632, 0x062D, 0x0637, 0x064A, 0x0643, 0x0644, 0x0645, 0x0646, 0x0633, 0x0639, 0x0641, 0x0635, 0x0642, 0x0631, 0x0634, 0x062A, 0x062B, 0x062E, 0x0630, 0x0636, 0x0638, 0x063A),
	"arabicAlpha":    runes(0x0623, 0x0628, 0x062A, 0x062B, 0x062C, 0x062D, 0x062E, 0x062F, 0x0630, 0x0631, 0x0632, 0x0633, 0x0634, 0x0635, 0x0636, 0x0637, 0x0638, 0x0639, 0x063A, 0x0641, 0x0642, 0x0643, 0x0644, 0x0645, 0x0646, 0x0647, 0x0648, 0x064A),
	"chicago":        runes('*', 0x2020, 0x2021, 0x00A7),
	"chosung":        runes(0x3131, 0x3134, 0x3137, 0x3139, 0x3141, 0x3142, 0x3145, 0x3147, 0x3148, 0x314A, 0x314B, 0x314C, 0x314D, 0x314E),
	"ganada":         runes(0xAC00, 0xB098, 0xB2E4, 0xB77C, 0xB9C8, 0xBC14, 0xC0AC, 0xC544, 0xC790, 0xCC28, 0xCE74, 0xD0C0, 0xD30C, 0xD558),
	"hindiConsonants": append(runeRange(0x0905, 0x0914),
		"अं", "अः"),
	"hindiVowels": runeRange(0x0915, 0x0939),
	"thaiLetters": append(append(append(runes(0x0E01, 0x0E02, 0x0E04), runeRange(0x0E07, 0x0E23)...), runes(0x0E25)...), runeRange(0x0E27, 0x0E2E)...),
}

// cyclingSets are the alphabets of the iroha formats, whose nth
// character starts the set again past its end, without repeating
// (17.18.59 iroha and irohaFullWidth).
//
// # Corpus evidence
//
//   - litho/docx: numbering-formats prints in Word iroha as ｲ, ﾛ, ﾊ and
//     irohaFullWidth as イ, ロ, ハ.
var cyclingSets = map[string][]string{
	"iroha":          runes(0xFF72, 0xFF9B, 0xFF8A, 0xFF86, 0xFF8E, 0xFF8D, 0xFF84, 0xFF81, 0xFF98, 0xFF87, 0xFF99, 0xFF66, 0xFF9C, 0xFF76, 0xFF96, 0xFF80, 0xFF9A, 0xFF7F, 0xFF82, 0xFF88, 0xFF85, 0xFF97, 0xFF91, 0xFF73, 0x30F0, 0xFF89, 0xFF75, 0xFF78, 0xFF94, 0xFF8F, 0xFF79, 0xFF8C, 0xFF7A, 0xFF74, 0xFF83, 0xFF71, 0xFF7B, 0xFF77, 0xFF95, 0xFF92, 0xFF90, 0xFF7C, 0x30F1, 0xFF8B, 0xFF93, 0xFF7E, 0xFF7D, 0xFF9D),
	"irohaFullWidth": runes(0x30A4, 0x30ED, 0x30CF, 0x30CB, 0x30DB, 0x30D8, 0x30C8, 0x30C1, 0x30EA, 0x30CC, 0x30EB, 0x30F2, 0x30EF, 0x30AB, 0x30E8, 0x30BF, 0x30EC, 0x30BD, 0x30C4, 0x30CD, 0x30CA, 0x30E9, 0x30E0, 0x30A6, 0x30F0, 0x30CE, 0x30AA, 0x30AF, 0x30E4, 0x30DE, 0x30B1, 0x30D5, 0x30B3, 0x30A8, 0x30C6, 0x30A2, 0x30B5, 0x30AD, 0x30E6, 0x30E1, 0x30DF, 0x30B7, 0x30F1, 0x30D2, 0x30E2, 0x30BB, 0x30B9, 0x30F3),
}

// digitSets are the digits 0 to 9 of the positional ST_NumberFormat
// formats (17.18.59) that write n's decimal digits each as its character.
//
// # Corpus evidence
//
//   - litho/docx: numbering-formats prints in Word decimalFullWidth as １,
//     ２, ３, hindiNumbers as १, २, ३ and thaiNumbers as ๑, ๒, ๓.
var digitSets = map[string][10]string{
	"decimalFullWidth":           [10]string(runeRange(0xFF10, 0xFF19)),
	"hindiNumbers":               [10]string(runeRange(0x0966, 0x096F)),
	"thaiNumbers":                [10]string(runeRange(0x0E50, 0x0E59)),
	"ideographDigital":           ideographDigits('〇'),
	"japaneseDigitalTenThousand": ideographDigits('〇'),
	"taiwaneseDigital":           ideographDigits('○'),
}

// ideographDigits returns the ideographic digits one to nine after the
// zero sign zero.
func ideographDigits(zero rune) [10]string {
	return [10]string(runes(zero, 0x4E00, 0x4E8C, 0x4E09, 0x56DB, 0x4E94, 0x516D, 0x4E03, 0x516B, 0x4E5D))
}

// alphabetic writes n as the nth string of set, repeated once more for
// each time n passes the set's end when repeat, else the set started
// again; "" for n below 1 or past [maxLetterRepeat] repetitions.
func alphabetic(n int, set []string, repeat bool) string {
	if n < 1 || len(set) == 0 {
		return ""
	}
	i, times := (n-1)%len(set), (n-1)/len(set)+1
	if !repeat {
		return set[i]
	}
	if times > maxLetterRepeat {
		return ""
	}
	return strings.Repeat(set[i], times)
}

// maxLetterRepeat bounds the repetitions of an alphabetic label: 30.
// Anti-DoS guard, not spec-mandated: a list item's number comes from the
// document, and a label thirty letters long already numbers past the
// eight hundredth item of the shortest set used for lists.
const maxLetterRepeat = 30

// hebrewNumeral writes 1 to 9999 in hebrew1 (17.18.59): the thousands
// and hundreds letters, then 15 and 16 as טו and טז, else the tens and
// ones letters; "" outside.
//
// # Corpus evidence
//
//   - litho/docx: numbering-formats prints in Word hebrew1 at 14 to 16 as
//     יד, טו and טז.
func hebrewNumeral(n int) string {
	if n < 1 || n > 9999 {
		return ""
	}
	ones := runes(0x05D0, 0x05D1, 0x05D2, 0x05D3, 0x05D4, 0x05D5, 0x05D6, 0x05D7, 0x05D8)
	tens := runes(0x05D9, 0x05DB, 0x05DC, 0x05DE, 0x05E0, 0x05E1, 0x05E2, 0x05E4, 0x05E6)
	hundreds := runes(0x05E7, 0x05E8, 0x05E9, 0x05EA, 0x05DA, 0x05DD, 0x05DF, 0x05E3, 0x05E5)
	var b strings.Builder
	if d := n / 1000; d > 0 {
		b.WriteString(ones[d-1])
	}
	if d := n / 100 % 10; d > 0 {
		b.WriteString(hundreds[d-1])
	}
	switch r := n % 100; r {
	case 15:
		b.WriteString("טו")
	case 16:
		b.WriteString("טז")
	default:
		if d := r / 10; d > 0 {
			b.WriteString(tens[d-1])
		}
		if d := r % 10; d > 0 {
			b.WriteString(ones[d-1])
		}
	}
	return b.String()
}

// hebrewAlphabet writes n in hebrew2 (17.18.59): the nth of the 22
// letters, followed by ת once for each pass through them.
func hebrewAlphabet(n int) string {
	letters := append(runeRange(0x05D0, 0x05D9), runes(0x05DB, 0x05DC, 0x05DE, 0x05E0, 0x05E1, 0x05E2, 0x05E4, 0x05E6, 0x05E7, 0x05E8, 0x05E9, 0x05EA)...)
	if n < 1 || (n-1)/len(letters) > maxLetterRepeat {
		return ""
	}
	return letters[(n-1)%len(letters)] + strings.Repeat("ת", (n-1)/len(letters))
}

// japaneseLegal writes 1 to 99,999 in japaneseLegal (17.18.59): each
// non-zero digit followed by its unit, 萬, 阡, 百 and 拾, the ones last;
// "" outside.
//
// # Corpus evidence
//
//   - litho/docx: numbering-formats prints in Word japaneseLegal as 壱, 弐,
//     参.
func japaneseLegal(n int) string {
	if n < 1 || n > 99_999 {
		return ""
	}
	digits := runes(0x58F1, 0x5F10, 0x53C2, 0x56DB, 0x4F0D, 0x516D, 0x4E03, 0x516B, 0x4E5D)
	units := []string{"萬", "阡", "百", "拾", ""}
	var b strings.Builder
	for i, p := range []int{10000, 1000, 100, 10, 1} {
		if d := n / p % 10; d > 0 {
			b.WriteString(digits[d-1])
			b.WriteString(units[i])
		}
	}
	return b.String()
}

// chineseCounting writes n in chineseCounting and taiwaneseCounting
// (17.18.59): below 100 with 十 for the tens, from 100 each decimal digit
// as its character, ○ (U+25CB) for zero.
//
// # Corpus evidence
//
//   - litho/docx: numbering-formats prints in Word chineseCounting at 99
//     to 101 as 九十九, 一○○ and 一○一.
func chineseCounting(n int) string {
	if n < 1 {
		return ""
	}
	digits := ideographDigits('○')
	if n >= 100 {
		return digitByDigit(n, digits)
	}
	var b strings.Builder
	if t := n / 10; t > 0 {
		if t > 1 {
			b.WriteString(digits[t])
		}
		b.WriteString("十")
	}
	if o := n % 10; o > 0 {
		b.WriteString(digits[o])
	}
	return b.String()
}

// scriptFormat writes n in the ST_NumberFormat format of 17.18.59 that is
// neither Latin nor East Asian counting, and reports whether format is
// one; "" when the format has no form for n, which then prints in
// decimal digits.
//
// numberInDash writes the number between two hyphen-minus signs, -1-, as
// Word prints it; the clause's example sets them apart with spaces.
//
// # Corpus evidence
//
//   - litho/docx: numbering-formats prints in Word numberInDash as -1-,
//     -2-, -3- and decimalHalfWidth as 1, 2, 3.
func scriptFormat(n int, format string) (string, bool) {
	if set, ok := repeatingSets[format]; ok {
		return alphabetic(n, set, true), true
	}
	if set, ok := cyclingSets[format]; ok {
		return alphabetic(n, set, false), true
	}
	if digits, ok := digitSets[format]; ok {
		return digitByDigit(n, digits), true
	}
	switch format {
	case "decimalHalfWidth":
		return strconv.Itoa(n), true
	case "hex":
		if n < 0 {
			return "", true
		}
		return strings.ToUpper(strconv.FormatInt(int64(n), 16)), true
	case "numberInDash":
		return "-" + strconv.Itoa(n) + "-", true
	case "hebrew1":
		return hebrewNumeral(n), true
	case "hebrew2":
		return hebrewAlphabet(n), true
	case "japaneseLegal":
		return japaneseLegal(n), true
	case "ideographEnclosedCircle":
		if n >= 1 && n <= 10 {
			return string(rune(0x3220 + n - 1)), true
		}
		return "", true
	}
	return "", false
}
