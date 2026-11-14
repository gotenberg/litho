// Package formula lays mathematical formulas out: a tree of MathML Core
// elements in, a box of positioned glyphs and filled paths out (MathML
// Core, W3C Candidate Recommendation, sections 3 to 5).
//
// A converter builds the tree, [Element] values, from its source: the
// HTML converter from the MathML of a document, an Office converter from
// an equation. It makes one [Setter] for a conversion, over the
// conversion's fonts, and hands each tree to [Setter.Layout] with the
// text style of the formula's root; the [Box] it returns gives the
// formula's width, its ascent and descent about its own baseline, and
// the fragments [Box.Draw] places on a page. Nothing else is needed: the
// tree holds no reference to its source. [AltText] gives the alternate
// description a tagged PDF hangs on the formula.
//
// The layout is the algorithms of MathML Core, section 3, step by step,
// reading the layout constants, glyph information and glyph variants of
// the OpenType MATH table of the first available font (package
// mathtable), and the fallback values of section 5 for a font without
// one, or Chromium's where it departs from them ([Setter.BrowserRules]).
// Each element's style either comes from the converter ([Element.Style],
// what CSS computed for it) or is derived from its parent's by the rules
// of the user agent stylesheet of MathML Core, Appendix A, and the
// element's attributes, which is what a converter without CSS needs.
//
// Not laid out: line breaking inside a formula; the elements of MathML 4
// that Core lacks (menclose, mlabeledtr, mglyph, maligngroup and the
// like), laid out as an mrow; HTML or SVG content inside a token element,
// whose text is laid out; the CSS display of a MathML element other than
// none, which is laid out as math whatever it says; positioned, floated,
// transformed, clipped or filtered elements inside a formula; dashed and
// dotted borders, drawn solid; the mirrored glyphs of a right-to-left
// formula; the glyph assemblies MathML Core, B.4 offers for a font
// without a MATH table; and writing modes other than horizontal-tb.
package formula
