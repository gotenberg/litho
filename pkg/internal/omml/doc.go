// Package omml reads the equations of Office documents, Office Math
// (ECMA-376 Part 1, 22.1), into the formula tree package formula lays
// out: a Word document's m:oMath and m:oMathPara, and the same zones a
// PowerPoint paragraph holds in a14:m.
//
// [Read] maps an equation's objects (22.1.2) to MathML Core elements, a
// run's text to tokens (letters to identifiers, digits to numbers, the
// rest to operators) and its sty and scr to the Mathematical
// Alphanumeric Symbols of Unicode. The converter resolves the
// word-processing properties of each run, its size and colour, through
// [Options.Run], since those are WordprocessingML in a Word document and
// DrawingML in a presentation. [Equation.Layout] lays the tree out
// through a [formula.Setter]; before it does, it measures the parts of
// each equation array and matrix that align at their alignment marks or
// at a column edge, which MathML Core's tables do not do, and pads them.
//
// [ReadSettings] reads the document's math settings (m:mathPr, 22.1.2.62):
// the math font, the default justification of a display equation and
// where the limits of n-ary operators go.
//
// Not drawn: line breaks inside an equation,
// automatic (brkBin, wrapIndent) or manual (m:brk); the font family a run
// names for itself, an equation being set in the math font; the growth of
// an n-ary operator with its argument (m:grow on m:nary); the vertical
// justification of m:groupChr; the row and column spacing rules of
// matrices and equation arrays; the colour and size of an object's own
// characters (m:ctrlPr).
package omml
