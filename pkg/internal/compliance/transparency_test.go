package compliance

import (
	"context"
	"testing"
)

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
		return checkTransparencyPart1(ctx, doc, collectPages(ctx, doc, cat))
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
