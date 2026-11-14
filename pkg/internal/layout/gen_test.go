package layout

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

func TestGenerate_DisplayNoneAndColumns(t *testing.T) {
	none := sty(display(css.DisplayNone))
	col := sty(display(css.TableColumn))
	rb := boxes(t, doc(el(sty(), txt(sty(), "a"), el(none, txt(none, "hidden")), el(col), txt(sty(), "b"))), page(100, 20))
	p := kid(t, rb, 0, 0)
	if len(p.kids) != 2 || !p.inlineKids {
		t.Fatalf("%d kids, want the two text runs alone", len(p.kids))
	}
}

func TestGenerate_DepthCapWarns(t *testing.T) {
	n := txt(inlineSty(), "deep")
	for range maxTreeDepth + 10 {
		n = el(inlineSty(), n)
	}
	res := run(t, el(sty(), n), page(100, 20))
	found := false
	for _, w := range res.Warnings {
		found = found || w.Code == warn.LimitReached
	}
	if !found {
		t.Error("no warning for content past the depth cap")
	}
}

func TestGenerate_SupportedDoNotWarn(t *testing.T) {
	abs := sty(func(s *css.Style) { s.Position = css.Absolute; s.FontVariant = css.SmallCaps })
	res := run(t, doc(el(abs, txt(abs, "a"))), page(100, 20))
	if len(res.Warnings) != 0 {
		t.Errorf("warnings %+v, want none: small-caps synthesised, positioning laid out", res.Warnings)
	}
}

func TestGenerate_MissingGlyphWarns(t *testing.T) {
	res := run(t, doc(el(sty(), txt(sty(), "a中"))), page(100, 20))
	found := false
	for _, w := range res.Warnings {
		found = found || w.Code == warn.MissingGlyph
	}
	if !found {
		t.Errorf("warnings %+v, want the missing glyph", res.Warnings)
	}
}

func TestStructure_FigureAltAndPruning(t *testing.T) {
	img := &Node{Kind: Replaced, Style: sty(), Picture: pic(color.NRGBA{R: 255, A: 255}, 2, 2), Role: "Figure", Alt: "a red square"}
	empty := el(sty())
	empty.Role = "Sect"
	res := run(t, doc(img, empty), page(10, 10))
	kids := res.Structure.Kids
	if len(kids) != 1 || kids[0].Role != "Figure" || kids[0].Alt != "a red square" {
		t.Fatalf("structure kids %+v, want the Figure alone: the empty Sect is pruned", kids)
	}
}

func TestStructure_DocumentRoleReused(t *testing.T) {
	root := el(sty(), el(sty(), txt(sty(), "a")))
	root.Role = "Document"
	root.Lang = "de"
	res := run(t, root, page(30, 20))
	if res.Structure.Role != "Document" || len(res.Structure.Kids) != 1 || res.Structure.Lang != "de" {
		t.Fatalf("structure %+v", res.Structure)
	}
	// The Document is a grouping element: its text hangs from a P of its
	// own (ISO 32005, Table 5).
	if p := res.Structure.Kids[0]; p.Role != "P" || texts(res, 1)[0].run.Parent != p {
		t.Error("text does not hang from a P under the Document element")
	}
}

func TestLayout_Deterministic(t *testing.T) {
	build := func() []byte {
		li := sty(display(css.ListItem))
		root := doc(el(sty(bg(lime), border(2, css.Groove, red)), txt(sty(), "aa bb cc dd"), el(li, txt(li, "x"))),
			el(sty(float(css.FloatRight), width(20), height(20), bg(blue))), el(sty(), txt(sty(), "ee ff gg hh ii")))
		res := run(t, root, page(60, 40))
		var b bytes.Buffer
		if err := typeset.Write(context.Background(), document(res), &b, typeset.Options{}); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	if !bytes.Equal(build(), build()) {
		t.Error("two layouts of the same tree wrote different bytes")
	}
}

// TestLayout_CancelledWhileGenerating checks that a context that ends
// while the boxes of a one-page document are generated fails the layout:
// the document reaches no second page, where the engine checks it too.
func TestLayout_CancelledWhileGenerating(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	item := sty(display(css.ListItem), func(s *css.Style) { s.ListStyleImage = "marker.png" })
	kids := make([]*Node, 0, 1+4*ctxCheckEvery)
	kids = append(kids, el(item))
	for range 4 * ctxCheckEvery {
		kids = append(kids, el(sty()))
	}
	loader := func(context.Context, string) (*Picture, error) {
		cancel()
		return pic(color.Black, 1, 1), nil
	}
	_, err := Layout(ctx, el(sty(), kids...), ahem(t), Options{Page: page(40, 20), Image: loader})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Layout = %v, want context.Canceled", err)
	}
}

func TestLayout_Concurrent(t *testing.T) {
	fonts := ahem(t)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			_, err := Layout(context.Background(), doc(el(sty(), txt(sty(), "aa bb cc dd ee"))), fonts, Options{Page: page(30, 30)})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestBackground_NoLoaderWarns(t *testing.T) {
	res := run(t, doc(el(sty(height(2), func(s *css.Style) { s.BackgroundImage = "x.png" }))), page(10, 10))
	if len(res.Warnings) != 1 || res.Warnings[0].Code != warn.PictureUnreadable || res.Warnings[0].Page != 1 {
		t.Errorf("warnings %+v, want one image warning on page 1", res.Warnings)
	}
}

func TestInline_EmptyBoxMakesNoLine(t *testing.T) {
	rb := boxes(t, doc(el(sty(), el(inlineSty()))), page(10, 10))
	if p := kid(t, rb, 0, 0); len(p.lines) != 0 || p.h != 0 {
		t.Errorf("%d lines, height %v: an empty inline box makes no line", len(p.lines), p.h)
	}
}

func TestGenerate_MissingEmojiWarnOnce(t *testing.T) {
	res := run(t, doc(el(sty(), txt(sty(), "a\U0001F680"), el(sty(), txt(sty(), "\U0001F389")))), page(100, 40))
	var msgs []string
	for _, w := range res.Warnings {
		if w.Code == warn.MissingGlyph {
			msgs = append(msgs, w.Message)
		}
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "emoji") {
		t.Errorf("missing-glyph warnings %q, want one for the emoji", msgs)
	}
}

// TestStructure_ActualText checks that a node's ActualText reaches its
// structure element: a role-less text run and a role-less inline element
// each take a Span carrying it, and a node with a role keeps its role. The
// P's own text "a" takes the anonymous Span of interleaved content.
func TestStructure_ActualText(t *testing.T) {
	sym := txt(inlineSty(), "b")
	sym.ActualText = "β"
	span := el(inlineSty(), txt(inlineSty(), "c"))
	span.ActualText = "γ"
	link := el(inlineSty(), txt(inlineSty(), "d"))
	link.Role, link.ActualText = "Link", "δ"
	p := el(sty(), txt(inlineSty(), "a"), sym, span, link)
	p.Role = "P"
	res := run(t, doc(p), page(100, 20))
	if len(res.Structure.Kids) != 1 {
		t.Fatalf("structure %+v", res.Structure)
	}
	var got []string
	for _, k := range res.Structure.Kids[0].Kids {
		got = append(got, k.Role+":"+k.ActualText)
	}
	if strings.Join(got, " ") != "Span: Span:β Span:γ Link:δ" {
		t.Errorf("kids %q, want a Span per role-less node and the Link", got)
	}
	for _, r := range texts(res, 1) {
		if r.text == "b" && (r.run.Parent == nil || r.run.Parent.ActualText != "β") {
			t.Errorf("the run b hangs from %+v", r.run.Parent)
		}
	}
}

// TestGenerate_MissingGlyphWarnsOnceWhereverLaidOut checks that a code
// point no font covers is reported once a document, whether its text is
// laid out once or laid out again: in a block that break-inside: avoid
// moves to the next page, in a paragraph whose orphans move it, in lines
// a column set balances, and in text-wrap: balance lines.
func TestGenerate_MissingGlyphWarnsOnceWhereverLaidOut(t *testing.T) {
	avoid := func(s *css.Style) { s.BreakInside = css.InsideAvoid; s.Orphans, s.Widows = 1, 1 }
	balance := func(s *css.Style) { s.TextWrapStyle = css.WrapStyleBalance }
	cols := func(s *css.Style) { s.Columns.Count = 2 }
	lines := func(mods ...func(*css.Style)) *Node {
		st := sty(mods...)
		return el(st, txt(st, "a 中 a a 中"))
	}
	for name, root := range map[string]*Node{
		"laid out once":         doc(lines()),
		"moved by break-inside": doc(el(sty(height(10))), lines(avoid)),
		"moved by orphans":      doc(el(sty(height(20))), lines()),
		"balanced lines":        doc(lines(balance)),
		"balanced columns":      doc(el(sty(cols), lines())),
		"twice in the document": doc(lines(), el(sty(height(10))), lines(avoid)),
	} {
		t.Run(name, func(t *testing.T) {
			res := run(t, root, page(10, 30))
			var msgs []string
			for _, w := range res.Warnings {
				if w.Code == warn.MissingGlyph {
					msgs = append(msgs, w.Message)
				}
			}
			if len(msgs) != 1 || !strings.Contains(msgs[0], "中") {
				t.Errorf("missing-glyph warnings %q, want one for 中", msgs)
			}
		})
	}
}

// TestGenerate_RootTable holds a root of display table to CSS 2.1, 17.4:
// it is a table wrapper box, as wide as its content, not the page.
func TestGenerate_RootTable(t *testing.T) {
	t.Parallel()
	for _, d := range []css.Display{css.Table, css.InlineTable} {
		root := el(sty(display(d)), el(sty(), el(sty(width(20), height(10)))))
		rb := boxes(t, root, page(100, 50))
		if rb.tpart != partWrapper {
			t.Fatalf("display %v: root is no table wrapper", d)
		}
		if got := rect(rb); got[2] != 20 {
			t.Errorf("display %v: root %v wide, want 20", d, got[2])
		}
	}
}

// TestGenerate_RootTableCanvas holds the canvas of a root table to CSS 2.1,
// 14.2: the root's background, on its table box, or else its first child
// element's.
func TestGenerate_RootTableCanvas(t *testing.T) {
	t.Parallel()
	rb := boxes(t, el(sty(display(css.Table), bg(red)), el(sty(height(10)))), page(100, 50))
	if c := canvasBox(rb); c == nil || c != rb.tdata.table {
		t.Errorf("canvas box %v, want the root's table box", c)
	}
	rb = boxes(t, el(sty(display(css.Table)), el(sty(height(10), bg(red)))), page(100, 50))
	if c := canvasBox(rb); c == nil || c.node == nil || !hasBackground(c.st) {
		t.Errorf("canvas box %v, want the first child element's", c)
	}
}
