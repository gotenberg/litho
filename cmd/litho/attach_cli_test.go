package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// listAttachments reads the embedded files back through the attachments
// command, the way pageRotations reads rotation back through pages.
//
// Scanning the output bytes for the file name would not do: litho writes an
// incremental update, so a name removed by detach is still present in the
// superseded object. Only the command's own view reports what a reader sees.
// It decodes into the command's result types, so renaming a JSON field breaks
// this test rather than silently changing the shape callers parse.
func listAttachments(t *testing.T, path string) []attachmentInfo {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"attachments", "-f", "json", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("attachments %s: exit %d, stderr: %s", path, rc, stderr.String())
	}
	var res attachmentsResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("decoding attachments JSON %q: %v", stdout.String(), err)
	}
	return res.Files
}

// dataFile writes a file for attach to embed and returns its path.
func dataFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// TestAttach_EmbedsTheFileAndDetectsTheMIMEType pins the auto-detection the
// -mime flag documents as its default.
//
// mimeFromExt maps the extension when -mime is absent. Nothing exercised that
// from the command before: the mutation layer is well covered, but the wiring
// that reaches it, flag parsing and the detection fallback, was not.
func TestAttach_EmbedsTheFileAndDetectsTheMIMEType(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	data := dataFile(t, "table.csv", "a,b\n1,2\n")
	out := filepath.Join(t.TempDir(), "attached.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"attach", "-name", "table.csv", "-data", data, "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}

	files := listAttachments(t, out)
	if len(files) != 1 {
		t.Fatalf("attachments reported %d files, want 1: %+v", len(files), files)
	}
	if files[0].Name != "table.csv" {
		t.Errorf("name = %q, want %q", files[0].Name, "table.csv")
	}
	if files[0].MIMEType != "text/csv" {
		t.Errorf("mimeType = %q, want %q: -mime was absent so mimeFromExt should have supplied it",
			files[0].MIMEType, "text/csv")
	}
}

// TestAttach_ExplicitMIMETypeOverridesDetection is the control for the test
// above. Without it, that one would also pass if the flag were ignored and
// detection always ran.
func TestAttach_ExplicitMIMETypeOverridesDetection(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	data := dataFile(t, "table.csv", "a,b\n1,2\n")
	out := filepath.Join(t.TempDir(), "attached.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"attach", "-name", "table.csv", "-data", data,
		"-mime", "application/octet-stream", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}

	files := listAttachments(t, out)
	if len(files) != 1 {
		t.Fatalf("attachments reported %d files, want 1: %+v", len(files), files)
	}
	if files[0].MIMEType != "application/octet-stream" {
		t.Errorf("mimeType = %q, want the explicit flag value: detection overrode it", files[0].MIMEType)
	}
}

// TestAttach_RelationshipStillEmbedsTheFile pins the branch nothing exercised.
//
// runAttach chooses between two different writer calls on the -relationship
// flag: AddAssociatedFile when set, AddAttachment when absent. The flag appears
// in no test anywhere in the repository, and the two calls reach different
// mutation entry points, so a wiring mistake would send an attachment down the
// wrong path silently.
//
// The /AFRelationship VALUE is not observable from the CLI: attachmentInfo
// carries only Name and MIMEType. What this pins is that selecting the
// associated-file path still embeds a file the reader finds, which
// TestExtractEmbeddedFilesFindsAssociatedFile establishes it should.
func TestAttach_RelationshipStillEmbedsTheFile(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	data := dataFile(t, "invoice.xml", "<invoice/>")
	out := filepath.Join(t.TempDir(), "attached.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"attach", "-name", "invoice.xml", "-data", data,
		"-relationship", "Data", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}

	files := listAttachments(t, out)
	if len(files) != 1 {
		t.Fatalf("the associated-file path embedded %d files, want 1: %+v", len(files), files)
	}
	if files[0].Name != "invoice.xml" {
		t.Errorf("name = %q, want %q", files[0].Name, "invoice.xml")
	}
}

// TestAttach_MissingDataFileFailsAndWritesNothing pins the error path and the
// promise that a failed mutation leaves no output behind.
func TestAttach_MissingDataFileFailsAndWritesNothing(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "attached.pdf")
	missing := filepath.Join(t.TempDir(), "absent.csv")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"attach", "-name", "absent.csv", "-data", missing, "-o", out, src,
	}, &stdout, &stderr); rc == exitOK {
		t.Fatalf("attaching an unreadable file succeeded; stderr: %s", stderr.String())
	}
	if stderr.Len() == 0 {
		t.Error("the failure was silent: nothing was written to stderr")
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed attach must not leave an output file", out)
	}
}

// TestAttach_MissingNameIsAUsageError pins the required-flag check. -name, -data
// and -o are all required, and a missing one must not reach the writer.
func TestAttach_MissingNameIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	data := dataFile(t, "table.csv", "a,b\n")
	out := filepath.Join(t.TempDir(), "attached.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"attach", "-data", data, "-o", out, src,
	}, &stdout, &stderr); rc == exitOK {
		t.Fatalf("attach without -name succeeded; stderr: %s", stderr.String())
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a usage error must not create an output file", out)
	}
}

// TestDetach_RemovesTheAttachment closes the round trip: the paired command has
// no CLI test either, and the corpus gate scores neither.
func TestDetach_RemovesTheAttachment(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	data := dataFile(t, "table.csv", "a,b\n1,2\n")
	dir := t.TempDir()
	attached := filepath.Join(dir, "attached.pdf")
	detached := filepath.Join(dir, "detached.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"attach", "-name", "table.csv", "-data", data, "-o", attached, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("attach: exit %d, stderr: %s", rc, stderr.String())
	}
	if got := len(listAttachments(t, attached)); got != 1 {
		t.Fatalf("precondition: %d attachments after attach, want 1", got)
	}

	stdout.Reset()
	stderr.Reset()
	if rc := run(context.Background(), []string{
		"detach", "-name", "table.csv", "-o", detached, attached,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("detach: exit %d, stderr: %s", rc, stderr.String())
	}
	if files := listAttachments(t, detached); len(files) != 0 {
		t.Errorf("detach left %d attachments, want 0: %+v", len(files), files)
	}
}
