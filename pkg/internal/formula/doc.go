// Package formula lays mathematical formulas out: a tree of MathML Core
// elements in, a box of positioned glyphs and filled paths out (MathML
// Core, W3C Candidate Recommendation, sections 3 to 5).
//
// A converter builds the tree, [Element] values, from its source: the
// HTML converter from the MathML of a document, an Office converter from
// an equation. It hands the tree to a [Setter], made once for a
// conversion, whose [Setter.Layout] lays it out in a text style and
// returns a [Box]: its width, its ascent and descent about its own
// baseline, and the fragments [Box.Draw] places on a page. Nothing else
// is needed: the tree holds no reference to its source.
//
// The layout is the algorithms of MathML Core, section 3, step by step,
// reading the layout constants, glyph information and glyph variants of
// the OpenType MATH table of the first available font
// ([mathtable.Table]), and the fallback values of section 5 for a font
// without one. Each element's style either comes from the converter
// ([Element.Style], what CSS computed for it) or is derived from its
// parent's by the rules of the user agent stylesheet of MathML Core,
// Appendix A, and its attributes ([derive]).
//
// Not laid out: line breaking inside a formula, the elements of MathML 4
// that Core lacks (menclose, mlabeledtr, mglyph, maligngroup and the
// like, laid out as an mrow), HTML content inside a token element (its
// text is laid out), and writing modes other than horizontal-tb.
package formula
