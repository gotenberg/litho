package xlsx

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// maxCellRunes caps the characters of one cell's text. Anti-DoS guard, not
// spec-mandated: Excel holds at most 32,767 characters in a cell.
const maxCellRunes = 32_767

// richText is a cell's text: plain, or rich with its runs.
type richText struct {
	text string
	runs []sheet.Run
}

// readSharedStrings reads the shared string table of the workbook part
// main (ECMA-376 Part 1, 18.4.9 sst); a missing part leaves it empty.
func (r *reader) readSharedStrings(main string) error {
	name := r.pkg.RelatedPart(main, relSharedStrings)
	if name == "" {
		return nil
	}
	root, err := r.pkg.ParseXML(r.ctx, name, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("the shared strings do not parse: %v", err))
			return nil
		}
		return err
	}
	for _, si := range root.Kids() {
		if si.Is(nsMain, "si") {
			r.strings = append(r.strings, r.richText(si))
		}
	}
	return nil
}

// richText reads a CT_Rst (ECMA-376 Part 1, 18.4.8 si, 18.3.1.53 is): a
// plain t, or runs r each with its properties rPr and text t. Phonetic
// runs (18.4.6 rPh) are dropped: Calc does not print them. A run without
// properties takes the cell's font; one with properties starts from
// LibreOffice's default font, as a font record does.
func (r *reader) richText(e *ooxml.Element) richText {
	var rt richText
	var b strings.Builder
	n := 0
	add := func(s string) string {
		s = decodeXString(s)
		if left := maxCellRunes - n; utf8.RuneCountInString(s) > left {
			s = string([]rune(s)[:max(0, left)])
		}
		n += utf8.RuneCountInString(s)
		b.WriteString(s)
		return s
	}
	rich := false
	for _, c := range e.Kids() {
		switch {
		case c.Is(nsMain, "t"):
			s := add(c.Text)
			rt.runs = append(rt.runs, sheet.Run{Text: s})
		case c.Is(nsMain, "r"):
			s := add(textOf(c.Child(nsMain, "t")))
			run := sheet.Run{Text: s}
			if pr := c.Child(nsMain, "rPr"); pr != nil {
				f := r.font(pr)
				run.Font = &f
				rich = true
			}
			rt.runs = append(rt.runs, run)
		}
	}
	rt.text = b.String()
	if !rich {
		rt.runs = nil
	}
	return rt
}

// decodeXString decodes the escapes of an ST_Xstring (ECMA-376 Part 1,
// 22.9.2.19): _xHHHH_ stands for the UTF-16 code unit HHHH, a surrogate
// pair for one character, and _x005F_ escapes an underscore.
func decodeXString(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	var b strings.Builder
	var high rune
	for i := 0; i < len(s); {
		if i+7 <= len(s) && s[i] == '_' && s[i+1] == 'x' && s[i+6] == '_' {
			if v, err := strconv.ParseUint(s[i+2:i+6], 16, 16); err == nil {
				u := rune(v)
				switch {
				case u >= 0xD800 && u < 0xDC00:
					high = u
				case u >= 0xDC00 && u < 0xE000 && high != 0:
					b.WriteRune((high-0xD800)<<10 + (u - 0xDC00) + 0x10000)
					high = 0
				case u == 0 || u >= 0xD800 && u < 0xE000:
					high = 0
				default:
					b.WriteRune(u)
					high = 0
				}
				i += 7
				continue
			}
		}
		high = 0
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// inlineText reads the inline string of a cell (18.3.1.53 is).
func (r *reader) inlineText(c *ooxml.Element) richText {
	return r.richText(c.Child(nsMain, "is"))
}

// sharedText returns shared string i, false when the table lacks it.
func (r *reader) sharedText(v string) (richText, bool) {
	i, ok := ooxml.Int(v)
	if !ok || i < 0 || i >= len(r.strings) {
		return richText{}, false
	}
	return r.strings[i], true
}
