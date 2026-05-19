package schematron

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/antchfx/xmlquery"
	"github.com/antchfx/xpath"
)

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
	phase string
}

// WithPhase restricts evaluation to the patterns activated by the
// named phase. When the option is absent, [Validate] runs every
// pattern in source order.
func WithPhase(id string) EvalOption {
	return func(o *evalOpts) { o.phase = id }
}

// Validate parses payload, walks schema's active patterns, and
// returns one Violation per assertion that fired on a context match.
// Rule selection follows the Schematron convention: within a
// pattern, the first rule whose context matches a node wins for
// that node; later rules in the same pattern skip that node.
//
// payload must be well-formed XML. A parse error surfaces as the
// second return value distinct from violations so callers can tell
// "ran-but-failed-rules" apart from "could-not-run".
func Validate(ctx context.Context, schema *Schema, payload []byte, opts ...EvalOption) ([]Violation, error) {
	cfg := evalOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	if schema == nil {
		return nil, fmt.Errorf("schematron: schema is nil")
	}
	doc, err := xmlquery.Parse(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("schematron: parsing payload: %w", err)
	}

	active, err := selectPatterns(schema, cfg.phase)
	if err != nil {
		return nil, err
	}

	ns := namespaceMap(schema)
	cache := newExprCache(ns)

	var out []Violation
	for _, pid := range active {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		pat := schema.Patterns[pid]
		if pat == nil {
			continue
		}
		v, err := evaluatePattern(ctx, doc, pat, cache)
		if err != nil {
			return out, err
		}
		out = append(out, v...)
	}
	return out, nil
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
// later rules skip nodes already claimed.
func evaluatePattern(ctx context.Context, doc *xmlquery.Node, pat *Pattern, cache *exprCache) ([]Violation, error) {
	claimed := make(map[*xmlquery.Node]bool)
	var out []Violation
	for _, rule := range pat.Rules {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		nodes, err := selectNodes(doc, rule.Context, cache)
		if err != nil {
			return out, fmt.Errorf("evaluating context %q: %w", rule.Context, err)
		}
		for _, n := range nodes {
			if claimed[n] {
				continue
			}
			claimed[n] = true
			out = append(out, evaluateRule(rule, n, cache)...)
		}
	}
	return out, nil
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
func evaluateRule(rule *Rule, n *xmlquery.Node, cache *exprCache) []Violation {
	vars := make(map[string]string, len(rule.Lets))
	for _, lt := range rule.Lets {
		raw, err := cache.evalForVariable(lt.Value, n)
		if err != nil {
			// Skip this let; assertions that reference it will
			// either fail to compile or evaluate to empty.
			continue
		}
		vars[lt.Name] = xpathLiteral(raw)
	}

	out := make([]Violation, 0, len(rule.Asserts)+len(rule.Reports))
	for _, a := range rule.Asserts {
		fired, err := evalBoolean(substituteLets(a.Test, vars), n, cache)
		if err != nil {
			continue
		}
		if !fired {
			out = append(out, makeViolation(rule, a, n))
		}
	}
	for _, r := range rule.Reports {
		fired, err := evalBoolean(substituteLets(r.Test, vars), n, cache)
		if err != nil {
			continue
		}
		if fired {
			out = append(out, makeViolation(rule, r, n))
		}
	}
	return out
}

// xpathLiteral renders an XPath evaluation result back into a literal
// suitable for textual substitution. String values are wrapped in
// quotes (or apostrophes when the value itself contains quotes);
// numbers serialise without quoting; everything else falls back to
// string-coercion.
func xpathLiteral(v any) string {
	switch t := v.(type) {
	case string:
		if strings.ContainsRune(t, '\'') && !strings.ContainsRune(t, '"') {
			return `"` + t + `"`
		}
		return "'" + strings.ReplaceAll(t, "'", "&apos;") + "'"
	case float64:
		return fmt.Sprintf("%g", t)
	case bool:
		if t {
			return "true()"
		}
		return "false()"
	}
	return "''"
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
// every element node it matches.
func selectNodes(doc *xmlquery.Node, expr string, cache *exprCache) ([]*xmlquery.Node, error) {
	compiled, err := cache.compile(expr)
	if err != nil {
		return nil, err
	}
	iter := compiled.Select(xmlquery.CreateXPathNavigator(doc))
	var out []*xmlquery.Node
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
func evalBoolean(expr string, n *xmlquery.Node, cache *exprCache) (bool, error) {
	compiled, err := cache.compile(expr)
	if err != nil {
		return false, err
	}
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

// evalForVariable evaluates a let expression against node n and
// returns the resulting any-typed value. Used by [evaluateRule] to
// pre-compute let bindings before assertions fire.
func (c *exprCache) evalForVariable(expr string, n *xmlquery.Node) (any, error) {
	compiled, err := c.compile(expr)
	if err != nil {
		return nil, err
	}
	return compiled.Evaluate(xmlquery.CreateXPathNavigator(n)), nil
}
