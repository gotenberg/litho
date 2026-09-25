package signature

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// twoPageCertifiedPDF is a document certified at the given DocMDP policy with
// two pages: the first (3) carries the certification signature widget (4,
// value 5) and a text field widget (6), the second (8) nothing.
func twoPageCertifiedPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 8 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}, "")
}

// TestEvaluateMDP_RemovedPage covers ISO 32000-1, 12.8.2.2, Table 254: no P
// permits deleting a page, whether the update rewrites the page tree or only
// frees the page in a cross-reference section, while instantiating a page
// template, which adds one, is permitted from P=2.
func TestEvaluateMDP_RemovedPage(t *testing.T) {
	pages := func(kids string, count int) func(t *testing.T, base []byte) []byte {
		return func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, updatedObject{2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, count)})
		}
	}
	freed := func(num int) func(t *testing.T, base []byte) []byte {
		return func(t *testing.T, base []byte) []byte { return freeObject(t, base, num) }
	}

	tests := []struct {
		name   string
		policy int
		update func(t *testing.T, base []byte) []byte
		want   bool
	}{
		{"P=1 page freed by the cross-reference section", 1, freed(8), false},
		{"P=2 page freed by the cross-reference section", 2, freed(8), false},
		{"P=2 page dropped from /Kids", 2, pages("3 0 R", 1), false},
		{"P=3 page dropped from /Kids", 3, pages("3 0 R", 1), false},

		{"P=1 unrelated object freed", 1, freed(9), true},
		{"P=2 pages reordered", 2, pages("8 0 R 3 0 R", 2), true},
		{"P=2 template page added", 2, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base,
				updatedObject{2, "<< /Type /Pages /Kids [3 0 R 8 0 R 9 0 R] /Count 3 >>"},
				updatedObject{9, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>"},
			)
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := twoPageCertifiedPDF(tc.policy)
			result := mdpVerdict(t, tc.update(t, base), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if !tc.want && !strings.Contains(result.MDPInfo, "page 2 (object 8) was removed") {
				t.Errorf("MDPInfo %q does not name the removed page", result.MDPInfo)
			}
			if strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: the signed revision was not read", result.MDPInfo)
			}
		})
	}
}

// TestEvaluateMDP_PageRemovalUncheckedWithoutASignedRevision pins that a
// /ByteRange ending where no revision ends leaves the page comparison undone,
// and says so, rather than comparing against a guessed revision.
func TestEvaluateMDP_PageRemovalUncheckedWithoutASignedRevision(t *testing.T) {
	base := twoPageCertifiedPDF(1)
	result := mdpVerdict(t, freeObject(t, base, 8), bytes.Index(base, []byte("xref")), 1)
	if !strings.Contains(result.MDPInfo, "page removals not checked") {
		t.Errorf("MDPValid = %v, MDPInfo %q: want MDPInfo to say page removals were not checked", result.MDPValid, result.MDPInfo)
	}
}
