package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// listBookmarks reads the outline back through the bookmarks command.
//
// It decodes into bookmarksResult, the command's own type, so renaming a JSON
// field breaks this test rather than silently changing the shape callers parse.
func listBookmarks(t *testing.T, path string) []models.Bookmark {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"bookmarks", "-f", "json", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("bookmarks %s: exit %d, stderr: %s", path, rc, stderr.String())
	}
	var res bookmarksResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("decoding bookmarks JSON %q: %v", stdout.String(), err)
	}
	return res.Bookmarks
}

// bookmarkJSON writes a --from-json payload and returns its path.
func bookmarkJSON(t *testing.T, bms []models.Bookmark) string {
	t.Helper()

	data, err := json.Marshal(bms)
	if err != nil {
		t.Fatalf("marshalling bookmarks: %v", err)
	}
	path := filepath.Join(t.TempDir(), "bookmarks.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing bookmark JSON: %v", err)
	}
	return path
}

// setBookmarksFails runs set-bookmarks and asserts it refused: non-zero exit,
// something on stderr, and no output file left behind.
func setBookmarksFails(t *testing.T, out string, args []string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
		t.Fatalf("set-bookmarks %v succeeded; stderr: %s", args, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Errorf("set-bookmarks %v failed silently: nothing on stderr", args)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed set-bookmarks must not leave an output file", out)
	}
}

// TestSetBookmarks_WritesTheOutlineFromJSON pins the --from-json wiring end to
// end: the file is read, unmarshalled into []models.Bookmark, and written as an
// outline the bookmarks command can read back.
//
// The fixture has no outline at all, so every bookmark read back came from this
// write and nothing else.
func TestSetBookmarks_WritesTheOutlineFromJSON(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "bookmarked.pdf")

	if before := listBookmarks(t, src); len(before) != 0 {
		t.Fatalf("precondition: fixture already has %d bookmark(s): %+v", len(before), before)
	}

	in := bookmarkJSON(t, []models.Bookmark{
		{Title: "First", PageNumber: 1},
		{Title: "Second", PageNumber: 1},
	})

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"set-bookmarks", "-from-json", in, "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("set-bookmarks: exit %d, stderr: %s", rc, stderr.String())
	}

	got := listBookmarks(t, out)
	if len(got) != 2 {
		t.Fatalf("outline has %d bookmark(s), want 2: %+v", len(got), got)
	}
	if got[0].Title != "First" || got[1].Title != "Second" {
		t.Errorf("titles = %q/%q, want First/Second", got[0].Title, got[1].Title)
	}
}

// TestSetBookmarks_GenerateWritesAReadableDocument covers the --generate arm,
// the one that does not read a file.
//
// [TestSetBookmarks_Generate_MinimalPDF] in enhancements_test.go already drives
// this arm, so this is deliberately the narrow increment over it rather than a
// second copy: that one calls runSetBookmarks directly and asserts the output
// file is non-empty, while this one goes through run, which is the only thing
// that covers the dispatcher entry, and reads the outline back through the
// bookmarks command instead of trusting a byte count. A non-empty file is not
// evidence that the outline survived.
//
// It deliberately does NOT assert how many bookmarks heading detection finds in
// this fixture: that number is a property of the detector, measured nowhere in
// this package, and pinning an unmeasured value here would make this test fail
// whenever the detector legitimately improves.
func TestSetBookmarks_GenerateWritesAReadableDocument(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "generated.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"set-bookmarks", "-generate", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("set-bookmarks -generate: exit %d, stderr: %s", rc, stderr.String())
	}

	if _, err := os.Stat(out); err != nil {
		t.Fatalf("no output file: %v", err)
	}
	listBookmarks(t, out)
}

// TestSetBookmarks_RejectsAnUnreadableJSONFile covers the os.ReadFile path,
// which reports before the document is written.
func TestSetBookmarks_RejectsAnUnreadableJSONFile(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	out := filepath.Join(dir, "bookmarked.pdf")

	setBookmarksFails(t, out, []string{
		"set-bookmarks", "-from-json", filepath.Join(dir, "absent.json"), "-o", out, src,
	})
}

// TestSetBookmarks_RejectsMalformedJSON is the second half of that path: the
// file reads cleanly but does not unmarshal, so it reaches json.Unmarshal
// rather than the read. A handler that only checked for existence would pass
// the test above and fail this one.
func TestSetBookmarks_RejectsMalformedJSON(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	out := filepath.Join(dir, "bookmarked.pdf")

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing the malformed payload: %v", err)
	}

	setBookmarksFails(t, out, []string{"set-bookmarks", "-from-json", bad, "-o", out, src})
}

// TestSetBookmarks_MissingSourceIsAUsageError pins that one of --from-json or
// --generate is required: with neither, the command would rewrite the file
// changing nothing.
func TestSetBookmarks_MissingSourceIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "bookmarked.pdf")

	setBookmarksFails(t, out, []string{"set-bookmarks", "-o", out, src})
}

// TestSetBookmarks_MissingOutputIsAUsageError pins the -o requirement.
func TestSetBookmarks_MissingOutputIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "bookmarked.pdf")

	setBookmarksFails(t, out, []string{"set-bookmarks", "-generate", src})
}
