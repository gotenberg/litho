package remediation

import (
	"compress/zlib"
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// pdfxIntentDoc builds a one-page document whose catalog lists the given
// output intents, objects 4 onwards, followed by the given profile streams.
// The page paints DeviceCMYK and DeviceRGB.
func pdfxIntentDoc(t *testing.T, oiArray string, objs ...string) []byte {
	t.Helper()
	return assemblePDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents " + oiArray + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}, objs...))
}

// TestAddOutputIntent_KeepsAPDFXIntent pins that adding the PDF/A output
// intent keeps a GTS_PDFX intent the catalog lists, and that every intent then
// names one destination profile object.
//
// ISO 32000-1, 14.11.5 admits several output intents, and ISO 19005-2, 6.2.3
// t2 requires the intents carrying /DestOutputProfile to name the same
// indirect object. A PDF/X CMYK profile PDF/A admits covers DeviceCMYK, so the
// new GTS_PDFA1 intent names it with the PDF/X output condition and the PDF/X
// intent is left as it was. Replacing the whole array dropped the PDF/X
// intent.
func TestAddOutputIntent_KeepsAPDFXIntent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cmyk := deflateAt(t, cmykICCProfile, zlib.BestCompression)
	doc, alloc := newTestDoc(t, pdfxIntentDoc(t, "[4 0 R]",
		"<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (FOGRA39) /RegistryName (http://www.color.org) /DestOutputProfile 5 0 R >>",
		iccObj(cmyk, 4),
	))
	const pdfxIntent, pdfxProfile = 4, 5

	violations := []compliance.Violation{{Context: "DeviceCMYK used without OutputIntent"}}
	fixes, err := fixDeviceColorIntent(ctx, alloc, doc, violations, 2)
	if err != nil {
		t.Fatalf("fixDeviceColorIntent: %v", err)
	}
	const keptFix = "kept OutputIntent[1] (/S /GTS_PDFX) beside the GTS_PDFA1 intent, which shares its destination profile"
	if !slices.Contains(fixes, keptFix) {
		t.Errorf("fixes = %q, want %q", fixes, keptFix)
	}

	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
	arr := liveArray(ctx, alloc, doc, catalog["OutputIntents"])
	if len(arr) != 2 {
		t.Fatalf("catalog /OutputIntents = %v, want the PDF/A intent and the PDF/X one", arr)
	}
	pdfa := resolveFromAllocOrDoc(ctx, alloc, doc, arr[0].(ast.PdfIndirectRef))
	if pdfa["S"] != ast.PdfName("GTS_PDFA1") {
		t.Errorf("OutputIntent[0] /S = %v, want GTS_PDFA1", pdfa["S"])
	}
	want := ast.PdfIndirectRef{ObjectNum: pdfxProfile}
	if got := pdfa["DestOutputProfile"]; got != want {
		t.Errorf("GTS_PDFA1 /DestOutputProfile = %v, want %v, the PDF/X intent's", got, want)
	}
	if got := pdfa["OutputConditionIdentifier"]; got != ast.PdfString("FOGRA39") {
		t.Errorf("GTS_PDFA1 /OutputConditionIdentifier = %v, want the PDF/X condition", got)
	}
	if arr[1] != (ast.PdfIndirectRef{ObjectNum: pdfxIntent}) {
		t.Errorf("OutputIntent[1] = %v, want the PDF/X intent %d 0 R", arr[1], pdfxIntent)
	}
	if _, pending := alloc.Pending()[pdfxIntent]; pending {
		t.Errorf("the PDF/X intent was rewritten, want it left as it was")
	}
}

// TestAddOutputIntent_UnifiesAPDFXIntentItCannotReuse pins that a PDF/X intent
// whose profile does not cover the colour space the PDF/A intent needs stays,
// pointed at the new PDF/A profile (ISO 19005-2, 6.2.3 t2), keeping its own
// /OutputConditionIdentifier.
func TestAddOutputIntent_UnifiesAPDFXIntentItCannotReuse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cmyk := deflateAt(t, cmykICCProfile, zlib.BestCompression)
	doc, alloc := newTestDoc(t, pdfxIntentDoc(t, "[4 0 R]",
		"<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (FOGRA39) /DestOutputProfile 5 0 R >>",
		iccObj(cmyk, 4),
	))
	const pdfxIntent = 4

	violations := []compliance.Violation{{Context: "DeviceRGB used without OutputIntent"}}
	if _, err := fixDeviceColorIntent(ctx, alloc, doc, violations, 2); err != nil {
		t.Fatalf("fixDeviceColorIntent: %v", err)
	}

	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
	arr := liveArray(ctx, alloc, doc, catalog["OutputIntents"])
	if len(arr) != 2 {
		t.Fatalf("catalog /OutputIntents = %v, want the PDF/A intent and the PDF/X one", arr)
	}
	pdfa := resolveFromAllocOrDoc(ctx, alloc, doc, arr[0].(ast.PdfIndirectRef))
	pdfx := resolveFromAllocOrDoc(ctx, alloc, doc, arr[1].(ast.PdfIndirectRef))
	if arr[1] != (ast.PdfIndirectRef{ObjectNum: pdfxIntent}) || pdfx["S"] != ast.PdfName("GTS_PDFX") {
		t.Fatalf("OutputIntent[1] = %v (%v), want the PDF/X intent", arr[1], pdfx)
	}
	if pdfa["DestOutputProfile"] != pdfx["DestOutputProfile"] {
		t.Errorf("the intents name /DestOutputProfile %v and %v, want one object",
			pdfa["DestOutputProfile"], pdfx["DestOutputProfile"])
	}
	if data := decodedProfile(ctx, alloc, doc, pdfa["DestOutputProfile"]); len(data) < 20 || string(data[16:20]) != "RGB " {
		t.Errorf("the PDF/A intent's profile is not an RGB one")
	}
	if got := pdfx["OutputConditionIdentifier"]; got != ast.PdfString("FOGRA39") {
		t.Errorf("PDF/X /OutputConditionIdentifier = %v, want FOGRA39 kept", got)
	}
}

// TestAddOutputIntent_ReplacesAProfilelessPDFAIntent pins that a GTS_PDFA1
// intent naming no profile is replaced by the new one, while the PDF/X intent
// beside it stays: two GTS_PDFA1 intents would name two PDF/A output
// conditions.
func TestAddOutputIntent_ReplacesAProfilelessPDFAIntent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	srgb := deflateAt(t, sRGBICCProfile, zlib.BestCompression)
	doc, alloc := newTestDoc(t, pdfxIntentDoc(t, "[4 0 R 5 0 R]",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) >>",
		"<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (sRGB IEC61966-2.1) /DestOutputProfile 6 0 R >>",
		iccObj(srgb, 3),
	))
	const pdfxIntent, pdfxProfile = 5, 6

	violations := []compliance.Violation{{Context: "DeviceRGB used without OutputIntent"}}
	if _, err := fixDeviceColorIntent(ctx, alloc, doc, violations, 1); err != nil {
		t.Fatalf("fixDeviceColorIntent: %v", err)
	}

	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
	arr := liveArray(ctx, alloc, doc, catalog["OutputIntents"])
	if len(arr) != 2 {
		t.Fatalf("catalog /OutputIntents = %v, want the new PDF/A intent and the PDF/X one", arr)
	}
	if arr[0] == (ast.PdfIndirectRef{ObjectNum: 4}) {
		t.Errorf("OutputIntent[0] is the profile-less GTS_PDFA1 intent, want it replaced")
	}
	pdfa := resolveFromAllocOrDoc(ctx, alloc, doc, arr[0].(ast.PdfIndirectRef))
	if got, want := pdfa["DestOutputProfile"], (ast.PdfIndirectRef{ObjectNum: pdfxProfile}); got != want {
		t.Errorf("GTS_PDFA1 /DestOutputProfile = %v, want %v, the PDF/X intent's sRGB profile", got, want)
	}
	if arr[1] != (ast.PdfIndirectRef{ObjectNum: pdfxIntent}) {
		t.Errorf("OutputIntent[1] = %v, want the PDF/X intent %d 0 R", arr[1], pdfxIntent)
	}
}
