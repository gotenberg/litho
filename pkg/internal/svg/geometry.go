package svg

import (
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// geometryProps are the geometry properties (SVG 2, 6.7 and 10) and the
// elements each applies to, whose attributes of the same name are their
// presentation attributes: a style rule or a style attribute that sets
// one wins over the attribute. The x, y, width and height of an svg
// element stay attributes, as Chromium draws them, departing from SVG 2,
// 5.1.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) draws a
//     circle, an ellipse, a rect, a path, an image and a use by the cx,
//     cy, r, rx, ry, x, y, width, height and d its style sheet sets over
//     their attributes, unitless numbers, percentages and em included,
//     an important rule over the style attribute, rx auto as ry, a rect
//     of width auto not at all and d none as no path; it keeps a text's
//     x and y and a nested svg's x, y, width and height from their
//     attributes.
var geometryProps = map[string][]string{
	"cx":     {"circle", "ellipse"},
	"cy":     {"circle", "ellipse"},
	"r":      {"circle"},
	"rx":     {"ellipse", "rect"},
	"ry":     {"ellipse", "rect"},
	"x":      {"rect", "image", "use", "foreignObject"},
	"y":      {"rect", "image", "use", "foreignObject"},
	"width":  {"rect", "image", "foreignObject"},
	"height": {"rect", "image", "foreignObject"},
	"d":      {"path"},
}

// geometry returns the value of the geometry property key of the element
// n and whether it has one: the last valid declaration of its style rules
// and style attribute in the cascade, else its attribute key. A value a
// declaration gives is a length, a number of user units or auto, and for
// d a path() or none, which ends in the path data or "".
func (d *Document) geometry(n *html.Node, key string) (string, bool) {
	if slices.Contains(geometryProps[key], n.Data) {
		d.mu.Lock()
		decls := d.declsOf(n)
		d.mu.Unlock()
		for _, dc := range slices.Backward(decls) {
			if dc.prop != key {
				continue
			}
			if key == "d" {
				if v, ok := pathProperty(dc.value); ok {
					return v, true
				}
				continue
			}
			if _, ok := parseLength(dc.value); ok || strings.EqualFold(dc.value, "auto") {
				return dc.value, true
			}
		}
	}
	return attrOK(n, key)
}

// geometryOf returns geometry's value, "" for none.
func (d *Document) geometryOf(n *html.Node, key string) string {
	v, _ := d.geometry(n, key)
	return v
}

// pathProperty returns the path data of a d property, path() with a
// string or none, the latter as no data (SVG 2, 9.3.1).
func pathProperty(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") {
		return "", true
	}
	name, rest, ok := strings.Cut(v, "(")
	if !ok || !strings.EqualFold(strings.TrimSpace(name), "path") || !strings.HasSuffix(rest, ")") {
		return "", false
	}
	s := strings.TrimSpace(strings.TrimSuffix(rest, ")"))
	if len(s) < 2 || s[0] != s[len(s)-1] || s[0] != '"' && s[0] != '\'' {
		return "", false
	}
	return s[1 : len(s)-1], true
}
