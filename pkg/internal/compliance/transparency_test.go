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
