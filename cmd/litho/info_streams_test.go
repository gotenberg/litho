package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"strings"
	"testing"
)

// salvagedContentPDF returns a one-page document whose content stream, object
// 4, is FlateDecode data without its zlib checksum.
func salvagedContentPDF(t *testing.T) []byte {
	t.Helper()
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	if _, err := w.Write([]byte("0 0 m 10 10 l S")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := z.Bytes()[:z.Len()-4]
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", len(data), data),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// TestInfo_Streams_ReportsSalvaged pins that `info --streams` lists a stream
// whose data litho salvages, in text and in JSON, and reports none for a
// sound document.
func TestInfo_Streams_ReportsSalvaged(t *testing.T) {
	path := writeTempPDF(t, salvagedContentPDF(t))
	var stdout, stderr bytes.Buffer
	if rc := runInfo(context.Background(), []string{"--streams", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit = %d, stderr: %s", rc, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "Salvaged:    1 stream(s)\n  obj 4: FlateDecode") {
		t.Errorf("text output lacks the salvaged stream:\n%s", out)
	}

	stdout.Reset()
	if rc := runInfo(context.Background(), []string{"--streams", "-f", "json", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit = %d, stderr: %s", rc, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, `"salvagedStreams"`) || !strings.Contains(out, `"objectNum": 4`) {
		t.Errorf("JSON output lacks the salvaged stream:\n%s", out)
	}

	stdout.Reset()
	if rc := runInfo(context.Background(), []string{"--streams", writeTempPDF(t, minimalPDF())}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit = %d, stderr: %s", rc, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "Salvaged:    none") {
		t.Errorf("sound document reports salvage:\n%s", out)
	}
}
