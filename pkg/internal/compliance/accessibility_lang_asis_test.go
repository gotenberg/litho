package compliance

import (
	"context"
	"testing"
)

// TestCheckLangRequired_TagJudgedAsWritten pins that PDF/UA judges a /Lang
// value as written (ISO 14289-1, 7.2 t29; ISO 14289-2, 8.4.4 t2).
//
// veraPDF runs /^[a-zA-Z]{1,8}(-[a-zA-Z0-9]{1,8})*$/ on the value itself. litho
// validated it after [document.DecodePdfString], which drops the bytes
// PDFDocEncoding leaves undefined, and after trimming white space, so
// "EN-US\x00" and "EN-US " passed. veraPDF reports both on a one-page file and
// accepts "EN-US".
//
// # Corpus evidence
//
//   - pdfjs issue12706.pdf: writes the /Lang "EN-US\x00", which passed while
//     litho validated the decoded, trimmed value.
func TestCheckLangRequired_TagJudgedAsWritten(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lang  string
		fails bool
	}{
		{"plain tag", "(EN-US)", false},
		{"UTF-16 tag", "<FEFF0045004E002D00550053>", false},
		{"trailing NUL", `(EN-US\000)`, true},
		{"trailing space", "(EN-US )", true},
		{"UTF-16 tag ending in U+0000", "<FEFF0045004E002D005500530000>", true},
	}
	for _, tt := range tests {
		for _, profile := range []string{"PDF/UA-1", "PDF/UA-2"} {
			t.Run(tt.name+" "+profile, func(t *testing.T) {
				t.Parallel()
				doc, cat := tableDoc(t, 1,
					"<< /Type /Catalog /Pages 2 0 R /Lang "+tt.lang+" >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				)
				got := checkLangRequired(context.Background(), doc, cat, profile)
				if fired := hasRule(got, langValidRule(profile)); fired != tt.fails {
					t.Errorf("/Lang %s: fired = %v, want %v (%v)", tt.lang, fired, tt.fails, got)
				}
			})
		}
	}
}

// TestCheckLangRequired_StructElemTagJudgedAsWritten pins the same as-written
// reading on a structure element: a /Sect carrying /Lang (EN-US\000) under a
// well-formed catalog /Lang, which veraPDF reports under ISO 14289-2, 8.4.4 t2.
//
// # Corpus evidence
//
//   - pdfjs issue12706.pdf: its /Sect carries /Lang (EN-US\000) under a
//     well-formed catalog /Lang, and veraPDF reports it under ISO 14289-2,
//     8.4.4 t2.
func TestCheckLangRequired_StructElemTagJudgedAsWritten(t *testing.T) {
	t.Parallel()

	doc, cat := tableDoc(t, 1,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Lang (en-US) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] >>",
		`<< /Type /StructElem /S /Sect /P 4 0 R /Lang (EN-US\000) >>`,
	)
	got := checkLangRequired(context.Background(), doc, cat, "PDF/UA-2")
	if !hasRule(got, Rule_14289_2_8_4_4_t02) {
		t.Errorf("structure element /Lang (EN-US\\000) is not a Language-Tag: want 8.4.4 t2, got %v", got)
	}
}
