package markdown

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"text/template/parse"

	"github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// DefaultTemplate is the template a Markdown document renders into when
// [Options.Template] is empty: a document without a stylesheet whose title
// is the first heading of the source.
const DefaultTemplate = `<!doctype html><html><head><meta charset="utf-8"><title>{{ .Title }}</title></head><body>{{ toHTML }}</body></html>`

// maxTemplateBytes caps the text of a template: 1 MiB. Anti-DoS guard, not
// spec-mandated: the template is caller text parsed in memory, and a page
// template with its stylesheet inlined is a few kilobytes.
const maxTemplateBytes = 1 << 20

// maxTemplateSteps caps the nodes one execution of a template can visit,
// every {{ template }} call expanded: 2^20. Anti-DoS guard, not
// spec-mandated: a template with no loop visits each node of its expansion
// once, and calls that nest can multiply the expansion past any output cap
// while writing nothing, so the bound is checked before execution. A page
// template visits a few hundred nodes.
const maxTemplateSteps = 1 << 20

// templateFunc is the one function a template calls.
const templateFunc = "toHTML"

// templateData is what a template sees as its dot: the title alone.
type templateData struct {
	// Title is the text of the source's first heading, "" without one.
	Title string
}

// pageTemplate is a parsed and checked template, ready to render one
// Markdown document.
type pageTemplate struct {
	t *template.Template
	// body is what toHTML returns, set before execution.
	body template.HTML
}

// parseTemplate parses text, [DefaultTemplate] when empty, and checks it
// (see [checkTemplate]). An error wraps [html.ErrInvalidOptions].
func parseTemplate(text string) (*pageTemplate, error) {
	if text == "" {
		text = DefaultTemplate
	}
	if len(text) > maxTemplateBytes {
		return nil, fmt.Errorf("%w: the Markdown template is longer than %d bytes", html.ErrInvalidOptions, maxTemplateBytes)
	}
	p := &pageTemplate{}
	t, err := template.New("markdown").Funcs(template.FuncMap{
		templateFunc: func() template.HTML { return p.body },
	}).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing the Markdown template: %w", html.ErrInvalidOptions, err)
	}
	if err := checkTemplate(t); err != nil {
		return nil, fmt.Errorf("%w: Markdown template: %w", html.ErrInvalidOptions, err)
	}
	p.t = t
	return p, nil
}

// render executes the template on body and title, into at most maxBytes
// bytes it charges to the budget of ctx. The caller releases the
// returned charge once it lets the document go. Past maxBytes it fails
// with an error wrapping [html.ErrInputTooLarge]; it stops at the first
// write after ctx ends. An execution error wraps [html.ErrInvalidOptions].
func (p *pageTemplate) render(ctx context.Context, body []byte, title string, maxBytes int64) (doc []byte, charged int64, err error) {
	// The renderer escapes the text it writes and passes the raw HTML of
	// the source through, as a browser-based converter receives it: the
	// body is HTML, not text to escape again.
	p.body = template.HTML(body) //nolint:gosec // the rendered Markdown is the document itself; litho runs no script
	w := &cappedWriter{ctx: ctx, mem: limits.BudgetFrom(ctx), max: maxBytes}
	w.buf.Grow(len(body) + len(DefaultTemplate))
	err = p.t.Execute(w, templateData{Title: title})
	switch {
	case err == nil:
		return w.buf.Bytes(), w.charged, nil
	case w.err != nil:
		w.release()
		return nil, 0, w.err
	default:
		w.release()
		return nil, 0, fmt.Errorf("%w: executing the Markdown template: %w", html.ErrInvalidOptions, err)
	}
}

// cappedWriter collects the rendered document, failing past max bytes,
// once ctx ends, or once the budget refuses the bytes.
type cappedWriter struct {
	ctx     context.Context //nolint:containedctx // lives for one Execute call; html/template takes an io.Writer
	mem     *limits.Budget
	max     int64
	buf     bytes.Buffer
	charged int64
	// err is the failure the writer returned, the one Execute reports.
	err error
}

func (w *cappedWriter) Write(b []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if err := w.ctx.Err(); err != nil {
		w.err = err
		return 0, err
	}
	if int64(w.buf.Len())+int64(len(b)) > w.max {
		w.err = fmt.Errorf("%w: the Markdown template renders past %d bytes", html.ErrInputTooLarge, w.max)
		return 0, w.err
	}
	if err := w.mem.Charge(int64(len(b)), "rendered Markdown"); err != nil {
		w.err = err
		return 0, err
	}
	w.charged += int64(len(b))
	return w.buf.Write(b)
}

func (w *cappedWriter) release() {
	w.mem.Release(w.charged)
	w.charged = 0
}

// errTemplateRefused marks a construct the template language allows and a
// Markdown template does not take.
var errTemplateRefused = errors.New("not supported in a Markdown template")

// checkTemplate refuses a template in which no toHTML call is reachable
// from the top-level template, or whose execution is not bounded.
//
// The walk starts at the top-level template and follows {{ template }}
// calls, {{ block }} included, so a {{ define }} nothing calls does not
// count; both branches of an {{ if }} or a {{ with }} count, a call inside
// one is reachable. The template sees one function, toHTML, and one field,
// .Title: a builtin function, another field, a {{ range }} (there is
// nothing to range over), a template that calls itself or one that does
// not exist, and an expansion past [maxTemplateSteps] nodes are refused.
// Without loops or recursion each node runs at most as often as the
// expansion counts it, so the count bounds the execution.
func checkTemplate(t *template.Template) error {
	if t.Tree == nil || t.Tree.Root == nil {
		return errors.New("the template never calls toHTML: it is empty")
	}
	c := &templateChecker{t: t, cost: map[string]int64{}, visiting: map[string]bool{}}
	steps, err := c.list(t.Tree.Root)
	if err != nil {
		return err
	}
	if steps > maxTemplateSteps {
		return fmt.Errorf("%w: the template expands past %d nodes", errTemplateRefused, maxTemplateSteps)
	}
	if !c.callsBody {
		return errors.New("the template never calls toHTML: the source would not print")
	}
	return nil
}

// templateChecker walks the parse trees of a template.
type templateChecker struct {
	t *template.Template
	// cost memoises the expanded node count of each named template.
	cost map[string]int64
	// visiting holds the named templates being expanded, to find a cycle.
	visiting  map[string]bool
	callsBody bool
}

// add sums two counts, saturating past [maxTemplateSteps].
func add(a, b int64) int64 {
	return min(a+b, maxTemplateSteps+1)
}

func (c *templateChecker) list(l *parse.ListNode) (int64, error) {
	if l == nil {
		return 0, nil
	}
	var steps int64
	for _, n := range l.Nodes {
		s, err := c.node(n)
		if err != nil {
			return 0, err
		}
		steps = add(steps, s)
	}
	return steps, nil
}

func (c *templateChecker) node(n parse.Node) (int64, error) {
	switch n := n.(type) {
	case *parse.TextNode, *parse.CommentNode:
		return 1, nil
	case *parse.ActionNode:
		s, err := c.pipe(n.Pipe)
		return add(s, 1), err
	case *parse.IfNode:
		return c.branch(&n.BranchNode)
	case *parse.WithNode:
		return c.branch(&n.BranchNode)
	case *parse.RangeNode:
		return 0, fmt.Errorf("%w: range at %s: the template data holds no list", errTemplateRefused, c.position(n))
	case *parse.TemplateNode:
		s, err := c.pipe(n.Pipe)
		if err != nil {
			return 0, err
		}
		called, err := c.named(n.Name, n)
		return add(add(s, called), 1), err
	default:
		return 0, fmt.Errorf("%w: %s at %s", errTemplateRefused, n, c.position(n))
	}
}

func (c *templateChecker) branch(b *parse.BranchNode) (int64, error) {
	s, err := c.pipe(b.Pipe)
	if err != nil {
		return 0, err
	}
	then, err := c.list(b.List)
	if err != nil {
		return 0, err
	}
	els, err := c.list(b.ElseList)
	if err != nil {
		return 0, err
	}
	return add(add(add(s, then), els), 1), nil
}

// named returns the expanded count of the template called name.
func (c *templateChecker) named(name string, at parse.Node) (int64, error) {
	if s, ok := c.cost[name]; ok {
		return s, nil
	}
	if c.visiting[name] {
		return 0, fmt.Errorf("%w: template %q calls itself at %s", errTemplateRefused, name, c.position(at))
	}
	called := c.t.Lookup(name)
	if called == nil || called.Tree == nil {
		return 0, fmt.Errorf("%w: template %q at %s is not defined", errTemplateRefused, name, c.position(at))
	}
	c.visiting[name] = true
	s, err := c.list(called.Tree.Root)
	delete(c.visiting, name)
	if err != nil {
		return 0, err
	}
	c.cost[name] = s
	return s, nil
}

func (c *templateChecker) pipe(p *parse.PipeNode) (int64, error) {
	if p == nil {
		return 0, nil
	}
	var steps int64 = 1
	for _, cmd := range p.Cmds {
		for _, arg := range cmd.Args {
			s, err := c.arg(arg)
			if err != nil {
				return 0, err
			}
			steps = add(steps, s)
		}
	}
	return steps, nil
}

func (c *templateChecker) arg(n parse.Node) (int64, error) {
	switch n := n.(type) {
	case *parse.IdentifierNode:
		if n.Ident != templateFunc {
			return 0, fmt.Errorf("%w: function %q at %s: the template calls toHTML alone", errTemplateRefused, n.Ident, c.position(n))
		}
		c.callsBody = true
		return 1, nil
	case *parse.FieldNode:
		if len(n.Ident) != 1 || n.Ident[0] != "Title" {
			return 0, fmt.Errorf("%w: field %s at %s: the template data holds .Title alone", errTemplateRefused, n, c.position(n))
		}
		return 1, nil
	case *parse.VariableNode:
		if len(n.Ident) > 2 || len(n.Ident) == 2 && n.Ident[1] != "Title" {
			return 0, fmt.Errorf("%w: field %s at %s: the template data holds .Title alone", errTemplateRefused, n, c.position(n))
		}
		return 1, nil
	case *parse.PipeNode:
		return c.pipe(n)
	case *parse.DotNode, *parse.StringNode, *parse.NumberNode, *parse.BoolNode, *parse.NilNode:
		return 1, nil
	default:
		return 0, fmt.Errorf("%w: %s at %s", errTemplateRefused, n, c.position(n))
	}
}

// position names where n is in the template text: the template's name,
// line and column. ErrorContext reads n's own tree, whichever tree it is
// called on.
func (c *templateChecker) position(n parse.Node) string {
	loc, _ := c.t.Tree.ErrorContext(n)
	return loc
}
