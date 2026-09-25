package remediation

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// wholeStreamsPDF returns a one-page PDF whose page content is data.
func wholeStreamsPDF(data string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(data), data),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestRemediationReadsContentStreamsWhole pins that remediation repairs the
// operators a viewer draws where validation, reading a stream as veraPDF
// does, reads no further: past a stray "]", and in a stream holding an array
// veraPDF cannot read, which validation judges nothing of. The undefined
// operators there go with the ones validation reports.
func TestRemediationReadsContentStreamsWhole(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name, content string
		judged        int
		fix           string
	}{
		{"past a stray ]", "q Q ] zz\n", 0, "removed 1 unknown content-stream operator(s)"},
		{"beside an array veraPDF cannot read", "q Q [1 xx] TJ zz\n", 0, "removed 1 unknown content-stream operator(s)"},
	} {
		data := wholeStreamsPDF(tc.content)
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.name, err)
		}
		viols, err := compliance.ValidatePDFA(ctx, doc, models.PDFA1b)
		if err != nil {
			t.Fatalf("%s: ValidatePDFA: %v", tc.name, err)
		}
		judged := 0
		for _, e := range viols {
			if v, ok := e.(compliance.Violation); ok && v.Rule == compliance.Rule_19005_1_6_2_10_t01 {
				judged++
			}
		}
		if judged != tc.judged {
			t.Fatalf("%s: validation reports %d undefined operators, want %d", tc.name, judged, tc.judged)
		}
		report, err := RemediatePDFA(ctx, doc, writer.NewObjAllocator(doc.HighestObjectNumber()), models.PDFA1b, Options{})
		if err != nil {
			t.Fatalf("%s: RemediatePDFA: %v", tc.name, err)
		}
		if !slices.Contains(report.Fixed, tc.fix) {
			t.Errorf("%s: remediation fixed %q, want %q among them", tc.name, report.Fixed, tc.fix)
		}
	}
}
