package html

import (
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// textColor returns the colour of the first glyph run of pg holding s.
func textColor(t *testing.T, pg *typeset.Page, s string) typeset.Color {
	t.Helper()
	var found *typeset.Text
	var walk func(fs []typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Group:
				walk(v.Content)
			case *typeset.Text:
				var b strings.Builder
				for _, g := range v.Glyphs {
					b.WriteString(g.Text)
				}
				if found == nil && strings.Contains(b.String(), s) {
					found = v
				}
			}
		}
	}
	walk(pg.Content)
	if found == nil {
		t.Fatalf("no run holds %q", s)
	}
	return found.Color
}

var (
	textRed  = typeset.Color{R: 1, A: 1}
	textBlue = typeset.Color{B: 1, A: 1}
)

func TestShadow_FlatTree(t *testing.T) {
	t.Parallel()
	// HTML Standard, 13.2.6.4.4; DOM Standard, 4.2.2: a declarative shadow
	// root replaces its host's children by its shadow tree, each slot
	// showing the children its name selects, or its own children when
	// none; a template where none attaches stays a template.
	tests := []struct {
		name, src, want string
	}{
		{"slots by name", `<div><template shadowrootmode="open">[<slot name="t"></slot>|<slot></slot>]</template>B<span slot="t">A</span>C</div>`, "[A|BC]"},
		{"closed alike", `<div><template shadowrootmode="closed">[<slot></slot>]</template>A</div>`, "[A]"},
		{"fallback of an empty slot", `<div><template shadowrootmode="open">[<slot name="x">F</slot>]</template>A</div>`, "[F]"},
		{"first slot of a name", `<div><template shadowrootmode="open">[<slot></slot>|<slot>F</slot>]</template>A</div>`, "[A|F]"},
		{"unassigned child hidden", `<div><template shadowrootmode="open">[<slot name="y"></slot>]</template><b slot="z">A</b></div>`, "[]"},
		{"first template wins", `<div><template shadowrootmode="open">[1]</template><template shadowrootmode="open">[2]</template></div>`, "[1]"},
		{"no valid host", `<table><template shadowrootmode="open">[1]</template></table>`, ""},
		{"no shadowrootmode", `<div><template>[1]</template>A</div>`, "A"},
		{"other mode value", `<div><template shadowrootmode="none">[1]</template>A</div>`, "A"},
		{"custom element", `<x-card><template shadowrootmode="OPEN">[<slot></slot>]</template>A</x-card>`, "[A]"},
		{"invalid custom name", `<xcard><template shadowrootmode="open">[1]</template>A</xcard>`, "A"},
		{"nested roots", `<div><template shadowrootmode="open">[<span><template shadowrootmode="open">(<slot></slot>)</template><slot></slot></span>]</template>A</div>`, "[(A)]"},
		{"plain slot element", `<slot>A</slot>`, "A"},
		{"slot in its own fallback", `<div><template shadowrootmode="open">[<slot name="a"><slot name="a">F</slot></slot>]</template>A</div>`, "[F]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := convertDoc(t, `<!DOCTYPE html><body style="margin: 0">`+tt.src, Options{})
			got := strings.ReplaceAll(pageText(doc.Pages[0]), " ", "")
			if got != tt.want {
				t.Errorf("text %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShadow_InheritsThroughTheFlatTree(t *testing.T) {
	t.Parallel()
	// CSS Scoping 1, 2.3: a slotted element inherits from its slot, a
	// top-level node of a shadow tree from its host.
	src := `<!DOCTYPE html><div style="color: blue"><template shadowrootmode="open">TOP<p style="color: red"><slot></slot></p></template><span>SLOTTED</span></div>`
	doc := convertDoc(t, src, Options{})
	if c := textColor(t, doc.Pages[0], "SLOTTED"); c != textRed {
		t.Errorf("slotted text %v, want red from its slot's parent", c)
	}
	if c := textColor(t, doc.Pages[0], "TOP"); c != textBlue {
		t.Errorf("shadow text %v, want blue from its host", c)
	}
}

func TestShadow_Bounded(t *testing.T) {
	t.Parallel()
	// Ten thousand sibling roots attach; ten thousand nested ones stop at
	// the parser's nesting cap.
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>")
	for range 10000 {
		b.WriteString(`<span><template shadowrootmode="open">x<slot></slot></template>y</span>`)
	}
	doc := convertDoc(t, b.String(), Options{})
	if got := strings.Count(pageText(doc.Pages[0]), "x"); got == 0 {
		t.Errorf("no shadow tree drawn")
	}
	b.Reset()
	b.WriteString("<!DOCTYPE html>")
	for range 10000 {
		b.WriteString(`<div><template shadowrootmode="open"><slot></slot>`)
	}
	_, _, _, err := parseDocument([]byte(b.String()))
	if err != nil && !errors.Is(err, ErrTooDeep) {
		t.Errorf("nested roots: %v", err)
	}
}

func TestShadow_CountsTowardTheElementCap(t *testing.T) {
	t.Parallel()
	// The elements of a shadow tree are the document's, detached from its
	// tree: the cap and the budget count them.
	src := `<!DOCTYPE html><div><template shadowrootmode="open"><b></b><b></b><b></b><b></b></template></div>`
	doc, _, shadows, err := parseDocument([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scanDocument(t.Context(), doc, shadows, documentURL, 7); !errors.Is(err, ErrTooManyElements) {
		t.Errorf("scan under a cap of 7: %v, want ErrTooManyElements", err)
	}
	if _, err := scanDocument(t.Context(), doc, shadows, documentURL, 8); err != nil {
		t.Errorf("scan under a cap of 8: %v", err)
	}
	plain, _ := treeCost(doc)
	charged, err := chargeTree(t.Context(), doc, shadows, len(src))
	if err != nil {
		t.Fatal(err)
	}
	if charged < plain+4*elementCost {
		t.Errorf("charged %d, want the shadow tree's four elements over %d", charged, plain)
	}
}

func TestShadow_TopLevelIsNoRoot(t *testing.T) {
	t.Parallel()
	src := `<!DOCTYPE html><style>:root { color: red }</style><div style="color: blue"><template shadowrootmode="open"><span>IN</span></template></div>`
	doc := convertDoc(t, src, Options{})
	if c := textColor(t, doc.Pages[0], "IN"); c != textBlue {
		t.Errorf("shadow text %v, want blue: its top-level span is no :root", c)
	}
}
