package layout

import (
	"bytes"
	"context"
	"image/color"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
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
		found = found || w.Code == codeTooDeep
	}
	if !found {
		t.Error("no warning for content past the depth cap")
	}
}

func TestGenerate_SupportedDoNotWarn(t *testing.T) {
	abs := sty(func(s *css.Style) { s.Position = css.Absolute; s.FontVariant = css.SmallCaps })
	res := run(t, doc(el(abs, txt(abs, "a"))), page(100, 20))
	codes := map[string]int{}
	for _, w := range res.Warnings {
		codes[w.Code]++
	}
	if codes[codeUnsupported] != 0 {
		t.Errorf("warnings %+v, want none: small-caps synthesised, positioning laid out", res.Warnings)
	}
}

func TestGenerate_MissingGlyphWarns(t *testing.T) {
	res := run(t, doc(el(sty(), txt(sty(), "a中"))), page(100, 20))
	found := false
	for _, w := range res.Warnings {
		found = found || w.Code == codeMissingGlyph
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
		if err := compose.Write(context.Background(), document(res), &b, compose.Options{}); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	if !bytes.Equal(build(), build()) {
		t.Error("two layouts of the same tree wrote different bytes")
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
	if len(res.Warnings) != 1 || res.Warnings[0].Code != codeImage || res.Warnings[0].Page != 1 {
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
		if w.Code == codeMissingGlyph {
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
