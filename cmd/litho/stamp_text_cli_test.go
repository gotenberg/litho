package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// stampTextFails runs stamp-text with the given arguments and asserts the
// command refused: a non-zero exit, something said on stderr, and no output
// file left behind.
//
// The last of those is the one a usage check alone would miss. createOutput
// runs before WriteTo, so a failure after it has to reach abandonOutput or the
// caller is left with a truncated file that looks like a result.
func stampTextFails(t *testing.T, out string, args []string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
		t.Fatalf("stamp-text %v succeeded; stderr: %s", args, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Errorf("stamp-text %v failed silently: nothing on stderr", args)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed stamp-text must not leave an output file", out)
	}
}

// TestStampText_RejectsAMalformedColor covers the -color parse path.
//
// parseHexColor takes exactly six hex digits after an optional #. Both ways of
// getting that wrong reach different branches: the wrong LENGTH fails before
// any digit is parsed, and the right length with a non-hex digit fails inside
// strconv.ParseUint. Neither had a test.
func TestStampText_RejectsAMalformedColor(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())

	for _, color := range []string{"#abc", "#gggggg"} {
		out := filepath.Join(t.TempDir(), "stamped.pdf")
		stampTextFails(t, out, []string{
			"stamp-text", "-text", "X", "-color", color, "-o", out, src,
		})
	}
}

// TestStampText_RejectsAnUnreadableFontFile covers the -font read path: the
// file named does not exist, so os.ReadFile fails before any PDF work starts.
func TestStampText_RejectsAnUnreadableFontFile(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	out := filepath.Join(dir, "stamped.pdf")

	stampTextFails(t, out, []string{
		"stamp-text", "-text", "X", "-font", filepath.Join(dir, "absent.ttf"), "-o", out, src,
	})
}

// TestStampText_RejectsAFontFileThatIsNotATTF is the second half of the -font
// path, and the reason the test above is not enough: a readable file still has
// to parse. This one reaches pdf.NewTTFFont, which the missing-file case never
// does, so a handler that read the bytes and skipped the parse would pass that
// test and fail this one.
func TestStampText_RejectsAFontFileThatIsNotATTF(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	out := filepath.Join(dir, "stamped.pdf")

	notAFont := filepath.Join(dir, "notafont.ttf")
	if err := os.WriteFile(notAFont, []byte("this is not a font"), 0o600); err != nil {
		t.Fatalf("writing the decoy font: %v", err)
	}

	stampTextFails(t, out, []string{
		"stamp-text", "-text", "X", "-font", notAFont, "-o", out, src,
	})
}

// TestStampText_MissingTextIsAUsageError pins the required-flag check. -text,
// -o and the input file are all required, and omitting one must not reach the
// writer.
func TestStampText_MissingTextIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	stampTextFails(t, out, []string{"stamp-text", "-o", out, src})
}

// TestStampText_RejectsAnOutOfRangePageRange covers -pages, which is parsed
// against the document's real page count rather than accepted blindly.
func TestStampText_RejectsAnOutOfRangePageRange(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	stampTextFails(t, out, []string{
		"stamp-text", "-text", "X", "-pages", "99", "-o", out, src,
	})
}
