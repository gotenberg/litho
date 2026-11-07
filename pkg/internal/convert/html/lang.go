package html

import (
	"strings"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// rootLanguage returns the language of the root element root: its lang
// attribute, else the pragma-set default language of a
// <meta http-equiv="content-language"> in its head, the first such meta
// whose value holds no comma (HTML Standard, 3.2.6.2 and 4.2.5.3), "" for
// none. The text stack picks the CJK faces of that language.
func rootLanguage(root *dom.Node) string {
	if root == nil {
		return ""
	}
	if lang, ok := attr(root, "lang"); ok {
		return strings.TrimSpace(lang)
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != dom.ElementNode || c.DataAtom != atom.Head {
			continue
		}
		for m := c.FirstChild; m != nil; m = m.NextSibling {
			if m.Type != dom.ElementNode || m.DataAtom != atom.Meta {
				continue
			}
			if v, ok := attr(m, "http-equiv"); !ok || !strings.EqualFold(strings.TrimSpace(v), "content-language") {
				continue
			}
			v, _ := attr(m, "content")
			if v = strings.TrimSpace(v); v != "" && !strings.Contains(v, ",") {
				return v
			}
		}
	}
	return ""
}
