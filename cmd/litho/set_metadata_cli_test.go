package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readMetadata reads the Info dictionary back through the metadata command,
// the way listAttachments reads embedded files through attachments.
//
// The result type metadata declares is local to runMetadata, so unlike
// attachmentsResult there is no package-level shape to decode into and this
// test declares its own. Renaming a JSON field there will not break this test.
func readMetadata(t *testing.T, path string) map[string]string {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"metadata", "-f", "json", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("metadata %s: exit %d, stderr: %s", path, rc, stderr.String())
	}
	var res struct {
		File     string            `json:"file"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("decoding metadata JSON %q: %v", stdout.String(), err)
	}
	return res.Metadata
}

// setMetadataFails runs set-metadata and asserts it refused: non-zero exit,
// something on stderr, and no output file left behind.
//
// The output-file check is the part a usage assertion would miss. createOutput
// runs before WriteTo, so any failure after it has to reach abandonOutput or
// the caller keeps a truncated file that looks like a result.
func setMetadataFails(t *testing.T, out string, args []string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
		t.Fatalf("set-metadata %v succeeded; stderr: %s", args, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Errorf("set-metadata %v failed silently: nothing on stderr", args)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed set-metadata must not leave an output file", out)
	}
}

// TestSetMetadata_WritesTheFieldsItIsGiven pins the flag wiring: every -flag
// the command declares must reach the Info dictionary under the key the
// metadata command reports.
//
// The fixture carries no /Info at all, which is what makes the assertion
// sound. [semantics.Metadata] does not read only the live dictionary: it also
// runs scanRawInfoFields over the raw bytes to recover fields from SUPERSEDED
// Info objects, because litho writes incremental updates. A value that was
// never written cannot be recovered from anywhere, so a positive assertion on
// a document that started empty tests this write and nothing else.
func TestSetMetadata_WritesTheFieldsItIsGiven(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "meta.pdf")

	if before := readMetadata(t, src); len(before) != 0 {
		t.Fatalf("precondition: the fixture already carries metadata: %+v", before)
	}

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"set-metadata",
		"-title", "A Title",
		"-author", "An Author",
		"-subject", "A Subject",
		"-keywords", "one two",
		"-creator", "A Creator",
		"-producer", "A Producer",
		"-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("set-metadata: exit %d, stderr: %s", rc, stderr.String())
	}

	got := readMetadata(t, out)
	for key, want := range map[string]string{
		"Title":    "A Title",
		"Author":   "An Author",
		"Subject":  "A Subject",
		"Keywords": "one two",
		"Creator":  "A Creator",
		"Producer": "A Producer",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q: the -%s flag did not reach the Info dictionary",
				key, got[key], want, key)
		}
	}
}

// TestSetMetadata_WritesOnlyTheFlagsGiven pins that an omitted flag writes no
// key, which is the CLI half of the contract mutations.SetMetadata states:
// setInfoField skips an empty value, so an unset flag leaves the field alone.
//
// This asserts ABSENCE on a document that started with no metadata, not
// preservation of an existing value. Preservation cannot be tested through the
// metadata command at all: scanRawInfoFields refills a missing key from the
// bytes of superseded Info objects, so a field the writer wiped would be
// recovered from the incremental update and the assertion would pass whatever
// the writer did. That contract is pinned where it is observable, in
// mutations.TestSetMetadata_EmptyOverrides.
func TestSetMetadata_WritesOnlyTheFlagsGiven(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "meta.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"set-metadata", "-title", "Only A Title", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("set-metadata: exit %d, stderr: %s", rc, stderr.String())
	}

	got := readMetadata(t, out)
	if got["Title"] != "Only A Title" {
		t.Fatalf("Title = %q, want %q", got["Title"], "Only A Title")
	}
	for _, key := range []string{"Author", "Subject", "Keywords", "Creator", "Producer"} {
		if v, ok := got[key]; ok && v != "" {
			t.Errorf("%s = %q, want absent: an unset flag must not write an empty field", key, v)
		}
	}
}

// TestSetMetadata_MissingOutputIsAUsageError pins the required-flag check: -o
// and the input file are both required.
func TestSetMetadata_MissingOutputIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "meta.pdf")

	setMetadataFails(t, out, []string{"set-metadata", "-title", "X", src})
}

// TestSetMetadata_MissingInputIsAUsageError covers the other half: -o alone,
// with no file to read.
func TestSetMetadata_MissingInputIsAUsageError(t *testing.T) {
	out := filepath.Join(t.TempDir(), "meta.pdf")

	setMetadataFails(t, out, []string{"set-metadata", "-title", "X", "-o", out})
}

// TestSetMetadata_RejectsAnUnreadableInput covers the open path: the named
// file does not exist, so openPDF fails before any write.
func TestSetMetadata_RejectsAnUnreadableInput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "meta.pdf")

	setMetadataFails(t, out, []string{
		"set-metadata", "-title", "X", "-o", out, filepath.Join(dir, "absent.pdf"),
	})
}

// TestSetMetadata_RejectsAnInputThatIsNotAPDF is the second half of that path.
// The file exists and reads cleanly, so it reaches the parser rather than the
// open: a handler that only checked for existence would pass the test above
// and fail this one.
func TestSetMetadata_RejectsAnInputThatIsNotAPDF(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "meta.pdf")

	notAPDF := filepath.Join(dir, "notapdf.pdf")
	if err := os.WriteFile(notAPDF, []byte("this is not a PDF"), 0o600); err != nil {
		t.Fatalf("writing the decoy input: %v", err)
	}

	setMetadataFails(t, out, []string{"set-metadata", "-title", "X", "-o", out, notAPDF})
}
