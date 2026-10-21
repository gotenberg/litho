package flatten

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func TestFlattenKeptFormsLoseTheirGroups(t *testing.T) {
	t.Parallel()
	in := fixture{
		resources: "/XObject << /Opaque 7 0 R /Masked 8 0 R >>",
		content:   "/Opaque Do q 30 0 0 30 60 60 cm /Masked Do Q",
		objs: []string{
			// Its own resources: a form without them may draw /Masked by
			// name, and the image then stays.
			stream("/Type /XObject /Subtype /Form /BBox [0 0 40 40] /Resources << >> /Group << /S /Transparency /I true >>", "1 0 0 rg 0 0 40 40 re f"),
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 9 0 R", "\x00"),
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x80"),
		},
	}
	out, _ := checkFlattened(t, in)
	ctx := context.Background()
	doc, pg := open(t, out)
	res, ok := pg.Dict["Resources"].(ast.PdfDict)
	if !ok {
		t.Fatalf("page resources %T", pg.Dict["Resources"])
	}
	xobjects, err := document.ResolveDict(ctx, doc, res["XObject"])
	if err != nil {
		t.Fatal(err)
	}
	if _, kept := xobjects["Masked"]; kept {
		t.Error("the soft-masked image the composite took over is still a page resource")
	}
	opaque, err := document.ResolveStream(ctx, doc, xobjects["Opaque"])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := opaque.Dict["Group"]; ok {
		t.Error("the opaque form the page keeps still holds a transparency group")
	}
}

func TestFlattenCountsTransparentAnnotations(t *testing.T) {
	t.Parallel()
	in := fixture{page: "/Annots [7 0 R 8 0 R]", objs: []string{
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /CA 0.5 >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /CA 1 >>",
	}}.bytes()
	_, report := flattenBytes(t, in, Options{})
	if report.TransparentAnnotations != 1 {
		t.Errorf("transparent annotations %d, want 1", report.TransparentAnnotations)
	}
}
