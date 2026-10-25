package pdf

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// taggedParagraphPDF builds a tagged one-page document whose paragraph,
// "Keep SECRET words" in 12-point Helvetica at (100, 700), is one
// marked-content sequence, MCID 0, under the /P element object 10. props is
// the sequence's property list after /MCID 0, and elem the element's entries
// after /K 0.
func taggedParagraphPDF(props, elem string) []byte {
	body := "/P <</MCID 0" + props + ">> BDC BT /F1 12 Tf 100 700 Td (Keep SECRET words) Tj ET EMC"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 8 0 R /MarkInfo << /Marked true >> >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R /StructParents 0 >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /StructTreeRoot /K 10 0 R /ParentTree << /Nums [0 [10 0 R]] >> /ParentTreeNextKey 1 >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /P /P 8 0 R /Pg 3 0 R /K 0" + elem + " >>\nendobj\n" +
		"trailer\n<< /Size 11 /Root 1 0 R >>\n%%EOF\n")
}

// coversMiddleWord takes "SECRET" of "Keep SECRET words" and neither space.
var coversMiddleWord = RedactRegion{Page: 1, Rect: [4]float64{133, 690, 179, 730}}

// TestWriterRedactKeepsTheRestOfATaggedParagraphTagged pins the output of a
// redaction that cuts a structure element's content: the removed word is gone
// from the page and from what the tree reads, and the words on either side
// are still marked content of the paragraph, MCID 0, in their order (ISO
// 32000-1, 14.7.4.2; ISO 14289-1, 7.1). Before, the redaction refused.
func TestWriterRedactKeepsTheRestOfATaggedParagraphTagged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out := redactAndWrite(t, taggedParagraphPDF("", ""), []RedactRegion{coversMiddleWord})
	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if strings.Contains(text, "SECRET") || !strings.Contains(text, "Keep") || !strings.Contains(text, "words") {
		t.Errorf("text = %q, want Keep and words without SECRET", text)
	}
	elements, err := r.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	var kept strings.Builder
	for _, el := range elements {
		if el.MCID != 0 {
			t.Errorf("%q carries MCID %d, want the paragraph's 0", el.Text, el.MCID)
		}
		kept.WriteString(el.Text)
	}
	if got := strings.Join(strings.Fields(kept.String()), " "); got != "Keep words" {
		t.Errorf("tagged text = %q, want %q", got, "Keep words")
	}
}

// TestWriterRedactRefusesTextSpanningTheCut pins the two copies of the
// paragraph's text a cut cannot divide: the element's /ActualText refuses
// before anything is written, and the marked-content sequence's own
// /ActualText (ISO 32000-1, 14.9.4), which the tree does not hold, is found in
// the rewritten content by the output scan.
func TestWriterRedactRefusesTextSpanningTheCut(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		props, elem string
		want        error
	}{
		"element":           {elem: " /ActualText (Keep SECRET words)", want: ErrRedactionRefused},
		"marked content":    {props: " /ActualText (Keep SECRET words)", want: ErrRedactionIncomplete},
		"element alt":       {elem: " /Alt (Keep SECRET words)", want: ErrRedactionRefused},
		"element expansion": {elem: " /E (Keep SECRET words)", want: ErrRedactionRefused},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			src := taggedParagraphPDF(tc.props, tc.elem)
			r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			w := NewWriter(r)
			defer w.Close()
			_, err = w.Redact(ctx, []RedactRegion{coversMiddleWord})
			if err == nil {
				err = w.WriteTo(ctx, &bytes.Buffer{})
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
