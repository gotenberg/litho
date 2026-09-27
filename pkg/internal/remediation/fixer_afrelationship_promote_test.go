package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// revalidatePDFA writes the document with the queued repairs applied, reads
// the result back and returns its violations against profile.
func revalidatePDFA(t *testing.T, doc *document.Document, alloc *writer.ObjAllocator, profile models.PDFAProfile) ([]compliance.Violation, *document.Document) {
	t.Helper()

	ctx := context.Background()
	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	errs, err := compliance.ValidatePDFA(ctx, reread, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA on the output: %v", err)
	}
	return toViolations(errs), reread
}

// firstPageAnnot returns the i-th annotation of the first page of doc.
func firstPageAnnot(t *testing.T, doc *document.Document, i int) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
	}
	annots, err := document.ResolveArray(ctx, doc, pages[0].Dict["Annots"])
	if err != nil || len(annots) <= i {
		t.Fatalf("the first page holds %d annotations (%v), want more than %d", len(annots), err, i)
	}
	annot, err := document.ResolveDict(ctx, doc, annots[i])
	if err != nil {
		t.Fatalf("resolving annotation %d: %v", i, err)
	}
	return annot
}

// failures returns the violations of rule among vs.
func failures(vs []compliance.Violation, rule compliance.Rule) []compliance.Violation {
	var out []compliance.Violation
	for _, v := range vs {
		if v.Rule == rule {
			out = append(out, v)
		}
	}
	return out
}

// inlineFileSpecsPDF builds a PDF/A-3 candidate whose page carries a
// FileAttachment annotation with its /FS written inline, a Link whose /GoToR
// action writes its /F inline, and whose /Names /EmbeddedFiles tree names a
// file specification typed /F. annotExtra is added to the FileAttachment
// annotation, which has no /F flags and no /AP of its own; object 7 is a form
// it may name as its appearance.
func inlineFileSpecsPDF(annotExtra string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(x.pdf) 6 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Annots [4 0 R 5 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [10 10 30 30]"+annotExtra+
			" /FS << /Type /Filespec /F (b.pdf) /UF (b.pdf) >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [40 10 60 30] /F 4 /Border [0 0 0]"+
			" /A << /S /GoToR /D [0 /Fit] /F << /Type /Filespec /F (c.pdf) /UF (c.pdf) /AFRelationship /Unspecified >> >> >>",
		"<< /Type /F /F (x.pdf) /UF (x.pdf) >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length 0 >>\nstream\n\nendstream",
	)
}

// TestFixAFRelationship_AssociatesInlineAndNameTreeFileSpecs pins that a
// PDF/A-3 remediation associates every file specification ISO 19005-3, 6.8 t4
// judges, including the ones it could not list in an /AF array.
//
// An /AF array lists indirect objects (ISO 32000-2, 14.13), so a file
// specification written inline in an annotation, its /FS or an action's /F,
// has no number an entry could name; the repair left both failing. The
// /EmbeddedFiles tree names a file specification whatever its /Type (ISO
// 32000-1, 7.11.4), and the repair listed only the dictionaries typed
// /Filespec. Each inline one now moves to an object of its own, which
// ISO 32000-1, 7.3.10 makes the same value, and every one is listed.
func TestFixAFRelationship_AssociatesInlineAndNameTreeFileSpecs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineFileSpecsPDF(" /F 4 /AP << /N 7 0 R >>"))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA3b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	if n := len(failures(toViolations(errs), compliance.Rule_19005_3_6_8_t04)); n != 3 {
		t.Fatalf("control: the source fails 6.8 t4 %d times, want 3", n)
	}

	if _, err := fixAFRelationship(ctx, alloc, doc, Options{}, nil, string(models.PDFA3b)); err != nil {
		t.Fatalf("fixAFRelationship: %v", err)
	}

	after, reread := revalidatePDFA(t, doc, alloc, models.PDFA3b)
	for _, rule := range []compliance.Rule{compliance.Rule_19005_3_6_8_t03, compliance.Rule_19005_3_6_8_t04} {
		for _, v := range failures(after, rule) {
			t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
		}
	}
	annot := firstPageAnnot(t, reread, 0)
	if _, ok := annot["FS"].(ast.PdfIndirectRef); !ok {
		t.Errorf("the annotation's /FS is %T, want an indirect reference", annot["FS"])
	}
	if fs, _ := document.ResolveDict(ctx, reread, annot["FS"]); fs["UF"] == nil || fs["AFRelationship"] == nil {
		t.Errorf("the promoted /FS lost an entry or gained no /AFRelationship: %v", fs)
	}
}

// TestRemediatePDFA_InlineFileSpecKeepsTheAnnotationPeersWrites pins that
// promoting an inline file specification builds on the annotation the peers
// ahead of af-relationship rewrote.
//
// annot-flags sets the print flag the FileAttachment lacks (ISO 19005-3,
// 6.3.2) and annot-appearance gives it the normal appearance it lacks (6.3.3),
// each by writing the annotation, the object the promotion writes too. A
// promotion that read the parsed annotation would put both back.
func TestRemediatePDFA_InlineFileSpecKeepsTheAnnotationPeersWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineFileSpecsPDF(""))
	if _, err := RemediatePDFA(ctx, doc, alloc, models.PDFA3b, Options{}); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}

	after, reread := revalidatePDFA(t, doc, alloc, models.PDFA3b)
	for _, v := range after {
		switch v.Rule.Clause {
		case "6.3.2", "6.3.3", "6.8":
			t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
		}
	}
	annot := firstPageAnnot(t, reread, 0)
	if annot["AP"] == nil || annot["F"] == nil {
		t.Errorf("the annotation lost a peer's write: /F %v, /AP %v", annot["F"], annot["AP"])
	}
	if _, ok := annot["FS"].(ast.PdfIndirectRef); !ok {
		t.Errorf("the annotation's /FS is %T, want an indirect reference", annot["FS"])
	}
}
