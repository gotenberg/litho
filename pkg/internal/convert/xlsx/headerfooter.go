package xlsx

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// maxHeaderRunes caps the characters of one header or footer text.
// Anti-DoS guard, not spec-mandated: Excel holds 255 characters a text.
const maxHeaderRunes = 4096

// headerFooter reads a headerFooter element (ECMA-376 Part 1, 18.3.1.46):
// the odd, even and first page texts, each parsed into its left, centre
// and right parts. As LibreOffice imports it, the even and first page
// texts apply only when their flag is set and they hold something, the
// header and the footer deciding apart; scaleWithDoc is not read.
func (r *reader) headerFooter(e *ooxml.Element) sheet.HeaderFooter {
	var hf sheet.HeaderFooter
	text := func(local string) string { return e.Child(nsMain, local).Text }
	base := r.normal().Font
	odd := [2]string{text("oddHeader"), text("oddFooter")}
	even := [2]string{text("evenHeader"), text("evenFooter")}
	first := [2]string{text("firstHeader"), text("firstFooter")}
	useEven := boolAttr(e, "differentOddEven", false) && (even[0] != "" || even[1] != "")
	useFirst := boolAttr(e, "differentFirst", false) && (first[0] != "" || first[1] != "")
	for i := range 2 {
		if !useEven || even[i] == "" {
			even[i] = odd[i]
		}
		if !useFirst || first[i] == "" {
			first[i] = odd[i]
		}
	}
	hf.Odd = sheet.Margins{Header: parseHeaderFooter(odd[0], base, r.theme), Footer: parseHeaderFooter(odd[1], base, r.theme)}
	if useEven {
		hf.DifferentOddEven = true
		hf.Even = sheet.Margins{Header: parseHeaderFooter(even[0], base, r.theme), Footer: parseHeaderFooter(even[1], base, r.theme)}
	}
	if useFirst {
		hf.DifferentFirst = true
		hf.First = sheet.Margins{Header: parseHeaderFooter(first[0], base, r.theme), Footer: parseHeaderFooter(first[1], base, r.theme)}
	}
	return hf
}

// hfParser is the state of one header or footer text's parsing.
type hfParser struct {
	base  sheet.Font
	theme *ooxml.Theme
	font  sheet.Font
	part  int
	parts [3][]sheet.Span
	buf   strings.Builder
}

// parseHeaderFooter parses a header or footer text (ECMA-376 Part 1,
// 18.3.1.46, the formatting codes) into its left, centre and right
// parts, as LibreOffice's import reads it: &L, &C and &R start a part
// (the centre one when the text starts with none), each part starting
// from base, the Normal style's font; &P, &N, &A, &F, &Z, &D and &T are
// the page, the page count, the sheet, the file name, the file path, the
// date and the time (&Z&F reads as the path alone); &B, &I, &U, &E, &S,
// &X and &Y toggle bold, italic, single and double underline, strike,
// superscript and subscript; &"font,style" sets the face and its bold or
// italic style ("-" is the Normal font's face), &nn the size in points,
// &Krrggbb or &Kttsnnn the colour (a theme index with a tint percent);
// && is an ampersand and a line break stays in the text. Other codes,
// &G (a picture), &O and &H (outline and shadow), are dropped. Every
// span carries the font it draws with.
func parseHeaderFooter(s string, base sheet.Font, theme *ooxml.Theme) [3][]sheet.Span {
	p := &hfParser{base: base, theme: theme, font: copyFont(base), part: 1}
	if utf8.RuneCountInString(s) > maxHeaderRunes {
		s = string([]rune(s)[:maxHeaderRunes])
	}
	s = decodeXString(s)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '&' {
			p.buf.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			break
		}
		i++
		code := s[i]
		if code >= 'a' && code <= 'z' {
			code -= 'a' - 'A'
		}
		switch code {
		case '&':
			p.buf.WriteByte('&')
		case 'L':
			p.newPart(0)
		case 'C':
			p.newPart(1)
		case 'R':
			p.newPart(2)
		case 'P':
			p.field(sheet.FieldPage)
		case 'N':
			p.field(sheet.FieldPages)
		case 'A':
			p.field(sheet.FieldSheet)
		case 'F':
			p.field(sheet.FieldFile)
		case 'Z':
			p.field(sheet.FieldPath)
			if i+2 < len(s) && s[i+1] == '&' && (s[i+2] == 'F' || s[i+2] == 'f') {
				i += 2
			}
		case 'D':
			p.field(sheet.FieldDate)
		case 'T':
			p.field(sheet.FieldTime)
		case 'B':
			p.flush()
			p.font.Bold = !p.font.Bold
		case 'I':
			p.flush()
			p.font.Italic = !p.font.Italic
		case 'U':
			p.flush()
			p.font.Underline = toggle(p.font.Underline, sheet.Single)
		case 'E':
			p.flush()
			p.font.Underline = toggle(p.font.Underline, sheet.Double)
		case 'S':
			p.flush()
			p.font.Strike = !p.font.Strike
		case 'X':
			p.flush()
			p.font.Script = toggleScript(p.font.Script, sheet.Superscript)
		case 'Y':
			p.flush()
			p.font.Script = toggleScript(p.font.Script, sheet.Subscript)
		case 'K':
			if i+6 < len(s) {
				p.flush()
				p.color(s[i+1 : i+7])
				i += 6
			}
		case '"':
			end := strings.IndexByte(s[i+1:], '"')
			if end < 0 {
				i = len(s)
				break
			}
			p.flush()
			p.fontName(s[i+1 : i+1+end])
			i += end + 1
		default:
			if code >= '0' && code <= '9' {
				j := i
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				if n, err := strconv.Atoi(s[i:j]); err == nil && j-i <= 4 && n > 0 && n <= 1000 {
					p.flush()
					p.font.Size = float64(n)
				}
				i = j - 1
			}
		}
	}
	p.flush()
	return p.parts
}

// copyFont returns f with its own family slice.
func copyFont(f sheet.Font) sheet.Font {
	f.Families = append([]string(nil), f.Families...)
	return f
}

// flush ends the text span being collected.
func (p *hfParser) flush() {
	if p.buf.Len() == 0 {
		return
	}
	f := copyFont(p.font)
	p.parts[p.part] = append(p.parts[p.part], sheet.Span{Text: p.buf.String(), Font: &f})
	p.buf.Reset()
}

// field adds a field span.
func (p *hfParser) field(f sheet.Field) {
	p.flush()
	font := copyFont(p.font)
	p.parts[p.part] = append(p.parts[p.part], sheet.Span{Field: f, Font: &font})
}

// newPart starts the part i, its font reset to the base font.
func (p *hfParser) newPart(i int) {
	p.flush()
	if i != p.part {
		p.part = i
		p.font = copyFont(p.base)
	}
}

// fontName applies a &"name,style" code: the style's words bold, italic
// and their German and Hungarian names, as LibreOffice reads them.
func (p *hfParser) fontName(spec string) {
	name, style, hasStyle := strings.Cut(spec, ",")
	name = strings.TrimSpace(name)
	switch {
	case name == "-":
		p.font.Families = copyFont(p.base).Families
	case name != "":
		p.font.Families = []string{name}
	}
	if !hasStyle {
		return
	}
	p.font.Bold, p.font.Italic = false, false
	for _, w := range strings.Fields(strings.ToLower(style)) {
		switch w {
		case "bold", "fett", "demibold", "halbfett", "black", "heavy", "félkövér":
			p.font.Bold = true
		case "italic", "kursiv", "oblique", "schräg", "dőlt":
			p.font.Italic = true
		}
	}
}

// color applies a &K code's six characters: RRGGBB, or TTSNNN, a theme
// colour index with a signed tint percent.
func (p *hfParser) color(code string) {
	if code[2] == '+' || code[2] == '-' {
		idx, err1 := strconv.Atoi(code[:2])
		pct, err2 := strconv.Atoi(code[2:])
		if err1 != nil || err2 != nil || idx < 0 || idx >= len(themeColorNames) {
			return
		}
		if c, ok := p.theme.Color(themeColorNames[idx]); ok {
			p.font.Color = tint(c, float64(pct)/100)
		}
		return
	}
	if c, ok := ooxml.HexColor(code); ok {
		p.font.Color = c
	}
}

// toggle switches underline u to kind, or off when it is kind already.
func toggle(u, kind sheet.Underline) sheet.Underline {
	if u == kind {
		return sheet.NoUnderline
	}
	return kind
}

// toggleScript switches the script s to kind, or to the baseline when it
// is kind already.
func toggleScript(s, kind sheet.Script) sheet.Script {
	if s == kind {
		return sheet.Baseline
	}
	return kind
}
