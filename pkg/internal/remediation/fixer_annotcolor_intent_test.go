package remediation

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// annotColorIntentFix is the description output-intent reports when it
// installs the sRGB intent for annotation colour arrays.
const annotColorIntentFix = "added sRGB OutputIntent to catalog for annotation colour arrays"

// annotColorViolation is the ISO 19005-1, 6.5.3 test 3 violation the
// validator raises on an annotation colour array.
var annotColorViolation = compliance.Violation{Rule: compliance.Rule_19005_1_6_5_3_t03}

// annotNoPaintPDF builds a one-page document with no output intent and no
// content stream, so it paints no device colour, whose page carries annot as
// object 4.
func annotNoPaintPDF(annot string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		annot,
	)
}

// liveIntents returns the output intents the catalog, object 1, carries once
// alloc's pending writes land.
func liveIntents(t *testing.T, doc *document.Document, alloc *writer.ObjAllocator) []intentProfile {
	t.Helper()
	ctx := context.Background()
	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
	if catalog == nil {
		t.Fatal("the catalog does not resolve")
	}
	return outputIntentProfiles(ctx, alloc, doc, catalog)
}

func TestMatchOutputIntent_MatchesAnnotColor(t *testing.T) {
	t.Parallel()

	if !matchOutputIntent(annotColorViolation) {
		t.Error("output-intent must match ISO 19005-1 6.5.3 test 3: it installs the intent that admits the arrays")
	}
}

// TestRemediatePDFA1_AddsAnSRGBIntentForAnnotColor pins the repair ISO
// 19005-1, 6.5.3 prefers: a file with no output intent and no device colour
// receives the sRGB intent, and its annotations keep both colour arrays.
func TestRemediatePDFA1_AddsAnSRGBIntentForAnnotColor(t *testing.T) {
	t.Parallel()

	data := annotNoPaintPDF(annotColorSquare)
	if n := countAnnotColorViolations(t, data, models.PDFA1b); n == 0 {
		t.Fatal("control: the source reports no 6.5.3 test 3 violation, so no fixer is asked")
	}

	doc, alloc := newTestDoc(t, data)
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA1b, Options{PreserveContent: true})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if !slices.Contains(report.Fixed, annotColorIntentFix) {
		t.Errorf("no sRGB intent reported for the annotation colours; fixed = %v", report.Fixed)
	}
	want := []intentProfile{{subtype: "GTS_PDFA1", colorSpace: "RGB "}}
	if got := liveIntents(t, doc, alloc); !slices.Equal(got, want) {
		t.Errorf("output intents = %v, want %v", got, want)
	}
	annot := liveAnnot(t, data, 4, alloc.Pending())
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := annot[key]; !has {
			t.Errorf("%s was removed although the added intent is RGB; annotation = %v", key, annot)
		}
	}
}

// TestFixOutputIntent_AnnotColorFollowsTheDeviceDecision pins the order of the
// two decisions: DeviceCMYK content gets its CMYK intent, and the annotation
// colour arrays do not add a second, conflicting one beside it.
func TestFixOutputIntent_AnnotColorFollowsTheDeviceDecision(t *testing.T) {
	t.Parallel()

	data := annotColorPaintPDF("0 0 0 1 k 100 100 200 200 re f")
	doc, alloc := newTestDoc(t, data)
	violations := []compliance.Violation{
		annotColorViolation,
		{Rule: compliance.Rule{Clause: "6.2.3.3"}, Context: "page 1 uses DeviceCMYK without an output intent"},
	}
	fixes, err := fixOutputIntent(context.Background(), alloc, doc, Options{}, violations, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixOutputIntent: %v", err)
	}
	if !slices.Contains(fixes, "added CMYK OutputIntent to catalog") {
		t.Fatalf("control: no CMYK intent for the DeviceCMYK content; fixes = %v", fixes)
	}
	if slices.Contains(fixes, annotColorIntentFix) {
		t.Errorf("an sRGB intent was installed for the annotations beside the CMYK one; fixes = %v", fixes)
	}
	want := []intentProfile{{subtype: "GTS_PDFA1", colorSpace: "CMYK"}}
	if got := liveIntents(t, doc, alloc); !slices.Equal(got, want) {
		t.Errorf("output intents = %v, want %v", got, want)
	}
}

// TestFixOutputIntent_KeepsASourceCMYKIntentForAnnotColor pins that the
// annotation colour rule alone never replaces the intent a source declares:
// the device colour decision's RGB default must not reach it, and annot-color
// resolves 6.5.3 against the CMYK intent instead.
func TestFixOutputIntent_KeepsASourceCMYKIntentForAnnotColor(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, annotColorIntentPDF(t, cmykICCProfile, 4, "[4 0 R]"))
	fixes, err := fixOutputIntent(context.Background(), alloc, doc, Options{}, []compliance.Violation{annotColorViolation}, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixOutputIntent: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("the source's CMYK intent was touched: fixes=%v pending=%v", fixes, alloc.Pending())
	}
}

// TestFixOutputIntent_NoIntentWithoutAnnotColor pins that the annotation path
// adds nothing when no annotation carries a colour array, here a /C written as
// null, which ISO 32000-1, 7.3.7 reads as absent.
func TestFixOutputIntent_NoIntentWithoutAnnotColor(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, annotNoPaintPDF("<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 /C null >>"))
	fixes, err := fixOutputIntent(context.Background(), alloc, doc, Options{}, []compliance.Violation{annotColorViolation}, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixOutputIntent: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an intent was added with no annotation colour to keep: fixes=%v pending=%d", fixes, len(alloc.Pending()))
	}
}

// TestFixOutputIntent_IgnoresAnnotColorTheRunRemoves pins that an annotation
// the annotations fixer removes later in the round does not earn an intent:
// ISO 19005-1, 6.5.2 forbids Movie in every mode and Polygon once content may
// change, so their arrays never reach the output. Polygon kept in the default
// mode is the control.
func TestFixOutputIntent_IgnoresAnnotColorTheRunRemoves(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		subtype  string
		preserve bool
		want     bool
	}{
		{subtype: "Movie", preserve: true, want: false},
		{subtype: "Polygon", preserve: false, want: false},
		{subtype: "Polygon", preserve: true, want: true},
	} {
		annot := "<< /Type /Annot /Subtype /" + tc.subtype + " /Rect [10 10 60 60] /F 4 /C [1 0 0] >>"
		doc, alloc := newTestDoc(t, annotNoPaintPDF(annot))
		fixes, err := fixOutputIntent(context.Background(), alloc, doc, Options{PreserveContent: tc.preserve},
			[]compliance.Violation{annotColorViolation}, "PDF/A-1b")
		if err != nil {
			t.Fatalf("%s preserve=%v: fixOutputIntent: %v", tc.subtype, tc.preserve, err)
		}
		if got := slices.Contains(fixes, annotColorIntentFix); got != tc.want {
			t.Errorf("%s preserve=%v: intent added = %v, want %v; fixes = %v", tc.subtype, tc.preserve, got, tc.want, fixes)
		}
	}
}

// TestRemediatePDFA1_NoIntentWithoutAnnotColorOrDeviceColor pins that a
// PDF/A-1 run adds no output intent to a file that needs none: no annotation
// carries a colour array and nothing paints a device colour.
func TestRemediatePDFA1_NoIntentWithoutAnnotColorOrDeviceColor(t *testing.T) {
	t.Parallel()

	data := annotNoPaintPDF("<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 >>")
	doc, alloc := newTestDoc(t, data)
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA1b, Options{PreserveContent: true})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if len(report.Fixed) == 0 {
		t.Fatal("control: the run fixed nothing, so no fixer was ever asked")
	}
	if got := liveIntents(t, doc, alloc); len(got) != 0 {
		t.Errorf("output intents = %v, want none; fixed = %v", got, report.Fixed)
	}
}

// TestRemediatePDFA2_AddsNoIntentForAnnotColor pins the part scope: ISO
// 19005-2 has no counterpart to 6.5.3 test 3, so a PDF/A-2 run neither adds an
// intent for the arrays nor removes them, and the annotation path refuses a
// later part when asked directly.
func TestRemediatePDFA2_AddsNoIntentForAnnotColor(t *testing.T) {
	t.Parallel()

	data := annotNoPaintPDF(annotColorSquare)
	doc, alloc := newTestDoc(t, data)
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if got := liveIntents(t, doc, alloc); len(got) != 0 {
		t.Errorf("output intents = %v, want none under PDF/A-2b; fixed = %v", got, report.Fixed)
	}
	annot := liveAnnot(t, data, 4, alloc.Pending())
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := annot[key]; !has {
			t.Errorf("%s was removed under PDF/A-2b; annotation = %v", key, annot)
		}
	}

	doc, alloc = newTestDoc(t, data)
	fixes, err := fixOutputIntent(context.Background(), alloc, doc, Options{}, []compliance.Violation{annotColorViolation}, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixOutputIntent: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("the annotation path acted under PDF/A-2b: fixes=%v pending=%d", fixes, len(alloc.Pending()))
	}
}
