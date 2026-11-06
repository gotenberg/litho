package css

import (
	"context"
	"math"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// a4 is an A4 print medium.
var a4 = Media{Type: "print", Width: 595.28, Height: 841.89}

// parseHTML parses an HTML document.
func parseHTML(t *testing.T, src string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parsing HTML: %v", err)
	}
	return doc
}

// mustParse parses an author stylesheet.
func mustParse(t *testing.T, src string) *Stylesheet {
	t.Helper()
	s, err := Parse(context.Background(), []byte(src), ParseOptions{})
	if err != nil {
		t.Fatalf("parsing CSS: %v", err)
	}
	return s
}

// computed styles doc against the UA sheet and author, with HTML hints.
type computed struct {
	t   *testing.T
	doc *html.Node
	c   *Computed
}

func compute(t *testing.T, doc, author string) computed {
	t.Helper()
	return computeMedia(t, doc, author, a4)
}

func computeMedia(t *testing.T, doc, author string, m Media) computed {
	t.Helper()
	root := parseHTML(t, doc)
	r := NewResolver(m, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: mustParse(t, author), Origin: Author}})
	c, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return computed{t: t, doc: root, c: c}
}

// byID finds the element with the id.
func byID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode {
		for _, a := range n.Attr {
			if a.Key == "id" && a.Val == id {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := byID(c, id); f != nil {
			return f
		}
	}
	return nil
}

// byTag finds the first element named tag.
func byTag(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := byTag(c, tag); f != nil {
			return f
		}
	}
	return nil
}

// of returns the style of the element with the id.
func (c computed) of(id string) *Style {
	c.t.Helper()
	n := byID(c.doc, id)
	if n == nil {
		c.t.Fatalf("no element #%s", id)
	}
	s := c.c.Style(n)
	if s == nil {
		c.t.Fatalf("no style for #%s", id)
	}
	return s
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func pts(v float64) Dimension { return Pts(v) }

func pct(v float64) Dimension { return Dimension{Kind: Percent, Value: v} }

func dimEq(a, b Dimension) bool { return a.Kind == b.Kind && near(a.Value, b.Value) }

func colorEq(a, b Color) bool {
	return near(a.R, b.R) && near(a.G, b.G) && near(a.B, b.B) && near(a.A, b.A)
}

func rgb(r, g, b uint8) Color { return rgb8(r, g, b) }

// hasWarning reports whether ws holds a warning with code whose message
// contains part.
func hasWarning(ws []Warning, code, part string) bool {
	for _, w := range ws {
		if w.Code == code && strings.Contains(w.Message, part) {
			return true
		}
	}
	return false
}
