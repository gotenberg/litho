package pdf

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// otherPageCopyPDF builds a two-page document. Page 1 draws "SECRET" at y=700,
// which [coversSecret] removes, and "KEEP" at y=600. Page 2 holds another copy
// of "SECRET" in the place kind names:
//
//   - "content": page 2's own content stream draws it.
//   - "form": a form XObject page 2 paints draws it.
//   - "appearance": the normal appearance of an annotation on page 2 draws it.
//   - "sharedform": the form page 2 paints is also in page 1's resources,
//     though page 1 does not paint it.
//   - "xmp": page 2's content draws it and page 2's XMP metadata stream holds it.
func otherPageCopyPDF(kind string) []byte {
	stream := func(num int, dict, data string) string {
		return strconv.Itoa(num) + " 0 obj\n<< " + dict + " /Length " + strconv.Itoa(len(data)) +
			" >>\nstream\n" + data + "\nendstream\nendobj\n"
	}
	secret := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	keep := "BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"
	form := "/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >>"

	page1Res := "<< /Font << /F1 4 0 R >> >>"
	page2Res := "<< /Font << /F1 4 0 R >> >>"
	page2Extra := ""
	page2Body := secret
	extra := ""
	switch kind {
	case "form", "sharedform":
		page2Res = "<< /Font << /F1 4 0 R >> /XObject << /Fm 9 0 R >> >>"
		page2Body = "/Fm Do"
		extra = stream(9, form, secret)
		if kind == "sharedform" {
			page1Res = page2Res
		}
	case "appearance":
		page2Body = keep
		page2Extra = " /Annots [10 0 R]"
		extra = stream(9, form, secret) +
			"10 0 obj\n<< /Type /Annot /Subtype /Square /Rect [0 0 612 792] /AP << /N 9 0 R >> >>\nendobj\n"
	case "xmp":
		page2Extra = " /Metadata 11 0 R"
		extra = stream(11, "/Type /Metadata /Subtype /XML",
			`<x:xmpmeta xmlns:x="adobe:ns:meta/"><dc:title>SECRET</dc:title></x:xmpmeta>`)
	}

	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R 7 0 R] /Count 2 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources " + page1Res + " /Contents 6 0 R >>\nendobj\n" +
		stream(6, "", secret+" "+keep) +
		"7 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources " + page2Res + " /Contents 8 0 R" + page2Extra + " >>\nendobj\n" +
		stream(8, "", page2Body) +
		extra +
		"trailer\n<< /Size 12 /Root 1 0 R >>\n%%EOF\n")
}

// redactOtherPageCopy redacts "SECRET" off page 1 and writes the result.
func redactOtherPageCopy(t *testing.T, src []byte, opts ...RedactOption) (*RedactReport, []byte, error) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	report, err := w.Redact(ctx, []RedactRegion{coversSecret}, opts...)
	if err != nil {
		return report, nil, err
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo after a redaction that succeeded: %v", err)
	}
	return report, out.Bytes(), nil
}

func TestWriter_Redact_WarnsAboutCopiesOtherPagesDraw(t *testing.T) {
	cases := []struct {
		kind   string
		object int
	}{
		{kind: "content", object: 8},
		{kind: "form", object: 9},
		{kind: "appearance", object: 9},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			report, out, err := redactOtherPageCopy(t, otherPageCopyPDF(tc.kind))
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if len(report.SurvivingCopies) != 0 {
				t.Errorf("SurvivingCopies = %+v, want none on a redaction that succeeded", report.SurvivingCopies)
			}
			found := slices.ContainsFunc(report.CopiesOnOtherPages, func(h RedactionHit) bool {
				return h.ObjectNum == tc.object && h.Token == "SECRET" && slices.Equal(h.Pages, []int{2})
			})
			if !found {
				t.Errorf("CopiesOnOtherPages = %+v, want SECRET in object %d on page 2", report.CopiesOnOtherPages, tc.object)
			}

			ctx := context.Background()
			r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			first, err := r.ExtractText(ctx, 1, WithHiddenText())
			if err != nil {
				t.Fatalf("ExtractText page 1: %v", err)
			}
			if strings.Contains(first, "SECRET") || !strings.Contains(first, "KEEP") {
				t.Errorf("page 1 reads %q, want KEEP without SECRET", first)
			}
		})
	}
}

func TestWriter_Redact_StrictRefusesCopiesOtherPagesDraw(t *testing.T) {
	report, _, err := redactOtherPageCopy(t, otherPageCopyPDF("content"), WithStrictRedaction())
	if !errors.Is(err, ErrRedactionIncomplete) {
		t.Fatalf("err = %v, want ErrRedactionIncomplete", err)
	}
	if report == nil || !slices.ContainsFunc(report.SurvivingCopies, func(h RedactionHit) bool { return h.ObjectNum == 8 }) {
		t.Errorf("report = %+v, want object 8 among SurvivingCopies", report)
	}
	if report != nil && len(report.CopiesOnOtherPages) != 0 {
		t.Errorf("CopiesOnOtherPages = %+v, want none under strict", report.CopiesOnOtherPages)
	}
}

func TestWriter_Redact_RefusesCopiesBeyondOtherPagesContent(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		object int
	}{
		// The redacted page names the form in its own resources, so the copy
		// is the redacted page's as much as page 2's.
		{kind: "sharedform", object: 9},
		// XMP is not content any page draws.
		{kind: "xmp", object: 11},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			report, _, err := redactOtherPageCopy(t, otherPageCopyPDF(tc.kind))
			if !errors.Is(err, ErrRedactionIncomplete) {
				t.Fatalf("err = %v, want ErrRedactionIncomplete", err)
			}
			if report == nil || !slices.ContainsFunc(report.SurvivingCopies, func(h RedactionHit) bool { return h.ObjectNum == tc.object }) {
				t.Errorf("report = %+v, want object %d among SurvivingCopies", report, tc.object)
			}
		})
	}
}

// TestWriter_Redact_CopiesOnOtherPagesHoldThroughAutoTag drives WriteTo's
// re-application: auto-tagging rebuilds page 1 from the source, so WriteTo
// redacts it again and sanitizes once more, and the copy page 2 draws must stay
// tolerated there as it was in Redact. Under strict, Redact refuses first.
func TestWriter_Redact_CopiesOnOtherPagesHoldThroughAutoTag(t *testing.T) {
	ctx := context.Background()
	src := otherPageCopyPDF("content")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{coversSecret}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	w.AutoTag()

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	first, err := r2.ExtractText(ctx, 1, WithHiddenText())
	if err != nil {
		t.Fatalf("ExtractText page 1: %v", err)
	}
	if strings.Contains(first, "SECRET") {
		t.Errorf("page 1 reads %q after auto-tagging, want SECRET gone", first)
	}
	second, err := r2.ExtractText(ctx, 2, WithHiddenText())
	if err != nil {
		t.Fatalf("ExtractText page 2: %v", err)
	}
	if !strings.Contains(second, "SECRET") {
		t.Errorf("page 2 reads %q, want its own SECRET kept", second)
	}
}
