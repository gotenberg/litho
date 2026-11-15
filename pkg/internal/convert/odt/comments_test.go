package odt

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// ann returns an office:annotation of name (none when ""), by who, with
// the paragraphs of text, and the extra attributes.
func ann(name, who, text, extra string) string {
	n := ""
	if name != "" {
		n = ` office:name="` + name + `"`
	}
	var ps strings.Builder
	for _, p := range strings.Split(text, "\n") {
		ps.WriteString("<text:p>" + p + "</text:p>")
	}
	return `<office:annotation` + n + extra + `><dc:creator>` + who + `</dc:creator><dc:date>2026-03-01T10:30:00</dc:date>` + ps.String() + `</office:annotation>`
}

func annEnd(name string) string { return `<office:annotation-end office:name="` + name + `"/>` }

// commentDoc holds a comment of every kind: a range over two paragraphs,
// a point, a range across a page break, a thread with a reply written
// before it and a resolved comment, overlapping ranges, a range in a
// footnote and one in the header. The range across the break is in
// DejaVu Sans, whose glyphs on the line opening the second page reach
// above the page's cut: the line is on the second page alone.
var commentDoc = doc{
	styles:      `<style:style style:name="Body" style:family="paragraph"><style:text-properties style:font-name="DejaVu Sans" fo:font-family="'DejaVu Sans'" fo:font-size="11pt"/></style:style>`,
	contentAuto: `<style:style style:name="Break2" style:family="paragraph" style:parent-style-name="Body"><style:paragraph-properties fo:break-before="page"/></style:style>`,
	masters:     `<style:master-page style:name="Standard" style:page-layout-name="pm1"><style:header><text:p>Head ` + ann("h", "Ada", "Header.", "") + `words` + annEnd("h") + `</text:p></style:header></style:master-page>`,
	autoStyles: `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm"/>` +
		`<style:header-style><style:header-footer-properties fo:min-height="0cm"/></style:header-style></style:page-layout>`,
	body: `<text:p>Range ` + ann("a", "Ada", "Two paragraphs.\nSecond line.", "") + `covered</text:p>` +
		`<text:p>next` + annEnd("a") + ` after.</text:p>` +
		`<text:p>Point` + ann("", "Ada", "A point.", "") + ` here.</text:p>` +
		`<text:p>Note<text:note text:id="n1" text:note-class="footnote"><text:note-citation>1</text:note-citation><text:note-body><text:p>In ` + ann("f", "Ada", "In a note.", "") + `note` + annEnd("f") + `</text:p></text:note-body></text:note></text:p>` +
		`<text:p>` + ann("r", "Grace", "Reply.", ` loext:parent-name="t"`) + ann("t", "Ada", "Thread.", "") + `thread` + annEnd("t") + annEnd("r") +
		` and ` + ann("d", "Alan", "Resolved.", ` loext:resolved="true"`) + `done` + annEnd("d") + `.</text:p>` +
		`<text:p>Overlap ` + ann("o1", "Ada", "A.", "") + `alpha ` + ann("o2", "Grace", "B.", "") + `beta` + annEnd("o1") + ` gamma` + annEnd("o2") + `.</text:p>` +
		`<text:p text:style-name="Body">Across ` + ann("x", "Alan", "Across the break.", "") + `before</text:p><text:p text:style-name="Break2">after` + annEnd("x") + `</text:p>`,
}

// TestComments_Annotations holds what a comment of an ODT document becomes
// under office.CommentsAnnotations (ODF 1.3, Part 3, 14.1 and 14.2): a
// mark over the lines of its range, in each paragraph it spans and on
// each page, a point where it has no range, a reply on its thread's
// comment (loext:parent-name), its resolution (loext:resolved), and
// nothing of a comment in a header.
func TestComments_Annotations(t *testing.T) {
	t.Parallel()
	d := convertDoc(t, commentDoc.flat(), office.Options{Comments: office.CommentsAnnotations})
	if len(d.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(d.Pages))
	}
	var got []string
	for i, pg := range d.Pages {
		for _, m := range pg.Comments {
			kind := fmt.Sprintf("%d lines", len(m.Lines))
			if m.Lines == nil {
				kind = "point"
			}
			got = append(got, fmt.Sprintf("p%d %s: %s by %s, %d replies, resolved %v", i+1, kind, m.Comment.Text, m.Comment.Author, len(m.Comment.Replies), m.Comment.Resolved))
			if m.Parent == nil || m.Parent.Role != "Annot" {
				t.Errorf("%q hangs from %+v", m.Comment.Text, m.Parent)
			}
		}
	}
	want := []string{
		"p1 2 lines: Two paragraphs.\nSecond line. by Ada, 0 replies, resolved false",
		"p1 point: A point. by Ada, 0 replies, resolved false",
		"p1 1 lines: Thread. by Ada, 1 replies, resolved false",
		"p1 1 lines: Resolved. by Alan, 0 replies, resolved true",
		"p1 1 lines: A. by Ada, 0 replies, resolved false",
		"p1 1 lines: B. by Grace, 0 replies, resolved false",
		"p1 1 lines: Across the break. by Alan, 0 replies, resolved false",
		"p1 1 lines: In a note. by Ada, 0 replies, resolved false",
		"p2 1 lines: Across the break. by Alan, 0 replies, resolved false",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("comments\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	c := d.Pages[0].Comments[0].Comment
	if c.Zoned || !c.Date.Equal(time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC)) {
		t.Errorf("date %v, zoned %v", c.Date, c.Zoned)
	}
	if !slices.ContainsFunc(d.Warnings, func(w typeset.Warning) bool {
		return w.Code == warn.ContentUnsupported && strings.Contains(w.Message, "headers and footers")
	}) {
		t.Errorf("warnings %+v", d.Warnings)
	}
	plain := convertDoc(t, commentDoc.flat(), office.Options{})
	for i := range d.Pages {
		if len(plain.Pages[i].Comments) > 0 {
			t.Error("the comments are written by default")
		}
		if a, b := runs(d.Pages[i]), runs(plain.Pages[i]); !slices.Equal(a, b) {
			t.Errorf("page %d: the comments move the text:\n%v\n%v", i+1, a, b)
		}
	}
}
