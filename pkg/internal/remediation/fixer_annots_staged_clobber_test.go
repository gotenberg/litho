package remediation

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// laterAnnotsFixer is a fixer that reads a page's /Annots after the annotations
// fixer, with the signature every fixer shares.
type laterAnnotsFixer func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, profile string) ([]string, error)

// TestAnnotsReadersKeepTheAnnotationsFixerRemovals is the clobber test for
// every fixer that reads a page's /Annots after the annotations fixer.
//
// The annotations fixer removes a forbidden annotation from an indirect /Annots
// array by writing the filtered array at the array's own object number, and
// nulls the annotation object. A later fixer that read the array as the file
// wrote it found the removed annotation there, read it past its pending null,
// and wrote it back with its own repair: the annotation came back as an orphan
// object, or back into /Annots for the fixer that rewrites the array. Each case
// pins that the page lists the kept annotation alone, that the removed one stays
// the null object, and that the later fixer did run.
func TestAnnotsReadersKeepTheAnnotationsFixerRemovals(t *testing.T) {
	t.Parallel()

	const (
		page    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots 4 0 R >>"
		square  = "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 >>"
		embFile = "<< /Type /EmbeddedFile /Length 1 >>\nstream\nx\nendstream"
	)

	tests := []struct {
		name    string
		profile string
		// objs are objects 5 on: 5 is the annotation the annotations fixer
		// removes and 6 the one it keeps.
		objs  []string
		later laterAnnotsFixer
		// ran fails the test unless the later fixer repaired what it should,
		// so a pass cannot come from a fixer that never reached the page.
		ran func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document)
	}{
		{
			name:    "annot-opacity",
			profile: "PDF/A-1b",
			objs: []string{
				"<< /Type /Annot /Subtype /Polygon /Rect [0 0 10 10] /F 4 /CA 0.5 /Vertices [0 0 10 0 5 5] >>",
				"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /CA 0.5 >>",
			},
			later: fixAnnotOpacity,
			ran: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				t.Helper()
				if _, has := resolveFromAllocOrDoc(context.Background(), alloc, doc, ast.PdfIndirectRef{ObjectNum: 6})["CA"]; has {
					t.Error("the kept annotation keeps its /CA: the later fixer never ran, so this proves nothing")
				}
			},
		},
		{
			name:    "annot-appearance",
			profile: "PDF/A-2b",
			objs: []string{
				"<< /Type /Annot /Subtype /Screen /Rect [0 0 10 10] /F 4 >>",
				square,
			},
			later: fixAnnotAppearance,
			ran: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				t.Helper()
				if _, has := resolveFromAllocOrDoc(context.Background(), alloc, doc, ast.PdfIndirectRef{ObjectNum: 6})["AP"]; !has {
					t.Error("the kept annotation has no /AP: the later fixer never ran, so this proves nothing")
				}
			},
		},
		{
			name:    "annot-artifact",
			profile: "PDF/A-2b",
			objs: []string{
				"<< /Type /Annot /Subtype /Screen /Rect [0 0 10 10] /F 32 /StructParent 0 >>",
				"<< /Type /Annot /Subtype /Popup /Rect [0 0 10 10] /F 4 /StructParent 1 >>",
			},
			later: fixAnnotArtifact,
			ran: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				t.Helper()
				if _, has := resolveFromAllocOrDoc(context.Background(), alloc, doc, ast.PdfIndirectRef{ObjectNum: 6})["StructParent"]; has {
					t.Error("the kept pop-up keeps its /StructParent: the later fixer never ran, so this proves nothing")
				}
			},
		},
		{
			name:    "af-relationship",
			profile: "PDF/A-4",
			objs: []string{
				"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 10 10] /F 4" +
					" /FS << /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 7 0 R >> >> >>",
				square,
				embFile,
				"<< /Type /Filespec /F (b.txt) /UF (b.txt) /EF << /F 7 0 R >> >>",
			},
			later: fixAFRelationship,
			ran: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				t.Helper()
				if _, has := resolveFromAllocOrDoc(context.Background(), alloc, doc, ast.PdfIndirectRef{ObjectNum: 8})["AFRelationship"]; !has {
					t.Error("file specification 8 has no /AFRelationship: the later fixer never ran, so this proves nothing")
				}
			},
		},
		{
			name:    "forbidden-annots",
			profile: "PDF/A-2b",
			objs: []string{
				"<< /Type /Annot /Subtype /Screen /Rect [0 0 10 10] /F 4 >>",
				square,
				"<< /Type /Annot /Subtype /TrapNet /Rect [0 0 10 10] /F 4 >>",
			},
			later: fixForbiddenAnnots,
			ran: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				t.Helper()
				if slices.Contains(liveAnnotNums(t, alloc, doc, 3), 7) {
					t.Error("the page keeps its TrapNet annotation: the later fixer never ran, so this proves nothing")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			annots := "[5 0 R 6 0 R]"
			if tc.name == "forbidden-annots" {
				annots = "[5 0 R 6 0 R 7 0 R]"
			}
			objs := append([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				page,
				annots,
			}, tc.objs...)
			doc, alloc := newTestDoc(t, buildSimplePDF(objs))

			if _, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, tc.profile); err != nil {
				t.Fatalf("fixAnnotations: %v", err)
			}
			assertNulled(t, alloc, 5)
			if _, err := tc.later(ctx, alloc, doc, Options{}, nil, tc.profile); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			if got, want := liveAnnotNums(t, alloc, doc, 3), []int{6}; !slices.Equal(got, want) {
				t.Errorf("/Annots = %v, want %v", got, want)
			}
			assertNulled(t, alloc, 5)
			tc.ran(t, alloc, doc)
		})
	}
}

// TestAnnotFlagsRemovalLeavesNoStaleAnnotsArray pins where annot-flags writes
// the /Annots array it removed an annotation from: at the array's own object
// number when the page names one, the object the page and the file's
// cross-reference table both name.
//
// It used to write the filtered array into the page and leave the array object
// as the file wrote it. Nothing names that object any more, but it stays in the
// output: an incremental update supersedes only the objects it rewrites, and a
// fixer that sweeps every object, filespec-fuf among them, read it as the file
// wrote it and wrote it back with a repair of the removed annotation's inline
// file specification. The removed annotation came back as part of an orphan.
func TestAnnotFlagsRemovalLeavesNoStaleAnnotsArray(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots 4 0 R >>",
		"[<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 10 10] /F 2" +
			" /FS << /Type /Filespec /F (a.txt) /EF << /F 6 0 R >> >> >> 5 0 R]",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 >>",
		"<< /Type /EmbeddedFile /Length 1 >>\nstream\nx\nendstream",
		"<< /Type /Filespec /F (b.txt) /EF << /F 6 0 R >> >>",
	}))

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if _, err := fixFileSpecFUF(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}

	if got, want := liveAnnotNums(t, alloc, doc, 3), []int{5}; !slices.Equal(got, want) {
		t.Errorf("/Annots = %v, want %v", got, want)
	}
	array := liveArray(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if !slices.Equal(array, ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}}) {
		t.Errorf("object 4 = %v, want [5 0 R]: the removed annotation stays in the array object the file names", array)
	}
	if _, has := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 7})["UF"]; !has {
		t.Error("file specification 7 has no /UF: filespec-fuf never ran, so this proves nothing")
	}
}
