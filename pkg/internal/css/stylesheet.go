package css

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/warn"
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
	// FetchCode returns the code of the warning an error of Fetch is
	// reported with, so that an import left out reads as the caller's
	// other resources do. nil reports every error as
	// [warn.ResourceFailed].
	FetchCode func(err error) warn.Code
	// Logger receives debug records of dropped imports; nil discards them.
	Logger *slog.Logger
	// predefined parses the predefined counter styles, whose names no
	// document rule may define.
	predefined bool
	// userAgent parses the user agent sheet, the one sheet whose
	// declarations may use the engine's internal values (see
	// [declCtx.userAgent]).
	userAgent bool
}

// Stylesheet is a parsed stylesheet with its imports. It is immutable and
// safe for concurrent use.
type Stylesheet struct {
	rules []styleRule
	pages []pageRule
	faces []fontFaceRule
	// featureValues are the @font-feature-values rules, one entry per
	// family each names, in order.
	featureValues []featureValuesRule
	// counterStyles are the @counter-style rules, in order.
	counterStyles []*counterStyleRule
	// keyframes are the @keyframes rules, in order.
	keyframes []*keyframesRule
	// layers are the paths of the cascade layers the sheet names, in the
	// order they first appear (CSS Cascade 5, 6.4), each rule holding an
	// index into it or unlayered.
	layers []string
	warns  []Warning
}

// Warnings returns what parsing dropped or approximated, deduplicated and
// capped. The slice is a copy.
func (s *Stylesheet) Warnings() []Warning { return append([]Warning(nil), s.warns...) }

// styleRule is a qualified rule with a valid selector list.
type styleRule struct {
	selectors []*complexSel
	decls     []Declaration
	cond      *mediaCond
	layer     int
}

// fontFaceRule is an @font-face rule and its media condition.
type fontFaceRule struct {
	face  FontFace
	cond  *mediaCond
	layer int
}

// featureValuesRule is an @font-feature-values rule's values for one
// family and its media condition.
type featureValuesRule struct {
	values FontFeatureValues
	cond   *mediaCond
	layer  int
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
	// layer is the index of the cascade layer the rules being parsed
	// belong to, unlayered outside any; layerIndex finds a layer path's
	// index, anon counts the anonymous layers.
	layer      int
	layerIndex map[string]int
	anon       int
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
	sp := &sheetParser{opts: opts, log: opts.Logger, sheet: &Stylesheet{}, layer: unlayered}
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

// flush moves c's warnings into the sheet's warnings.
func (sp *sheetParser) flush(c *declCtx) {
	for _, w := range c.warns {
		sp.warns.add(w)
	}
	c.warns = nil
}

// ignoredAtRules are at-rules litho drops with their blocks without a
// warning: they style nothing a static page shows. @starting-style gives
// the style a transition starts from on an element's first style (CSS
// Transitions 2, 3.3), and a converted document runs no transition.
var ignoredAtRules = map[string]bool{
	"-moz-keyframes": true, "-o-keyframes": true, "starting-style": true,
	"font-palette-values": true,
	"property":            true, "viewport": true, "-ms-viewport": true, "view-transition": true,
	"namespace": true, "charset": true,
}

// rule handles one top-level or nested rule.
func (sp *sheetParser) rule(ctx context.Context, it item, base string, cond *mediaCond, depth int, importsAllowed *bool) {
	if err := ctx.Err(); err != nil {
		sp.err = err
		return
	}
	c := &declCtx{baseURL: base, userAgent: sp.opts.userAgent}
	defer sp.flush(c)
	switch it.kind { //nolint:exhaustive // declarations and errors cannot reach the top level
	case itemQualified:
		*importsAllowed = false
		sels, err := parseSelectorList(it.prelude, 0, false)
		if err != nil {
			if err.unsupported != "" {
				c.warn(warn.CSSUnsupported, warn.Approximated, "a rule with the selector %s was ignored", err.unsupported)
			}
			return
		}
		sp.styleBlock(ctx, it.prelude, sels, it.block, base, cond, depth, c)
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
			// Cascade layers (CSS Cascade 5, 6.4): a block holds the
			// rules of one layer, a statement fixes the order of names.
			if it.hasBlock {
				sp.enterLayer(it.prelude, func() { sp.nested(ctx, it.block, base, cond, depth) })
			} else {
				sp.declareLayers(it.prelude)
			}
		case "page":
			sels, unsupported, ok := parsePageSelectors(it.prelude)
			if !ok {
				if unsupported != "" {
					c.warn(warn.CSSUnsupported, warn.Approximated, "an @page rule with the selector %s was ignored", unsupported)
				} else {
					c.warn(warn.CSSInvalid, warn.Approximated, "an @page rule with an invalid selector was ignored")
				}
				return
			}
			if !it.hasBlock || !sp.count() {
				return
			}
			ctx, boxes := pageCtxDecls(it.block, c)
			sp.sheet.pages = append(sp.sheet.pages, pageRule{sels: sels, decls: pageDecls(it.block, c), cond: cond, context: ctx, margins: boxes, layer: sp.layer})
		case "counter-style":
			if !it.hasBlock {
				return
			}
			if r, ok := parseCounterStyleRule(it.prelude, it.block, c, sp.opts.predefined); ok && sp.count() {
				r.cond, r.layer = cond, sp.layer
				sp.sheet.counterStyles = append(sp.sheet.counterStyles, r)
			}
		case "font-face":
			if !it.hasBlock {
				return
			}
			if f, ok := parseFontFace(it.block, c); ok && sp.count() {
				sp.sheet.faces = append(sp.sheet.faces, fontFaceRule{face: f, cond: cond, layer: sp.layer})
			}
		case "keyframes", "-webkit-keyframes":
			// The Compatibility Standard defines @-webkit-keyframes as
			// @keyframes; Chromium drops the -moz- and -o- names.
			if !it.hasBlock {
				return
			}
			if r, ok := parseKeyframesRule(it.prelude, it.block, c); ok && sp.count() {
				r.cond, r.layer = cond, sp.layer
				sp.sheet.keyframes = append(sp.sheet.keyframes, r)
			}
		case "font-feature-values":
			if !it.hasBlock || !sp.count() {
				return
			}
			for _, v := range parseFontFeatureValues(it.prelude, it.block) {
				sp.sheet.featureValues = append(sp.sheet.featureValues, featureValuesRule{values: v, cond: cond, layer: sp.layer})
			}
		default:
			if !ignoredAtRules[name] && !strings.HasPrefix(name, "-") {
				c.warn(warn.CSSUnsupported, warn.Approximated, "the @%s rule is not supported and was ignored", name)
			}
		}
	}
}

// styleBlock adds the rules of the block of a style rule whose selector
// list prelude parses to sels (CSS Nesting 1): the declarations under
// sels, and the rules nested among them after the declarations before
// them; declarations after a nested rule form a rule of their own under
// sels, after it in the cascade order (CSS Nesting 1, 3.3, nested
// declarations rules). Conditional group rules nest with their contents
// relative to sels (3.2).
func (sp *sheetParser) styleBlock(ctx context.Context, prelude []token, sels []*complexSel, block []token, base string, cond *mediaCond, depth int, c *declCtx) {
	var decls []Declaration
	first := true
	flush := func() {
		if len(decls) == 0 && !first || !sp.count() {
			return
		}
		sp.sheet.rules = append(sp.sheet.rules, styleRule{selectors: sels, decls: decls, cond: cond, layer: sp.layer})
		decls, first = nil, false
	}
	if sp.nesting >= maxNestingDepth {
		sp.sheet.rules = append(sp.sheet.rules, styleRule{selectors: sels, decls: parseDeclList(block, c), cond: cond, layer: sp.layer})
		return
	}
	sp.nesting++
	defer func() { sp.nesting-- }()
	p := newListParser(block)
	p.nested = true
	p.declarations(func(n item) bool {
		switch n.kind { //nolint:exhaustive // errors drop out
		case itemDeclaration:
			if d := c.declare(n.name, n.value, n.important); d.declares() {
				decls = append(decls, d)
			}
		case itemQualified:
			flush()
			sp.nestedRule(ctx, prelude, n, base, cond, depth)
		case itemAtRule:
			flush()
			sp.nestedAtRule(ctx, prelude, sels, n, base, cond, depth, c)
		}
		return sp.err == nil
	})
	flush()
}

// nestedRule adds the style rule n nested in a rule of selector list
// parent (CSS Nesting 1, 2): the nesting selector & stands for
// :is(parent), whose specificity is the most specific of the parent's
// selectors (6); a selector without & is relative to it, a descendant
// unless it starts with a combinator (2.1).
func (sp *sheetParser) nestedRule(ctx context.Context, parent []token, n item, base string, cond *mediaCond, depth int) {
	is := []token{{typ: tColon}, {typ: tFunction, value: "is", children: trimWS(parent)}}
	var prelude []token
	for _, sel := range splitCommas(trimWS(n.prelude)) {
		sel = trimWS(sel)
		if len(prelude) > 0 {
			prelude = append(prelude, token{typ: tComma})
		}
		if hasNesting(sel) {
			prelude = append(prelude, nest(sel, is)...)
		} else {
			prelude = append(prelude, is...)
			prelude = append(prelude, token{typ: tWhitespace})
			prelude = append(prelude, sel...)
		}
		if len(prelude) > maxNestedPrelude {
			return
		}
	}
	allowed := false
	sp.rule(ctx, item{kind: itemQualified, prelude: prelude, block: n.block}, base, cond, depth, &allowed)
}

// hasNesting reports a selector that holds the nesting selector &, at
// any depth.
func hasNesting(l []token) bool {
	for _, t := range l {
		if t.isDelim('&') || len(t.children) > 0 && hasNesting(t.children) {
			return true
		}
	}
	return false
}

// nest returns l with each nesting selector replaced by is.
func nest(l []token, is []token) []token {
	out := make([]token, 0, len(l))
	for _, t := range l {
		switch {
		case t.isDelim('&'):
			out = append(out, is...)
		case len(t.children) > 0:
			t.children = nest(t.children, is)
			out = append(out, t)
		default:
			out = append(out, t)
		}
	}
	return out
}

// nestedAtRule adds the at-rule n nested in a style rule of selector
// list parent, parsed to sels: @media, @supports and @layer hold
// declarations under sels and rules relative to them (CSS Nesting 1,
// 3.2), under their condition.
func (sp *sheetParser) nestedAtRule(ctx context.Context, parent []token, sels []*complexSel, n item, base string, cond *mediaCond, depth int, c *declCtx) {
	if !n.hasBlock {
		return
	}
	switch name := lower(n.name); name {
	case "media":
		sp.styleBlock(ctx, parent, sels, n.block, base, &mediaCond{list: n.prelude, parent: cond}, depth, c)
	case "supports":
		if evalSupports(n.prelude) {
			sp.styleBlock(ctx, parent, sels, n.block, base, cond, depth, c)
		}
	case "layer":
		sp.enterLayer(n.prelude, func() { sp.styleBlock(ctx, parent, sels, n.block, base, cond, depth, c) })
	default:
		if !ignoredAtRules[name] && !strings.HasPrefix(name, "-") {
			c.warn(warn.CSSUnsupported, warn.Approximated, "the @%s rule is not supported and was ignored", name)
		}
	}
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
	// layer and layer(<layer-name>) put the imported rules in a new
	// anonymous layer or the named one (CSS Cascade 5, 2.2).
	var layer []token
	inLayer := false
	if len(rest) > 0 && (rest[0].isIdent("layer") || rest[0].typ == tFunction && eqFold(rest[0].value, "layer")) {
		if rest[0].typ == tFunction {
			if layer = significant(rest[0].children); len(layer) == 0 {
				return
			}
		}
		inLayer = true
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
		c.warn(warn.ResourceFailed, warn.Approximated, "the @import of %s was ignored: it imports itself", target)
		return
	}
	if depth+1 > maxImportDepth || sp.imports >= maxImports {
		c.warn(warn.LimitReached, warn.Approximated, "the @import of %s was ignored: more than %d imports or %d levels", target, maxImports, maxImportDepth)
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
		code := warn.ResourceFailed
		if sp.opts.FetchCode != nil {
			code = sp.opts.FetchCode(err)
		}
		// An import left out loses styles, never content: the document's
		// content prints, styled without the sheet.
		c.warn(code, warn.Approximated, "the @import of %s failed: %v", target, err)
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
	parse := func() { sp.parseSheet(ctx, body, final, inner, depth+1) }
	if !inLayer {
		parse()
	} else if !sp.enterLayer(layer, parse) {
		c.warn(warn.CSSInvalid, warn.Approximated, "the @import of %s was ignored: its layer name is invalid", target)
	}
	sp.chain = sp.chain[:len(sp.chain)-1]
}

// ParseDeclarations parses a list of declarations, a style attribute's
// value (CSS Style Attributes, 3). Invalid declarations are dropped; a
// text past the stylesheet size cap yields nil, and one past the token or
// nesting cap the declarations read before it. Warnings travel with the
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
