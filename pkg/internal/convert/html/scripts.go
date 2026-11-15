package html

import (
	"fmt"
	"slices"
	"strings"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// javaScriptTypes are the JavaScript MIME type essence strings (MIME
// Sniffing, 4.6): a script element whose type string matches one, ASCII
// case-insensitively, is a classic script. Immutable lookup table.
var javaScriptTypes = []string{
	"application/ecmascript", "application/javascript", "application/x-ecmascript", "application/x-javascript",
	"text/ecmascript", "text/javascript", "text/javascript1.0", "text/javascript1.1", "text/javascript1.2",
	"text/javascript1.3", "text/javascript1.4", "text/javascript1.5", "text/jscript", "text/livescript",
	"text/x-ecmascript", "text/x-javascript",
}

// runsScript reports whether a browser runs the script element n, of the
// HTML or the SVG namespace, as the HTML Standard, 4.12.1.1 prepares it:
// its type string is the type attribute stripped of ASCII whitespace,
// "text/" and the language attribute without a type, text/javascript when
// both are absent or one is empty; a JavaScript MIME type makes it a
// classic script, "module" a module script, anything else a data block
// that never runs (application/ld+json, text/template, an import map,
// speculation rules). A classic script with nomodule, an empty src and an
// inline script of white space alone run nothing.
//
// Chromium departs from the clause on one point, and litho follows it: a
// type of "module" with white space around it is a data block.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 runs a script
//     with no type, an empty type or language, " TEXT/JavaScript ",
//     text/javascript followed by a space, application/ecmascript,
//     application/x-javascript, text/jscript, text/livescript,
//     text/javascript1.5, language="JavaScript1.2" and "ecmascript", a type
//     of text/javascript beside language="vbscript", a src, an SVG script
//     with no type and with text/ecmascript, and type "module" and
//     "MODULE"; it runs none of "text/javascript; charset=utf-8",
//     application/json, application/ld+json, text/template, text/babel,
//     "javascript", " Module ", "module " and " module",
//     language="vbscript", nomodule, an empty src and an import map.
func runsScript(n *dom.Node) bool {
	typ, hasType := attr(n, "type")
	lang, hasLang := attr(n, "language")
	var classic, module bool
	switch {
	case hasType && typ == "", !hasType && hasLang && lang == "", !hasType && !hasLang:
		classic = true
	case hasType:
		classic = isJavaScriptType(strings.Trim(typ, "\t\n\f\r "))
		module = strings.EqualFold(typ, "module")
	default:
		classic = isJavaScriptType("text/" + lang)
	}
	if !classic && !module {
		return false
	}
	if _, ok := attr(n, "nomodule"); ok && classic {
		return false
	}
	if src, ok := attr(n, "src"); ok {
		return src != ""
	}
	return strings.Trim(textContent(n), "\t\n\f\r ") != ""
}

// isJavaScriptType reports a JavaScript MIME type essence match (MIME
// Sniffing, 4.6): the whole string, parameters included, matches one of
// [javaScriptTypes] ASCII case-insensitively.
func isJavaScriptType(s string) bool {
	return slices.ContainsFunc(javaScriptTypes, func(t string) bool { return strings.EqualFold(s, t) })
}

// loadHandlers are the event handler content attributes whose events a
// browser fires as a page loads and prints, with no user, by the element
// that carries them (HTML Standard, 8.1.8.2): load and error at the window
// through the body and frameset elements, which also forward pageshow and
// beforeprint, and at the elements that fetch what they show or run. A
// handler of another event, or of one the element never receives (a div's
// onload), waits for a user or never runs; afterprint fires once the page
// is printed, too late to change it. Immutable lookup table.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints what the
//     body's onload, onpageshow and onbeforeprint, an img's onload and
//     onerror, a link's onerror, an svg's onload and an SVG image's
//     onerror wrote, and nothing of a div's onload or onerror or of the
//     body's onafterprint.
var loadHandlers = map[atom.Atom][]string{
	atom.Body:     {"onload", "onerror", "onpageshow", "onbeforeprint"},
	atom.Frameset: {"onload", "onerror", "onpageshow", "onbeforeprint"},
	atom.Img:      {"onload", "onerror"},
	atom.Script:   {"onload", "onerror"},
	atom.Link:     {"onload", "onerror"},
	atom.Style:    {"onload", "onerror"},
	atom.Iframe:   {"onload", "onerror"},
	atom.Frame:    {"onload", "onerror"},
	atom.Object:   {"onload", "onerror"},
	atom.Embed:    {"onload", "onerror"},
	atom.Input:    {"onload", "onerror"},
}

// svgLoadHandlers are [loadHandlers] of the SVG elements that load: the
// outermost svg element and the elements that fetch a resource (SVG 2,
// 15.2, the load event). Immutable lookup table.
var svgLoadHandlers = map[string][]string{
	"svg":    {"onload", "onerror"},
	"image":  {"onload", "onerror"},
	"script": {"onload", "onerror"},
}

// scriptsOf returns how many scripts a browser would run on its own in the
// element n: a script element that runs ([runsScript]), each handler of
// [loadHandlers] holding code, and an iframe or a frame whose src is a
// javascript: URL, which the browser runs as it navigates the frame (HTML
// Standard, 7.4.2.3.2). A javascript: URL of a link or a form waits for a
// user and does not count; neither does one an object element names,
// which Chromium does not run.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints what an
//     iframe's javascript: src wrote, and nothing of an object's data or
//     an a element's href holding one.
func scriptsOf(n *dom.Node) int {
	count := 0
	var handlers []string
	switch n.Namespace {
	case "":
		handlers = loadHandlers[n.DataAtom]
		switch n.DataAtom { //nolint:exhaustive // every other element runs nothing of its own
		case atom.Script:
			if runsScript(n) {
				count++
			}
		case atom.Iframe, atom.Frame:
			if src, ok := attr(n, "src"); ok {
				src = strings.Trim(src, "\t\n\f\r ")
				if len(src) >= 11 && strings.EqualFold(src[:11], "javascript:") {
					count++
				}
			}
		}
	case "svg":
		handlers = svgLoadHandlers[n.Data]
		if n.Data == "script" && runsScript(n) {
			count++
		}
	}
	for _, name := range handlers {
		if v, ok := attr(n, name); ok && strings.Trim(v, "\t\n\f\r ") != "" {
			count++
		}
	}
	return count
}

// scriptWarning returns the warning of a document holding scripts a
// browser would run, none for scripts == 0. litho runs no script: what one
// would draw or change is missing from the output, so its effect is
// [warn.Omitted]. noscript content stays out all the same (see
// [skipped]): the page's own layout expects its scripts to have run.
func scriptWarning(scripts int) []typeset.Warning {
	if scripts == 0 {
		return nil
	}
	msg := fmt.Sprintf("%d scripts did not run: litho runs no script", scripts)
	if scripts == 1 {
		msg = "1 script did not run: litho runs no script"
	}
	return []typeset.Warning{{Code: warn.ScriptNotRun, Effect: warn.Omitted, Message: msg}}
}
