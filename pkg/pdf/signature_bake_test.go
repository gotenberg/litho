package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// signatureAppearance is what the signed signature widget of signedFormPDF
// draws: text a reader can extract once it is page content.
const signatureAppearance = "BT /Helv 10 Tf 5 20 Td (Signed by Jane) Tj ET"

// signedFormPDF is a one-page form holding a signed signature field that draws
// signatureAppearance, a signed one with no area (an invisible signature), an
// unsigned signature field and a text field, each merged with its widget.
// withSigned false leaves the two signed fields out, so the page is the one a
// copy that bakes them should look like, less the baked appearance.
func signedFormPDF(withSigned bool) []byte {
	annots, fields := "[5 0 R 6 0 R]", "[5 0 R 6 0 R]"
	if withSigned {
		annots, fields = "[4 0 R 5 0 R 6 0 R 10 0 R]", "[4 0 R 5 0 R 6 0 R 10 0 R]"
	}
	return buildPDF([]string{
		// 1: catalog.
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields " + fields + " /SigFlags 3 >> >>",
		// 2: pages.
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// 3: page.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots " + annots + " >>",
		// 4: the signed signature field and its widget.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (signed) /V 7 0 R /Rect [50 700 250 750] /F 4 " +
			"/P 3 0 R /AP << /N 8 0 R >> >>",
		// 5: an unsigned signature field.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (blank) /Rect [50 600 250 650] /F 4 /P 3 0 R >>",
		// 6: a text field.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /V (Paris) /Rect [50 500 250 520] /F 4 /P 3 0 R >>",
		// 7: the signature dictionary both signed fields hold.
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached " +
			"/ByteRange [0 100 200 300] /Contents <30820001> >>",
		// 8: the signed widget's appearance.
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 50] /Resources << /Font << /Helv 9 0 R >> >> "+
			"/Length %d >>\nstream\n%s\nendstream", len(signatureAppearance), signatureAppearance),
		// 9: font.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		// 10: an invisible signature: signed, with no area to draw in.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (invisible) /V 7 0 R /Rect [0 0 0 0] /F 132 " +
			"/P 3 0 R /AP << /N 8 0 R >> >>",
	})
}

// blankPagePDF is a one-page document with no form.
func blankPagePDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	})
}

// pageContent returns the decoded content streams of the 1-based page of data,
// joined.
func pageContent(ctx context.Context, t *testing.T, data []byte, pageNum int) string {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	catalog, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, r.doc, catalog)
	if err != nil || len(pages) < pageNum {
		t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
	}
	contents := pages[pageNum-1].Dict["Contents"]
	var streams ast.PdfArray
	if arr, err := document.ResolveArray(ctx, r.doc, contents); err == nil && arr != nil {
		streams = arr
	} else {
		streams = ast.PdfArray{contents}
	}
	var out strings.Builder
	for _, obj := range streams {
		resolved, err := document.ResolveObject(ctx, r.doc, obj)
		if err != nil {
			t.Fatalf("resolving a content stream: %v", err)
		}
		stream, ok := resolved.(ast.PdfStream)
		if !ok {
			continue
		}
		data, err := r.doc.DecodeStream(ctx, stream)
		if err != nil {
			t.Fatalf("decoding a content stream: %v", err)
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.String()
}

// pageText returns the text litho extracts from the 1-based page of data.
func pageText(ctx context.Context, t *testing.T, data []byte, pageNum int) string {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	text, err := r.ExtractText(ctx, pageNum)
	if err != nil {
		t.Fatalf("ExtractText(%d): %v", pageNum, err)
	}
	return text
}

// checkSignaturesBaked checks that data's form lists the unsigned signature
// field and the text field of signedFormPDF and nothing signed, that no widget
// is left outside the form, and that the 1-based page draws the signed
// field's appearance as its own content.
func checkSignaturesBaked(ctx context.Context, t *testing.T, data []byte, pageNum int) {
	t.Helper()
	listed, outside := formRoots(ctx, t, data)
	if !slices.Equal(listed, []string{"blank", "city"}) {
		t.Errorf("root fields = %v, want [blank city]: the signed fields left out, the unsigned one kept fillable", listed)
	}
	if len(outside) != 0 {
		t.Errorf("widgets outside the form = %v, want none", outside)
	}
	if text := pageText(ctx, t, data, pageNum); !strings.Contains(text, "Signed by Jane") {
		t.Errorf("page %d text = %q, want the signed field's appearance, \"Signed by Jane\", drawn into it", pageNum, text)
	}
}

// TestWriter_InsertPages_BakesCopiedSignatures pins that a signed signature
// field on an inserted page leaves the form and the page, its appearance drawn
// into the page content, as [Writer.FlattenForm] draws one.
//
// The signature's digest covers bytes of the source file (ISO 32000-1,
// 12.8.1), which the new document does not hold, so the field cannot carry
// over. Its widget used to stay on the page outside the form, which qpdf
// reports as a widget not reachable from /AcroForm. An invisible signature has
// no area to draw in and leaves without drawing. An unsigned signature field
// signs nothing and is listed like any other field.
func TestWriter_InsertPages_BakesCopiedSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out := insertPageBytes(ctx, t, blankPagePDF(), signedFormPDF(true), 1, 1)
	checkSignaturesBaked(ctx, t, out, 2)
}

// TestMerge_BakesSignedSignatures pins that a merge draws a signed signature
// widget into its page and keeps an unsigned signature field as a fillable
// field. It used to leave every signature field out of the form, unsigned ones
// included, and their widgets on the page outside it.
func TestMerge_BakesSignedSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := openDeclaration(t, signedFormPDF(true), false)
	defer src.Close()
	blank := openDeclaration(t, blankPagePDF(), false)
	defer blank.Close()

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{blank, src}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	checkSignaturesBaked(ctx, t, out.Bytes(), 2)
}

// TestSplit_BakesSignedSignatures pins that a split does what a merge does
// with a signed signature widget and an unsigned signature field.
func TestSplit_BakesSignedSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := openDeclaration(t, signedFormPDF(true), false)
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{1}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	checkSignaturesBaked(ctx, t, out.Bytes(), 1)
}

// TestSplit_BakesASignatureOnAnInheritingPage pins that a page inheriting its
// /Resources from its /Pages node keeps both its own text and the signature
// drawn into it.
//
// A copied page states the resources it inherited, since the flat output tree
// gives it nothing to inherit from (ISO 32000-1, 7.7.3.4). Stating them from
// the source page rather than from the drawn one left out the appearance the
// drawing names, so the signature vanished from the page.
func TestSplit_BakesASignatureOnAnInheritingPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const pageContent = "BT /F1 12 Tf 72 740 Td (Contract) Tj ET"
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /Font << /F1 7 0 R /Helv 7 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 8 0 R /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (signed) /V 5 0 R /Rect [50 700 250 750] /F 4 " +
			"/P 3 0 R /AP << /N 6 0 R >> >>",
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached " +
			"/ByteRange [0 100 200 300] /Contents <30820001> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 50] /Resources << /Font << /Helv 7 0 R >> >> "+
			"/Length %d >>\nstream\n%s\nendstream", len(signatureAppearance), signatureAppearance),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageContent), pageContent),
	})
	src := openDeclaration(t, data, false)
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{1}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	text := pageText(ctx, t, out.Bytes(), 1)
	for _, want := range []string{"Contract", "Signed by Jane"} {
		if !strings.Contains(text, want) {
			t.Errorf("page text = %q, want %q in it", text, want)
		}
	}
}

// TestWriter_InsertPages_BakedSignatureIsAnArtifact pins that inserting a page
// with a signed signature widget into a tagged document marks the drawn
// appearance as an artifact.
//
// No structure element of the document encloses the copied widget, since an
// insertion carries the page's content and not its source's structure, so the
// appearance is not part of the logical structure (ISO 32000-1, 14.8.2.2), as
// for a widget no element encloses when [Writer.FlattenForm] draws it. Bare, it
// would be content neither tagged nor marked, which ISO 14289-1, 7.1 and ISO
// 14289-2, 8.2.2 forbid. The insertion adds no PDF/UA violation the same page
// without the signed widgets does not add.
func TestWriter_InsertPages_BakedSignatureIsAnArtifact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	baked := insertPageBytes(ctx, t, taggedFormPDF(), signedFormPDF(true), 1, 1)
	plain := insertPageBytes(ctx, t, taggedFormPDF(), signedFormPDF(false), 1, 1)

	content := pageContent(ctx, t, baked, 2)
	if !strings.Contains(content, " Do") {
		t.Fatalf("page 2 draws no appearance: %q", content)
	}
	if !strings.Contains(content, "/Artifact BMC") {
		t.Errorf("the drawn signature appearance is not marked as an artifact: %q", content)
	}

	for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
		got, want := uaRuleIDs(ctx, t, baked, profile), uaRuleIDs(ctx, t, plain, profile)
		if !slices.Equal(got, want) {
			t.Errorf("%s: the baked insertion fails %v, the same page without its signed widgets %v", profile, got, want)
		}
	}
}
