package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// annotColorSquare is a Square annotation carrying both colour arrays and a
// conforming /F, so 6.5.3 test 3 is the only annotation rule it trips.
const annotColorSquare = "<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 /C [1 0 0] /IC [0 0 1] >>"

// contentStream renders a content stream object body.
func contentStream(ops string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ops), ops)
}

// annotColorPaintPDF builds a one-page document with no output intent that
// paints with ops and carries [annotColorSquare] as object 4. Object 5 is the
// content stream.
func annotColorPaintPDF(ops string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R /Annots [4 0 R] >>",
		annotColorSquare,
		contentStream(ops),
	)
}

// annotColorIntentPDF builds a one-page document whose catalog carries a
// GTS_PDFA1 output intent (object 5) with the given destination profile
// (object 6), and whose page carries annots as its /Annots value. extra objects
// follow from number 7.
func annotColorIntentPDF(t *testing.T, profile []byte, n int, annots string, extra ...string) []byte {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [5 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots " + annots + " >>",
		annotColorSquare,
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (test) /DestOutputProfile 6 0 R >>",
		iccObj(deflateAt(t, profile, zlib.BestCompression), n),
	}
	return assemblePDF(append(objs, extra...))
}

// countAnnotColorViolations validates doc's source bytes at profile and counts
// ISO 19005-1, 6.5.3 test 3 violations: the control that proves the fixer was
// asked to act.
func countAnnotColorViolations(t *testing.T, data []byte, profile models.PDFAProfile) int {
	t.Helper()
	doc, _ := newTestDoc(t, data)
	errs, err := compliance.ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	n := 0
	for _, e := range errs {
		if v, ok := e.(compliance.Violation); ok && matchAnnotColor(v) {
			n++
		}
	}
	return n
}

// liveAnnot returns the annotation at objNum as the output will carry it: the
// pending rewrite when there is one, the source otherwise.
func liveAnnot(t *testing.T, data []byte, objNum int, pending map[int]ast.PdfObject) ast.PdfDict {
	t.Helper()
	if d, ok := pending[objNum].(ast.PdfDict); ok {
		return d
	}
	doc, alloc := newTestDoc(t, data)
	d := resolveFromAllocOrDoc(context.Background(), alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	if d == nil {
		t.Fatalf("object %d is not a dictionary", objNum)
	}
	return d
}

func TestMatchAnnotColor(t *testing.T) {
	t.Parallel()

	if !matchAnnotColor(compliance.Violation{Rule: compliance.Rule_19005_1_6_5_3_t03}) {
		t.Error("must match ISO 19005-1 6.5.3 test 3")
	}
	for _, r := range []compliance.Rule{
		compliance.Rule_19005_1_6_5_3_t01,
		compliance.Rule_19005_1_6_5_3_t02,
		compliance.Rule_19005_1_6_5_3_t04,
		compliance.Rule_19005_2_6_3_3_t01,
	} {
		if matchAnnotColor(compliance.Violation{Rule: r}) {
			t.Errorf("must not match %s %s test %d", r.Specification, r.Clause, r.TestNumber)
		}
	}
}

// TestAnnotColor_RunsAfterTheIntentFixers pins the ordering the decision
// depends on: output-intent installs the intent a source lacks, and corrupt-icc
// replaces a profile it rejects, so annot-color must read their writes.
func TestAnnotColor_RunsAfterTheIntentFixers(t *testing.T) {
	t.Parallel()

	index := func(name string) int {
		return slices.IndexFunc(registry, func(f fixer) bool { return f.name == name })
	}
	self := index("annot-color")
	if self < 0 {
		t.Fatal("annot-color is not registered")
	}
	for _, peer := range []string{"output-intent", "corrupt-icc", "output-intent-cleanup"} {
		if i := index(peer); i < 0 || i > self {
			t.Errorf("%s is at %d, annot-color at %d: it must run first", peer, i, self)
		}
	}
}

// TestRemediatePDFA1_DefersToTheRoundAfterAnAddedIntent pins the deferral: a
// CMYK intent output-intent installs in this pass may be replaced by an sRGB
// one in the next fixpoint round, so the arrays stay until a round starts from
// that intent. The removal the next round makes is pinned in pkg/pdf, which
// runs the fixpoint loop.
func TestRemediatePDFA1_DefersToTheRoundAfterAnAddedIntent(t *testing.T) {
	t.Parallel()

	data := annotColorPaintPDF("0 0 0 1 k 100 100 200 200 re f")
	if n := countAnnotColorViolations(t, data, models.PDFA1b); n == 0 {
		t.Fatal("control: the source reports no 6.5.3 test 3 violation, so the fixer is never asked")
	}

	doc, alloc := newTestDoc(t, data)
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA1b, Options{PreserveContent: true})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if !slices.Contains(report.Fixed, "added CMYK OutputIntent to catalog") {
		t.Fatalf("control: output-intent did not add a CMYK intent, so there is nothing to defer; fixed = %v", report.Fixed)
	}

	annot := liveAnnot(t, data, 4, alloc.Pending())
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := annot[key]; !has {
			t.Errorf("%s was removed in the pass that installed the intent; annotation = %v", key, annot)
		}
	}
}

// TestFixAnnotColor_DefersOnARebuiltDocument pins the second deferral:
// output-intent installs nothing on a document whose cross-reference table was
// rebuilt, and the intent it installs after the full rewrite may be sRGB, so
// the arrays stay until that round.
func TestFixAnnotColor_DefersOnARebuiltDocument(t *testing.T) {
	t.Parallel()

	data := annotColorIntentPDF(t, cmykICCProfile, 4, "[4 0 R]")
	cut := bytes.LastIndex(data, []byte("xref\n"))
	if cut < 0 {
		t.Fatal("fixture has no cross-reference table to drop")
	}
	broken := append(data[:cut:cut], "trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n"...)

	doc, alloc := newTestDoc(t, broken)
	if !doc.Rebuilt {
		t.Fatal("control: the document parsed without a rebuild, so there is nothing to defer")
	}
	fixes, err := fixAnnotColor(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("acted on a rebuilt document: fixes=%v pending=%d", fixes, len(alloc.Pending()))
	}
}

// TestFixAnnotColor_ActsOnTheIntentItsRoundStartsFrom is the other half of the
// deferral: once the CMYK intent is in the round's own source and nothing
// replaces it, the arrays go.
func TestFixAnnotColor_ActsOnTheIntentItsRoundStartsFrom(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotColorIntentPDF(t, cmykICCProfile, 4, "[4 0 R]"))
	fixes, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("expected one fix, got %v", fixes)
	}
	annot := pendingDict(t, alloc, 4)
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := annot[key]; has {
			t.Errorf("%s survived under a CMYK output intent; annotation = %v", key, annot)
		}
	}
	if annot["F"] != ast.PdfInt(4) || annot["Subtype"] != ast.PdfName("Square") {
		t.Errorf("the rewrite lost other keys: %v", annot)
	}
}

// TestRemediatePDFA1_KeepsAnnotColorUnderAnAddedRGBIntent is the negative
// control: DeviceRGB content makes output-intent install an sRGB intent in the
// same run, which permits both arrays, so they stay although the source had no
// intent and reported the violation.
func TestRemediatePDFA1_KeepsAnnotColorUnderAnAddedRGBIntent(t *testing.T) {
	t.Parallel()

	data := annotColorPaintPDF("1 0 0 rg 100 100 200 200 re f")
	if n := countAnnotColorViolations(t, data, models.PDFA1b); n == 0 {
		t.Fatal("control: the source reports no 6.5.3 test 3 violation, so the fixer is never asked")
	}

	doc, alloc := newTestDoc(t, data)
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA1b, Options{PreserveContent: true})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if !slices.Contains(report.Fixed, "added sRGB OutputIntent to catalog") {
		t.Fatalf("control: output-intent did not add an sRGB intent; fixed = %v", report.Fixed)
	}

	annot := liveAnnot(t, data, 4, alloc.Pending())
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := annot[key]; !has {
			t.Errorf("%s was removed although the output intent is RGB; annotation = %v", key, annot)
		}
	}
}

// TestRemediatePDFA1_DropsAnnotColorUnderASourceCMYKIntent covers a source
// that already carries a CMYK intent, where no other fixer touches the intent.
func TestRemediatePDFA1_DropsAnnotColorUnderASourceCMYKIntent(t *testing.T) {
	t.Parallel()

	data := annotColorIntentPDF(t, cmykICCProfile, 4, "[4 0 R]")
	if n := countAnnotColorViolations(t, data, models.PDFA1b); n == 0 {
		t.Fatal("control: the source reports no 6.5.3 test 3 violation")
	}

	doc, alloc := newTestDoc(t, data)
	if _, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA1b, Options{PreserveContent: true}); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	annot := liveAnnot(t, data, 4, alloc.Pending())
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := annot[key]; has {
			t.Errorf("%s survived under a CMYK output intent; annotation = %v", key, annot)
		}
	}
}

// TestRemediatePDFA2_LeavesAnnotColor pins the part scope: ISO 19005-2 has no
// counterpart to 6.5.3 test 3, so a PDF/A-2 run leaves the arrays alone under
// the same CMYK intent, and the fixer itself refuses a later part.
func TestRemediatePDFA2_LeavesAnnotColor(t *testing.T) {
	t.Parallel()

	data := annotColorIntentPDF(t, cmykICCProfile, 4, "[4 0 R]")

	doc, alloc := newTestDoc(t, data)
	if _, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true}); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	annot := liveAnnot(t, data, 4, alloc.Pending())
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := annot[key]; !has {
			t.Errorf("%s was removed under PDF/A-2b; annotation = %v", key, annot)
		}
	}

	doc, alloc = newTestDoc(t, data)
	fixes, err := fixAnnotColor(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("fixAnnotColor acted under PDF/A-2b: fixes=%v pending=%d", fixes, len(alloc.Pending()))
	}
}

// TestFixAnnotColor_ReadsAReplacedProfile pins the allocator read of the
// decision: corrupt-icc runs first and may replace the destination profile, so
// a CMYK source profile an earlier fixer replaced with sRGB permits the arrays.
func TestFixAnnotColor_ReadsAReplacedProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotColorIntentPDF(t, cmykICCProfile, 4, "[4 0 R]"))
	if !alloc.AllocAt(6, ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, sRGBICCProfile)) {
		t.Fatal("AllocAt(6) refused the earlier fixer's profile")
	}

	if _, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if _, queued := alloc.Pending()[4]; queued {
		t.Errorf("the annotation was rewritten although the pending profile is sRGB: %v", alloc.Pending()[4])
	}
}

// TestFixAnnotColor_UnreadableProfileIsNotRGB pins the conservative reading: a
// destination profile too short to name a colour space is not a known RGB one,
// and veraPDF's gOutputCS is then not "RGB " either.
func TestFixAnnotColor_UnreadableProfileIsNotRGB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotColorIntentPDF(t, []byte("short"), 3, "[4 0 R]"))
	if _, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	annot := pendingDict(t, alloc, 4)
	if _, has := annot["C"]; has {
		t.Errorf("/C survived under an unreadable profile: %v", annot)
	}
}

// TestFixAnnotColor_NonPDFA1IntentWithCMYKProfile pins that every carried
// profile counts: litho reads the first intent carrying one, so an RGB
// GTS_PDFA1 intent listed after a CMYK one does not make the arrays conform.
func TestFixAnnotColor_NonPDFA1IntentWithCMYKProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [7 0 R 5 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		annotColorSquare,
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (rgb) /DestOutputProfile 6 0 R >>",
		iccObj(deflateAt(t, sRGBICCProfile, zlib.BestCompression), 3),
		"<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (cmyk) /DestOutputProfile 8 0 R >>",
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestCompression), 4),
	})
	doc, alloc := newTestDoc(t, data)
	if _, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if _, has := pendingDict(t, alloc, 4)["C"]; has {
		t.Error("/C survived beside a CMYK destination profile")
	}
}

// TestFixAnnotColor_KeepsASoleRGBIntent is the control for the test above: the
// same RGB GTS_PDFA1 intent alone permits the arrays.
func TestFixAnnotColor_KeepsASoleRGBIntent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotColorIntentPDF(t, sRGBICCProfile, 3, "[4 0 R]"))
	fixes, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an RGB intent permits the arrays: fixes=%v pending=%d", fixes, len(alloc.Pending()))
	}
}

// TestFixAnnotColor_InlineAnnotInInlineArray covers an annotation written
// inline in an inline /Annots array: the only object to rewrite is the page.
func TestFixAnnotColor_InlineAnnotInInlineArray(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotColorIntentPDF(t, cmykICCProfile, 4, "["+annotColorSquare+"]"))
	fixes, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("expected one fix, got %v", fixes)
	}
	annots, _ := pendingDict(t, alloc, 3)["Annots"].(ast.PdfArray)
	if len(annots) != 1 {
		t.Fatalf("page /Annots = %v, want the one inline annotation", annots)
	}
	annot, ok := annots[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Annots[0] is %T, want the inline dictionary", annots[0])
	}
	if _, has := annot["C"]; has {
		t.Errorf("/C survived on the inline annotation: %v", annot)
	}
	if _, has := annot["IC"]; has {
		t.Errorf("/IC survived on the inline annotation: %v", annot)
	}
}

// TestFixAnnotColor_InlineAnnotInIndirectArray covers an inline annotation in
// an indirect /Annots array: the array is rewritten at its own object number
// and the page is left alone.
func TestFixAnnotColor_InlineAnnotInIndirectArray(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotColorIntentPDF(t, cmykICCProfile, 4, "7 0 R", "["+annotColorSquare+" 4 0 R]"))
	fixes, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if len(fixes) != 1 || fixes[0] != "removed /C and /IC colour arrays from 2 annotation(s): the PDF/A-1 output intent is not RGB" {
		t.Fatalf("expected both annotations counted, got %v", fixes)
	}
	arr, ok := alloc.Pending()[7].(ast.PdfArray)
	if !ok || len(arr) != 2 {
		t.Fatalf("object 7 = %v, want the rewritten two-entry array", alloc.Pending()[7])
	}
	if inline, _ := arr[0].(ast.PdfDict); inline == nil || inline["C"] != nil {
		t.Errorf("/Annots[0] still carries /C: %v", arr[0])
	}
	if arr[1] != (ast.PdfIndirectRef{ObjectNum: 4}) {
		t.Errorf("/Annots[1] = %v, want the untouched reference 4 0 R", arr[1])
	}
	if _, has := pendingDict(t, alloc, 4)["C"]; has {
		t.Error("the indirect annotation kept /C")
	}
	if _, queued := alloc.Pending()[3]; queued {
		t.Error("the page was rewritten although its /Annots is indirect")
	}
}

// TestFixAnnotColor_NullColourIsAbsent pins the validator's reading: a /C
// written as null is absent (ISO 32000-1, 7.3.7), so nothing is rewritten.
func TestFixAnnotColor_NullColourIsAbsent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotColorIntentPDF(t, cmykICCProfile, 4, "[7 0 R]",
		"<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 /C null >>"))
	fixes, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a null /C needs no fix: fixes=%v pending=%d", fixes, len(alloc.Pending()))
	}
}
