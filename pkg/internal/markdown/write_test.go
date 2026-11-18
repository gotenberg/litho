package markdown

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// line lays text out as glyphs on one baseline at (x, y), 6 points apart,
// appending them to shown and returning their indices.
func line(shown *typography.ShownText, text string, x, y float64) []int {
	var indices []int
	for i, r := range text {
		indices = append(indices, len(shown.Elements))
		shown.Elements = append(shown.Elements, models.TextElement{
			Text: string(r), X: x + float64(i)*6, Y: y, Width: 6, FontSize: 12, SpaceWidth: 3, MCID: -1,
		})
		shown.PageOps = append(shown.PageOps, -1)
	}
	return indices
}

// leaf is a content node holding the glyphs at indices of page.
func leaf(page int, indices []int) *node {
	return &node{glyphs: refsOn(page, indices)}
}

func el(role string, kids ...*node) *node {
	return &node{role: role, kids: kids}
}

// write renders root over pages as [Write] does once it has a tree.
func write(t *testing.T, pages []Page, root *node, opts Options) string {
	t.Helper()
	w := &writer{ctx: context.Background(), pages: pages, opts: opts, used: make([][]bool, len(pages))}
	for i, p := range pages {
		w.used[i] = make([]bool, len(p.Shown.Elements))
	}
	return joinBlocks(w.withLeftovers(w.blocks(root)))
}

// TestWriteMarksNestButNeverCross pins the inline marks: a mark inside
// another nests, a link crossing a mark is left out, and a code span holds
// its text unescaped and no other mark (CommonMark, 6.1).
func TestWriteMarksNestButNeverCross(t *testing.T) {
	t.Parallel()
	var shown typography.ShownText
	a := line(&shown, "use go_test now", 72, 700)
	page := []Page{{Shown: shown}}
	// The link covers "go_test", glyphs 4 to 10.
	linked := []Page{{Shown: shown, Links: []Link{{Rect: [4]float64{97, 695, 137, 715}, URI: "https://go.dev"}}}}

	cases := []struct {
		name  string
		pages []Page
		root  *node
		want  string
	}{
		{
			"code span unescaped", page,
			el("P", leaf(0, a[:4]), el("Code", leaf(0, a[4:11])), leaf(0, a[11:])),
			"use `go_test` now\n",
		},
		{
			"strong holds emphasis", page,
			el("P", el("Strong", leaf(0, a[:4]), el("Em", leaf(0, a[4:11]))), leaf(0, a[11:])),
			"**use *go\\_test*** now\n",
		},
		{
			"link inside code", linked,
			el("P", leaf(0, a[:4]), el("Code", leaf(0, a[4:11])), leaf(0, a[11:])),
			"use `go_test` now\n",
		},
		{
			"link holds the code", linked,
			el("P", leaf(0, a[:4]), el("Code", leaf(0, a[4:6])), leaf(0, a[6:])),
			"use [`go`\\_test](https://go.dev) now\n",
		},
		{
			"link crossing strong left out", linked,
			el("P", el("Strong", leaf(0, a[:6])), leaf(0, a[6:])),
			"**use go**\\_test now\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := write(t, c.pages, el("Document", c.root), Options{}); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestWriteLists pins how a list is numbered: by its /ListNumbering, else by
// its first label, else by the marker the layout leaves in an item's text,
// which is then dropped.
func TestWriteLists(t *testing.T) {
	t.Parallel()
	var labels, markers typography.ShownText
	l1, b1 := line(&labels, "3.", 72, 700), line(&labels, "Install", 90, 700)
	l2, b2 := line(&labels, "4.", 72, 685), line(&labels, "Run", 90, 685)
	m1 := line(&markers, "• Fast", 72, 650)
	m2 := line(&markers, "• Safe", 72, 635)

	labelled := func(numbering string) *node {
		list := el("L",
			el("LI", el("Lbl", leaf(0, l1)), el("LBody", leaf(0, b1))),
			el("LI", el("Lbl", leaf(0, l2)), el("LBody", leaf(0, b2))))
		list.numbering = numbering
		return list
	}
	cases := []struct {
		name  string
		shown typography.ShownText
		root  *node
		want  string
	}{
		{"numbered by its first label", labels, labelled(""), "3. Install\n4. Run\n"},
		{"bulleted by its numbering", labels, labelled("Disc"), "- Install\n- Run\n"},
		{
			"marker in the text", markers,
			el("L", el("LI", el("LBody", leaf(0, m1))), el("LI", el("LBody", leaf(0, m2)))),
			"- Fast\n- Safe\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := write(t, []Page{{Shown: c.shown}}, el("Document", c.root), Options{}); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestWriteKeepsTextTheStructureMisses checks that the glyphs no block holds
// follow the blocks of their page, before the next page's, and that the
// furniture the layout sets apart is kept only when asked.
func TestWriteKeepsTextTheStructureMisses(t *testing.T) {
	t.Parallel()
	var one, two typography.ShownText
	h := line(&one, "Title", 72, 700)
	line(&one, "Stray", 72, 600)
	foot := line(&one, "Page 1", 72, 40)
	p := line(&two, "Body", 72, 700)
	pages := []Page{{Shown: one}, {Shown: two}}
	root := func() *node {
		return el("Document", el("H1", leaf(0, h)), el(roleArtifact, leaf(0, foot)), el("P", leaf(1, p)))
	}

	if got, want := write(t, pages, root(), Options{}), "# Title\n\nStray\n\nBody\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := write(t, pages, root(), Options{Artifacts: true}), "# Title\n\nPage 1\n\nStray\n\nBody\n"; got != want {
		t.Errorf("with artifacts: got %q, want %q", got, want)
	}
}

// TestEscape pins the escaping of text that would otherwise read as Markdown.
func TestEscape(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{"# not a heading", `\# not a heading`},
		{"- not an item", `\- not an item`},
		{"1. not an item", `1\. not an item`},
		{"2024. A year", `2024\. A year`},
		{"-5 degrees", "-5 degrees"},
		{"> not a quote", `\> not a quote`},
		{"plain", "plain"},
	} {
		if got := escapeLineStart(c.in); got != c.want {
			t.Errorf("escapeLineStart(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got, want := escapeInline(`a*b_c[d]e<f>`+"`"+`\`), `a\*b\_c\[d\]e\<f>`+"\\`"+`\\`; got != want {
		t.Errorf("escapeInline = %q, want %q", got, want)
	}
	for _, c := range []struct{ in, want string }{
		{"https://example.com/a?b=c", "https://example.com/a?b=c"},
		{"https://example.com/a b", "<https://example.com/a b>"},
		{"https://example.com/(a)", "<https://example.com/(a)>"},
	} {
		if got := linkDestination(c.in); got != c.want {
			t.Errorf("linkDestination(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
