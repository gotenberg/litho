package remediation

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// interpolatedImages returns how many inline images of ops ask for
// interpolation under either key.
func interpolatedImages(ops []content.Operation) int {
	n := 0
	for _, op := range ops {
		if !beginsInlineImage(op) || len(op.Operands) != 1 {
			continue
		}
		dict, _ := op.Operands[0].(ast.PdfDict)
		if _, asks := uninterpolated(dict); asks > 0 {
			n++
		}
	}
	return n
}

// TestFixImageIssues_InlineRepairKeepsThePeersRepairs is the clobber guard
// against the real peers that rewrite the same page content: string-limit
// (resource tier), unknown-ops, content-limits and text-control (content
// tier, registry ahead of images) rewrite it first, and the inline image
// repair builds on their write; the aggressive content rewrite (registry
// after images) rewrites it last, and builds on the repair. Every repair
// lands, and the image data survives all of them.
func TestFixImageIssues_InlineRepairKeepsThePeersRepairs(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", pdfA2StringLimit+10)
	image := interpolatedInline("/I")
	cases := []struct {
		name       string
		data       string
		profile    string
		peer       contentFixer
		violations []compliance.Violation
		// peerFirst reports that the peer runs ahead of images.
		peerFirst bool
		// repaired reports whether body holds the peer's repair.
		repaired func(body []byte) bool
	}{
		{
			name: "string-limit", profile: "PDF/A-2b", peer: fixStringLimit, peerFirst: true,
			data:     "q BT /F1 12 Tf (" + long + ") Tj ET " + image + " Q",
			repaired: func(body []byte) bool { return !bytes.Contains(body, []byte(long)) },
		},
		{
			name: "unknown-ops", profile: "PDF/A-2b", peer: fixUnknownOps, peerFirst: true,
			data:     "q zz " + image + " Q",
			repaired: func(body []byte) bool { return !bytes.Contains(body, []byte("zz")) },
		},
		{
			name: "content-limits", profile: "PDF/A-1b", peer: fixContentLimits, peerFirst: true,
			data:     "q 99999.5 w " + image + " Q",
			repaired: func(body []byte) bool { return bytes.Contains(body, []byte("32767 w")) },
		},
		{
			name: "text-control", profile: "PDF/A-2b", peer: fixTextControl, peerFirst: true,
			data:     "q BT /F1 12 Tf (a\x01b) Tj ET " + image + " Q",
			repaired: func(body []byte) bool { return bytes.Contains(body, []byte("(ab) Tj")) },
		},
		{
			name: "aggressive", profile: "PDF/A-2b", peer: fixAggressive, violations: aggressiveIntent,
			data: "q /Foo ri BI /W 4 /H 1 /BPC 8 /CS /G /Intent /Custom /I true" + spliceImageData + " Q",
			repaired: func(body []byte) bool {
				return !bytes.Contains(body, []byte("/Foo")) && !bytes.Contains(body, []byte("/Custom"))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, inlineSplicePage(tc.data))
			images := func() {
				t.Helper()
				if _, err := fixImageIssues(ctx, alloc, doc, Options{}, nil, tc.profile); err != nil {
					t.Fatalf("fixImageIssues: %v", err)
				}
			}
			peer := func() {
				t.Helper()
				if _, err := tc.peer(ctx, alloc, doc, Options{}, tc.violations, tc.profile); err != nil {
					t.Fatalf("%s: %v", tc.name, err)
				}
			}

			if tc.peerFirst {
				peer()
				if body := pendingStreamBytes(t, alloc, 4); !tc.repaired(body) {
					t.Fatalf("control: %s did not repair the page: %q", tc.name, body[:min(len(body), 120)])
				}
				images()
			} else {
				images()
				if ops := validationOps(t, tc.data, pendingStreamBytes(t, alloc, 4)); interpolatedImages(ops) != 0 {
					t.Fatal("control: the images fixer did not repair the page")
				}
				peer()
			}

			body := pendingStreamBytes(t, alloc, 4)
			ops := validationOps(t, tc.data, body)
			if !tc.repaired(body) {
				t.Errorf("the %s repair did not survive: %q", tc.name, body[:min(len(body), 120)])
			}
			if n := interpolatedImages(ops); n != 0 {
				t.Errorf("validation reads %d interpolated images, want the repair kept", n)
			}
		})
	}
}

// TestFixImageIssues_InlineRepairKeepsTheGraphicsRepair is the clobber guard
// against the peer that answers the same finding and writes the same form
// first: graphics (resource tier) removes a form's /OPI (ISO 19005-2, 6.2.9
// t1) by writing its dictionary over its body, and the inline image repair
// rewrites that form's content, so the /OPI stays removed and the image in it
// no longer asks for interpolation.
func TestFixImageIssues_InlineRepairKeepsTheGraphicsRepair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	form := "q 10 0 0 10 0 0 cm " + interpolatedInline("/I") + " Q"
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /XObject << /X0 5 0 R >> >> >>",
		streamBody("", "/X0 Do"),
		streamBody("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /OPI << /2.0 << /Type /OPI /Version 2.0 /MainImage (x.tif) >> >>", form),
	))
	if _, err := fixGraphics(ctx, alloc, doc, Options{}, nil, string(models.PDFA2b)); err != nil {
		t.Fatalf("fixGraphics: %v", err)
	}
	if s, ok := alloc.Pending()[5].(ast.PdfStream); !ok || s.Dict["OPI"] != nil {
		t.Fatalf("control: graphics did not remove the form's /OPI: %v", alloc.Pending()[5])
	}

	if _, err := fixImageIssues(ctx, alloc, doc, Options{}, nil, string(models.PDFA2b)); err != nil {
		t.Fatalf("fixImageIssues: %v", err)
	}
	if s := alloc.Pending()[5].(ast.PdfStream); s.Dict["OPI"] != nil {
		t.Error("the inline image repair rebuilt the form from the document: /OPI is back")
	}
	if ops := validationOps(t, form, pendingStreamBytes(t, alloc, 5)); interpolatedImages(ops) != 0 {
		t.Error("the form's inline image still asks for interpolation")
	}

	after, _ := revalidatePDFA(t, doc, alloc, models.PDFA2b)
	for _, rule := range []compliance.Rule{compliance.Rule_19005_2_6_2_8_t03, compliance.Rule_19005_2_6_2_9_t01} {
		for _, v := range failures(after, rule) {
			t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
		}
	}
}
