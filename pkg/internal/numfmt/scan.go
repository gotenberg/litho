package numfmt

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// ErrSyntax reports a format code Calc refuses: an unclosed quote or
// bracket, an unknown bracket keyword, more than four sections, or a code
// longer than [maxCodeBytes].
var ErrSyntax = errors.New("malformed number format code")

// maxCodeBytes caps a format code. Anti-DoS guard, not spec-mandated:
// ECMA-376 Part 1, 18.8.31 gives no limit, Excel stops at 255 characters,
// and the longest built-in code is under 60.
const maxCodeBytes = 1024

// maxSections is the most sections a code holds: positive, negative,
// zero and text (ECMA-376 Part 1, 18.8.31).
const maxSections = 4

// tokKind is the kind of a format code token.
type tokKind uint8

const (
	tLit     tokKind = iota // literal text
	tDigit                  // 0 # or ?
	tDot                    // decimal point
	tComma                  // thousands separator or scaling comma
	tPercent                // %
	tExp                    // E+ or E-
	tSlash                  // fraction bar
	tDate                   // date or time keyword
	tAMPM                   // AM/PM
	tAP                     // A/P
	tSecFrac                // fraction-of-second digits after s
	tAt                     // @
	tFill                   // *x
	tGeneral                // General
)

// dateKey is a date or time keyword.
type dateKey uint8

const (
	kYear dateKey = iota
	kMonth
	kDay
	kHour
	kMinute
	kSecond
	kElapsedHour
	kElapsedMinute
	kElapsedSecond
)

// token is one piece of a section.
type token struct {
	kind tokKind
	// text is a literal's text, the placeholder character of a digit,
	// the sign of an exponent ('+' or '-'), the repeated character of a
	// fill.
	text string
	// key and n are a date keyword and its letter count; n is the digit
	// count of a fraction of a second.
	key dateKey
	n   int
}

// condOp is a comparison of a section condition.
type condOp uint8

const (
	opNone condOp = iota
	opEQ
	opNE
	opLT
	opLE
	opGT
	opGE
)

// section is one part of a code between semicolons.
type section struct {
	toks  []token
	color *css.Color
	op    condOp
	limit float64
	// explicitCond reports a condition the code wrote.
	explicitCond bool
	kind         sectionKind
	// empty reports a section the code left blank, which shows nothing.
	empty bool
}

// sectionKind says how a section formats a number.
type sectionKind uint8

const (
	secLiteral  sectionKind = iota // literal text only
	secGeneral                     // General with literals
	secNumber                      // digits, maybe scientific
	secFraction                    // a fraction
	secDate                        // date or time keywords
	secText                        // @ with literals
)

// The standard colours of the colour keywords (LibreOffice
// svl/source/numbers/zforscan.cxx, StandardColor: COL_BLACK, COL_LIGHTBLUE,
// COL_LIGHTGREEN, COL_LIGHTCYAN, COL_LIGHTRED, COL_LIGHTMAGENTA, COL_BROWN,
// COL_GRAY, COL_YELLOW, COL_WHITE).
var namedColors = map[string]css.Color{
	"BLACK":   rgb(0x000000),
	"BLUE":    rgb(0x0000FF),
	"GREEN":   rgb(0x00FF00),
	"CYAN":    rgb(0x00FFFF),
	"RED":     rgb(0xFF0000),
	"MAGENTA": rgb(0xFF00FF),
	"BROWN":   rgb(0x808000),
	"GREY":    rgb(0x808080),
	"GRAY":    rgb(0x808080),
	"YELLOW":  rgb(0xFFFF00),
	"WHITE":   rgb(0xFFFFFF),
}

// palette is the default colour palette [Color n] indexes, 1 to 56
// (ECMA-376 Part 1, 18.8.27 indexedColors, entries 8 to 63).
var palette = [56]uint32{
	0x000000, 0xFFFFFF, 0xFF0000, 0x00FF00, 0x0000FF, 0xFFFF00, 0xFF00FF, 0x00FFFF,
	0x800000, 0x008000, 0x000080, 0x808000, 0x800080, 0x008080, 0xC0C0C0, 0x808080,
	0x9999FF, 0x993366, 0xFFFFCC, 0xCCFFFF, 0x660066, 0xFF8080, 0x0066CC, 0xCCCCFF,
	0x000080, 0xFF00FF, 0xFFFF00, 0x00FFFF, 0x800080, 0x800000, 0x008080, 0x0000FF,
	0x00CCFF, 0xCCFFFF, 0xCCFFCC, 0xFFFF99, 0x99CCFF, 0xFF99CC, 0xCC99FF, 0xFFCC99,
	0x3366FF, 0x33CCCC, 0x99CC00, 0xFFCC00, 0xFF9900, 0xFF6600, 0x666699, 0x969696,
	0x003366, 0x339966, 0x003300, 0x333300, 0x993300, 0x993366, 0x333399, 0x333333,
}

func rgb(v uint32) css.Color {
	return css.Color{R: float64(v>>16&0xFF) / 255, G: float64(v>>8&0xFF) / 255, B: float64(v&0xFF) / 255, A: 1}
}

// blankWidths is how many spaces "_x" inserts for each printable ASCII
// character x, from the space on (LibreOffice svl/source/numbers/
// zformat.cxx, cCharWidths); any other character inserts two.
var blankWidths = [95]uint8{
	1, 1, 1, 2, 2, 3, 2, 1, 1, 1, 1, 2, 1, 1, 1, 1,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 2, 2, 2, 2,
	3, 2, 2, 2, 2, 2, 2, 3, 2, 1, 2, 2, 2, 3, 3, 3,
	2, 3, 2, 2, 2, 2, 2, 3, 2, 2, 2, 1, 1, 1, 2, 2,
	1, 2, 2, 2, 2, 2, 1, 2, 2, 1, 1, 2, 1, 3, 2, 2,
	2, 2, 1, 2, 1, 2, 2, 2, 2, 2, 2, 1, 1, 1, 2,
}

// blanks returns the spaces "_x" stands for.
func blanks(r rune) string {
	n := 2
	if r >= 32 && r < 127 {
		n = int(blankWidths[r-32])
	}
	return strings.Repeat(" ", n)
}

// splitSections cuts code at the semicolons outside quotes, brackets and
// escapes.
func splitSections(code string) ([]string, error) {
	var out []string
	start := 0
	for i := 0; i < len(code); i++ {
		switch code[i] {
		case '"':
			j := strings.IndexByte(code[i+1:], '"')
			if j < 0 {
				return nil, fmt.Errorf("%w: unclosed quote", ErrSyntax)
			}
			i += j + 1
		case '\\', '*', '_':
			if i+1 < len(code) {
				_, sz := utf8.DecodeRuneInString(code[i+1:])
				i += sz
			}
		case '[':
			j := strings.IndexByte(code[i+1:], ']')
			if j < 0 {
				return nil, fmt.Errorf("%w: unclosed bracket", ErrSyntax)
			}
			i += j + 1
		case ';':
			out = append(out, code[start:i])
			start = i + 1
		}
	}
	out = append(out, code[start:])
	if len(out) > maxSections {
		return nil, fmt.Errorf("%w: %d sections, at most %d", ErrSyntax, len(out), maxSections)
	}
	return out, nil
}

// scanSection reads one section.
func scanSection(src string) (*section, error) {
	s := &section{}
	if src == "" {
		s.empty = true
		return s, nil
	}
	lit := func(t string) {
		if n := len(s.toks); n > 0 && s.toks[n-1].kind == tLit {
			s.toks[n-1].text += t
			return
		}
		s.toks = append(s.toks, token{kind: tLit, text: t})
	}
	upper := strings.ToUpper(src)
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '"':
			j := strings.IndexByte(src[i+1:], '"')
			if j < 0 {
				return nil, fmt.Errorf("%w: unclosed quote", ErrSyntax)
			}
			lit(src[i+1 : i+1+j])
			i += j + 2
		case c == '\\' || c == '_' || c == '*':
			if i+1 >= len(src) {
				i++
				continue
			}
			r, sz := utf8.DecodeRuneInString(src[i+1:])
			switch c {
			case '\\':
				lit(string(r))
			case '_':
				lit(blanks(r))
			default:
				s.toks = append(s.toks, token{kind: tFill, text: string(r)})
			}
			i += 1 + sz
		case c == '[':
			j := strings.IndexByte(src[i+1:], ']')
			if j < 0 {
				return nil, fmt.Errorf("%w: unclosed bracket", ErrSyntax)
			}
			if err := s.bracket(src[i+1:i+1+j], lit); err != nil {
				return nil, err
			}
			i += j + 2
		case c == '0' || c == '#' || c == '?':
			s.toks = append(s.toks, token{kind: tDigit, text: string(c)})
			i++
		case c == '.':
			s.toks = append(s.toks, token{kind: tDot})
			i++
		case c == ',':
			s.toks = append(s.toks, token{kind: tComma})
			i++
		case c == '%':
			s.toks = append(s.toks, token{kind: tPercent})
			i++
		case c == '/':
			s.toks = append(s.toks, token{kind: tSlash})
			i++
		case c == '@':
			s.toks = append(s.toks, token{kind: tAt})
			i++
		case strings.HasPrefix(upper[i:], "GENERAL"):
			s.toks = append(s.toks, token{kind: tGeneral})
			i += len("GENERAL")
		case strings.HasPrefix(upper[i:], "AM/PM"):
			s.toks = append(s.toks, token{kind: tAMPM})
			i += len("AM/PM")
		case strings.HasPrefix(upper[i:], "A/P"):
			s.toks = append(s.toks, token{kind: tAP})
			i += len("A/P")
		case (c == 'E' || c == 'e') && i+1 < len(src) && (src[i+1] == '+' || src[i+1] == '-'):
			s.toks = append(s.toks, token{kind: tExp, text: src[i+1 : i+2]})
			i += 2
		case strings.ContainsRune("YyMmDdHhSs", rune(c)):
			n := 1
			for i+n < len(src) && upper[i+n] == upper[i] {
				n++
			}
			var k dateKey
			switch upper[i] {
			case 'Y':
				k = kYear
			case 'M':
				k = kMonth
			case 'D':
				k = kDay
			case 'H':
				k = kHour
			default:
				k = kSecond
			}
			s.toks = append(s.toks, token{kind: tDate, key: k, n: n})
			i += n
		default:
			r, sz := utf8.DecodeRuneInString(src[i:])
			lit(string(r))
			i += sz
		}
	}
	s.resolve()
	return s, nil
}

// bracket reads the content of a [...] group: a colour, a condition, a
// locale or currency tag, or an elapsed time keyword.
func (s *section) bracket(b string, lit func(string)) error {
	u := strings.ToUpper(strings.TrimSpace(b))
	switch {
	case u == "":
		return fmt.Errorf("%w: empty bracket", ErrSyntax)
	case strings.HasPrefix(u, "$"):
		// A currency or locale tag: [$symbol-locale] shows the symbol
		// (ECMA-376 Part 1, 18.8.31); the locale is not used.
		sym, _, _ := strings.Cut(b[strings.IndexByte(b, '$')+1:], "-")
		if sym != "" {
			lit(sym)
		}
		return nil
	case u[0] == '<' || u[0] == '>' || u[0] == '=':
		return s.condition(u)
	case strings.HasPrefix(u, "COLOR"):
		n, err := strconv.Atoi(strings.TrimSpace(u[len("COLOR"):]))
		if err != nil || n < 1 || n > len(palette) {
			return fmt.Errorf("%w: colour %q", ErrSyntax, b)
		}
		c := rgb(palette[n-1])
		s.color = &c
		return nil
	case strings.Trim(u, "H") == "" || strings.Trim(u, "M") == "" || strings.Trim(u, "S") == "":
		k := map[byte]dateKey{'H': kElapsedHour, 'M': kElapsedMinute, 'S': kElapsedSecond}[u[0]]
		s.toks = append(s.toks, token{kind: tDate, key: k, n: len(u)})
		return nil
	case u[0] == '~' || strings.HasPrefix(u, "NATNUM") || strings.HasPrefix(u, "DBNUM"):
		return nil
	}
	if c, ok := namedColors[u]; ok {
		s.color = &c
		return nil
	}
	return fmt.Errorf("%w: unknown bracket [%s]", ErrSyntax, b)
}

// condition reads a [op value] condition.
func (s *section) condition(u string) error {
	ops := []struct {
		p  string
		op condOp
	}{{"<=", opLE}, {">=", opGE}, {"<>", opNE}, {"<", opLT}, {">", opGT}, {"=", opEQ}}
	for _, o := range ops {
		if rest, ok := strings.CutPrefix(u, o.p); ok {
			v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
			if err != nil {
				return fmt.Errorf("%w: condition [%s]", ErrSyntax, u)
			}
			s.op, s.limit, s.explicitCond = o.op, v, true
			return nil
		}
	}
	return fmt.Errorf("%w: condition [%s]", ErrSyntax, u)
}

// resolve decides the section's kind and turns the m keywords into
// minutes or months, and the 0 after a second into fraction-of-second
// digits.
func (s *section) resolve() {
	hasDate := false
	for _, t := range s.toks {
		if t.kind == tDate || t.kind == tAMPM || t.kind == tAP {
			hasDate = true
		}
	}
	if hasDate {
		s.kind = secDate
		s.resolveDate()
		return
	}
	// E, H and the other date letters only mean something next to their
	// kind of section: in a number section they are literals.
	digits, slash, at, general := false, false, false, false
	for _, t := range s.toks {
		switch t.kind {
		case tDigit:
			digits = true
		case tSlash:
			slash = true
		case tAt:
			at = true
		case tGeneral:
			general = true
		case tLit, tDot, tComma, tPercent, tExp, tDate, tAMPM, tAP, tSecFrac, tFill:
		}
	}
	switch {
	case general:
		s.kind = secGeneral
	case digits && slash:
		s.kind = secFraction
	case digits:
		s.kind = secNumber
	case at:
		s.kind = secText
	default:
		s.kind = secLiteral
	}
	if s.kind != secNumber && s.kind != secFraction {
		s.literalize(tDot, ".")
		s.literalize(tComma, ",")
		s.literalize(tSlash, "/")
		s.literalize(tPercent, "%")
	}
	if s.kind != secNumber {
		for i, t := range s.toks {
			if t.kind == tExp {
				s.toks[i] = token{kind: tLit, text: "E" + t.text}
			}
		}
	}
}

// literalize turns the tokens of kind k into the literal text.
func (s *section) literalize(k tokKind, text string) {
	for i, t := range s.toks {
		if t.kind == k {
			s.toks[i] = token{kind: tLit, text: text}
		}
	}
}

// resolveDate applies LibreOffice's rule for m (svl/source/numbers/
// zforscan.cxx, ScanType): a minute when the keyword before it, literals
// skipped, is an hour, the keyword after it is a second, or an hour or a
// second came since the last minute; else a month. The digits after a
// second's decimal point are its fraction.
func (s *section) resolveDate() {
	timeSeen, haveMinute := false, false
	prevKey := func(i int) (dateKey, bool) {
		for j := i - 1; j >= 0; j-- {
			if s.toks[j].kind == tDate {
				return s.toks[j].key, true
			}
		}
		return 0, false
	}
	nextKey := func(i int) (dateKey, bool) {
		for j := i + 1; j < len(s.toks); j++ {
			if s.toks[j].kind == tDate {
				return s.toks[j].key, true
			}
		}
		return 0, false
	}
	for i := range s.toks {
		t := &s.toks[i]
		if t.kind != tDate {
			continue
		}
		switch t.key {
		case kHour:
			timeSeen = true
		case kSecond:
			if !haveMinute {
				timeSeen = true
			}
		case kMonth:
			if t.n > 2 {
				continue
			}
			p, pok := prevKey(i)
			n, nok := nextKey(i)
			if (pok && (p == kHour || p == kElapsedHour)) || (nok && (n == kSecond || n == kElapsedSecond)) || timeSeen {
				t.key = kMinute
				timeSeen = false
				haveMinute = true
			}
		case kYear, kDay, kMinute, kElapsedHour, kElapsedMinute, kElapsedSecond:
		}
	}
	var out []token
	for i := 0; i < len(s.toks); i++ {
		t := s.toks[i]
		if t.kind == tDot && i+1 < len(s.toks) && s.toks[i+1].kind == tDigit && s.toks[i+1].text == "0" {
			if p, ok := prevKey(i); ok && (p == kSecond || p == kElapsedSecond) {
				n := 0
				for i+1 < len(s.toks) && s.toks[i+1].kind == tDigit && s.toks[i+1].text == "0" {
					n++
					i++
				}
				out = append(out, token{kind: tSecFrac, n: n})
				continue
			}
		}
		switch t.kind {
		case tDot:
			t = token{kind: tLit, text: "."}
		case tComma:
			t = token{kind: tLit, text: ","}
		case tSlash:
			t = token{kind: tLit, text: "/"}
		case tPercent:
			t = token{kind: tLit, text: "%"}
		case tDigit:
			t = token{kind: tLit, text: t.text}
		case tExp:
			t = token{kind: tLit, text: "E" + t.text}
		case tGeneral:
			t = token{kind: tLit, text: "General"}
		case tLit, tDate, tAMPM, tAP, tSecFrac, tAt, tFill:
		}
		if t.kind == tLit && len(out) > 0 && out[len(out)-1].kind == tLit {
			out[len(out)-1].text += t.text
			continue
		}
		out = append(out, t)
	}
	s.toks = out
}
