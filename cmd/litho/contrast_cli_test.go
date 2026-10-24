package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// contrastTextPDF sets one word in black and one in 0.6 grey, about 2.85:1 on
// white, on a 400-point square page.
func contrastTextPDF() []byte {
	cs := "BT /F1 12 Tf 20 360 Td (DARKWORD) Tj ET\nBT 0.6 g /F1 12 Tf 20 320 Td (PALEWORD) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, 6)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		buf.WriteString(itoa(i+1) + " 0 obj\n" + body + "\nendobj\n")
	}
	offsets[5] = buf.Len()
	buf.WriteString("5 0 obj\n<< /Length " + itoa(len(cs)) + " >>\nstream\n" + cs + "endstream\nendobj\n")
	start := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		buf.WriteString(pad10(offsets[i]) + " 00000 n \n")
	}
	buf.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n" + itoa(start) + "\n%%EOF\n")
	return buf.Bytes()
}

// TestContrast_ListsTextShortOfWCAG pins the CLI surface of
// [pdf.Reader.CheckTextContrast]: the grey word is listed with its ratio, the
// black one is not, and a finding leaves the exit status at 0, the check being
// advisory.
func TestContrast_ListsTextShortOfWCAG(t *testing.T) {
	path := writeTempPDF(t, contrastTextPDF())

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"contrast", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "page 1: 2.85:1 < 4.5:1 (12.0pt) #999999 on #ffffff: PALEWORD") {
		t.Errorf("contrast = %q, want the grey word at 2.85:1", got)
	}
	if strings.Contains(got, "DARKWORD") {
		t.Errorf("contrast listed the black word: %q", got)
	}

	stdout.Reset()
	if rc := run(context.Background(), []string{"contrast", "-f", "json", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	var res contrastResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("json: %v: %s", err, stdout.String())
	}
	if len(res.Pages) != 1 || res.Pages[0].Report.Judged != 2 || len(res.Pages[0].Report.Failures) != 1 {
		t.Errorf("json pages = %+v, want two runs judged, one failure", res.Pages)
	}
}
