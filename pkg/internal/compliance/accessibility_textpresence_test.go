package compliance

import (
	"context"
	"testing"
)

// TestTextStringPresence pins how the rules that test a text string for
// presence tell an empty string from an absent one, as veraPDF does.
//
// veraPDF reads /Contents and /TU through COSObject.getStringKey, which returns
// null only when the key is missing, so an empty string is present. ISO 14289-1
// §7.2 t24 (Contents == null || ...) and t25 (TU == null || ...) therefore ask an
// empty string for a determinable language, and ISO 14289-2 §8.10.2.3 t1
// (Contents != null) accepts an empty /Contents on a widget. On one-annotation
// fixtures veraPDF reports t24 and t25 for an empty value and passes the widget.
func TestTextStringPresence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("7.2 t24", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			contents string
			fires    bool
		}{{" /Contents ()", true}, {"", false}} {
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
				"<< /Type /Annot /Subtype /Text /Rect [10 10 30 30]"+tc.contents+" >>",
			)
			if fired := hasRule(checkAnnotFormLang(ctx, doc, cat), Rule_14289_1_7_2_t24); fired != tc.fires {
				t.Errorf("contents %q: t24 fired = %v, want %v", tc.contents, fired, tc.fires)
			}
		}
	})

	t.Run("7.2 t25", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			tu    string
			fires bool
		}{{" /TU ()", true}, {"", false}} {
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /FT /Tx /T (f)"+tc.tu+" >>",
			)
			if fired := hasRule(checkAnnotFormLang(ctx, doc, cat), Rule_14289_1_7_2_t25); fired != tc.fires {
				t.Errorf("tu %q: t25 fired = %v, want %v", tc.tu, fired, tc.fires)
			}
		}
	})

	t.Run("8.10.2.3 t1", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			contents string
			fires    bool
		}{{" /Contents ()", false}, {"", true}} {
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 6 0 R >>",
				"<< /Type /StructElem /S /Form /K [7 0 R] >>",
				"<< /Nums [0 5 0 R] >>",
				"<< /Type /OBJR /Obj 8 0 R >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /StructParent 0"+tc.contents+" >>",
			)
			if fired := hasRule(checkPDFUA2FormStructure(ctx, doc, cat, "PDF/UA-2"), Rule_14289_2_8_10_2_3_t01); fired != tc.fires {
				t.Errorf("contents %q: 8.10.2.3 t1 fired = %v, want %v", tc.contents, fired, tc.fires)
			}
		}
	})
}
