package pdf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
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
//   - "images": no other copy of the text, but page 2 paints a JPEG whose
//     bytes spell it in a comment segment, and page 1 paints, outside the
//     region, an uncompressed image whose samples spell it.
//   - "inline": no other copy of the text, but both pages paint an inline
//     image whose samples spell it, page 1 outside the region.
//   - "inlinetext": as "inline", and page 1 draws the text again outside the
//     region, next to its inline image.
func otherPageCopyPDF(kind string) []byte {
	stream := func(num int, dict, data string) string {
		return strconv.Itoa(num) + " 0 obj\n<< " + dict + " /Length " + strconv.Itoa(len(data)) +
			" >>\nstream\n" + data + "\nendstream\nendobj\n"
	}
	secret := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	keep := "BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"
	form := "/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >>"

	page1Res := "<< /Font << /F1 4 0 R >> >>"
	page1Body := secret + " " + keep
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
	case "images":
		page1Res = "<< /Font << /F1 4 0 R >> /XObject << /Im 10 0 R >> >>"
		page1Body += " q 60 0 0 10 100 100 cm /Im Do Q"
		page2Res = "<< /XObject << /Im 9 0 R >> >>"
		page2Body = "q 80 0 0 80 100 100 cm /Im Do Q"
		image := "/Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /BitsPerComponent 8"
		extra = stream(9, image+" /Filter /DCTDecode", string(jpegWithComment("SECRET"))) +
			stream(10, "/Type /XObject /Subtype /Image /Width 6 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "SECRET")
	case "inline", "inlinetext":
		inline := " q 60 0 0 10 100 100 cm BI /W 6 /H 1 /CS /G /BPC 8 ID SECRET EI Q"
		page1Body += inline
		if kind == "inlinetext" {
			page1Body += " BT /F1 12 Tf 100 300 Td (SECRET) Tj ET"
		}
		page2Res = "<< >>"
		page2Body = inline
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
		stream(6, "", page1Body) +
		"7 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources " + page2Res + " /Contents 8 0 R" + page2Extra + " >>\nendobj\n" +
		stream(8, "", page2Body) +
		extra +
		"trailer\n<< /Size 12 /Root 1 0 R >>\n%%EOF\n")
}

// jpegWithComment encodes an 8 by 8 grey JPEG with a comment segment (ITU-T
// T.81, B.2.4.5, the COM marker) holding text, written after the SOI marker.
func jpegWithComment(text string) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		panic(err)
	}
	data := buf.Bytes()
	segLen := len(text) + 2
	com := append([]byte{0xFF, 0xFE, byte(segLen >> 8), byte(segLen)}, text...)
	return slices.Concat(data[:2], com, data[2:])
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

// TestWriter_Redact_IgnoresImageBytesSpellingTheText pins that the bytes of an
// image are not searched for removed text: a JPEG another page paints whose
// bytes spell the removed word, and an image on the redacted page whose
// samples do, are pixels, not a copy a reader extracts (ISO 32000-1, 8.9.5,
// 9.10.2). Both refused the redaction with ErrRedactionIncomplete before.
func TestWriter_Redact_IgnoresImageBytesSpellingTheText(t *testing.T) {
	report, out, err := redactOtherPageCopy(t, otherPageCopyPDF("images"))
	if err != nil {
		t.Fatalf("Redact: %v (surviving %+v)", err, report)
	}
	if len(report.SurvivingCopies) != 0 || len(report.CopiesOnOtherPages) != 0 {
		t.Errorf("report names image bytes as copies: surviving %+v, other pages %+v",
			report.SurvivingCopies, report.CopiesOnOtherPages)
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
}

// TestWriter_Redact_IgnoresInlineImageSamplesSpellingTheText pins that the
// samples of an inline image, the bytes between ID and EI (ISO 32000-1,
// 8.9.7), are not searched for removed text, on the redacted page or another,
// while the text drawn again next to one still refuses the redaction.
func TestWriter_Redact_IgnoresInlineImageSamplesSpellingTheText(t *testing.T) {
	report, out, err := redactOtherPageCopy(t, otherPageCopyPDF("inline"))
	if err != nil {
		t.Fatalf("Redact: %v (surviving %+v)", err, report)
	}
	if len(report.SurvivingCopies) != 0 || len(report.CopiesOnOtherPages) != 0 {
		t.Errorf("report names inline image samples as copies: surviving %+v, other pages %+v",
			report.SurvivingCopies, report.CopiesOnOtherPages)
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

	if _, _, err := redactOtherPageCopy(t, otherPageCopyPDF("inlinetext")); !errors.Is(err, ErrRedactionIncomplete) {
		t.Errorf("Redact with the text drawn again next to an inline image: %v, want ErrRedactionIncomplete", err)
	}
}
