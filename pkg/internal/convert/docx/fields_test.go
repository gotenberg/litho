package docx

import (
	"testing"
)

func TestFields_ReferenceLinks(t *testing.T) {
	t.Parallel()
	field := func(instr, result string) string {
		return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> ` + instr + ` </w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>` + result + `</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
	}
	body := `<w:p><w:bookmarkStart w:id="1" w:name="Mark"/><w:r><w:t>target</w:t></w:r><w:bookmarkEnd w:id="1"/></w:p>` +
		`<w:p>` + field(`REF Mark \h`, "one") + field(`PAGEREF Mark \h`, "two") + field(`NOTEREF Mark \f \h`, "three") +
		field(`REF Mark`, "plain") + field(`Mark`, "bare") + `<w:fldSimple w:instr=" REF Mark \r \h "><w:r><w:t>simple</w:t></w:r></w:fldSimple></w:p>` +
		`<w:p><w:hyperlink w:anchor="Mark"><w:r><w:t>entry </w:t></w:r>` + field(`PAGEREF Mark \h`, "4") + `</w:hyperlink></w:p>`
	links := role(build(t, body, nil).root, "Link")
	var got []string
	for _, l := range links {
		if l.Href != "#Mark" {
			t.Errorf("link %q to %q", textOf(l), l.Href)
		}
		got = append(got, textOf(l))
	}
	want := []string{"one", "two", "three", "simple", "entry 4"}
	if len(got) != len(want) {
		t.Fatalf("links %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d is %q, want %q", i, got[i], want[i])
		}
	}
}
