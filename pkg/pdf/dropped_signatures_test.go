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

// certifiedObjs returns the objects of a two-page certified form. Page 1
// presents "author", a signed signature field /Perms /DocMDP names, drawing
// "Signed by Jane"; "blank", an unsigned signature field; "city", a text
// field; and "form1", a field that is not a signature field, whose kids are
// "witness", a signed signature field drawing "Witnessed by Joe", and "note",
// a text field. /Perms /UR3 names a usage rights signature no field holds.
// Page 2 shows "Second page".
func certifiedObjs() []string {
	sig := func(extra string) string {
		return "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached " +
			"/ByteRange [0 100 200 300] /Contents <30820001> " + extra + ">>"
	}
	stream := func(dict, content string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(content), content)
	}
	appearance := func(text string) string {
		return stream("/Type /XObject /Subtype /Form /BBox [0 0 200 50] /Resources << /Font << /Helv 9 0 R >> >>",
			"BT /Helv 10 Tf 5 20 Td ("+text+") Tj ET")
	}
	return []string{
		// 1: catalog.
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R 11 0 R] /SigFlags 3 >> " +
			"/Perms << /DocMDP 7 0 R /UR3 12 0 R >> >>",
		// 2: pages.
		"<< /Type /Pages /Kids [3 0 R 13 0 R] /Count 2 >>",
		// 3: page 1.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R 14 0 R 16 0 R] >>",
		// 4: the certifying signature field.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (author) /V 7 0 R /Rect [50 700 250 750] /F 4 " +
			"/P 3 0 R /AP << /N 8 0 R >> >>",
		// 5: an unsigned signature field.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (blank) /Rect [50 600 250 650] /F 4 /P 3 0 R >>",
		// 6: a text field.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /V (Paris) /Rect [50 500 250 520] /F 4 /P 3 0 R >>",
		// 7: the certification signature.
		sig("/Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P 2 /V /1.2 >> >>] "),
		// 8: its appearance.
		appearance("Signed by Jane"),
		// 9: font.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		// 10: the witness signature.
		sig(""),
		// 11: form1.
		"<< /T (form1) /Kids [14 0 R 16 0 R] >>",
		// 12: the usage rights signature.
		sig("/Reference [<< /Type /SigRef /TransformMethod /UR3 >>] "),
		// 13: page 2.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 17 0 R /Resources << /Font << /F1 9 0 R >> >> >>",
		// 14: form1.witness.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (witness) /Parent 11 0 R /V 10 0 R " +
			"/Rect [300 700 500 750] /F 4 /P 3 0 R /AP << /N 15 0 R >> >>",
		// 15: its appearance.
		appearance("Witnessed by Joe"),
		// 16: form1.note.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (note) /Parent 11 0 R /V (hi) " +
			"/Rect [300 500 500 520] /F 4 /P 3 0 R >>",
		// 17: page 2's content.
		stream("", "BT /F1 12 Tf 72 700 Td (Second page) Tj ET"),
	}
}

// certifiedPDF is the document [certifiedObjs] describes.
func certifiedPDF() []byte {
	return buildPDF(certifiedObjs())
}

// certifiedDropped is what a write of [certifiedPDF] that leaves every
// signature out reports.
//
//nolint:gochecknoglobals // static expectation
var certifiedDropped = []DroppedSignature{
	{Field: "author", Perms: "DocMDP"},
	{Field: "form1.witness"},
	{Perms: "UR3"},
}

// writeSigned opens data, applies edit to a Writer on it and returns the output
// and the signatures the Writer reports dropping.
func writeSigned(ctx context.Context, t *testing.T, data []byte, edit func(w *Writer) []WriteOption) ([]byte, []DroppedSignature) {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	opts := edit(w)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out, opts...); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes(), w.DroppedSignatures()
}

// catalogOf returns the catalog of data, opened with password when it is not
// empty.
func catalogOf(ctx context.Context, t *testing.T, data []byte, password string) ast.PdfDict {
	t.Helper()
	var opts []OpenOption
	if password != "" {
		opts = append(opts, WithPassword(password))
	}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), opts...)
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	catalog, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	return catalog.Dict
}

// signaturesIn returns the signatures litho extracts from data.
func signaturesIn(ctx context.Context, t *testing.T, data []byte, password string) []models.Signature {
	t.Helper()
	var opts []OpenOption
	if password != "" {
		opts = append(opts, WithPassword(password))
	}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), opts...)
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	sigs, err := r.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	return sigs
}

// checkCertifiedBaked checks an unencrypted rewrite of [certifiedPDF] holds no
// signature, lists the fields that are not signed ones, and draws both visible
// signatures into page 1.
func checkCertifiedBaked(ctx context.Context, t *testing.T, data []byte) {
	t.Helper()
	if sigs := signaturesIn(ctx, t, data, ""); len(sigs) != 0 {
		t.Errorf("the output holds %d signatures, want none: %v", len(sigs), sigs)
	}
	if perms, has := catalogOf(ctx, t, data, "")["Perms"]; has {
		t.Errorf("the output keeps /Perms %v, whose signatures it does not hold", perms)
	}
	listed, outside := formRoots(ctx, t, data)
	if !slices.Equal(listed, []string{"blank", "city", "form1"}) {
		t.Errorf("root fields = %v, want [blank city form1]", listed)
	}
	if len(outside) != 0 {
		t.Errorf("widgets outside the form = %v, want none", outside)
	}
	text := pageText(ctx, t, data, 1)
	for _, want := range []string{"Signed by Jane", "Witnessed by Joe"} {
		if !strings.Contains(text, want) {
			t.Errorf("page 1 text = %q, want the appearance %q drawn into it", text, want)
		}
	}
}

// TestWriter_FullRewriteDropsTheSourceSignatures pins that every edit a full
// rewrite writes leaves the source's signatures out, draws their appearances
// into the page, and reports them.
//
// A signature covers the bytes its /ByteRange names (ISO 32000-1, 12.8.1),
// and a full rewrite moves every one of them. It used to copy the signed
// fields, which poppler's pdfsig then reported invalid or not verified, and
// /Perms kept naming signatures the file no longer held (ISO 32000-1, 12.8.4).
// Split and merge have followed this rule since they learned to draw a signed
// widget into its page.
func TestWriter_FullRewriteDropsTheSourceSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name string
		edit func(t *testing.T, w *Writer) []WriteOption
	}{
		{"DeletePages", func(t *testing.T, w *Writer) []WriteOption {
			if err := w.DeletePages(2); err != nil {
				t.Fatalf("DeletePages: %v", err)
			}
			return nil
		}},
		{"InsertPages", func(t *testing.T, w *Writer) []WriteOption {
			blank := openDeclaration(t, blankPagePDF(), false)
			t.Cleanup(func() { blank.Close() })
			if err := w.InsertPages(2, blank); err != nil {
				t.Fatalf("InsertPages: %v", err)
			}
			return nil
		}},
		{"WithFullRewrite", func(*testing.T, *Writer) []WriteOption {
			return []WriteOption{WithFullRewrite()}
		}},
		{"Redact", func(t *testing.T, w *Writer) []WriteOption {
			if _, err := w.Redact(ctx, []RedactRegion{{Page: 2, Rect: [4]float64{0, 600, 612, 792}}}); err != nil {
				t.Fatalf("Redact: %v", err)
			}
			return nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, dropped := writeSigned(ctx, t, certifiedPDF(), func(w *Writer) []WriteOption { return tc.edit(t, w) })
			checkCertifiedBaked(ctx, t, out)
			if !slices.Equal(dropped, certifiedDropped) {
				t.Errorf("DroppedSignatures = %v, want %v", dropped, certifiedDropped)
			}
		})
	}
}

// TestWriter_EncryptionDropsTheSourceSignatures pins [Writer.Encrypt] and
// [WithEncryption], whose output is a full rewrite read back with a password.
func TestWriter_EncryptionDropsTheSourceSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := EncryptConfig{UserPassword: "user", OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1}

	for _, tc := range []struct {
		name string
		edit func(w *Writer) []WriteOption
	}{
		{"Encrypt", func(w *Writer) []WriteOption { w.Encrypt(cfg); return nil }},
		{"WithEncryption", func(*Writer) []WriteOption { return []WriteOption{WithEncryption(cfg)} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, dropped := writeSigned(ctx, t, certifiedPDF(), tc.edit)
			if sigs := signaturesIn(ctx, t, out, "user"); len(sigs) != 0 {
				t.Errorf("the output holds %d signatures, want none: %v", len(sigs), sigs)
			}
			if perms, has := catalogOf(ctx, t, out, "user")["Perms"]; has {
				t.Errorf("the output keeps /Perms %v", perms)
			}
			if !slices.Equal(dropped, certifiedDropped) {
				t.Errorf("DroppedSignatures = %v, want %v", dropped, certifiedDropped)
			}
		})
	}
}

// TestWriter_FlattenFormDropsTheCertification pins that flattening, which
// takes the whole form out, also takes out the /Perms entries naming its
// signatures, and reports them.
func TestWriter_FlattenFormDropsTheCertification(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out, dropped := writeSigned(ctx, t, certifiedPDF(), func(w *Writer) []WriteOption { w.FlattenForm(); return nil })

	catalog := catalogOf(ctx, t, out, "")
	for _, key := range []ast.PdfName{"AcroForm", "Perms"} {
		if v, has := catalog[key]; has {
			t.Errorf("the flattened output keeps /%s %v", string(key), v)
		}
	}
	text := pageText(ctx, t, out, 1)
	for _, want := range []string{"Signed by Jane", "Witnessed by Joe"} {
		if !strings.Contains(text, want) {
			t.Errorf("page 1 text = %q, want %q drawn into it", text, want)
		}
	}
	if !slices.Equal(dropped, certifiedDropped) {
		t.Errorf("DroppedSignatures = %v, want %v", dropped, certifiedDropped)
	}
}

// signedCertifiedPDF returns [certifiedPDF] with a valid signature litho adds
// on top, named Signature1, and the signer that made it.
func signedCertifiedPDF(ctx context.Context, t *testing.T) []byte {
	t.Helper()
	signed, dropped := writeSigned(ctx, t, certifiedPDF(), func(w *Writer) []WriteOption {
		w.Sign(newCMSTestSigner(t))
		return nil
	})
	if dropped != nil {
		t.Fatalf("signing in an incremental update reports dropping %v", dropped)
	}
	return signed
}

// validSignatures returns the fields of the signatures of data whose digest
// matches the bytes their /ByteRange names.
func validSignatures(ctx context.Context, t *testing.T, data []byte) []string {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	results, err := r.VerifySignatures(ctx, nil)
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	var valid []string
	for _, v := range results {
		if v.HashValid {
			valid = append(valid, v.FieldName)
		}
	}
	return valid
}

// TestWriter_IncrementalUpdateKeepsTheSignatures pins that an edit written as
// an incremental update keeps every signature, a valid one still valid, and
// reports none dropped: the update appends to the bytes the signatures cover.
func TestWriter_IncrementalUpdateKeepsTheSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	signed := signedCertifiedPDF(ctx, t)

	out, dropped := writeSigned(ctx, t, signed, func(w *Writer) []WriteOption {
		if err := w.RotatePages(90, 1); err != nil {
			t.Fatalf("RotatePages: %v", err)
		}
		return nil
	})
	if dropped != nil {
		t.Errorf("DroppedSignatures = %v, want none", dropped)
	}
	if got := len(signaturesIn(ctx, t, out, "")); got != 3 {
		t.Errorf("the output holds %d signatures, want the 3 it had", got)
	}
	if valid := validSignatures(ctx, t, out); !slices.Equal(valid, []string{"Signature1"}) {
		t.Errorf("valid signatures = %v, want [Signature1]", valid)
	}
}

// TestWriter_SignedRewriteKeepsOnlyItsOwnSignature pins that a Writer that
// signs a full rewrite drops the source's signatures and keeps the one it
// adds, valid, on top.
func TestWriter_SignedRewriteKeepsOnlyItsOwnSignature(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out, dropped := writeSigned(ctx, t, signedCertifiedPDF(ctx, t), func(w *Writer) []WriteOption {
		w.Sign(newCMSTestSigner(t))
		if err := w.DeletePages(2); err != nil {
			t.Fatalf("DeletePages: %v", err)
		}
		return nil
	})
	sigs := signaturesIn(ctx, t, out, "")
	if len(sigs) != 1 {
		t.Fatalf("the output holds %d signatures, want the new one alone: %v", len(sigs), sigs)
	}
	if valid := validSignatures(ctx, t, out); !slices.Equal(valid, []string{sigs[0].FieldName}) {
		t.Errorf("valid signatures = %v, want [%s]", valid, sigs[0].FieldName)
	}
	want := append(slices.Clone(certifiedDropped[:2]), DroppedSignature{Field: "Signature1"}, certifiedDropped[2])
	if !slices.Equal(dropped, want) {
		t.Errorf("DroppedSignatures = %v, want %v", dropped, want)
	}
}

// certifiedNonBinaryPDF is [certifiedPDF] with a header comment of ASCII
// bytes, which ISO 19005-2, 6.1.2 forbids and a PDF/A remediation can repair
// only in a full rewrite. The comment keeps its length, so every offset stays.
func certifiedNonBinaryPDF() []byte {
	data := certifiedPDF()
	return bytes.Replace(data, []byte("%\xE2\xE3\xCF\xD3\n"), []byte("%abcd\n"), 1)
}

// TestWriter_RemediationRewriteDropsTheSignatures pins the remediation paths:
// one whose repairs need a full rewrite, and one that writes a deletion queued
// before it. Each writes the output before [Writer.WriteTo], which streams it,
// and the report still names what it left out.
func TestWriter_RemediationRewriteDropsTheSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		data  []byte
		queue func(t *testing.T, w *Writer)
	}{
		{"repairs", certifiedNonBinaryPDF(), func(*testing.T, *Writer) {}},
		{"queued deletion", certifiedPDF(), func(t *testing.T, w *Writer) {
			if err := w.DeletePages(2); err != nil {
				t.Fatalf("DeletePages: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, dropped := writeSigned(ctx, t, tc.data, func(w *Writer) []WriteOption {
				tc.queue(t, w)
				if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
					t.Fatalf("RemediatePDFA: %v", err)
				}
				return nil
			})
			if sigs := signaturesIn(ctx, t, out, ""); len(sigs) != 0 {
				t.Errorf("the output holds %d signatures, want none: %v", len(sigs), sigs)
			}
			if !slices.Equal(dropped, certifiedDropped) {
				t.Errorf("DroppedSignatures = %v, want %v", dropped, certifiedDropped)
			}
		})
	}
}

// TestWriter_DroppedSignaturesWaitsForTheOutput pins that nothing is reported
// before WriteTo writes, nor after a write that fails.
func TestWriter_DroppedSignaturesWaitsForTheOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := certifiedPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := w.DeletePages(2); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	if got := w.DroppedSignatures(); got != nil {
		t.Errorf("before WriteTo, DroppedSignatures = %v, want nil", got)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := w.WriteTo(cancelled, &bytes.Buffer{}); err == nil {
		t.Fatal("WriteTo under a cancelled context succeeded")
	}
	if got := w.DroppedSignatures(); got != nil {
		t.Errorf("after a failed WriteTo, DroppedSignatures = %v, want nil", got)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := w.DroppedSignatures(); !slices.Equal(got, certifiedDropped) {
		t.Errorf("after the retry, DroppedSignatures = %v, want %v", got, certifiedDropped)
	}
}

// TestSplitMerge_ReportDroppedSignatures pins [WithDroppedSignatures] on the
// two operations that build a new file, and that a split leaves out a signed
// field nested below one that is not signed.
func TestSplitMerge_ReportDroppedSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := openDeclaration(t, certifiedPDF(), false)
	defer src.Close()
	blank := openDeclaration(t, blankPagePDF(), false)
	defer blank.Close()

	var split bytes.Buffer
	var splitDropped []DroppedSignature
	if err := Split(ctx, src, []int{1}, &split, WithDroppedSignatures(&splitDropped)); err != nil {
		t.Fatalf("Split: %v", err)
	}
	if !slices.Equal(splitDropped, certifiedDropped) {
		t.Errorf("Split dropped %v, want %v", splitDropped, certifiedDropped)
	}
	if sigs := signaturesIn(ctx, t, split.Bytes(), ""); len(sigs) != 0 {
		t.Errorf("the split holds %d signatures, want none: %v", len(sigs), sigs)
	}

	var merged bytes.Buffer
	var mergeDropped []DroppedSignature
	if err := Merge(ctx, []*Reader{blank, src, src}, &merged, WithDroppedSignatures(&mergeDropped)); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !slices.Equal(mergeDropped, certifiedDropped) {
		t.Errorf("Merge dropped %v, want %v, each once", mergeDropped, certifiedDropped)
	}
}

// TestPipeline_ReportsDroppedSignatures pins [PipelineResult.DroppedSignatures]
// over an Apply stage that rewrites and one that does not, before a Split.
func TestPipeline_ReportsDroppedSignatures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		apply func(ctx context.Context, w *Writer) error
	}{
		{"rewriting stage", func(_ context.Context, w *Writer) error { return w.DeletePages(2) }},
		{"incremental stage", func(_ context.Context, w *Writer) error { return w.RotatePages(90, 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := openDeclaration(t, certifiedPDF(), false)
			defer src.Close()
			var out bytes.Buffer
			var viaOption []DroppedSignature
			result, err := NewPipeline().Open(src).Apply(tc.apply).Split([]int{1}).
				WriteTo(ctx, &out, WithDroppedSignatures(&viaOption))
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if !slices.Equal(result.DroppedSignatures, certifiedDropped) {
				t.Errorf("DroppedSignatures = %v, want %v", result.DroppedSignatures, certifiedDropped)
			}
			if !slices.Equal(viaOption, certifiedDropped) {
				t.Errorf("WithDroppedSignatures got %v, want %v", viaOption, certifiedDropped)
			}
		})
	}
}

// taggedSignedPDF builds a tagged page presenting a signed signature field and
// a text field, each the only kid of its own Form element, and the signature's
// appearance drawing "Signed by Jane". The page's text is a paragraph.
func taggedSignedPDF() []byte {
	const pageContent = "/P <</MCID 0>> BDC BT /F1 12 Tf 50 740 Td (form) Tj ET EMC\n"
	const ap = "BT /F1 10 Tf 5 20 Td (Signed by Jane) Tj ET"
	return buildPDF([]string{
		// 1: catalog.
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R 6 0 R] /SigFlags 3 >> " +
			"/StructTreeRoot 8 0 R /MarkInfo << /Marked true >> /Lang (en-US) " +
			"/ViewerPreferences << /DisplayDocTitle true >> >>",
		// 2: pages.
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// 3: page.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 13 0 R >> >> /Annots [5 0 R 6 0 R] /StructParents 0 /Tabs /S >>",
		// 4: page content.
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageContent), pageContent),
		// 5: the signed signature field.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /TU (Approval) /V 7 0 R " +
			"/Rect [50 650 250 700] /F 4 /P 3 0 R /AP << /N 12 0 R >> /StructParent 1 >>",
		// 6: a text field.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /TU (Name) /V (Ada) " +
			"/Rect [50 600 150 620] /F 4 /P 3 0 R /AP << /N 14 0 R >> /StructParent 2 >>",
		// 7: the signature.
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached " +
			"/ByteRange [0 100 200 300] /Contents <30820001> >>",
		// 8: structure tree root.
		"<< /Type /StructTreeRoot /K 9 0 R " +
			"/ParentTree << /Nums [0 [10 0 R] 1 11 0 R 2 15 0 R] >> /ParentTreeNextKey 3 >>",
		// 9: document element.
		"<< /Type /StructElem /S /Document /P 8 0 R /K [10 0 R 11 0 R 15 0 R] >>",
		// 10: paragraph.
		"<< /Type /StructElem /S /P /P 9 0 R /Pg 3 0 R /K 0 >>",
		// 11: the signature's Form element.
		"<< /Type /StructElem /S /Form /P 9 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 5 0 R >> >>",
		// 12: the signature's appearance.
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 50] /Resources << /Font << /F1 13 0 R >> >> "+
			"/Length %d >>\nstream\n%s\nendstream", len(ap), ap),
		// 13: font.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		// 14: the text field's appearance.
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 20] /Length %d >>\nstream\n%s\nendstream",
			len(flattenAppearance), flattenAppearance),
		// 15: the text field's Form element.
		"<< /Type /StructElem /S /Form /P 9 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 6 0 R >> >>",
	})
}

// TestWriter_FullRewriteTagsTheDrawnSignature pins that a full rewrite of a
// tagged document tags the signature it draws under the Form element that
// enclosed the widget, as flattening tags a field (see [flattenTags]), and
// adds no PDF/UA violation.
//
// The rewrite carries the document's structure, so the drawn appearance is
// page content its element owns: ISO 14289-1, 7.1 and ISO 14289-2, 8.2.2
// require every piece of content to be tagged or an artifact. The element's
// object reference becomes a marked-content reference, and the element takes a
// PrintField role, which ISO 32000-1, Table 340 asks of a Form element holding
// content (ISO 14289-1, 7.18.4 test 2).
func TestWriter_FullRewriteTagsTheDrawnSignature(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := taggedSignedPDF()
	out, dropped := writeSigned(ctx, t, in, func(*Writer) []WriteOption { return []WriteOption{WithFullRewrite()} })

	if want := []DroppedSignature{{Field: "approval"}}; !slices.Equal(dropped, want) {
		t.Errorf("DroppedSignatures = %v, want %v", dropped, want)
	}
	content := pageContent(ctx, t, out, 1)
	if !strings.Contains(content, "/Form <</MCID 1>> BDC") {
		t.Errorf("the drawn signature is not tagged as MCID 1 of a Form element: %q", content)
	}
	if !strings.Contains(pageText(ctx, t, out, 1), "Signed by Jane") {
		t.Error("the signature's appearance is not drawn into the page")
	}
	for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
		before, after := uaRuleIDs(ctx, t, in, profile), uaRuleIDs(ctx, t, out, profile)
		for _, id := range after {
			if !slices.Contains(before, id) {
				t.Errorf("%s: the rewrite fails %s, which the input passes (input %v, output %v)", profile, id, before, after)
			}
		}
	}
}
