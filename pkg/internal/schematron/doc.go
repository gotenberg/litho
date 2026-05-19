// Package schematron provides a minimal ISO Schematron loader and
// evaluator scoped to the subset of constructs the EN 16931 and
// XRechnung rule sets use. It is not a general-purpose Schematron
// implementation: abstract patterns, phases-with-roles, diagnostics,
// and let-substitution beyond a single namespace's worth of bindings
// are intentionally out of scope.
//
// # XPath subset
//
// Rules are compiled and evaluated against github.com/antchfx/xpath,
// an XPath 1.0 engine that also implements a handful of XPath 2.0
// builtins. A lightweight rewriter in xpath_rewrite.go bridges the
// remaining gap for the constructs the bundled rule sets rely on.
//
// Supported (compiled and evaluated):
//
//   - XPath 1.0 in full: path expressions, predicates, axes,
//     arithmetic, comparison, boolean and node-set operators.
//   - XPath 2.0 builtins provided by antchfx/xpath: matches, replace,
//     string-join, upper-case, lower-case, reverse, ends-with.
//   - XPath 2.0 builtins emulated by the rewriter: xs:decimal /
//     xs:integer / xs:double / xs:string / xs:date / xs:dateTime
//     (cast → number or string), exists (→ boolean), abs (algebraic
//     identity), count(distinct-values(X)) = 1 (uniqueness idiom),
//     "every $V in PATH satisfies EXPR" (→ not(PATH[not(EXPR')])),
//     "some $V in PATH satisfies EXPR" (→ boolean(PATH[EXPR'])),
//     "EXPR = (a, b, c)" and "EXPR != (a, b, c)" (→ disjunction /
//     conjunction of per-item comparisons), bare sequence
//     "(X, Y, Z)" in boolean context (→ disjunction of boolean
//     coercions), path-step cast like ".[pred]/xs:decimal(child)"
//     (cast elided so implicit coercion at the surrounding operator
//     does the work), round(...) (wrapped in number() to dodge an
//     antchfx/xpath bug that emits int instead of float64).
//
// Unsupported (rule is skipped; the evaluator surfaces the gap via
// [SkippedRule]):
//
//   - tokenize(): regex-based string splitting with no XPath 1.0
//     equivalent.
//   - distinct-values() outside the "count = 1" idiom.
//   - "for $x in EXPR return BODY" expressions.
//   - "if (COND) then A else B" conditional expressions.
//   - string-to-codepoints() and other Unicode helpers.
//   - User-defined xsl:function helpers (e.g. u:decimalOrZero).
//   - xs:duration, xs:gYear, xs:* casts with no numeric equivalent.
//
// Callers that need full XPath 2.0 coverage of skipped rules should
// pipe the embedded CII XML through an external XSLT 2.0 processor
// (Saxon-EE, KoSIT validator) and treat the results as authoritative.
//
// # Concurrency
//
// A [Schema] is immutable once [Load] returns. Concurrent readers
// are safe. Mutating a Schema after Load is undefined.
package schematron
