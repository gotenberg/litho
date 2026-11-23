package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestTrailingLineBreak gives the mark of a paragraph that ends with a
// text wrapping break a line of its own, as Word prints it, and leaves a
// break inside the text as it is.
func TestTrailingLineBreak(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want int
	}{
		{"trailing", `<w:p><w:r><w:t>a</w:t><w:br/></w:r></w:p>`, 2},
		{"alone", `<w:p><w:r><w:br/></w:r></w:p>`, 2},
		{"inside", `<w:p><w:r><w:t>a</w:t><w:br/><w:t>b</w:t></w:r></w:p>`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := build(t, tt.body, nil)
			ps := role(b.root, "P")
			if len(ps) != 1 {
				t.Fatalf("paragraphs %d, want 1", len(ps))
			}
			if got := countBreaks(ps[0]); got != tt.want {
				t.Errorf("line breaks %d, want %d", got, tt.want)
			}
		})
	}
}

// countBreaks counts the line breaks under n.
func countBreaks(n *layout.Node) int {
	c := 0
	if n.Kind == layout.LineBreak {
		c++
	}
	for _, k := range n.Children {
		c += countBreaks(k)
	}
	return c
}

// TestBuild_TextEdgeSpaces locks the white space at the edges of a w:t:
// dropped unless the element keeps it with xml:space "preserve", as Word
// reads it (see [wordText]).
func TestBuild_TextEdgeSpaces(t *testing.T) {
	b := build(t, `<w:p><w:r><w:t>SDT </w:t></w:r><w:r><w:t> Run</w:t></w:r>`+
		`<w:r><w:t xml:space="preserve"> kept </w:t></w:r><w:r><w:t>end</w:t></w:r></w:p>`, nil)
	if got, want := textOf(b.root), "SDTRun kept end"; got != want {
		t.Errorf("text %q, want %q", got, want)
	}
}

// TestBuild_TextLineEnds locks a line end inside a w:t: a space on the
// line, not a break, as Word reads it (see [wordText]).
func TestBuild_TextLineEnds(t *testing.T) {
	b := build(t, "<w:p><w:r><w:t xml:space=\"preserve\">One.\r\nTwo.\nThree.</w:t></w:r></w:p>", nil)
	if got, want := textOf(b.root), "One. Two. Three."; got != want {
		t.Errorf("text %q, want %q", got, want)
	}
	if n := countBreaks(b.root); n != 0 {
		t.Errorf("%d breaks, want none", n)
	}
}

// TestBuild_RunBorder draws a run's border around its text on every side,
// its space between them, and none for a run without one or the paragraph
// whose mark has one.
func TestBuild_RunBorder(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:pPr><w:rPr><w:bdr w:val="single" w:sz="8" w:space="2"/></w:rPr></w:pPr>` +
		`<w:r><w:rPr><w:bdr w:val="single" w:sz="8" w:space="2"/></w:rPr><w:t>boxed</w:t></w:r><w:r><w:t>plain</w:t></w:r></w:p>`
	b := build(t, body, nil)
	runs := find(b.root, func(n *layout.Node) bool {
		return n.Kind == layout.Element && n.Role == "" && len(n.Children) > 0 && n.Children[0].Kind == layout.Text
	})
	if len(runs) != 2 {
		t.Fatalf("%d runs, want 2", len(runs))
	}
	for _, r := range runs {
		boxed := textOf(r) == "boxed"
		for side := range r.Style.Border {
			if got := r.Style.Border[side].Width == 1 && r.Style.Padding[side] == css.Pts(2); got != boxed {
				t.Errorf("%q side %d: border %v padding %v", textOf(r), side, r.Style.Border[side], r.Style.Padding[side])
			}
		}
	}
	if p := role(b.root, "P")[0]; p.Style.Border[css.Top].Width != 0 {
		t.Errorf("the paragraph took its mark's border")
	}
}

// TestRuns_LeftToRightInRightToLeftParagraphs embeds a run without w:rtl
// in a right-to-left paragraph as left-to-right text (ECMA-376 Part 1,
// 17.3.2.30), and leaves a run of w:rtl, and any run of a left-to-right
// paragraph, in the paragraph's direction.
func TestRuns_LeftToRightInRightToLeftParagraphs(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:pPr><w:bidi/></w:pPr><w:r><w:t>in M$</w:t></w:r><w:r><w:rPr><w:rtl/></w:rPr><w:t>rtl</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>plain</w:t></w:r></w:p>`
	runs := find(build(t, body, nil).root, func(n *layout.Node) bool { return n.Kind == layout.Text })
	want := map[string][2]any{
		"in M$": {css.LTR, css.Embed},
		"rtl":   {css.RTL, css.BidiNormal},
		"plain": {css.LTR, css.BidiNormal},
	}
	for _, r := range runs {
		w, ok := want[r.Text]
		if !ok {
			continue
		}
		delete(want, r.Text)
		if r.Style.Direction != w[0] || r.Style.UnicodeBidi != w[1] {
			t.Errorf("run %q: direction %v, unicode-bidi %v; want %v", r.Text, r.Style.Direction, r.Style.UnicodeBidi, w)
		}
	}
	if len(want) > 0 {
		t.Errorf("runs not found: %v", want)
	}
}
