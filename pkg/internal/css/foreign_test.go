package css

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestParseColor_Exported(t *testing.T) {
	tests := []struct {
		s       string
		c       Color
		current bool
		ok      bool
	}{
		{"red", Color{R: 1, A: 1}, false, true},
		{" #00f ", Color{B: 1, A: 1}, false, true},
		{"rgba(0, 255, 0, 0.5)", Color{G: 1, A: 0.5}, false, true},
		{"currentColor", Color{}, true, true},
		{"transparent", Color{}, false, true},
		{"red blue", Color{}, false, false},
		{"nonsense", Color{}, false, false},
	}
	for _, tt := range tests {
		c, cur, ok := ParseColor(tt.s)
		if c != tt.c || cur != tt.current || ok != tt.ok {
			t.Errorf("ParseColor(%q) = %v, %v, %v; want %v, %v, %v", tt.s, c, cur, ok, tt.c, tt.current, tt.ok)
		}
	}
}

func TestSelector_MatchesForeignContent(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<div class="icon"><svg><g id="g"><rect class="a b"/></g></svg></div>`))
	if err != nil {
		t.Fatal(err)
	}
	var rect *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "rect" {
			rect = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	m := NewMatcher("", 1<<20)
	tests := []struct {
		sel  string
		spec uint32
		ok   bool
	}{
		{"rect", packSpec(0, 0, 1), true},
		{".icon rect.a", packSpec(0, 2, 1), true},
		{"#g > .a.b, rect", packSpec(1, 2, 0), true},
		{"g + rect", 0, false},
		{"circle", 0, false},
		{"rect::before", 0, false},
	}
	for _, tt := range tests {
		s, ok := ParseSelector(tt.sel)
		if !ok {
			t.Fatalf("ParseSelector(%q) failed", tt.sel)
		}
		spec, got := m.Match(s, rect)
		if got != tt.ok || spec != tt.spec {
			t.Errorf("Match(%q) = %v, %v; want %v, %v", tt.sel, spec, got, tt.spec, tt.ok)
		}
	}
	if _, ok := ParseSelector("a[[b"); ok {
		t.Errorf("invalid selector parsed")
	}
	if m.Exhausted() {
		t.Errorf("budget exhausted")
	}
}
