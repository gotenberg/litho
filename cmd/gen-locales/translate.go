package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// keywords maps the letters of a locale's localized format code keywords
// to the SpreadsheetML ones (svl/source/numbers/zforscan.cxx,
// ImpSvNumberformatScan::SetDependentKeywords): a run of one letter is a
// keyword, its length kept.
type keywords struct {
	// letters maps a localized letter to the English one.
	letters map[byte]byte
	// yearA reports a locale that writes the year AA or AAAA, which then
	// writes the day of the week OOO or OOOO.
	yearA bool
	// german reports a locale that names colours in German.
	german bool
}

// The locales whose keywords LibreOffice localizes, by tag, and their
// letters (SetDependentKeywords): German, Dutch, French, Finnish, Italian,
// Danish, Norwegian, Swedish, Portuguese and Spanish of the listed
// countries. Every other locale writes the English keywords.
var (
	germanTags = []string{"de-DE", "de-CH", "de-AT", "de-LU", "de-LI"}
	dutchTags  = []string{"nl-NL", "nl-BE"}
	frenchTags = []string{"fr-FR", "fr-BE", "fr-CA", "fr-CH", "fr-LU", "fr-MC"}
	italianTag = []string{"it-IT", "it-CH"}
	nordicTags = []string{"da-DK", "no-NO", "nb-NO", "nn-NO", "sv-SE", "sv-FI"}
	iberian    = []string{
		"pt-PT", "pt-BR", "es-ES", "es-MX", "es-GT", "es-CR", "es-PA", "es-DO", "es-VE", "es-CO",
		"es-PE", "es-AR", "es-EC", "es-CL", "es-UY", "es-PY", "es-BO", "es-SV", "es-HN", "es-NI", "es-PR",
	}
)

// keywordsFor returns the keyword letters of the locale tag.
func keywordsFor(tag string) keywords {
	kw := keywords{letters: map[byte]byte{}}
	switch {
	case slices.Contains(germanTags, tag):
		kw.letters['T'], kw.letters['J'] = 'D', 'Y'
		kw.german = true
	case slices.Contains(dutchTags, tag):
		kw.letters['J'], kw.letters['U'] = 'Y', 'H'
	case slices.Contains(frenchTags, tag):
		kw.letters['J'] = 'D'
		kw.yearA = true
	case slices.Contains(italianTag, tag):
		kw.letters['G'] = 'D'
		kw.yearA = true
	case tag == "fi-FI":
		kw.letters['P'], kw.letters['K'], kw.letters['V'], kw.letters['T'] = 'D', 'M', 'Y', 'H'
	case slices.Contains(nordicTags, tag):
		kw.letters['T'] = 'H'
	case slices.Contains(iberian, tag):
		kw.yearA = true
	}
	return kw
}

// germanColors maps the German colour keywords to the English ones
// (zforscan.cxx, sGermanColorNames).
var germanColors = map[string]string{
	"FARBE": "COLOR", "SCHWARZ": "BLACK", "BLAU": "BLUE", "GRÜN": "GREEN", "CYAN": "CYAN", "ROT": "RED",
	"MAGENTA": "MAGENTA", "BRAUN": "BROWN", "GRAU": "GREY", "GELB": "YELLOW", "WEISS": "WHITE",
}

// errKeyword reports a keyword numfmt does not read: an era, a quarter, a
// week, a calendar letter.
var errKeyword = errors.New("unsupported keyword")

// translator turns a locale's format code into SpreadsheetML syntax: the
// English keywords, "." the decimal point and "," the thousands separator.
type translator struct {
	kw      keywords
	general string
	decimal string
	group   string
	// time100 separates a second from its fraction.
	time100 string
	// dowSep is what NNNN writes after the day of the week
	// (LongDateDayOfWeekSeparator).
	dowSep string
}

// piece is one token of a section being translated.
type piece struct {
	text string
	// sep is the locale separator a raw character stands for: 'd' decimal,
	// 'g' group, 0 none.
	raw bool
	// date reports a date or time keyword; digit a digit placeholder;
	// second a seconds keyword.
	date, digit, second bool
}

// translate returns code in SpreadsheetML syntax.
func (t translator) translate(code string) (string, error) {
	if t.general != "" {
		if i := strings.Index(strings.ToUpper(code), strings.ToUpper(t.general)); i >= 0 && !strings.EqualFold(t.general, "General") {
			code = code[:i] + "General" + code[i+len(t.general):]
		}
	}
	var out []string
	for _, sec := range splitSections(code) {
		s, err := t.section(sec)
		if err != nil {
			return "", err
		}
		out = append(out, s)
	}
	return strings.Join(out, ";"), nil
}

// splitSections cuts a code at the semicolons outside quotes, brackets and
// escapes.
func splitSections(code string) []string {
	var out []string
	start := 0
	for i := 0; i < len(code); i++ {
		switch code[i] {
		case '"':
			if j := strings.IndexByte(code[i+1:], '"'); j >= 0 {
				i += j + 1
			}
		case '\\', '_', '*':
			i++
		case '[':
			if j := strings.IndexByte(code[i:], ']'); j >= 0 {
				i += j
			}
		case ';':
			out = append(out, code[start:i])
			start = i + 1
		}
	}
	return append(out, code[start:])
}

// section translates one section.
func (t translator) section(src string) (string, error) {
	var ps []piece
	upper := strings.ToUpper(src)
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '"':
			j := strings.IndexByte(src[i+1:], '"')
			if j < 0 {
				return "", errors.New("unclosed quote")
			}
			ps = append(ps, piece{text: src[i : i+j+2]})
			i += j + 2
		case c == '\\' || c == '_' || c == '*':
			_, sz := utf8.DecodeRuneInString(src[i+1:])
			ps = append(ps, piece{text: src[i : i+1+sz]})
			i += 1 + sz
		case c == '[':
			j := strings.IndexByte(src[i:], ']')
			if j < 0 {
				return "", errors.New("unclosed bracket")
			}
			b := src[i+1 : i+j]
			if t.kw.german {
				if en, ok := germanColors[strings.ToUpper(b)]; ok {
					b = en
				}
			}
			if strings.EqualFold(b, "ROT") {
				b = "RED"
			}
			if u := strings.ToUpper(b); u != "" && strings.Trim(u, u[:1]) == "" {
				// An elapsed time, [TT] for [HH] in Swedish.
				if p, err := t.keyword(u[0], len(u), b); err == nil && p.date {
					b = p.text
				}
			}
			ps = append(ps, piece{text: "[" + b + "]", date: isElapsed(b)})
			i += j + 1
		case strings.HasPrefix(upper[i:], "AM/PM"):
			ps = append(ps, piece{text: "AM/PM", date: true})
			i += 5
		case strings.HasPrefix(upper[i:], "A/P"):
			ps = append(ps, piece{text: "A/P", date: true})
			i += 3
		case strings.HasPrefix(upper[i:], "GENERAL"):
			ps = append(ps, piece{text: "General"})
			i += 7
		case c == '0' || c == '#' || c == '?':
			ps = append(ps, piece{text: string(c), digit: true})
			i++
		case isLetter(c):
			n := 1
			for i+n < len(src) && upper[i+n] == upper[i] {
				n++
			}
			p, err := t.keyword(upper[i], n, src[i:i+n])
			if err != nil {
				return "", err
			}
			ps = append(ps, p)
			i += n
		default:
			_, sz := utf8.DecodeRuneInString(src[i:])
			ps = append(ps, piece{text: src[i : i+sz], raw: true})
			i += sz
		}
	}
	date := slices.ContainsFunc(ps, func(p piece) bool { return p.date })
	var b strings.Builder
	afterSecond := false
	for k, p := range ps {
		if !p.raw {
			if p.date {
				afterSecond = p.second
			}
			b.WriteString(p.text)
			continue
		}
		prevDigit := k > 0 && ps[k-1].digit
		nextDigit := k+1 < len(ps) && ps[k+1].digit
		nextZero := k+1 < len(ps) && ps[k+1].text == "0"
		switch {
		case date && (p.text == t.decimal || p.text == t.time100) && afterSecond && nextZero:
			b.WriteByte('.')
		case !date && p.text == t.decimal && (prevDigit || nextDigit):
			b.WriteByte('.')
		case !date && (p.text == t.group || p.text == " " && t.spaceGroup()) && prevDigit && nextDigit:
			b.WriteByte(',')
		case p.text == "." || p.text == ",":
			// A literal the SpreadsheetML syntax would read as a
			// separator.
			if date && !(afterSecond && nextZero && p.text == ".") {
				b.WriteString(p.text)
			} else {
				b.WriteString(`\` + p.text)
			}
		default:
			b.WriteString(p.text)
		}
	}
	return b.String(), nil
}

// spaceGroup reports a group separator that is a no-break space, which a
// space in a code stands for too (svl zformat.cxx, the SvNumberformat
// constructor).
func (t translator) spaceGroup() bool {
	return t.group == "\u00a0" || t.group == "\u202f"
}

// isElapsed reports an elapsed time bracket: [H], [MM], [SS].
func isElapsed(b string) bool {
	u := strings.ToUpper(b)
	return u != "" && (strings.Trim(u, "H") == "" || strings.Trim(u, "M") == "" || strings.Trim(u, "S") == "")
}

// isLetter reports an ASCII letter.
func isLetter(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

// keyword translates a run of n letters c (upper case), raw as written.
func (t translator) keyword(c byte, n int, raw string) (piece, error) {
	if en, ok := t.kw.letters[c]; ok {
		c = en
	} else if t.kw.yearA && c == 'A' && (n == 2 || n == 4) {
		c = 'Y'
	} else if t.kw.yearA && c == 'O' && (n == 3 || n == 4) {
		c = 'D'
	} else if !t.kw.yearA && c == 'A' && (n == 3 || n == 4) {
		c = 'D'
	}
	switch c {
	case 'Y', 'M', 'D', 'H':
		return piece{text: strings.Repeat(string(c), n), date: true}, nil
	case 'S':
		return piece{text: strings.Repeat("S", n), date: true, second: true}, nil
	case 'N':
		switch n {
		case 2:
			return piece{text: "DDD", date: true}, nil
		case 3:
			return piece{text: "DDDD", date: true}, nil
		case 4:
			return piece{text: "DDDD" + quoted(t.dowSep), date: true}, nil
		}
	case 'E':
		// The exponent of a scientific format: E+ E-.
		if n == 1 {
			return piece{text: raw}, nil
		}
	}
	return piece{}, fmt.Errorf("%w: %s", errKeyword, raw)
}

// quoted returns s as a quoted literal, "" for none.
func quoted(s string) string {
	if s == "" {
		return ""
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
