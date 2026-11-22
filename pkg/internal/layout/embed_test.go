package layout

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// embedded returns a picture of an embedded document of no natural size,
// 300 by 150 px by default, which make lays out on its viewport of w by h
// points.
func embedded(make func(w, h float64) *Embedded) *Picture {
	return &Picture{
		Width: px(300), Height: px(150), NoNaturalWidth: true, NoNaturalHeight: true, NoRatio: true,
		Embed: func(_ context.Context, w, h float64) *Embedded { return make(w, h) },
	}
}

// redDoc returns a document that paints a red square of side px pixels at
// its viewport's origin, hanging from a P of its own.
func redDoc(side float64) *Embedded {
	p := &typeset.StructElem{Role: "P"}
	return &Embedded{
		Content:   []typeset.Fragment{&typeset.Path{Ops: rectOps(typeset.Rect{Width: px(side), Height: px(side)}), Fill: &typeset.Fill{Color: typeset.Color{R: 1, A: 1}}, Parent: p}},
		Structure: &typeset.StructElem{Role: "Document", Lang: "fr", Kids: []*typeset.StructElem{p}},
	}
}

// The default object size of a frame: CSS 2.1, 10.3.2 and 10.6.2.
func TestEmbed_TakesTheDefaultObjectSize(t *testing.T) {
	rb := boxes(t, doc(&Node{Kind: Replaced, Style: sty(), Role: "DocumentFragment", Picture: embedded(func(float64, float64) *Embedded { return nil })}), page(400, 400))
	if b := kid(t, rb, 0, 0); !near(b.w/css.Pt, 300) || !near(b.h/css.Pt, 150) {
		t.Errorf("size %v by %v, want 300 by 150", b.w/css.Pt, b.h/css.Pt)
	}
}

// A document paints in the content box, its viewport, cut at its edges
// (HTML Standard, 15.3.2), whatever it paints past them.
func TestEmbed_PaintsClippedToTheContentBox(t *testing.T) {
	var gotW, gotH float64
	pic := embedded(func(w, h float64) *Embedded {
		gotW, gotH = w, h
		return redDoc(10)
	})
	frame := &Node{Kind: Replaced, Style: sty(width(3), height(2), margin(1), func(s *css.Style) { s.Display = css.Block }), Role: "DocumentFragment", Picture: pic}
	assertPixels(t, run(t, doc(frame), page(6, 4)), 1, `
		______
		_RRR__
		_RRR__
		______`)
	if !near(gotW/css.Pt, 3) || !near(gotH/css.Pt, 2) {
		t.Errorf("viewport %v by %v px, want 3 by 2", gotW/css.Pt, gotH/css.Pt)
	}
}

// The document's structure hangs from the frame's element, its leaves
// keeping their elements, and the frame takes the document's language.
func TestEmbed_StructureHangsFromTheFrame(t *testing.T) {
	frame := &Node{Kind: Replaced, Style: sty(width(3), height(2)), Role: "DocumentFragment", Picture: embedded(func(float64, float64) *Embedded { return redDoc(2) })}
	res := run(t, doc(el(sty(), &Node{Kind: Text, Style: sty(), Text: "x"}), frame), page(10, 30))
	var fragment *typeset.StructElem
	var walk func(*typeset.StructElem)
	walk = func(e *typeset.StructElem) {
		if e.Role == "DocumentFragment" {
			fragment = e
		}
		for _, k := range e.Kids {
			walk(k)
		}
	}
	walk(res.Structure)
	if fragment == nil {
		t.Fatal("no DocumentFragment in the structure tree")
	}
	if len(fragment.Kids) != 1 || fragment.Kids[0].Role != "P" || fragment.Lang != "fr" {
		t.Fatalf("DocumentFragment kids %v, lang %q; want one P, fr", fragment.Kids, fragment.Lang)
	}
	found := false
	var leaves func([]typeset.Fragment)
	leaves = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Group:
				leaves(v.Content)
			case *typeset.Path:
				found = found || v.Parent == fragment.Kids[0]
			}
		}
	}
	leaves(res.Pages[0].Content)
	if !found {
		t.Error("the document's square does not hang from its P")
	}
}

// A link of the document is placed in the frame's viewport, cut to it,
// and one outside it is left out.
func TestEmbed_LinksArePlacedAndCut(t *testing.T) {
	lk := &typeset.StructElem{Role: "Link"}
	make := func(float64, float64) *Embedded {
		d := redDoc(1)
		d.Structure.Kids = append(d.Structure.Kids, lk)
		d.Links = []typeset.Link{
			{Rect: typeset.Rect{X: px(1), Y: px(1), Width: px(10), Height: px(1)}, URI: "https://example.com/in", Parent: lk, Contents: "in"},
			{Rect: typeset.Rect{X: px(1), Y: px(5), Width: px(1), Height: px(1)}, URI: "https://example.com/out", Parent: lk},
			{Rect: typeset.Rect{X: px(1), Y: px(5), Width: px(1), Height: px(1)}, Dest: &typeset.Dest{Page: 1}},
		}
		return d
	}
	frame := &Node{Kind: Replaced, Style: sty(width(3), height(2), margin(1), func(s *css.Style) { s.Display = css.Block }), Role: "DocumentFragment", Picture: embedded(make)}
	res := run(t, doc(frame), page(10, 10))
	links := res.Pages[0].Links
	if len(links) != 1 {
		t.Fatalf("%d links, want 1: %+v", len(links), links)
	}
	l := links[0]
	want := typeset.Rect{X: px(2), Y: px(2), Width: px(2), Height: px(1)}
	if l.URI != "https://example.com/in" || !near(l.Rect.X, want.X) || !near(l.Rect.Y, want.Y) || !near(l.Rect.Width, want.Width) || !near(l.Rect.Height, want.Height) || l.Parent != lk || l.Contents != "in" {
		t.Errorf("link %+v, want %s at %+v under the document's Link", l, "https://example.com/in", want)
	}
}

// A document whose groups nest deeper than MaxEmbedDepth is left out with
// a warning.
func TestEmbed_TooDeepIsLeftOut(t *testing.T) {
	make := func(float64, float64) *Embedded {
		d := redDoc(4)
		for range MaxEmbedDepth + 1 {
			d.Content = []typeset.Fragment{&typeset.Group{Opacity: 1, Content: d.Content}}
		}
		return d
	}
	frame := &Node{Kind: Replaced, Style: sty(width(3), height(2)), Role: "DocumentFragment", Picture: embedded(make)}
	res := run(t, doc(frame), page(4, 4))
	assertPixels(t, res, 1, `
		____
		____
		____
		____`)
	if !hasWarning(res, warn.LimitReached, "a frame whose document nests groups") {
		t.Errorf("warnings %v, want the frame left out", res.Warnings)
	}
}

// A document whose structure would nest the tree past maxStructDepth
// hangs its content from the frame's element, with a warning.
func TestEmbed_DeepStructureHangsFromTheFrame(t *testing.T) {
	make := func(float64, float64) *Embedded {
		d := redDoc(2)
		e := d.Structure.Kids[0]
		for range maxStructDepth {
			k := &typeset.StructElem{Role: "Div"}
			e.Kids = append(e.Kids, k)
			e = k
		}
		return d
	}
	frame := &Node{Kind: Replaced, Style: sty(width(3), height(2)), Role: "DocumentFragment", Picture: embedded(make)}
	res := run(t, doc(frame), page(4, 4))
	if !hasWarning(res, warn.LimitReached, "hangs from the frame's element") {
		t.Errorf("warnings %v, want the structure cut", res.Warnings)
	}
}

// A second paint of the same layout does not hang the document's
// structure twice.
func TestEmbed_HangsOnce(t *testing.T) {
	e := &engine{embeds: map[*typeset.StructElem]int{}}
	p := &painter{e: e}
	frame := &typeset.StructElem{Role: "DocumentFragment"}
	e.embeds[frame] = 2
	d := redDoc(1)
	if !p.hang(frame, d.Structure) || !p.hang(frame, d.Structure) {
		t.Fatal("hang refused")
	}
	if len(frame.Kids) != 1 {
		t.Errorf("%d kids after two paints, want 1", len(frame.Kids))
	}
}

// hasWarning reports a warning of res with code whose message holds sub.
func hasWarning(res *Result, code warn.Code, sub string) bool {
	for _, w := range res.Warnings {
		if w.Code == code && strings.Contains(w.Message, sub) {
			return true
		}
	}
	return false
}

// The viewport's edges sit on whole CSS pixels of the page, as Blink lays
// a frame's document out, so the document does not paint over a border
// sharing a pixel with it.
func TestEmbed_ViewportOnWholePixels(t *testing.T) {
	var gotW, gotH float64
	pic := embedded(func(w, h float64) *Embedded {
		gotW, gotH = w, h
		return redDoc(10)
	})
	frame := &Node{Kind: Replaced, Style: sty(width(3.4), height(2.2), func(s *css.Style) {
		s.Display = css.Block
		s.Margin[css.Top] = css.Pts(px(1.3))
		s.Margin[css.Left] = css.Pts(px(0.7))
	}), Role: "DocumentFragment", Picture: pic}
	run(t, doc(frame), page(6, 6))
	// From (0.7, 1.3) to (4.1, 3.5): from (1, 1) to (4, 4).
	if !near(gotW/css.Pt, 3) || !near(gotH/css.Pt, 3) {
		t.Errorf("viewport %v by %v px, want 3 by 3", gotW/css.Pt, gotH/css.Pt)
	}
}

// A frame whose document has no room left under the groups around it is
// left out with a warning that says it is a frame's document.
func TestEmbed_DroppedUnderGroupsIsNamed(t *testing.T) {
	make := func(float64, float64) *Embedded {
		d := redDoc(4)
		for range MaxEmbedDepth {
			d.Content = []typeset.Fragment{&typeset.Group{Opacity: 1, Content: d.Content}}
		}
		return d
	}
	n := &Node{Kind: Replaced, Style: sty(width(3), height(2)), Role: "DocumentFragment", Picture: embedded(make)}
	for range maxGroupDepth {
		n = el(sty(func(s *css.Style) { s.Opacity = 0.5 }), n)
	}
	res := run(t, doc(n), page(4, 4))
	if !hasWarning(res, warn.LimitReached, "a frame's document under groups") {
		t.Errorf("warnings %v, want the frame named", res.Warnings)
	}
	if hasWarning(res, warn.LimitReached, "a vector picture") {
		t.Errorf("warnings %v name a vector picture", res.Warnings)
	}
}

// A link of the document to a part of it the frame shows goes to that
// place in the frame's box, its element kept (HTML Standard, 7.4.6.4: the
// fragment scrolls the frame's own navigable); a link to a part the frame
// does not show is left out with a warning, never written as a
// destination the reader cannot see.
func TestEmbed_FragmentLinksGoInsideTheFrame(t *testing.T) {
	doc0 := redDoc(1)
	target := doc0.Structure.Kids[0]
	doc0.Links = []typeset.Link{
		{Rect: typeset.Rect{X: 0, Y: 0, Width: px(1), Height: px(1)}, Dest: &typeset.Dest{Page: 1, X: px(2), Y: px(1), Elem: target}},
		{Rect: typeset.Rect{X: px(1), Y: 0, Width: px(1), Height: px(1)}, Dest: &typeset.Dest{Page: 1, X: 0, Y: px(30)}},
	}
	make := func(float64, float64) *Embedded { return doc0 }
	frame := &Node{Kind: Replaced, Style: sty(width(3), height(2), margin(1), func(s *css.Style) { s.Display = css.Block }), Role: "DocumentFragment", Picture: embedded(make)}
	res := run(t, doc(frame), page(10, 10))
	links := res.Pages[0].Links
	if len(links) != 1 || links[0].Dest == nil {
		t.Fatalf("links %+v, want one internal link", links)
	}
	d := links[0].Dest
	if d.Page != 1 || !near(d.X, px(3)) || !near(d.Y, px(2)) || d.Elem != target {
		t.Errorf("destination %+v, want page 1 at (3, 2) px on the P", d)
	}
	if !hasWarning(res, warn.LinkUnresolved, "does not show") {
		t.Errorf("warnings %v, want the hidden target reported", res.Warnings)
	}
}
