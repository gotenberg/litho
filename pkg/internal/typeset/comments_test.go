package typeset_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// commentDoc is a tagged document of two pages whose text carries
// comments: one over two lines with a reply, resolved, one on a point,
// and one whose text runs from the first page to the second. Each
// comment's text sits in an Annot element, which its annotations join.
// Without comments it is the same pages with nothing more.
func commentDoc(t *testing.T, comments bool) *typeset.Document {
	t.Helper()
	root := &typeset.StructElem{Role: "Document"}
	p := &typeset.StructElem{Role: "P"}
	root.Kids = append(root.Kids, p)
	annot := func() *typeset.StructElem {
		e := &typeset.StructElem{Role: "Annot"}
		p.Kids = append(p.Kids, e)
		return e
	}
	first, point, across := annot(), annot(), annot()
	l1 := run(t, sans, 12, typeset.Point{X: 40, Y: 60}, "Commented words")
	l2 := run(t, sans, 12, typeset.Point{X: 40, Y: 76}, "on two lines")
	l3 := run(t, sans, 12, typeset.Point{X: 40, Y: 92}, "Point")
	l4 := run(t, sans, 12, typeset.Point{X: 40, Y: 700}, "Across")
	l5 := run(t, sans, 12, typeset.Point{X: 40, Y: 60}, "pages")
	l1.Parent, l2.Parent, l3.Parent, l4.Parent, l5.Parent = first, first, point, across, across
	page1, page2 := letter(l1, l2, l3, l4), letter(l5)
	if comments {
		date := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
		reply := &typeset.Comment{Author: "Grace Hopper", Date: date, Zoned: true, Text: "Yes."}
		lines := &typeset.Comment{Author: "Ada Lovelace", Date: date, Zoned: true, Text: "Is this right?\nPlease check.", Resolved: true, Replies: []*typeset.Comment{reply}}
		at := &typeset.Comment{Author: "Ada Lovelace", Date: time.Date(2026, 1, 3, 9, 30, 0, 0, time.UTC), Text: "A point."}
		long := &typeset.Comment{Author: "Alan Turing", Text: "Across pages."}
		page1.Comments = []typeset.CommentMark{
			{Comment: lines, Lines: []typeset.Rect{{X: 40, Y: 50, Width: 90, Height: 14}, {X: 40, Y: 66, Width: 70, Height: 14}}, Parent: first},
			{Comment: at, At: typeset.Rect{X: 70, Y: 82, Width: 14, Height: 14}, Parent: point},
			{Comment: long, Lines: []typeset.Rect{{X: 40, Y: 690, Width: 40, Height: 14}}, Parent: across},
		}
		page2.Comments = []typeset.CommentMark{
			{Comment: long, Lines: []typeset.Rect{{X: 40, Y: 50, Width: 30, Height: 14}}, Parent: across},
		}
	}
	return &typeset.Document{
		Metadata:  typeset.Metadata{Title: "Comments", Lang: "en"},
		Pages:     []*typeset.Page{page1, page2},
		Structure: root,
	}
}

// TestWriteCommentsReadBack holds the annotations of comments (ISO
// 32000-1, 12.5.6): a Text annotation a page over the lines a comment
// covers, or at its point, a reply in reply to its comment on the
// comment's first page, and a resolved comment's Completed state.
func TestWriteCommentsReadBack(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := typeset.Write(context.Background(), commentDoc(t, true), &buf, typeset.Options{Version: typeset.Version17}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.Bytes()
	r := open(t, out)
	type annot struct{ subtype, contents string }
	read := func(page int) ([]annot, []models.Annotation) {
		got, err := r.ExtractAnnotations(context.Background(), page)
		if err != nil {
			t.Fatalf("ExtractAnnotations: %v", err)
		}
		var have []annot
		for _, a := range got {
			have = append(have, annot{a.Subtype, a.Contents})
		}
		return have, got
	}
	have, got := read(1)
	want := []annot{
		{"Text", "Is this right?\nPlease check."},
		{"Text", "Yes."},
		{"Text", "Completed"},
		{"Text", "A point."},
		{"Text", "Across pages."},
	}
	if !slices.Equal(have, want) {
		t.Fatalf("page 1 annotations = %+v\nwant %+v", have, want)
	}
	if rect := got[0].Rect; rect != [4]float64{40, 712, 130, 742} {
		t.Errorf("highlight rectangle = %v", rect)
	}
	if got[1].Rect != got[0].Rect || got[3].Rect != [4]float64{70, 696, 84, 710} {
		t.Errorf("reply at %v, point at %v", got[1].Rect, got[3].Rect)
	}
	if have, _ := read(2); !slices.Equal(have, []annot{{"Text", "Across pages."}}) {
		t.Errorf("page 2 annotations = %+v", have)
	}
	for _, key := range []string{"/IRT", "/StateModel (Review)", "/State (Completed)", "/T (Ada Lovelace)", "/M (D:20260102100000Z00'00')", "/CreationDate (D:20260103093000)", "/Name /Comment", "/F 4"} {
		if !bytes.Contains(out, []byte(key)) {
			t.Errorf("the file holds no %s", key)
		}
	}
	if n := bytes.Count(out, []byte("/StructParent ")); n != 6 {
		t.Errorf("%d annotations joined the structure tree, want 6", n)
	}
}

// TestWriteCommentsDrawNothing renders the pages with their comments and
// without: an annotation's appearance stream is empty, so the pages are
// the same pixels.
func TestWriteCommentsDrawNothing(t *testing.T) {
	t.Parallel()
	with, without := open(t, write(t, commentDoc(t, true))), open(t, write(t, commentDoc(t, false)))
	for page := 1; page <= 2; page++ {
		samePixels(t, render(t, with, page), render(t, without, page), 0)
	}
}

// TestWriteCommentsRemediate remediates a document with comments to PDF/A
// and PDF/UA: the annotations as written meet each profile, so no fix
// touches them and the output validates.
func TestWriteCommentsRemediate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		version typeset.Version
		fix     func(context.Context, *pdf.Writer) (*models.RemediationReport, error)
		check   func(context.Context, *pdf.Reader) ([]error, error)
	}{
		{"PDF/A-1b", typeset.Version14, func(ctx context.Context, w *pdf.Writer) (*models.RemediationReport, error) {
			return w.RemediatePDFA(ctx, models.PDFA1b)
		}, func(ctx context.Context, r *pdf.Reader) ([]error, error) { return r.ValidatePDFA(ctx, models.PDFA1b) }},
		{"PDF/A-2b", typeset.Version17, func(ctx context.Context, w *pdf.Writer) (*models.RemediationReport, error) {
			return w.RemediatePDFA(ctx, models.PDFA2b)
		}, func(ctx context.Context, r *pdf.Reader) ([]error, error) { return r.ValidatePDFA(ctx, models.PDFA2b) }},
		{"PDF/A-4", typeset.Version20, func(ctx context.Context, w *pdf.Writer) (*models.RemediationReport, error) {
			return w.RemediatePDFA(ctx, models.PDFA4)
		}, func(ctx context.Context, r *pdf.Reader) ([]error, error) { return r.ValidatePDFA(ctx, models.PDFA4) }},
		{"PDF/UA-1", typeset.Version17, func(ctx context.Context, w *pdf.Writer) (*models.RemediationReport, error) {
			return w.RemediatePDFUA(ctx, models.PDFUA1)
		}, func(ctx context.Context, r *pdf.Reader) ([]error, error) { return r.ValidatePDFUA(ctx, models.PDFUA1) }},
		{"PDF/UA-2", typeset.Version20, func(ctx context.Context, w *pdf.Writer) (*models.RemediationReport, error) {
			return w.RemediatePDFUA(ctx, models.PDFUA2)
		}, func(ctx context.Context, r *pdf.Reader) ([]error, error) { return r.ValidatePDFUA(ctx, models.PDFUA2) }},
	} {
		var buf bytes.Buffer
		if err := typeset.Write(context.Background(), commentDoc(t, true), &buf, typeset.Options{Version: c.version}); err != nil {
			t.Fatalf("Write: %v", err)
		}
		report, fixed := remediate(t, buf.Bytes(), c.fix)
		for _, f := range report.Fixed {
			t.Logf("%s fixed: %s", c.name, f)
			if strings.Contains(strings.ToLower(f), "annot") {
				t.Errorf("%s fixed an annotation: %s", c.name, f)
			}
		}
		for _, v := range report.Residual {
			t.Errorf("%s residual: %v", c.name, v)
		}
		violations, err := c.check(context.Background(), open(t, fixed))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, v := range violations {
			t.Errorf("%s remediated output: %v", c.name, v)
		}
	}
}

// TestWriteCommentsInPDF14 writes a document with comments as a PDF 1.4
// file, which has no /IRT, /CreationDate or state model (ISO 32000-1,
// Tables 170 and 172, PDF 1.5 entries): a reply is a comment of its own at
// its thread's place, and a resolved thread writes no state.
func TestWriteCommentsInPDF14(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := typeset.Write(context.Background(), commentDoc(t, true), &buf, typeset.Options{Version: typeset.Version14}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.Bytes()
	for _, key := range []string{"/IRT", "/State", "/CreationDate"} {
		if bytes.Contains(out, []byte(key)) {
			t.Errorf("a PDF 1.4 file holds %s", key)
		}
	}
	annots, err := open(t, out).ExtractAnnotations(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractAnnotations: %v", err)
	}
	if len(annots) != 4 || annots[1].Contents != "Yes." {
		t.Errorf("page 1 annotations %+v, want the comment, its reply, the point and the range across pages", annots)
	}
}

// TestWriteCommentsEmptyTextNamesItsAuthor gives a comment without text,
// which its replies keep, its author as its content: ISO 14289-1, 7.18.1
// asks every annotation for one.
func TestWriteCommentsEmptyTextNamesItsAuthor(t *testing.T) {
	t.Parallel()
	page := letter()
	page.Comments = []typeset.CommentMark{{
		Comment: &typeset.Comment{Author: "Ada Lovelace", Text: " ", Replies: []*typeset.Comment{{Author: "Grace Hopper", Text: "Why empty?"}}},
		At:      typeset.Rect{X: 10, Y: 10, Width: 12, Height: 12},
	}}
	annots, err := open(t, write(t, &typeset.Document{Pages: []*typeset.Page{page}})).ExtractAnnotations(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractAnnotations: %v", err)
	}
	if len(annots) != 2 || annots[0].Contents != "Ada Lovelace" || annots[1].Contents != "Why empty?" {
		t.Errorf("annotations %+v", annots)
	}
}

func TestWriteCommentsDeterministic(t *testing.T) {
	t.Parallel()
	if a, b := write(t, commentDoc(t, true)), write(t, commentDoc(t, true)); !bytes.Equal(a, b) {
		t.Error("two writes of one document with comments differ")
	}
}

func TestValidateRefusesBadComments(t *testing.T) {
	t.Parallel()
	ok := &typeset.Comment{Text: "Fine."}
	for _, c := range []struct {
		name string
		mark typeset.CommentMark
		want error
	}{
		{"no comment", typeset.CommentMark{At: typeset.Rect{Width: 1, Height: 1}}, typeset.ErrInvalidComment},
		{"empty point", typeset.CommentMark{Comment: ok}, typeset.ErrInvalidComment},
		{"empty line", typeset.CommentMark{Comment: ok, Lines: []typeset.Rect{{Width: 0, Height: 10}}}, typeset.ErrInvalidComment},
		{"reply with replies", typeset.CommentMark{Comment: &typeset.Comment{Replies: []*typeset.Comment{{Replies: []*typeset.Comment{ok}}}}, At: typeset.Rect{Width: 1, Height: 1}}, typeset.ErrInvalidComment},
		{"long text", typeset.CommentMark{Comment: &typeset.Comment{Text: strings.Repeat("a", typeset.MaxCommentText+1)}, At: typeset.Rect{Width: 1, Height: 1}}, typeset.ErrLimitExceeded},
		{"not UTF-8", typeset.CommentMark{Comment: &typeset.Comment{Author: "\xff"}, At: typeset.Rect{Width: 1, Height: 1}}, typeset.ErrInvalidComment},
	} {
		page := letter()
		page.Comments = []typeset.CommentMark{c.mark}
		err := (&typeset.Document{Pages: []*typeset.Page{page}}).Validate(context.Background())
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}
