package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// directGState is a graphics state parameter dictionary breaking two rules
// the graphics repair owns: a transfer function (ISO 19005-2, 6.2.5 t1; ISO
// 19005-1, 6.2.8 t1) and a rendering intent outside ISO 32000-1, Table 70
// (ISO 19005-2, 6.2.6 t1; ISO 19005-1, 6.2.9 t1).
const directGState = "<< /Type /ExtGState /TR /Identity /RI /Foo >>"

// formStream renders a form XObject whose /Resources are resources and whose
// content is body.
func formStream(resources, body string) string {
	return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources %s /Length %d >>\nstream\n%s\nendstream",
		resources, len(body), body)
}

// contentObj renders a content stream holding body.
func contentObj(body string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
}

// directGStateShapes are the places ISO 32000-1, 7.3.7 lets a graphics state
// sit with no object number of its own, each with the object whose
// /Resources hold it.
func directGStateShapes() []struct {
	name  string
	owner int
	objs  []string
} {
	const page = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"
	content := contentObj("q /GS0 gs Q")
	return []struct {
		name  string
		owner int
		objs  []string
	}{
		{name: "inline resources", owner: 3, objs: []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			page + " /Resources << /ExtGState << /GS0 " + directGState + " >> >> >>",
			content,
		}},
		{name: "indirect resources", owner: 3, objs: []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			page + " /Resources 5 0 R >>",
			content,
			"<< /ExtGState << /GS0 " + directGState + " >> >>",
		}},
		{name: "indirect ExtGState dictionary", owner: 3, objs: []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			page + " /Resources << /ExtGState 5 0 R >> >>",
			content,
			"<< /GS0 " + directGState + " >>",
		}},
		{name: "inherited resources", owner: 2, objs: []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /ExtGState << /GS0 " + directGState + " >> >> >>",
			page + " >>",
			content,
		}},
		{name: "form resources", owner: 5, objs: []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			page + " /Resources << /XObject << /Fm0 5 0 R >> >> >>",
			contentObj("/Fm0 Do"),
			formStream("<< /ExtGState << /GS0 "+directGState+" >> >>", "q /GS0 gs Q"),
		}},
	}
}

// liveGState returns the graphics state /GS0 as the output will carry it in
// the /Resources of the page or form at owner.
func liveGState(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, owner int) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	res := liveOwnerResources(t, alloc, doc, owner)
	gs := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["ExtGState"])["GS0"])
	if gs == nil {
		t.Fatalf("/GS0 did not resolve in the /Resources of object %d: %v", owner, res)
	}
	return gs
}

// assertGStateRepaired fails when gs still carries what [directGState]
// plants.
func assertGStateRepaired(t *testing.T, gs ast.PdfDict) {
	t.Helper()

	if _, has := gs["TR"]; has {
		t.Errorf("/GS0 keeps /TR: %v", gs)
	}
	if ri, _ := gs["RI"].(ast.PdfName); ri != "RelativeColorimetric" {
		t.Errorf("/GS0 /RI = %v, want /RelativeColorimetric: %v", gs["RI"], gs)
	}
}

// TestFixGraphics_RepairsADirectGraphicsState pins that a graphics state
// written directly in a resource dictionary is repaired where it sits.
//
// ISO 32000-1, 7.3.7 lets /Resources, /ExtGState and each graphics state be
// direct objects. The repair wrote a state back only when it had an object
// number of its own, so a direct one kept its /TR and its /RI through
// remediation while the report claimed both removed.
//
// # Corpus evidence
//
//   - litho gstate_2b.pdf: GS7, GS9, H1, H2 and the forms' GSn kept 6.2.5 t1,
//     t3, t4 and 6.2.6 t1 after a PDF/A-2b remediation, and 6.2.8 t1 and 6.2.9
//     t1 after a PDF/A-1b one, on litho and veraPDF alike.
func TestFixGraphics_RepairsADirectGraphicsState(t *testing.T) {
	t.Parallel()

	for _, tc := range directGStateShapes() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, buildStructPDF(tc.objs...))
			notes, err := fixGraphics(ctx, alloc, doc, Options{}, nil, "")
			if err != nil {
				t.Fatalf("fixGraphics: %v", err)
			}
			if len(notes) == 0 {
				t.Error("fixGraphics reported no repair")
			}
			assertGStateRepaired(t, liveGState(t, alloc, doc, tc.owner))
		})
	}
}

// TestFixGraphics_KeepsTheBodyOfAFormItRepairs pins that repairing a direct
// graphics state in a form's /Resources carries the form's content across.
func TestFixGraphics_KeepsTheBodyOfAFormItRepairs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	shapes := directGStateShapes()
	objs := shapes[len(shapes)-1].objs
	doc, alloc := newTestDoc(t, buildStructPDF(objs...))
	if _, err := fixGraphics(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixGraphics: %v", err)
	}
	form, ok := alloc.Pending()[5].(ast.PdfStream)
	if !ok {
		t.Fatalf("the form is %T, want a pending stream", alloc.Pending()[5])
	}
	body, err := doc.DecodeStream(ctx, form)
	if err != nil {
		t.Fatalf("decoding the form: %v", err)
	}
	if string(body) != "q /GS0 gs Q" {
		t.Errorf("the form's content is %q, want %q", body, "q /GS0 gs Q")
	}
}

// TestNormalizeHalftone_DropsTheTransferFunctionOfATopLevelHalftone pins that a
// halftone a graphics state names directly loses its /TransferFunction.
//
// ISO 32000-1, 10.5.5, Table 130 wants /TransferFunction on a halftone only
// when it is a component of a type 5 halftone standing for a nonprimary or
// nonstandard primary colour component, and ISO 19005-2, 6.2.5 t6 forbids it
// elsewhere. The repair turned a type 6 halftone into a type 1 and kept the
// transfer function its source carried, so 6.2.5 t4 gave way to t6.
//
// # Corpus evidence
//
//   - litho gstate_2b.pdf: GS4's type 6 halftone kept 6.2.5 t6 after a
//     PDF/A-2b remediation, on litho and veraPDF alike.
func TestNormalizeHalftone_DropsTheTransferFunctionOfATopLevelHalftone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, _ := newTestDoc(t, minimalCatalogPDF(""))
	for _, ht := range []ast.PdfDict{
		{"Type": ast.PdfName("Halftone"), "HalftoneType": ast.PdfInt(6), "Width": ast.PdfInt(8), "Height": ast.PdfInt(8), "TransferFunction": ast.PdfName("Identity")},
		{"Type": ast.PdfName("Halftone"), "HalftoneType": ast.PdfInt(1), "Frequency": ast.PdfInt(60), "Angle": ast.PdfInt(45), "SpotFunction": ast.PdfName("Round"), "TransferFunction": ast.PdfName("Identity")},
	} {
		got, changed := normalizeHalftone(ctx, doc, ht)
		if !changed {
			t.Errorf("halftone %v: no change", ht)
			continue
		}
		if _, has := got["TransferFunction"]; has {
			t.Errorf("halftone %v keeps /TransferFunction: %v", ht, got)
		}
	}
}
