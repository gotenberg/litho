package compliance

import (
	"context"
	"testing"
)

// TestCheckFormFieldTU_DecodedText pins that ISO 14289-1 §7.18.1 t3 judges the
// /TU and /Alt text strings by the text they decode to, not by their bytes.
//
// A text string holding only the UTF-16 byte order mark <FEFF> decodes to the
// empty string (ISO 32000-1, 7.9.2.2). pdfjs bug1847733.pdf gives every field
// such a /TU; veraPDF reads each as empty and reports the widget, while litho
// counted the two bytes as a tooltip.
func TestCheckFormFieldTU_DecodedText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tu    string
		elem  string
		fires bool
	}{
		{"a /TU that is only a byte order mark", "<FEFF>", "<< /Type /StructElem /S /Form >>", true},
		{"a /TU with text", "(Name)", "<< /Type /StructElem /S /Form >>", false},
		{"an /Alt that is only a byte order mark", "<FEFF>", "<< /Type /StructElem /S /Form /Alt <FEFF> >>", true},
		{"an /Alt with text", "<FEFF>", "<< /Type /StructElem /S /Form /Alt (Name field) >>", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> /StructTreeRoot 6 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /TU "+tt.tu+" /Rect [10 10 50 30] /P 3 0 R /StructParent 0 >>",
				tt.elem,
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 7 0 R >>",
				"<< /Nums [0 5 0 R] >>",
			)
			got := checkFormFieldTU(context.Background(), doc, cat, "PDF/UA-1")
			if fired := hasRule(got, Rule_14289_1_7_18_1_t03); fired != tt.fires {
				t.Errorf("7.18.1 t3 fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
