package pdf

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// insertPageBytes inserts sourcePage of source at pos of target and returns the
// written output.
func insertPageBytes(ctx context.Context, t *testing.T, target, source []byte, pos, sourcePage int) []byte {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(target), int64(len(target)))
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	defer r.Close()
	src, err := Open(ctx, bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	defer src.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := w.InsertPages(pos, src, sourcePage); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// formRootNames returns the partial name of every root field the output's
// /AcroForm lists, and fails the test when a widget on a page belongs to a
// field the form does not reach.
func formRootNames(ctx context.Context, t *testing.T, data []byte) []string {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	doc := r.doc
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	acroForm, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"])
	if err != nil || acroForm == nil {
		t.Fatalf("the output has no /AcroForm (err %v)", err)
	}
	roots, err := document.ResolveArray(ctx, doc, acroForm["Fields"])
	if err != nil {
		t.Fatalf("resolving /Fields: %v", err)
	}
	listed := make(map[int]bool, len(roots))
	var names []string
	for _, root := range roots {
		ref, ok := root.(ast.PdfIndirectRef)
		if !ok {
			t.Fatalf("/Fields entry %v is not a reference", root)
		}
		listed[ref.ObjectNum] = true
		dict, err := document.ResolveDict(ctx, doc, ref)
		if err != nil {
			t.Fatalf("resolving root field %d: %v", ref.ObjectNum, err)
		}
		name, _ := document.ResolveString(ctx, doc, dict["T"])
		names = append(names, document.DecodePdfString(name))
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	for i, page := range pages {
		annots, _ := document.ResolveArray(ctx, doc, page.Dict["Annots"])
		for _, annot := range annots {
			ref, ok := annot.(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			root := ref
			for range maxFieldParentDepthForTest {
				dict, err := document.ResolveDict(ctx, doc, root)
				if err != nil {
					t.Fatalf("resolving field %d: %v", root.ObjectNum, err)
				}
				parent, hasParent := dict["Parent"].(ast.PdfIndirectRef)
				if !hasParent {
					break
				}
				root = parent
			}
			if !listed[root.ObjectNum] {
				t.Errorf("page %d: widget %d belongs to field %d, which /AcroForm /Fields does not list",
					i+1, ref.ObjectNum, root.ObjectNum)
			}
		}
	}
	slices.Sort(names)
	return names
}

// maxFieldParentDepthForTest bounds the test's own /Parent ascent.
const maxFieldParentDepthForTest = 32

// TestWriter_InsertPages_ListsCopiedFields pins that a page inserted with its
// widgets brings their fields into the target's interactive form.
//
// ISO 32000-1, 12.7.2 has /AcroForm /Fields list the document's root fields,
// and a widget whose field it does not reach is outside the form: qpdf reports
// it as "not reachable from /AcroForm". Two root fields cannot share a fully
// qualified name (12.7.3.2), so the copy of a field whose name the target
// already uses is renamed.
//
// # Corpus evidence
//
//   - litho: 9 fixtures carried a widget the form did not reach after a page
//     move, each reported by qpdf.
func TestWriter_InsertPages_ListsCopiedFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := formPDF()
	out := insertPageBytes(ctx, t, data, data, 1, 1)

	got := formRootNames(ctx, t, out)
	want := []string{"email", "email_2", "name", "name_2"}
	if !slices.Equal(got, want) {
		t.Errorf("root fields = %v, want %v", got, want)
	}
}

// TestWriter_InsertPages_ListsTheTargetsUnlistedFields pins that a target whose
// pages present widget fields with no /AcroForm keeps them inside the form the
// insertion creates, rather than falling outside it.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: presents 27 widget fields with no /AcroForm, and a
//     form listing only the copied fields left the rest unreachable.
func TestWriter_InsertPages_ListsTheTargetsUnlistedFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	target := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (own) /V (x) /Rect [50 700 150 720] /F 4 >>",
	})
	source := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /V (Paris) /Rect [50 700 150 720] /F 4 >>",
	})
	out := insertPageBytes(ctx, t, target, source, 1, 1)

	if got := formRootNames(ctx, t, out); !slices.Equal(got, []string{"city", "own"}) {
		t.Errorf("root fields = %v, want [city own]", got)
	}
}

// TestWriter_InsertPages_CreatesTheForm pins that inserting a page with a field
// into a document with no interactive form gives it one, carrying the source
// form's default appearance so the field can still be drawn (ISO 32000-1,
// 12.7.3.3).
func TestWriter_InsertPages_CreatesTheForm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	target := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	})
	source := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /DA (/Helv 9 Tf 0 g) " +
			"/DR << /Font << /Helv 5 0 R >> >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /V (Paris) /Rect [50 700 150 720] /F 4 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
	out := insertPageBytes(ctx, t, target, source, 1, 1)

	if got := formRootNames(ctx, t, out); !slices.Equal(got, []string{"city"}) {
		t.Errorf("root fields = %v, want [city]", got)
	}

	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	catalog, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	acroForm, err := document.ResolveDict(ctx, r.doc, catalog.Dict["AcroForm"])
	if err != nil || acroForm == nil {
		t.Fatalf("no /AcroForm: %v", err)
	}
	if _, ok := acroForm["DA"]; !ok {
		t.Error("the new /AcroForm lost the source form's /DA")
	}
	if _, ok := acroForm["DR"]; !ok {
		t.Error("the new /AcroForm lost the source form's /DR")
	}
}
