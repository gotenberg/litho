package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pageOpFails runs a page-mutating command and asserts it refused: non-zero
// exit, something on stderr, and no output file left behind.
//
// Every refusal in runDelete and runInsert reports before createOutput runs, so
// an output file appearing here means one of them moved after it.
func pageOpFails(t *testing.T, out string, args []string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
		t.Fatalf("%v succeeded; stderr: %s", args, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Errorf("%v failed silently: nothing on stderr", args)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed command must not leave an output file", out)
	}
	return stderr.String()
}

// TestDelete_RemovesTheNamedPage pins what the command is for.
//
// The only other CLI coverage of delete drives it to test the encryption
// warning, asserting nothing about deletion. DeletePages itself is covered at
// package level; what had nothing behind it is that the command reaches it
// with the right page.
//
// The count is read back through the pages command rather than from the output
// bytes: litho writes an incremental update, so the superseded page objects are
// still in the file.
//
// # Corpus evidence
//
//   - All corpora: the corpus gate never scores delete.
func TestDelete_RemovesTheNamedPage(t *testing.T) {
	src := twoPageTempPDF(t)
	out := filepath.Join(t.TempDir(), "deleted.pdf")

	if before := pageCountOf(t, src); before != 2 {
		t.Fatalf("precondition: fixture has %d pages, want 2", before)
	}

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"delete", "-pages", "1", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("delete: exit %d, stderr: %s", rc, stderr.String())
	}

	if after := pageCountOf(t, out); after != 1 {
		t.Errorf("output has %d pages, want 1: delete did not remove the page", after)
	}
}

// TestDelete_RefusesToEmptyTheDocument pins a refusal that belongs to the
// mutations layer and has to survive the trip through the CLI.
//
// "cannot delete all N pages" comes from DeletePages, not from the command, so
// this asserts the error reaches the user rather than being swallowed into a
// bare non-zero exit.
func TestDelete_RefusesToEmptyTheDocument(t *testing.T) {
	src := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "deleted.pdf")

	errOut := pageOpFails(t, out, []string{"delete", "-pages", "1", "-o", out, src})
	if !strings.Contains(errOut, "cannot delete all") {
		t.Errorf("stderr = %q, want it to explain that the document cannot be emptied",
			strings.TrimSpace(errOut))
	}
}

// TestDelete_RejectsAnUnparseablePageRange covers the pdf.ParsePageRanges arm, which is
// a different failure from the usage guard: -pages is present, so the command
// gets as far as parsing it.
func TestDelete_RejectsAnUnparseablePageRange(t *testing.T) {
	src := twoPageTempPDF(t)
	out := filepath.Join(t.TempDir(), "deleted.pdf")

	pageOpFails(t, out, []string{"delete", "-pages", "not-a-range", "-o", out, src})
}

// TestDelete_MissingPagesIsAUsageError pins that --pages is required: without
// it the command would rewrite the file changing nothing.
func TestDelete_MissingPagesIsAUsageError(t *testing.T) {
	src := twoPageTempPDF(t)
	out := filepath.Join(t.TempDir(), "deleted.pdf")

	pageOpFails(t, out, []string{"delete", "-o", out, src})
}

// TestInsert_AddsTheSourcePages pins the success path: a one-page source
// spliced into a one-page target yields two pages.
func TestInsert_AddsTheSourcePages(t *testing.T) {
	target := onePageTempPDF(t)
	source := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "inserted.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"insert", "-source", source, "-pos", "0", "-o", out, target,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("insert: exit %d, stderr: %s", rc, stderr.String())
	}

	if after := pageCountOf(t, out); after != 2 {
		t.Errorf("output has %d pages, want 2: the source pages were not inserted", after)
	}
}

// TestInsert_RejectsAnUnreadableSource covers the SECOND openPDF call, which
// reports its own message.
//
// A handler that opened only the target would pass every other test in this
// file and fail this one.
func TestInsert_RejectsAnUnreadableSource(t *testing.T) {
	target := onePageTempPDF(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "inserted.pdf")

	errOut := pageOpFails(t, out, []string{
		"insert", "-source", filepath.Join(dir, "absent.pdf"), "-pos", "0", "-o", out, target,
	})
	if !strings.Contains(errOut, "source") {
		t.Errorf("stderr = %q, want it to name the source file that failed to open",
			strings.TrimSpace(errOut))
	}
}

// TestInsert_MissingSourceIsAUsageError pins that --source is required.
func TestInsert_MissingSourceIsAUsageError(t *testing.T) {
	target := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "inserted.pdf")

	pageOpFails(t, out, []string{"insert", "-pos", "0", "-o", out, target})
}

// TestInsert_MissingPosIsAUsageError pins that --pos is required. It defaults
// to -1 rather than 0 precisely so an omitted flag is distinguishable from an
// insertion at the front.
func TestInsert_MissingPosIsAUsageError(t *testing.T) {
	target := onePageTempPDF(t)
	source := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "inserted.pdf")

	pageOpFails(t, out, []string{"insert", "-source", source, "-o", out, target})
}
