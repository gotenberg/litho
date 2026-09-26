package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckType3FontTransparency_RenderedGlyphsOnly covers the rendered-glyph
// scope of Type 3 font transparency: a transparent CharProc that is never painted
// does not make the page transparent (veraPDF PDPage containsTransparency), while
// a rendered transparent glyph does. The font's /square glyph (code 97) paints
// with /GS0 (ca 0.5); /triangle (code 98) is opaque.
func TestCheckType3FontTransparency_RenderedGlyphsOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	streamObj := func(body string) string {
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
	}
	build := func(t *testing.T, shown string) bool {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Page /Contents 3 0 R /Resources << /Font << /F1 4 0 R >> >> >>",
			streamObj("BT /F1 12 Tf 0 0 Td ("+shown+") Tj ET"),
			"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 100 100] /FontMatrix [0.001 0 0 0.001 0 0] "+
				"/CharProcs << /square 5 0 R /triangle 6 0 R >> /Encoding << /Differences [97 /square /triangle] >> "+
				"/Resources << /ExtGState << /GS0 << /ca 0.5 >> >> >> /FirstChar 97 /LastChar 98 /Widths [1000 1000] >>",
			streamObj("1000 0 d0\n/GS0 gs\n0 0 100 100 re f"), // /square: transparent
			streamObj("1000 0 d0\n0 0 100 100 re f"),          // /triangle: opaque
		)
		page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve page: %v", err)
		}
		return pagePaintsTransparency(ctx, doc, page, 2)
	}

	if build(t, "b") { // renders /triangle (opaque); /square never painted
		t.Error("rendering only the opaque glyph must not report transparency")
	}
	if !build(t, "a") { // renders /square (transparent)
		t.Error("rendering the transparent glyph must report transparency")
	}
}

// TestCheckTransparency_DeviceGroupCSNeedNotMatchOutputIntent covers PDF/A-4 §6.2.9
// t2 (and PDF/A-2/3 §6.2.10 t2): the rule requires a blending colour space to EXIST
// for a transparent page, not that a device /Group /CS match the OutputIntent's
// colour space. veraPDF's test is existence-only; a DeviceRGB blending CS under a
// CMYK OutputIntent is reported by §6.2.4.3, not here.
func TestCheckTransparency_DeviceGroupCSNeedNotMatchOutputIntent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// ICC stream whose colour-space signature (bytes 16..20) is "CMYK".
	icc := "<< /Length 20 >>\nstream\n                CMYK\nendstream"
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /OutputIntents [<< /S /GTS_PDFA1 /DestOutputProfile 2 0 R >>] >>",
		icc,
		"<< /Type /Page /Group << /S /Transparency /CS /DeviceRGB >> /Resources << /ExtGState << /GS0 << /ca 0.5 >> >> >> >>",
	)
	page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil || page == nil {
		t.Fatalf("resolve page: %v", err)
	}
	got := checkTransparencyParts2Plus(ctx, doc, cat, pageCtxsForPages(ctx, doc, page), "PDF/A-4", 4)
	if hasRule(got, Rule_19005_4_6_2_9_t02) {
		t.Errorf("DeviceRGB /Group /CS under a CMYK OutputIntent must not fire §6.2.9 t2, got %v", got)
	}
}

// formStream builds a Form XObject stream object body with the given extra
// dictionary entries (e.g. a /Group), for use with objsDocPDF.
func formStream(extra string) string {
	return "<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] " + extra +
		" /Length 0 >>\nstream\nendstream"
}

func TestResourcesUseTransparency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// image builds an Image XObject stream body, optionally carrying an /SMask.
	image := func(smask string) string {
		return "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 " +
			"/ColorSpace /DeviceGray " + smask + " /Length 0 >>\nstream\nendstream"
	}

	run := func(imgObj string) bool {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Fm 4 0 R >> >> >>",
			"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /XObject << /Im 5 0 R >> >> /Length 0 >>\nstream\nendstream",
			imgObj,
			image(""), // obj 6: the soft-mask image target
		)
		page := collectPages(ctx, doc, cat)[0]
		res := resolveResources(ctx, doc, page)
		// This exercises the resource survey itself, which a page whose content
		// litho cannot read falls back to.
		return resourcesUseTransparency(ctx, doc, res, 0, map[string]bool{})
	}

	t.Run("soft-mask image nested in a form is detected", func(t *testing.T) {
		t.Parallel()
		if !run(image("/SMask 6 0 R")) {
			t.Error("expected transparency from an SMask image nested in a Form XObject")
		}
	})

	t.Run("plain image nested in a form is not transparency", func(t *testing.T) {
		t.Parallel()
		if run(image("")) {
			t.Error("a plain nested image must not count as transparency")
		}
	})
}

func TestCheckAnnotAPTransparency1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(form string) []Violation {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
			"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /AP << /N 5 0 R >> >>",
			form,
		)
		return checkTransparencyPart1(ctx, doc, pageCtxsFor(ctx, doc, cat))
	}

	t.Run("transparency group on widget appearance fires t3", func(t *testing.T) {
		t.Parallel()
		vs := build(formStream("/Group << /S /Transparency >>"))
		if !hasRule(vs, Rule_19005_1_6_4_t03) {
			t.Errorf("expected 6.4-t3 on a transparency group in a widget appearance, got %v", vs)
		}
	})

	t.Run("SMask on widget appearance fires t2", func(t *testing.T) {
		t.Parallel()
		vs := build(formStream("/SMask 6 0 R"))
		if !hasRule(vs, Rule_19005_1_6_4_t02) {
			t.Errorf("expected 6.4-t2 on an SMask in a widget appearance, got %v", vs)
		}
	})

	t.Run("non-transparency group does not fire", func(t *testing.T) {
		t.Parallel()
		vs := build(formStream("/Group << /S /Whatever >>"))
		if hasRule(vs, Rule_19005_1_6_4_t03) {
			t.Errorf("a non-Transparency group must not fire 6.4-t3, got %v", vs)
		}
	})

	t.Run("plain appearance form is clean", func(t *testing.T) {
		t.Parallel()
		vs := build(formStream(""))
		if len(vs) != 0 {
			t.Errorf("a plain appearance form must not fire any §6.4 rule, got %v", vs)
		}
	})
}

// TestCheckAnnotationTransparency_AppearanceSubStates covers a widget whose /AP /N
// is a sub-dictionary of appearance states (e.g. /On, /Off), not a single stream.
// Transparency inside a state's appearance form must still be detected.
func TestCheckAnnotationTransparency_AppearanceSubStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /AP << /N << /Off 5 0 R /On 6 0 R >> >> >>",
		formStream(""), // /Off: opaque
		formStream("/Group << /S /Transparency >>"), // /On: transparency group
	)
	page := collectPages(ctx, doc, cat)[0]
	if !pagePaintsTransparency(ctx, doc, page, 2) {
		t.Error("transparency in a widget /AP /N sub-state (/On) must be detected")
	}
}
