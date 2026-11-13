package css

import (
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxContentItems bounds the items of one content value: 256. Anti-DoS
// guard, not spec-mandated: a generated label holds a few items, and each
// one costs a counter lookup per pseudo-element.
const maxContentItems = 256

// maxCounterOps bounds the counters one counter-reset, counter-increment
// or counter-set value names: 64. Anti-DoS guard, not spec-mandated: a
// document scopes a handful of counters per element, and the converter
// walks every one of them on every element that carries the value.
const maxCounterOps = 64

// maxQuoteDepth bounds the quote pairs of a quotes value: 32. Anti-DoS
// guard, not spec-mandated: quotes nest a few levels deep.
const maxQuoteDepth = 32

// contentProp defines content (CSS Generated Content 3, 1.1).
func contentProp() propDef {
	return propDef{
		name: "content", parse: parseContent,
		compute: func(_ *styler, st *Style, v any) {
			if c, ok := v.(Content); ok {
				st.Content = c
			}
		},
		copyFrom: field(func(s *Style) *Content { return &s.Content }),
	}
}

// contentKeywords are the quote items of a content list.
var contentKeywords = map[string]ContentItemKind{
	"open-quote": ItemOpenQuote, "close-quote": ItemCloseQuote,
	"no-open-quote": ItemNoOpenQuote, "no-close-quote": ItemNoCloseQuote,
}

// parseContent parses normal, none, or a list of content items with an
// optional alternative text after a slash (CSS Generated Content 3, 1.1).
// An item litho does not generate, such as an image function it does not
// draw, makes the value invalid with a warning.
func parseContent(v []token, c *declCtx) (any, bool) {
	if k, ok := keyword(v); ok {
		switch k {
		case "normal":
			return Content{Kind: ContentNormal}, true
		case "none":
			return Content{Kind: ContentNone}, true
		}
	}
	items := v
	var alt []token
	for i, t := range v {
		if t.isDelim('/') {
			items, alt = v[:i], v[i+1:]
			break
		}
	}
	if len(items) == 0 || len(items) > maxContentItems {
		return nil, false
	}
	out := Content{Kind: ContentList, Items: make([]ContentItem, 0, len(items))}
	for _, t := range items {
		it, ok := contentItem(t, c)
		if !ok {
			return nil, false
		}
		out.Items = append(out.Items, it)
	}
	var b strings.Builder
	for _, t := range alt {
		if t.typ != tString {
			if t.typ == tFunction && eqFold(t.value, "attr") {
				continue
			}
			return nil, false
		}
		b.WriteString(t.value)
	}
	out.Alt = b.String()
	return out, true
}

// contentItem parses one item of a content list.
func contentItem(t token, c *declCtx) (ContentItem, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tString:
		return ContentItem{Kind: ItemString, Text: t.value}, true
	case tIdent:
		if k, ok := contentKeywords[lower(t.value)]; ok {
			return ContentItem{Kind: k}, true
		}
		return ContentItem{}, false
	case tURL:
		u, _ := parseURL(t, c.baseURL)
		return ContentItem{Kind: ItemImage, Text: u}, true
	case tFunction:
	default:
		return ContentItem{}, false
	}
	name := lower(t.value)
	args := splitCommas(significant(t.children))
	switch name {
	case "url", "src":
		u, ok := parseURL(t, c.baseURL)
		return ContentItem{Kind: ItemImage, Text: u}, ok
	case "attr":
		return attrItem(args)
	case "counter":
		if len(args) < 1 || len(args) > 2 {
			return ContentItem{}, false
		}
		it := ContentItem{Kind: ItemCounter, Style: Decimal}
		var ok bool
		if it.Text, ok = counterName(args[0]); !ok {
			return ContentItem{}, false
		}
		if len(args) == 2 {
			if it.Style, it.StyleName, ok = counterStyleArg(args[1], c); !ok {
				return ContentItem{}, false
			}
		}
		return it, true
	case "counters":
		if len(args) < 2 || len(args) > 3 || len(args[1]) != 1 || args[1][0].typ != tString {
			return ContentItem{}, false
		}
		it := ContentItem{Kind: ItemCounters, Style: Decimal, Separator: args[1][0].value}
		var ok bool
		if it.Text, ok = counterName(args[0]); !ok {
			return ContentItem{}, false
		}
		if len(args) == 3 {
			if it.Style, it.StyleName, ok = counterStyleArg(args[2], c); !ok {
				return ContentItem{}, false
			}
		}
		return it, true
	}
	if it, known, valid := targetItem(name, args, c); known {
		return it, valid
	}
	if it, known, valid := gcpmItem(name, args); known {
		return it, valid
	}
	if unsupportedImages[name] {
		c.warn(warn.CSSUnsupported, warn.Omitted, "content: %s() is not generated", name)
		return ContentItem{}, false
	}
	c.warn(warn.CSSUnsupported, warn.Omitted, "content: %s() is not supported and the declaration was ignored", name)
	return ContentItem{}, false
}

// targetParts are the second arguments of target-text() (CSS Generated
// Content 3, 6.3): those of content() in string-set but marker.
var targetParts = map[string]bool{"content": true, "before": true, "after": true, "first-letter": true}

// targetItem parses the cross-reference functions of CSS Generated Content
// 3, 6: target-counter(), target-counters() and target-text(). ok reports
// a function it knows; valid whether its arguments parse.
//
//	target-counter( [ <string> | <url> ] , <custom-ident> , <counter-style>? )
//	target-counters( [ <string> | <url> ] , <custom-ident> , <string> , <counter-style>? )
//	target-text( [ <string> | <url> ] , [ content | before | after | first-letter ]? )
//
// The URL may be an attr() of the originating element, as the draft's
// examples write it: attr(href url) or attr(href).
func targetItem(name string, args [][]token, c *declCtx) (it ContentItem, ok, valid bool) {
	var lo, hi int
	switch name {
	case "target-counter":
		it.Kind, lo, hi = ItemTargetCounter, 2, 3
	case "target-counters":
		it.Kind, lo, hi = ItemTargetCounters, 3, 4
	case "target-text":
		it.Kind, lo, hi = ItemTargetText, 1, 2
	default:
		return ContentItem{}, false, false
	}
	if len(args) < lo || len(args) > hi {
		return ContentItem{}, true, false
	}
	if it.Target, it.TargetAttr, valid = targetURL(args[0], c); !valid {
		return ContentItem{}, true, false
	}
	if it.Kind == ItemTargetText {
		it.Text = "content"
		if len(args) == 2 {
			k, isKw := keyword(args[1])
			if !isKw || !targetParts[k] {
				return ContentItem{}, true, false
			}
			it.Text = k
		}
		return it, true, true
	}
	if it.Text, valid = counterName(args[1]); !valid {
		return ContentItem{}, true, false
	}
	it.Style = Decimal
	rest := args[2:]
	if it.Kind == ItemTargetCounters {
		if len(rest[0]) != 1 || rest[0][0].typ != tString {
			return ContentItem{}, true, false
		}
		it.Separator, rest = rest[0][0].value, rest[1:]
	}
	if len(rest) == 1 {
		if it.Style, it.StyleName, valid = counterStyleArg(rest[0], c); !valid {
			return ContentItem{}, true, false
		}
	}
	return it, true, true
}

// targetURL parses the first argument of a cross-reference function: a
// string or a url() naming the target, or attr() of an attribute holding
// it, read as a URL or a string (CSS Values 5, 7.7). attr reports the
// attribute form, whose name it returns.
func targetURL(arg []token, c *declCtx) (u string, attr, ok bool) {
	if len(arg) != 1 {
		return "", false, false
	}
	t := arg[0]
	switch {
	case t.typ == tString:
		return strings.TrimSpace(t.value), false, true
	case t.typ == tFunction && eqFold(t.value, "attr"):
		a := significant(t.children)
		if len(a) < 1 || len(a) > 2 || a[0].typ != tIdent || len(a) == 2 && !a[1].isIdent("url") && !a[1].isIdent("string") {
			return "", false, false
		}
		return lower(a[0].value), true, true
	}
	u, ok = parseURL(t, c.baseURL)
	return u, false, ok
}

// attrItem parses the arguments of attr(): a name, an optional type or
// unit litho reads as string, and an optional string fallback (CSS Values
// 5, 7.7).
func attrItem(args [][]token) (ContentItem, bool) {
	if len(args) < 1 || len(args) > 2 || len(args[0]) < 1 || len(args[0]) > 2 || args[0][0].typ != tIdent {
		return ContentItem{}, false
	}
	it := ContentItem{Kind: ItemAttr, Text: lower(args[0][0].value)}
	if len(args[0]) == 2 && !args[0][1].isIdent("string") {
		return ContentItem{}, false
	}
	if len(args) == 2 {
		if len(args[1]) != 1 || args[1][0].typ != tString {
			return ContentItem{}, false
		}
		it.Fallback = args[1][0].value
	}
	return it, true
}

// counterName returns the <counter-name> of a counter() or counters()
// argument: a <custom-ident> other than none (CSS Lists 3, 4.8), so no
// CSS-wide keyword (CSS Values 4, 4.2).
func counterName(arg []token) (string, bool) {
	if len(arg) != 1 || arg[0].typ != tIdent || arg[0].isIdent("none") || cssWideKeywords[lower(arg[0].value)] {
		return "", false
	}
	return arg[0].value, true
}

// counterStyleArg returns the <counter-style> argument of counter() or
// counters() (CSS Lists 3, 4.8): a style [ListStyleType] covers, or
// ListNamed and the name of any other, symbols() included.
func counterStyleArg(arg []token, c *declCtx) (ListStyleType, string, bool) {
	if len(arg) != 1 {
		return 0, "", false
	}
	switch arg[0].typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		k := lower(arg[0].value)
		if lt, ok := listStyleTypes[k]; ok {
			return lt, "", true
		}
		if cssWideKeywords[k] {
			return 0, "", false
		}
		return ListNamed, counterStyleName(arg[0].value), true
	case tFunction:
		if eqFold(arg[0].value, "symbols") {
			if name, ok := parseSymbolsFunction(arg[0], c); ok {
				return ListNamed, name, true
			}
		}
	}
	return 0, "", false
}

// counterProp defines counter-reset, counter-increment or counter-set
// (CSS Lists 3, 4.1 to 4.3): none, or names each followed by an optional
// integer, def when it is left out.
func counterProp(name string, def int, f func(*Style) *[]CounterOp) propDef {
	reset := name == "counter-reset"
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			if k, ok := keyword(v); ok && k == "none" {
				return []CounterOp(nil), true
			}
			var out []CounterOp
			for i := 0; i < len(v); i++ {
				t := v[i]
				op := CounterOp{Value: def}
				switch {
				case t.typ == tIdent && !t.isIdent("none"):
					op.Name = t.value
				case reset && t.typ == tFunction && eqFold(t.value, "reversed"):
					a := significant(t.children)
					if len(a) != 1 || a[0].typ != tIdent || a[0].isIdent("none") {
						return nil, false
					}
					op.Name, op.Reversed, op.Implicit = a[0].value, true, true
				default:
					return nil, false
				}
				if i+1 < len(v) {
					if n, ok := parseInteger(v[i+1]); ok {
						op.Value, op.Implicit = n, false
						i++
					}
				}
				if len(out) == maxCounterOps {
					return nil, false
				}
				out = append(out, op)
			}
			return out, len(out) > 0
		},
		compute: func(_ *styler, st *Style, v any) {
			if ops, ok := v.([]CounterOp); ok {
				*f(st) = ops
			}
		},
		copyFrom: field(f),
	}
}

// quotesProp defines quotes (CSS Generated Content 3, 4.1): auto, none,
// or pairs of strings. The value that computes to the parent's content
// language has no settled name in the draft (Issue 5478) and is refused,
// match-parent, an earlier draft's name for it, included.
func quotesProp() propDef {
	return propDef{
		name: "quotes", inherited: true,
		parse: func(v []token, _ *declCtx) (any, bool) {
			if k, ok := keyword(v); ok {
				switch k {
				case "auto":
					return Quotes{}, true
				case "none":
					return Quotes{None: true}, true
				}
				return nil, false
			}
			if len(v) == 0 || len(v)%2 != 0 || len(v) > 2*maxQuoteDepth {
				return nil, false
			}
			q := Quotes{Pairs: make([][2]string, 0, len(v)/2)}
			for i := 0; i < len(v); i += 2 {
				if v[i].typ != tString || v[i+1].typ != tString {
					return nil, false
				}
				q.Pairs = append(q.Pairs, [2]string{v[i].value, v[i+1].value})
			}
			return q, true
		},
		compute: func(_ *styler, st *Style, v any) {
			if q, ok := v.(Quotes); ok {
				st.Quotes = q
			}
		},
		copyFrom: field(func(s *Style) *Quotes { return &s.Quotes }),
	}
}

// TargetUse is what the cross-reference functions of a document's
// stylesheets read (CSS Generated Content 3, 6).
type TargetUse struct {
	// Any reports a content value that holds one.
	Any bool
	// Counters are the counters target-counter() and target-counters()
	// read, page and pages aside, sorted.
	Counters []string
}

// TargetUse returns what the content values of the style rules and the
// page-margin boxes read through target-counter(), target-counters() and
// target-text(). A content value holding var() is parsed only when
// cascaded, and is not read.
func (r *Resolver) TargetUse() TargetUse {
	var u TargetUse
	scan := func(decls []Declaration) {
		for _, d := range decls {
			for _, e := range d.entries {
				c, ok := e.v.(Content)
				if !ok || e.id != pContent {
					continue
				}
				for _, it := range c.Items {
					switch it.Kind { //nolint:exhaustive // the other items read no target
					case ItemTargetText:
						u.Any = true
					case ItemTargetCounter, ItemTargetCounters:
						u.Any = true
						if it.Text != "page" && it.Text != "pages" && !slices.Contains(u.Counters, it.Text) {
							u.Counters = append(u.Counters, it.Text)
						}
					}
				}
			}
		}
	}
	for i := range r.rules {
		scan(r.rules[i].rule.decls)
	}
	for i := range r.pages {
		for _, m := range r.pages[i].rule.margins {
			scan(m.decls)
		}
	}
	slices.Sort(u.Counters)
	return u
}
