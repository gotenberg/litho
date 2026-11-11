package html

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

func TestPrepare_UserStylesheetsCascadeBetweenEngineAndDocument(t *testing.T) {
	t.Parallel()
	src := `<style>#a { color: blue } #b { color: blue }</style>` +
		`<p id=a>a<p id=b>b<p id=c>c<p id=d style="color: blue !important">d`
	user := []string{
		`p { color: red; margin-top: 7px } #b { color: green !important }`,
		`#c { color: green } #d { color: green !important }`,
	}
	p := prep(t, src, Options{UserStylesheets: user})
	for id, want := range map[string]css.Color{"a": blue, "b": green, "c": green, "d": green} {
		if got := byID(t, p.root, id).Style.Color; got != want {
			t.Errorf("%s: colour %+v, want %+v", id, got, want)
		}
	}
	// 7px over the engine's 1em margin of a paragraph.
	if got := byID(t, p.root, "a").Style.Margin[css.Top]; got != css.Pts(5.25) {
		t.Errorf("margin-top %+v, want the user's 7px over the engine's", got)
	}
	if len(p.warnings) != 0 {
		t.Errorf("warnings %+v", p.warnings)
	}
}

func TestPrepare_UserStylesheetResolvesAgainstTheDocument(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"css/u.css": {Data: []byte("#i { color: green }")},
	}
	src := `<base href="css/"><p id=i>i<p id=m>m`
	user := []string{`@import "u.css"; @media print { #m { color: green } } @media screen { #m { color: red } } #m { background-image: url(missing.png) }`}
	p := prep(t, src, Options{Resources: fsys, UserStylesheets: user, Media: MediaPrint})
	for _, id := range []string{"i", "m"} {
		if got := byID(t, p.root, id).Style.Color; got != green {
			t.Errorf("%s: colour %+v, want green", id, got)
		}
	}
	// The image is fetched as the document's are, and refused the same
	// way: the layout fetches it.
	if got := byID(t, p.root, "m").Style.BackgroundImage; got != rootPrefix+"css/missing.png" {
		t.Errorf("background image %q, want it resolved against the document's base", got)
	}
}

func TestPrepare_UserStylesheetHeldToTheResourceCaps(t *testing.T) {
	t.Parallel()
	big := strings.Repeat(" ", maxResourceBytes+1) + "#x { color: red }"
	p := prep(t, `<p id=x>x`, Options{UserStylesheets: []string{big, "#x { margin-top: 7px }"}})
	if got := byID(t, p.root, "x").Style.Color; got == red {
		t.Error("a user stylesheet past the cap applied")
	}
	if got := byID(t, p.root, "x").Style.Margin[css.Top]; got != css.Pts(5.25) {
		t.Errorf("the next user stylesheet did not apply: margin-top %+v", got)
	}
	if !hasWarning(p.warnings, CodeResourceLimit, "user stylesheet 1 was left out") {
		t.Errorf("warnings %+v, want the sheet left out", p.warnings)
	}

	r := newResources(nil)
	r.read = maxTotalResourceBytes - 4
	if err := r.admit(context.Background(), 5); warningCode(err) != CodeResourceLimit {
		t.Errorf("admit past the total cap: %v", err)
	}

	ctx, _, done := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 1 << 20})
	defer done()
	if err := newResources(nil).admit(ctx, 2<<20); err == nil || ctx.Err() == nil {
		t.Errorf("admit past the budget: %v, context %v", err, ctx.Err())
	}
}

func TestConvert_UserPageRules(t *testing.T) {
	t.Parallel()
	const body = `<style>body { margin: 0 } p { margin: 0 }</style><p>one</p>`
	const page = `@page { size: 400pt 300pt; margin: 40pt; font: 10pt "Test Serif"; @top-center { content: "TC" } }`
	for _, prefer := range []bool{true, false} {
		o := Options{PreferCSSPageSize: prefer, PageWidth: 200, PageHeight: 500}
		uo := o
		uo.UserStylesheets = []string{page}
		user := convertDoc(t, body, uo).Pages[0]
		author := convertDoc(t, "<style>"+page+"</style>"+body, o).Pages[0]
		if user.Width != author.Width || user.Height != author.Height {
			t.Errorf("prefer %v: page %vx%v, want the author's %vx%v", prefer, user.Width, user.Height, author.Width, author.Height)
		}
		if u, a := find(t, user, "TC"), find(t, author, "TC"); u != a {
			t.Errorf("prefer %v: margin box at %+v, want the author's %+v", prefer, u, a)
		}
	}
}

func TestConvert_UserStylesheetLeavesTemplatesAlone(t *testing.T) {
	t.Parallel()
	o := Options{MarginTop: 72, HeaderTemplate: `<p>head</p>`, UserStylesheets: []string{`p { font-size: 30px }`}}
	pg := convertDoc(t, `<p>body</p>`, o).Pages[0]
	if got := find(t, pg, "body").size; got != 22.5 {
		t.Errorf("document text size %v, want the user's 30px", got)
	}
	if got := find(t, pg, "head").size; got == 22.5 {
		t.Error("the user stylesheet styled the header template")
	}
}
