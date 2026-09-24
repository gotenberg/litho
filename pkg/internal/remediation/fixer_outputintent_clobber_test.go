package remediation

import (
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestPatchResourceColorSpace_KeepsTheDedupOutputIntentQueued pins that the
// default colour space injection starts from the /ColorSpace dictionary the
// same fixer's dedup pass already rewrote.
//
// fixOutputIntent adds an output intent, then substitutes every ICCBased space
// that duplicates it, then injects the default colour space for the other
// device family. The substitution is written at the indirect /ColorSpace
// dictionary, object 4 here. patchResourceColorSpace then read that dictionary
// from the document and inlined the copy, plus the default, into the
// /Resources it rewrites, so the output pointed at a /CS0 still duplicating the
// output intent (ISO 19005-4, 6.2.4.2 test 3) and object 4's substitution was
// orphaned.
//
// The page's /CS0 carries the sRGB profile this package embeds, so the output
// intent fixOutputIntent adds is exactly the one it duplicates.
func TestPatchResourceColorSpace_KeepsTheDedupOutputIntentQueued(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const content = "q /CS0 cs 1 0 0 sc 10 10 100 100 re f Q\n"
	srgb := deflateAt(t, sRGBICCProfile, zlib.BestCompression)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /ColorSpace 4 0 R >> >>",
		"<< /CS0 [/ICCBased 6 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		iccObj(srgb, 3),
	}))

	const pageObj, csDictObj = 3, 4

	if _, err := fixOutputIntent(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixOutputIntent: %v", err)
	}

	// The dedup pass must have reached object 4, or nothing below is about a
	// lost write.
	queued, ok := alloc.Pending()[csDictObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("control: the dedup pass never rewrote object %d, so the fixture does not "+
			"reach the path under test; pending = %v", csDictObj, alloc.Pending())
	}
	if name, _ := queued["CS0"].(ast.PdfName); name != "DeviceRGB" {
		t.Fatalf("control: object %d carries /CS0 %v, want /DeviceRGB", csDictObj, queued["CS0"])
	}

	page := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: pageObj})
	res, ok := page["Resources"].(ast.PdfDict)
	if !ok {
		t.Fatalf("page /Resources is %T, want an inline dict", page["Resources"])
	}
	cs := res["ColorSpace"]
	var live ast.PdfDict
	switch v := cs.(type) {
	case ast.PdfDict:
		live = v
	case ast.PdfIndirectRef:
		live = resolveFromAllocOrDoc(ctx, alloc, doc, v)
	}
	if live == nil {
		t.Fatalf("page /ColorSpace %v did not resolve", cs)
	}
	if _, has := live["DefaultCMYK"]; !has {
		t.Fatalf("the page /ColorSpace carries no /DefaultCMYK: the injection did not run, so "+
			"this test proves nothing; /ColorSpace = %v", live)
	}
	if name, _ := live["CS0"].(ast.PdfName); name != "DeviceRGB" {
		t.Errorf("/CS0 is %v in the /ColorSpace the output reads, want /DeviceRGB: "+
			"patchResourceColorSpace copied object %d from the document and discarded the "+
			"dedup pass's write", live["CS0"], csDictObj)
	}
}

// TestRecurseColorSpacePatch_KeepsTheDedupOutputIntentQueuedInAForm pins that
// the default colour space injection into a Form XObject starts from the form
// the same fixer's dedup pass already rewrote.
//
// fixOutputIntent's dedup pass rewrites form 4, whose /Resources and
// /ColorSpace are inline, to substitute its duplicated /CS0. The injection then
// walked the page's /XObject entries, read form 4 from the document and wrote
// it back with the default added, restoring the ICCBased space that duplicates
// the output intent (ISO 19005-4, 6.2.4.2 test 3).
func TestRecurseColorSpacePatch_KeepsTheDedupOutputIntentQueuedInAForm(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const formContent = "q /CS0 cs 1 0 0 sc 0 0 10 10 re f Q\n"
	srgb := deflateAt(t, sRGBICCProfile, zlib.BestCompression)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Fm0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 100]"+
			" /Resources << /ColorSpace << /CS0 [/ICCBased 5 0 R] >> >> /Length %d >>\nstream\n%s\nendstream",
			len(formContent), formContent),
		iccObj(srgb, 3),
	}))

	const formObj = 4

	if _, err := fixOutputIntent(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixOutputIntent: %v", err)
	}

	form, ok := alloc.Pending()[formObj].(ast.PdfStream)
	if !ok {
		t.Fatalf("form %d is pending as %T, want a stream; pending = %v",
			formObj, alloc.Pending()[formObj], alloc.Pending())
	}
	body, err := form.Bytes(ctx)
	if err != nil || string(body) != formContent {
		t.Fatalf("form %d body is %q (%v), want %q", formObj, body, err, formContent)
	}
	cs := formColourSpace(t, form.Dict)
	if _, has := cs["DefaultCMYK"]; !has {
		t.Fatalf("form %d carries no /DefaultCMYK: the injection never rewrote it, so this "+
			"test proves nothing; /ColorSpace = %v", formObj, cs)
	}
	if name, _ := cs["CS0"].(ast.PdfName); name != "DeviceRGB" {
		t.Errorf("/CS0 in form %d is %v, want /DeviceRGB: recurseColorSpacePatch rebuilt the "+
			"form from the document and discarded the dedup pass's write", formObj, cs["CS0"])
	}
}
