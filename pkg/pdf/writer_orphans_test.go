package pdf

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// unreferencedInUse returns the objects the cross-reference index of data
// lists as in use that nothing reached from its trailer references, ascending.
func unreferencedInUse(t *testing.T, data []byte) []int {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	doc := r.doc

	reached := make(map[int]bool)
	var walk func(obj ast.PdfObject)
	walk = func(obj ast.PdfObject) {
		switch v := obj.(type) {
		case ast.PdfIndirectRef:
			if reached[v.ObjectNum] {
				return
			}
			reached[v.ObjectNum] = true
			resolved, err := doc.Resolve(ctx, v)
			if err != nil {
				return
			}
			walk(resolved)
		case ast.PdfArray:
			for _, e := range v {
				walk(e)
			}
		case ast.PdfDict:
			for _, e := range v {
				walk(e)
			}
		case ast.PdfStream:
			walk(v.Dict)
		}
	}
	walk(doc.Index.Trailer)

	var orphans []int
	for num, entry := range doc.XRefEntries() {
		if num == 0 || entry.Free || reached[num] {
			continue
		}
		obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil {
			continue
		}
		if s, ok := obj.(ast.PdfStream); ok {
			if typ, _ := s.Dict["Type"].(ast.PdfName); typ == "XRef" || typ == "ObjStm" {
				continue
			}
		}
		orphans = append(orphans, num)
	}
	return orphans
}

// TestRemediatePDFAFreesTheProfileItUnreferences pins that an incremental
// remediation deletes what its repairs leave nothing referencing.
// twoprofile_1b names two destination profiles, and unifying the intents on
// the PDF/A one (ISO 19005-1, 6.2.2) drops the last reference to the other. At
// PDF/A-2b the new XMP at a new number does the same to the old metadata
// stream. threedcs_4 repairs the ICC profile its 3D stream's colour space
// names, then replaces that colour space, and wrote the repaired profile
// although nothing named it. The update kept listing each as in use; it now
// writes none of them and gives each a free entry (ISO 32000-1, 7.5.6), and
// keeps the source bytes it appends to.
func TestRemediatePDFAFreesTheProfileItUnreferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		fixture  string
		profiles []models.PDFAProfile
		dropped  string
	}{
		{"twoprofile_1b", []models.PDFAProfile{models.PDFA1b, models.PDFA2b}, ""},
		{"threedcs_4", []models.PDFAProfile{models.PDFA1b}, "\n34 0 obj"},
	}
	for _, tc := range cases {
		data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/" + tc.fixture + ".pdf")
		if err != nil {
			t.Fatalf("reading the fixture: %v", err)
		}
		if got := unreferencedInUse(t, data); len(got) != 0 {
			t.Fatalf("%s already lists unreferenced objects %v", tc.fixture, got)
		}
		for _, profile := range tc.profiles {
			t.Run(tc.fixture+"/"+string(profile), func(t *testing.T) {
				t.Parallel()
				r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				w := NewWriter(r)
				defer w.Close()
				if _, err := w.RemediatePDFA(ctx, profile); err != nil {
					t.Fatalf("RemediatePDFA: %v", err)
				}
				var out bytes.Buffer
				if err := w.WriteTo(ctx, &out); err != nil {
					t.Fatalf("WriteTo: %v", err)
				}
				if !bytes.HasPrefix(out.Bytes(), data) {
					t.Fatal("the remediation rewrote the file, so the incremental path never ran")
				}
				if got := unreferencedInUse(t, out.Bytes()); len(got) != 0 {
					t.Errorf("the output lists unreferenced objects %v as in use", got)
				}
				if tc.dropped != "" && bytes.Contains(out.Bytes()[len(data):], []byte(tc.dropped)) {
					t.Errorf("the update writes %q, which nothing references", tc.dropped[1:])
				}
			})
		}
	}
}

// TestRemediatePDFUAFreesWhatAPromotedPageDropped pins that the objects a
// page spelled out directly in a /Kids array referenced count as the source's:
// kidsarray_2b writes two pages inside its /Kids array, which the update
// numbers as objects of their own (ISO 32000-1, 7.7.3.2), and the PDF/UA-1
// remediation gives each new content. Their old content streams, which only
// the array named, were listed as in use.
func TestRemediatePDFUAFreesWhatAPromotedPageDropped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/kidsarray_2b.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	if got := unreferencedInUse(t, data); len(got) != 0 {
		t.Fatalf("the fixture already lists unreferenced objects %v", got)
	}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.RemediatePDFUA(ctx, models.PDFUA1); err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), data) {
		t.Fatal("the remediation rewrote the file, so the incremental path never ran")
	}
	if got := unreferencedInUse(t, out.Bytes()); len(got) != 0 {
		t.Errorf("the output lists unreferenced objects %v as in use", got)
	}
}

// attachmentPDF builds a document embedding one file, a.txt, through its
// /EmbeddedFiles name tree.
func attachmentPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a.txt) 4 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 5 0 R >> >>",
		"<< /Type /EmbeddedFile /Length 5 >>\nstream\nhello\nendstream",
	})
}

// removeAttachment removes a.txt from src through an incremental update.
func removeAttachment(t *testing.T, src []byte) []byte {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	w.RemoveAttachment("a.txt")
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), src) {
		t.Fatal("the edit rewrote the file, so the incremental path never ran")
	}
	return out.Bytes()
}

// TestIncrementalUpdateFreesWhatTheEditUnreferences pins that an update
// deleting the last reference to objects gives each a free entry, with the
// next generation (ISO 32000-1, 7.5.4), and that the objects it leaves
// referenced, and an edit unreferencing nothing, get none.
func TestIncrementalUpdateFreesWhatTheEditUnreferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := attachmentPDF()
	out := removeAttachment(t, src)
	if got := unreferencedInUse(t, out); len(got) != 0 {
		t.Errorf("the output lists unreferenced objects %v as in use", got)
	}

	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	var freed []int
	for num, entry := range r.doc.XRefEntries() {
		if num != 0 && entry.Free {
			freed = append(freed, num)
			if entry.Generation != 1 {
				t.Errorf("object %d is freed at generation %d, want 1", num, entry.Generation)
			}
		}
	}
	if !slices.Equal(freed, []int{4, 5}) {
		t.Errorf("freed objects = %v, want the filespec and its stream [4 5]", freed)
	}
	pages, err := r.PageCount(ctx)
	if err != nil || pages != 1 {
		t.Errorf("PageCount = %d, %v; want 1", pages, err)
	}

	// Rotating a page drops no reference, so the update frees nothing and
	// its cross-reference section lists the page alone.
	rr, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rr.Close()
	w := NewWriter(rr)
	defer w.Close()
	if err := w.RotatePages(90, 1); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	var rotated bytes.Buffer
	if err := w.WriteTo(ctx, &rotated); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	tail := rotated.Bytes()[len(src):]
	if bytes.Contains(tail, []byte(" f\r\n")) && !bytes.Contains(tail, []byte("0000000000 65535 f\r\n")) {
		t.Errorf("an edit unreferencing nothing wrote a free entry:\n%s", tail)
	}
	if n := bytes.Count(tail, []byte(" f\r\n")); n > 1 {
		t.Errorf("an edit unreferencing nothing wrote %d free entries:\n%s", n, tail)
	}
}

// TestIncrementalUpdateOfASignedSourceFreesNothing pins that an update of a
// signed document deletes no object of the revisions its signature covers,
// even one the edit leaves unreferenced: that is a change of the signed
// document a certification signature's permissions judge (ISO 32000-1,
// 12.8.2.2), and the signature still verifies.
func TestIncrementalUpdateOfASignedSourceFreesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pki := newTestPKI(t, key)
	signer, err := NewSigner(pki.leaf, key)
	if err != nil {
		t.Fatal(err)
	}
	src := signWith(t, attachmentPDF(), signer)
	out := removeAttachment(t, src)

	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	for _, num := range []int{4, 5} {
		if !r.doc.DefinesObject(num) {
			t.Errorf("the update of a signed source freed object %d", num)
		}
	}
	results, err := r.VerifySignatures(ctx, pki.roots())
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("the output carries no signature")
	}
	for _, v := range results {
		if !v.HashValid || !v.CertChainValid {
			t.Errorf("signature %q no longer verifies: %q", v.FieldName, v.ErrorMessage)
		}
	}
}
