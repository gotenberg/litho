package css

// The caps below bound the work a hostile stylesheet or document can ask
// for. None of them is spec-mandated.

// maxStylesheetBytes caps the size of one stylesheet's text: 8 MiB. The
// largest framework bundles in use (a full Tailwind or Bootstrap build with
// every utility) stay under 4 MiB. Anti-DoS guard, not spec-mandated.
const maxStylesheetBytes = 8 << 20

// maxTokens caps the tokens of one stylesheet, whitespace included: 2^21.
// A dense 8 MiB stylesheet holds about two million tokens, and a token
// costs about a hundred bytes while its rule is parsed. Anti-DoS guard,
// not spec-mandated.
const maxTokens = 1 << 21

// maxNestingDepth caps the nesting of blocks and functions: 64. Real
// stylesheets nest a style rule inside two or three conditional rules, and
// calc() expressions inside a few parentheses. Anti-DoS guard, not
// spec-mandated: it bounds the parser's recursion.
const maxNestingDepth = 64

// maxRules caps the style, @page and @font-face rules a stylesheet
// contributes, imports included: 2^17. The largest framework builds hold
// about 30 000 rules. Anti-DoS guard, not spec-mandated.
const maxRules = 1 << 17

// maxImportDepth caps the @import chain: 8. Real sites import one or two
// levels deep. Anti-DoS guard, not spec-mandated.
const maxImportDepth = 8

// maxImports caps the stylesheets one Parse call fetches through @import:
// 64. Anti-DoS guard, not spec-mandated: each import is a fetch.
const maxImports = 64

// maxSelectorNesting caps the nesting of :not(), :is(), :where() and
// :nth-child(of S): 8. Real selectors nest one or two levels. Anti-DoS
// guard, not spec-mandated: it bounds the matcher's recursion.
const maxSelectorNesting = 8

// maxCompounds caps the compound selectors of one complex selector: 32.
// Real selectors chain fewer than ten. Anti-DoS guard, not spec-mandated:
// a descendant combinator backtracks, so matching time grows with the
// length.
const maxCompounds = 32

// maxSelectorsPerList caps the complex selectors of one selector list:
// 1024. Real lists group a few dozen. Anti-DoS guard, not spec-mandated.
const maxSelectorsPerList = 1024

// maxCalcDepth caps the nesting of calc() and its parentheses: 16. Anti-DoS
// guard, not spec-mandated: it bounds the calc parser's recursion.
const maxCalcDepth = 16

// maxVarTokens caps the tokens of one value after var() substitution:
// 2^14. Design-system variables expand to a few hundred tokens at most,
// and the cap stops a chain of variables from doubling at each step
// (CSS Variables 1, 3.3 allows such a cap). Anti-DoS guard, not
// spec-mandated.
const maxVarTokens = 1 << 14

// maxComputeSteps caps the work of one Compute call: 2^27 steps, a step
// being one compound selector tested against one element, one custom
// property copied or one token substituted. A document of 50 000 elements
// against 30 000 indexed rules takes a few million. Anti-DoS guard, not
// spec-mandated: past it Compute returns [ErrTooComplex].
const maxComputeSteps = 1 << 27

// maxWarnings caps the warnings a Computed or a Stylesheet keeps: 128.
// Past it a warning says how many were dropped. Anti-DoS guard, not
// spec-mandated.
const maxWarnings = 128

// maxFontSize caps a computed font size: 10 000 points. Wider than any
// page a converter produces. Anti-DoS guard, not spec-mandated: it keeps
// lengths finite and the layout engine's arithmetic exact.
const maxFontSize = 10000

// maxLength caps the magnitude of a computed length: 10^7 points, about 3.5
// km. Anti-DoS guard, not spec-mandated: it keeps every length finite.
const maxLength = 1e7
