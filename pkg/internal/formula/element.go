package formula

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Element is one element of a formula: a MathML Core element (MathML
// Core, 3) with its attributes, the text of a token, and its children.
//
// A converter builds the tree and does not change it once it hands it to
// [Setter.Layout], which only reads it.
type Element struct {
	// Name is the element's local name in lowercase: math, mrow, mi, mn,
	// mo, mtext, ms, mspace, mfrac, msqrt, mroot, msub, msup, msubsup,
	// munder, mover, munderover, mmultiscripts, mprescripts, mtable, mtr,
	// mtd, mpadded, mphantom, mstyle, merror, semantics or maction. A name
	// outside that list is laid out as an mrow, as MathML Core, 4.1 lays
	// out an unknown MathML element.
	Name string
	// Attrs are the element's attributes in source order, their names in
	// lowercase. Values are as the source writes them: the layout trims
	// and compares them as MathML Core says of each.
	Attrs []Attr
	// Text is the text of a token element (mi, mn, mo, mtext, ms), its
	// white space collapsed by the layout as CSS white-space: normal
	// does. Other elements ignore it.
	Text string
	// Children are the element's children in source order.
	Children []*Element
	// Style, when not nil, is the element's computed style, as a CSS
	// cascade gave it. nil derives it from the parent's style by the
	// user agent stylesheet of MathML Core (Appendix A) and the
	// element's attributes, which is what a converter without CSS needs.
	Style *Style
}

// Attr is one attribute of an [Element].
type Attr struct {
	Name, Value string
}

// Attr returns the value of the attribute name, and whether the element
// has it.
func (e *Element) Attr(name string) (string, bool) {
	for _, a := range e.Attrs {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

// attrIs reports whether the attribute name is an ASCII case-insensitive
// match to value, leading and trailing white space aside (MathML Core,
// 2.1.2: booleans and keywords compare without case).
func (e *Element) attrIs(name, value string) bool {
	v, ok := e.Attr(name)
	return ok && strings.EqualFold(strings.TrimSpace(v), value)
}

// Style is the computed style of an [Element]: what the layout reads of
// the CSS properties that apply to MathML (MathML Core, 2.2.2 and 4).
// Lengths are in points.
type Style struct {
	// Families are the font families in preference order, as
	// [text.Style.Families] takes them; the first available font, the
	// face they match, gives the MATH table (MathML Core, 5).
	Families []string
	// Size is the font size, positive.
	Size float64
	// Weight is the CSS font weight, 400 for normal.
	Weight int
	// Italic asks for an italic face.
	Italic bool
	// Lang is the BCP 47 language of the text, "" when unknown.
	Lang string
	// Fallback picks the face of a character the first available font
	// lacks.
	Fallback text.Fallback
	// Color paints the text, the fraction bars and the radical's
	// overbar.
	Color typeset.Color
	// Background fills the element's border box; transparent fills
	// nothing.
	Background typeset.Color
	// Display is math-style: normal, false for compact (MathML Core,
	// 4.3).
	Display bool
	// Cramped is math-shift: compact, false for normal (MathML Core,
	// 4.4).
	Cramped bool
	// Depth is the computed math-depth (MathML Core, 4.5).
	Depth int
	// MathAuto is text-transform: math-auto (MathML Core, 4.2): a token
	// of one character found in the italic mappings is drawn as its
	// mathematical italic form.
	MathAuto bool
	// Hidden is visibility: hidden: the element keeps its box and paints
	// nothing.
	Hidden bool
	// RTL is direction: rtl: the element's children are placed from the
	// right (MathML Core, 3.1.1, the inline offset of a child box).
	RTL bool
	// Padding, Border and Margin are the widths of each side, indexed
	// top, right, bottom, left (MathML Core, 3.1.2).
	Padding, Border, Margin [4]float64
	// BorderColor paints each side's border.
	BorderColor [4]typeset.Color
	// Width and Height, when HasWidth and HasHeight are set, size the
	// element's content box in place of its math content box (MathML
	// Core, 3.1.2): the CSS width and height of the element.
	Width, Height       float64
	HasWidth, HasHeight bool
}

// Sides of the [Style] edge arrays.
const (
	top = iota
	right
	bottom
	left
)

// AltText returns the alternate description of the formula root: its
// alttext attribute when it has one that is not blank (MathML Core,
// 2.1.1), else the text of its tokens in reading order, one space
// between two tokens. The children of semantics and maction past the
// first, which no layout draws, are left out.
func AltText(root *Element) string {
	if root == nil {
		return ""
	}
	if v, ok := root.Attr("alttext"); ok && strings.TrimSpace(v) != "" {
		return strings.Join(strings.Fields(v), " ")
	}
	var words []string
	var walk func(e *Element, depth int)
	walk = func(e *Element, depth int) {
		if depth > MaxDepth {
			return
		}
		if isToken(e.Name) {
			if t := strings.Join(strings.Fields(e.Text), " "); t != "" {
				words = append(words, t)
			}
			return
		}
		for i, c := range e.Children {
			if i > 0 && (e.Name == "semantics" || e.Name == "maction") {
				break
			}
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return strings.Join(words, " ")
}

// isToken reports the token elements, whose content is text (MathML
// Core, 3.2).
func isToken(name string) bool {
	switch name {
	case "mi", "mn", "mo", "mtext", "ms":
		return true
	}
	return false
}
