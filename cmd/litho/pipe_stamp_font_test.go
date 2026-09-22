package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shared stamp path tells a caller whose text WinAnsi cannot express to
// "supply a TTF with -font". That advice reached the pipe stage, which did not
// define -font, so a pipeline was told to pass a flag the same command then
// rejected with "flag provided but not defined": a closed loop with no way out.
//
// The flag is also the only way to stamp and keep PDF/A conformance, since the
// default standard font is not embedded (ISO 19005-2, 6.2.11.4.1).
//
// These mirror the standalone command's own -font tests, which use negative
// fixtures because this package deliberately carries no TTF: a missing file and
// a file that is not a font. The valid-font case lives in pkg/pdf, which has a
// synthetic TTF builder this package cannot reach.

// unencodableStampText returns Cyrillic "СУВОРОВ" built from explicit bytes.
// Spelling it as an escape risks an intervening layer rewriting it, which has
// silently neutered a byte assertion here before.
func unencodableStampText() string {
	return string([]byte{0xD0, 0xA1, 0xD0, 0xA3, 0xD0, 0x92, 0xD0, 0x9E, 0xD0, 0xA0, 0xD0, 0x9E, 0xD0, 0x92})
}

const flagNotDefined = "flag provided but not defined"

// TestPipeStampText_AcceptsTheFontFlag is the defect itself. The assertion that
// carries it is the absence of "flag provided but not defined": the stage must
// reach the font-reading path rather than reject the flag outright.
func TestPipeStampText_AcceptsTheFontFlag(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	absent := filepath.Join(t.TempDir(), "absent.ttf")

	rc, _, stderr := runPipeCLI(t,
		"pipe", "-o", out, src, "stamp-text", "-text", "X", "-font", absent)

	if strings.Contains(stderr, flagNotDefined) {
		t.Errorf("the stage rejected -font, the very flag its own error advises; stderr: %s", stderr)
	}
	if rc == exitOK {
		t.Error("a missing font file was accepted silently")
	}
	if !strings.Contains(stderr, "reading font file") {
		t.Errorf("no explanation that the font file could not be read; stderr: %s", stderr)
	}
}

// TestPipeStampText_RejectsAFontFileThatIsNotATTF reaches pdf.NewTTFFont, which
// the missing-file case never does, mirroring the standalone pair.
func TestPipeStampText_RejectsAFontFileThatIsNotATTF(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	dir := t.TempDir()
	out := filepath.Join(dir, "out.pdf")
	notAFont := filepath.Join(dir, "notafont.ttf")
	if err := os.WriteFile(notAFont, []byte("this is not a font"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	rc, _, stderr := runPipeCLI(t,
		"pipe", "-o", out, src, "stamp-text", "-text", "X", "-font", notAFont)

	if strings.Contains(stderr, flagNotDefined) {
		t.Errorf("the stage rejected -font instead of parsing the file; stderr: %s", stderr)
	}
	if rc == exitOK {
		t.Error("a file that is not a TTF was accepted")
	}
	if !strings.Contains(stderr, "loading font") {
		t.Errorf("the error did not come from the font parser; stderr: %s", stderr)
	}
}

// TestPipeStampText_AdviceIsNowActionable pins the reason the flag exists here.
// The shared path still refuses text WinAnsi cannot express and still names
// -font, and that flag is now something this surface accepts.
func TestPipeStampText_AdviceIsNowActionable(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, _, stderr := runPipeCLI(t,
		"pipe", "-o", out, src, "stamp-text", "-text", unencodableStampText())

	if rc == exitOK {
		t.Fatal("text the standard font cannot express was stamped anyway; the guard is gone")
	}
	if !strings.Contains(stderr, "-font") {
		t.Errorf("the refusal no longer names the remedy; stderr: %s", stderr)
	}
}

// TestPipeStampText_PlainTextStillWorks is the control. The stage must not have
// become harder to use for text that never needed a font.
func TestPipeStampText_PlainTextStillWorks(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, _, stderr := runPipeCLI(t, "pipe", "-o", out, src, "stamp-text", "-text", "DRAFT")
	if rc != exitOK {
		t.Fatalf("plain ASCII stamp: exit %d, want %d; stderr: %s", rc, exitOK, stderr)
	}
	data, err := os.ReadFile(out) //nolint:gosec // path is this test's own temp file
	if err != nil || len(data) == 0 {
		t.Errorf("no output written: %v", err)
	}
}

// TestStampTextFontFlag_ParityAcrossSurfaces states the contract the gap broke:
// a flag one entry point advises must exist on the entry point that advises it.
func TestStampTextFontFlag_ParityAcrossSurfaces(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	absent := filepath.Join(t.TempDir(), "absent.ttf")

	standaloneOut := filepath.Join(t.TempDir(), "s.pdf")
	_, _, standaloneErr := runPipeCLI(t,
		"stamp-text", "-text", "X", "-font", absent, "-o", standaloneOut, src)

	pipedOut := filepath.Join(t.TempDir(), "p.pdf")
	_, _, pipedErr := runPipeCLI(t,
		"pipe", "-o", pipedOut, src, "stamp-text", "-text", "X", "-font", absent)

	for name, out := range map[string]string{"standalone": standaloneErr, "pipe": pipedErr} {
		if strings.Contains(out, flagNotDefined) {
			t.Errorf("%s does not define -font; stderr: %s", name, out)
		}
	}
}
