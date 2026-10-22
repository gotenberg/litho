package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestFlattenTransparency_Overprint pins --overprint and the report of the
// overprinting objects a composite takes in, in text and JSON.
func TestFlattenTransparency_Overprint(t *testing.T) {
	t.Parallel()
	content := "1 0 0 0 k 0 0 100 100 re f q /OP gs 0 1 0 0 k 0 0 100 100 re f Q q /Half gs 0 0 0 1 k 40 40 20 20 re f Q"
	src := writeTempPDF(t, renderPrintPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /ExtGState << /Half 5 0 R /OP 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /ExtGState /ca 0.5 >>",
		"<< /Type /ExtGState /OP true /OPM 1 >>",
	}))
	for _, flags := range [][]string{nil, {"--overprint"}} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		args := append(append([]string{"flatten-transparency", "--dpi", "72", "-f", "json"}, flags...), "-o", out, src)
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
			t.Fatalf("%q: exit %d: %s", args, rc, stderr.String())
		}
		var report flattenTransparencyResult
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("report %q: %v", stdout.String(), err)
		}
		if len(report.Pages) != 1 || report.Pages[0].Overprinted != 1 {
			t.Errorf("%q: report %+v", args, report)
		}
	}
	var text, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"flatten-transparency", "--dpi", "72", "-o", filepath.Join(t.TempDir(), "out.pdf"), src}, &text, &stderr); rc != exitOK {
		t.Fatalf("text: exit %d: %s", rc, stderr.String())
	}
	if !strings.Contains(text.String(), "1 object(s) painting under overprint composited into an image") {
		t.Errorf("text report %q", text.String())
	}
}
