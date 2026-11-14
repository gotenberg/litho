package markdown

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	md "github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	mdhtml "github.com/gomarkdown/markdown/html"

	"github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxTitleBytes caps the title taken from the first heading: 4 KiB.
// Anti-DoS guard, not spec-mandated: a heading spanning a whole page would
// otherwise be copied into the document information. A real heading is a
// line.
const maxTitleBytes = 4 << 10

// Options parametrises a Markdown conversion.
type Options struct {
	// HTML are the options the rendered document converts with.
	HTML html.Options
	// Template is the HTML document the Markdown renders into, a template
	// of html/template in which {{ toHTML }} is the rendered Markdown and
	// {{ .Title }} the text of its first heading (see the package
	// documentation). Empty is [DefaultTemplate].
	Template string
}

// Convert lays the Markdown document src out as a Document.
//
// It checks opts.Template first and fails with an error wrapping
// [typeset.ErrInvalidOptions] when the template does not parse or is refused,
// before src is read. It then reads src to its end, at most
// opts.MaxInputBytes bytes ([html.DefaultMaxInputBytes] when zero), and
// returns an error wrapping [limits.ErrInputTooLarge] past them, before
// anything is rendered; src is never closed. The document the template
// renders is capped at the ceiling of the input budget,
// [limits.MaxConvertInputBytesCeiling], past which it fails with
// [limits.ErrInputTooLarge], and is converted by [html.Convert] with opts
// under that ceiling, failing as html.Convert does. A cap out of range, or
// an error executing the template, fails with [typeset.ErrInvalidOptions].
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, o Options) (*typeset.Document, error) {
	// The instances of variable fonts the conversion draws belong to it.
	fonts = fonts.ForConversion()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := o.HTML
	maxBytes := opts.MaxInputBytes
	if maxBytes == 0 {
		maxBytes = html.DefaultMaxInputBytes
	}
	if maxBytes < 0 || maxBytes > limits.MaxConvertInputBytesCeiling {
		return nil, fmt.Errorf("%w: input cap %d is not in [1, %d]", typeset.ErrInvalidOptions, maxBytes, int64(limits.MaxConvertInputBytesCeiling))
	}
	tmpl, err := parseTemplate(o.Template)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(src, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the source: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: the source is longer than %d bytes", limits.ErrInputTooLarge, maxBytes)
	}
	body, title, err := render(ctx, data)
	if err != nil {
		return nil, err
	}
	page, charged, err := tmpl.render(ctx, body, title, limits.MaxConvertInputBytesCeiling)
	// The rendered body is garbage once the template wrote it into page.
	limits.BudgetFrom(ctx).Release(int64(len(body)))
	if err != nil {
		return nil, err
	}
	defer limits.BudgetFrom(ctx).Release(charged)
	opts.MaxInputBytes = limits.MaxConvertInputBytesCeiling
	return html.Convert(ctx, bytes.NewReader(page), fonts, opts)
}

// astNodeCost is what one node of the parsed Markdown tree costs the
// conversion's memory budget while the tree is rendered: the node, 130 to
// 250 bytes on a 64-bit platform by its kind, and the slot its parent
// keeps. Its text points into the source, which the caller charges.
//
// # Corpus evidence
//
//   - probe, markdown: 20,000 paragraphs, list items, headings, quotes,
//     emphases, code spans, links, table rows and fenced code blocks parse
//     into trees of 130 to 242 bytes of live heap a node, a fenced block
//     the most; a list item is three nodes for four source bytes, 132
//     bytes of tree a byte (TestASTNodeCostCoversANode).
const astNodeCost = 256

// render returns the HTML the Markdown src renders to, the renderer's
// output at its defaults, and the text of its first heading. It charges
// the conversion's budget the parsed tree while it renders it
// ([astNodeCost]), then the rendered body, which the caller releases once
// it drops the body.
func render(ctx context.Context, src []byte) (body []byte, title string, err error) {
	doc := md.Parse(src, nil)
	mem := limits.BudgetFrom(ctx)
	nodes := int64(0)
	ast.WalkFunc(doc, func(_ ast.Node, entering bool) ast.WalkStatus {
		if entering {
			nodes++
		}
		return ast.GoToNext
	})
	if err := mem.ChargeUnits(nodes, astNodeCost, "the Markdown tree"); err != nil {
		return nil, "", err
	}
	defer mem.Release(nodes * astNodeCost)
	// The renderer markdown.ToHTML(src, nil, nil) builds.
	opts := mdhtml.RendererOptions{Flags: mdhtml.CommonFlags}
	if root, ok := doc.(*ast.Document); ok && root.CommonMark {
		opts.Flags = mdhtml.UseXHTML
	}
	body = md.Render(doc, mdhtml.NewRenderer(opts))
	if err := mem.Charge(int64(len(body)), "rendered Markdown"); err != nil {
		return nil, "", err
	}
	return body, firstHeading(doc), nil
}

// firstHeading returns the text of the document's first heading, its
// whitespace collapsed, "" without one.
func firstHeading(doc ast.Node) string {
	var heading ast.Node
	ast.WalkFunc(doc, func(n ast.Node, entering bool) ast.WalkStatus {
		if h, ok := n.(*ast.Heading); ok && entering {
			heading = h
			return ast.Terminate
		}
		return ast.GoToNext
	})
	if heading == nil {
		return ""
	}
	var b strings.Builder
	ast.WalkFunc(heading, func(n ast.Node, entering bool) ast.WalkStatus {
		if !entering || b.Len() >= maxTitleBytes {
			return ast.GoToNext
		}
		switch n.(type) {
		case *ast.Text, *ast.Code:
			b.Write(n.AsLeaf().Literal)
		case *ast.Softbreak, *ast.Hardbreak:
			b.WriteByte(' ')
		}
		return ast.GoToNext
	})
	t := strings.ToValidUTF8(b.String(), "�")
	if len(t) > maxTitleBytes {
		t = strings.ToValidUTF8(t[:maxTitleBytes], "")
	}
	return strings.Join(strings.Fields(t), " ")
}
