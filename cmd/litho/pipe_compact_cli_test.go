package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestPipe_CompactPacksTheFinalWrite covers the gap that made pipe the odd one
// out: every standalone command that rewrites the whole file takes --compact,
// while a pipeline containing the same stage could not ask for it.
//
// The fixture is compactFixturePDF, not untaggedTextPDF. The writer declines to
// pack a batch it cannot shrink, and untaggedTextPDF is five short objects, so
// a pipeline built on it would pack nothing and the assertion would hold while
// proving nothing.
//
// delete is the stage under test because it forces a full rewrite. Compaction
// runs only on that path, which is why the flag is documented as applying to
// the final write rather than to every pipeline.
func TestPipe_CompactPacksTheFinalWrite(t *testing.T) {
	src := writeTempPDF(t, compactFixturePDF())

	run := func(extra ...string) []byte {
		t.Helper()
		out := filepath.Join(t.TempDir(), "piped.pdf")
		args := append([]string{"pipe", src, "delete", "-pages", "1"}, extra...)
		args = append(args, "-o", out)
		rc, errOut := runPipeArgs(t, args)
		if rc != exitOK {
			t.Fatalf("pipe %v: exit %d, stderr: %s", extra, rc, errOut)
		}
		data, err := os.ReadFile(out) //nolint:gosec // the test's own temp path
		if err != nil {
			t.Fatalf("reading output: %v", err)
		}
		return data
	}

	plain := run()
	if bytes.Contains(plain, []byte("/Type /ObjStm")) {
		t.Fatal("precondition: the pipeline already packed without the flag")
	}

	packed := run("--compact")
	if !bytes.Contains(packed, []byte("/Type /ObjStm")) {
		t.Error("pipe --compact produced no object stream, so the flag is inert")
	}
	if !bytes.Contains(packed, []byte("/Type /XRef")) {
		t.Error("pipe --compact did not switch to a cross-reference stream")
	}
	if len(packed) >= len(plain) {
		t.Errorf("pipe --compact did not shrink the output: %d vs %d", len(packed), len(plain))
	}
}

// TestPipe_CompactDoesNotSwallowTheNextArgument is the hazard the new global
// sits next to.
//
// parsePipeGlobals marks a token consumed when the flag before it takes a
// value, and expectsValue returns true for any unrecognised dash-token. A
// boolean global that wrongly set consumed would eat the following -o, leaving
// the command with no output path: the same shape as the recorded incident
// where a stage flag's value was read as the global -o and the command wrote a
// file literally named "-o".
//
// Both spellings are checked because -o and -p are matched literally.
func TestPipe_CompactDoesNotSwallowTheNextArgument(t *testing.T) {
	for _, spelling := range []string{"--compact", "-compact"} {
		src := writeTempPDF(t, compactFixturePDF())
		out := filepath.Join(t.TempDir(), "piped.pdf")

		rc, errOut := runPipeArgs(t, []string{
			"pipe", src, "delete", "-pages", "1", spelling, "-o", out,
		})
		if rc != exitOK {
			t.Fatalf("%s: exit %d, stderr: %s", spelling, rc, errOut)
		}
		if _, err := os.Stat(out); err != nil {
			t.Errorf("%s swallowed the -o that followed it: %v", spelling, err)
		}
		// A stray file named after the flag is the other half of that bug.
		if _, err := os.Stat("-o"); err == nil {
			os.Remove("-o") //nolint:errcheck // best-effort cleanup of a file this failure created
			t.Errorf("%s left a file named \"-o\" in the working directory", spelling)
		}
	}
}

// TestPipe_CompactSurvivesAnIncrementalStage checks what actually holds when
// the pipeline does not force a full rewrite.
//
// An earlier version of this test was named for inertness and asserted only
// that the file was non-empty, which proves nothing of the kind. Its premise
// was shaky too: WriteTo forces a full rewrite when the source was rebuilt,
// has a non-zero header offset, or is encrypted, and a hand-built fixture is a
// rebuild candidate, so rotate may well take the full-rewrite path here.
//
// So the test distinguishes the paths instead of assuming one. Only an
// incremental update leaves the source bytes as a prefix, and only on that
// path is packing impossible: the appended section cannot describe objects
// held in a stream it does not contain.
func TestPipe_CompactSurvivesAnIncrementalStage(t *testing.T) {
	fixture := compactFixturePDF()
	src := writeTempPDF(t, fixture)
	out := filepath.Join(t.TempDir(), "piped.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "rotate", "-angle", "90", "--compact", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe rotate --compact: exit %d, stderr: %s", rc, errOut)
	}
	data, err := os.ReadFile(out) //nolint:gosec // the test's own temp path
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("rotate with --compact wrote an empty file")
	}

	incremental := bytes.HasPrefix(data, fixture)
	packed := bytes.Contains(data, []byte("/Type /ObjStm"))
	t.Logf("write path: incremental=%v packed=%v", incremental, packed)

	if incremental && packed {
		t.Error("an appended section cannot describe objects packed into a stream it does not contain")
	}
}
