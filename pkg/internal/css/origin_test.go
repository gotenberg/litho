package css

import (
	"context"
	"fmt"
	"testing"
)

// originDecl is one declaration of the origin test: its origin, its
// importance and the color it gives.
type originDecl struct {
	origin    Origin
	important bool
	color     string
}

func (d originDecl) String() string {
	names := [...]string{UserAgent: "user agent", User: "user", Author: "author"}
	if d.important {
		return names[d.origin] + " important"
	}
	return names[d.origin] + " normal"
}

// TestCascade_OriginPairs locks the order of the origins (CSS Cascade 4,
// 6.2): for every pair of origin and importance, the declaration of the
// higher one wins, whichever sheet comes first.
func TestCascade_OriginPairs(t *testing.T) {
	t.Parallel()
	// precedence lists the six cases from lowest to highest.
	precedence := []originDecl{
		{UserAgent, false, "red"},
		{User, false, "orange"},
		{Author, false, "yellow"},
		{Author, true, "green"},
		{User, true, "blue"},
		{UserAgent, true, "purple"},
	}
	want := map[string]Color{
		"red": rgb(255, 0, 0), "orange": rgb(255, 165, 0), "yellow": rgb(255, 255, 0),
		"green": rgb(0, 128, 0), "blue": rgb(0, 0, 255), "purple": rgb(128, 0, 128),
	}
	for lo := range precedence {
		for hi := lo + 1; hi < len(precedence); hi++ {
			a, b := precedence[lo], precedence[hi]
			t.Run(fmt.Sprintf("%s under %s", a, b), func(t *testing.T) {
				t.Parallel()
				// The losing declaration has the higher specificity, so
				// only the origin and the importance decide.
				for _, order := range [][2]originDecl{{a, b}, {b, a}} {
					sheets := make([]Sheet, 0, 2)
					for i, d := range order {
						sel := "p"
						if d == a {
							sel = "#p"
						}
						imp := ""
						if d.important {
							imp = " !important"
						}
						src := fmt.Sprintf("%s { color: %s%s }", sel, d.color, imp)
						sheets = append(sheets, Sheet{Sheet: mustParse(t, src), Origin: order[i].origin})
					}
					root := parseHTML(t, `<p id="p">x</p>`)
					c, err := NewResolver(a4, sheets).Compute(context.Background(), root, ComputeOptions{})
					if err != nil {
						t.Fatalf("Compute: %v", err)
					}
					if got := c.Style(byID(root, "p")).Color; !colorEq(got, want[b.color]) {
						t.Errorf("sheets %s then %s: color = %+v, want %s", order[0], order[1], got, b.color)
					}
				}
			})
		}
	}
}

// TestCascade_UserOrigin locks how the user origin meets the author's
// other declarations: presentational hints and style attributes are the
// author's, and specificity and order decide within the user origin.
func TestCascade_UserOrigin(t *testing.T) {
	t.Parallel()
	doc := `<body><font id="hint" color="red">a</font>
<p id="attr" style="color: red !important">b</p>
<p id="spec" class="c">c</p>
<p id="order">d</p></body>`
	user := `
font { color: blue }
p { color: blue !important }
#spec { margin-top: 7px }
.c { margin-top: 9px }
#order { margin-left: 1px }
#order { margin-left: 2px }
`
	root := parseHTML(t, doc)
	r := NewResolver(a4, []Sheet{
		{Sheet: HTMLUserAgent(), Origin: UserAgent},
		{Sheet: mustParse(t, user), Origin: User},
		{Sheet: mustParse(t, "#hint { margin-top: 3px }"), Origin: Author},
	})
	c, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	of := func(id string) *Style { return c.Style(byID(root, id)) }
	if got := of("hint").Color; !colorEq(got, rgb(255, 0, 0)) {
		t.Errorf("hint color = %+v, want the presentational hint's red over a normal user rule", got)
	}
	if got := of("attr").Color; !colorEq(got, rgb(0, 0, 255)) {
		t.Errorf("attr color = %+v, want the important user rule's blue over an important style attribute", got)
	}
	if got := of("spec").Margin[Top]; !dimEq(got, pts(5.25)) {
		t.Errorf("spec margin-top = %+v, want the id selector's 7px", got)
	}
	if got := of("order").Margin[Left]; !dimEq(got, pts(1.5)) {
		t.Errorf("order margin-left = %+v, want the later rule's 2px", got)
	}
}

// TestCascade_UserSheetsInOrder locks that several user stylesheets
// cascade in the order given.
func TestCascade_UserSheetsInOrder(t *testing.T) {
	t.Parallel()
	root := parseHTML(t, `<p id="p">x</p>`)
	r := NewResolver(a4, []Sheet{
		{Sheet: mustParse(t, "p { color: red }"), Origin: User},
		{Sheet: mustParse(t, "p { color: blue }"), Origin: User},
	})
	c, err := r.Compute(context.Background(), root, ComputeOptions{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got := c.Style(byID(root, "p")).Color; !colorEq(got, rgb(0, 0, 255)) {
		t.Errorf("color = %+v, want the second sheet's blue", got)
	}
}

// TestCascade_AuthorRevert locks revert in the author origin: it rolls
// back to the user origin's value of the longhand, and behaves as unset
// where that origin sets none (CSS Cascade 4, 7.3.4).
func TestCascade_AuthorRevert(t *testing.T) {
	t.Parallel()
	root := parseHTML(t, `<div style="color: green; margin-top: 4px"><p id="p">x</p></div>`)
	r := NewResolver(a4, []Sheet{
		{Sheet: mustParse(t, "p { color: blue; margin-left: 3px }"), Origin: User},
		{Sheet: mustParse(t, "p { color: red; color: revert; margin: 9px; margin: revert; border-top-width: revert }"), Origin: Author},
	})
	c, err := r.Compute(context.Background(), root, ComputeOptions{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	p := c.Style(byID(root, "p"))
	if !colorEq(p.Color, rgb(0, 0, 255)) {
		t.Errorf("color = %+v, want the user's blue", p.Color)
	}
	if !dimEq(p.Margin[Left], pts(2.25)) || !dimEq(p.Margin[Top], pts(0)) {
		t.Errorf("margins = %+v, want the user's 3px on the left and unset elsewhere", p.Margin)
	}

	// Without a user declaration, revert stays unset: color inherits.
	root = parseHTML(t, `<div style="color: green"><p id="p">x</p></div>`)
	r = NewResolver(a4, []Sheet{{Sheet: mustParse(t, "p { color: red; color: revert }"), Origin: Author}})
	if c, err = r.Compute(context.Background(), root, ComputeOptions{}); err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got := c.Style(byID(root, "p")).Color; !colorEq(got, rgb(0, 128, 0)) {
		t.Errorf("color = %+v, want the inherited green", got)
	}
}

// TestPage_UserOrigin locks the user origin in the cascade of @page rules
// and their margin boxes: a normal user declaration loses to the
// author's, an important one wins over the author's important one.
func TestPage_UserOrigin(t *testing.T) {
	t.Parallel()
	user := `@page { size: A5; margin-top: 1in; margin-left: 2in !important; @top-center { content: "user" } }`
	author := `@page { margin-top: 0.5in; margin-left: 0.25in !important }`
	r := NewResolver(a4, []Sheet{{Sheet: mustParse(t, user), Origin: User}, {Sheet: mustParse(t, author), Origin: Author}})
	pg := r.Page(PageSelector{First: true, Right: true})
	if !near(pg.Width, 148*72/25.4) {
		t.Errorf("page width %v, want the user's A5", pg.Width)
	}
	if !dimEq(pg.Margin[Top], pts(36)) || !dimEq(pg.Margin[Left], pts(144)) {
		t.Errorf("page margins %+v, want the author's top and the important user left", pg.Margin)
	}
	if !r.HasMarginBoxes() {
		t.Error("the user's margin box is not seen")
	}
}
