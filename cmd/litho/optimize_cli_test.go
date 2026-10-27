package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pdfa1DeclaredPDF builds a 20-page document whose catalog XMP packet
// declares pdfaid:part 1. Twenty page dictionaries give packing enough small
// objects to shrink, so the undeclared control is packed.
func pdfa1DeclaredPDF() []byte {
	const xmp = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">` +
		`<pdfaid:part>1</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
	const pages = 20
	const content = "0 0 1 rg 10 10 100 100 re f"
	kids := make([]string, pages)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 5+i)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 3 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(xmp), xmp),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	for range pages {
		objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>")
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// runCLI runs litho with args and returns the exit code and stderr.
func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), args, &stdout, &stderr)
	return rc, stderr.String()
}

// readOutput returns the bytes of a file a command wrote.
func readOutput(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is the test's own temp file
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return data
}

// TestOptimize_RunsEveryPassByDefault pins that optimize, given no flag,
// packs objects and recompresses the uncompressed image, so the output is
// smaller than the input and still has both pages.
func TestOptimize_RunsEveryPassByDefault(t *testing.T) {
	t.Parallel()
	src := imageFixtureFile(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	if rc, stderr := runCLI(t, "optimize", src, "-o", out); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr)
	}
	data := readOutput(t, out)
	if !bytes.Contains(data, []byte("/ObjStm")) {
		t.Error("the output carries no object stream, so packing did not run")
	}
	if len(data) >= len(imageCompactFixturePDF()) {
		t.Errorf("output is %d bytes, input %d: the uncompressed image was not recompressed", len(data), len(imageCompactFixturePDF()))
	}
	if n := pageCountOf(t, out); n != 2 {
		t.Errorf("output has %d pages, want 2", n)
	}
}

// TestOptimize_NoFlagsTurnPassesOff pins that each --no- flag removes its
// pass, and that turning every pass off is refused rather than rewriting the
// file for nothing.
func TestOptimize_NoFlagsTurnPassesOff(t *testing.T) {
	t.Parallel()
	src := imageFixtureFile(t)

	out := filepath.Join(t.TempDir(), "out.pdf")
	if rc, stderr := runCLI(t, "optimize", src, "--no-compact", "-o", out); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr)
	}
	if bytes.Contains(readOutput(t, out), []byte("/ObjStm")) {
		t.Error("--no-compact still packed objects into an object stream")
	}

	none := filepath.Join(t.TempDir(), "none.pdf")
	rc, stderr := runCLI(t, "optimize", src, "--no-compact", "--no-recompress-images", "--no-subset-fonts", "-o", none)
	if rc != exitError || !strings.Contains(stderr, "leaves nothing to optimize") {
		t.Errorf("every pass off: exit %d, stderr %q, want a refusal", rc, stderr)
	}
	if _, err := os.Stat(none); !os.IsNotExist(err) {
		t.Error("the refused run left an output file")
	}
}

// TestOptimize_RefusesSignedInput pins that optimize refuses a signed input,
// whose signatures its rewrite cannot keep (ISO 32000-1, 12.8.1), unless
// --allow-signature-loss, which writes the output and warns per signature as
// every rewriting command does.
func TestOptimize_RefusesSignedInput(t *testing.T) {
	t.Parallel()
	src := writeTempPDF(t, signedTwoPagePDF())

	for _, args := range [][]string{
		{"optimize", src},
		{"pipe", src, "optimize"},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		rc, stderr := runCLI(t, append(args, "-o", out)...)
		if rc != exitError || !strings.Contains(stderr, `the input is signed ("approval")`) {
			t.Errorf("%v: exit %d, stderr %q, want a refusal naming the field", args, rc, stderr)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("%v: the refused run left an output file", args)
		}
	}

	for _, args := range [][]string{
		{"optimize", src, "--allow-signature-loss"},
		{"pipe", src, "optimize", "--allow-signature-loss"},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		rc, stderr := runCLI(t, append(args, "-o", out)...)
		if rc != exitOK {
			t.Fatalf("%v: exit %d, stderr: %s", args, rc, stderr)
		}
		for _, want := range signatureWarnings {
			if !strings.Contains(stderr, want) {
				t.Errorf("%v: stderr lacks %q: %q", args, want, stderr)
			}
		}
	}
}

// TestOptimize_DoesNotPackADeclaredPDFA1Input pins that optimize keeps a
// cross-reference stream out of an input declaring PDF/A-1, which ISO
// 19005-1, 6.1.4 forbids, and says so, while a PDF/A-2 declaration or none
// would be packed.
func TestOptimize_DoesNotPackADeclaredPDFA1Input(t *testing.T) {
	t.Parallel()
	src := writeTempPDF(t, pdfa1DeclaredPDF())

	for _, args := range [][]string{
		{"optimize", src},
		{"pipe", src, "optimize"},
		{"pipe", src, "rotate", "-angle", "90", "optimize"},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		rc, stderr := runCLI(t, append(args, "-o", out)...)
		if rc != exitOK {
			t.Fatalf("%v: exit %d, stderr: %s", args, rc, stderr)
		}
		if !strings.Contains(stderr, "the input declares PDF/A-1") {
			t.Errorf("%v: no warning that packing was withheld: %q", args, stderr)
		}
		data := readOutput(t, out)
		if bytes.Contains(data, []byte("/ObjStm")) || bytes.Contains(data, []byte("/XRef")) {
			t.Errorf("%v: the output carries an object stream or a cross-reference stream", args)
		}
	}

	// The control: the same document without the declaration is packed.
	plain := writeTempPDF(t, bytes.Replace(pdfa1DeclaredPDF(), []byte("<pdfaid:part>1<"), []byte("<pdfaid:part>2<"), 1))
	out := filepath.Join(t.TempDir(), "out.pdf")
	if rc, stderr := runCLI(t, "optimize", plain, "-o", out); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr)
	}
	if !bytes.Contains(readOutput(t, out), []byte("/ObjStm")) {
		t.Error("a PDF/A-2 declaration was not packed, so the PDF/A-1 check proves nothing")
	}
}

// TestPipe_OptimizeStage pins the optimize stage: alone it writes what the
// standalone command writes, it may follow any stage, it combines with the
// global flags, and a second stage or a --no- flag against a global flag is
// refused.
func TestPipe_OptimizeStage(t *testing.T) {
	t.Parallel()
	src := imageFixtureFile(t)

	standalone := filepath.Join(t.TempDir(), "standalone.pdf")
	if rc, stderr := runCLI(t, "optimize", src, "-o", standalone); rc != exitOK {
		t.Fatalf("optimize: exit %d, stderr: %s", rc, stderr)
	}
	piped := filepath.Join(t.TempDir(), "piped.pdf")
	if rc, stderr := runCLI(t, "pipe", src, "optimize", "-o", piped); rc != exitOK {
		t.Fatalf("pipe optimize: exit %d, stderr: %s", rc, stderr)
	}
	if !bytes.Equal(readOutput(t, standalone), readOutput(t, piped)) {
		t.Error("pipe optimize wrote other bytes than optimize")
	}

	// rotate alone writes an incremental update, which carries no object
	// stream: optimize after it makes the final write a packed rewrite.
	rotated := filepath.Join(t.TempDir(), "rotated.pdf")
	if rc, stderr := runCLI(t, "pipe", src, "optimize", "--no-recompress-images", "rotate", "-angle", "90", "-o", rotated); rc != exitOK {
		t.Fatalf("pipe optimize rotate: exit %d, stderr: %s", rc, stderr)
	}
	if !bytes.Contains(readOutput(t, rotated), []byte("/ObjStm")) {
		t.Error("optimize before rotate did not pack the final write")
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"optimize", "optimize"}, "only one optimize stage"},
		{[]string{"--compact", "optimize", "--no-compact"}, "--no-compact contradicts the pipe's --compact"},
		{[]string{"optimize", "--no-compact", "--no-recompress-images", "--no-subset-fonts"}, "leaves nothing to optimize"},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		rc, stderr := runCLI(t, append(append([]string{"pipe", src}, tc.args...), "-o", out)...)
		if rc != exitError || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d, stderr %q, want a refusal containing %q", tc.args, rc, stderr, tc.want)
		}
	}
}

// TestOptimize_WarnsThatAnEncryptedInputIsWrittenDecrypted pins that optimize
// reports the confidentiality downgrade every rewriting command reports, the
// pipe stage included when it is the only stage.
func TestOptimize_WarnsThatAnEncryptedInputIsWrittenDecrypted(t *testing.T) {
	t.Parallel()
	enc := encryptTempPDF(t, twoPageTempPDF(t), "s3cret")

	for _, args := range [][]string{
		{"optimize", "-p", "s3cret", enc},
		{"pipe", enc, "-p", "s3cret", "optimize"},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		rc, stderr := runCLI(t, append(args, "-o", out)...)
		if rc != exitOK {
			t.Fatalf("%v: exit %d, stderr: %s", args, rc, stderr)
		}
		if !strings.Contains(stderr, encryptionWarning) {
			t.Errorf("%v: no encryption warning: %q", args, stderr)
		}
		if !opensWithoutAPassword(t, out) {
			t.Errorf("%v: the output is still encrypted", args)
		}
	}
}
