package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/models"
)

// terminalCalls names the Writer methods a remediated Writer still accepts.
// DroppedSignatures edits nothing: it reports on the output WriteTo wrote.
var terminalCalls = []string{"Sign", "WriteTo", "Close", "DroppedSignatures"}

// refusedCall is one Writer method a remediated Writer refuses, called the way
// a caller would call it after the remediation.
type refusedCall struct {
	name string
	call func(ctx context.Context, t *testing.T, w *Writer) error
}

// refusedCalls returns one call of every Writer method but [terminalCalls].
// The methods that return no error report nil here, and their refusal is read
// off [Writer.WriteTo].
func refusedCalls(t *testing.T) []refusedCall {
	t.Helper()

	pngData := buildPNG(t, false)
	openSource := func(ctx context.Context, t *testing.T) *Reader {
		t.Helper()
		data := signablePDF()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() {
			if err := r.Close(); err != nil {
				t.Errorf("closing the source: %v", err)
			}
		})
		return r
	}
	return []refusedCall{
		{"SetMetadata", func(_ context.Context, _ *testing.T, w *Writer) error {
			w.SetMetadata(Metadata{Title: "late"})
			return nil
		}},
		{"ClearMetadata", func(_ context.Context, _ *testing.T, w *Writer) error { w.ClearMetadata(); return nil }},
		{"RotatePages", func(_ context.Context, _ *testing.T, w *Writer) error { return w.RotatePages(90) }},
		{"DeletePages", func(_ context.Context, _ *testing.T, w *Writer) error { return w.DeletePages(1) }},
		{"InsertPages", func(ctx context.Context, t *testing.T, w *Writer) error {
			return w.InsertPages(0, openSource(ctx, t))
		}},
		{"SetBookmarks", func(_ context.Context, _ *testing.T, w *Writer) error {
			w.SetBookmarks([]models.Bookmark{{Title: "late", PageNumber: 1}})
			return nil
		}},
		{"GenerateBookmarks", func(_ context.Context, _ *testing.T, w *Writer) error {
			w.GenerateBookmarks(BookmarkGenerationOptions{}, BookmarkReplace)
			return nil
		}},
		{"AutoTag", func(_ context.Context, _ *testing.T, w *Writer) error { w.AutoTag(); return nil }},
		{"Encrypt", func(_ context.Context, _ *testing.T, w *Writer) error {
			w.Encrypt(EncryptConfig{OwnerPassword: "o", Algorithm: EncryptAES256, Permissions: -1})
			return nil
		}},
		{"RemoveEncryption", func(_ context.Context, _ *testing.T, w *Writer) error { w.RemoveEncryption(); return nil }},
		{"SetFormField", func(_ context.Context, _ *testing.T, w *Writer) error {
			w.SetFormField("name", "late")
			return nil
		}},
		{"FlattenForm", func(_ context.Context, _ *testing.T, w *Writer) error { w.FlattenForm(); return nil }},
		{"StampText", func(_ context.Context, _ *testing.T, w *Writer) error {
			return w.StampText(TextStamp{Text: "LATE", X: 50, Y: 50})
		}},
		{"StampImage", func(_ context.Context, _ *testing.T, w *Writer) error {
			return w.StampImage(ImageStamp{Data: pngData, MimeType: "image/png", X: 10, Y: 10, Width: 20})
		}},
		{"StampPDF", func(ctx context.Context, t *testing.T, w *Writer) error {
			return w.StampPDF(PDFStamp{Source: openSource(ctx, t), Page: 1, Scale: 0.2})
		}},
		{"AddAttachment", func(_ context.Context, _ *testing.T, w *Writer) error {
			w.AddAttachment("late.txt", []byte("late"), "text/plain")
			return nil
		}},
		{"AddAssociatedFile", func(_ context.Context, _ *testing.T, w *Writer) error {
			w.AddAssociatedFile(AssociatedFile{Name: "late.xml", Data: []byte("<late/>"), MimeType: "application/xml", Relationship: "Data"})
			return nil
		}},
		{"RemoveAttachment", func(_ context.Context, _ *testing.T, w *Writer) error {
			w.RemoveAttachment("late.txt")
			return nil
		}},
		{"Redact", func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{10, 690, 100, 720}}})
			return err
		}},
		{"RemediatePDFA", func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.RemediatePDFA(ctx, models.PDFA1b)
			return err
		}},
		{"RemediatePDFUA", func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.RemediatePDFUA(ctx, models.PDFUA1)
			return err
		}},
		{"RemediateWTPDF", func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.RemediateWTPDF(ctx, models.WTPDF1Reuse)
			return err
		}},
		{"RemediateFacturX", func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.RemediateFacturX(ctx, FacturXConfig{InvoiceXML: []byte("<x/>"), Level: models.FacturXMinimum})
			return err
		}},
		{"RepairFacturX", func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.RepairFacturX(ctx)
			return err
		}},
	}
}

// TestWriterRefusesEveryEditAfterARemediation pins that a remediation is the
// last edit on a Writer: every other method but Sign, WriteTo and Close, a
// second remediation included, is refused with [ErrEditAfterRemediation]
// naming the call, and WriteTo returns the refusal and writes nothing, whether
// the method could return it or not.
//
// A remediation's report describes the document it judged, and an edit queued
// after it changed that document behind the report's back.
//
// # Corpus evidence
//
//   - All corpora: of 316 edits (stamp, attach, rotate, delete a page, set
//     metadata) queued after a PDF/A-2b or PDF/UA-1 remediation of 36 files
//     sampled across govdocs, pdfjs, verapdf, pdf20, pdfcpu and litho, 128
//     outputs validated to a residual other than the reported one. All 316
//     are refused now, 172 by the call and 144 by WriteTo.
func TestWriterRefusesEveryEditAfterARemediation(t *testing.T) {
	t.Parallel()

	for _, tc := range refusedCalls(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := signablePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}

			callErr := tc.call(ctx, t, w)
			if callErr != nil {
				if !errors.Is(callErr, ErrEditAfterRemediation) {
					t.Fatalf("Writer.%s after a remediation returned %v, want ErrEditAfterRemediation", tc.name, callErr)
				}
				if !strings.Contains(callErr.Error(), "Writer."+tc.name) {
					t.Errorf("the refusal %q does not name Writer.%s", callErr, tc.name)
				}
			}

			var out bytes.Buffer
			err = w.WriteTo(ctx, &out)
			if !errors.Is(err, ErrEditAfterRemediation) {
				t.Fatalf("WriteTo after Writer.%s was refused returned %v, want ErrEditAfterRemediation", tc.name, err)
			}
			if !strings.Contains(err.Error(), "Writer."+tc.name) {
				t.Errorf("WriteTo's refusal %q does not name Writer.%s", err, tc.name)
			}
			if out.Len() != 0 {
				t.Errorf("WriteTo wrote %d bytes after refusing Writer.%s", out.Len(), tc.name)
			}
		})
	}
}

// TestRefusedCallsCoverEveryWriterMethod pins [refusedCalls] against the type:
// every exported Writer method is either one a remediated Writer accepts or
// one the refusal test calls, so a method added later cannot slip past the
// rule by being forgotten here.
func TestRefusedCallsCoverEveryWriterMethod(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}
	var methods []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || !fn.Name.IsExported() {
					continue
				}
				star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				if id, ok := star.X.(*ast.Ident); ok && id.Name == "Writer" {
					methods = append(methods, fn.Name.Name)
				}
			}
		}
	}

	// Controls: a parser that matched nothing would report no gap.
	if len(methods) < 20 || !slices.Contains(methods, "RemediatePDFA") || !slices.Contains(methods, "RotatePages") {
		t.Fatalf("found %d exported Writer methods (%v), so the parser is not reading the package", len(methods), methods)
	}

	refused := make(map[string]bool)
	for _, c := range refusedCalls(t) {
		refused[c.name] = true
	}
	for _, m := range methods {
		if !refused[m] && !slices.Contains(terminalCalls, m) {
			t.Errorf("Writer.%s is neither refused after a remediation by the test nor one of %v", m, terminalCalls)
		}
		delete(refused, m)
	}
	for name := range refused {
		t.Errorf("refusedCalls names Writer.%s, which the package does not define", name)
	}
}

// appearedFormPDF builds a page with one text field whose stored appearance
// shows its value, in a form that does not ask the viewer to build appearances.
func appearedFormPDF() []byte {
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	const ap = "/Tx BMC BT /F1 12 Tf 2 5 Td (value) Tj ET EMC\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> /Annots [7 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Fields [7 0 R] /DA (/Helv 0 Tf 0 g) >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (value) /F 4 /P 3 0 R" +
			" /Rect [100 600 300 620] /AP << /N 8 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 20] /Resources << /Font << /F1 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(ap), ap),
	})
}

// remediateFor runs the remediation of profile, "PDF/A-2b" or "PDF/UA-1", on w.
func remediateFor(ctx context.Context, w *Writer, profile string) (*RemediationReport, error) {
	if profile == string(models.PDFUA1) {
		return w.RemediatePDFUA(ctx, models.PDFUA1)
	}
	return w.RemediatePDFA(ctx, models.PDFA2b)
}

// validateFor validates data against profile, "PDF/A-2b" or "PDF/UA-1".
func validateFor(t *testing.T, data []byte, profile string) []error {
	t.Helper()

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r.Close()
	var res []error
	if profile == string(models.PDFUA1) {
		res, err = r.ValidatePDFUA(ctx, models.PDFUA1)
	} else {
		res, err = r.ValidatePDFA(ctx, models.PDFA2b)
	}
	if err != nil {
		t.Fatalf("validating the output: %v", err)
	}
	return res
}

// TestWriterEditBeforeARemediationValidatesAsTheReportSays pins that the
// output of edits queued before a remediation validates to exactly the
// residual the remediation reports: the remediation judged the edited
// document, and nothing edits it afterwards.
func TestWriterEditBeforeARemediationValidatesAsTheReportSays(t *testing.T) {
	t.Parallel()

	pngData := buildPNG(t, false)
	stamp := signablePDF()
	for _, tc := range []struct {
		name string
		data []byte
		edit func(ctx context.Context, t *testing.T, w *Writer) error
	}{
		{"a text stamp", mixedColorPDF(), func(_ context.Context, _ *testing.T, w *Writer) error {
			return w.StampText(TextStamp{Text: "DRAFT", X: 50, Y: 50, Opacity: 0.5})
		}},
		{"an image stamp", mixedColorPDF(), func(_ context.Context, _ *testing.T, w *Writer) error {
			return w.StampImage(ImageStamp{Data: pngData, MimeType: "image/png", X: 10, Y: 10, Width: 20})
		}},
		{"a PDF stamp", mixedColorPDF(), func(ctx context.Context, t *testing.T, w *Writer) error {
			r, err := Open(ctx, bytes.NewReader(stamp), int64(len(stamp)))
			if err != nil {
				return err
			}
			t.Cleanup(func() {
				if err := r.Close(); err != nil {
					t.Errorf("closing the stamp: %v", err)
				}
			})
			return w.StampPDF(PDFStamp{Source: r, Page: 1, Scale: 0.2})
		}},
		{"an attachment", signablePDF(), func(_ context.Context, _ *testing.T, w *Writer) error {
			w.AddAttachment("note.txt", []byte("a note"), "text/plain")
			return nil
		}},
		{"a rotation", signablePDF(), func(_ context.Context, _ *testing.T, w *Writer) error {
			return w.RotatePages(90)
		}},
		{"a page deletion", threePagePDF(), func(_ context.Context, _ *testing.T, w *Writer) error {
			return w.DeletePages(1)
		}},
		{"a metadata edit", signablePDF(), func(_ context.Context, _ *testing.T, w *Writer) error {
			w.SetMetadata(Metadata{Title: "Edited title", Author: "Edited author"})
			return nil
		}},
		{"a form flatten", placeholderFormPDF(), func(_ context.Context, _ *testing.T, w *Writer) error {
			w.FlattenForm()
			return nil
		}},
		{"a form field edit", appearedFormPDF(), func(_ context.Context, _ *testing.T, w *Writer) error {
			w.SetFormField("name", "other")
			return nil
		}},
		{"auto-tagging", untaggedHeadingPDF(), func(_ context.Context, _ *testing.T, w *Writer) error {
			w.AutoTag()
			return nil
		}},
		{"a redaction", redactableTextPDF(""), func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.Redact(ctx, []RedactRegion{coversSecret})
			return err
		}},
	} {
		for _, profile := range []string{string(models.PDFA2b), string(models.PDFUA1)} {
			t.Run(tc.name+", "+profile, func(t *testing.T) {
				t.Parallel()

				ctx := context.Background()
				r, err := Open(ctx, bytes.NewReader(tc.data), int64(len(tc.data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				w := NewWriter(r)
				defer w.Close()
				if err := tc.edit(ctx, t, w); err != nil {
					t.Fatalf("the edit: %v", err)
				}
				report, err := remediateFor(ctx, w, profile)
				if err != nil {
					t.Fatalf("remediation: %v", err)
				}
				var out bytes.Buffer
				if err := w.WriteTo(ctx, &out); err != nil {
					t.Fatalf("WriteTo: %v", err)
				}
				got, want := violationTexts(validateFor(t, out.Bytes(), profile)), violationTexts(report.Residual)
				if !slices.Equal(got, want) {
					t.Errorf("the output validates to %q, want the reported residual %q", got, want)
				}
			})
		}
	}
}

// TestWriterRemediationJudgesTheEditedDocument pins that a remediation
// validates the document the edits queued before it produce, not the source,
// so its report answers for the output.
//
// The remediation validated the source while its fixers repaired the objects
// the edits had staged. A stamp drawn at half opacity puts transparency on a
// page that had none, which ISO 19005-2, 6.2.10 t2 asks to blend in a
// transparency group's colour space, and the remediation never gave the page
// one. A page deletion rewrites the whole file, and the report still claimed
// the full rewrite repairing the source's cross-reference formatting, which
// the edited document no longer had. A flattened form is the control: its
// /NeedAppearances flag goes with the form, and neither design claims it.
func TestWriterRemediationJudgesTheEditedDocument(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		data  []byte
		edit  func(w *Writer) error
		fix   string
		wants bool
	}{
		{
			name: "a stamp's transparency gets its blending space",
			data: signablePDF(),
			edit: func(w *Writer) error {
				return w.StampText(TextStamp{Text: "DRAFT", X: 50, Y: 50, Opacity: 0.5})
			},
			fix:   "normalised /Group on 1 page(s)",
			wants: true,
		},
		{
			name:  "a deletion's rewrite leaves no formatting to repair",
			data:  threePagePDF(),
			edit:  func(w *Writer) error { return w.DeletePages(1) },
			fix:   remediation.FullRewriteNote,
			wants: false,
		},
		{
			name:  "a flattened form claims no repair of its flag",
			data:  placeholderFormPDF(),
			edit:  func(w *Writer) error { w.FlattenForm(); return nil },
			fix:   "removed /NeedAppearances from AcroForm",
			wants: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			r, err := Open(ctx, bytes.NewReader(tc.data), int64(len(tc.data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			if err := tc.edit(w); err != nil {
				t.Fatalf("the edit: %v", err)
			}
			report, err := w.RemediatePDFA(ctx, models.PDFA2b)
			if err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			if got := slices.Contains(report.Fixed, tc.fix); got != tc.wants {
				t.Errorf("the report lists %q: %v, want %v; fixed: %q", tc.fix, got, tc.wants, report.Fixed)
			}
		})
	}
}
