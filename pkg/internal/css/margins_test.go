package css

import (
	"reflect"
	"strings"
	"testing"
)

// resolverOf returns the resolver of the author sheet src.
func resolverOf(t *testing.T, src string) *Resolver {
	t.Helper()
	return NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: mustParse(t, src), Origin: Author}})
}

func TestPageProperty_Computes(t *testing.T) {
	c := compute(t, `<div id=a></div><div id=b></div><div id=c></div><div id=f><p id=g></p></div><div id=h></div>`,
		`#a { page: Chapter } #b { page: auto } #c { page: 3 } #f { display: flex } #g { page: x } #h { page: inherit }`)
	for _, tt := range []struct{ id, want string }{{"a", "Chapter"}, {"b", ""}, {"c", ""}, {"g", ""}, {"h", ""}} {
		if got := c.of(tt.id).Page; got != tt.want {
			t.Errorf("#%s page %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestPageIn_MarginBoxes(t *testing.T) {
	r := resolverOf(t, `
		@page { font-size: 20px; color: red;
			@top-left { content: "TL" }
			@bottom-center { content: counter(page); text-align: left; color: blue }
			@left-top { content: normal }
			@right-bottom { content: none }
			@top-right { margin: 3px }
		}
		@page :first { @top-left { content: none } }
		@page :blank { @top-left { content: "blank" } }
		@page chapter { @top-left { content: "chapter" } }
	`)
	if !r.HasMarginBoxes() || !r.HasNamedPages() {
		t.Fatal("margin boxes or named pages not reported")
	}
	ps := r.PageIn(PageSelector{Right: true}, nil)
	if len(ps.MarginBoxes) != 2 {
		t.Fatalf("%d boxes, want 2: content normal, none and absent generate nothing", len(ps.MarginBoxes))
	}
	tl := ps.MarginBoxes[MarginTopLeft]
	if tl == nil || tl.Content.Items[0].Text != "TL" || tl.FontSize != 15 || tl.TextAlign != AlignLeft || tl.VerticalAlign.Kind != Middle {
		t.Errorf("top-left %+v: want TL, 15pt inherited from the page context, left, middle", tl)
	}
	if tl.Color != (Color{R: 1, A: 1}) {
		t.Errorf("top-left colour %+v, want the page context's red", tl.Color)
	}
	bc := ps.MarginBoxes[MarginBottomCenter]
	if bc == nil || bc.TextAlign != AlignLeft || bc.Content.Items[0].Kind != ItemCounter {
		t.Errorf("bottom-center %+v: the author's text-align wins over the default", bc)
	}
	for _, tt := range []struct {
		name string
		sel  PageSelector
		want string
	}{
		{"first removes it", PageSelector{First: true, Right: true}, ""},
		{"blank outranks first", PageSelector{First: true, Right: true, Blank: true}, "blank"},
		{"named outranks blank", PageSelector{Name: "chapter", Right: true, Blank: true}, "chapter"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := ""
			if st := r.PageIn(tt.sel, nil).MarginBoxes[MarginTopLeft]; st != nil {
				got = st.Content.Items[0].Text
			}
			if got != tt.want {
				t.Errorf("top-left %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPageIn_Defaults(t *testing.T) {
	var src string
	for _, n := range marginBoxNames {
		src += "@" + n + " { content: 'x' } "
	}
	ps := resolverOf(t, "@page { "+src+"}").PageIn(PageSelector{}, nil)
	if len(ps.MarginBoxes) != int(NumMarginBoxes) {
		t.Fatalf("%d boxes, want %d", len(ps.MarginBoxes), NumMarginBoxes)
	}
	for b, st := range ps.MarginBoxes {
		ta := map[string]TextAlign{"left": AlignLeft, "right": AlignRight, "center": AlignCenter}[marginDefaults[b][0]]
		va := map[string]AlignKind{"top": AlignTop, "middle": Middle, "bottom": AlignBottom}[marginDefaults[b][1]]
		if st.TextAlign != ta || st.VerticalAlign.Kind != va {
			t.Errorf("%v: text-align %v vertical-align %v, want %v %v", b, st.TextAlign, st.VerticalAlign.Kind, ta, va)
		}
		if st.Display != Block {
			t.Errorf("%v: display %v, want block", b, st.Display)
		}
	}
}

func TestPageIn_InheritsFromRoot(t *testing.T) {
	root := *Initial()
	// A root element font-size: 30px sets: an absolute length, not the
	// medium keyword the initial style carries, which fixMonospaceSize
	// would size the page context from again.
	root.FontSize, root.fontSizeKeyword, root.fontSizeAbsolute = 30, 0, true
	ps := resolverOf(t, `@page { @top-center { content: "x" } }`).PageIn(PageSelector{}, &root)
	if got := ps.MarginBoxes[MarginTopCenter].FontSize; got != 30 {
		t.Errorf("font size %v, want 30 from the root element", got)
	}
}

func TestPage_BlankSelector(t *testing.T) {
	r := resolverOf(t, `@page :blank { margin: 1pt }`)
	if m := r.Page(PageSelector{}).Margin[Top]; m.Kind != Auto {
		t.Errorf("non-blank margin %+v, want auto", m)
	}
	if m := r.Page(PageSelector{Blank: true}).Margin[Top]; m.Kind != Points || m.Value != 1 {
		t.Errorf("blank margin %+v, want 1pt", m)
	}
}

func TestMarginBox_String(t *testing.T) {
	if MarginBottomRightCorner.String() != "bottom-right-corner" || NumMarginBoxes.String() != "margin-box" {
		t.Error("margin box names")
	}
}

func TestPageIn_NoteArea(t *testing.T) {
	t.Parallel()
	r := resolverOf(t, `@page { @note-area { content: element(side, all-once); position: absolute; right: 0; display: flex } }`)
	if !r.HasNoteArea() || r.HasMarginBoxes() || r.HasFootnoteArea() {
		t.Fatalf("HasNoteArea %v, HasMarginBoxes %v, HasFootnoteArea %v", r.HasNoteArea(), r.HasMarginBoxes(), r.HasFootnoteArea())
	}
	st := r.PageIn(PageSelector{}, nil).NoteArea
	if st == nil {
		t.Fatal("no note area style")
	}
	if st.Position != Absolute || st.Display != Flex || st.Right != Pts(0) {
		t.Errorf("Position %v, Display %v, Right %+v", st.Position, st.Display, st.Right)
	}
	if want := []ContentItem{{Kind: ItemElement, Text: "side", Pick: PickAllOnce}}; !reflect.DeepEqual(st.Content.Items, want) {
		t.Errorf("content %+v, want %+v", st.Content.Items, want)
	}
}

func TestPageIn_VarInMarginBox(t *testing.T) {
	for _, tt := range []struct{ src, want string }{
		{`@page { @top-center { content: var(--x, "a") } }`, "a"},
		{`@page { --x: "b"; @top-center { content: var(--x) } }`, "b"},
		{`@page { @top-center { --y: "c"; content: var(--y) } }`, "c"},
	} {
		ps := resolverOf(t, tt.src).PageIn(PageSelector{Right: true}, nil)
		tc := ps.MarginBoxes[MarginTopCenter]
		if tc == nil || len(tc.Content.Items) != 1 || tc.Content.Items[0].Text != tt.want {
			t.Errorf("%s: top-center %+v, want content %q", tt.src, tc, tt.want)
		}
	}
}

// TestPageIn_ManyDeclarationsKeepOrder locks the order of the
// declarations of one @page rule's margin blocks past 65,536 in one
// block: the later block wins (CSS Cascade 4, 6.1, order of appearance).
func TestPageIn_ManyDeclarationsKeepOrder(t *testing.T) {
	var b strings.Builder
	b.WriteString("@page { @top-center { ")
	for range 1<<16 + 1 {
		b.WriteString("color:red;")
	}
	b.WriteString(`content: "x" } @top-center { content: "y" } }`)
	ps := resolverOf(t, b.String()).PageIn(PageSelector{Right: true}, nil)
	tc := ps.MarginBoxes[MarginTopCenter]
	if tc == nil {
		t.Fatal("no top-center box")
	}
	if len(tc.Content.Items) != 1 || tc.Content.Items[0].Text != "y" {
		t.Errorf("top-center content %+v, want the later block's", tc.Content)
	}
}
