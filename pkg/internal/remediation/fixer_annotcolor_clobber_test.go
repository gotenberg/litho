package remediation

import (
	"compress/zlib"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixAnnotColor_ComposesWithThePDFA1AnnotationPeers pins that the colour
// repair and the peers writing the same annotation dictionary under PDF/A-1
// keep each other's edits.
//
// annot-flags and annot-opacity run before annot-color and rewrite the
// annotation at its own object number; annot-appearance runs after it and does
// the same. Each copies the dictionary it reads and writes the copy back whole,
// so a stale read on either side of annot-color drops an edit: a document read
// by annot-color restores the hidden /F and the /CA 0.5, and one by
// annot-appearance restores both colour arrays.
//
// The annotation trips all four repairs: /F 2 is Hidden without Print, /CA 0.5
// is not 1.0, /C and /IC sit under a CMYK intent, and it has no appearance.
func TestFixAnnotColor_ComposesWithThePDFA1AnnotationPeers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [5 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 2 /CA 0.5 /C [1 0 0] /IC [0 0 1] >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (cmyk) /DestOutputProfile 6 0 R >>",
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestCompression), 4),
	}))

	const annotObj = 4
	const profile = "PDF/A-1b"

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if _, err := fixAnnotOpacity(ctx, alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixAnnotOpacity: %v", err)
	}
	if f := pendingDict(t, alloc, annotObj)["F"]; f != ast.PdfInt(4) {
		t.Fatalf("control: annot-flags queued /F %v, want 4, so there is no earlier edit to lose", f)
	}

	if _, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}

	got := pendingDict(t, alloc, annotObj)
	if _, has := got["AP"]; !has {
		t.Fatalf("control: annot-appearance added no /AP, so the check below proves nothing about "+
			"a later write; annotation = %v", got)
	}
	if got["F"] != ast.PdfInt(4) {
		t.Errorf("/F is %v, want 4: annot-color rebuilt the annotation from the document", got["F"])
	}
	if _, has := got["CA"]; has {
		t.Errorf("/CA is back: annot-color rebuilt the annotation from the document")
	}
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := got[key]; has {
			t.Errorf("%s is back: annot-appearance rebuilt the annotation from the document", key)
		}
	}
}

// TestFixAnnotColor_KeepsTheCompactedAnnotsArray pins the same composition on
// an inline annotation, where every write goes through the page's indirect
// /Annots array.
//
// annotations removes the forbidden Movie annotation and compacts the array at
// its own object number, annot-flags then writes the inline annotation back
// into it, and aggressive mode's appearance repair runs last and does the same.
// annot-color rebuilds the array wholesale between them, so reading it from the
// document would restore the Movie annotation and the missing /F, and a later
// stale read would restore the colour arrays.
func TestFixAnnotColor_KeepsTheCompactedAnnotsArray(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [5 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots 7 0 R >>",
		"<< /Type /Annot /Subtype /Movie /Rect [0 0 10 10] /F 4 >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (cmyk) /DestOutputProfile 6 0 R >>",
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestCompression), 4),
		"[<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /C [1 0 0] /IC [0 0 1] >> 4 0 R]",
	}))

	const annotsObj = 7
	const profile = "PDF/A-1b"

	if _, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixAnnotations: %v", err)
	}
	if arr, _ := alloc.Pending()[annotsObj].(ast.PdfArray); len(arr) != 1 {
		t.Fatalf("control: annotations did not compact object %d to one entry: %v", annotsObj, alloc.Pending()[annotsObj])
	}
	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if _, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if _, err := fixAnnotationAppearances(ctx, alloc, doc); err != nil {
		t.Fatalf("fixAnnotationAppearances: %v", err)
	}

	arr, ok := alloc.Pending()[annotsObj].(ast.PdfArray)
	if !ok {
		t.Fatalf("object %d is not a pending array: %T", annotsObj, alloc.Pending()[annotsObj])
	}
	if len(arr) != 1 {
		t.Fatalf("/Annots has %d entries, want 1: the removed Movie annotation is back; array = %v", len(arr), arr)
	}
	got, ok := arr[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Annots[0] is %T, want the inline annotation", arr[0])
	}
	if _, has := got["AP"]; !has {
		t.Fatalf("control: the appearance repair added no /AP, so the check below proves nothing about "+
			"a later write; annotation = %v", got)
	}
	if got["F"] != ast.PdfInt(4) {
		t.Errorf("/F is %v, want 4: annot-color rebuilt the array from the document", got["F"])
	}
	for _, key := range []ast.PdfName{"C", "IC"} {
		if _, has := got[key]; has {
			t.Errorf("%s is back: the appearance repair rebuilt the array from the document", key)
		}
	}
}

// TestFixAnnotColor_KeepsTheWidgetPeersWrites chains the colour repair between
// the two PDF/UA fixers that rewrite a widget dictionary: annot-nesting, which
// gives it a /StructParent, and form-tooltip, which gives its field a /TU.
//
// A single run never pairs them with annot-color, which acts under PDF/A-1
// only, but the three write the same dictionary through one allocator contract,
// and a stale read on either side of annot-color drops an edit.
func TestFixAnnotColor_KeepsTheWidgetPeersWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [6 0 R] >> /OutputIntents [7 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [] >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (field name) /Rect [0 0 10 10] /P 3 0 R /C [1 0 0] >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (cmyk) /DestOutputProfile 8 0 R >>",
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestCompression), 4),
	}))

	const widgetObj = 6

	if _, err := fixAnnotNesting(ctx, alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixAnnotNesting: %v", err)
	}
	if _, has := pendingDict(t, alloc, widgetObj)["StructParent"]; !has {
		t.Fatalf("control: annot-nesting queued no /StructParent at object %d, so there is no "+
			"earlier edit to lose", widgetObj)
	}
	if _, err := fixAnnotColor(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixAnnotColor: %v", err)
	}
	if _, err := fixFormTooltip(ctx, alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixFormTooltip: %v", err)
	}

	got := pendingDict(t, alloc, widgetObj)
	if got["TU"] != ast.PdfString("field name") {
		t.Fatalf("control: /TU is %v, want the field name; the tooltip repair did not run", got["TU"])
	}
	if _, has := got["StructParent"]; !has {
		t.Errorf("/StructParent is gone: annot-color rebuilt the widget from the document; widget = %v", got)
	}
	if _, has := got["C"]; has {
		t.Errorf("/C is back: form-tooltip rebuilt the widget from the document; widget = %v", got)
	}
}
