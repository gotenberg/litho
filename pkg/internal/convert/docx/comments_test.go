package docx

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
	"github.com/gotenberg/litho/pkg/models"
)

// commentParts returns the comments part of the comments given as id,
// author, text, and Word's extension part giving each its parent's id
// ("" for none) and done flag.
func commentParts(comments [][3]string, parents map[string]string, done map[string]bool) map[string]string {
	var c, x strings.Builder
	c.WriteString(`<w:comments ` + nsDecl + ` xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml">`)
	x.WriteString(`<w15:commentsEx xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml">`)
	para := func(id string) string { return "0000000" + id }
	for _, cm := range comments {
		fmt.Fprintf(&c, `<w:comment w:id="%s" w:author="%s" w:date="2026-01-02T10:00:00Z" w:initials="X">`, cm[0], cm[1])
		for i, p := range strings.Split(cm[2], "\n") {
			ref := ""
			if i == 0 {
				ref = `<w:r><w:annotationRef/></w:r>`
			}
			pid := "1111" + fmt.Sprintf("%04d", i)
			if i == len(strings.Split(cm[2], "\n"))-1 {
				pid = para(cm[0])
			}
			fmt.Fprintf(&c, `<w:p w14:paraId="%s">%s<w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, pid, ref, p)
		}
		c.WriteString(`</w:comment>`)
		attrs := ""
		if p := parents[cm[0]]; p != "" {
			attrs += ` w15:paraIdParent="` + para(p) + `"`
		}
		if done[cm[0]] {
			attrs += ` w15:done="1"`
		}
		fmt.Fprintf(&x, `<w15:commentEx w15:paraId="%s"%s/>`, para(cm[0]), attrs)
	}
	c.WriteString(`</w:comments>`)
	x.WriteString(`</w15:commentsEx>`)
	return map[string]string{"word/comments.xml": c.String(), "word/commentsExtended.xml": x.String()}
}

// commentsOf returns the comment marks of doc's pages, page by page.
func commentsOf(doc *typeset.Document) [][]typeset.CommentMark {
	out := make([][]typeset.CommentMark, len(doc.Pages))
	for i, p := range doc.Pages {
		out[i] = p.Comments
	}
	return out
}

func rs(id string) string { return `<w:commentRangeStart w:id="` + id + `"/>` }
func re(id string) string { return `<w:commentRangeEnd w:id="` + id + `"/>` }
func rf(id string) string { return `<w:r><w:commentReference w:id="` + id + `"/></w:r>` }
func tr(s string) string  { return `<w:r><w:t xml:space="preserve">` + s + `</w:t></w:r>` }

// TestComments_Annotations holds what a comment of a DOCX document becomes
// under models.CommentsAnnotations (ECMA-376 Part 1, 17.13.4): a mark over
// the lines of its range, in each paragraph it spans and on each side of a
// page break, a point where its range is empty or missing, the replies of
// its thread (w15:paraIdParent) on its comment, its resolution (w15:done),
// its author, date and text, paragraphs a line each.
func TestComments_Annotations(t *testing.T) {
	t.Parallel()
	body := `<w:p>` + tr("Range ") + rs("0") + tr("covered words") + `</w:p>` +
		`<w:p>` + tr("next paragraph") + re("0") + rf("0") + tr(" after.") + `</w:p>` +
		`<w:p>` + tr("Point") + rf("1") + tr(" and empty") + rs("2") + re("2") + rf("2") + `</w:p>` +
		`<w:p>` + rs("3") + rs("4") + tr("thread") + re("3") + rf("3") + re("4") + rf("4") + `</w:p>` +
		`<w:p>` + rs("5") + tr("before") + `<w:r><w:br w:type="page"/></w:r>` + tr("after") + re("5") + rf("5") + `</w:p>` +
		`<w:p>` + rs("6") + `<w:hyperlink r:id="rIdlink">` + tr("link") + re("6") + tr(" text") + `</w:hyperlink>` + rf("6") + `</w:p>`
	extra := commentParts([][3]string{
		{"0", "Ada", "Two paragraphs.\nSecond line."},
		{"1", "Ada", "A point."},
		{"2", "Grace", "Empty range."},
		{"3", "Ada", "Thread."},
		{"4", "Grace", "Reply."},
		{"5", "Alan", "Across the break."},
		{"6", "Alan", "Into a link."},
	}, map[string]string{"4": "3"}, map[string]bool{"3": true})
	doc := convertBytes(t, docx(t, body, extra), office.Options{Comments: models.CommentsAnnotations})
	pages := commentsOf(doc)
	if len(pages) != 2 {
		t.Fatalf("%d pages, want 2", len(doc.Pages))
	}
	var got []string
	for i, marks := range pages {
		for _, m := range marks {
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
		"p1 point: Empty range. by Grace, 0 replies, resolved false",
		"p1 1 lines: Thread. by Ada, 1 replies, resolved true",
		"p1 1 lines: Across the break. by Alan, 0 replies, resolved false",
		"p2 1 lines: Across the break. by Alan, 0 replies, resolved false",
		"p2 1 lines: Into a link. by Alan, 0 replies, resolved false",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("comments\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	c := pages[0][0].Comment
	if !c.Zoned || !c.Date.Equal(time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("date %v, zoned %v", c.Date, c.Zoned)
	}
	if r := pages[0][3].Comment.Replies; len(r) != 1 || r[0].Text != "Reply." || r[0].Author != "Grace" {
		t.Errorf("replies %+v", r)
	}
	plain := convertBytes(t, docx(t, body, extra), office.Options{})
	for _, p := range plain.Pages {
		if len(p.Comments) > 0 {
			t.Error("the comments are written by default")
		}
	}
	if a, b := docText(doc), docText(plain); a != b {
		t.Errorf("the comments change the text:\n%q\n%q", a, b)
	}
	for i := range doc.Pages {
		if a, b := textOrigins(doc.Pages[i]), textOrigins(plain.Pages[i]); a != b {
			t.Errorf("page %d: the comments move the text", i+1)
		}
	}
}

// textOrigins returns where each run of pg starts and what it draws.
func textOrigins(pg *typeset.Page) string {
	var b strings.Builder
	var walk func([]typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Text:
				fmt.Fprintf(&b, "%.4f,%.4f:", v.Origin.X, v.Origin.Y)
				for _, g := range v.Glyphs {
					fmt.Fprintf(&b, "%d@%.4f ", g.GID, g.X)
				}
				b.WriteByte('\n')
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(pg.Content)
	return b.String()
}

// TestComments_EmptyLeftOut leaves out a comment that holds no text and
// no reply: nothing of it would reach a reader.
func TestComments_EmptyLeftOut(t *testing.T) {
	t.Parallel()
	body := `<w:p>` + rs("0") + tr("covered") + re("0") + rf("0") + rs("1") + tr(" kept") + re("1") + rf("1") + `</w:p>`
	doc := convertBytes(t, docx(t, body, commentParts([][3]string{{"0", "Ada", " "}, {"1", "Ada", "Kept."}}, nil, nil)), office.Options{Comments: models.CommentsAnnotations})
	if m := doc.Pages[0].Comments; len(m) != 1 || m[0].Comment.Text != "Kept." {
		t.Errorf("comments %+v", m)
	}
}

// TestComments_HeaderLeftOut warns of a comment in a header, which is
// left out.
func TestComments_HeaderLeftOut(t *testing.T) {
	t.Parallel()
	body := `<w:p>` + tr("Body") + `</w:p><w:sectPr><w:headerReference w:type="default" r:id="rIdheader1"/></w:sectPr>`
	extra := commentParts([][3]string{{"0", "Ada", "In the header."}}, nil, nil)
	extra["word/header1.xml"] = `<w:hdr ` + nsDecl + `><w:p>` + rs("0") + tr("Head") + re("0") + rf("0") + `</w:p></w:hdr>`
	doc := convertBytes(t, docx(t, body, extra), office.Options{Comments: models.CommentsAnnotations})
	for _, p := range doc.Pages {
		if len(p.Comments) > 0 {
			t.Errorf("header comment written: %+v", p.Comments)
		}
	}
	found := false
	for _, w := range doc.Warnings {
		found = found || (w.Code == warn.ContentUnsupported && strings.Contains(w.Message, "headers and footers"))
	}
	if !found {
		t.Errorf("warnings %+v", doc.Warnings)
	}
}

// TestComments_TextBounded cuts a comment's text at typeset.MaxCommentText
// bytes, on a character, with a warning.
func TestComments_TextBounded(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", typeset.MaxCommentText)
	body := `<w:p>` + rs("0") + tr("x") + re("0") + rf("0") + `</w:p>`
	doc := convertBytes(t, docx(t, body, commentParts([][3]string{{"0", "Ada", long}}, nil, nil)), office.Options{Comments: models.CommentsAnnotations})
	c := doc.Pages[0].Comments[0].Comment
	if len(c.Text) > typeset.MaxCommentText || !strings.HasPrefix(long, c.Text) {
		t.Errorf("text of %d bytes", len(c.Text))
	}
	found := false
	for _, w := range doc.Warnings {
		found = found || w.Code == warn.LimitReached
	}
	if !found {
		t.Errorf("warnings %+v", doc.Warnings)
	}
}

// TestComments_LeaveDrawingsAndControlsAsTheyAre covers, with comment
// ranges, a picture placed out of the line and the result of a text form
// field: the picture keeps its place, as it is laid out without the
// comment, and the field keeps its value.
func TestComments_LeaveDrawingsAndControlsAsTheyAre(t *testing.T) {
	t.Parallel()
	body := `<w:p>` + rs("0") + `<w:r><w:drawing><wp:anchor behindDoc="0"><wp:positionH relativeFrom="column"><wp:posOffset>254000</wp:posOffset></wp:positionH>` +
		`<wp:positionV relativeFrom="paragraph"><wp:posOffset>127000</wp:posOffset></wp:positionV><wp:extent cx="1270000" cy="1270000"/><wp:wrapNone/>` +
		`<wp:docPr id="2" name="q"/>` + graphic("") + `</wp:anchor></w:drawing></w:r>` + tr("text beside") + re("0") + rf("0") + `</w:p>` +
		`<w:p><w:r><w:fldChar w:fldCharType="begin"><w:ffData><w:name w:val="Text1"/><w:enabled/><w:textInput/></w:ffData></w:fldChar></w:r>` +
		`<w:r><w:instrText xml:space="preserve"> FORMTEXT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		rs("1") + tr("Jane") + `<w:r><w:fldChar w:fldCharType="end"/></w:r>` + tr(" Doe") + re("1") + rf("1") + `</w:p>`
	extra := commentParts([][3]string{{"0", "Ada", "On the picture."}, {"1", "Ada", "On the field."}}, nil, nil)
	extra["word/media/a.png"] = pngData(t, 40, 20)
	on := convertBytes(t, docx(t, body, extra), office.Options{Comments: models.CommentsAnnotations, FormFields: true})
	off := convertBytes(t, docx(t, body, extra), office.Options{FormFields: true})
	if len(on.Pages[0].Comments) != 2 {
		t.Fatalf("comments %+v", on.Pages[0].Comments)
	}
	images := func(pg *typeset.Page) []typeset.Rect {
		var out []typeset.Rect
		var walk func([]typeset.Fragment)
		walk = func(fs []typeset.Fragment) {
			for _, f := range fs {
				switch v := f.(type) {
				case *typeset.Image:
					out = append(out, v.Rect)
				case *typeset.Group:
					walk(v.Content)
				}
			}
		}
		walk(pg.Content)
		return out
	}
	if a, b := images(on.Pages[0]), images(off.Pages[0]); len(a) != 1 || len(b) != 1 || a[0] != b[0] {
		t.Errorf("the picture at %v with the comments, %v without", a, b)
	}
	if a, b := textOrigins(on.Pages[0]), textOrigins(off.Pages[0]); a != b {
		t.Error("the comments move the text")
	}
	if f := on.Pages[0].Fields; len(f) != 1 || f[0].Value != "Jane" {
		t.Errorf("fields %+v", f)
	}
}
