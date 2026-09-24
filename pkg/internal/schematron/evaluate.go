package schematron

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/antchfx/xmlquery"
	"github.com/antchfx/xpath"
)

// SkippedRule captures one assertion or report that could not be
// evaluated. The evaluator silently skips such rules so an
// unsupported XPath construct in one rule does not abort the whole
// pass; this surface lets callers audit coverage instead of guessing
// from missing violations.
type SkippedRule struct {
	RuleID  string
	Context string
	Reason  string
}

// Violation is one Schematron assertion firing on a context node.
// RuleID and Flag/Role come from the source schema; Path locates
// the context node in the document for diagnostics.
//
//nolint:errname // mirrors the compliance.Violation precedent; "ViolationError" would read redundantly.
type Violation struct {
	RuleID  string
	Flag    string
	Role    string
	Path    string
	Message string
	// Negate reflects the source kind: false for sch:assert, true
	// for sch:report. Useful when callers want to render the two
	// shapes with different prefixes.
	Negate bool
}

// Error implements the error interface so callers can return
// Violations alongside other errors.
func (v Violation) Error() string {
	kind := "assert"
	if v.Negate {
		kind = "report"
	}
	if v.Path != "" {
		return fmt.Sprintf("schematron %s %s at %s: %s", kind, v.RuleID, v.Path, v.Message)
	}
	return fmt.Sprintf("schematron %s %s: %s", kind, v.RuleID, v.Message)
}

// EvalOption configures [Validate]. Closures must be safe to call
// from any goroutine.
type EvalOption func(*evalOpts)

type evalOpts struct {
	phase     string
	overrides map[string]RuleOverride
}

// RuleOverride is a Go implementation of one Schematron assertion or
// report test. It receives the matched context node and returns the
// boolean that the rule's XPath test expression would have produced:
//
//   - For sch:assert: true means the rule passes (no violation).
//   - For sch:report: true means the rule fires (one violation).
//
// Overrides exist so callers can handle rules whose XPath uses
// XPath 2.0 constructs the bundled engine does not implement
// (for/return, if/then/else, string-to-codepoints). The evaluator
// consults the override map before attempting XPath compilation; a
// registered override always takes precedence. When an override
// returns an error, the rule lands in the skipped slice with the
// error's message as the reason, matching the shape of an XPath skip.
//
// Implementations must be safe to call from any goroutine; [Validate]
// may invoke the same override concurrently against different context
// nodes when patterns share a rule id.
type RuleOverride func(ctx context.Context, node *xmlquery.Node) (bool, error)

// WithPhase restricts evaluation to the patterns activated by the
// named phase. When the option is absent, [Validate] runs every
// pattern in source order.
func WithPhase(id string) EvalOption {
	return func(o *evalOpts) { o.phase = id }
}

// WithRuleOverrides registers per-assertion Go evaluators that take
// precedence over the bundled XPath engine. The map key is the
// assertion's id attribute (e.g. "BR-DE-19"); the value is the
// Go function that decides whether the rule passes for a given
// context node. Unknown ids in the map are ignored.
//
// Use this to handle rules whose XPath the engine cannot compile or
// evaluate. The companion package cii_schematron ships
// cii_schematron.BuiltinRuleOverrides with overrides for known
// holes in the EN 16931 / XRechnung rule sets.
func WithRuleOverrides(overrides map[string]RuleOverride) EvalOption {
	return func(o *evalOpts) { o.overrides = overrides }
}

// Validate parses payload, walks schema's active patterns, and
// returns one Violation per assertion that fired on a context match.
// Rule selection follows the Schematron convention: within a
// pattern, the first rule whose context matches a node wins for
// that node; later rules in the same pattern skip that node.
//
// The skipped return lists every assertion or report whose XPath
// could not compile or evaluate. The evaluator silently drops these
// so a single unsupported construct does not abort the pass; the
// list lets callers audit the coverage gap.
//
// payload must be well-formed XML. A parse error surfaces as the
// third return value distinct from violations so callers can tell
// "ran-but-failed-rules" apart from "could-not-run".
func Validate(ctx context.Context, schema *Schema, payload []byte, opts ...EvalOption) ([]Violation, []SkippedRule, error) {
	cfg := evalOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	if schema == nil {
		return nil, nil, fmt.Errorf("schematron: schema is nil")
	}
	doc, err := xmlquery.Parse(bytes.NewReader(payload))
	if err != nil {
		return nil, nil, fmt.Errorf("schematron: parsing payload: %w", err)
	}

	active, err := selectPatterns(schema, cfg.phase)
	if err != nil {
		return nil, nil, err
	}

	ns := namespaceMap(schema)
	cache := newExprCache(ns)

	// Schema-level lets resolve once and stay constant for every
	// pattern below. Their XPath text substitutes into pattern lets,
	// rule contexts, and assertion tests before XPath compilation
	// (see [resolveLevelLets]).
	//
	// XRechnung and similar schemas adopt a "variable-pattern"
	// convention: declare constants in a dedicated pattern whose
	// lets are referenced by every other pattern. Strict
	// per-pattern scoping would break that idiom, so the schema's
	// pattern lets fold into the shared map up front, in source
	// order so a later pattern can reference an earlier one.
	schemaVars := resolveLevelLets(schema.Lets, nil)
	for _, pid := range schema.PatternOrder {
		pat := schema.Patterns[pid]
		if pat == nil {
			continue
		}
		schemaVars = resolveLevelLets(pat.Lets, schemaVars)
	}

	var (
		out     []Violation
		skipped []SkippedRule
	)
	for _, pid := range active {
		if err := ctx.Err(); err != nil {
			return out, skipped, err
		}
		pat := schema.Patterns[pid]
		if pat == nil {
			continue
		}
		v, s := evaluatePattern(ctx, doc, pat, cache, schemaVars, cfg.overrides)
		out = append(out, v...)
		skipped = append(skipped, s...)
	}
	return out, skipped, nil
}

// resolveLevelLets folds a list of let bindings into a textual
// substitution map. Bindings are processed in source order so a
// later let may reference an earlier one. Each let's value is
// kept as its source XPath (with prior $vars already substituted)
// rather than evaluated to a literal: path-valued lets, e.g.
// "rsm:.../ram:IBANID", must remain XPath subexpressions so the
// references they participate in (boolean coercion, sequence
// membership) evaluate correctly against the live document. A
// literal-evaluated path collapses to an empty string and flips
// the verdict.
//
// Parentheses wrap each substituted value so the precedence of
// surrounding operators does not chop the let body in half.
func resolveLevelLets(lets []Let, inherited map[string]string) map[string]string {
	out := make(map[string]string, len(inherited)+len(lets))
	maps.Copy(out, inherited)
	for _, lt := range lets {
		out[lt.Name] = "(" + substituteLets(lt.Value, out) + ")"
	}
	return out
}

// selectPatterns returns the ordered pattern ids that the requested
// phase activates. When phase is empty, returns every pattern in
// source order. An unknown phase surfaces as an error so misconfigured
// callers fail fast.
func selectPatterns(s *Schema, phase string) ([]string, error) {
	if phase == "" {
		out := make([]string, len(s.PatternOrder))
		copy(out, s.PatternOrder)
		return out, nil
	}
	ph, ok := s.Phases[phase]
	if !ok {
		return nil, fmt.Errorf("schematron: phase %q not declared in schema", phase)
	}
	return ph.Active, nil
}

// evaluatePattern walks every rule in pat against doc. The standard
// Schematron rule-selection semantics applies: within a pattern, the
// first rule whose context matches a node "claims" that node, and
// later rules skip nodes already claimed. Rules whose context XPath
// fails to compile or evaluate silently skip; the second return
// reports them so callers can audit Schematron coverage.
func evaluatePattern(ctx context.Context, doc *xmlquery.Node, pat *Pattern, cache *exprCache, schemaVars map[string]string, overrides map[string]RuleOverride) ([]Violation, []SkippedRule) {
	claimed := make(map[*xmlquery.Node]bool)
	var (
		out     []Violation
		skipped []SkippedRule
	)
	patternVars := resolveLevelLets(pat.Lets, schemaVars)
	for _, rule := range pat.Rules {
		if err := ctx.Err(); err != nil {
			return out, skipped
		}
		// Apply schema + pattern lets to the rule context. Rule-scoped
		// lets evaluate per context match inside evaluateRule because
		// they may depend on the matched node.
		contextExpr := substituteLets(rule.Context, patternVars)
		// ISO Schematron 5.4.3: a rule's context is shorthand for
		// "//<context>" so the rule fires on every descendant of the
		// document that matches. A bare relative path like
		// "ram:URIUniversalCommunication/ram:URIID" would otherwise
		// resolve to children of the document node and silently miss
		// every actual match.
		contextExpr = anchorRuleContext(contextExpr)
		nodes, err := selectNodes(doc, contextExpr, cache)
		if err != nil {
			// Whole rule unevaluable: record one SkippedRule per
			// assertion the rule would have run, since each
			// assertion identity matters for coverage tallies.
			for _, a := range rule.Asserts {
				skipped = append(skipped, SkippedRule{
					RuleID: a.ID, Context: rule.Context,
					Reason: fmt.Sprintf("context xpath: %v", err),
				})
			}
			for _, r := range rule.Reports {
				skipped = append(skipped, SkippedRule{
					RuleID: r.ID, Context: rule.Context,
					Reason: fmt.Sprintf("context xpath: %v", err),
				})
			}
			continue
		}
		for _, n := range nodes {
			if claimed[n] {
				continue
			}
			claimed[n] = true
			v, s := evaluateRule(ctx, rule, n, cache, patternVars, overrides)
			out = append(out, v...)
			skipped = append(skipped, s...)
		}
	}
	return out, skipped
}

// evaluateRule applies every assertion in rule against the single
// context node n. Let bindings declared on the rule are substituted
// textually into each assertion's test expression before
// compilation; assertions whose test expression fails to compile or
// evaluate get silently skipped so a single unsupported XPath
// construct does not abort the whole pass.
//
// The silent-skip behaviour exists because real-world Schematron
// schemas (EN 16931, XRechnung) lean on XPath 2.0 functions and
// xsl:function helpers that antchfx/xpath does not implement. A
// per-rule abort would leave most documents producing zero
// violations, which is worse than no Schematron at all. The second
// return lists the skipped rules so callers can audit coverage.
func evaluateRule(ctx context.Context, rule *Rule, n *xmlquery.Node, cache *exprCache, inherited map[string]string, overrides map[string]RuleOverride) ([]Violation, []SkippedRule) {
	// Rule lets stack on top of the inherited schema + pattern lets.
	// The inherited map is copied (not mutated) so concurrent calls
	// on different match nodes do not stomp each other.
	vars := resolveLevelLets(rule.Lets, inherited)

	out := make([]Violation, 0, len(rule.Asserts)+len(rule.Reports))
	var skipped []SkippedRule
	for _, a := range rule.Asserts {
		fired, err := runAssertion(ctx, a, n, vars, cache, overrides)
		if err != nil {
			skipped = append(skipped, SkippedRule{
				RuleID: a.ID, Context: rule.Context,
				Reason: err.Error(),
			})
			continue
		}
		if !fired {
			out = append(out, makeViolation(rule, a, n))
		}
	}
	for _, r := range rule.Reports {
		fired, err := runAssertion(ctx, r, n, vars, cache, overrides)
		if err != nil {
			skipped = append(skipped, SkippedRule{
				RuleID: r.ID, Context: rule.Context,
				Reason: err.Error(),
			})
			continue
		}
		if fired {
			out = append(out, makeViolation(rule, r, n))
		}
	}
	return out, skipped
}

// runAssertion evaluates a single assertion: a registered override
// wins over the XPath engine, otherwise the test compiles and runs
// against the bundled antchfx/xpath, with one wrinkle: a top-level
// XPath 2.0 quantifier (every / some) is peeled off and evaluated in
// Go before handing the body to antchfx. antchfx misparses bare
// quantifiers (the resulting iterator is always empty) and the
// textual "not(PATH[not(BODY)])" rewrite trips an antchfx bug where
// absolute paths inside the predicate context resolve to nothing.
// Looping in Go and substituting the bound variable as a string
// literal lets the body run in the original rule context.
//
// The boolean returned matches the test expression's verdict;
// callers translate that into pass/fire per assertion kind.
func runAssertion(ctx context.Context, a *Assertion, n *xmlquery.Node, vars map[string]string, cache *exprCache, overrides map[string]RuleOverride) (bool, error) {
	if a.ID != "" {
		if fn, ok := overrides[a.ID]; ok {
			return fn(ctx, n)
		}
	}
	expr := substituteLets(a.Test, vars)
	// Prefix every top-level relative path with the rule context's
	// positional XPath, then evaluate against the document root.
	// antchfx/xpath silently breaks "/abs" and "//abs" paths when the
	// navigator's current node is not the document root, so the
	// expression is rewritten and the navigator re-rooted at the
	// document so both absolute and relative paths resolve correctly.
	prefix := positionalXPath(n)
	if prefix != "" {
		expr = prefixRelativePaths(expr, prefix)
		return evalBooleanWithQuantifier(expr, documentRoot(n), cache)
	}
	return evalBooleanWithQuantifier(expr, n, cache)
}

// evalBooleanWithQuantifier peels a top-level XPath 2.0 quantifier
// off expr and evaluates its bindings in Go, then recurses on the
// substituted body. When expr is not a quantifier it falls straight
// through to [evalBoolean] against antchfx.
func evalBooleanWithQuantifier(expr string, n *xmlquery.Node, cache *exprCache) (bool, error) {
	// XPath 2.0 "for $V in ... return ..." let-list expressions
	// compile cleanly under antchfx but evaluate as garbled name
	// references, silently producing false on perfectly valid
	// invoices. Skipping the rule surfaces the coverage gap via the
	// SkippedRule slice rather than firing a spurious violation.
	// Detection is conservative: only top-level "for $name in"
	// triggers the skip; nested forms inside parentheses fall through
	// to the regular compile path.
	if hasLeadingForBinding(expr) {
		return false, fmt.Errorf("xpath 2.0 'for $var in ... return ...' let-list not supported")
	}
	kind, varName, path, body, ok := peelQuantifier(expr)
	if !ok {
		return evalBoolean(expr, n, cache)
	}
	root := documentRoot(n)
	bindings, err := selectNodes(root, path, cache)
	if err != nil {
		return false, fmt.Errorf("quantifier path: %w", err)
	}
	isEvery := kind == "every"
	if len(bindings) == 0 {
		// XPath 2.0 vacuous truth: "every" on an empty sequence is
		// true; "some" on an empty sequence is false.
		return isEvery, nil
	}
	for _, b := range bindings {
		literal := xpathStringLiteral(b.InnerText())
		substituted := substituteVar(body, varName, literal)
		// Evaluate against the document root, not the rule context:
		// quantifier bodies overwhelmingly use absolute paths, and
		// antchfx/xpath silently resolves "/foo" to an empty node-set
		// when the context is not the document node. The bound
		// variable has already been substituted to a string literal,
		// so the body no longer references the rule context.
		fired, err := evalBooleanWithQuantifier(substituted, root, cache)
		if err != nil {
			return false, err
		}
		if isEvery && !fired {
			return false, nil
		}
		if !isEvery && fired {
			return true, nil
		}
	}
	return isEvery, nil
}

// hasLeadingForBinding reports whether expr begins with an XPath 2.0
// "for $V in ..." let-list. The check is whitespace-tolerant and
// uses the same boundary rules as [peelQuantifier]: the "for" keyword
// must not run into another name character, and the next non-space
// byte must be the dollar sign that introduces the bound variable.
func hasLeadingForBinding(expr string) bool {
	const ws = " \t\n\r"
	s := strings.TrimLeft(expr, ws)
	if !strings.HasPrefix(s, "for") {
		return false
	}
	after := s[3:]
	if after == "" || isXPathNameChar(after[0]) {
		return false
	}
	after = strings.TrimLeft(after, ws)
	return strings.HasPrefix(after, "$")
}

// peelQuantifier matches a top-level "every $V in PATH satisfies
// BODY" or "some $V in PATH satisfies BODY" prefix on expr. Returns
// ok=false when expr does not start with a quantifier or the syntax
// is malformed.
//
// Top-level only: the parser does not recurse into parenthesised
// groups or boolean disjunctions, which means
// "(every $V ...) or X" stays opaque to it. The bundled EN 16931
// rule set keeps every quantifier at the top of its assertion test,
// so the narrow scope matches the in-the-wild usage.
func peelQuantifier(expr string) (kind, varName, path, body string, ok bool) {
	// Trim every form of XPath whitespace (space, tab, LF, CR): the
	// bundled Factur-X profile schematrons ship with CRLF line endings
	// inside test attributes, and missing CR from the trim leaves an
	// orphan carriage return between the variable name and the "in"
	// keyword, which silently breaks peelQuantifier on every "every $V
	// in ..." rule.
	const ws = " \t\n\r"
	s := strings.TrimLeft(expr, ws)
	for _, kw := range []string{"every", "some"} {
		if !strings.HasPrefix(s, kw) {
			continue
		}
		after := s[len(kw):]
		if after == "" || isXPathNameChar(after[0]) {
			continue
		}
		after = strings.TrimLeft(after, ws)
		if !strings.HasPrefix(after, "$") {
			continue
		}
		nameEnd := 1
		for nameEnd < len(after) && isXPathNameChar(after[nameEnd]) {
			nameEnd++
		}
		if nameEnd == 1 {
			continue
		}
		v := after[1:nameEnd]
		afterName := strings.TrimLeft(after[nameEnd:], ws)
		if !strings.HasPrefix(afterName, "in") || len(afterName) < 3 || isXPathNameChar(afterName[2]) {
			continue
		}
		afterIn := strings.TrimLeft(afterName[2:], ws)
		// Find the matching " satisfies " keyword at depth 0
		// relative to afterIn so a nested satisfies inside the path
		// does not get picked up.
		satStart, satEnd := indexSatisfies(afterIn, 0)
		if satStart < 0 {
			continue
		}
		path := strings.TrimSpace(afterIn[:satStart])
		body := strings.TrimSpace(afterIn[satEnd:])
		return kw, v, path, body, true
	}
	return "", "", "", "", false
}

// anchorRuleContext prefixes the rule context expression with "//"
// when it starts with a name token, matching the ISO Schematron
// implicit-descendant semantics. Absolute paths ("/abs" or "//abs"),
// expressions wrapped in parens, predicates, or already-prefixed
// forms pass through unchanged.
func anchorRuleContext(expr string) string {
	s := strings.TrimLeft(expr, " \t\n")
	if s == "" {
		return expr
	}
	switch s[0] {
	case '/', '(', '$', '.', '*':
		return expr
	}
	if isNameStart(s[0]) {
		return "//" + s
	}
	return expr
}

// documentRoot returns the topmost ancestor of n. Used by
// [evalBooleanWithQuantifier] so the quantifier's path expression
// resolves against the document root rather than the rule context,
// matching XPath 2.0 semantics.
func documentRoot(n *xmlquery.Node) *xmlquery.Node {
	for n != nil && n.Parent != nil {
		n = n.Parent
	}
	return n
}

// positionalXPath builds a document-root-anchored XPath that
// resolves uniquely to n. The path uses each ancestor's element
// name plus a 1-based positional predicate among siblings sharing
// the same name, e.g. "/rsm:CrossIndustryInvoice/rsm:SupplyChain
// TradeTransaction/ram:IncludedSupplyChainTradeLineItem[2]".
// Returns the empty string when n is the document node or nil so
// callers can skip the rewrite.
//
// Used by [runAssertion] so the test expression can be evaluated
// against the document root with relative paths prefixed; antchfx's
// absolute-path resolution from non-root contexts is broken, and
// re-rooting the navigator is the cleanest workaround.
func positionalXPath(n *xmlquery.Node) string {
	if n == nil || n.Parent == nil {
		return ""
	}
	var parts []string
	for cur := n; cur != nil && cur.Parent != nil; cur = cur.Parent {
		if cur.Type != xmlquery.ElementNode {
			continue
		}
		name := cur.Data
		if cur.Prefix != "" {
			name = cur.Prefix + ":" + cur.Data
		}
		pos := 1
		for sib := cur.PrevSibling; sib != nil; sib = sib.PrevSibling {
			if sib.Type != xmlquery.ElementNode {
				continue
			}
			sibName := sib.Data
			if sib.Prefix != "" {
				sibName = sib.Prefix + ":" + sib.Data
			}
			if sibName == name {
				pos++
			}
		}
		parts = append([]string{fmt.Sprintf("%s[%d]", name, pos)}, parts...)
	}
	return "/" + strings.Join(parts, "/")
}

// prefixRelativePaths walks expr and prepends "prefix/" to every
// top-level relative path-start. Absolute paths ("/abs", "//abs"),
// variables ("$var"), string literals, numeric literals, function
// calls, and paths inside a predicate "[...]" are left alone:
// predicates resolve relative to the matched step, not the rule
// context.
//
// The rewriter recognises path-starts at:
//
//   - The beginning of the expression.
//   - Immediately after "(", "[", or "," (function args, predicates,
//     grouping).
//   - Immediately after an arithmetic / comparison / boolean operator
//     keyword.
//
// At a path-start, a name-character or "." or "@" introduces a
// relative path step. A name immediately followed by "(" is a
// function call; the rewriter looks inside the function arguments
// for its own path-starts but leaves the function name alone.
//
// XPath 1.0 is regular enough that a hand-rolled tokenizer is
// reliable for the constructs Schematron rules use; a full parser is
// not necessary for the bundled EN 16931 / XRechnung sets.
func prefixRelativePaths(expr, prefix string) string {
	if prefix == "" {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr) + 32)

	bracketDepth := 0
	inStr := byte(0)
	pathStart := true
	// stepFuncStack tracks, per open paren, whether the paren
	// introduces the argument list of a path-step function call
	// (the "/xs:decimal(...)" shape). When the top of the stack is
	// true, paths inside resolve against the path step's context,
	// not the rule context, so the prefix must not be applied.
	stepFuncStack := []bool{}

	for i := 0; i < len(expr); {
		c := expr[i]
		if inStr != 0 {
			b.WriteByte(c)
			if c == inStr {
				inStr = 0
			}
			i++
			continue
		}
		switch c {
		case '\'', '"':
			b.WriteByte(c)
			inStr = c
			i++
			continue
		case '[':
			bracketDepth++
			b.WriteByte(c)
			i++
			pathStart = true
			continue
		case ']':
			bracketDepth--
			b.WriteByte(c)
			i++
			pathStart = false
			continue
		case '(':
			// Push the path-step flag for whatever this paren
			// opens. Determined by what's been written so far: if
			// a name token immediately precedes "(", it's a
			// function call; if the byte before the name is "/",
			// the function's result is consumed as a path step
			// and its arguments resolve in the step's context
			// rather than the rule context.
			stepFuncStack = append(stepFuncStack, isFollowingPathStepFuncCall(&b))
			b.WriteByte(c)
			i++
			pathStart = true
			continue
		case ',':
			b.WriteByte(c)
			i++
			pathStart = true
			continue
		case ')':
			if len(stepFuncStack) > 0 {
				stepFuncStack = stepFuncStack[:len(stepFuncStack)-1]
			}
			b.WriteByte(c)
			i++
			pathStart = false
			continue
		case ' ', '\t', '\n':
			b.WriteByte(c)
			i++
			continue
		}
		if op, opLen := matchInfixOperator(expr, i); op != "" {
			b.WriteString(expr[i : i+opLen])
			i += opLen
			pathStart = true
			continue
		}
		if !pathStart {
			b.WriteByte(c)
			i++
			continue
		}
		if bracketDepth > 0 {
			b.WriteByte(c)
			i++
			pathStart = false
			continue
		}
		if len(stepFuncStack) > 0 && stepFuncStack[len(stepFuncStack)-1] {
			// Inside a path-step function call's arg list; the
			// arg is relative to the step's iterated context, not
			// the rule context. Emit the byte as-is and let later
			// path bytes follow.
			b.WriteByte(c)
			i++
			pathStart = false
			continue
		}
		switch {
		case c == '/':
			b.WriteByte(c)
			i++
		case c == '$':
			b.WriteByte(c)
			i++
		case c >= '0' && c <= '9':
			b.WriteByte(c)
			i++
		case c == '.':
			b.WriteString(prefix)
			b.WriteByte('/')
			b.WriteByte(c)
			i++
		case c == '@':
			b.WriteString(prefix)
			b.WriteByte('/')
			b.WriteByte(c)
			i++
		case isNameStart(c):
			nameEnd := scanQName(expr, i)
			name := expr[i:nameEnd]
			j := nameEnd
			for j < len(expr) && (expr[j] == ' ' || expr[j] == '\t' || expr[j] == '\n') {
				j++
			}
			if j < len(expr) && expr[j] == '(' {
				// Function call at path-start: write the name
				// without prefixing it. The "(" branch below
				// handles the stack push uniformly when the byte
				// arrives in its own iteration.
				b.WriteString(name)
				i = nameEnd
				pathStart = false
				continue
			}
			if isXPathKeyword(name) {
				b.WriteString(name)
				i = nameEnd
				continue
			}
			b.WriteString(prefix)
			b.WriteByte('/')
			b.WriteString(name)
			i = nameEnd
		default:
			b.WriteByte(c)
			i++
		}
		pathStart = false
	}
	return b.String()
}

// isFollowingPathStepFuncCall reports whether the "(" about to be
// emitted into b opens the argument list of a path-step function
// call. The check looks at what has already been written: a name
// token directly preceding "(" plus a "/" before that name means
// the function's result feeds back into a path step
// (e.g. ".../xs:decimal(child)"), and its arguments resolve in the
// step's iterated context rather than the rule context.
func isFollowingPathStepFuncCall(b *strings.Builder) bool {
	s := b.String()
	end := len(s)
	for end > 0 && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	if end == 0 || (!isXPathNameChar(s[end-1]) && s[end-1] != ':') {
		return false
	}
	for end > 0 && (isXPathNameChar(s[end-1]) || s[end-1] == ':') {
		end--
	}
	for end > 0 && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return end > 0 && s[end-1] == '/'
}

// matchInfixOperator detects an XPath infix operator starting at
// position i in expr. Returns the operator text and its byte length,
// or ("", 0) when no operator starts at i. Both punctuation
// operators (=, !=, <, <=, >, >=, +, -, *, |) and keyword operators
// (and, or, mod, div) are recognised; keyword operators require a
// preceding whitespace and a trailing non-name character so a
// substring like "land" or "more" is not mistaken for one.
func matchInfixOperator(expr string, i int) (string, int) {
	switch expr[i] {
	case '=':
		return "=", 1
	case '!':
		if i+1 < len(expr) && expr[i+1] == '=' {
			return "!=", 2
		}
	case '<':
		if i+1 < len(expr) && expr[i+1] == '=' {
			return "<=", 2
		}
		return "<", 1
	case '>':
		if i+1 < len(expr) && expr[i+1] == '=' {
			return ">=", 2
		}
		return ">", 1
	case '+', '*', '|':
		return string(expr[i]), 1
	case '-':
		if i > 0 && isXPathNameChar(expr[i-1]) {
			return "", 0
		}
		return "-", 1
	}
	for _, kw := range []string{"and", "or", "mod", "div"} {
		if !strings.HasPrefix(expr[i:], kw) {
			continue
		}
		if i > 0 && isXPathNameChar(expr[i-1]) {
			continue
		}
		end := i + len(kw)
		if end < len(expr) && isXPathNameChar(expr[end]) {
			continue
		}
		return kw, len(kw)
	}
	return "", 0
}

// scanQName walks one qualified XPath name (optional prefix + local
// name) starting at i and returns the index just past it. Returns i
// when no name starts at that position.
func scanQName(expr string, i int) int {
	if i >= len(expr) || !isNameStart(expr[i]) {
		return i
	}
	j := i + 1
	for j < len(expr) && (isXPathNameChar(expr[j]) || expr[j] == ':') {
		j++
	}
	return j
}

// isXPathKeyword reports whether name is an XPath / XPath 2.0
// keyword that should not be prefixed as a path step. The list
// covers the keywords the bundled Schematron rules use; missing
// entries surface as path-prefix errors immediately, so the set is
// easy to extend.
func isXPathKeyword(name string) bool {
	switch name {
	case "every", "some", "in", "satisfies", "for", "return",
		"if", "then", "else", "to", "and", "or", "mod", "div",
		"true", "false":
		return true
	}
	return false
}

// isNameStart reports whether c can begin an XPath QName. XPath
// names start with a letter or underscore; subsequent characters
// also allow digit, hyphen, period, and colon (for ns prefixes).
func isNameStart(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c == '_':
		return true
	}
	return false
}

// xpathStringLiteral wraps s as an XPath string literal. Apostrophes
// in s flip the delimiter to double quotes; when both are present the
// literal is constructed via concat() so neither needs escaping in
// the source.
func xpathStringLiteral(s string) string {
	hasApos := strings.Contains(s, "'")
	hasQuot := strings.Contains(s, `"`)
	switch {
	case !hasApos:
		return "'" + s + "'"
	case !hasQuot:
		return `"` + s + `"`
	default:
		var parts []string
		for p := range strings.SplitSeq(s, "'") {
			parts = append(parts, "'"+p+"'")
		}
		return "concat(" + strings.Join(parts, `,"'",`) + ")"
	}
}

// substituteLets replaces XPath variable references ($name) in expr
// with the literal values captured in vars. Variable name matching
// honours XPath's name-character rules: alpha/digit/underscore/hyphen
// after the leading $; everything else terminates the name.
func substituteLets(expr string, vars map[string]string) string {
	if len(vars) == 0 || !strings.ContainsRune(expr, '$') {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); i++ {
		if expr[i] != '$' {
			b.WriteByte(expr[i])
			continue
		}
		// Collect the variable name following $.
		j := i + 1
		for j < len(expr) && isXPathNameChar(expr[j]) {
			j++
		}
		name := expr[i+1 : j]
		if lit, ok := vars[name]; ok {
			b.WriteString(lit)
		} else {
			b.WriteString(expr[i:j])
		}
		i = j - 1
	}
	return b.String()
}

func isXPathNameChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_' || b == '-' || b == '.':
		return true
	}
	return false
}

func makeViolation(_ *Rule, a *Assertion, n *xmlquery.Node) Violation {
	return Violation{
		RuleID:  a.ID,
		Flag:    a.Flag,
		Role:    a.Role,
		Path:    nodePath(n),
		Message: a.Message,
		Negate:  a.Negate,
	}
}

// nodePath renders the path from doc root to n as a slash-joined
// list of local names. Namespace prefixes are dropped for
// readability; CII keeps local names unambiguous.
func nodePath(n *xmlquery.Node) string {
	if n == nil {
		return ""
	}
	var parts []string
	for cur := n; cur != nil && cur.Type == xmlquery.ElementNode; cur = cur.Parent {
		parts = append([]string{cur.Data}, parts...)
	}
	return "/" + strings.Join(parts, "/")
}

// selectNodes evaluates a context XPath against doc and returns
// every element node it matches. Panic recovery mirrors the
// evalBoolean guard: an antchfx/xpath panic surfaces as an error
// so the caller skips the rule rather than crashing the host.
func selectNodes(doc *xmlquery.Node, expr string, cache *exprCache) (out []*xmlquery.Node, err error) {
	compiled, err := cache.compile(expr)
	if err != nil {
		return nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("xpath evaluator panic on context: %v", r)
			out = nil
		}
	}()
	iter := compiled.Select(xmlquery.CreateXPathNavigator(doc))
	for iter.MoveNext() {
		nav, ok := iter.Current().(*xmlquery.NodeNavigator)
		if !ok {
			continue
		}
		out = append(out, nav.Current())
	}
	return out, nil
}

// evalBoolean compiles expr and evaluates it against node n with
// the boolean semantics Schematron expects. Let-binding substitution
// has already happened at the caller; this function just compiles
// and runs the (possibly XPath 2.0) test expression.
//
// Panic recovery guards against upstream evaluator bugs in
// antchfx/xpath: a malformed query graph has been observed to deref
// a nil node deep inside the evaluator on real EN 16931 / XRechnung
// payloads, and the host process should not crash for that. A
// recovered panic surfaces as an error so the caller logs the rule
// as skipped rather than ignoring the gap.
func evalBoolean(expr string, n *xmlquery.Node, cache *exprCache) (result bool, err error) {
	compiled, err := cache.compile(expr)
	if err != nil {
		return false, err
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("xpath evaluator panic: %v", r)
		}
	}()
	nav := xmlquery.CreateXPathNavigator(n)
	return xpathBoolean(compiled.Evaluate(nav)), nil
}

// xpathBoolean coerces an XPath evaluation result to the boolean
// semantics Schematron expects:
//
//   - bool: returned as-is.
//   - float64: true for non-zero non-NaN.
//   - string: true when non-empty.
//   - *xpath.NodeIterator: true when the node set is non-empty.
//
// Any other type returns false; antchfx/xpath does not emit anything
// outside those kinds today.
func xpathBoolean(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0 && t == t // NaN check
	case string:
		return t != ""
	case *xpath.NodeIterator:
		return t.MoveNext()
	}
	return false
}

// namespaceMap collapses the schema's NS list into a prefix → URI
// map suitable for the antchfx/xpath compiler.
func namespaceMap(s *Schema) map[string]string {
	out := make(map[string]string, len(s.Namespaces))
	for _, ns := range s.Namespaces {
		out[ns.Prefix] = ns.URI
	}
	return out
}

// exprCache memoises compiled XPath expressions across rules so a
// pattern with N context-matching nodes pays the compile cost once
// per unique expression rather than N times.
type exprCache struct {
	ns       map[string]string
	compiled map[string]*xpath.Expr
}

func newExprCache(ns map[string]string) *exprCache {
	return &exprCache{ns: ns, compiled: make(map[string]*xpath.Expr)}
}

func (c *exprCache) compile(expr string) (*xpath.Expr, error) {
	if cached, ok := c.compiled[expr]; ok {
		return cached, nil
	}
	// Rewrite XPath 2.0 constructs antchfx/xpath does not support
	// (xs:decimal casts, upper-case/lower-case) into the closest
	// XPath 1.0 equivalents before compilation. Lossy by design; see
	// rewriteXPath2 for the accepted approximations.
	rewritten := rewriteXPath2(expr)
	// antchfx/xpath returns an empty node-set for "ns:*" wildcard
	// steps and the same step nested in a predicate, regardless of
	// what the document actually contains. Replace "ns:*" with
	// "*[namespace-uri()='URI']" using the schema's prefix map; the
	// namespace-uri predicate evaluates correctly and matches the
	// same element set the XPath 1.0 spec defines for "ns:*".
	rewritten = rewriteNamespaceWildcard(rewritten, c.ns)
	compiled, err := xpath.CompileWithNS(rewritten, c.ns)
	if err != nil {
		return nil, fmt.Errorf("compiling xpath %q: %w", expr, err)
	}
	c.compiled[expr] = compiled
	return compiled, nil
}

// rewriteNamespaceWildcard converts "prefix:*" wildcard steps into
// the "*[namespace-uri()='URI']" form. The substitution looks up the
// prefix in ns; unknown prefixes pass through unchanged so a typo or
// a prefix referenced before its declaration surfaces as a regular
// XPath compile error instead of a silent rewrite.
//
// "*" is only replaced when it occupies a path-step position, never
// as the multiplication operator. The walker recognises a path-step
// "*" when it follows ":" (after a name prefix), or when the
// preceding character is "/", or start-of-input. Anywhere else the
// "*" stays as the arithmetic operator.
func rewriteNamespaceWildcard(expr string, ns map[string]string) string {
	if !strings.Contains(expr, ":*") || len(ns) == 0 {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr) + 64)
	for i := 0; i < len(expr); {
		c := expr[i]
		if !isNameStart(c) {
			b.WriteByte(c)
			i++
			continue
		}
		// Scan a name token.
		j := i + 1
		for j < len(expr) && isXPathNameChar(expr[j]) {
			j++
		}
		// Need the form "name:*" with no other characters between.
		if j+1 < len(expr) && expr[j] == ':' && expr[j+1] == '*' {
			// Confirm "*" is not followed by a name char (e.g. avoid
			// matching "foo:*bar" which is invalid XPath but defensive).
			after := j + 2
			if after >= len(expr) || !isXPathNameChar(expr[after]) {
				prefix := expr[i:j]
				if uri, ok := ns[prefix]; ok {
					b.WriteString("*[namespace-uri()='")
					b.WriteString(uri)
					b.WriteString("']")
					i = after
					continue
				}
			}
		}
		b.WriteString(expr[i:j])
		i = j
	}
	return b.String()
}
