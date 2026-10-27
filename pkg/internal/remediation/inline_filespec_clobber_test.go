package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// inlineSpecClobberPDF builds a page, object 3, whose /Annots holds a Link of
// its own, object 4, and a Link written inline, and an image XObject, object
// 6, each listing in /AF a file specification written inline with /F alone,
// no /AFRelationship and the embedded file 5, which is no PDF.
func inlineSpecClobberPDF() []byte {
	const spec = "<< /Type /Filespec /F (a.txt) /EF << /F 5 0 R >> >>"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im 6 0 R >> >>"+
			" /Annots [4 0 R << /Type /Annot /Subtype /Link /Rect [0 0 10 10] /AF ["+spec+"] >>] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /AF ["+spec+"] >>",
		"<< /Type /EmbeddedFile /Subtype /text#2Fplain /Length 3 >>\nstream\nabc\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray"+
			" /BitsPerComponent 8 /AF ["+spec+"] /Length 1 >>\nstream\n\x00\nendstream",
	)
}

// TestInlineFileSpecRepairs_KeepEarlierPendingWrites pins that every repair of
// a file specification written inline writes its container back from the
// allocator's view: the page, the annotation and the image XObject hold an
// earlier fixer's pending rewrite, marked here by /NM on each dictionary and a
// new body on the image, and each must survive the repair, which must happen
// too. A repair built on the parsed document would put back what the earlier
// fixer removed or changed, as the annotation and resource fixers that run
// ahead of these once did.
func TestInlineFileSpecRepairs_KeepEarlierPendingWrites(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		run  func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error
		// repaired reports whether the specification at the page's
		// /Annots[1]/AF[0], the annotation's /AF[0] or the image's /AF[0]
		// carries the repair.
		repaired func(spec ast.PdfObject) bool
	}{
		{
			name: "filespec-fuf",
			run: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixFileSpecFUF(ctx, alloc, doc, Options{}, nil, "")
				return err
			},
			repaired: func(spec ast.PdfObject) bool { _, ok := spec.(ast.PdfDict)["UF"]; return ok },
		},
		{
			name: "afrelationship at PDF/A-4",
			run: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixAFRelationship(ctx, alloc, doc, Options{}, nil, string(models.PDFA4))
				return err
			},
			repaired: func(spec ast.PdfObject) bool { _, ok := spec.(ast.PdfDict)["AFRelationship"]; return ok },
		},
		{
			name: "afrelationship at PDF/A-3b",
			run: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixAFRelationship(ctx, alloc, doc, Options{}, nil, string(models.PDFA3b))
				return err
			},
			repaired: func(spec ast.PdfObject) bool { _, ok := spec.(ast.PdfIndirectRef); return ok },
		},
		{
			name: "pdfa1 embedded files",
			run: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := stripEFFromInlineFileSpecs(ctx, alloc, doc)
				return err
			},
			repaired: func(spec ast.PdfObject) bool { _, ok := spec.(ast.PdfDict)["EF"]; return !ok },
		},
		{
			name: "embedded compliance",
			run: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
				var vs []compliance.Violation
				for _, label := range []string{"object 3 /Annots[1]/AF[0]", "object 4 /AF[0]", "object 6 /AF[0]"} {
					vs = append(vs, compliance.Violation{
						Rule:    compliance.Rule_19005_4_6_9_t03,
						Context: label + ": embedded file is not a PDF document",
					})
				}
				_, err := fixEmbeddedCompliance(ctx, alloc, doc, Options{}, vs, string(models.PDFA4))
				return err
			},
			repaired: func(spec ast.PdfObject) bool { _, ok := spec.(ast.PdfDict)["EF"]; return !ok },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, inlineSpecClobberPDF())
			for _, objNum := range []int{3, 4} {
				d := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
				marked := make(ast.PdfDict, len(d)+1)
				maps.Copy(marked, d)
				marked["NM"] = ast.PdfString("pending")
				alloc.AllocAt(objNum, marked)
			}
			img, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 6})
			if !ok {
				t.Fatal("precondition: object 6 is no stream")
			}
			imgDict := make(ast.PdfDict, len(img.Dict)+1)
			maps.Copy(imgDict, img.Dict)
			imgDict["NM"] = ast.PdfString("pending")
			alloc.AllocAt(6, ast.NewStream(imgDict, []byte{0xff}))

			if err := tc.run(ctx, alloc, doc); err != nil {
				t.Fatalf("repair: %v", err)
			}

			page := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 3})
			annot := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 4})
			img, _ = liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 6})
			for name, d := range map[string]ast.PdfDict{"page": page, "annotation": annot, "image": img.Dict} {
				if d["NM"] != ast.PdfString("pending") {
					t.Errorf("the %s lost the earlier fixer's pending /NM", name)
				}
			}
			if body, err := img.Bytes(ctx); err != nil || len(body) != 1 || body[0] != 0xff {
				t.Errorf("the image lost the earlier fixer's pending body: %v, %v", body, err)
			}

			inlineAnnot, _ := page["Annots"].(ast.PdfArray)[1].(ast.PdfDict)
			for where, af := range map[string]ast.PdfObject{
				"inline annotation": inlineAnnot["AF"], "annotation": annot["AF"], "image": img.Dict["AF"],
			} {
				arr, _ := af.(ast.PdfArray)
				if len(arr) != 1 || !tc.repaired(arr[0]) {
					t.Errorf("the %s's inline file specification was not repaired: %v", where, af)
				}
			}
		})
	}
}
