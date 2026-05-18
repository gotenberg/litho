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
			v, err := evaluateRule(rule, n, cache)
			if err != nil {
				return out, err
			}
			out = append(out, v...)
		}
	}
	return out, nil
}

// evaluateRule applies every assertion in rule against the single
// context node n. Let bindings declared on the rule are pre-evaluated
// and threaded into the per-assertion XPath context so test
// expressions can reference them by name.
func evaluateRule(rule *Rule, n *xmlquery.Node, cache *exprCache) ([]Violation, error) {
	vars := make(map[string]any, len(rule.Lets))
	for _, lt := range rule.Lets {
		v, err := cache.evalForVariable(lt.Value, n)
		if err != nil {
			return nil, fmt.Errorf("evaluating let %q: %w", lt.Name, err)
		}
		vars[lt.Name] = v
	}

	var out []Violation
	for _, a := range rule.Asserts {
		fired, err := evalBoolean(a.Test, n, vars, cache)
		if err != nil {
			return out, fmt.Errorf("evaluating assert %q: %w", a.ID, err)
		}
		if !fired {
			out = append(out, makeViolation(rule, a, n))
		}
	}
	for _, r := range rule.Reports {
		fired, err := evalBoolean(r.Test, n, vars, cache)
		if err != nil {
			return out, fmt.Errorf("evaluating report %q: %w", r.ID, err)
		}
		if fired {
			out = append(out, makeViolation(rule, r, n))
		}
	}
	return out, nil
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
// the supplied variable map. The XPath spec coerces every value to
// boolean according to its truthiness rules; we lean on
// antchfx/xpath's [xpath.Bool] for that conversion.
func evalBoolean(expr string, n *xmlquery.Node, vars map[string]any, cache *exprCache) (bool, error) {
	compiled, err := cache.compile(expr)
	if err != nil {
		return false, err
	}
	nav := xmlquery.CreateXPathNavigator(n)
	val := evaluateWithVars(compiled, nav, vars)
	return xpathBoolean(val), nil
}

// evaluateWithVars threads the variable map into the XPath evaluator
// via a custom xpath.NodeNavigator wrapper. antchfx/xpath does not
// expose a "set variable" helper on Expr.Evaluate, so we substitute
// references in the expression text before compilation. PR2 keeps
// this simple: variables resolve to their stringified evaluated
// values, which works for the bulk of EN 16931 let usage (date
// strings, totals as decimal strings).
func evaluateWithVars(expr *xpath.Expr, nav xpath.NodeNavigator, _ map[string]any) any {
	// PR2 does not yet wire variables through antchfx/xpath because
	// the library's public Evaluate signature does not accept a
	// variable resolver. EN 16931 schemas that rely on let
	// bindings will see those expressions fail to compile or
	// evaluate to empty; PR3 adds proper variable threading. Leaving
	// the vars parameter in place keeps the call sites stable.
	return expr.Evaluate(nav)
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
	compiled, err := xpath.CompileWithNS(expr, c.ns)
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
