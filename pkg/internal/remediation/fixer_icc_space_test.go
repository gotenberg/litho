package remediation

import (
	"compress/zlib"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/models"
)

// iccSpacePDF is a one-page document painting in ICCBased colour spaces over
// version 4 profiles: the page fills in /CS0 over object 5 and paints image
// object 6, whose space names object 5 too, and form object 7, which fills in
// its own /CS1 over object 8. Each profile is Flate-encoded with /N n.
func iccSpacePDF(t *testing.T, profile []byte, n int) []byte {
	t.Helper()
	enc := deflateAt(t, profile, zlib.BestCompression)
	const form = "/CS1 cs 0.25 sc 0 0 5 5 re f\n"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R" +
			" /Resources << /ColorSpace << /CS0 [/ICCBased 5 0 R] >> /XObject << /Im0 6 0 R /Fm0 7 0 R >> >> >>",
		streamObj("", "/CS0 cs 0.5 sc 0 0 10 10 re f q 10 0 0 10 20 20 cm /Im0 Do Q /Fm0 Do\n"),
		iccObj(enc, n),
		streamObj(" /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace [/ICCBased 5 0 R]", string(make([]byte, n))),
		streamObj(" /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /ColorSpace << /CS1 [/ICCBased 8 0 R] >> >>", form),
		iccObj(enc, n),
	})
}

// iccSpaceVersionViolations returns the ISO 19005-1, 6.2.3.2 t1 violations of
// doc at PDF/A-1b.
func iccSpaceVersionViolations(t *testing.T, doc *document.Document) []compliance.Violation {
	t.Helper()
	errs, err := compliance.ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	var out []compliance.Violation
	for _, e := range errs {
		if v, ok := e.(compliance.Violation); ok && v.Rule == compliance.Rule_19005_1_6_2_3_2_t01 {
			out = append(out, v)
		}
	}
	return out
}

// TestRemediatePDFA1_ReencodesV4ICCBasedProfilesAsV2 pins that a PDF/A-1
// remediation re-encodes the version 4 profile of every ICCBased colour space
// as version 2 in place, the page's, the image's and the form's, so that the
// page and the image keep sharing one profile stream, each converting colours
// as before, and the output passes ISO 19005-1, 6.2.3.2 t1. Only the output
// intent's destination profile was re-encoded, so the violation survived
// every remediation.
func TestRemediatePDFA1_ReencodesV4ICCBasedProfilesAsV2(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := sGrayV4Profile()
	data := iccSpacePDF(t, src, 1)
	doc, _ := newTestDoc(t, data)
	if len(iccSpaceVersionViolations(t, doc)) == 0 {
		t.Fatal("control: the version 4 profiles do not fail 6.2.3.2 t1 at PDF/A-1b")
	}

	out := remediateAndReopen(t, data, models.PDFA1b, Options{})
	if vs := iccSpaceVersionViolations(t, out); len(vs) != 0 {
		t.Fatalf("after remediation: %v", vs)
	}

	before, err := icc.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := before.NewTransform(icc.RelativeColorimetric)
	if err != nil {
		t.Fatal(err)
	}
	profileOf := func(t *testing.T, csObj ast.PdfObject) (ast.PdfIndirectRef, []byte) {
		t.Helper()
		arr, err := document.ResolveArray(ctx, out, csObj)
		if err != nil || len(arr) != 2 {
			t.Fatalf("colour space %v, %v; want an ICCBased array", arr, err)
		}
		ref, _ := arr[1].(ast.PdfIndirectRef)
		stream, err := document.ResolveStream(ctx, out, arr[1])
		if err != nil {
			t.Fatalf("no profile stream: %v", err)
		}
		got, err := out.DecodeStream(ctx, stream)
		if err != nil {
			t.Fatalf("decoding the profile: %v", err)
		}
		if got[8] != 2 || string(got[16:20]) != "GRAY" {
			t.Fatalf("profile version %d, colour space %q; want a version 2 grey profile", got[8], got[16:20])
		}
		after, err := icc.Parse(got)
		if err != nil {
			t.Fatal(err)
		}
		ta, err := after.NewTransform(icc.RelativeColorimetric)
		if err != nil {
			t.Fatal(err)
		}
		for v := range 256 {
			r1, g1, b1 := tb.Convert([]byte{byte(v)})
			r2, g2, b2 := ta.Convert([]byte{byte(v)})
			if r1 != r2 || g1 != g2 || b1 != b2 {
				t.Fatalf("grey %d: %d,%d,%d before, %d,%d,%d after", v, r1, g1, b1, r2, g2, b2)
			}
		}
		return ref, got
	}

	res := firstPage(t, ctx, out).Resources
	cs, _ := document.ResolveDict(ctx, out, res["ColorSpace"])
	xo, _ := document.ResolveDict(ctx, out, res["XObject"])
	pageRef, _ := profileOf(t, cs["CS0"])
	im, err := document.ResolveStream(ctx, out, xo["Im0"])
	if err != nil {
		t.Fatal(err)
	}
	imRef, _ := profileOf(t, im.Dict["ColorSpace"])
	if pageRef != imRef {
		t.Errorf("page profile %v, image profile %v; want the one shared stream", pageRef, imRef)
	}
	fm, err := document.ResolveStream(ctx, out, xo["Fm0"])
	if err != nil {
		t.Fatal(err)
	}
	fmRes, _ := document.ResolveDict(ctx, out, fm.Dict["Resources"])
	fmCS, _ := document.ResolveDict(ctx, out, fmRes["ColorSpace"])
	profileOf(t, fmCS["CS1"])
}

// TestFixICCSpaceV2_KeepsV4ProfileAfterPDFA1 pins that the re-encoding is a
// PDF/A-1 repair alone: ISO 19005-2, 6.2.4.2 admits a version 4 profile.
func TestFixICCSpaceV2_KeepsV4ProfileAfterPDFA1(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, iccSpacePDF(t, sGrayV4Profile(), 1))
	fixes, err := fixICCSpaceV2(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixICCSpaceV2: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("PDF/A-2b: fixes %v, pending %v; want the version 4 profiles kept", fixes, alloc.Pending())
	}
}

// TestFixICCSpaceV2_KeepsUnconvertibleV4Profile pins that a version 4
// profile whose colour lives in a table version 2 has no type for stays as it
// is at PDF/A-1, with its violation: neither a resampled table nor a stock
// profile converts the content's colours as the source does.
func TestFixICCSpaceV2_KeepsUnconvertibleV4Profile(t *testing.T) {
	t.Parallel()

	atob := []byte("A2B0mAB \x00\x00\x00\x00\x01\x03\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	data := iccSpacePDF(t, iccTestProfile(0x04200000, "mntr", "GRAY", atob), 1)
	doc, alloc := newTestDoc(t, data)
	fixes, err := fixICCSpaceV2(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixICCSpaceV2: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("fixes %v, pending %v; want the profiles kept", fixes, alloc.Pending())
	}
	if len(iccSpaceVersionViolations(t, remediateAndReopen(t, data, models.PDFA1b, Options{}))) == 0 {
		t.Error("the unconvertible profile no longer fails 6.2.3.2 t1 after remediation")
	}
}
