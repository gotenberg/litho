package docx

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestNotes(t *testing.T) {
	t.Parallel()
	notes := func(kind string) string {
		return `<w:` + kind + `s ` + nsDecl + `><w:` + kind + ` w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:` + kind + `>` +
			`<w:` + kind + ` w:id="1"><w:p><w:r><w:` + kind + `Ref/></w:r><w:r><w:t xml:space="preserve"> ` + kind + ` body</w:t></w:r></w:p></w:` + kind + `></w:` + kind + `s>`
	}
	body := `<w:p><w:r><w:t>Text</w:t></w:r><w:r><w:footnoteReference w:id="1"/></w:r><w:r><w:t xml:space="preserve"> and </w:t></w:r><w:r><w:endnoteReference w:id="1"/></w:r></w:p>`
	extra := map[string]string{"word/footnotes.xml": notes("footnote"), "word/endnotes.xml": notes("endnote")}
	b := build(t, body, extra)
	calls := find(b.root, func(n *layout.Node) bool { return n.Footnote != nil })
	if len(calls) != 1 || textOf(calls[0]) != "1" || !strings.Contains(textOf(calls[0].Footnote), "1 footnote body") || calls[0].Footnote.Role != "Note" {
		t.Fatalf("footnote calls %d", len(calls))
	}
	if len(b.d.endnoteBodies) != 1 || !strings.Contains(textOf(b.d.endnoteBodies[0]), "i endnote body") {
		t.Errorf("endnote bodies %d", len(b.d.endnoteBodies))
	}
	doc := convertBytes(t, docx(t, body, extra), office.Options{})
	if got := docText(doc); !strings.Contains(got, "footnote body") || !strings.Contains(got, "endnote body") {
		t.Errorf("notes not drawn: %q", got)
	}
	// Each call links to its note's number and the number back, as
	// LibreOffice exports a note.
	var dests []int
	for _, p := range doc.Pages {
		for _, l := range p.Links {
			if l.Dest != nil {
				dests = append(dests, l.Dest.Page)
			}
		}
	}
	if len(dests) != 4 {
		t.Errorf("note links %v, want 4", dests)
	}
}

func TestFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ instr, want string }{
		{` HYPERLINK "https://example.com/a b" \o "tip"`, "https://example.com/a b"},
		{` HYPERLINK \l "_Toc1" `, "#_Toc1"},
		{`HYPERLINK "https://x.test/" \l "frag"`, "https://x.test/#frag"},
		{` PAGEREF _Toc1 \h `, ""},
	} {
		if got := hyperlinkTarget(tc.instr); got != tc.want {
			t.Errorf("hyperlinkTarget(%q) = %q, want %q", tc.instr, got, tc.want)
		}
	}
	fld := func(instr, result string) string {
		return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve">` + instr + `</w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>` + result + `</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
	}
	body := `<w:p>` + fld(` DATE \@ "d MMMM yyyy" `, "7 November 2026") + `<w:r><w:t xml:space="preserve"> / </w:t></w:r>` +
		fld(` HYPERLINK "https://example.com/" `, "site") + `<w:r><w:t xml:space="preserve"> / </w:t></w:r>` +
		fld(` PAGE `, "3") + `</w:p>` +
		`<w:p><w:hyperlink r:id="rIdlink"><w:r><w:t>rel link</w:t></w:r></w:hyperlink><w:hyperlink w:anchor="mark"><w:r><w:t>inner link</w:t></w:r></w:hyperlink></w:p>` +
		`<w:p><w:bookmarkStart w:id="0" w:name="mark"/><w:r><w:t>target</w:t></w:r><w:bookmarkEnd w:id="0"/></w:p>`
	b := build(t, body, nil)
	if got := textOf(role(b.root, "P")[0]); got != "7 November 2026 / site / 3" {
		t.Errorf("cached field results %q", got)
	}
	links := role(b.root, "Link")
	if len(links) != 3 || links[0].Href != "https://example.com/" || textOf(links[0]) != "site" ||
		links[1].Href != "https://example.com/" || links[2].Href != "#mark" {
		for _, l := range links {
			t.Logf("link %q %q", l.Href, textOf(l))
		}
		t.Errorf("%d links", len(links))
	}
	if role(b.root, "P")[2].ID != "mark" {
		t.Errorf("bookmark not anchored on its paragraph")
	}
	doc := convertBytes(t, docx(t, body, nil), office.Options{})
	if len(doc.Pages[0].Links) != 3 {
		t.Errorf("%d PDF links", len(doc.Pages[0].Links))
	}
}

func TestSymbolsAndBreaks(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:t>a</w:t><w:tab/><w:t>b</w:t><w:br/><w:t>c</w:t><w:cr/><w:sym w:font="Symbol" w:char="F0B7"/><w:noBreakHyphen/><w:softHyphen/></w:r>` +
		`<m:oMath><m:r><m:t>x=1</m:t></m:r></m:oMath></w:p>`
	b := build(t, body, nil)
	p := role(b.root, "P")[0]
	if got := textOf(p); got != "a\tbc•‑­x=1" {
		t.Errorf("text %q", got)
	}
	if n := len(find(p, func(n *layout.Node) bool { return n.Kind == layout.LineBreak })); n != 2 {
		t.Errorf("%d line breaks", n)
	}
	if len(b.d.warnings) == 0 {
		t.Error("no warning for the equation")
	}
}

// TestFields_Set hides the result of a SET field, complex or simple, as
// LibreOffice imports it, and keeps the results of the fields around it.
func TestFields_Set(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body, want string
	}{
		{"complex", `<w:r><w:t>a</w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> SET TEST_VAR 99 </w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>99</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:t>b</w:t></w:r>`, "ab"},
		{"simple", `<w:r><w:t>a</w:t></w:r><w:fldSimple w:instr=" set X 1 "><w:r><w:t>1</w:t></w:r></w:fldSimple><w:r><w:t>b</w:t></w:r>`, "ab"},
		{"other", `<w:r><w:t>a</w:t></w:r><w:fldSimple w:instr=" SEQ X "><w:r><w:t>1</w:t></w:r></w:fldSimple><w:r><w:t>b</w:t></w:r>`, "a1b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := build(t, `<w:p>`+tt.body+`</w:p>`, nil)
			if got := textOf(role(b.root, "P")[0]); got != tt.want {
				t.Errorf("text %q, want %q", got, tt.want)
			}
		})
	}
}
