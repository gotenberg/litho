package odt

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// region returns a text:changed-region of id, kind and author, holding
// content after its change-info.
func region(id, kind, author, content string) string {
	return `<text:changed-region text:id="` + id + `"><text:` + kind + `><office:change-info><dc:creator>` + author +
		`</dc:creator><dc:date>2026-01-03T09:00:00</dc:date></office:change-info>` + content + `</text:` + kind + `></text:changed-region>`
}

// trackedRowStyle is a table row style whose changes are the row's own.
const trackedRowStyle = `<style:style style:name="R1" style:family="table-row"><style:table-row-properties loext:text-changes-only="false"/></style:style>`

// cell returns a table cell holding one paragraph of content.
func cell(content string) string {
	return `<table:table-cell office:value-type="string"><text:p>` + content + `</text:p></table:table-cell>`
}

func TestConvert_TrackedChangesViews(t *testing.T) {
	cases := []struct {
		name                       string
		regions, body, contentAuto string
		accepted, rejected, marked string
	}{
		{
			name:     "insertion",
			regions:  region("i", "insertion", "Ada", ""),
			body:     `<text:p>the <text:change-start text:change-id="i"/>quick <text:change-end text:change-id="i"/>fox</text:p>`,
			accepted: "the quick fox", rejected: "the fox", marked: "the quick fox",
		},
		{
			name:     "deletion",
			regions:  region("d", "deletion", "Ada", `<text:p>slow </text:p>`),
			body:     `<text:p>the <text:change text:change-id="d"/>fox</text:p>`,
			accepted: "the fox", rejected: "the slow fox", marked: "the slow fox",
		},
		{
			name:     "deletion inside a span",
			regions:  region("d", "deletion", "Ada", `<text:p>slow </text:p>`),
			body:     `<text:p><text:span>the <text:change text:change-id="d"/>fox</text:span></text:p>`,
			accepted: "the fox", rejected: "the slow fox", marked: "the slow fox",
		},
		{
			name:     "range deletion",
			regions:  region("d", "deletion", "Ada", ""),
			body:     `<text:p>the <text:change-start text:change-id="d"/>very <text:change-end text:change-id="d"/>end</text:p>`,
			accepted: "the end", rejected: "the very end", marked: "the very end",
		},
		{
			name:     "format change keeps the new formatting",
			regions:  region("f", "format-change", "Ada", ""),
			body:     `<text:p><text:change-start text:change-id="f"/>now<text:change-end text:change-id="f"/> bold</text:p>`,
			accepted: "now bold", rejected: "now bold", marked: "now bold",
		},
		{
			name:     "inserted paragraph break",
			regions:  region("i", "insertion", "Ada", ""),
			body:     `<text:p>first<text:change-start text:change-id="i"/></text:p><text:p><text:change-end text:change-id="i"/>second</text:p>`,
			accepted: "first\nsecond", rejected: "firstsecond", marked: "first\nsecond",
		},
		{
			name:     "deleted paragraph break",
			regions:  region("d", "deletion", "Ada", `<text:p/><text:p/>`),
			body:     `<text:p>first<text:change text:change-id="d"/>second</text:p>`,
			accepted: "firstsecond", rejected: "first\nsecond", marked: "first\nsecond",
		},
		{
			name:     "deleted whole paragraph",
			regions:  region("d", "deletion", "Ada", `<text:p>gone</text:p><text:p/>`),
			body:     `<text:p><text:change text:change-id="d"/>next</text:p>`,
			accepted: "next", rejected: "gone\nnext", marked: "gone\nnext",
		},
		{
			name:     "deleted paragraphs",
			regions:  region("d", "deletion", "Ada", `<text:p>a</text:p><text:p>middle</text:p><text:p>b</text:p>`),
			body:     `<text:p>x<text:change text:change-id="d"/>y</text:p>`,
			accepted: "xy", rejected: "xa\nmiddle\nby", marked: "xa\nmiddle\nby",
		},
		{
			name:     "inserted whole paragraph",
			regions:  region("i", "insertion", "Ada", ""),
			body:     `<text:p><text:change-start text:change-id="i"/>added</text:p><text:p><text:change-end text:change-id="i"/>next</text:p>`,
			accepted: "added\nnext", rejected: "next", marked: "added\nnext",
		},
		{
			name:    "deleted table",
			regions: region("d", "deletion", "Ada", ""),
			body: `<text:p>before<text:change-start text:change-id="d"/></text:p><table:table><table:table-column/><table:table-row>` +
				cell("cell") + `</table:table-row></table:table><text:p><text:change-end text:change-id="d"/>after</text:p>`,
			accepted: "beforeafter", rejected: "before\ncell\nafter", marked: "before\ncell\nafter",
		},
		{
			name: "tracked rows",
			regions: region("d1", "deletion", "Ada", "") + region("d2", "deletion", "Ada", "") +
				region("i1", "insertion", "Ada", "") + region("i2", "insertion", "Ada", ""),
			contentAuto: trackedRowStyle,
			body: `<table:table><table:table-column table:number-columns-repeated="2"/>` +
				`<table:table-row>` + cell("kept") + cell("kept") + `</table:table-row>` +
				`<table:table-row table:style-name="R1">` + cell(`<text:change-start text:change-id="d1"/>del<text:change-end text:change-id="d1"/>`) +
				cell(`<text:change-start text:change-id="d2"/>del<text:change-end text:change-id="d2"/>`) + `</table:table-row>` +
				`<table:table-row table:style-name="R1">` + cell(`<text:change-start text:change-id="i1"/>ins<text:change-end text:change-id="i1"/>`) +
				cell(`<text:change-start text:change-id="i2"/>ins<text:change-end text:change-id="i2"/>`) + `</table:table-row></table:table>`,
			accepted: "keptkept\ninsins", rejected: "keptkept\ndeldel", marked: "keptkept\ndeldel\ninsins",
		},
		{
			name:        "a row not tracked stays",
			regions:     region("d1", "deletion", "Ada", ""),
			contentAuto: trackedRowStyle,
			body: `<table:table><table:table-column table:number-columns-repeated="2"/>` +
				`<table:table-row table:style-name="R1">` + cell(`<text:change-start text:change-id="d1"/>del<text:change-end text:change-id="d1"/>`) +
				cell("kept") + `</table:table-row></table:table>`,
			accepted: "kept", rejected: "delkept", marked: "delkept",
		},
		{
			name:     "unknown change ids are ignored",
			regions:  region("d", "deletion", "Ada", `<text:p>x</text:p>`),
			body:     `<text:p>a<text:change text:change-id="nope"/><text:change-start text:change-id="nope"/>b</text:p>`,
			accepted: "ab", rejected: "ab", marked: "ab",
		},
	}
	for _, tc := range cases {
		for _, v := range []struct {
			view office.TrackedChanges
			want string
		}{{office.ChangesAccepted, tc.accepted}, {office.ChangesRejected, tc.rejected}, {office.ChangesMarked, tc.marked}} {
			t.Run(tc.name+"/"+[]string{"accepted", "rejected", "marked"}[v.view], func(t *testing.T) {
				d := doc{contentAuto: tc.contentAuto, body: `<text:tracked-changes>` + tc.regions + `</text:tracked-changes>` + tc.body}
				res := convertDoc(t, d.pack(t), office.Options{TrackedChanges: v.view})
				if got := docText(res); got != v.want {
					t.Errorf("text = %q, want %q", got, v.want)
				}
			})
		}
	}
}

// markedRuns returns the text runs of the marked view of body, with the
// colour and decoration of each.
func markedRuns(t *testing.T, regions, body string) map[string]*css.Style {
	t.Helper()
	b := newTestBuilder(t, doc{body: `<text:tracked-changes>` + regions + `</text:tracked-changes>` + body})
	el := odfBody(b)
	marks, err := resolveChanges(context.Background(), el, office.ChangesMarked, b.rowTracked, b.warn)
	if err != nil {
		t.Fatal(err)
	}
	b.changeMarks = marks
	root, _ := b.document(el)
	out := map[string]*css.Style{}
	walkNodes(root, func(n *layout.Node) {
		if n.Kind == layout.Text {
			out[strings.TrimSpace(n.Text)] = n.Style
		}
	})
	return out
}

func TestConvert_TrackedChangesMarks(t *testing.T) {
	regions := region("a", "insertion", "Ada", "") + region("g", "insertion", "Grace", "") +
		region("d", "deletion", "Grace", `<text:p>gone</text:p>`) + region("f", "format-change", "Ada", "")
	body := `<text:p>plain <text:change-start text:change-id="a"/>ada<text:change-end text:change-id="a"/> ` +
		`<text:change-start text:change-id="g"/>grace<text:change-end text:change-id="g"/> <text:change text:change-id="d"/> ` +
		`<text:change-start text:change-id="f"/>formatted<text:change-end text:change-id="f"/></text:p>`
	got := markedRuns(t, regions, body)
	cases := []struct {
		text  string
		deco  css.TextDecoration
		color css.Color
	}{
		{"ada", css.Underline, office.RGB(office.AuthorColor(0))},
		{"grace", css.Underline, office.RGB(office.AuthorColor(1))},
		{"gone", css.LineThrough, office.RGB(office.AuthorColor(1))},
	}
	for _, tc := range cases {
		st := got[tc.text]
		if st == nil {
			t.Fatalf("no run %q in %v", tc.text, got)
		}
		if st.TextDecoration != tc.deco || st.Color != tc.color || st.TextDecorationColor != tc.color {
			t.Errorf("%q: decoration %v in %v, text %v; want %v in %v", tc.text, st.TextDecoration, st.TextDecorationColor, st.Color, tc.deco, tc.color)
		}
	}
	for _, s := range []string{"plain", "formatted"} {
		if st := got[s]; st == nil || st.TextDecoration != 0 {
			t.Errorf("%q is marked: %+v", s, st)
		}
	}
}

func TestConvert_TrackedChangesAuthorsCycle(t *testing.T) {
	var regions, body strings.Builder
	for i := range 9 + 1 {
		id := string(rune('a' + i))
		regions.WriteString(region(id, "insertion", "Author "+id, ""))
		body.WriteString(`<text:p><text:change-start text:change-id="` + id + `"/>run` + id + `<text:change-end text:change-id="` + id + `"/></text:p>`)
	}
	got := markedRuns(t, regions.String(), body.String())
	if c := got["runa"].Color; c != office.RGB(office.AuthorColor(0)) {
		t.Errorf("first author %v, want %v", c, office.RGB(office.AuthorColor(0)))
	}
	if c := got["run"+string(rune('a'+9))].Color; c != office.RGB(office.AuthorColor(0)) {
		t.Errorf("tenth author %v, want the first colour %v", c, office.RGB(office.AuthorColor(0)))
	}
}

func TestResolveChanges_KeepsTheSpaceASplitPutsFirst(t *testing.T) {
	for _, view := range []office.TrackedChanges{office.ChangesRejected, office.ChangesMarked} {
		b := newTestBuilder(t, doc{body: `<text:tracked-changes>` + region("d", "deletion", "Ada", `<text:p/><text:p/>`) +
			`</text:tracked-changes><text:p>first<text:change text:change-id="d"/> second</text:p>`})
		el := odfBody(b)
		if _, err := resolveChanges(context.Background(), el, view, b.rowTracked, b.warn); err != nil {
			t.Fatal(err)
		}
		ps := el.All(odf.NSText, "p")
		if len(ps) != 2 {
			t.Fatalf("view %d: %d paragraphs, want 2", view, len(ps))
		}
		var kinds []string
		for _, k := range ps[1].Children {
			if k.IsText() {
				kinds = append(kinds, "text:"+k.Text)
			} else {
				kinds = append(kinds, k.Name.Local)
			}
		}
		if got := strings.Join(kinds, ","); !strings.Contains(got, "s,text:second") {
			t.Errorf("view %d: second paragraph %s, want a text:s before %q", view, got, "second")
		}
	}
}
