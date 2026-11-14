package html

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestDetails_ClosedDrawsItsSummaryAlone(t *testing.T) {
	t.Parallel()
	// HTML Standard, 15.5.5: the first slot takes the first summary child,
	// wherever it stands; the second slot, which holds the rest, is
	// content-visibility: hidden while the open attribute is absent.
	for _, tc := range []struct {
		name, src, want string
	}{
		{"closed", `<details><p>B1</p><summary>S1</summary><p>C1</p></details>`, "S1"},
		{"open", `<details open><p>B1</p><summary>S1</summary><p>C1</p></details>`, "S1B1C1"},
		{"second summary", `<details open><summary>S1</summary><summary>S2</summary></details>`, "S1S2"},
		{"closed without summary", `<details><p>C1</p></details>`, "Details"},
		{"open without summary", `<details open><div><summary>S1</summary></div></details>`, "DetailsS1"},
		{"content shown by the author", `<style>details::details-content { content-visibility: visible }</style><details><summary>S1</summary>C1</details>`, "S1C1"},
		{"content hidden by the author", `<style>details::details-content { display: none }</style><details open><summary>S1</summary>C1</details>`, "S1"},
	} {
		got := strings.Join(strings.Fields(nodeText(prep(t, `<!DOCTYPE html>`+tc.src, Options{}).root)), "")
		if got != tc.want {
			t.Errorf("%s: text %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDetails_DefaultSummaryTakesTheUserAgentStyles(t *testing.T) {
	t.Parallel()
	// HTML Standard, 15.5.5: the default summary is an element of the
	// details element's shadow tree, a list item with a disclosure marker
	// that inherits from the details element; no author rule reaches it.
	src := `<!DOCTYPE html><style>summary { color: red } ::marker { color: lime } details { color: blue }</style>` +
		`<details id=c><p>C1</p></details><details id=o open><p>O1</p></details>`
	p := prep(t, src, Options{})
	for _, tc := range []struct {
		id   string
		want css.ListStyleType
	}{{"c", css.DisclosureClosed}, {"o", css.DisclosureOpen}} {
		d := byID(t, p.root, tc.id)
		if len(d.Children) == 0 || d.Children[0].Style == nil {
			t.Fatalf("%s: no default summary", tc.id)
		}
		s := d.Children[0]
		if s.Style.Display != css.ListItem || s.Style.ListStyleType != tc.want || s.Style.ListStylePosition != css.Inside {
			t.Errorf("%s: summary display %v, list style %v %v; want a list item, %v inside", tc.id, s.Style.Display, s.Style.ListStyleType, s.Style.ListStylePosition, tc.want)
		}
		if s.Marker == nil || s.Marker.Style == nil || s.Marker.Style.Color != d.Style.Color || s.Style.Color != d.Style.Color {
			t.Errorf("%s: summary or marker takes an author colour", tc.id)
		}
	}
}

func TestDetails_NameGroupKeepsTheFirstOpen(t *testing.T) {
	t.Parallel()
	// HTML Standard, 4.11.1: the parser runs the insertion steps of each
	// details element, which close one whose name group already holds an
	// open element.
	src := `<!DOCTYPE html><details name=g open><summary>S1</summary>A1</details>` +
		`<details name=g open><summary>S2</summary>A2</details>` +
		`<details name=h open><summary>S3</summary>A3</details>` +
		`<details name="" open><summary>S4</summary>A4</details><details name="" open><summary>S5</summary>A5</details>`
	got := strings.Join(strings.Fields(nodeText(prep(t, src, Options{}).root)), "")
	if want := "S1A1S2S3A3S4A4S5A5"; got != want {
		t.Errorf("text %q, want %q", got, want)
	}
}

func TestDetails_ClosedContentCountsNothing(t *testing.T) {
	t.Parallel()
	// CSS Contain 2, 4.1: content-visibility: hidden skips the contents,
	// which then number nothing.
	src := `<!DOCTYPE html><style>body { counter-reset: k } p::before { counter-increment: k; content: counter(k) }</style>` +
		`<p></p><details><p></p><p></p></details><p></p>`
	var got []string
	eachNode(prep(t, src, Options{}).root, func(n *layout.Node) {
		if n.Kind == layout.Text && strings.TrimSpace(n.Text) != "" {
			got = append(got, n.Text)
		}
	})
	if s := strings.Join(got, ","); s != "1,Details,2" {
		t.Errorf("text %q, want 1,Details,2", s)
	}
}
