package signature

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// restoreObject appends an update that holds nothing but a cross-reference
// section pointing object num back at its first definition in base.
func restoreObject(t *testing.T, base []byte, num int) []byte {
	t.Helper()
	at := bytes.Index(base, fmt.Appendf(nil, "\n%d 0 obj", num))
	if at < 0 {
		t.Fatalf("base defines no object %d", num)
	}
	var buf bytes.Buffer
	buf.Write(base)
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n%d 1\n%010d 00000 n \r\ntrailer\n<< /Size 12 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n",
		num, at+1, startXRef(t, base), xref)
	return buf.Bytes()
}

// TestEvaluateMDP_AddedPage covers ISO 32000-1, 12.8.2.2, Table 254, which
// permits no change at P=1 and, at P=2 and P=3, adds a page only by
// instantiating a page template, and 12.7.6, where an instantiation copies a
// template page the signed revision names. A page that draws other content,
// with other resources or boxes, or lists widgets that copy no template
// widget, is a change; a copy of the template, renamed fields and all, is not.
func TestEvaluateMDP_AddedPage(t *testing.T) {
	copied := spawnedAmount(" /AA << /K 24 0 R >>")
	spawn := spawned(copied, updatedObject{24, keystroke})
	page := func(extra, annots string) updatedObject {
		return updatedObject{20, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" + extra + " /Annots [" + annots + "] >>"}
	}
	own := []updatedObject{
		{2, "<< /Type /Pages /Kids [3 0 R 20 0 R] /Count 2 >>"},
		page(" /Contents 30 0 R", ""),
		{30, streamObject("BT /Helv 48 Tf 72 400 Td (PAID) Tj ET")},
	}
	const addedNoTemplate = "page 2 (object 20) was added and instantiates no page template of the signed revision"

	tests := []struct {
		name      string
		policy    int
		templates bool
		update    []updatedObject
		want      bool
		reported  string // what MDPInfo names when the verdict is a violation
	}{
		{"P=1 page added", 1, true, own, false, "added after signing"},
		{"P=2 page added drawing its own content", 2, true, own, false, addedNoTemplate},
		{"P=3 page added drawing its own content", 3, true, own, false, addedNoTemplate},
		{"P=2 page added without a signed template", 2, false, own, false, addedNoTemplate},
		{"P=2 spawn draws another content stream", 2, true, replaced(spawn,
			page(" /Contents 30 0 R", "23 0 R"),
			updatedObject{30, streamObject("BT /Helv 12 Tf 50 80 Td (Amount due: 0) Tj ET")},
		), false, addedNoTemplate},
		{"P=2 spawn redefines the template's content stream", 2, true, replaced(spawn,
			updatedObject{11, streamObject("BT /Helv 12 Tf 50 80 Td (Amount due: 0) Tj ET")},
		), false, addedNoTemplate},
		{"P=2 spawn draws with other resources", 2, true, replaced(spawn,
			page(" /Contents 11 0 R /Resources << /Font << /Helv << /Type /Font /Subtype /Type1 /BaseFont /Courier >> >> >>", "23 0 R"),
		), false, addedNoTemplate},
		{"P=2 spawn inherits another media box", 2, true, replaced(spawn,
			updatedObject{2, "<< /Type /Pages /Kids [3 0 R 40 0 R] /Count 2 >>"},
			updatedObject{40, "<< /Type /Pages /Parent 2 0 R /Kids [20 0 R] /Count 1 /MediaBox [0 0 100 100] >>"},
			updatedObject{20, "<< /Type /Page /Parent 40 0 R /Contents 11 0 R /Annots [23 0 R] >>"},
		), false, addedNoTemplate},
		{"P=2 spawn runs a page action", 2, true, replaced(spawn,
			page(" /Contents 11 0 R /AA << /O 30 0 R >>", "23 0 R"),
			updatedObject{30, "<< /S /JavaScript /JS (app.alert\\(1\\)) >>"},
		), false, addedNoTemplate},
		{"P=2 spawn lists a signed field's widget", 2, true, replaced(spawn,
			page(" /Contents 11 0 R", "23 0 R 6 0 R"),
		), false, addedNoTemplate},

		{"P=2 faithful spawn", 2, true, spawn, true, ""},
		{"P=3 faithful spawn", 3, true, spawn, true, ""},
		{"P=2 spawn copies the content stream", 2, true, replaced(spawn,
			page(" /Contents 30 0 R", "23 0 R"),
			updatedObject{30, streamObject("BT /Helv 12 Tf 50 80 Td (Amount) Tj ET")},
		), true, ""},
		{"P=2 spawn inherits the template's media box", 2, true, replaced(spawn,
			updatedObject{2, "<< /Type /Pages /Kids [3 0 R 20 0 R] /Count 2 /MediaBox [0 0 612 792] >>"},
			updatedObject{20, "<< /Type /Page /Parent 2 0 R /Contents 11 0 R /Annots [23 0 R] >>"},
		), true, ""},
		{"P=2 spawn signed on the new page", 2, true, replaced(spawn,
			page(" /Contents 11 0 R", "23 0 R 40 0 R"),
			updatedObject{40, "<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /Rect [300 300 400 350] /P 20 0 R /V 41 0 R >>"},
			updatedObject{41, "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 1 1] /Contents <30> >>"},
			updatedObject{7, "<< /Fields [4 0 R 6 0 R 9 0 R 14 0 R 21 0 R 40 0 R] /SigFlags 3 /DA (/Helv 0 Tf 0 g) >>"},
		), true, ""},
		{"P=3 spawn with a comment on the new page", 3, true, replaced(spawn,
			page(" /Contents 11 0 R", "23 0 R 40 0 R"),
			updatedObject{40, "<< /Type /Annot /Subtype /Text /Rect [300 300 320 320] /P 20 0 R /Contents (checked) >>"},
		), true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := templateCertifiedPDF(tc.policy, tc.templates)
			result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
			if strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: a comparison did not run", result.MDPInfo)
			}
		})
	}
}

// TestEvaluateMDP_PageAddedByCrossReference covers ISO 32000-1, 12.8.2.2,
// Table 254, which permits no change at P=1 and adds a page only by
// instantiating a template from P=2, for an update that appends no object: its
// cross-reference section points the page tree back at the definition the
// signed revision replaced, which lists one more page (7.5.6).
func TestEvaluateMDP_PageAddedByCrossReference(t *testing.T) {
	for _, policy := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("P=%d", policy), func(t *testing.T) {
			base := appendUpdate(t, twoPageCertifiedPDF(policy), updatedObject{2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"})
			result := mdpVerdict(t, restoreObject(t, base, 2), len(base), policy)
			if result.MDPValid {
				t.Fatalf("MDPValid = true, want false (MDPInfo %q)", result.MDPInfo)
			}
			if !strings.Contains(result.MDPInfo, "page 2 (object 8) was added") {
				t.Errorf("MDPInfo %q does not name the added page", result.MDPInfo)
			}
		})
	}
}

// TestEvaluateMDP_AddedPageCompareBound pins [maxTemplatePageCompare]: a
// template whose resources reach thousands of objects, instantiated on
// hundreds of pages, costs more pairs than the bound, and the page whose
// comparison runs out is reported rather than compared without end.
func TestEvaluateMDP_AddedPageCompareBound(t *testing.T) {
	const resources, pages = 5000, 300
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /Names << /Templates 9 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		certification(2),
		"<< /Type /Page /MediaBox [0 0 612 792] /Contents 8 0 R /Resources 10 0 R >>",
		"<< /Fields [4 0 R] /SigFlags 3 >>",
		streamObject("BT ET"),
		"<< /Names [(t) 6 0 R] >>",
	}
	var props strings.Builder
	for i := range resources {
		fmt.Fprintf(&props, "%d 0 R ", 11+i)
	}
	objs = append(objs, "<< /Properties << /P0 << /Kids ["+props.String()+"] >> >> >>")
	for i := range resources {
		objs = append(objs, fmt.Sprintf("<< /N %d >>", i))
	}
	base := buildSigPDF(objs, "")

	first := 11 + resources
	kids := "3 0 R"
	var update []updatedObject
	for i := range pages {
		kids += fmt.Sprintf(" %d 0 R", first+i)
		update = append(update, updatedObject{first + i, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 8 0 R /Resources 10 0 R >>"})
	}
	update = append(update, updatedObject{2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, pages+1)})

	result := mdpVerdict(t, appendUpdate(t, base, update...), len(base), 2)
	if result.MDPValid || !strings.Contains(result.MDPInfo, "could not be compared") {
		t.Fatalf("MDPValid = %v, MDPInfo %q: want the comparison bound reported", result.MDPValid, result.MDPInfo)
	}
}
