package remediation

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// embeddedTreePDF builds a page whose catalog names, in its /EmbeddedFiles
// tree, the file specification 4, of body spec, and the file specification 6,
// typed /F as pdfjs issue9367.pdf types its own, whose embedded file 7 carries
// /Subtype /text#2Fplain#3B#20charset#3Dutf-8, a media type with a parameter
// the "type/subtype" grammar refuses. stream holds the dictionary entries of
// the embedded file 5 besides /Type and /Length.
func embeddedTreePDF(spec, stream string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a.txt) 4 0 R (b.txt) 6 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		spec,
		"<< /Type /EmbeddedFile "+stream+" /Length 3 >>\nstream\nabc\nendstream",
		"<< /Type /F /F (b.txt) /UF (b.txt) /AFRelationship /Data /EF << /F 7 0 R >> >>",
		"<< /Type /EmbeddedFile /Subtype /text#2Fplain#3B#20charset#3Dutf-8 /Length 3 >>\nstream\nabc\nendstream",
	)
}

// mimeFindings returns the embedded-file media type findings of doc at
// profile: ISO 19005-3, 6.8 t1 and ISO 19005-4, 6.9 t1.
func mimeFindings(t *testing.T, doc *document.Document, profile models.PDFAProfile) []compliance.Violation {
	t.Helper()

	errs, err := compliance.ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	var out []compliance.Violation
	for _, v := range toViolations(errs) {
		if v.Rule.TestNumber == 1 && (v.Rule == compliance.Rule_19005_3_6_8_t01 || v.Rule == compliance.Rule_19005_4_6_9_t01) {
			out = append(out, v)
		}
	}
	return out
}

func TestMatchEmbeddedSubtype(t *testing.T) {
	t.Parallel()

	for _, v := range []compliance.Violation{
		{Rule: compliance.Rule_19005_3_6_8_t01, Context: `object 4: embedded file /Subtype "/" is not a valid MIME type`},
		{Rule: compliance.Rule_19005_4_6_9_t01, Context: "embedded file stream missing /Subtype MIME type"},
	} {
		if !matchEmbeddedSubtype(v) {
			t.Errorf("must match %s %s t%d", v.Rule.Specification, v.Rule.Clause, v.Rule.TestNumber)
		}
	}
	for _, r := range []compliance.Rule{
		compliance.Rule_19005_3_6_8_t02,
		compliance.Rule_19005_3_6_8_t03,
		compliance.Rule_19005_3_6_8_t04,
		compliance.Rule_19005_2_6_8_t02,
	} {
		if matchEmbeddedSubtype(compliance.Violation{Rule: r, Context: "embedded file /Subtype is not a valid MIME type"}) {
			t.Errorf("must not match %s %s t%d", r.Specification, r.Clause, r.TestNumber)
		}
	}
}

// TestRemediatePDFA_GivesEveryEmbeddedFileAMediaType pins that a PDF/A-3b
// remediation gives an embedded file a media type ISO 19005-3, 6.8 t1
// accepts, as a PDF/A-4 one did under ISO 19005-4, 6.9 t1: an embedded file
// with no /Subtype, under a file specification typed /F, and one whose
// /Subtype carries a parameter. embedded-subtype claimed the part-4 rule
// alone, and read the media type with a looser test than the validator's.
func TestRemediatePDFA_GivesEveryEmbeddedFileAMediaType(t *testing.T) {
	t.Parallel()

	for _, profile := range []models.PDFAProfile{models.PDFA3b, models.PDFA4f} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, embeddedTreePDF("<< /Type /F /F (a.txt) /UF (a.txt) /AFRelationship /Data /EF << /F 5 0 R >> >>", ""))
			if n := len(mimeFindings(t, doc, profile)); n != 2 {
				t.Fatalf("precondition: the source fails the media type rule %d times, want 2", n)
			}
			if _, err := RemediatePDFA(ctx, doc, alloc, profile, Options{}); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			var out bytes.Buffer
			if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
				t.Fatalf("FullRewrite: %v", err)
			}
			reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			for _, v := range mimeFindings(t, reread, profile) {
				t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
			}
		})
	}
}

// TestFixEmbeddedSubtype_ReachesTheFileSpecsOutsideTheTree pins that
// embedded-subtype gives a media type to the embedded file of a /Filespec no
// /EmbeddedFiles tree names, here a FileAttachment's, which ISO 19005-3, 6.8
// t1 and ISO 19005-4, 6.9 t1 judge as they judge the tree's. It leaves the
// file specification's missing /UF to filespec-fuf, which refuses to copy an
// empty /F.
func TestFixEmbeddedSubtype_ReachesTheFileSpecsOutsideTheTree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] /FS 5 0 R >>",
		"<< /Type /Filespec /F () /AFRelationship /Data /EF << /F 6 0 R >> >>",
		"<< /Type /EmbeddedFile /Subtype /text#232Fplain /Length 3 >>\nstream\nabc\nendstream",
	))
	for _, profile := range []models.PDFAProfile{models.PDFA3b, models.PDFA4f} {
		if n := len(mimeFindings(t, doc, profile)); n != 1 {
			t.Fatalf("precondition: the source fails the media type rule %d times at %s, want 1", n, profile)
		}
	}

	if _, err := fixEmbeddedSubtype(ctx, alloc, doc, Options{}, nil, string(models.PDFA3b)); err != nil {
		t.Fatalf("fixEmbeddedSubtype: %v", err)
	}
	stream, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 6})
	if !ok {
		t.Fatal("object 6 is no stream")
	}
	if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); subtype != "application/octet-stream" {
		t.Errorf("the embedded file's /Subtype = %q, want application/octet-stream", subtype)
	}
	if _, rewritten := alloc.Pending()[5]; rewritten {
		t.Errorf("the file specification was rewritten: %v", alloc.Pending()[5])
	}
}

// TestFixEmbeddedSubtype_KeepsStringLimitsWrites pins that embedded-subtype
// builds on the file specification and the embedded file string-limit
// (registry 50) wrote before it (54): each carries a string past the 32767
// bytes of ISO 19005-2 and -3, 6.1.13 t3, which string-limit truncates, and
// embedded-subtype writes both again, the file specification for its missing
// /UF and the embedded file for its missing /Subtype. Read from the document,
// both came back whole.
func TestFixEmbeddedSubtype_KeepsStringLimitsWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	long := "(" + strings.Repeat("x", pdfA2StringLimit+10) + ")"
	doc, alloc := newTestDoc(t, embeddedTreePDF(
		"<< /Type /Filespec /F (a.txt) /Desc "+long+" /AFRelationship /Data /EF << /F 5 0 R >> >>",
		"/Note "+long))
	if _, err := fixStringLimit(ctx, alloc, doc, Options{}, nil, string(models.PDFA3b)); err != nil {
		t.Fatalf("fixStringLimit: %v", err)
	}
	if _, queued := alloc.Pending()[5]; !queued {
		t.Fatal("control: string-limit truncated nothing in the embedded file, so there is no earlier write to lose")
	}
	if _, err := fixEmbeddedSubtype(ctx, alloc, doc, Options{}, nil, string(models.PDFA3b)); err != nil {
		t.Fatalf("fixEmbeddedSubtype: %v", err)
	}

	spec := pendingDict(t, alloc, 4)
	if desc, _ := spec["Desc"].(ast.PdfString); len(desc) > pdfA2StringLimit {
		t.Errorf("the file specification's /Desc is %d bytes again, string-limit's truncation is lost", len(desc))
	}
	if spec["UF"] == nil {
		t.Errorf("the file specification has no /UF: %v", spec["UF"])
	}
	stream, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 5})
	if !ok {
		t.Fatal("object 5 is no stream")
	}
	if note, _ := stream.Dict["Note"].(ast.PdfString); len(note) > pdfA2StringLimit {
		t.Errorf("the embedded file's /Note is %d bytes again, string-limit's truncation is lost", len(note))
	}
	if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); !compliance.IsValidMIMEType(string(subtype)) {
		t.Errorf("the embedded file's /Subtype = %q, want a media type", subtype)
	}
}

// TestFixStreamFileKeys_KeepsTheEmbeddedFileMediaType pins that
// stream-file-keys (registry 77), which rewrites the embedded file's
// dictionary after embedded-subtype (54) wrote it, keeps the /Subtype
// embedded-subtype gave it while it drops the /FFilter ISO 19005-3, 6.1.7.1
// forbids.
func TestFixStreamFileKeys_KeepsTheEmbeddedFileMediaType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, embeddedTreePDF("<< /Type /Filespec /F (a.txt) /UF (a.txt) /AFRelationship /Data /EF << /F 5 0 R >> >>", "/FFilter /FlateDecode"))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA3b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)
	for _, name := range []string{"embedded-subtype", "stream-file-keys"} {
		f, matched := matchedBy(t, name, violations)
		if _, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA3b)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	stream, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 5})
	if !ok {
		t.Fatal("object 5 is no stream")
	}
	if _, has := stream.Dict["FFilter"]; has {
		t.Errorf("control: stream-file-keys kept /FFilter, so there is no later write to test: %v", stream.Dict)
	}
	if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); subtype != "application/octet-stream" {
		t.Errorf("the embedded file's /Subtype = %q once stream-file-keys ran, want application/octet-stream", subtype)
	}
}
