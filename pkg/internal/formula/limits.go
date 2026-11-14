package formula

// MaxDepth is the deepest nesting of elements a formula is laid out to;
// what lies deeper is left out with a warning. Anti-DoS guard, not
// spec-mandated: the layout recurses once a level, and formulas written
// by people or produced by editors nest a few dozen levels at most, such
// as a continued fraction of thirty terms.
const MaxDepth = 128

// MaxElements is the most elements of one formula the layout lays out;
// the rest is left out with a warning. Anti-DoS guard, not
// spec-mandated: a page of formulas holds a few thousand elements, and a
// formula is laid out apart from the page.
const MaxElements = 50_000

// MaxTableCells is the most cells of one mtable the layout lays out; the
// rows past it are left out with a warning. Anti-DoS guard, not
// spec-mandated: the matrices and aligned equations of real documents
// hold tens of cells, and a table is sized by passes over every cell.
const MaxTableCells = 10_000

// MaxAssemblyGlyphs is the most glyphs one stretched operator is built
// of (MathML Core, 5.3.1): past it the assembly stops short of its
// target with a warning. Anti-DoS guard, not spec-mandated: a target
// size comes from the formula, and an extender a font makes small would
// otherwise repeat without bound; a parenthesis a page tall takes about
// a hundred glyphs of DejaVu Math TeX Gyre.
const MaxAssemblyGlyphs = 1000
