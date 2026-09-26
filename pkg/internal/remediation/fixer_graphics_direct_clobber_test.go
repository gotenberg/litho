package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestFixGraphics_KeepsOutputIntentsResourcesWrite pins that repairing a direct
// graphics state rebuilds the /Resources around it from the copy output-intent
// queued, not from the document.
//
// output-intent (registry 6) injects a /Default colour space into the same
// /Resources ahead of graphics (30). A direct graphics state is written through
// the /Resources dictionary, or the page holding it, so a copy of the parsed
// resources would drop the injected space.
func TestFixGraphics_KeepsOutputIntentsResourcesWrite(t *testing.T) {
	t.Parallel()

	for _, tc := range directGStateShapes() {
		if tc.name == "form resources" || tc.name == "indirect ExtGState dictionary" {
			// output-intent writes a page's /Resources, which neither shape
			// rebuilds.
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, buildStructPDF(tc.objs...))
			if n := injectTestDefaultRGB(t, alloc, doc); n == 0 {
				t.Fatal("control: output-intent patched no page, so there is no earlier edit to lose")
			}
			if _, err := fixGraphics(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixGraphics: %v", err)
			}

			// output-intent pins the /Resources a page inherits on the page
			// itself, with the injected space, so the page holds the state
			// the output reads.
			const page = 3
			assertGStateRepaired(t, liveGState(t, alloc, doc, page))
			res := liveOwnerResources(t, alloc, doc, page)
			if cs := liveDictValue(ctx, alloc, doc, res["ColorSpace"]); cs["DefaultRGB"] == nil {
				t.Errorf("/DefaultRGB is gone: graphics rebuilt /Resources from the document "+
					"and discarded output-intent's write; /Resources = %v", res)
			}
		})
	}
}

// TestFixTransparency_KeepsGraphicsDirectStateRepair pins that the
// transparency repair, the next registry entry after graphics, keeps what
// graphics wrote to a direct graphics state and to the form holding one.
//
// Both write the same states and the same holders: the page when the state
// sits inline in its /Resources, the form stream when it sits in the form's.
func TestFixTransparency_KeepsGraphicsDirectStateRepair(t *testing.T) {
	t.Parallel()

	const gs = "<< /Type /ExtGState /TR /Identity /RI /Foo /CA 0.5 >>"
	for _, tc := range []struct {
		name  string
		owner int
		objs  []string
	}{
		{name: "page", owner: 3, objs: []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /ExtGState << /GS0 " + gs + " >> >> >>",
			contentObj("q /GS0 gs Q"),
		}},
		{name: "form with a transparency group", owner: 5, objs: []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm0 5 0 R >> >> >>",
			contentObj("/Fm0 Do"),
			"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency >> " +
				"/Resources << /ExtGState << /GS0 " + gs + " >> >> /Length 11 >>\nstream\nq /GS0 gs Q\nendstream",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, buildStructPDF(tc.objs...))
			if _, err := fixGraphics(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixGraphics: %v", err)
			}
			if _, has := liveGState(t, alloc, doc, tc.owner)["TR"]; has {
				t.Fatal("control: graphics did not repair the direct state, so there is no earlier edit to lose")
			}
			if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
				t.Fatalf("fixTransparency: %v", err)
			}

			got := liveGState(t, alloc, doc, tc.owner)
			assertGStateRepaired(t, got)
			if ca := document.Float(got["CA"]); ca != 1 {
				t.Errorf("/GS0 /CA = %v, want 1: the transparency repair did not land", got["CA"])
			}
			if tc.owner != 5 {
				return
			}
			form, ok := alloc.Pending()[5].(ast.PdfStream)
			if !ok {
				t.Fatalf("the form is %T, want a pending stream", alloc.Pending()[5])
			}
			if _, has := form.Dict["Group"]; has {
				t.Errorf("the form keeps its transparency group: %v", form.Dict)
			}
			if body, err := doc.DecodeStream(ctx, form); err != nil || string(body) != "q /GS0 gs Q" {
				t.Errorf("the form's content is %q (%v), want %q", body, err, "q /GS0 gs Q")
			}
		})
	}
}

// TestFixPageResources_KeepsGraphicsInheritedStateRepair pins that pinning
// inherited /Resources on a page (page-resources, registry 52) copies what
// graphics (30) repaired in the ancestor holding them.
//
// graphics writes a direct state in inherited /Resources through the /Pages
// node that holds them; page-resources then copies that dictionary onto the
// page, and a copy of the parsed one would bring the state back unrepaired.
func TestFixPageResources_KeepsGraphicsInheritedStateRepair(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var objs []string
	for _, tc := range directGStateShapes() {
		if tc.name == "inherited resources" {
			objs = tc.objs
		}
	}
	doc, alloc := newTestDoc(t, buildStructPDF(objs...))
	if _, err := fixGraphics(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixGraphics: %v", err)
	}
	if _, err := fixPageResources(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixPageResources: %v", err)
	}
	page := pendingDict(t, alloc, 3)
	if _, own := page["Resources"]; !own {
		t.Fatalf("control: page-resources pinned no /Resources on the page: %v", page)
	}
	assertGStateRepaired(t, liveGState(t, alloc, doc, 3))
}
