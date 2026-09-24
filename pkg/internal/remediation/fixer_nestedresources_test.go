package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// nestedFormPDF builds a page whose /Resources declare a colour space /CS0 and
// a form XObject (object 4) that paints with /CS0 but carries NO /Resources of
// its own. That is the shape ISO 19005-2/3/4 6.2.2 t2 reports as resolved "only
// through inherited Resources".
//
// # Corpus evidence
//
//   - verapdf 6-2-2-t02-fail-b: has this shape.
func nestedFormPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /X0 4 0 R >> /ColorSpace << /CS0 [/CalGray 5 0 R] >> >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length 16 >>\nstream\nq /CS0 cs 0.5 sc\nendstream",
		"<< /WhitePoint [0.9505 1 1.089] >>",
	)
}

// TestFixNestedResources_GivesAFormXObjectItsOwnResources pins the gap this
// closes.
//
// page-resources already claims this violation but repairs PAGES, pinning an
// inherited dictionary onto the page dict. Here the page already declares
// /CS0; it is the form XObject that lacks a /Resources entry, so the page-level
// repair cannot reach it.
//
// # Corpus evidence
//
//   - All corpora: the page-level repair measured inert on these cells, and
//     they stayed flagged.
func TestFixNestedResources_GivesAFormXObjectItsOwnResources(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, nestedFormPDF())
	fixes, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("the repair reported nothing")
	}

	pending, ok := alloc.Pending()[4]
	if !ok {
		t.Fatal("the form XObject was not rewritten")
	}
	stream, ok := pending.(ast.PdfStream)
	if !ok {
		t.Fatalf("object 4 is %T, want an ast.PdfStream", pending)
	}
	res, ok := stream.Dict["Resources"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Resources is %v, want a dictionary", stream.Dict["Resources"])
	}
	cs, ok := res["ColorSpace"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Resources/ColorSpace is %v, want a dictionary", res["ColorSpace"])
	}
	if _, ok := cs["CS0"]; !ok {
		t.Errorf("/Resources/ColorSpace lacks /CS0, got %v", cs)
	}
}

// TestFixNestedResources_LeavesASharedXObjectAlone guards a hazard no real
// file exercises.
//
// Two pages may bind the same name to different objects, so copying one page's
// binding into an XObject both share would be wrong. Only this test can catch a
// regression here.
//
// # Corpus evidence
//
//   - All corpora: every fixture carrying this violation is single-page, so the
//     corpus cannot falsify this hazard.
func TestFixNestedResources_LeavesASharedXObjectAlone(t *testing.T) {
	t.Parallel()

	shared := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /X0 4 0 R >> /ColorSpace << /CS0 [/CalGray 5 0 R] >> >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length 16 >>\nstream\nq /CS0 cs 0.5 sc\nendstream",
		"<< /WhitePoint [0.9505 1 1.089] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /X0 4 0 R >> /ColorSpace << /CS0 [/CalRGB 5 0 R] >> >> >>",
	)

	doc, alloc := newTestDoc(t, shared)
	if _, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if _, rewritten := alloc.Pending()[4]; rewritten {
		t.Error("a form XObject referenced by two pages was rewritten; the pages bind /CS0 differently")
	}
}

// TestResourcesForStream_MirrorsTheDetectorsSkips pins the nuances copied from
// compliance's checkContentStreamResourcesOps. A fixer predicate that disagrees
// with the checker repairs the wrong thing or declines the right one.
func TestResourcesForStream_MirrorsTheDetectorsSkips(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, nestedFormPDF())
	page := ast.PdfDict{
		"ColorSpace": ast.PdfDict{"CS0": ast.PdfName("DeviceGray")},
		"Properties": ast.PdfDict{"MC0": ast.PdfDict{}},
		"Font":       ast.PdfDict{"F1": ast.PdfDict{}},
	}

	for _, tc := range []struct {
		name    string
		content string
		want    int
	}{
		{"a named colour space is copied", "q /CS0 cs 0.5 sc", 1},
		{"a device colour space needs no entry", "q /DeviceGray cs 0.5 sc", 0},
		{"an empty name references nothing", "q / cs 0.5 sc", 0},
		{"BDC with a name resolves Properties", "/Tag /MC0 BDC EMC", 1},
		{"BDC with an inline dict needs no lookup", "/Tag << /K 1 >> BDC EMC", 0},
		{"a name the page does not define is left alone", "q /CS9 cs 0.5 sc", 0},
		{"a font the stream selects is copied", "BT /F1 12 Tf ET", 1},
	} {
		_, added := resourcesForStream(context.Background(), alloc, doc, []byte(tc.content), nil, page)
		if added != tc.want {
			t.Errorf("%s: added %d, want %d", tc.name, added, tc.want)
		}
	}
}

// TestMatchNestedResources_ClaimsOnlyTheInheritedWording keeps the claim narrow.
//
// ISO 19005-1 numbers an unrelated OutputIntents rule 6.2.2 t2, and the "not in
// Resources" wording is test 3, a genuinely undefined name that copying cannot
// repair. Only the inherited case is this fixer's business.
func TestMatchNestedResources_ClaimsOnlyTheInheritedWording(t *testing.T) {
	t.Parallel()

	claimed := compliance.Violation{
		Rule:    compliance.Rule_19005_2_6_2_2_t02,
		Context: `page 1: cs operator resolves "/CS0" only through inherited Resources/ColorSpace`,
	}
	if !matchNestedResources(claimed) {
		t.Error("must claim the inherited-resources case")
	}

	for _, v := range []compliance.Violation{
		{Rule: compliance.Rule_19005_2_6_2_2_t02, Context: `page 1: cs operator references "/CS9" not in Resources/ColorSpace`},
		{Rule: compliance.Rule_19005_1_6_2_2_t02, Context: "output intents disagree on DestOutputProfile"},
		{Rule: compliance.Rule_19005_2_6_2_2_t01, Context: `page 1: undefined operator "zz"`},
	} {
		if matchNestedResources(v) {
			t.Errorf("must not claim %s/%s t%d with context %q",
				v.Rule.Specification, v.Rule.Clause, v.Rule.TestNumber, v.Context)
		}
	}
}
