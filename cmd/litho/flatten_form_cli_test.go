package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// formFixturePDF builds a one-page document carrying an /AcroForm with two
// text fields, so flatten-form has something to remove and the forms command
// has something to report.
//
// The xref offsets are computed rather than transplanted from another
// fixture's hardcoded table: a stale offset makes the file unopenable, which
// would fail the test for a reason that has nothing to do with flattening.
func formFixturePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (John) /Rect [100 700 300 720] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (email) /V (john@example.com) /Rect [100 660 300 680] >>",
		"<< /Fields [4 0 R 5 0 R] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// listFormFields reads the form fields back through the forms command, the way
// listAttachments reads attachments and pageRotations reads rotation.
//
// Searching the output bytes for the field names would not work: litho writes
// an incremental update, so the superseded widget objects are still in the
// file after flattening. Only the command's view reports what a reader sees.
func listFormFields(t *testing.T, path string) []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
} {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"forms", "-f", "json", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("forms %s: exit %d, stderr: %s", path, rc, stderr.String())
	}
	var res struct {
		File   string `json:"file"`
		Fields []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("decoding forms JSON %q: %v", stdout.String(), err)
	}
	return res.Fields
}

// TestFlattenForm_RemovesTheFields pins what the command is for.
//
// flatten-form has no CLI test at all, so the only thing standing behind the
// wiring is this. The mutation itself is covered at pkg/pdf level; what is
// asserted here is that the command reaches it and writes the result.
//
// # Corpus evidence
//
//   - All corpora: the corpus gate scores neither flatten-form nor any other
//     page-mutating command.
func TestFlattenForm_RemovesTheFields(t *testing.T) {
	src := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "flat.pdf")

	// Precondition: the fixture really does carry fields. Without it, a
	// flatten that silently did nothing would pass, and so would a fixture
	// that never had an AcroForm in the first place.
	if before := listFormFields(t, src); len(before) != 2 {
		t.Fatalf("precondition: fixture reports %d form fields, want 2: %+v", len(before), before)
	}

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"flatten-form", "-o", out, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("flatten-form: exit %d, stderr: %s", rc, stderr.String())
	}

	if after := listFormFields(t, out); len(after) != 0 {
		t.Errorf("flatten-form left %d fields, want 0: %+v", len(after), after)
	}
}

// TestFlattenForm_MissingOutputIsAUsageError pins the shared
// runSimpleMutation guard. decrypt and autotag take the same path, so this
// covers the branch for all three.
func TestFlattenForm_MissingOutputIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "flat.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"flatten-form", src}, &stdout, &stderr); rc == exitOK {
		t.Fatalf("flatten-form without -o succeeded; stderr: %s", stderr.String())
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a usage error must not create an output file", out)
	}
}
