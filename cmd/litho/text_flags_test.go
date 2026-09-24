package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// invisibleTextPDF draws one visible word and one in rendering mode 3, which is
// how a scanned page carries its OCR layer (ISO 32000-1, Table 106).
func invisibleTextPDF() []byte {
	cs := "BT /F1 12 Tf 72 700 Td (VISIBLE) Tj ET\n" +
		"BT 3 Tr /F1 12 Tf 72 680 Td (OCRLAYER) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
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

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func pad10(n int) string {
	s := itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}

// TestText_InvisibleFlagReachesTheOCRLayer pins that the text command can ask
// for rendering-mode-3 text.
//
// The reader has always supported it, but the command exposed no flag, so a
// searchable scan extracted as empty. The default is deliberately unchanged;
// what was missing was any way to ask.
//
// # Corpus evidence
//
//   - pdfjs JBIG2Globals.pdf: the text command gave 0 words where poppler
//     gave 1447.
func TestText_InvisibleFlagReachesTheOCRLayer(t *testing.T) {
	path := writeTempPDF(t, invisibleTextPDF())

	run1 := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), append(args, path), &stdout, &stderr); rc != exitOK {
			t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
		}
		return stdout.String()
	}

	plain := run1("text")
	if !strings.Contains(plain, "VISIBLE") {
		t.Errorf("default extraction lost the visible word: %q", plain)
	}
	if strings.Contains(plain, "OCRLAYER") {
		t.Errorf("default extraction included mode-3 text; the default is meant to stay as it was: %q", plain)
	}

	for _, flag := range []string{"-invisible", "-hidden"} {
		got := run1("text", flag)
		if !strings.Contains(got, "OCRLAYER") {
			t.Errorf("%s did not reach the mode-3 text: %q", flag, got)
		}
		if !strings.Contains(got, "VISIBLE") {
			t.Errorf("%s lost the visible word: %q", flag, got)
		}
	}
}
