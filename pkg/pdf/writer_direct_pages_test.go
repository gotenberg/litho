package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// directPageWords are the words the three pages of directPagesPDF show, in
// page order.
var directPageWords = []string{"Alpha", "Bravo", "Charlie"}

// directPagesPDF builds a three-page document whose first two pages are
// written directly in the /Kids of node 3, against ISO 32000-1, 7.7.3.2,
// Table 29, and whose third is listed by reference in the root. Page 1 states
// no /Resources and inherits node 3's; page 2 states its own. Each page shows
// its word of directPageWords.
//
// catalogExtra, page1Extra and page2Extra are entries added to the catalog and
// to the two direct pages, and extra are objects 10 on.
func directPagesPDF(catalogExtra, page1Extra, page2Extra string, extra ...string) []byte {
	content := func(word string) string {
		body := "BT /F1 12 Tf 20 200 Td (" + word + ") Tj ET"
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 3 /MediaBox [0 0 300 300] >>",
		"<< /Type /Pages /Parent 2 0 R /Count 2 /Resources 6 0 R /Kids [" +
			"<< /Type /Page /Parent 3 0 R /Contents 7 0 R " + page1Extra + " >> " +
			"<< /Type /Page /Parent 3 0 R /Contents 8 0 R /Resources 6 0 R " + page2Extra + " >>] >>",
		"<< /Type /Page /Parent 2 0 R /Contents 9 0 R /Resources 6 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Font << /F1 5 0 R >> >>",
		content(directPageWords[0]),
		content(directPageWords[1]),
		content(directPageWords[2]),
	}
	return buildPDF(append(objs, extra...))
}

// openDirect opens data, failing the test when it does not open.
func openDirect(t *testing.T, data []byte) *Reader {
	t.Helper()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// writeDirect queues edit on a Writer over data and returns what WriteTo
// writes.
func writeDirect(t *testing.T, data []byte, edit func(*Writer) error) []byte {
	t.Helper()
	w := NewWriter(openDirect(t, data))
	defer w.Close()
	if err := edit(w); err != nil {
		t.Fatalf("queuing the edit: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// directPageTexts returns the text of every page of data, artifacts included,
// and fails unless the page tree names each page by reference.
func directPageTexts(t *testing.T, data []byte) []string {
	t.Helper()
	ctx := context.Background()
	r := openDirect(t, data)
	pages, err := r.resolvePages(ctx)
	if err != nil {
		t.Fatalf("resolving the pages: %v", err)
	}
	texts := make([]string, len(pages))
	for i, page := range pages {
		if page.Ref.ObjectNum <= 0 {
			t.Errorf("page %d is written directly in /Kids in the output", i+1)
		}
		text, err := r.ExtractText(ctx, i+1, WithArtifacts())
		if err != nil {
			t.Fatalf("ExtractText(%d): %v", i+1, err)
		}
		texts[i] = text
	}
	return texts
}

// assertPageWords fails unless the pages of data show want, one word a page in
// order, and every page shows also.
func assertPageWords(t *testing.T, data []byte, want []string, also string) {
	t.Helper()
	texts := directPageTexts(t, data)
	if len(texts) != len(want) {
		t.Fatalf("the output has %d pages, want %d: %q", len(texts), len(want), texts)
	}
	for i, text := range texts {
		if !strings.Contains(text, want[i]) {
			t.Errorf("page %d reads %q, want %q", i+1, text, want[i])
		}
		if also != "" && !strings.Contains(text, also) {
			t.Errorf("page %d reads %q, want %q on it too", i+1, text, also)
		}
	}
}

// TestWriter_EditsPagesWrittenDirectlyInKids pins that every Writer path
// reaches a page written directly in its node's /Kids as it reaches one listed
// by reference, and that the output names every page by reference.
//
// ISO 32000-1, 7.7.3.2, Table 29 makes /Kids an array of indirect references,
// and the Writer's edits replace a page at its object number, which such a page
// does not have: a stamp, a rotation or a flattened widget was dropped, a page
// tree rebuilt from the pages' references listed the page as object 0, a signed
// signature was not drawn into it, and a copy of it pulled its whole source
// tree in through /Parent. The Writer now numbers each such page first (see
// [Writer.promotePageTree]).
//
// # Corpus evidence
//
//   - litho kidsrepair_1b.pdf: mutate-stamp left page 3 unstamped, and
//     mutate-pages wrote one page of four.
func TestWriter_EditsPagesWrittenDirectlyInKids(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := directPagesPDF("", "", "")

	t.Run("stamp", func(t *testing.T) {
		t.Parallel()
		out := writeDirect(t, base, func(w *Writer) error {
			return w.StampText(TextStamp{Text: "STAMPED", X: 10, Y: 10})
		})
		assertPageWords(t, out, directPageWords, "STAMPED")
	})

	t.Run("rotate", func(t *testing.T) {
		t.Parallel()
		out := writeDirect(t, base, func(w *Writer) error { return w.RotatePages(90) })
		r := openDirect(t, out)
		for n := 1; n <= 3; n++ {
			page, err := r.Page(ctx, n)
			if err != nil {
				t.Fatalf("Page(%d): %v", n, err)
			}
			if page.Rotate != 90 {
				t.Errorf("page %d is rotated %d, want 90", n, page.Rotate)
			}
		}
		assertPageWords(t, out, directPageWords, "")
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		out := writeDirect(t, base, func(w *Writer) error { return w.DeletePages(3) })
		assertPageWords(t, out, directPageWords[:2], "")
	})

	t.Run("move the first page to the end", func(t *testing.T) {
		t.Parallel()
		out := writeDirect(t, base, func(w *Writer) error {
			if err := w.InsertPages(3, openDirect(t, base), 1); err != nil {
				return err
			}
			return w.DeletePages(1)
		})
		assertPageWords(t, out, []string{"Bravo", "Charlie", "Alpha"}, "")
	})

	t.Run("insert from a source", func(t *testing.T) {
		t.Parallel()
		out := writeDirect(t, blankPagePDF(), func(w *Writer) error {
			return w.InsertPages(1, openDirect(t, base), 1, 2)
		})
		assertPageWords(t, out, []string{"", "Alpha", "Bravo"}, "")
		assertOnePagesNode(t, out)
	})

	t.Run("split", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if err := Split(ctx, openDirect(t, base), []int{1, 2}, &out); err != nil {
			t.Fatalf("Split: %v", err)
		}
		assertPageWords(t, out.Bytes(), directPageWords[:2], "")
		assertOnePagesNode(t, out.Bytes())
	})

	t.Run("merge", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if err := Merge(ctx, []*Reader{openDirect(t, base), openDirect(t, base)}, &out); err != nil {
			t.Fatalf("Merge: %v", err)
		}
		assertPageWords(t, out.Bytes(), slices.Concat(directPageWords, directPageWords), "")
		assertOnePagesNode(t, out.Bytes())
	})

	t.Run("bookmarks", func(t *testing.T) {
		t.Parallel()
		out := writeDirect(t, base, func(w *Writer) error {
			w.SetBookmarks([]models.Bookmark{{Title: "Alpha", PageNumber: 1}, {Title: "Bravo", PageNumber: 2}})
			return nil
		})
		marks, err := openDirect(t, out).Bookmarks(ctx)
		if err != nil {
			t.Fatalf("Bookmarks: %v", err)
		}
		if len(marks) != 2 || marks[0].PageNumber != 1 || marks[1].PageNumber != 2 {
			t.Errorf("the output's bookmarks are %+v, want Alpha on page 1 and Bravo on page 2", marks)
		}
		directPageTexts(t, out)
	})

	t.Run("autotag", func(t *testing.T) {
		t.Parallel()
		var report *AutoTagReport
		out := writeDirect(t, base, func(w *Writer) error {
			report = w.AutoTag()
			return nil
		})
		if report.Skipped != nil {
			t.Fatalf("AutoTag skipped: %v", report.Skipped)
		}
		r := openDirect(t, out)
		pages, err := r.resolvePages(ctx)
		if err != nil {
			t.Fatalf("resolving the pages: %v", err)
		}
		tagged := structElementPages(ctx, t, r)
		for i, page := range pages {
			if !tagged[page.Ref.ObjectNum] || page.Ref.ObjectNum <= 0 {
				t.Errorf("no structure element names page %d (object %d) in /Pg", i+1, page.Ref.ObjectNum)
			}
		}
	})

	t.Run("redact", func(t *testing.T) {
		t.Parallel()
		w := NewWriter(openDirect(t, base))
		defer w.Close()
		if _, err := w.Redact(ctx, []RedactRegion{{Page: 2, Rect: [4]float64{0, 0, 300, 300}}}); err != nil {
			t.Fatalf("Redact: %v", err)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		texts := directPageTexts(t, out.Bytes())
		if len(texts) != 3 || strings.Contains(texts[1], "Bravo") || !strings.Contains(texts[0], "Alpha") || !strings.Contains(texts[2], "Charlie") {
			t.Errorf("the redacted pages read %q, want page 2 without Bravo and the others whole", texts)
		}
	})

	t.Run("stamp then sign", func(t *testing.T) {
		t.Parallel()
		signer := newCMSTestSigner(t)
		out := writeDirect(t, base, func(w *Writer) error {
			w.Sign(signer)
			return w.StampText(TextStamp{Text: "STAMPED", X: 10, Y: 10})
		})
		checkSignedOutput(t, out, signer)
		assertPageWords(t, out, directPageWords, "STAMPED")
	})
}

// TestWriter_FormPathsReachAPageWrittenDirectlyInKids pins that flattening a
// form, drawing a signed signature into its page on a full rewrite, and
// removing an attachment annotation reach a widget or annotation that sits on a
// page written directly in /Kids.
func TestWriter_FormPathsReachAPageWrittenDirectlyInKids(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	appearance := func(text string) string {
		body := "BT /F1 10 Tf 2 5 Td (" + text + ") Tj ET"
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 50] /Resources 6 0 R /Length %d >>\nstream\n%s\nendstream",
			len(body), body)
	}

	t.Run("flatten", func(t *testing.T) {
		t.Parallel()
		data := directPagesPDF("/AcroForm << /Fields [10 0 R] >>", "", "/Annots [10 0 R]",
			"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /V (Paris) /Rect [20 100 220 150] /F 4 /AP << /N 11 0 R >> >>",
			appearance("Paris"))
		out := writeDirect(t, data, func(w *Writer) error {
			w.FlattenForm()
			return nil
		})
		if got := pageContent(ctx, t, out, 2); !strings.Contains(got, " Do") {
			t.Errorf("page 2 does not draw the flattened widget: %q", got)
		}
		annots, err := openDirect(t, out).ExtractAnnotations(ctx, 2)
		if err != nil {
			t.Fatalf("ExtractAnnotations: %v", err)
		}
		if len(annots) != 0 {
			t.Errorf("page 2 keeps annotations %+v after flattening", annots)
		}
	})

	signed := directPagesPDF("/AcroForm << /Fields [10 0 R] /SigFlags 3 >>", "", "/Annots [10 0 R]",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (signed) /V 12 0 R /Rect [20 150 220 200] /F 4 /AP << /N 11 0 R >> >>",
		appearance("Signed by Jane"),
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 100 200 300] /Contents <30820001> >>")

	t.Run("signed signature drawn into an inserted page", func(t *testing.T) {
		t.Parallel()
		out := writeDirect(t, blankPagePDF(), func(w *Writer) error {
			return w.InsertPages(1, openDirect(t, signed), 2)
		})
		if got := pageContent(ctx, t, out, 2); !strings.Contains(got, " Do") {
			t.Errorf("the inserted page does not draw the signed signature it leaves behind: %q", got)
		}
		annots, err := openDirect(t, out).ExtractAnnotations(ctx, 2)
		if err != nil {
			t.Fatalf("ExtractAnnotations: %v", err)
		}
		if len(annots) != 0 {
			t.Errorf("the inserted page keeps the signed widget %+v", annots)
		}
	})

	t.Run("signed signature drawn on a rewrite", func(t *testing.T) {
		t.Parallel()
		w := NewWriter(openDirect(t, signed))
		defer w.Close()
		w.Encrypt(EncryptConfig{OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1})
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		if dropped := w.DroppedSignatures(); len(dropped) != 1 {
			t.Errorf("the rewrite reports %d dropped signatures, want 1", len(dropped))
		}
		if got := pageContent(ctx, t, out.Bytes(), 2); !strings.Contains(got, " Do") {
			t.Errorf("page 2 does not draw the signed signature the rewrite drops: %q", got)
		}
		annots, err := openDirect(t, out.Bytes()).ExtractAnnotations(ctx, 2)
		if err != nil {
			t.Fatalf("ExtractAnnotations: %v", err)
		}
		if len(annots) != 0 {
			t.Errorf("page 2 keeps the signed widget %+v on a rewrite", annots)
		}
	})

	t.Run("remove an attachment", func(t *testing.T) {
		t.Parallel()
		data := directPagesPDF("/Names << /EmbeddedFiles << /Names [(a.txt) 11 0 R] >> >>", "", "/Annots [10 0 R]",
			"<< /Type /Annot /Subtype /FileAttachment /Rect [20 20 40 40] /F 4 /FS 11 0 R /Contents (a.txt) >>",
			"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 12 0 R >> >>",
			"<< /Type /EmbeddedFile /Length 5 >>\nstream\nhello\nendstream")
		out := writeDirect(t, data, func(w *Writer) error {
			w.RemoveAttachment("a.txt")
			return nil
		})
		annots, err := openDirect(t, out).ExtractAnnotations(ctx, 2)
		if err != nil {
			t.Fatalf("ExtractAnnotations: %v", err)
		}
		for _, a := range annots {
			if a.Subtype == "FileAttachment" {
				t.Errorf("page 2 keeps the attachment annotation %+v", a)
			}
		}
	})
}

// TestWriter_RemediationRepairsAPageWrittenDirectlyInKids pins that a PDF/A
// remediation lands its page repairs on pages written directly in /Kids: the
// JavaScript action of a page /AA and the print flag of an annotation.
func TestWriter_RemediationRepairsAPageWrittenDirectlyInKids(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := directPagesPDF("", "/AA << /O << /S /JavaScript /JS (app.alert\\(1\\)) >> >>", "/Annots [10 0 R]",
		"<< /Type /Annot /Subtype /Text /Rect [20 20 40 40] /Contents (note) >>")
	w := NewWriter(openDirect(t, data))
	defer w.Close()
	if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r := openDirect(t, out.Bytes())
	pages, err := r.resolvePages(ctx)
	if err != nil {
		t.Fatalf("resolving the pages: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("the output has %d pages, want 3", len(pages))
	}
	if aa, err := document.ResolveDict(ctx, r.doc, pages[0].Dict["AA"]); err == nil && aa["O"] != nil {
		t.Errorf("page 1 keeps its JavaScript /AA action: %v", aa)
	}
	annots, err := document.ResolveArray(ctx, r.doc, pages[1].Dict["Annots"])
	if err != nil || len(annots) != 1 {
		t.Fatalf("page 2 holds /Annots %v (%v), want the note", pages[1].Dict["Annots"], err)
	}
	note, err := document.ResolveDict(ctx, r.doc, annots[0])
	if err != nil {
		t.Fatalf("resolving the note: %v", err)
	}
	if flags, _ := note["F"].(ast.PdfInt); flags&4 == 0 {
		t.Errorf("the note's /F is %v, want the print flag set (ISO 19005-2, 6.3.2)", note["F"])
	}
}

// assertOnePagesNode fails unless data holds one page tree node: the flat tree
// a split, a merge or an insertion writes. A copied page that kept its source
// /Parent pulled the source's nodes in with it, and every page they list.
func assertOnePagesNode(t *testing.T, data []byte) {
	t.Helper()
	ctx := context.Background()
	r := openDirect(t, data)
	nodes := 0
	for objNum := range r.doc.XRefEntries() {
		dict, err := document.ResolveDict(ctx, r.doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || dict == nil {
			continue
		}
		if typ, _ := dict["Type"].(ast.PdfName); typ == "Pages" {
			nodes++
		}
	}
	if nodes != 1 {
		t.Errorf("the output holds %d page tree nodes, want the one it lists its pages in", nodes)
	}
}

// structElementPages returns the object number of every page a structure
// element of r names in /Pg.
func structElementPages(ctx context.Context, t *testing.T, r *Reader) map[int]bool {
	t.Helper()
	catalog, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	root, err := document.ResolveDict(ctx, r.doc, catalog.Dict["StructTreeRoot"])
	if err != nil || root == nil {
		t.Fatalf("the output has no structure tree: %v", err)
	}
	pages := make(map[int]bool)
	seen := make(map[int]bool)
	var walk func(obj ast.PdfObject, depth int)
	walk = func(obj ast.PdfObject, depth int) {
		if depth > 64 {
			return
		}
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return
			}
			seen[ref.ObjectNum] = true
		}
		resolved, err := document.ResolveObject(ctx, r.doc, obj)
		if err != nil {
			return
		}
		switch v := resolved.(type) {
		case ast.PdfArray:
			for _, kid := range v {
				walk(kid, depth+1)
			}
		case ast.PdfDict:
			if pg, ok := v["Pg"].(ast.PdfIndirectRef); ok {
				pages[pg.ObjectNum] = true
			}
			walk(v["K"], depth+1)
		}
	}
	walk(root["K"], 0)
	return pages
}
