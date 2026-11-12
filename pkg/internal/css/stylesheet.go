package css

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

// ParseOptions configures [Parse] and [ParseDeclarations].
type ParseOptions struct {
	// BaseURL is the stylesheet's URL: url() values and @import resolve
	// against it. Empty keeps relative URLs as written.
	BaseURL string
	// Fetch returns the bytes of an imported stylesheet and the URL they
	// came from, the response's after redirects, "" when it is url itself:
	// the imported sheet's url() values and imports resolve against that
	// URL (CSS Values 4, 4.5.1, the stylesheet's location being its
	// response's URL). nil disables @import. It is called sequentially, in
	// source order, at most maxImports times per Parse call.
	Fetch func(ctx context.Context, url string) (body []byte, final string, err error)
	// Logger receives debug records of dropped imports; nil discards them.
	Logger *slog.Logger
}

// Stylesheet is a parsed stylesheet with its imports. It is immutable and
// safe for concurrent use.
type Stylesheet struct {
	rules []styleRule
	pages []pageRule
	faces []fontFaceRule
	warns []Warning
}

// Warnings returns what parsing dropped or approximated, deduplicated and
// capped. The slice is a copy.
func (s *Stylesheet) Warnings() []Warning { return append([]Warning(nil), s.warns...) }

// styleRule is a qualified rule with a valid selector list.
type styleRule struct {
	selectors []*complexSel
	decls     []Declaration
	cond      *mediaCond
}

// fontFaceRule is an @font-face rule and its media condition.
type fontFaceRule struct {
	face FontFace
	cond *mediaCond
}

// sheetParser parses one stylesheet and its imports.
type sheetParser struct {
	opts    ParseOptions
	log     *slog.Logger
	sheet   *Stylesheet
	warns   warnings
	rules   int
	imports int
	// nesting is the depth of the nested style rules being parsed.
	nesting int
	chain   []string
	err     error
}

// Parse parses a stylesheet (CSS Syntax 3, 5.3.3) and the stylesheets it
// imports. Malformed CSS recovers as the syntax asks: a bad declaration,
// rule or at-rule is dropped and parsing goes on. The input is UTF-8; a
// leading byte order mark is skipped, and @charset is ignored.
//
// Parse returns [ErrTooLarge] when the text, an imported text, the token
// count, the nesting depth or the rule count passes its cap, and ctx's
// error when it is done; the returned Stylesheet then holds the rules read
// so far.
func Parse(ctx context.Context, src []byte, opts ParseOptions) (*Stylesheet, error) {
	sp := &sheetParser{opts: opts, log: opts.Logger, sheet: &Stylesheet{}}
	if sp.log == nil {
		sp.log = slog.New(slog.DiscardHandler)
	}
	if len(src) > maxStylesheetBytes {
		return sp.sheet, fmt.Errorf("parsing stylesheet of %d bytes: %w", len(src), ErrTooLarge)
	}
	if opts.BaseURL != "" {
		sp.chain = append(sp.chain, opts.BaseURL)
	}
	sp.parseSheet(ctx, src, opts.BaseURL, nil, 0)
	sp.sheet.warns = sp.warns.result()
	return sp.sheet, sp.err
}

// parseSheet parses one stylesheet's text into the shared result.
func (sp *sheetParser) parseSheet(ctx context.Context, src []byte, base string, cond *mediaCond, depth int) {
	text := strings.TrimPrefix(string(src), "\xef\xbb\xbf")
	p := &parser{tz: newTokenizer(text, maxTokens)}
	importsAllowed := true
	p.rules(true, func(it item) bool {
		sp.rule(ctx, it, base, cond, depth, &importsAllowed)
		return sp.err == nil
	})
	if sp.err == nil && (p.tz.over || p.deep) {
		sp.err = fmt.Errorf("parsing stylesheet %q: %w", base, ErrTooLarge)
	}
}

// count accounts for one more rule.
func (sp *sheetParser) count() bool {
	sp.rules++
	if sp.rules > maxRules {
		sp.err = fmt.Errorf("parsing stylesheets of more than %d rules: %w", maxRules, ErrTooLarge)
		return false
	}
	return true
}

// ctxFor returns a declaration context for base.
func (sp *sheetParser) flush(c *declCtx) {
	for _, w := range c.warns {
		sp.warns.add(w)
	}
	c.warns = nil
}

// ignoredAtRules are at-rules litho drops with their blocks without a
// warning: they style nothing a static page shows.
var ignoredAtRules = map[string]bool{
	"keyframes": true, "-webkit-keyframes": true, "-moz-keyframes": true, "-o-keyframes": true,
	"counter-style": true, "font-feature-values": true, "font-palette-values": true,
	"property": true, "viewport": true, "-ms-viewport": true, "view-transition": true,
	"namespace": true, "charset": true,
}

// rule handles one top-level or nested rule.
func (sp *sheetParser) rule(ctx context.Context, it item, base string, cond *mediaCond, depth int, importsAllowed *bool) {
	if err := ctx.Err(); err != nil {
		sp.err = err
		return
	}
	c := &declCtx{baseURL: base}
	defer sp.flush(c)
	switch it.kind { //nolint:exhaustive // declarations and errors cannot reach the top level
	case itemQualified:
		*importsAllowed = false
		sels, err := parseSelectorList(it.prelude, 0, false)
		if err != nil {
			if err.unsupported != "" {
				c.warn("unsupported-css-selector", "a rule with the selector %s was ignored", err.unsupported)
			}
			return
		}
		if !sp.count() {
			return
		}
		sp.sheet.rules = append(sp.sheet.rules, styleRule{selectors: sels, decls: parseDeclList(it.block, c), cond: cond})
		sp.nestedRules(ctx, it, base, cond, depth)
	case itemAtRule:
		name := lower(it.name)
		if name != "import" && name != "charset" && (name != "layer" || it.hasBlock) {
			*importsAllowed = false
		}
		switch name {
		case "import":
			if *importsAllowed {
				sp.importRule(ctx, it, base, cond, depth, c)
			}
		case "media":
			if it.hasBlock {
				sp.nested(ctx, it.block, base, &mediaCond{list: it.prelude, parent: cond}, depth)
			}
		case "supports":
			if it.hasBlock && evalSupports(it.prelude) {
				sp.nested(ctx, it.block, base, cond, depth)
			}
		case "layer":
			// Cascade layers (CSS Cascade 5) are flattened: a layer's
			// rules join the unlayered ones in source order, which keeps
			// what layered frameworks draw and loses only the layer
			// precedence.
			if it.hasBlock {
				sp.nested(ctx, it.block, base, cond, depth)
			}
		case "page":
			sels, ok := parsePageSelectors(it.prelude)
			if !ok || !it.hasBlock || !sp.count() {
				return
			}
			ctx, boxes := pageCtxDecls(it.block, c)
			sp.sheet.pages = append(sp.sheet.pages, pageRule{sels: sels, decls: pageDecls(it.block, c), cond: cond, context: ctx, margins: boxes})
		case "font-face":
			if !it.hasBlock {
				return
			}
			if f, ok := parseFontFace(it.block, c); ok && sp.count() {
				sp.sheet.faces = append(sp.sheet.faces, fontFaceRule{face: f, cond: cond})
			}
		default:
			if !ignoredAtRules[name] && !strings.HasPrefix(name, "-") {
				c.warn("unsupported-css-at-rule", "the @%s rule is not supported and was ignored", name)
			}
		}
	}
}

// nestedRules parses the style rules nested in the style rule it (CSS
// Nesting 1, 2 and 3): each starts with the nesting selector &, which
// stands for each selector of the parent rule in turn, and follows the
// parent's declarations in the cascade order.
func (sp *sheetParser) nestedRules(ctx context.Context, it item, base string, cond *mediaCond, depth int) {
	if sp.nesting >= maxNestingDepth {
		return
	}
	sp.nesting++
	defer func() { sp.nesting-- }()
	parents := splitCommas(trimWS(it.prelude))
	allowed := false
	newListParser(it.block).declarations(func(n item) bool {
		if n.kind != itemQualified {
			return true
		}
		var prelude []token
		for _, sel := range splitCommas(trimWS(n.prelude)) {
			for _, par := range parents {
				if len(prelude) > 0 {
					prelude = append(prelude, token{typ: tComma})
				}
				for _, t := range trimWS(sel) {
					if t.isDelim('&') {
						prelude = append(prelude, trimWS(par)...)
						continue
					}
					prelude = append(prelude, t)
				}
			}
			if len(prelude) > maxNestedPrelude {
				return true
			}
		}
		sp.rule(ctx, item{kind: itemQualified, prelude: prelude, block: n.block}, base, cond, depth, &allowed)
		return sp.err == nil
	})
}

// maxNestedPrelude caps the tokens of the selector list a nested rule
// expands to: 4096. Anti-DoS guard, not spec-mandated: each level
// multiplies the parent's selectors by its own, and real nesting expands
// to a few selectors.
const maxNestedPrelude = 4096

// nested parses the rule list inside a conditional group rule (CSS
// Conditional 3, 2).
func (sp *sheetParser) nested(ctx context.Context, block []token, base string, cond *mediaCond, depth int) {
	allowed := false
	newListParser(block).rules(false, func(it item) bool {
		sp.rule(ctx, it, base, cond, depth, &allowed)
		return sp.err == nil
	})
}

// parseDeclList parses the declarations of a style rule's block.
func parseDeclList(block []token, c *declCtx) []Declaration {
	var out []Declaration
	newListParser(block).declarations(func(it item) bool {
		if it.kind == itemDeclaration {
			if d := c.declare(it.name, it.value, it.important); d.declares() {
				out = append(out, d)
			}
		}
		return true
	})
	return out
}

// declares reports whether d takes part in the cascade.
func (d *Declaration) declares() bool {
	return d.isCustom || len(d.entries) > 0 || len(d.pending) > 0
}

// importRule handles @import (CSS Cascade 4, 2): the URL, an optional
// layer, an optional supports() condition and a media query list.
func (sp *sheetParser) importRule(ctx context.Context, it item, base string, cond *mediaCond, depth int, c *declCtx) {
	pre := trimWS(it.prelude)
	if len(pre) == 0 {
		return
	}
	ref := pre[0].value
	if pre[0].typ != tString {
		u, ok := parseURL(pre[0], "")
		if !ok {
			return
		}
		ref = u
	}
	rest := trimWS(pre[1:])
	if len(rest) > 0 && (rest[0].isIdent("layer") || rest[0].typ == tFunction && eqFold(rest[0].value, "layer")) {
		rest = trimWS(rest[1:])
	}
	if len(rest) > 0 && rest[0].typ == tFunction && eqFold(rest[0].value, "supports") {
		if !evalSupports(rest[0].children) {
			return
		}
		rest = trimWS(rest[1:])
	}
	target := resolveURL(base, ref)
	if sp.opts.Fetch == nil {
		sp.log.DebugContext(ctx, "css: @import ignored, fetching is disabled", slog.String("url", target))
		return
	}
	if slices.Contains(sp.chain, target) {
		c.warn("css-import-failed", "the @import of %s was ignored: it imports itself", target)
		return
	}
	if depth+1 > maxImportDepth || sp.imports >= maxImports {
		c.warn("css-limit", "the @import of %s was ignored: more than %d imports or %d levels", target, maxImports, maxImportDepth)
		return
	}
	sp.imports++
	body, final, err := sp.opts.Fetch(ctx, target)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			sp.err = ctxErr
			return
		}
		sp.log.DebugContext(ctx, "css: @import failed", slog.String("url", target), slog.String("error", err.Error()))
		c.warn("css-import-failed", "the @import of %s failed: %v", target, err)
		return
	}
	if len(body) > maxStylesheetBytes {
		sp.err = fmt.Errorf("importing %s of %d bytes: %w", target, len(body), ErrTooLarge)
		return
	}
	inner := cond
	if len(rest) > 0 {
		inner = &mediaCond{list: rest, parent: cond}
	}
	if final == "" {
		final = target
	}
	sp.chain = append(sp.chain, target)
	sp.flush(c)
	sp.parseSheet(ctx, body, final, inner, depth+1)
	sp.chain = sp.chain[:len(sp.chain)-1]
}

// ParseDeclarations parses a list of declarations, a style attribute's
// value (CSS Style Attributes, 3). Invalid declarations are dropped; a
// text past the stylesheet size cap yields nil. Warnings travel with the
// declarations and surface in [Computed.Warnings] when they are used.
func ParseDeclarations(src string, opts ParseOptions) []Declaration {
	if len(src) > maxStylesheetBytes {
		return nil
	}
	c := &declCtx{baseURL: opts.BaseURL}
	p := &parser{tz: newTokenizer(src, maxTokens)}
	var out []Declaration
	var pendingWarns []Warning
	p.declarations(func(it item) bool {
		if it.kind != itemDeclaration {
			return true
		}
		d := c.declare(it.name, it.value, it.important)
		if !d.declares() {
			pendingWarns = append(pendingWarns, c.warns...)
			c.warns = nil
			return true
		}
		d.warns = append(append(d.warns, pendingWarns...), c.warns...)
		pendingWarns, c.warns = nil, nil
		out = append(out, d)
		return true
	})
	if len(pendingWarns) > 0 {
		if len(out) > 0 {
			out[len(out)-1].warns = append(out[len(out)-1].warns, pendingWarns...)
		} else {
			out = append(out, Declaration{warns: pendingWarns})
		}
	}
	return out
}

// Hint returns a presentational hint (CSS 2.1, 6.4.4): the declaration
// property: value, as an HTML attribute maps it. An invalid value yields a
// Declaration that declares nothing.
func Hint(property, value string) Declaration {
	toks, _ := parseComponentValues(value)
	c := &declCtx{}
	d := c.declare(property, toks, false)
	d.warns = c.warns
	return d
}
