package layout

import (
	"context"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// paragraphs returns a tree of n paragraphs, each of its own style
// pointer, all styled alike, with a list item among them.
func paragraphs(n int) *Node {
	root := el(sty())
	for range n {
		p := sty()
		root.Children = append(root.Children, el(p, txt(p, "x x x"), el(sty(func(s *css.Style) { s.Display = css.Block }), txt(p, "y"))))
	}
	li := sty(func(s *css.Style) { s.Display = css.ListItem })
	root.Children = append(root.Children, el(li, txt(li, "z")))
	return root
}

func TestShareStylesKeepsTheLayout(t *testing.T) {
	plain := runWith(t, paragraphs(40), Options{Page: page(100, 200)})
	tree := paragraphs(40)
	ShareStyles(context.Background(), tree)
	distinct := map[*css.Style]bool{}
	var walk func(n *Node)
	walk = func(n *Node) {
		distinct[n.Style] = true
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree)
	if len(distinct) != 2 {
		t.Errorf("the shared tree holds %d styles, want 2", len(distinct))
	}
	shared := runWith(t, tree, Options{Page: page(100, 200)})
	if !reflect.DeepEqual(plain.Pages, shared.Pages) {
		t.Error("sharing the styles changed the pages")
	}
}

func TestShareStylesStopsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tree := paragraphs(2000)
	ShareStyles(ctx, tree)
	if tree.Children[len(tree.Children)-1].Style == tree.Children[0].Style {
		t.Error("a cancelled walk shared the whole tree")
	}
	ShareStyles(context.Background(), nil)
}

func TestEngineSharesAnonymousStyles(t *testing.T) {
	tree := paragraphs(10)
	ShareStyles(context.Background(), tree)
	rb := boxes(t, tree, page(100, 2000))
	anon := map[*css.Style]int{}
	var walk func(b *box)
	walk = func(b *box) {
		if b.anon {
			anon[b.st]++
		}
		for _, k := range b.kids {
			walk(k)
		}
	}
	walk(rb)
	if len(anon) != 1 {
		t.Errorf("the anonymous blocks hold %d styles, want 1", len(anon))
	}
}

func TestLayoutChargesAStyleOnce(t *testing.T) {
	peak := func(share bool) int64 {
		ctx, b, done := limits.WithBudget(context.Background(), limits.Limits{})
		defer done()
		tree := paragraphs(200)
		if share {
			ShareStyles(ctx, tree)
		}
		if _, err := Layout(ctx, tree, ahem(t), Options{Page: page(100, 200)}); err != nil {
			t.Fatalf("Layout: %v", err)
		}
		return b.Peak()
	}
	plain, shared := peak(false), peak(true)
	if shared >= plain-200*styleCost {
		t.Errorf("the account peaks at %d bytes with shared styles, %d without", shared, plain)
	}
}
