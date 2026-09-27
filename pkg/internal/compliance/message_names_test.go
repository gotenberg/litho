package compliance

import (
	"context"
	"testing"

	pdfast "github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// TestEmbeddedSubtypeMessageNamesTheDefect pins the /Subtype message of ISO
// 19005-3, 6.8 t1 and ISO 19005-4, 6.9 t1: an absent entry reads as missing,
// a name is quoted without its solidus, and the PDF/A-4 message names the file
// specification as the PDF/A-3 one does. A missing /Subtype printed as "/", a
// value the stream does not have.
func TestEmbeddedSubtypeMessageNamesTheDefect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name, stream, want string
	}{
		{"missing", "<< /Type /EmbeddedFile /Length 4 >>", `object 4: embedded file /Subtype is missing`},
		{"null", "<< /Type /EmbeddedFile /Subtype null /Length 4 >>", `object 4: embedded file /Subtype is missing`},
		{"string", "<< /Type /EmbeddedFile /Subtype (text/plain) /Length 4 >>", `object 4: embedded file /Subtype is not a name`},
		{"invalid", "<< /Type /EmbeddedFile /Subtype /plain /Length 4 >>", `object 4: embedded file /Subtype "plain" is not a valid MIME type`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, catalog := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /Type /Filespec /F (a.txt) /UF (a.txt) /AFRelationship /Data /EF << /F 5 0 R >> >>",
				tc.stream+"\nstream\ndata\nendstream",
			)
			for _, got := range []Violation{
				onlyContext(t, checkEmbeddedSubtypeMIME(ctx, doc, collectFileSpecs(ctx, doc, catalog)[0])),
				onlyContext(t, checkEmbeddedFileMIME(ctx, doc, catalog)),
			} {
				if got.Context != tc.want {
					t.Errorf("clause %s: context = %q, want %q", got.Rule.Clause, got.Context, tc.want)
				}
			}
		})
	}
}

// TestInlineEmbeddedFileNotPDFNamesItsContainer pins that ISO 19005-4, 6.9 t3
// names a file specification written inline by the object holding it and its
// path there, as every other clause 6.9 message does. It cited the object
// number of the file specification itself, which an inline one does not have,
// and so read "object 0", an object no file holds.
func TestInlineEmbeddedFileNotPDFNamesItsContainer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /F 4"+
			" /AF [<< /Type /Filespec /F (a.txt) /UF (a.txt) /AFRelationship /Data /EF << /F 5 0 R >> >>] >>",
		"<< /Type /EmbeddedFile /Subtype /text#2Fplain /Length 4 >>\nstream\ndata\nendstream",
	)

	var embedded []fileSpecEntry
	for _, fs := range collectFileSpecs(ctx, doc, catalog) {
		if fs.hasEF {
			embedded = append(embedded, fs)
		}
	}
	got := onlyContext(t, checkEmbeddedFileCompliance(ctx, doc, embedded, Rule_19005_4_6_9_t03,
		[]models.PDFAProfile{models.PDFA4, models.PDFA2b, models.PDFA1b}))
	if want := "object 4 /AF[0]: embedded file is not a PDF document"; got.Context != want {
		t.Errorf("context = %q, want %q", got.Context, want)
	}
}

// TestNameMessagesQuoteTheName pins the messages that quote a name the file
// may leave out or write as another type: a missing entry reads as missing and
// a name is quoted without its solidus, where %q of the resolved PdfName
// printed "/" for both.
func TestNameMessagesQuoteTheName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := nopDoc()

	threeD := func(streamDict string) string {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [] /Count 0 >>",
			streamDict+"\nstream\ndata\nendstream",
		)
		return onlyContext(t, check3DStreamSubtype(ctx, doc, pdfast.PdfDict{"3DD": pdfast.PdfIndirectRef{ObjectNum: 3}}, "annot")).Context
	}
	intent := func(value pdfast.PdfObject) string {
		img := pdfast.PdfDict{"Intent": value, "BitsPerComponent": pdfast.PdfInt(8)}
		for _, v := range checkImageXObject(ctx, doc, img, "Im1", 1, 2, false) {
			if v.Rule == renderingIntentRule(2) {
				return v.Context
			}
		}
		t.Fatalf("no rendering intent violation for /Intent %v", value)
		return ""
	}

	for _, tc := range []struct{ got, want string }{
		{threeD("<< /Length 4 >>"), "annot: 3D stream /Subtype is missing (must be U3D or PRC)"},
		{threeD("<< /Length 4 /Subtype /X3D >>"), `annot: 3D stream Subtype="X3D" (must be U3D or PRC)`},
		{threeD("<< /Length 4 /Subtype (U3D) >>"), "annot: 3D stream /Subtype is not a name (must be U3D or PRC)"},
		{intent(pdfast.PdfName("Vivid")), `page 1, XObject /Im1: invalid rendering intent "Vivid"`},
		{intent(pdfast.PdfNull{}), "page 1, XObject /Im1: /Intent is null, not a rendering intent"},
		{intent(pdfast.PdfInt(1)), "page 1, XObject /Im1: /Intent is not a name"},
	} {
		if tc.got != tc.want {
			t.Errorf("context = %q, want %q", tc.got, tc.want)
		}
	}
}

// onlyContext returns the one violation vs holds, failing the test otherwise.
func onlyContext(t *testing.T, vs []Violation) Violation {
	t.Helper()
	if len(vs) != 1 {
		t.Fatalf("got %d violations, want 1: %v", len(vs), vs)
	}
	return vs[0]
}
