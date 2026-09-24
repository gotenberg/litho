package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// clipPathTextPDF draws one visible word and one that the graphics-state
// clipping path excludes.
//
// The clip admits y>=600 while the second run is painted at y=100, so a
// conforming viewer paints nothing for it. It is still text in the file, which
// is what -clip-path exists to reach.
//
// The 0 Tr is not decoration: the text rendering mode lives in the graphics
// state (ISO 32000-1, 8.4.2, Table 104) and ET does not reset it, so without
// it a preceding mode-3 run would leak in and the clipped run would also be
// invisible, making it impossible to tell which flag revealed it.
func clipPathTextPDF() []byte {
	cs := "BT 0 Tr /F1 12 Tf 72 700 Td (PAINTED) Tj ET\n" +
		"q 0 600 612 192 re W n BT 0 Tr /F1 12 Tf 72 100 Td (CLIPAWAY) Tj ET Q\n"
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

// TestText_ClipPathFlagReachesPathClippedText pins the CLI surface for the
// fourth strip flag.
//
// litho has four categories of normally-stripped text but long had only three
// granular options, so text excluded by the graphics-state clipping path could
// be reached only by riding -invisible or by -hidden. -clip-path gives it an
// option of its own; -invisible must no longer return it.
//
// A CLI test is the only thing watching this flag.
//
// # Corpus evidence
//
//   - All corpora: no corpus gate reaches cmd/litho.
func TestText_ClipPathFlagReachesPathClippedText(t *testing.T) {
	path := writeTempPDF(t, clipPathTextPDF())

	run1 := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), append(args, path), &stdout, &stderr); rc != exitOK {
			t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
		}
		return stdout.String()
	}

	plain := run1("text")
	if !strings.Contains(plain, "PAINTED") {
		t.Fatalf("precondition: the painted word did not extract, so nothing below is meaningful: %q", plain)
	}
	if strings.Contains(plain, "CLIPAWAY") {
		t.Fatalf("precondition: path-clipped text survived a default extraction, so this fixture proves nothing: %q", plain)
	}

	for _, flag := range []string{"-clip-path", "-hidden"} {
		got := run1("text", flag)
		if !strings.Contains(got, "CLIPAWAY") {
			t.Errorf("%s did not reach the path-clipped text: %q", flag, got)
		}
		if !strings.Contains(got, "PAINTED") {
			t.Errorf("%s lost the painted word: %q", flag, got)
		}
	}

	// The separation is the point: -invisible covers rendering mode 3 and
	// white-on-white, and must not quietly carry this category too.
	inv := run1("text", "-invisible")
	if strings.Contains(inv, "CLIPAWAY") {
		t.Errorf("-invisible returned path-clipped text: %q\n"+
			"that category belongs to -clip-path", inv)
	}
	if !strings.Contains(inv, "PAINTED") {
		t.Errorf("-invisible lost the painted word: %q", inv)
	}
}
