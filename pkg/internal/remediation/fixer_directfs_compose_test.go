package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// annotDirectFSPDF builds a document whose only FileAttachment annotation holds
// its file specification as a direct dictionary, missing both /UF and
// /AFRelationship. Object 4 is the annotation, the object both fixers must
// rewrite.
func annotDirectFSPDF() []byte {
	return buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] " +
			"/FS << /Type /Filespec /F (a.tex) /EF << /F 5 0 R >> >> >>",
		"<< /Type /EmbeddedFile /Subtype /text#2Fplain /Length 5 >>\nstream\nlitho\nendstream",
	})
}

// TestFileSpecFixersComposeOnTheSameAnnotation pins that the /F+/UF fixer and
// the /AFRelationship fixer do not discard each other's work.
//
// Both repair a direct /FS by rewriting the annotation that holds it, and
// filespec-fuf runs first in the registry. The AFRelationship walk resolved the
// annotation straight from the document, so its AllocAt overwrote the pending
// dictionary and dropped the /UF just written. The output was still correct only
// because the fixpoint loop ran a second round and put /UF back, which hid the
// clobber behind a duplicated "filled missing /F or /UF" line.
func TestFileSpecFixersComposeOnTheSameAnnotation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotDirectFSPDF())

	if _, err := fixFileSpecFUF(ctx, alloc, doc, Options{}, nil, "PDF/A-3b"); err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}
	if _, err := fixAFRelationship(ctx, alloc, doc, Options{}, nil, "PDF/A-3b"); err != nil {
		t.Fatalf("fixAFRelationship: %v", err)
	}

	// At PDF/A-3b the /AFRelationship fixer also moves the /FS to an object
	// of its own, so an /AF array can list it; read it wherever it now lives.
	annot := pendingDict(t, alloc, 4)
	fs := liveDictValue(ctx, alloc, doc, annot["FS"])
	if fs == nil {
		t.Fatalf("/FS = %T, want a file specification", annot["FS"])
	}

	// Both repairs must survive in the single pending file specification.
	if _, hasUF := fs["UF"]; !hasUF {
		t.Error("/UF was discarded by the AFRelationship fixer")
	}
	if fs["AFRelationship"] != ast.PdfName("Unspecified") {
		t.Errorf("/AFRelationship = %v, want /Unspecified", fs["AFRelationship"])
	}
	if fs["F"] != ast.PdfString("a.tex") {
		t.Errorf("/F = %v, want (a.tex)", fs["F"])
	}
}

// TestDirectFileSpecFUFFillsUnicodeName checks the /F+/UF repair reaches a file
// specification written inline, which the cross-reference sweep cannot see.
func TestDirectFileSpecFUFFillsUnicodeName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotDirectFSPDF())

	fixes, err := fixFileSpecFUF(ctx, alloc, doc, Options{}, nil, "PDF/A-3b")
	if err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want one repair reported", fixes)
	}

	annot := pendingDict(t, alloc, 4)
	fs, ok := annot["FS"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/FS = %T, want a direct dictionary", annot["FS"])
	}
	if fs["UF"] != ast.PdfString("a.tex") {
		t.Errorf("/UF = %v, want (a.tex) derived from /F", fs["UF"])
	}
	// The annotation's own entries must survive the rewrite.
	if annot["Subtype"] != ast.PdfName("FileAttachment") {
		t.Errorf("/Subtype = %v, want /FileAttachment", annot["Subtype"])
	}
}

// TestDirectFileSpecFUFIsIdempotent checks a second run reports nothing, so the
// fixpoint loop terminates instead of rewriting the annotation every round.
func TestDirectFileSpecFUFIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotDirectFSPDF())

	if _, err := fixFileSpecFUF(ctx, alloc, doc, Options{}, nil, "PDF/A-3b"); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	fixes, err := fixFileSpecFUF(ctx, alloc, doc, Options{}, nil, "PDF/A-3b")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("second pass reported %v, want no repair", fixes)
	}
}
