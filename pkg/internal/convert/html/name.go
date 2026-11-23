package html

import (
	"strings"

	dom "golang.org/x/net/html"
)

// imageName returns the name the author states for an image element n, an
// img, an input of type image or an svg element, as the accessible name
// computation gives it (Accessible Name and Description Computation 1.2,
// 4.3.2; HTML-AAM 1.0, 4.1.2 and 4.1.3; SVG-AAM 1.0, 5.1): the text of
// the elements its aria-labelledby names, else its aria-label, else what
// own returns, its alt or its title. decorative reports an image its
// author hides from a reader, which the PDF draws as an artifact (ISO
// 32000-1, 14.8.2.2.2): aria-hidden="true", or a role of presentation or
// none, or an empty own name the host language makes presentational (an
// img's alt=""), without a name aria gives it, which overrides the role
// (WAI-ARIA 1.2, 9.3 presentational roles conflict resolution).
//
// These are the author's words for the image, so the image takes them by
// default; [Options.DerivedAltText] only adds what the document states
// beside it, a figure's caption and an svg's desc.
func (b *builder) imageName(n *dom.Node, own func() (name string, presentational bool)) (name string, decorative bool) {
	if v, _ := attr(n, "aria-hidden"); strings.EqualFold(strings.TrimSpace(v), "true") {
		return "", true
	}
	if name = b.labelledBy(n); name == "" {
		v, _ := attr(n, "aria-label")
		name = bounded(collapse(v))
	}
	if name != "" {
		return name, false
	}
	if r, _ := attr(n, "role"); presentationalRole(r) {
		return "", true
	}
	name, presentational := own()
	return bounded(collapse(name)), presentational
}

// presentationalRole reports a role attribute whose first role WAI-ARIA
// 1.2 knows is presentation or none (5.3.2 and 5.4: the first token a
// user agent knows applies).
func presentationalRole(r string) bool {
	for _, tok := range strings.Fields(strings.ToLower(r)) {
		return tok == "presentation" || tok == "none"
	}
	return false
}

// maxLabelledBy bounds the IDs one aria-labelledby names that are read.
// Anti-DoS guard, not spec-mandated: a label names an element or two.
const maxLabelledBy = 16

// labelledBy returns the text of the elements whose IDs n's
// aria-labelledby lists, joined by a space (Accessible Name and
// Description Computation 1.2, 4.3.2 step 2B), "" for none.
func (b *builder) labelledBy(n *dom.Node) string {
	v, ok := attr(n, "aria-labelledby")
	if !ok {
		return ""
	}
	ids := strings.Fields(v)
	if len(ids) == 0 {
		return ""
	}
	if b.labelIDs == nil {
		b.labelIDs = map[string]*dom.Node{}
		root := n
		for root.Parent != nil {
			root = root.Parent
		}
		stack := []*dom.Node{root}
		for len(stack) > 0 && len(b.labelIDs) < MaxElements {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if c.Type == dom.ElementNode {
				if id, ok := attr(c, "id"); ok && id != "" && b.labelIDs[id] == nil {
					b.labelIDs[id] = c
				}
			}
			for k := c.FirstChild; k != nil; k = k.NextSibling {
				stack = append(stack, k)
			}
		}
	}
	var parts []string
	for i, id := range ids {
		if i == maxLabelledBy {
			break
		}
		if e := b.labelIDs[id]; e != nil {
			if s := collapse(textContent(e)); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return bounded(strings.Join(parts, " "))
}
