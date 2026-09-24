package schematron

import "strings"

// rewriteXPath2 transforms a subset of XPath 2.0 expressions into
// XPath 1.0 equivalents that antchfx/xpath can compile. The
// transformations are textual and intentionally narrow: cover the
// constructs EN 16931 and XRechnung lean on, leave everything else
// untouched.
//
// Specifically:
//
//   - xs:decimal(x) / xs:integer(x) / xs:double(x) → number(x).
//     Loses the exact-arithmetic semantics of xs:decimal but produces
//     the right verdict for the existence / equality / range checks
//     that dominate EN 16931's arithmetic rules.
//   - xs:string(x) / xs:date(x) / xs:dateTime(x) / xs:anyURI(x) →
//     string(x). Date comparisons fall back to lexicographic string
//     comparison; CCYY-MM-DD ordering happens to coincide with that.
//   - upper-case(x) / lower-case(x) → translate(x, lo, up) / mirror.
//   - The lossy transformations above are documented at the call
//     site so callers know what to expect when comparing verdicts
//     against Saxon.
//
// Functions outside this list (tokenize, string-join, current-date,
// xsl:function helpers) are left alone; rules that call them fail
// to compile and the evaluator skips them via the per-rule isolation
// in [evaluateRule].
func rewriteXPath2(expr string) string {
	out := expr
	// Tolerance must run before the cast rewrite so the marker
	// "xs:decimal(" is still in the input.
	out = rewriteDecimalEquality(out)
	for _, cast := range numericCasts {
		out = rewriteCast(out, "xs:"+cast, "number")
	}
	for _, cast := range stringCasts {
		out = rewriteCast(out, "xs:"+cast, "string")
	}
	out = elideCastInStep(out, "number")
	out = elideCastInStep(out, "string")
	out = rewriteCaseFunction(out, "upper-case", upperCaseLo, upperCaseUp)
	out = rewriteCaseFunction(out, "lower-case", lowerCaseUp, lowerCaseLo)
	out = rewriteFnRename(out, "exists", "boolean")
	out = rewriteDistinctValuesEqOne(out)
	out = rewriteRoundAsFloor(out)
	out = rewriteNotNormalizeSpace(out)
	out = rewriteNotNumericCast(out)
	out = rewriteValueComparisonOperators(out)
	out = rewriteTokenizeWhitespaceLiteral(out)
	out = rewriteAbsToleranceComparison(out)
	out = rewriteSequenceQuantifier(out)
	out = rewriteBooleanLiteralComparison(out)
	out = rewriteSequenceEquality(out)
	// Bare sequence construction runs AFTER sequence-equality so the
	// equality form's "= (a, b)" gets first dibs on the literal and
	// is rewritten as a disjunction of equality comparisons rather
	// than a disjunction of boolean coercions.
	out = rewriteBareSequenceAsDisjunction(out)
	return out
}

//nolint:gochecknoglobals // immutable cast tables shared across calls.
var (
	numericCasts = []string{
		"decimal", "integer", "double", "float", "long", "int",
		"short", "byte", "positiveInteger", "nonNegativeInteger",
	}
	stringCasts = []string{
		"string", "date", "dateTime", "time", "anyURI",
		"normalizedString", "token", "language",
	}
)

// rewriteCast walks the expression and replaces every call of the
// form "fullName(EXPR)" with "replacement(EXPR)". Paren-aware so
// nested casts like xs:decimal(xs:decimal(sum(x))) collapse to
// number(number(sum(x))) in a single pass.
func rewriteCast(expr, fullName, replacement string) string {
	prefix := fullName + "("
	if !strings.Contains(expr, prefix) {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); {
		if strings.HasPrefix(expr[i:], prefix) && (i == 0 || !isXPathNameChar(expr[i-1])) {
			start := i + len(prefix)
			end := matchCloseParen(expr, start)
			if end < 0 {
				b.WriteByte(expr[i])
				i++
				continue
			}
			b.WriteString(replacement)
			b.WriteByte('(')
			b.WriteString(expr[start:end])
			b.WriteByte(')')
			i = end + 1
			continue
		}
		b.WriteByte(expr[i])
		i++
	}
	return b.String()
}

// elideCastInStep drops a numeric or string cast that sits directly
// after a path-step "/". XPath 1.0 forbids a function call in that
// position, but XPath 2.0 (and the EN 16931 Schematron's BR-CO-17 in
// particular) uses the form to type-cast a path step in place:
//
//	.[predicate]/xs:decimal(ram:Rate)
//
// After the generic [rewriteCast] pass that becomes ".[pred]/number(
// ram:Rate)" which antchfx/xpath rejects as an invalid token.
// Dropping the cast wrapper leaves ".[pred]/ram:Rate", a plain path
// step; implicit numeric coercion at the surrounding arithmetic
// operator preserves the verdict. Used for "number" and "string"
// (the targets the generic cast pass produces).
func elideCastInStep(expr, fnName string) string {
	target := "/" + fnName + "("
	for {
		idx := strings.Index(expr, target)
		if idx < 0 {
			return expr
		}
		argStart := idx + len(target)
		argEnd := matchCloseParen(expr, argStart)
		if argEnd < 0 {
			return expr
		}
		arg := expr[argStart:argEnd]
		expr = expr[:idx+1] + arg + expr[argEnd+1:]
	}
}

// rewriteRoundAsFloor rewrites every round(EXPR) call into
// floor((EXPR) + 0.5). antchfx/xpath v1.3.6's round() returns a Go
// int rather than float64; that int propagates as NaN through every
// downstream arithmetic operation because the engine's asNumber
// coercion has no case for int. floor() goes through math.Floor and
// returns float64 cleanly, and floor(x + 0.5) matches XPath 1.0's
// rounding semantics for non-negative numbers (round-half-up). EN
// 16931's BR-CO-* arithmetic rules use round() on always-positive
// monetary sums, so the half-up vs banker's-rounding distinction
// does not bite. For negative halves the result differs from
// XPath 1.0's "round half toward +Inf" by one ULP, which is below
// the tolerance every Schematron arithmetic rule already accepts.
func rewriteRoundAsFloor(expr string) string {
	if !strings.Contains(expr, "round") {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr) + 16)
	for i := 0; i < len(expr); {
		// "round" must appear as a function-call boundary: the byte
		// before must not be a name char (so "ground" is left alone)
		// and an open paren must follow, with optional whitespace
		// between the identifier and the paren. The bundled Factur-X
		// profile schematrons use the spaced form "round (X)" in
		// places, which the old prefix-only matcher silently skipped
		// and left the broken antchfx round() behaviour in place.
		if !strings.HasPrefix(expr[i:], "round") || (i > 0 && isXPathNameChar(expr[i-1])) {
			b.WriteByte(expr[i])
			i++
			continue
		}
		k := i + len("round")
		for k < len(expr) && (expr[k] == ' ' || expr[k] == '\t' || expr[k] == '\n') {
			k++
		}
		if k >= len(expr) || expr[k] != '(' {
			b.WriteByte(expr[i])
			i++
			continue
		}
		start := k + 1
		end := matchCloseParen(expr, start)
		if end < 0 {
			b.WriteByte(expr[i])
			i++
			continue
		}
		// Recurse into the argument so a nested round() (which the
		// upstream Factur-X schematrons use heavily, e.g. round of a
		// sum-of-rounds expression) also gets rewritten. Without the
		// recursion the outer rewrite preserves the inner "round(X)"
		// untouched and the broken antchfx integer-returning round()
		// silently NaN-propagates through the surrounding arithmetic.
		b.WriteString("floor((")
		b.WriteString(rewriteRoundAsFloor(expr[start:end]))
		b.WriteString(") + 0.5)")
		i = end + 1
	}
	return b.String()
}

// rewriteNotNormalizeSpace converts every "not(normalize-space(X))"
// call into "string-length(normalize-space(X))=0", with X defaulting
// to "." when the call site omits the argument.
//
// antchfx/xpath v1.3.6 evaluates not("") as false rather than true:
// the engine's string-to-boolean coercion only fires for some code
// paths, so "not(empty-string)" silently stays at the default false.
// That breaks every "//*[not(normalize-space())]" rule context:
// FX-SCH-A-000372's PEPPOL-EN16931-R008 "Document MUST not contain
// empty elements" rule is the canonical victim. The string-length
// form sidesteps the bug because antchfx coerces the integer to a
// boolean correctly.
func rewriteNotNormalizeSpace(expr string) string {
	prefix := "not(normalize-space"
	if !strings.Contains(expr, prefix) {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr) + 32)
	for i := 0; i < len(expr); {
		if !strings.HasPrefix(expr[i:], prefix) || (i > 0 && isXPathNameChar(expr[i-1])) {
			b.WriteByte(expr[i])
			i++
			continue
		}
		k := i + len(prefix)
		if k >= len(expr) || expr[k] != '(' {
			b.WriteByte(expr[i])
			i++
			continue
		}
		argStart := k + 1
		argEnd := matchCloseParen(expr, argStart)
		if argEnd < 0 {
			b.WriteByte(expr[i])
			i++
			continue
		}
		notClose := argEnd + 1
		if notClose >= len(expr) || expr[notClose] != ')' {
			b.WriteByte(expr[i])
			i++
			continue
		}
		arg := strings.TrimSpace(expr[argStart:argEnd])
		if arg == "" {
			arg = "."
		}
		b.WriteString("string-length(normalize-space(")
		b.WriteString(arg)
		b.WriteString("))=0")
		i = notClose + 1
	}
	return b.String()
}

// rewriteNotNumericCast strips a "not(number(X))" wrapper down to
// "not(X)" when X is a simple path expression. Whitespace between
// "not" and its open paren is tolerated because XPath permits it
// and the bundled Factur-X profile schematrons use the spaced form.
//
// antchfx/xpath v1.3.6 evaluates not(NaN) as false rather than the
// XPath 1.0 not(0)=true semantics. Schematron arithmetic rules
// (notably BR-CO-16 / FX-SCH-A-000122) rely on the
// "not(xs:decimal(optional-field))" idiom to detect a missing
// field, and the rewriter converts the xs:decimal cast to number()
// before this pass. Without the unwrap the test silently flips
// from "field absent" to false, producing false positives once the
// arithmetic disjuncts run.
//
// The unwrap keeps XPath 1.0 semantics for the "missing" case:
// not(empty-node-set) is true. For numerically-zero values the
// outer rule's matching equality disjunct still covers the test
// (e.g. BR-CO-16's first clause covers TotalPrepaid=0 via the
// arithmetic), so the precision loss for that branch does not
// propagate into a wrong verdict.
//
// The rewrite only fires when the cast wraps a plain path: function
// calls, comparisons, and arithmetic stay untouched so callers that
// rely on the cast for non-existence semantics keep their behaviour.
//
// # Corpus evidence
//
//   - All corpora: without the unwrap the test flipped from "field
//     absent" to false on every fixture, producing dozens of false
//     positives once the arithmetic disjuncts ran.
func rewriteNotNumericCast(expr string) string {
	if !strings.Contains(expr, "not") || !strings.Contains(expr, "number(") {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr))
	i := 0
	for i < len(expr) {
		// Look for "not" as a function-call boundary.
		if !strings.HasPrefix(expr[i:], "not") || (i > 0 && isXPathNameChar(expr[i-1])) {
			b.WriteByte(expr[i])
			i++
			continue
		}
		k := i + 3
		// Skip whitespace between "not" and its open paren.
		for k < len(expr) && (expr[k] == ' ' || expr[k] == '\t' || expr[k] == '\n') {
			k++
		}
		if k >= len(expr) || expr[k] != '(' {
			b.WriteByte(expr[i])
			i++
			continue
		}
		// Skip whitespace inside the not(...) call before the next
		// token.
		k++
		for k < len(expr) && (expr[k] == ' ' || expr[k] == '\t' || expr[k] == '\n') {
			k++
		}
		if !strings.HasPrefix(expr[k:], "number(") {
			b.WriteByte(expr[i])
			i++
			continue
		}
		argStart := k + len("number(")
		argEnd := matchCloseParen(expr, argStart)
		if argEnd < 0 {
			b.WriteByte(expr[i])
			i++
			continue
		}
		// After the inner number(...) close paren, optional whitespace
		// then the not(...) close paren.
		tail := argEnd + 1
		for tail < len(expr) && (expr[tail] == ' ' || expr[tail] == '\t' || expr[tail] == '\n') {
			tail++
		}
		if tail >= len(expr) || expr[tail] != ')' {
			b.WriteByte(expr[i])
			i++
			continue
		}
		inner := strings.TrimSpace(expr[argStart:argEnd])
		if !looksLikePathOrCall(inner) {
			b.WriteByte(expr[i])
			i++
			continue
		}
		b.WriteString("not(")
		b.WriteString(inner)
		b.WriteString(")")
		i = tail + 1
	}
	return b.String()
}

// rewriteValueComparisonOperators rewrites XPath 2.0 value-comparison
// operators (eq, ne, lt, le, gt, ge) into their XPath 1.0
// general-comparison counterparts (=, !=, <, <=, >, >=). The forms
// differ on node-set vs atomic semantics in XPath 2.0, but the
// bundled EN 16931 / Factur-X schematrons use the value operators
// exclusively against atomic results (count(X) eq 1, position() lt
// last(), etc.) where the substitution preserves the verdict.
//
// Tokens are matched on whitespace-and-name-boundary so an element
// named "ram:eq" or a variable like "$eqValue" is left alone.
func rewriteValueComparisonOperators(expr string) string {
	replacements := []struct{ from, to string }{
		{" eq ", " = "},
		{" ne ", " != "},
		{" lt ", " < "},
		{" le ", " <= "},
		{" gt ", " > "},
		{" ge ", " >= "},
	}
	out := expr
	for _, r := range replacements {
		if !strings.Contains(out, r.from) {
			continue
		}
		var b strings.Builder
		b.Grow(len(out))
		i := 0
		for i < len(out) {
			if !strings.HasPrefix(out[i:], r.from) {
				b.WriteByte(out[i])
				i++
				continue
			}
			// Confirm the surrounding bytes form a valid token boundary:
			// the byte before the leading space must close an identifier
			// or a paren/number, never another name char.
			if i == 0 {
				b.WriteByte(out[i])
				i++
				continue
			}
			b.WriteString(r.to)
			i += len(r.from)
		}
		out = b.String()
	}
	return out
}

// rewriteSequenceQuantifier expands inline XPath 2.0 quantifier
// expressions whose binding sequence is a literal "(v1, v2, ...)"
// into the equivalent XPath 1.0 disjunction / conjunction of the
// substituted body. Used when the quantifier sits inside a
// predicate or function argument and [peelQuantifier] cannot
// surface it.
//
//	some $V in (a, b, c) satisfies body($V)
//	    -> (body(a) or body(b) or body(c))
//	every $V in (a, b, c) satisfies body($V)
//	    -> (body(a) and body(b) and body(c))
//
// EN 16931 / XRechnung / Factur-X pair this idiom with
// [rewriteTokenizeWhitespaceLiteral] so the typical
//
//	[some $code in tokenize('30 58', '\s') satisfies normalize-space(.) = $code]
//
// predicate becomes a plain disjunction antchfx can compile.
func rewriteSequenceQuantifier(expr string) string {
	if !strings.Contains(expr, "every $") && !strings.Contains(expr, "some $") {
		return expr
	}
	for {
		next := rewriteOneSequenceQuantifier(expr)
		if next == expr {
			return expr
		}
		expr = next
	}
}

func rewriteOneSequenceQuantifier(expr string) string {
	for _, kw := range []string{"every", "some"} {
		// Walk every occurrence of the keyword and try to peel it.
		for searchFrom := 0; searchFrom < len(expr); {
			idx := strings.Index(expr[searchFrom:], kw)
			if idx < 0 {
				break
			}
			idx += searchFrom
			searchFrom = idx + 1
			// Boundary check: identifier-class boundary on both sides.
			if idx > 0 && isXPathNameChar(expr[idx-1]) {
				continue
			}
			tail := idx + len(kw)
			if tail >= len(expr) || isXPathNameChar(expr[tail]) {
				continue
			}
			// Skip whitespace and require "$varName".
			k := tail
			for k < len(expr) && isXMLWhitespace(expr[k]) {
				k++
			}
			if k >= len(expr) || expr[k] != '$' {
				continue
			}
			nameEnd := k + 1
			for nameEnd < len(expr) && isXPathNameChar(expr[nameEnd]) {
				nameEnd++
			}
			if nameEnd == k+1 {
				continue
			}
			varName := expr[k+1 : nameEnd]
			afterName := nameEnd
			for afterName < len(expr) && isXMLWhitespace(expr[afterName]) {
				afterName++
			}
			if !strings.HasPrefix(expr[afterName:], "in") {
				continue
			}
			afterIn := afterName + 2
			if afterIn < len(expr) && isXPathNameChar(expr[afterIn]) {
				continue
			}
			for afterIn < len(expr) && isXMLWhitespace(expr[afterIn]) {
				afterIn++
			}
			if afterIn >= len(expr) || expr[afterIn] != '(' {
				continue
			}
			seqClose := matchCloseParen(expr, afterIn+1)
			if seqClose < 0 {
				continue
			}
			items := splitSequenceLiteral(expr[afterIn+1 : seqClose])
			if len(items) < 1 {
				continue
			}
			// Require " satisfies " after the sequence literal.
			satStart, satEnd := indexSatisfies(expr, seqClose+1)
			if satStart < 0 || satStart != seqClose+1 && !isAllWhitespace(expr[seqClose+1:satStart+1]) {
				continue
			}
			bodyEnd := captureSatisfiesBody(expr, satEnd)
			if bodyEnd <= satEnd {
				continue
			}
			body := strings.TrimSpace(expr[satEnd:bodyEnd])
			var parts []string
			for _, item := range items {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				parts = append(parts, substituteVar(body, varName, item))
			}
			if len(parts) == 0 {
				continue
			}
			joinOp := " or "
			if kw == "every" {
				joinOp = " and "
			}
			replacement := "(" + strings.Join(parts, joinOp) + ")"
			return expr[:idx] + replacement + expr[bodyEnd:]
		}
	}
	return expr
}

// captureSatisfiesBody walks forward from start (the byte index just
// past "satisfies") and returns the index where the quantifier body
// ends. The body terminates at the same boundaries quantifier
// expressions use elsewhere: matching close paren / bracket, a
// top-level boolean keyword (and / or), or end-of-input. String
// literals and nested parens are honoured so a top-level "and" in
// a sub-expression does not terminate the capture early.
func captureSatisfiesBody(expr string, start int) int {
	for start < len(expr) && isXMLWhitespace(expr[start]) {
		start++
	}
	depth := 0
	inStr := byte(0)
	end := start
	for end < len(expr) {
		c := expr[end]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			end++
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '(', '[':
			depth++
		case ')', ']':
			if depth == 0 {
				return end
			}
			depth--
		case ' ', '\t', '\n', '\r':
			if depth == 0 && isAndOrBoundary(expr, end) {
				return end
			}
		}
		end++
	}
	return end
}

func isXMLWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isAllWhitespace(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isXMLWhitespace(s[i]) {
			return false
		}
	}
	return true
}

// rewriteBooleanLiteralComparison rewrites comparisons against the
// XPath 1.0 boolean constructors false() / true() into their string-
// literal equivalents 'false' / 'true'. antchfx panics with a nil-
// pointer dereference when a node-set compares against a function-
// call boolean on certain predicate shapes (BR-CL-19's
// "ram:Indicator = false()" is the canonical case); the string-
// comparison form sidesteps the bug because the elements carry the
// literal text "false" / "true" anyway.
//
// The transform looks for the exact "= false()" / "= true()" /
// "!= false()" / "!= true()" tail (case-sensitive, no nested args)
// and replaces it with the quoted literal. The leading whitespace
// before the operator is preserved so source-level formatting stays
// readable in compile-error messages.
func rewriteBooleanLiteralComparison(expr string) string {
	if !strings.Contains(expr, "true()") && !strings.Contains(expr, "false()") {
		return expr
	}
	replacements := []struct{ from, to string }{
		{"= true()", "= 'true'"},
		{"!= true()", "!= 'true'"},
		{"= false()", "= 'false'"},
		{"!= false()", "!= 'false'"},
	}
	for _, r := range replacements {
		expr = strings.ReplaceAll(expr, r.from, r.to)
	}
	return expr
}

// rewriteTokenizeWhitespaceLiteral converts the XPath 2.0 idiom
//
//	tokenize('v1 v2 v3', '\s')
//
// into the equivalent XPath 1.0 sequence literal
//
//	('v1', 'v2', 'v3')
//
// so the downstream some / every quantifier path picks it up via
// [peelQuantifier] without needing XPath 2.0 tokenize support in
// antchfx. The transform fires only when both arguments are
// single-quoted string literals and the separator regex is one of
// the canonical whitespace patterns ('\s', '\\s', ' '); other
// tokenize forms (path-based input, regex-based splitting on
// non-whitespace, positional access like tokenize(X,':')[7]) stay
// untouched and surface via [evaluateRule]'s skip slice.
//
// EN 16931 / XRechnung / Factur-X use this idiom for "BT-XX value
// is one of {V1, V2, ...}" code-list membership checks
// (PEPPOL-EN16931-R061, FX-SCH-A-000132, BR-DE-TMP-32 etc.);
// without the rewrite the rules silently land in the skipped slice.
func rewriteTokenizeWhitespaceLiteral(expr string) string {
	const prefix = "tokenize("
	if !strings.Contains(expr, prefix) {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); {
		if !strings.HasPrefix(expr[i:], prefix) || (i > 0 && isXPathNameChar(expr[i-1])) {
			b.WriteByte(expr[i])
			i++
			continue
		}
		argStart := i + len(prefix)
		argEnd := matchCloseParen(expr, argStart)
		if argEnd < 0 {
			b.WriteByte(expr[i])
			i++
			continue
		}
		// Split on the top-level comma inside the argument list. A
		// regex argument may legally contain a comma, so the splitter
		// honours quoting depth.
		commaIdx := topLevelComma(expr[argStart:argEnd])
		if commaIdx < 0 {
			b.WriteByte(expr[i])
			i++
			continue
		}
		input := strings.TrimSpace(expr[argStart : argStart+commaIdx])
		sep := strings.TrimSpace(expr[argStart+commaIdx+1 : argEnd])
		if !isStringLiteral(input) || !isWhitespaceRegexLiteral(sep) {
			b.WriteByte(expr[i])
			i++
			continue
		}
		fields := strings.Fields(unquoteStringLiteral(input))
		if len(fields) == 0 {
			b.WriteByte(expr[i])
			i++
			continue
		}
		parts := make([]string, 0, len(fields))
		for _, f := range fields {
			parts = append(parts, "'"+strings.ReplaceAll(f, "'", "''")+"'")
		}
		b.WriteByte('(')
		b.WriteString(strings.Join(parts, ", "))
		b.WriteByte(')')
		i = argEnd + 1
	}
	return b.String()
}

// rewriteAbsToleranceComparison rewrites the XPath 2.0 idiom
//
//	abs(X) le tolerance
//	abs(X) <= tolerance
//
// into the equivalent XPath 1.0 conjunction
//
//	(X < tolerance and X > -tolerance)
//
// antchfx/xpath returns NaN for abs() of a numeric cast on a
// missing element, which silently mis-evaluates the entire
// tolerance check. Rewriting peels the abs() wrapper and recovers
// the same verdict via two strict comparisons against the
// tolerance and its negation.
//
// The "le" keyword has already been rewritten to "<=" by
// [rewriteValueComparisonOperators] when this transform runs, so
// the matcher targets "<=" only. Both sides of the comparison may
// be arbitrary expressions; X is what abs() wraps and tolerance is
// the right-hand operand.
func rewriteAbsToleranceComparison(expr string) string {
	const prefix = "abs("
	if !strings.Contains(expr, prefix) || !strings.Contains(expr, "<=") {
		return expr
	}
	for {
		idx := strings.Index(expr, prefix)
		if idx < 0 {
			return expr
		}
		if idx > 0 && isXPathNameChar(expr[idx-1]) {
			expr = expr[:idx+1] + rewriteAbsToleranceComparison(expr[idx+1:])
			return expr
		}
		argStart := idx + len(prefix)
		argEnd := matchCloseParen(expr, argStart)
		if argEnd < 0 {
			return expr
		}
		// After the abs(...) close paren, accept whitespace + "<=".
		k := argEnd + 1
		for k < len(expr) && (expr[k] == ' ' || expr[k] == '\t' || expr[k] == '\n' || expr[k] == '\r') {
			k++
		}
		if k+1 >= len(expr) || expr[k] != '<' || expr[k+1] != '=' {
			// Not a tolerance comparison. Skip past this abs() call.
			expr = expr[:idx+1] + rewriteAbsToleranceComparison(expr[idx+1:])
			return expr
		}
		rhsStart := k + 2
		rhsEnd := rhsCaptureEnd(expr, rhsStart)
		if rhsEnd <= rhsStart {
			expr = expr[:idx+1] + rewriteAbsToleranceComparison(expr[idx+1:])
			return expr
		}
		inner := strings.TrimSpace(expr[argStart:argEnd])
		tolerance := strings.TrimSpace(expr[rhsStart:rhsEnd])
		replacement := "((" + inner + ") < (" + tolerance + ") and (" + inner + ") > -(" + tolerance + "))"
		expr = expr[:idx] + replacement + expr[rhsEnd:]
	}
}

// topLevelComma returns the byte index of the first top-level comma
// in s (i.e. outside parens, brackets and quoted strings), or -1
// when none exists.
func topLevelComma(s string) int {
	depth := 0
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// isStringLiteral reports whether s is a single- or double-quoted
// XPath string literal with no embedded quotes.
func isStringLiteral(s string) bool {
	if len(s) < 2 {
		return false
	}
	q := s[0]
	if (q != '\'' && q != '"') || s[len(s)-1] != q {
		return false
	}
	for i := 1; i < len(s)-1; i++ {
		if s[i] == q {
			return false
		}
	}
	return true
}

// isWhitespaceRegexLiteral reports whether s is the literal regex
// EN 16931 uses for tokenize's whitespace separator. Accepts the
// common spellings: '\s', '\\s', ' ', "\\s", and so on.
func isWhitespaceRegexLiteral(s string) bool {
	if !isStringLiteral(s) {
		return false
	}
	body := s[1 : len(s)-1]
	switch body {
	case `\s`, `\\s`, ` `:
		return true
	}
	return false
}

// unquoteStringLiteral strips the outer quote characters from a
// validated string literal. Caller guarantees [isStringLiteral]
// returned true.
func unquoteStringLiteral(s string) string {
	return s[1 : len(s)-1]
}

// rhsCaptureEnd locates the end of the right-hand operand of a
// "<=" comparison starting at i. The walk honours paren depth and
// string literals, stopping at the next top-level boolean keyword
// (`and`, `or`) or unmatched closing paren / bracket.
func rhsCaptureEnd(expr string, start int) int {
	depth := 0
	inStr := byte(0)
	for start < len(expr) && (expr[start] == ' ' || expr[start] == '\t' || expr[start] == '\n' || expr[start] == '\r') {
		start++
	}
	end := start
	for end < len(expr) {
		c := expr[end]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			end++
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '(', '[':
			depth++
		case ')', ']':
			if depth == 0 {
				return end
			}
			depth--
		case ' ', '\t', '\n', '\r':
			if depth == 0 && isAndOrBoundary(expr, end) {
				return end
			}
		}
		end++
	}
	return end
}

// decimalEqualityEpsilon is the tolerance used when [rewriteDecimalEquality]
// converts an xs:decimal equality into an absolute-difference check.
// EN 16931 / XRechnung Schematron arithmetic rules work in monetary
// values with two decimal places, so a half-cent rounding error
// (5e-3) is the smallest meaningful divergence; 1e-6 is six orders
// of magnitude tighter than that, but several orders of magnitude
// looser than the worst-case IEEE jitter (~1e-13) of summing the
// kinds of two-decimal values these rules compare. Sized to make
// genuine errors still fire while ignoring representation noise.
const decimalEqualityEpsilon = "0.000001"

// rewriteDecimalEquality converts equality comparisons whose source
// expression marks at least one side with an XPath 2.0 numeric cast
// (xs:decimal / xs:integer / xs:double / ...) into an
// absolute-difference tolerance check. Without the rewrite, the
// rest of the pipeline collapses the cast to a number() and the
// equality runs in IEEE float, which trips bit-level on common
// monetary sums (e.g. 473.00 + 56.87 != 529.87). With the rewrite:
//
//	xs:decimal(LHS) = RHS   →  (abs((LHS) - (RHS)) < 0.000001)
//	xs:decimal(LHS) != RHS  →  (abs((LHS) - (RHS)) >= 0.000001)
//
// LHS and RHS are captured by paren-depth-aware boundary walk so the
// rewrite respects operator precedence: top-level "and"/"or" or an
// unmatched close paren terminates the capture. Strings literals are
// honoured.
//
// The rewriter targets only the equality operators (= and !=); the
// strict comparison operators (<, <=, >, >=) keep their exact
// semantics because EN 16931 rules that use them already build in
// their own tolerance (e.g. "abs(X) - 1 <= round(Y)") and would
// double-tolerance if the rewriter touched them.
//
// Unblocks BR-CO-13, BR-CO-14, BR-CO-15, BR-CO-16, and the
// VAT-category sibling rules (BR-S-08, BR-S-09, etc.) which all read
// "BT-X = BT-Y +/- BT-Z" against two-decimal monetary values.
func rewriteDecimalEquality(expr string) string {
	if !looksLikeMonetaryEquality(expr) {
		return expr
	}
	for {
		next := rewriteOneDecimalEquality(expr)
		if next == expr {
			return expr
		}
		expr = next
	}
}

func rewriteOneDecimalEquality(expr string) string {
	inStr := byte(0)
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			inStr = c
			continue
		}
		op, opLen := matchEqualityOperator(expr, i)
		if op == "" {
			continue
		}
		lhsStart, lhsEnd := captureLHS(expr, i)
		if lhsStart < 0 || lhsEnd <= lhsStart {
			continue
		}
		rhsStart, rhsEnd := captureRHS(expr, i+opLen)
		if rhsStart < 0 || rhsEnd <= rhsStart {
			continue
		}
		lhs := strings.TrimSpace(expr[lhsStart:lhsEnd])
		rhs := strings.TrimSpace(expr[rhsStart:rhsEnd])
		// looksLikeMonetaryEquality gates the transform so equality
		// comparisons that aren't arithmetic (count = 1,
		// position = last, code = 'VAT', Indicator = true()) keep
		// their exact XPath 1.0 semantics. The gate fires when a
		// numeric cast or known numeric helper appears in the
		// source on either side.
		if !looksLikeMonetaryEquality(lhs) && !looksLikeMonetaryEquality(rhs) {
			continue
		}
		// Tolerance shape: |X - Y| < eps for equality, |X - Y| >= eps
		// for inequality. Expanded as two comparisons to avoid
		// abs(): antchfx/xpath cannot coerce a boolean to a number,
		// so the canonical "X * (1 - 2 * number(X<0))" idiom yields
		// NaN universally and any abs()-based form silently mis-
		// evaluates.
		diff := "((" + lhs + ") - (" + rhs + "))"
		var replacement string
		if op == "=" {
			replacement = "(" + diff + " < " + decimalEqualityEpsilon + " and " + diff + " > -" + decimalEqualityEpsilon + ")"
		} else {
			replacement = "(" + diff + " >= " + decimalEqualityEpsilon + " or " + diff + " <= -" + decimalEqualityEpsilon + ")"
		}
		return expr[:lhsStart] + replacement + expr[rhsEnd:]
	}
	return expr
}

// captureRHS returns the byte offsets of the RHS expression that
// follows the equality operator at position start. The capture stops
// at end-of-input, an unmatched close paren/bracket at the original
// depth, or a top-level "and"/"or" keyword. String literals are
// honoured so a kw embedded in a quoted string does not terminate
// the capture.
func captureRHS(expr string, start int) (int, int) {
	for start < len(expr) && (expr[start] == ' ' || expr[start] == '\t' || expr[start] == '\n') {
		start++
	}
	if start >= len(expr) {
		return -1, -1
	}
	depth := 0
	inStr := byte(0)
	end := start
	for end < len(expr) {
		c := expr[end]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			end++
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '(', '[':
			depth++
		case ')', ']':
			if depth == 0 {
				return start, end
			}
			depth--
		case ' ', '\t', '\n':
			if depth == 0 && isAndOrBoundary(expr, end) {
				return start, end
			}
		}
		end++
	}
	return start, end
}

// isAndOrBoundary reports whether the whitespace at i in expr is
// immediately followed by the "and" or "or" keyword (with a trailing
// non-name character). Used by [captureRHS] to stop at a top-level
// boolean junction.
func isAndOrBoundary(expr string, i int) bool {
	j := i + 1
	for j < len(expr) && (expr[j] == ' ' || expr[j] == '\t' || expr[j] == '\n') {
		j++
	}
	for _, kw := range []string{"and", "or"} {
		if !strings.HasPrefix(expr[j:], kw) {
			continue
		}
		k := j + len(kw)
		if k >= len(expr) {
			return true
		}
		if !isXPathNameChar(expr[k]) {
			return true
		}
	}
	return false
}

// looksLikeMonetaryEquality reports whether expr looks like a
// monetary arithmetic expression worth threading through the
// decimal-equality tolerance rewrite. The gate fires on XPath 2.0
// numeric casts (xs:decimal-family) plus the XPath 1.0 numeric
// helpers Schematron arithmetic rules lean on (round, sum, floor,
// ceiling). Strict comparisons like "Code = 'VAT'" and "Indicator =
// true()" stay outside the rewrite because their RHS carries
// neither cast nor numeric helper, preserving exact string and
// boolean equality semantics.
func looksLikeMonetaryEquality(expr string) bool {
	if strings.Contains(expr, "xs:") {
		for _, cast := range numericCasts {
			if strings.Contains(expr, "xs:"+cast+"(") {
				return true
			}
		}
	}
	for _, fn := range []string{"round(", "sum(", "floor(", "ceiling("} {
		if strings.Contains(expr, fn) {
			return true
		}
	}
	return false
}

// rewriteDistinctValuesEqOne converts the narrow XPath 2.0 idiom
//
//	count(distinct-values(EXPR)) = 1
//
// into the XPath 1.0 equivalent
//
//	(not(EXPR) or not((EXPR)[. != (EXPR)[1]]))
//
// Semantically: when EXPR is empty, both forms say "no distinct
// values"; when EXPR is non-empty, both ask whether every member
// equals the first. CII-SR-462 is the canonical case ("only one
// DueDateTypeCode shall be present"). Other distinct-values forms
// (count = 0, count = count(EXPR), nested in arithmetic) are
// deliberately left alone; the per-rule skip in [evaluateRule]
// catches them and surfaces a coverage gap instead of producing a
// wrong verdict.
//
// EXPR is captured by matching parens, so nested function calls and
// path predicates inside the argument do not throw off the rewriter.
func rewriteDistinctValuesEqOne(expr string) string {
	target := "count(distinct-values("
	for {
		idx := strings.Index(expr, target)
		if idx < 0 {
			return expr
		}
		if idx > 0 && isXPathNameChar(expr[idx-1]) {
			expr = expr[:idx+1] + rewriteDistinctValuesEqOne(expr[idx+1:])
			return expr
		}
		innerStart := idx + len(target)
		innerEnd := matchCloseParen(expr, innerStart)
		if innerEnd < 0 {
			return expr
		}
		// After the inner ")", the next byte should be the close of
		// count(...).
		if innerEnd+1 >= len(expr) || expr[innerEnd+1] != ')' {
			return expr
		}
		countClose := innerEnd + 1
		// Look for " = 1" (whitespace flexible) after count(...).
		k := countClose + 1
		for k < len(expr) && (expr[k] == ' ' || expr[k] == '\t' || expr[k] == '\n') {
			k++
		}
		if k >= len(expr) || expr[k] != '=' {
			return expr
		}
		k++
		for k < len(expr) && (expr[k] == ' ' || expr[k] == '\t' || expr[k] == '\n') {
			k++
		}
		if k >= len(expr) || expr[k] != '1' {
			return expr
		}
		if k+1 < len(expr) {
			next := expr[k+1]
			if next >= '0' && next <= '9' {
				return expr
			}
			if next == '.' {
				return expr
			}
		}
		inner := expr[innerStart:innerEnd]
		replacement := "(not(" + inner + ") or not((" + inner + ")[. != (" + inner + ")[1]]))"
		expr = expr[:idx] + replacement + expr[k+1:]
	}
}

// rewriteSequenceEquality converts XPath 2.0 sequence-literal
// comparisons of the form
//
//	EXPR = ('a', 'b', 'c')
//	EXPR != ('a', 'b', 'c')
//
// into the XPath 1.0 disjunction
//
//	(EXPR = 'a' or EXPR = 'b' or EXPR = 'c')
//	(EXPR != 'a' and EXPR != 'b' and EXPR != 'c')
//
// The XRechnung Schematron leans on this pattern for code-list
// containment checks (BR-DE-17, BR-DE-19, BR-DE-23, BR-DE-24) which
// would otherwise skip wholesale on the trailing parenthesis-comma
// triggering antchfx/xpath's "has an invalid token" error.
//
// EXPR is conservatively captured backwards from the operator until
// a balancing whitespace or operator boundary; the rewriter walks
// each match individually so adjacent sequence literals do not get
// over-matched.
func rewriteSequenceEquality(expr string) string {
	out := expr
	for {
		next := rewriteOneSequenceEquality(out)
		if next == out {
			return out
		}
		out = next
	}
}

func rewriteOneSequenceEquality(expr string) string {
	for i := 0; i < len(expr); i++ {
		op, opLen := matchEqualityOperator(expr, i)
		if op == "" {
			continue
		}
		// Find the opening paren of the sequence literal that
		// follows the operator (whitespace allowed in between).
		k := i + opLen
		for k < len(expr) && (expr[k] == ' ' || expr[k] == '\n' || expr[k] == '\t') {
			k++
		}
		if k >= len(expr) || expr[k] != '(' {
			continue
		}
		closeIdx := matchCloseParen(expr, k+1)
		if closeIdx < 0 {
			continue
		}
		items := splitSequenceLiteral(expr[k+1 : closeIdx])
		if len(items) < 2 {
			// One-item sequence or empty literal: probably not the
			// XPath 2.0 sequence comparison this pass targets.
			continue
		}
		exprStart, exprEnd := captureLHS(expr, i)
		if exprStart < 0 {
			continue
		}
		lhs := strings.TrimSpace(expr[exprStart:exprEnd])
		joinOp := " or "
		perItemOp := " = "
		if op == "!=" {
			joinOp = " and "
			perItemOp = " != "
		}
		var parts []string
		for _, item := range items {
			parts = append(parts, lhs+perItemOp+strings.TrimSpace(item))
		}
		replacement := "(" + strings.Join(parts, joinOp) + ")"
		return expr[:exprStart] + replacement + expr[closeIdx+1:]
	}
	return expr
}

// matchEqualityOperator detects "=" or "!=" at position i in expr.
// Returns the operator string and its byte length, or ("",0) when
// no operator starts at i.
func matchEqualityOperator(expr string, i int) (string, int) {
	if i+1 < len(expr) && expr[i] == '!' && expr[i+1] == '=' {
		return "!=", 2
	}
	if expr[i] == '=' {
		// Skip "==" which is not an XPath operator but might be a
		// user typo; also skip "<=" / ">=" by checking the prior byte.
		if i > 0 && (expr[i-1] == '<' || expr[i-1] == '>' || expr[i-1] == '!' || expr[i-1] == '=') {
			return "", 0
		}
		if i+1 < len(expr) && expr[i+1] == '=' {
			return "", 0
		}
		return "=", 1
	}
	return "", 0
}

// captureLHS walks backwards from the operator at opIdx to find the
// start of the EXPR being compared. The capture honours XPath name
// characters, predicate brackets, and balanced parens so an
// expression like normalize-space(x/y) becomes the LHS atomically.
func captureLHS(expr string, opIdx int) (start, end int) {
	end = opIdx
	for end > 0 && (expr[end-1] == ' ' || expr[end-1] == '\t' || expr[end-1] == '\n') {
		end--
	}
	start = end
	depth := 0
	for start > 0 {
		c := expr[start-1]
		switch c {
		case ')', ']':
			depth++
		case '(', '[':
			depth--
			if depth < 0 {
				return start, end
			}
		case ' ', '\t', '\n':
			if depth == 0 {
				return start, end
			}
		}
		if depth == 0 {
			// Stop on operators (=, !=, <, >, and, or, +, -, *, /, etc.)
			// at the top level. Recognising every operator would be its
			// own parser; conservatively break on common XPath delimiters.
			if c == '|' || c == ',' {
				return start, end
			}
		}
		start--
	}
	return start, end
}

// rewriteBareSequenceAsDisjunction converts a top-level XPath 2.0
// sequence construction "(X, Y, Z)" used in a boolean context into
// "(boolean(X) or boolean(Y) or boolean(Z))". Identifies grouping
// parens (those not following an identifier name, i.e. not a
// function-call argument list) and checks for a top-level comma; a
// comma means the parens enclose a sequence literal rather than a
// parenthesised single expression.
//
// XRechnung's BR-DE-16 leans on this pattern to assert "X exists or
// Y exists" in one step. XPath 2.0 interprets the sequence in a
// boolean position as true when any member yields a non-empty
// node-set; XPath 1.0's "or" with implicit boolean coercion
// reproduces the same verdict.
func rewriteBareSequenceAsDisjunction(expr string) string {
	for {
		next, changed := rewriteOneBareSequence(expr)
		if !changed {
			return expr
		}
		expr = next
	}
}

func rewriteOneBareSequence(expr string) (string, bool) {
	for i := 0; i < len(expr); i++ {
		if expr[i] != '(' {
			continue
		}
		if isFunctionCallParen(expr, i) {
			continue
		}
		end := matchCloseParen(expr, i+1)
		if end < 0 {
			continue
		}
		items := splitSequenceLiteral(expr[i+1 : end])
		if len(items) < 2 {
			continue
		}
		// Skip sequences whose items look like equality comparisons:
		// those were the target of rewriteSequenceEquality and any
		// leftover commas here indicate a different construct (e.g.
		// step predicates) that the rewriter should leave alone.
		safe := true
		for _, item := range items {
			if !looksLikePathOrCall(strings.TrimSpace(item)) {
				safe = false
				break
			}
		}
		if !safe {
			continue
		}
		var parts []string
		for _, item := range items {
			parts = append(parts, "boolean("+strings.TrimSpace(item)+")")
		}
		replacement := "(" + strings.Join(parts, " or ") + ")"
		return expr[:i] + replacement + expr[end+1:], true
	}
	return expr, false
}

// isFunctionCallParen reports whether the open paren at position i
// in expr belongs to a function call (preceding non-whitespace byte
// is an XPath name char). Grouping parens follow operators, the
// start of expression, or another open paren.
func isFunctionCallParen(expr string, i int) bool {
	j := i - 1
	for j >= 0 && (expr[j] == ' ' || expr[j] == '\t' || expr[j] == '\n') {
		j--
	}
	if j < 0 {
		return false
	}
	return isXPathNameChar(expr[j])
}

// looksLikePathOrCall returns true when s is plausibly a path
// expression or a function call: starts with a letter, slash, or
// dot, and contains no operator that would indicate this is a
// comparison or arithmetic expression (those are deliberately left
// alone).
//
// Paren-wrapped paths (e.g. "(rsm:.../ram:ID)" produced by let
// substitution) are recognised by peeling matched outer parens; this
// keeps the sequence-as-disjunction rewriter applicable to expressions
// that have already been through substitution.
func looksLikePathOrCall(s string) bool {
	for len(s) >= 2 && s[0] == '(' {
		end := matchCloseParen(s, 1)
		if end != len(s)-1 {
			break
		}
		s = strings.TrimSpace(s[1:end])
	}
	if s == "" {
		return false
	}
	c := s[0]
	letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	if c != '/' && c != '.' && c != '@' && c != '_' && !letter {
		return false
	}
	// Reject any top-level operator that would make the item a
	// comparison or arithmetic expression rather than a bare path.
	depth := 0
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '=', '<', '>', '+':
			if depth == 0 {
				return false
			}
		}
	}
	return true
}

// indexSatisfies returns the byte offsets of the top-level
// "satisfies" keyword starting at or after pos. The first return is
// the position of the whitespace immediately preceding "satisfies"
// (so the caller can slice the path expression up to that point);
// the second return is the position just past the keyword.
// Returns (-1, -1) when no top-level "satisfies" exists.
//
// Paren/bracket depth and quoted strings are honoured so a
// "satisfies" nested inside a sub-expression or string literal
// does not get picked up.
func indexSatisfies(expr string, pos int) (int, int) {
	depth := 0
	inStr := byte(0)
	for i := pos; i < len(expr); i++ {
		c := expr[i]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
			continue
		case '(', '[':
			depth++
			continue
		case ')', ']':
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			continue
		}
		// Scan forward over whitespace to find the next token. CR is
		// included so CRLF line endings inside the schematron source
		// (the bundled Factur-X profile schematrons use them) do not
		// block the "satisfies" lookup.
		k := i + 1
		for k < len(expr) && (expr[k] == ' ' || expr[k] == '\t' || expr[k] == '\n' || expr[k] == '\r') {
			k++
		}
		if strings.HasPrefix(expr[k:], "satisfies") {
			endByte := k + len("satisfies")
			// "satisfies" must be followed by a non-name character
			// so "satisfiesX" doesn't match.
			if endByte >= len(expr) || !isXPathNameChar(expr[endByte]) {
				return i, endByte
			}
		}
	}
	return -1, -1
}

// substituteVar replaces every "$name" reference in expr with the
// supplied replacement. Honours XPath name-character boundaries so
// "$line" does not get mistaken for the start of "$lineage".
func substituteVar(expr, name, replacement string) string {
	target := "$" + name
	if !strings.Contains(expr, target) {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); {
		if strings.HasPrefix(expr[i:], target) {
			tail := i + len(target)
			if tail < len(expr) && isXPathNameChar(expr[tail]) {
				b.WriteByte(expr[i])
				i++
				continue
			}
			b.WriteString(replacement)
			i = tail
			continue
		}
		b.WriteByte(expr[i])
		i++
	}
	return b.String()
}

// splitSequenceLiteral splits the inside of a sequence literal on
// commas at depth zero, respecting quoted string literals.
func splitSequenceLiteral(body string) []string {
	var out []string
	depth := 0
	start := 0
	inStr := byte(0)
	for i := 0; i < len(body); i++ {
		c := body[i]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, body[start:])
	return out
}

// rewriteFnRename replaces every fn(EXPR) call with replacement(EXPR).
// Used to redirect XPath 2.0 function names to their XPath 1.0
// equivalents when the semantics match exactly. For example,
// exists(x) and boolean(x) return the same value for any node set
// or sequence: empty maps to false, non-empty to true.
func rewriteFnRename(expr, fn, replacement string) string {
	prefix := fn + "("
	if !strings.Contains(expr, prefix) {
		return expr
	}
	// Replace only at function-call boundaries: the byte preceding
	// "fn(" must not be a name character, otherwise the rewrite would
	// catch "myExists(" or similar.
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); {
		if strings.HasPrefix(expr[i:], prefix) && (i == 0 || !isXPathNameChar(expr[i-1])) {
			b.WriteString(replacement)
			b.WriteByte('(')
			i += len(prefix)
			continue
		}
		b.WriteByte(expr[i])
		i++
	}
	return b.String()
}

const (
	upperCaseLo = "abcdefghijklmnopqrstuvwxyz"
	upperCaseUp = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	lowerCaseLo = "abcdefghijklmnopqrstuvwxyz"
	lowerCaseUp = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// rewriteCaseFunction rewrites every fn(EXPR) call into
// translate(EXPR, from, to). Walking byte-by-byte to handle nested
// parens correctly: regex with a single capture group misses cases
// where the argument itself contains parens, which EN 16931 uses
// often (e.g. upper-case(normalize-space(x))).
func rewriteCaseFunction(expr, fn, from, to string) string {
	prefix := fn + "("
	if !strings.Contains(expr, prefix) {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); {
		if strings.HasPrefix(expr[i:], prefix) {
			start := i + len(prefix)
			end := matchCloseParen(expr, start)
			if end < 0 {
				b.WriteByte(expr[i])
				i++
				continue
			}
			arg := expr[start:end]
			b.WriteString("translate(")
			b.WriteString(arg)
			b.WriteString(",'")
			b.WriteString(from)
			b.WriteString("','")
			b.WriteString(to)
			b.WriteString("')")
			i = end + 1
			continue
		}
		b.WriteByte(expr[i])
		i++
	}
	return b.String()
}

// matchCloseParen returns the index of the close paren that matches
// the open paren conceptually preceding start. The function counts
// parens and respects single/double-quoted XPath string literals so
// a paren inside a string does not throw off the depth counter.
// Returns -1 when no match exists.
func matchCloseParen(s string, start int) int {
	depth := 1
	i := start
	for i < len(s) {
		c := s[i]
		switch c {
		case '\'', '"':
			// Skip the entire string literal.
			j := i + 1
			for j < len(s) && s[j] != c {
				j++
			}
			if j >= len(s) {
				return -1
			}
			i = j + 1
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}
