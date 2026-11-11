package pdf

import (
	"context"
	"errors"
	"testing"
)

// TestPipeline_AutoTagBeforeASplitReportsWhyItSkipped pins that an AutoTag
// stage followed by a Split leaves a report the split fills: the Writer
// applies AutoTag when the split materializes the document, not at the final
// write, and the report says so after WriteTo, naming the reason it added no
// tree.
func TestPipeline_AutoTagBeforeASplitReportsWhyItSkipped(t *testing.T) {
	t.Parallel()

	const text = "BT /F1 12 Tf 72 700 Td (A line of text.) Tj ET"
	for _, tc := range []struct {
		name     string
		data     []byte
		want     error
		wantPage int
	}{
		// ISO 32000-1, 14.7.1: /MarkInfo /Marked true declares a tagged
		// document.
		{"already tagged", helveticaPagePDF("/MarkInfo << /Marked true >> ", streamBody(text)), ErrAutoTagAlreadyTagged, 0},
		// ISO 32000-1, 14.6: the page already opens a marked-content sequence.
		{"marked content", helveticaPagePDF("", streamBody("/P << /MCID 0 >> BDC "+text+" EMC")), ErrAutoTagMarkedContent, 1},
		// ISO 32000-1, 7.7.3.3, Table 30: /Contents is a stream or an array
		// of streams, and a dictionary is neither.
		{"unreadable content", helveticaPagePDF("", "<< /Kind /NotAStream >>"), ErrAutoTagUnreadableContent, 1},
		{"nothing to tag", helveticaPagePDF("", streamBody("72 72 100 100 re f")), ErrAutoTagNothingToTag, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var report *AutoTagReport
			p := NewPipeline().Open(openColorPDF(t, tc.data)).
				Apply(func(_ context.Context, w *Writer) error {
					report = w.AutoTag()
					return nil
				}).
				Split([]int{1})
			runPipeline(t, p)
			if report == nil || !report.Applied {
				t.Fatalf("after WriteTo the report is %+v, want it applied", report)
			}
			if !errors.Is(report.Skipped, tc.want) {
				t.Errorf("Skipped = %v, want %v", report.Skipped, tc.want)
			}
			if report.Page != tc.wantPage {
				t.Errorf("Page = %d, want %d", report.Page, tc.wantPage)
			}
		})
	}
}
