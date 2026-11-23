package css

import (
	"strconv"
	"strings"
)

// quirkyLengthProps are the properties whose values take a <quirky-length>
// when CSS is parsed in quirks mode (CSS Values 4, Appendix C): a number
// that reads as that many pixels. The shorthands that include them, border
// and background among them, do not.
var quirkyLengthProps = map[string]bool{
	"background-position": true, "border-spacing": true,
	"border-top-width": true, "border-right-width": true, "border-bottom-width": true, "border-left-width": true, "border-width": true,
	"bottom": true, "clip": true, "font-size": true, "height": true, "left": true, "letter-spacing": true,
	"margin-right": true, "margin-left": true, "margin-top": true, "margin-bottom": true, "margin": true,
	"max-height": true, "max-width": true, "min-height": true, "min-width": true,
	"padding-top": true, "padding-right": true, "padding-bottom": true, "padding-left": true, "padding": true,
	"right": true, "text-indent": true, "top": true, "vertical-align": true, "width": true, "word-spacing": true,
}

// quirkyColorProps are the properties whose values take a <quirky-color>
// when CSS is parsed in quirks mode (CSS Color 4, Appendix B): a hex colour
// written without its "#". The background and border shorthands do not.
var quirkyColorProps = map[string]bool{
	"background-color": true, "border-color": true,
	"border-top-color": true, "border-right-color": true, "border-bottom-color": true, "border-left-color": true,
	"color": true,
}

// quirkyValue returns the value v of the property name, its whitespace
// left out, with its quirky lengths written as pixels and its quirky
// colours as hex colours, as CSS parsed in quirks mode reads them; v
// itself when the property takes neither. Only the top level of the value
// is read, and the arguments of rect() in clip: a quirky value is invalid
// inside any other function (CSS Values 4, Appendix C; CSS Color 4,
// Appendix B).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 reads width: 100,
//     margin: 10 20, border-width: 7, font-size: 20, vertical-align: 3,
//     background-position: 10 20, clip: rect(1, 2, 3, 4), border-spacing: 4,
//     color: ff0000, color: 123abc, color: abc and border-color: 0000ff in
//     quirks mode, and refuses border: 4 solid, calc(100 + 10px),
//     outline-width: 5, gap: 10, background: 00ff00 and
//     border-top-color: 1e3.
func quirkyValue(name string, v []token) []token {
	lengths, colors := quirkyLengthProps[name], quirkyColorProps[name]
	if !lengths && !colors {
		return v
	}
	var out []token
	set := func(i int, t token) {
		if out == nil {
			out = append([]token(nil), v...)
		}
		out[i] = t
	}
	for i, t := range v {
		switch {
		case lengths && t.typ == tNumber:
			set(i, pixels(t))
		case lengths && name == "clip" && t.typ == tFunction && eqFold(t.value, "rect"):
			kids := append([]token(nil), t.children...)
			for k := range kids {
				if kids[k].typ == tNumber {
					kids[k] = pixels(kids[k])
				}
			}
			t.children = kids
			set(i, t)
		case colors:
			if hex, ok := quirkyHex(t); ok {
				set(i, token{typ: tHash, value: hex})
			}
		}
	}
	if out == nil {
		return v
	}
	return out
}

// pixels returns the number token t as a dimension of that many pixels.
func pixels(t token) token {
	return token{typ: tDimension, repr: t.repr, num: t.num, integer: t.integer, value: "px"}
}

// quirkyHex returns the hex digits a <quirky-color> token t stands for
// (CSS Color 4, Appendix B): an identifier of 3 or 6 hex digits, or an
// integer number or dimension whose serialization, padded with zeros to 6
// characters, is 6 hex digits.
func quirkyHex(t token) (string, bool) {
	var s string
	switch {
	case t.typ == tIdent && (len(t.value) == 3 || len(t.value) == 6):
		s = t.value
	case (t.typ == tNumber || t.typ == tDimension) && t.integer && t.num >= 0 && t.num < 1e6:
		s = strconv.FormatInt(int64(t.num), 10)
		if t.typ == tDimension {
			s += t.value
		}
		if len(s) < 6 {
			s = strings.Repeat("0", 6-len(s)) + s
		}
		if len(s) != 6 {
			return "", false
		}
	default:
		return "", false
	}
	for _, c := range s {
		if !isHexDigit(c) {
			return "", false
		}
	}
	return s, true
}
