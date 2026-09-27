package remediation

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// printOnlyFlags is an /F holding the Print flag alone (ISO 32000-1, Table 165,
// bit 3), what the flag repair leaves on an annotation that had no other flag.
const printOnlyFlags = 1 << 2

// liveAnnotNums returns the object numbers the live /Annots of the page at
// pageNum lists, read the way the next fixer reads them: the page from alloc
// first, and its array from alloc first. An entry written in place is -1.
func liveAnnotNums(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, pageNum int) []int {
	t.Helper()
	ctx := context.Background()
	page := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: pageNum})
	if page == nil {
		t.Fatalf("page %d does not resolve", pageNum)
	}
	var nums []int
	for _, entry := range liveArray(ctx, alloc, doc, page["Annots"]) {
		if ref, ok := entry.(ast.PdfIndirectRef); ok {
			nums = append(nums, ref.ObjectNum)
			continue
		}
		nums = append(nums, -1)
	}
	return nums
}

// liveFlags returns the /F of the annotation at objNum as the allocator holds it.
func liveFlags(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, objNum int) int {
	t.Helper()
	annot := resolveFromAllocOrDoc(context.Background(), alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	if annot == nil {
		t.Fatalf("annotation %d does not resolve", objNum)
	}
	f, ok := annot["F"].(ast.PdfInt)
	if !ok {
		t.Fatalf("annotation %d: /F = %T, want an integer", objNum, annot["F"])
	}
	return int(f)
}

// assertNulled fails unless the allocator replaced objNum with the null object.
func assertNulled(t *testing.T, alloc *writer.ObjAllocator, objNum int) {
	t.Helper()
	if _, isNull := alloc.Pending()[objNum].(ast.PdfNull); !isNull {
		t.Errorf("object %d: pending %T, want the null object for a removed annotation", objNum, alloc.Pending()[objNum])
	}
}

// hasFix reports whether a fix description contains every part.
func hasFix(fixes []string, parts ...string) bool {
	return slices.ContainsFunc(fixes, func(fix string) bool {
		for _, part := range parts {
			if !strings.Contains(fix, part) {
				return false
			}
		}
		return true
	})
}

// TestFixAnnotFlagsRemovesAnnotationsThatNeverShow pins the user decision on ISO
// 19005-2, 6.3.2: an annotation that shows neither on screen nor in print is
// removed rather than made visible, and every other one keeps the flag repair.
//
// ISO 32000-1, 12.5.3, Table 165: a Hidden annotation is neither displayed nor
// printed, and a NoView one is not displayed and prints only with Print set.
// Clearing the flags and setting Print, which the fixer did for every one of
// them, turned content the author kept off the page into visible content.
// ToggleNoView inverts NoView on events a viewer still receives, so an
// annotation carrying it shows on screen and keeps the repair.
func TestFixAnnotFlagsRemovesAnnotationsThatNeverShow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R 7 0 R 8 0 R 9 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 2 >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 32 >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 36 >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 288 >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 6 >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 0 >>",
	}))

	fixes, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}

	if got, want := liveAnnotNums(t, alloc, doc, 3), []int{6, 7, 9}; !slices.Equal(got, want) {
		t.Fatalf("/Annots = %v, want %v: the Hidden and the unprinted NoView annotations go, the others stay", got, want)
	}
	for _, num := range []int{4, 5, 8} {
		assertNulled(t, alloc, num)
	}
	for _, num := range []int{6, 7, 9} {
		if f := liveFlags(t, alloc, doc, num); f != printOnlyFlags {
			t.Errorf("annotation %d: /F = %d, want %d (Print alone)", num, f, printOnlyFlags)
		}
	}

	if !hasFix(fixes, "removed Hidden Square annotation", "page 1") {
		t.Errorf("fixes = %q, want a removal of the Hidden annotation reported", fixes)
	}
	if !hasFix(fixes, "removed NoView Square annotation", "does not print", "page 1") {
		t.Errorf("fixes = %q, want a removal of the unprinted NoView annotation reported", fixes)
	}
	if !hasFix(fixes, "added /F flag to 3 annotations") {
		t.Errorf("fixes = %q, want the flag repair of the three kept annotations reported", fixes)
	}

	again, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second pass reported %q, want nothing: the removal is not idempotent", again)
	}
}

// TestFixAnnotFlagsRemovesAWidgetWithItsField pins that a removed widget takes
// its form field when it was the field's only widget.
//
// AcroForm /Fields holds the root fields and a field's /Kids its children (ISO
// 32000-1, 12.7.2 and 12.7.3.1). A widget merged with its field (12.5.6.19)
// leaves the array that lists it, a field whose every widget went leaves its
// parent in turn, and a field keeping another widget keeps its place. /CO names
// fields (Table 218) and loses the ones that went.
func TestFixAnnotFlagsRemovesAWidgetWithItsField(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 10 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R 7 0 R 8 0 R 14 0 R] >>",
		// 4: a hidden field merged with its widget, listed in /Fields.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (a) /Rect [0 0 10 10] /F 2 >>",
		// 5: a displayed field merged with its widget.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (b) /Rect [0 0 10 10] /F 4 >>",
		// 6: the only widget of field c, hidden.
		"<< /Type /Annot /Subtype /Widget /Parent 11 0 R /Rect [0 0 10 10] /F 2 >>",
		// 7 and 8: the two widgets of field d, the first unprinted NoView.
		"<< /Type /Annot /Subtype /Widget /Parent 12 0 R /Rect [0 0 10 10] /F 32 >>",
		"<< /Type /Annot /Subtype /Widget /Parent 12 0 R /Rect [0 0 10 10] /F 4 >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Fields [4 0 R 5 0 R 11 0 R 12 0 R 13 0 R] /CO [4 0 R 5 0 R 11 0 R] >>",
		"<< /FT /Tx /T (c) /Kids [6 0 R] >>",
		"<< /FT /Tx /T (d) /Kids [7 0 R 8 0 R] >>",
		// 13: a non-terminal field whose only child, 14, is a hidden field
		// merged with its widget.
		"<< /T (grp) /Kids [14 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (e) /Parent 13 0 R /Rect [0 0 10 10] /F 6 >>",
	}))

	fixes, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}

	if got, want := liveAnnotNums(t, alloc, doc, 3), []int{5, 8}; !slices.Equal(got, want) {
		t.Fatalf("/Annots = %v, want %v", got, want)
	}

	form := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 10})
	refNums := func(obj ast.PdfObject) []int {
		var nums []int
		for _, entry := range liveArray(ctx, alloc, doc, obj) {
			if ref, ok := entry.(ast.PdfIndirectRef); ok {
				nums = append(nums, ref.ObjectNum)
			}
		}
		return nums
	}
	if got, want := refNums(form["Fields"]), []int{5, 12}; !slices.Equal(got, want) {
		t.Errorf("/Fields = %v, want %v: fields a, c and grp lost every widget they had", got, want)
	}
	if got, want := refNums(form["CO"]), []int{5}; !slices.Equal(got, want) {
		t.Errorf("/CO = %v, want %v", got, want)
	}
	fieldD := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 12})
	if got, want := refNums(fieldD["Kids"]), []int{8}; !slices.Equal(got, want) {
		t.Errorf("field d /Kids = %v, want %v: it keeps its displayed widget", got, want)
	}
	for _, name := range []string{`"c"`, `"grp"`} {
		if !hasFix(fixes, "removed form field "+name) {
			t.Errorf("fixes = %q, want the removal of field %s reported", fixes, name)
		}
	}
}

// TestFixAnnotFlagsPrunesTheStructureOfARemovedAnnotation pins the tagged side
// of a removal: the element enclosing the annotation loses its object
// reference, an element left holding nothing leaves its parent, and the
// annotation's /StructParent leaves the /ParentTree (ISO 32000-1, 14.7.4.3 and
// 14.7.4.4). An OBJR left naming a removed annotation points at nothing.
func TestFixAnnotFlagsPrunesTheStructureOfARemovedAnnotation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (a) /Rect [0 0 10 10] /F 6 /StructParent 1 >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (b) /Rect [0 0 10 10] /F 4 /StructParent 2 >>",
		"<< /Type /StructTreeRoot /K 7 0 R /ParentTree 10 0 R /ParentTreeNextKey 3 >>",
		"<< /Type /StructElem /S /Document /P 6 0 R /K [8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /Form /P 7 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 4 0 R >> >>",
		"<< /Type /StructElem /S /Form /P 7 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 5 0 R >> >>",
		"<< /Nums [1 8 0 R 2 9 0 R] >>",
	}))

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2a"); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}

	if got, want := liveAnnotNums(t, alloc, doc, 3), []int{5}; !slices.Equal(got, want) {
		t.Fatalf("/Annots = %v, want %v", got, want)
	}

	documentElem := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 7})
	var kids []int
	for _, kid := range liveArray(ctx, alloc, doc, documentElem["K"]) {
		if ref, ok := kid.(ast.PdfIndirectRef); ok {
			kids = append(kids, ref.ObjectNum)
		}
	}
	if want := []int{9}; !slices.Equal(kids, want) {
		t.Errorf("Document /K = %v, want %v: the Form element of the removed widget holds nothing", kids, want)
	}

	root := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 6})
	tree := liveDictValue(ctx, alloc, doc, root["ParentTree"])
	nums := liveArray(ctx, alloc, doc, tree["Nums"])
	for i := 0; i+1 < len(nums); i += 2 {
		if key, _ := nums[i].(ast.PdfInt); key == 1 {
			t.Errorf("/ParentTree still lists key 1, the removed widget's /StructParent: %v", nums)
		}
	}
	if len(nums) != 2 {
		t.Errorf("/ParentTree /Nums = %v, want the kept widget's key alone", nums)
	}
}

// TestFixAnnotFlagsRemovesPopupsAndRepliesWithTheirAnnotation pins the
// annotations that go with a removed one, and the reference a kept annotation
// loses.
//
// A pop-up does not appear alone but with its parent (ISO 32000-1, 12.5.6.14),
// and a reply is displayed with the annotation it replies to, never on its own
// (Table 170, RT). A kept annotation whose pop-up goes keeps no /Popup naming
// it.
func TestFixAnnotFlagsRemovesPopupsAndRepliesWithTheirAnnotation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R 7 0 R 8 0 R] >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /F 6 /Popup 5 0 R >>",
		"<< /Type /Annot /Subtype /Popup /Rect [0 0 10 10] /F 28 /Parent 4 0 R >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /F 4 /Popup 7 0 R >>",
		"<< /Type /Annot /Subtype /Popup /Rect [0 0 10 10] /F 2 /Parent 6 0 R >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /F 4 /IRT 4 0 R >>",
	}))

	fixes, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}

	if got, want := liveAnnotNums(t, alloc, doc, 3), []int{6}; !slices.Equal(got, want) {
		t.Fatalf("/Annots = %v, want %v", got, want)
	}
	for _, num := range []int{4, 5, 7, 8} {
		assertNulled(t, alloc, num)
	}
	kept := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 6})
	if _, has := kept["Popup"]; has {
		t.Errorf("the kept Text annotation still names its removed pop-up: %v", kept["Popup"])
	}
	if !hasFix(fixes, "removed Popup annotation", "removed annotation") {
		t.Errorf("fixes = %q, want the pop-up of the removed Text annotation reported", fixes)
	}
	if !hasFix(fixes, "reply") {
		t.Errorf("fixes = %q, want the reply to the removed annotation reported", fixes)
	}
}

// TestFixAnnotFlagsKeepsASignedSignatureWidget pins that a signed signature
// field keeps its widget and its flag repair. The signature is reached through
// the field's /V (ISO 32000-1, 12.7.4.5 and 12.8.1), so removing the field
// would take the signature out of the document.
func TestFixAnnotFlagsKeepsASignedSignatureWidget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /SigFlags 3 >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (sig) /V 5 0 R /Rect [0 0 0 0] /F 2 >>",
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 0 0 0] /Contents <00> >>",
	}))

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if got, want := liveAnnotNums(t, alloc, doc, 3), []int{4}; !slices.Equal(got, want) {
		t.Fatalf("/Annots = %v, want %v: the signed signature widget stays", got, want)
	}
	if f := liveFlags(t, alloc, doc, 4); f != printOnlyFlags {
		t.Errorf("/F = %d, want %d: a kept widget keeps the flag repair", f, printOnlyFlags)
	}
}

// TestFixAnnotFlagsRemovesAnInlineHiddenAnnotation pins the removal of an
// annotation written directly into /Annots (ISO 32000-1, 12.5.2 does not
// require an indirect one): the page carries the filtered array.
func TestFixAnnotFlagsRemovesAnInlineHiddenAnnotation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Annots [<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 2 >> 4 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 >>",
	}))

	fixes, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if got, want := liveAnnotNums(t, alloc, doc, 3), []int{4}; !slices.Equal(got, want) {
		t.Fatalf("/Annots = %v, want %v", got, want)
	}
	if !hasFix(fixes, "removed Hidden Square annotation") {
		t.Errorf("fixes = %q, want the removal reported", fixes)
	}
}

// TestFixAnnotFlagsRemovalSurvivesALaterFixer is the clobber test for an
// indirect /Annots array. The filtered array is written back at its own object
// number, and annot-appearance, which runs later, reads it there: read as the
// file wrote it, the array still listed the removed annotation, which the fixer
// read past its pending null and wrote back with an appearance.
func TestFixAnnotFlagsRemovalSurvivesALaterFixer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots 4 0 R >>",
		"[5 0 R 6 0 R]",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 2 >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 >>",
	}))

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}

	if got, want := liveAnnotNums(t, alloc, doc, 3), []int{6}; !slices.Equal(got, want) {
		t.Fatalf("/Annots = %v, want %v", got, want)
	}
	assertNulled(t, alloc, 5)
	kept := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 6})
	if _, has := kept["AP"]; !has {
		t.Errorf("the kept annotation has no /AP: the later fixer never ran, so this proves nothing")
	}
}

// TestFixAnnotFlagsRemovesNothingOutsidePDFA pins that the removal answers a
// PDF/A profile alone. ISO 14289-1 and ISO 14289-2 do not forbid the flags, so
// a hidden annotation is the author's to keep there.
func TestFixAnnotFlagsRemovesNothingOutsidePDFA(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 2 >>",
	}))

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if got, want := liveAnnotNums(t, alloc, doc, 3), []int{4}; !slices.Equal(got, want) {
		t.Errorf("/Annots = %v, want %v", got, want)
	}
}
