package remediation

import (
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// indirectCS0PDF stores the page's /CS0 colour space as an INDIRECT array
// (object 8), which dedupPDF does not: it writes /CS0 inline as
// [/ICCBased 6 0 R]. That difference decides everything here. With an inline
// array the `ref, isRef := entry.(ast.PdfIndirectRef)` test in
// dedupICCInResources is false, the AllocAt at the array's own object number
// never fires, and the code falls through to rewriting the /ColorSpace dict,
// the /Resources dict or the page instead. Both existing suites assert
// alloc.Pending()[3], the PAGE, which is exactly that fallback.
//
// Object 5 is the OutputIntent profile, 6 the matching page profile, 8 the
// indirect colour-space array, 9 a DIFFERENT profile used by the peer's edit.
func indirectCS0PDF(t *testing.T) []byte {
	t.Helper()

	const content = "q /CS0 cs 0 0 0 1 sc 10 10 100 100 re f Q\n"
	cmyk := deflateAt(t, cmykICCProfile, zlib.BestCompression)
	srgb := deflateAt(t, sRGBICCProfile, zlib.BestCompression)

	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R" +
			" /Resources << /ColorSpace << /CS0 8 0 R >> >> >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (Coated FOGRA39)" +
			" /DestOutputProfile 5 0 R >>",
		iccObj(cmyk, 4),
		iccObj(cmyk, 4),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"[/ICCBased 6 0 R]",
		iccObj(srgb, 3),
	})
}

// TestDedupICCInResources_KeepsAnEarlierFixersPendingColourSpaceWrite pins that
// the redundant-ICC substitution judges the colour space an earlier fixer
// already queued, not the on-disk copy.
//
// [dedupICCInResources] writes the substitute back at the colour-space array's
// own object number. It used to read that array with document.ResolveArray,
// consulting no alloc-aware helper, so an earlier fixer's edit to the same
// colour-space object was invisible to it. It now reads the array with
// liveArray.
//
// Peer and ordering are measured: colour-space objects are written earlier by
// output-intent and output-intent-cleanup, and by graphics, transparency and
// group-cs. icc-dedup follows all of them in the registry.
//
// THE DEFECT SHOWED UP AS AN UNWANTED SUBSTITUTION, NOT A LOST EDIT, which is
// why the test is shaped differently from the other clobber tests. Reading the
// document copy, the fixer still saw the CMYK profile that duplicates the
// OutputIntent and collapsed /CS0 to /DeviceCMYK. Reading the allocator, it
// sees the peer's sRGB profile, finds no duplicate, and correctly leaves the
// colour space alone. So the failure replaced a colour space the document no
// longer used.
//
// The CONTROL runs the same fixture unseeded to prove the substitution path
// actually reaches object 8; without it, "nothing was written" could mean the
// fixture never fired rather than that the repair declined.
func TestDedupICCInResources_KeepsAnEarlierFixersPendingColourSpaceWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const csObj = 8

	// CONTROL: unseeded, the fixer must substitute at object 8 itself.
	doc, alloc := newTestDoc(t, indirectCS0PDF(t))
	if _, err := fixICCDedup(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("control fixICCDedup: %v", err)
	}
	control, present := alloc.Pending()[csObj]
	if !present {
		t.Fatalf("control: object %d was never written, so the fixture does not reach "+
			"the substitution and this test proves nothing; pending = %v",
			csObj, alloc.Pending())
	}
	if name, _ := control.(ast.PdfName); name != "DeviceCMYK" {
		t.Fatalf("control: object %d became %v, want /DeviceCMYK; the fixture does not "+
			"exercise the path under test", csObj, control)
	}

	// SEEDED: an earlier colour-space fixer repointed /CS0 at a DIFFERENT
	// profile. That profile does not duplicate the OutputIntent, so a
	// correctly alloc-aware read leaves the colour space untouched.
	doc2, alloc2 := newTestDoc(t, indirectCS0PDF(t))
	peer := ast.PdfArray{ast.PdfName("ICCBased"), ast.PdfIndirectRef{ObjectNum: 9}}
	if !alloc2.AllocAt(csObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", csObj)
	}

	if _, err := fixICCDedup(ctx, alloc2, doc2, Options{}, nil, ""); err != nil {
		t.Fatalf("fixICCDedup: %v", err)
	}

	got := alloc2.Pending()[csObj]
	if name, isName := got.(ast.PdfName); isName && name == "DeviceCMYK" {
		t.Errorf("object %d was collapsed to /DeviceCMYK: dedupICCInResources read the "+
			"on-disk array, matched the OutputIntent profile the document no longer "+
			"uses, and discarded the earlier fixer's write; pending = %v", csObj, got)
	}
}

// iccDedupObjects returns objects 1 to 7 of the ICC-dedup fixtures below: the
// catalog, the page tree, a page whose /Resources value is resources, the
// OutputIntent, its CMYK profile at object 5, the same profile again at object
// 6 for the page to use, and the page content at object 7.
func iccDedupObjects(t *testing.T, resources string) []string {
	t.Helper()

	const content = "q /CS0 cs 0 0 0 1 sc 10 10 100 100 re f Q\n"
	cmyk := deflateAt(t, cmykICCProfile, zlib.BestCompression)
	return []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R /Resources " + resources + " >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (Coated FOGRA39)" +
			" /DestOutputProfile 5 0 R >>",
		iccObj(cmyk, 4),
		iccObj(cmyk, 4),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
}

// TestDedupICCInResources_KeepsAnEarlierFixersPendingColourSpaceDictWrite pins
// that the redundant-ICC substitution rebuilds an indirect /ColorSpace
// dictionary from the copy an earlier fixer queued, not from the document.
//
// The peer is real and adjacent: devicen runs two entries ahead of icc-dedup
// and writes the /Colorants repair of an inline DeviceN into the same
// dictionary at its own object number. Object 8 holds both colour spaces
// inline, so each fixer rebuilds the whole dictionary.
func TestDedupICCInResources_KeepsAnEarlierFixersPendingColourSpaceDictWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	objs := append(iccDedupObjects(t, "<< /ColorSpace 8 0 R >>"),
		"<< /CS0 [/ICCBased 6 0 R] /CSN [/DeviceN [/Cyan /SPOT] /DeviceCMYK 9 0 R] >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [1 1 0 0] /N 1 >>",
	)
	doc, alloc := newTestDoc(t, assemblePDF(objs))

	const csDictObj = 8

	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}
	seeded, ok := alloc.Pending()[csDictObj].(ast.PdfDict)
	if !ok || !hasColorants(seeded["CSN"]) {
		t.Fatalf("control: devicen did not queue the /Colorants repair at object %d, so there "+
			"is no earlier edit to lose; pending = %v", csDictObj, alloc.Pending())
	}

	if _, err := fixICCDedup(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixICCDedup: %v", err)
	}

	got, ok := alloc.Pending()[csDictObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d is no longer a pending dict: %T", csDictObj, alloc.Pending()[csDictObj])
	}
	if name, _ := got["CS0"].(ast.PdfName); name != "DeviceCMYK" {
		t.Fatalf("/CS0 in object %d is %v, want /DeviceCMYK: the substitution did not rewrite "+
			"the dictionary, so this test proves nothing about clobbering", csDictObj, got["CS0"])
	}
	if !hasColorants(got["CSN"]) {
		t.Errorf("/CSN lost its /Colorants: dedupICCInResources rebuilt the /ColorSpace "+
			"dictionary from the document and discarded devicen's write; /CSN = %v", got["CSN"])
	}
}

// TestDedupICCInResources_KeepsAnEarlierFixersPendingResourcesWrite pins that
// the redundant-ICC substitution rebuilds an indirect /Resources dictionary
// from the copy output-intent queued, and lands where the output reads it.
//
// The /Resources dictionary is object 8. With the /ColorSpace dictionary inline
// in it, reading object 8 from the document drops the /DefaultRGB output-intent
// injected. With the /ColorSpace dictionary at object 9, output-intent inlines
// a copy of it into object 8, so following the document's reference to object 9
// writes the substitution where the output no longer looks.
func TestDedupICCInResources_KeepsAnEarlierFixersPendingResourcesWrite(t *testing.T) {
	t.Parallel()

	const cs = "<< /CS0 [/ICCBased 6 0 R] >>"
	for _, tc := range []struct {
		name  string
		extra []string
		res   string
	}{
		{name: "inline colour space dict", res: "<< /ColorSpace " + cs + " >>"},
		{name: "indirect colour space dict", res: "<< /ColorSpace 9 0 R >>", extra: []string{cs}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			objs := append(iccDedupObjects(t, "8 0 R"), tc.res)
			objs = append(objs, tc.extra...)
			doc, alloc := newTestDoc(t, assemblePDF(objs))

			const resObj = 8

			if n := injectTestDefaultRGB(t, alloc, doc); n == 0 {
				t.Fatal("control: output-intent patched no page, so there is no earlier edit to lose")
			}
			if _, has := liveResourceColourSpace(t, alloc, doc, resObj)["DefaultRGB"]; !has {
				t.Fatalf("control: output-intent did not queue /DefaultRGB for /Resources %d; pending = %v",
					resObj, alloc.Pending())
			}

			if _, err := fixICCDedup(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixICCDedup: %v", err)
			}

			got := liveResourceColourSpace(t, alloc, doc, resObj)
			if _, has := got["DefaultRGB"]; !has {
				t.Errorf("/DefaultRGB is gone: dedupICCInResources rebuilt /Resources %d from the "+
					"document and discarded output-intent's write; /ColorSpace = %v", resObj, got)
			}
			if name, _ := got["CS0"].(ast.PdfName); name != "DeviceCMYK" {
				t.Errorf("/CS0 is %v in the /ColorSpace the output reads, want /DeviceCMYK: the "+
					"substitution was written where the pending /Resources no longer points", got["CS0"])
			}
		})
	}
}

// TestDedupICCInXObjects_RewritesTheFormAsAStream pins that the redundant-ICC
// substitution inside a Form XObject whose /Resources and /ColorSpace are both
// inline writes the form back as a STREAM, keeping its content, and keeps an
// earlier fixer's pending rewrite of that form.
//
// dedupICCInResources falls back to rewriting the dictionary that carries
// /Resources, which for a form is the stream's own dictionary. It wrote that
// dictionary back bare at the form's object number, so the output replaced a
// Form XObject (ISO 32000-1, 8.10.1: a form is a stream) with a dictionary and
// dropped its content. It also read the form from the document, so a form
// output-intent had already given a /DefaultRGB lost it.
func TestDedupICCInXObjects_RewritesTheFormAsAStream(t *testing.T) {
	t.Parallel()

	const formContent = "q /CS0 cs 0 0 0 1 sc 0 0 10 10 re f Q\n"
	form := fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 100]"+
		" /Resources << /ColorSpace << /CS0 [/ICCBased 6 0 R] >> >> /Length %d >>\nstream\n%s\nendstream",
		len(formContent), formContent)

	for _, tc := range []struct {
		name string
		peer bool
	}{
		{name: "alone", peer: false},
		{name: "after output-intent rewrote the form", peer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			objs := append(iccDedupObjects(t, "<< /XObject << /Fm0 8 0 R >> >>"), form)
			doc, alloc := newTestDoc(t, assemblePDF(objs))

			const formObj = 8

			if tc.peer {
				injectTestDefaultRGB(t, alloc, doc)
				seeded, ok := alloc.Pending()[formObj].(ast.PdfStream)
				if !ok || !formColourSpaceHas(t, seeded.Dict, "DefaultRGB") {
					t.Fatalf("control: output-intent did not queue /DefaultRGB in form %d, so there "+
						"is no earlier edit to lose; pending = %v", formObj, alloc.Pending()[formObj])
				}
			}

			if _, err := fixICCDedup(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixICCDedup: %v", err)
			}

			var dict ast.PdfDict
			switch got := alloc.Pending()[formObj].(type) {
			case ast.PdfStream:
				dict = got.Dict
				body, err := got.Bytes(ctx)
				if err != nil {
					t.Fatalf("reading the rewritten form body: %v", err)
				}
				if string(body) != formContent {
					t.Errorf("the form body is %q, want %q", body, formContent)
				}
			case ast.PdfDict:
				dict = got
				t.Errorf("form %d was rewritten as a %T: a Form XObject is a stream, and its "+
					"content is gone", formObj, got)
			default:
				t.Fatalf("form %d was never rewritten (%T), so the substitution path is not "+
					"reached and this test proves nothing", formObj, got)
			}
			cs := formColourSpace(t, dict)
			if name, _ := cs["CS0"].(ast.PdfName); name != "DeviceCMYK" {
				t.Fatalf("/CS0 in form %d is %v, want /DeviceCMYK: the substitution did not "+
					"rewrite the form", formObj, cs["CS0"])
			}
			if _, has := cs["DefaultRGB"]; tc.peer && !has {
				t.Errorf("/DefaultRGB is gone from form %d: dedupICCInXObjects read the form from "+
					"the document and discarded output-intent's write; /ColorSpace = %v", formObj, cs)
			}
		})
	}
}

// formColourSpace returns the inline /Resources /ColorSpace dictionary of a
// form XObject dictionary.
func formColourSpace(t *testing.T, dict ast.PdfDict) ast.PdfDict {
	t.Helper()

	res, ok := dict["Resources"].(ast.PdfDict)
	if !ok {
		t.Fatalf("form /Resources is %T, want an inline dict", dict["Resources"])
	}
	cs, ok := res["ColorSpace"].(ast.PdfDict)
	if !ok {
		t.Fatalf("form /ColorSpace is %T, want an inline dict", res["ColorSpace"])
	}
	return cs
}

// formColourSpaceHas reports whether a form XObject dictionary's inline
// /ColorSpace defines key.
func formColourSpaceHas(t *testing.T, dict ast.PdfDict, key ast.PdfName) bool {
	t.Helper()

	_, has := formColourSpace(t, dict)[key]
	return has
}
