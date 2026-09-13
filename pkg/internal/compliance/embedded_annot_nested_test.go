package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCollectFileSpecs_NestedInAnnotation pins that a file specification
// written directly inside an annotation is found wherever it sits, not only at
// the annotation's own /FS. A Screen annotation's Rendition action carries its
// media clip's data at /A/R/C/D (ISO 32000-1 §13.2.3), and ISO 19005-1 §6.1.11
// t1 forbids the /EF key on a file specification wherever it appears.
//
// Five veraPDF test-suite files (PDF_UA-1__7.18.6.2-t01/t02-*) embed a video
// exactly that way; veraPDF reports every one and litho saw only /FS.
func TestCollectFileSpecs_NestedInAnnotation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		// The filespec sits four levels down, inside the Rendition action's
		// media clip, exactly as a Screen annotation carries a video.
		"<< /Type /Annot /Subtype /Screen /Rect [0 0 10 10] /P 3 0 R"+
			" /A << /Type /Action /S /Rendition"+
			" /R << /Type /Rendition /S /MR"+
			" /C << /Type /MediaClip /S /MCD"+
			" /D << /Type /Filespec /F (video.mp4) /UF (video.mp4) /EF << /F 5 0 R >> >>"+
			" >> >> >> >>",
		"<< /Type /EmbeddedFile /Length 4 >>\nstream\ndata\nendstream",
	)

	specs := collectFileSpecs(ctx, doc, catalog)

	var nested *fileSpecEntry
	for i := range specs {
		if specs[i].path == "/A/R/C/D" {
			nested = &specs[i]
			break
		}
	}
	if nested == nil {
		paths := make([]string, 0, len(specs))
		for _, s := range specs {
			paths = append(paths, s.path)
		}
		t.Fatalf("filespec at /A/R/C/D not collected, got paths %v", paths)
	}
	if !nested.hasEF {
		t.Errorf("nested filespec must report hasEF")
	}

	// The whole point: §6.1.11 t1 now fires for it under PDF/A-1.
	got := checkEmbeddedFiles(ctx, doc, catalog, "PDF/A-1b")
	if !hasRule(got, Rule_19005_1_6_1_11_t01) {
		t.Errorf("nested /EF filespec must report §6.1.11 t1, got %v", got)
	}
}

// TestCollectFileSpecs_IndirectNestedNotDuplicated pins the skip on indirect
// values: the cross-reference sweep already visits those, so following them
// from an annotation would report the same dictionary twice.
func TestCollectFileSpecs_IndirectNestedNotDuplicated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Screen /Rect [0 0 10 10] /P 3 0 R /FS 5 0 R >>",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 6 0 R >> >>",
		"<< /Type /EmbeddedFile /Length 4 >>\nstream\ndata\nendstream",
	)

	count := 0
	for _, s := range collectFileSpecs(ctx, doc, catalog) {
		if _, isDict := ast.PdfObject(s.dict).(ast.PdfDict); isDict && s.hasEF {
			count++
		}
	}
	if count != 1 {
		t.Errorf("an indirect filespec must be collected once, got %d", count)
	}
}
