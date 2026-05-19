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
// XPath 2.0 constructs the bundled engine does not implement (for…
// return, if/then/else, string-to-codepoints, etc.). The evaluator
// consults the override map before attempting XPath compilation; a
// registered override always takes precedence. When an override
// returns an error, the rule lands in the skipped slice with the
// error's message as the reason - same shape as an XPath skip.
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
// [cii_schematron.BuiltinRuleOverrides] with overrides for known
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

	// Schema-level lets evaluate against the document root once and
	// stay constant for every pattern below. Their literal values
	// substitute into pattern lets, rule contexts, and assertion
	// tests before XPath compilation.
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
// context node n. Let bindings declared on the rule are pre-evaluated
// and substituted into each assertion's test expression before
// compilation; assertions whose test expression fails to compile or
// evaluate get silently skipped so a single unsupported XPath
// construct does not abort the whole pass.
//
// The silent-skip behaviour exists because real-world Schematron
// schemas (EN 16931, XRechnung) lean on XPath 2.0 functions and
// xsl:function helpers that antchfx/xpath does not implement. A
// per-rule abort would leave most documents producing zero
// violations - worse than no Schematron at all. PR-followup
// territory: surface skipped rules via a logger hook so callers can
// audit coverage.
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
// against the bundled antchfx/xpath. The boolean returned matches
// the test expression's verdict; callers translate that into
// pass/fire per assertion kind.
func runAssertion(ctx context.Context, a *Assertion, n *xmlquery.Node, vars map[string]string, cache *exprCache, overrides map[string]RuleOverride) (bool, error) {
	if a.ID != "" {
		if fn, ok := overrides[a.ID]; ok {
			return fn(ctx, n)
		}
	}
	return evalBoolean(substituteLets(a.Test, vars), n, cache)
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

func makeViolation(rule *Rule, a *Assertion, n *xmlquery.Node) Violation {
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
	compiled, err := xpath.CompileWithNS(rewritten, c.ns)
	if err != nil {
		return nil, fmt.Errorf("compiling xpath %q: %w", expr, err)
	}
	c.compiled[expr] = compiled
	return compiled, nil
}
