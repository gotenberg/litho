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

// compactFixturePDF builds a two-page document carrying enough padded
// dictionaries that packing them pays for an object stream's own overhead.
//
// Size is the point of the fixture. The writer declines to pack a batch it
// cannot shrink, so a handful of short objects would exercise that guard
// rather than the flag, and the assertions below would hold while nothing was
// packed. The xref offsets are computed for the reason formFixturePDF gives: a
// stale offset makes the file unopenable and fails the test for an unrelated
// reason.
func compactFixturePDF() []byte {
	pad := strings.Repeat("padding for a realistic dictionary body ", 6)

	// The padded objects hang off the SECOND page's /Annots, and that
	// matters. A full rewrite copies outward from the catalog and drops
	// whatever it cannot reach, so free-floating objects are garbage
	// collected before packing ever sees them: the first version of this
	// fixture left them unreferenced and both outputs came back at 331
	// bytes, measuring the collector rather than the flag. Deleting page 1
	// keeps page 2, so every padded object survives into the rewrite.
	const nAnnots = 14
	const firstAnnotObj = 5

	annotRefs := make([]string, 0, nAnnots)
	for i := range nAnnots {
		annotRefs = append(annotRefs, fmt.Sprintf("%d 0 R", firstAnnotObj+i))
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [%s] >>",
			strings.Join(annotRefs, " ")),
	}
	for i := range nAnnots {
		objs = append(objs, fmt.Sprintf(
			"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /Index %d /Note (%s) >>", i, pad))
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

func compactFixtureFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "in.pdf")
	if err := os.WriteFile(path, compactFixturePDF(), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

// runDeleteTo runs the delete command, which always forces a full rewrite, and
// returns the bytes it wrote.
func runDeleteTo(t *testing.T, src string, extra ...string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.pdf")
	args := append([]string{"delete", "-pages", "1", "-o", out}, extra...)
	args = append(args, src)

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("delete %v: exit %d, stderr: %s", extra, rc, stderr.String())
	}
	data, err := os.ReadFile(out) //nolint:gosec // path is this test's own temp file
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return data
}

// TestCompactFlag_PacksAndShrinksAFullRewrite is what the flag exists for: a
// command that rewrites the whole file must be able to compact it.
//
// The precondition carries half the test. Asserting the plain output contains
// no object stream is what stops the packed assertion passing on a document
// that was already packed, and comparing against the same command without the
// flag keeps the saving attributable to compaction rather than to the rewrite.
func TestCompactFlag_PacksAndShrinksAFullRewrite(t *testing.T) {
	src := compactFixtureFile(t)

	plain := runDeleteTo(t, src)
	if bytes.Contains(plain, []byte("/Type /ObjStm")) {
		t.Fatal("precondition: the plain rewrite already packed objects")
	}

	packed := runDeleteTo(t, src, "--compact")
	if !bytes.Contains(packed, []byte("/Type /ObjStm")) {
		t.Error("--compact produced no object stream")
	}
	// Packing implies a cross-reference stream, since a traditional table
	// cannot name a compressed object.
	if !bytes.Contains(packed, []byte("/Type /XRef")) {
		t.Error("--compact did not switch to a cross-reference stream")
	}
	if len(packed) >= len(plain) {
		t.Errorf("--compact did not shrink the output: %d bytes vs %d", len(packed), len(plain))
	}
}

// TestCompactFlag_OutputStaysReadable asks a reader rather than the bytes,
// which is the only sound way to state that the file still works. A smaller
// file nothing can open is not a saving.
func TestCompactFlag_OutputStaysReadable(t *testing.T) {
	src := compactFixtureFile(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"delete", "-pages", "1", "--compact", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("delete --compact: exit %d, stderr: %s", rc, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if rc := run(context.Background(), []string{"info", out}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("the compacted output does not open: exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Pages:") {
		t.Errorf("info reported no page count for the compacted output: %s", stdout.String())
	}
}

// TestCompactFlag_AbsentFromIncrementalCommands pins the reason the flag is
// registered per command instead of on the shared helper.
//
// Packing and dedup only run on a full rewrite. rotate, set-metadata and
// autotag append an incremental update, so offering them the flag would
// advertise an option that silently does nothing.
func TestCompactFlag_AbsentFromIncrementalCommands(t *testing.T) {
	src := compactFixtureFile(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	for _, cmd := range [][]string{
		{"rotate", "-angle", "90", "-o", out, src},
		{"set-metadata", "-title", "x", "-o", out, src},
		{"autotag", "-o", out, src},
	} {
		args := append([]string{cmd[0], "--compact"}, cmd[1:]...)
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
			t.Errorf("%s accepted --compact, which cannot take effect on an incremental write", cmd[0])
		}
	}
}
