package typeset

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// Comment is a remark a reviewer left on the text of a document, with the
// replies of its thread: the content of a markup annotation (ISO 32000-1,
// 12.5.6.2).
type Comment struct {
	// Author names who wrote the comment, "" for no one.
	Author string
	// Date is when the comment was written, as its source records it; the
	// zero time records none. Zoned reports a source that states the
	// offset from UTC, which Date then holds; without it Date's clock
	// reads as the source wrote it and the PDF date names no offset.
	Date  time.Time
	Zoned bool
	// Text is the comment's content as plain text, its paragraphs parted
	// by line feeds. It holds at most [MaxCommentText] bytes of UTF-8.
	Text string
	// Resolved marks a comment its thread closed.
	Resolved bool
	// Replies are the replies of the comment's thread, in the order they
	// were written. A reply has no reply of its own: a thread is one
	// comment and its replies. At most [MaxCommentReplies].
	Replies []*Comment
}

// CommentMark places a comment on a page: the lines of text it covers
// there, or the point it refers to. A comment whose text runs over
// several pages has a mark on each.
type CommentMark struct {
	Comment *Comment
	// Lines are the boxes of the text the comment covers on the page, one a
	// line, in page coordinates and document order. nil places the
	// comment on a point.
	Lines []Rect
	// At is the box of the point a comment on a point refers to, in page
	// coordinates, with a positive width and height. Lines leaves it
	// unused.
	At Rect
	// Parent is the Annot structure element the comment's annotations
	// belong to, nil for an untagged document.
	Parent *StructElem
}

// MaxCommentText bounds the bytes of a [Comment]'s Author and Text.
// Anti-DoS guard, not spec-mandated: ISO 32000-1, Annex C.2 bounds a
// string at 32767 bytes, which a text written as UTF-16 reaches at 16 KiB
// of UTF-8, and a comment holds a few sentences.
const MaxCommentText = 16 << 10

// MaxCommentReplies bounds the replies of one [Comment]. Anti-DoS guard,
// not spec-mandated: each reply is an annotation on its comment's first
// page, whose /Annots Annex C.2 bounds at 8191 entries, and a thread
// holds a few replies.
const MaxCommentReplies = 1000

// maxCommentLines bounds the [CommentMark.Lines] of one mark. Anti-DoS
// guard, not spec-mandated: the lines are checked and joined each time
// the page is written, and a page holds a few hundred lines.
const maxCommentLines = 1000

// maxCommentMarksPerPage bounds the [Page.Comments] of one page. Anti-DoS
// guard, not spec-mandated: each mark writes an annotation in the page's
// /Annots, which Annex C.2 bounds at 8191 entries with the links and the
// widgets.
const maxCommentMarksPerPage = 2048

// commentCost is what one comment annotation held until the end of the
// file costs the conversion's budget, past the bytes of its text: its
// dictionary of a dozen entries as Go maps and values. Memory guard, not
// spec-mandated.
const commentCost = 1 << 10

// stateCompleted is the state a resolved comment's thread is set to: the
// Review state model's Completed (ISO 32000-1, 12.5.6.3, Table 171).
const stateCompleted = "Completed"

// pendingComment is a comment annotation whose dictionary is finished at
// the end: its /StructParent key is the page count plus its index among
// the annotations of the structure tree.
type pendingComment struct {
	ref   ast.PdfIndirectRef
	dict  ast.PdfDict
	annot int
}

// comment checks m.
func (v *validator) comment(m CommentMark) error {
	c := m.Comment
	if c == nil {
		return fmt.Errorf("%w: no comment", ErrInvalidComment)
	}
	if len(m.Lines) > maxCommentLines {
		return fmt.Errorf("%w: %d lines, at most %d", ErrLimitExceeded, len(m.Lines), maxCommentLines)
	}
	for _, r := range m.Lines {
		if !finiteRect(r) || r.Width <= 0 || r.Height <= 0 {
			return fmt.Errorf("%w: line %v", ErrInvalidComment, r)
		}
	}
	if m.Lines == nil && (!finiteRect(m.At) || m.At.Width <= 0 || m.At.Height <= 0) {
		return fmt.Errorf("%w: point %v", ErrInvalidComment, m.At)
	}
	if len(c.Replies) > MaxCommentReplies {
		return fmt.Errorf("%w: %d replies, at most %d", ErrLimitExceeded, len(c.Replies), MaxCommentReplies)
	}
	for _, r := range append([]*Comment{c}, c.Replies...) {
		if r == nil {
			return fmt.Errorf("%w: no reply", ErrInvalidComment)
		}
		if r != c && len(r.Replies) > 0 {
			return fmt.Errorf("%w: a reply with replies", ErrInvalidComment)
		}
		if len(r.Text) > MaxCommentText || len(r.Author) > MaxCommentText {
			return fmt.Errorf("%w: text past %d bytes", ErrLimitExceeded, MaxCommentText)
		}
		if !utf8.ValidString(r.Text) || !utf8.ValidString(r.Author) {
			return fmt.Errorf("%w: text that is not UTF-8", ErrInvalidComment)
		}
	}
	return v.parent(m.Parent)
}

// comments writes the annotations of the comment marks of the page at
// index page, of height points, and returns their references for the
// page's /Annots. Each mark is a Text annotation (ISO 32000-1, 12.5.6.4),
// a markup annotation (12.5.6.2) whose rectangle covers the lines of text
// the comment covers on the page, or its point. The first mark of a
// comment carries its thread: a Text annotation in reply to it for each
// reply (/IRT, Table 170), and, for a resolved comment, a state
// annotation setting it to Completed in the Review model (12.5.6.3).
// Every annotation draws nothing: its appearance stream is empty, so the
// page shows what it shows without it, and PDF/A's rule that every
// annotation has one holds (ISO 19005-2, 6.3.3).
//
// A Highlight (12.5.6.10) would mark the lines in the reader's comment
// list as well, but a reader may draw it from its /QuadPoints whatever
// its appearance says, and the page then shows more than without it.
//
// # Corpus evidence
//
//   - probe, docx: LibreOffice 26.8 of the pinned image exports every
//     comment as a Text annotation, with a pop-up, at its anchor in the
//     text: the end of a DOCX comment's range, the start of an ODT one's.
//   - probe, docx: poppler 26.11 draws a Highlight annotation whose
//     appearance stream is empty as a black box over its quadrilaterals,
//     with no /C or an empty one; a Text annotation whose appearance
//     stream is empty it draws as nothing.
func (w *docWriter) comments(ctx context.Context, page int, height float64, marks []CommentMark) (ast.PdfArray, error) {
	if len(marks) == 0 {
		return nil, nil
	}
	refs := make(ast.PdfArray, 0, len(marks))
	for _, m := range marks {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("writing page %d: %w", page+1, err)
		}
		rect := m.At
		if len(m.Lines) > 0 {
			rect = m.Lines[0]
			for _, r := range m.Lines[1:] {
				rect = union(rect, r)
			}
		}
		ref, err := w.commentAnnot(ctx, page, height, m, m.Comment, rect, ast.PdfIndirectRef{})
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
		if w.threaded[m.Comment] {
			continue
		}
		if w.threaded == nil {
			w.threaded = make(map[*Comment]bool)
		}
		w.threaded[m.Comment] = true
		for _, r := range m.Comment.Replies {
			rr, err := w.commentAnnot(ctx, page, height, m, r, rect, ref)
			if err != nil {
				return nil, err
			}
			refs = append(refs, rr)
		}
		if m.Comment.Resolved && w.version != Version14 {
			state := &Comment{Author: m.Comment.Author, Text: stateCompleted}
			sr, err := w.commentAnnot(ctx, page, height, m, state, rect, ref)
			if err != nil {
				return nil, err
			}
			w.comment[len(w.comment)-1].dict["StateModel"] = textString("Review")
			w.comment[len(w.comment)-1].dict["State"] = textString(stateCompleted)
			refs = append(refs, sr)
		}
	}
	return refs, nil
}

// commentAnnot writes the annotation of c at rect on the page at index
// page, a reply to the annotation at irt when irt is set, and returns its
// reference. Its dictionary waits for the end of the file
// ([docWriter.finishComments]).
func (w *docWriter) commentAnnot(ctx context.Context, page int, height float64, m CommentMark, c *Comment, rect Rect, irt ast.PdfIndirectRef) (ast.PdfIndirectRef, error) {
	cost := int64(commentCost + len(c.Text) + len(c.Author))
	if err := w.mem.ChargeUnits(cost, 1, "the comments of the pages written"); err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("writing page %d: %w", page+1, err)
	}
	w.commentCharged += cost
	ap, err := w.appearance(ctx, page, rect, nil, false)
	if err != nil {
		return ast.PdfIndirectRef{}, err
	}
	// The page's coordinates run down from its top; PDF's up from its
	// bottom (ISO 32000-1, 12.5.2, Table 164: /Rect is [llx lly urx ury]).
	dict := ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("Text"),
		"Name":    ast.PdfName("Comment"),
		"Rect":    ast.PdfArray{pdfReal(rect.X), pdfReal(height - rect.Y - rect.Height), pdfReal(rect.X + rect.Width), pdfReal(height - rect.Y)},
		// ISO 19005-2, 6.3.2: Print set, Hidden, Invisible and NoView
		// clear.
		"F":  ast.PdfInt(4),
		"AP": ast.PdfDict{"N": ap},
		// ISO 14289-1, 7.18.1: every annotation carries its content, or
		// its element an Alt; a comment without text, which a thread's
		// replies keep, says whose it is.
		"Contents": textString(c.Text),
	}
	if strings.TrimSpace(c.Text) == "" {
		dict["Contents"] = textString(c.Author)
	}
	// /IRT, /CreationDate and the state model are PDF 1.5 entries (ISO
	// 32000-1, Table 170 and 172): a PDF 1.4 file writes a reply as a
	// comment of its own at its thread's place, and no state.
	modern := w.version != Version14
	if irt.ObjectNum != 0 && modern {
		dict["IRT"] = irt
	}
	if c.Author != "" {
		dict["T"] = textString(c.Author)
	}
	if !c.Date.IsZero() {
		d := pdfDate(c.Date, c.Zoned)
		dict["M"] = d
		if modern {
			dict["CreationDate"] = d
		}
	}
	w.hashBytes([]byte(c.Author))
	w.hashBytes([]byte(c.Text))
	ref := w.alloc.Alloc(ast.PdfNull{})
	pc := pendingComment{ref: ref, dict: dict, annot: -1}
	if w.st != nil && m.Parent != nil {
		pc.annot = w.st.addAnnot(page, ref, m.Parent, nil)
	}
	w.comment = append(w.comment, pc)
	return ref, nil
}

// finishComments queues the comment annotations of a file of n pages and
// gives back what holding them cost.
func (w *docWriter) finishComments(n int) {
	for _, pc := range w.comment {
		if pc.annot >= 0 {
			pc.dict["StructParent"] = ast.PdfInt(n + pc.annot)
		}
		w.alloc.AllocAt(pc.ref.ObjectNum, pc.dict)
	}
	w.comment = nil
	w.mem.Release(w.commentCharged)
	w.commentCharged = 0
}

// pdfDate returns t as a PDF date (ISO 32000-1, 7.9.4): D:YYYYMMDDHHmmSS,
// then the offset from UTC when zoned, Z00'00' for UTC, nothing
// otherwise. A year outside 0 to 9999 is held at its bound.
//
// # Corpus evidence
//
//   - probe, docx: pdfcpu's strict validation, the targets oracle,
//     refuses a /CreationDate that ends in a bare Z and takes Z00'00'.
func pdfDate(t time.Time, zoned bool) ast.PdfString {
	y := min(max(t.Year(), 0), 9999)
	var sb strings.Builder
	fmt.Fprintf(&sb, "D:%04d%02d%02d%02d%02d%02d", y, int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
	if zoned {
		_, off := t.Zone()
		if off == 0 {
			sb.WriteString("Z00'00'")
		} else {
			sign := byte('+')
			if off < 0 {
				sign = '-'
			}
			m := int(math.Abs(float64(off))) / 60
			fmt.Fprintf(&sb, "%c%02d'%02d'", sign, m/60, m%60)
		}
	}
	return ast.PdfString(sb.String())
}

// union returns the smallest rectangle holding a and b.
func union(a, b Rect) Rect {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.Width, b.X+b.Width), max(a.Y+a.Height, b.Y+b.Height)
	return Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}
