package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// The five output-producing commands that pipe could not run.
//
// pipe's own doc comment says "Stages reuse the same names and flags as
// individual mutation commands", and for attach, detach, insert, set-bookmarks
// and stamp-pdf that was false: each is an ordinary mutation command with an
// -o, and each answered `unknown stage`. sign and redact stay out on purpose;
// [stageCmds] carries the reason.
//
// Each stage is asserted through a SIBLING SUBCOMMAND rather than by scanning
// the output bytes, the convention the other pipe stage tests set: litho writes
// an incremental update, so a superseded object still carries the old value and
// a byte scan answers neither direction.

// TestPipe_AttachStageEmbedsTheFile also pins the MIME fallback, since the
// stage resolves it the same way the standalone command does.
func TestPipe_AttachStageEmbedsTheFile(t *testing.T) {
	src := onePageTempPDF(t)
	data := dataFile(t, "table.csv", "a,b\n1,2\n")
	out := filepath.Join(t.TempDir(), "attached.pdf")

	if before := listAttachments(t, src); len(before) != 0 {
		t.Fatalf("fixture already carries %d attachment(s), so the assertion below proves nothing", len(before))
	}

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "attach", "-name", "table.csv", "-data", data, "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe attach: exit %d, stderr: %s", rc, errOut)
	}

	files := listAttachments(t, out)
	if len(files) != 1 {
		t.Fatalf("attachments reported %d file(s), want 1: the attach stage did not run", len(files))
	}
	if files[0].Name != "table.csv" {
		t.Errorf("name = %q, want %q", files[0].Name, "table.csv")
	}
	if files[0].MIMEType != "text/csv" {
		t.Errorf("mimeType = %q, want %q: -mime was absent so mimeFromExt should have supplied it",
			files[0].MIMEType, "text/csv")
	}
}

// TestPipe_AttachThenDetachInOnePipeline is the round trip, and it is worth
// more than the two stages separately: it proves the second stage observes what
// the first one did, which is the whole claim a pipeline makes.
func TestPipe_AttachThenDetachInOnePipeline(t *testing.T) {
	src := onePageTempPDF(t)
	data := dataFile(t, "table.csv", "a,b\n1,2\n")
	out := filepath.Join(t.TempDir(), "roundtrip.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src,
		"attach", "-name", "table.csv", "-data", data,
		"detach", "-name", "table.csv",
		"-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe attach detach: exit %d, stderr: %s", rc, errOut)
	}

	if files := listAttachments(t, out); len(files) != 0 {
		t.Errorf("the pipeline left %d attachment(s), want 0: the detach stage did not see the attach stage's work: %+v",
			len(files), files)
	}
}

// TestPipe_InsertStageAddsThePages covers the second document a stage opens for
// itself, the path only merge exercised before.
//
// The reader has to stay live until WriteTo executes the stage, so a stage that
// closed it in addStage would read a closed document here rather than failing
// at build time.
func TestPipe_InsertStageAddsThePages(t *testing.T) {
	target := onePageTempPDF(t)
	source := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "inserted.pdf")

	if before := pageCountOf(t, target); before != 1 {
		t.Fatalf("fixture has %d page(s), want 1", before)
	}

	rc, errOut := runPipeArgs(t, []string{
		"pipe", target, "insert", "-source", source, "-pos", "0", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe insert: exit %d, stderr: %s", rc, errOut)
	}

	if after := pageCountOf(t, out); after != 2 {
		t.Errorf("output has %d page(s), want 2: the insert stage did not run", after)
	}
}

// TestPipe_SetBookmarksStageSetsThem reads the outline back through the
// bookmarks command.
func TestPipe_SetBookmarksStageSetsThem(t *testing.T) {
	src := onePageTempPDF(t)
	want := []models.Bookmark{{Title: "Introduction", PageNumber: 1}}
	jsonPath := bookmarkJSON(t, want)
	out := filepath.Join(t.TempDir(), "bookmarked.pdf")

	if before := listBookmarks(t, src); len(before) != 0 {
		t.Fatalf("fixture already carries %d bookmark(s), so the assertion below proves nothing", len(before))
	}

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "set-bookmarks", "-from-json", jsonPath, "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe set-bookmarks: exit %d, stderr: %s", rc, errOut)
	}

	if got := listBookmarks(t, out); !reflect.DeepEqual(got, want) {
		t.Errorf("bookmarks = %+v, want %+v: the set-bookmarks stage did not run", got, want)
	}
}

// TestPipe_GenerateDoesNotSwallowTheFollowingStage is a regression test for the
// splitter, not for set-bookmarks.
//
// -generate is the second boolean any stage defines. A boolean consumes no
// token, so the token after it is a genuine stage name; a value-expecting flag
// consumes it. Had -generate been left out of [boolStageFlags], the splitter
// would have read `attach` as its value, produced ONE group instead of two, and
// exited 0 having attached nothing. The failure is silent, which is why it is
// pinned end to end rather than trusted to the table test below.
func TestPipe_GenerateDoesNotSwallowTheFollowingStage(t *testing.T) {
	src := onePageTempPDF(t)
	data := dataFile(t, "table.csv", "a,b\n1,2\n")
	out := filepath.Join(t.TempDir(), "generated.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src,
		"set-bookmarks", "-generate",
		"attach", "-name", "table.csv", "-data", data,
		"-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe set-bookmarks -generate attach: exit %d, stderr: %s", rc, errOut)
	}

	if files := listAttachments(t, out); len(files) != 1 {
		t.Errorf("attachments reported %d file(s), want 1: -generate swallowed the attach stage, "+
			"so the pipeline ran one stage and reported success", len(files))
	}
}

// TestSplitStageGroups_BooleanFlagsDoNotConsumeAStage states the rule directly,
// so a future boolean stage flag that is not registered fails here with a
// readable diff rather than only as a silent behaviour change downstream.
func TestSplitStageGroups_BooleanFlagsDoNotConsumeAStage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc string
		args []string
		want [][]string
	}{
		{
			desc: "a boolean does not consume the next stage",
			args: []string{"set-bookmarks", "-generate", "rotate", "-angle", "90"},
			want: [][]string{{"set-bookmarks", "-generate"}, {"rotate", "-angle", "90"}},
		},
		{
			desc: "the other boolean behaves the same",
			args: []string{"remediate", "-profile", "PDF/A-2b", "-aggressive", "rotate"},
			want: [][]string{{"remediate", "-profile", "PDF/A-2b", "-aggressive"}, {"rotate"}},
		},
		{
			desc: "a value-expecting flag still consumes a stage-looking token",
			args: []string{"attach", "-name", "rotate", "-data", "f.csv"},
			want: [][]string{{"attach", "-name", "rotate", "-data", "f.csv"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			if got := splitStageGroups(tc.args); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitStageGroups(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestPipe_StampPDFStageOverlaysTheStampAsAnArtifact asserts in both
// directions, the way the stamp-text stage test does.
//
// StampPDF draws the stamp page as a Form XObject and marks it an artifact
// (ISO 32000-1, 14.8.2.2), so ExtractText omits it unless WithArtifacts is
// passed. Measured on this fixture: plain text reports the heading once,
// `text -artifacts` reports it twice. Asserting only the artifact count would
// pass if the stamp were not an artifact at all, and asserting only the page
// count would pass if the stage did nothing, so both are checked.
func TestPipe_StampPDFStageOverlaysTheStampAsAnArtifact(t *testing.T) {
	const heading = "Document Heading"

	src := writeTempPDF(t, untaggedTextPDF())
	stamp := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "stamp-pdf", "-stamp", stamp, "-x", "50", "-y", "50", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe stamp-pdf: exit %d, stderr: %s", rc, errOut)
	}

	if got := strings.Count(pipeTextOf(t, out, true), heading); got != 2 {
		t.Errorf("text -artifacts reports %q %d time(s), want 2: the stamp-pdf stage did not overlay the stamp", heading, got)
	}
	if got := strings.Count(pipeTextOf(t, out, false), heading); got != 1 {
		t.Errorf("plain text reports %q %d time(s), want 1: the overlay is not marked as an artifact", heading, got)
	}
	if before, after := pageCountOf(t, src), pageCountOf(t, out); before != after {
		t.Errorf("page count changed from %d to %d: stamp-pdf overlays, it does not add pages", before, after)
	}
}

// TestPipe_NewStagesRejectTheirMissingRequiredFlags pins the usage checks, which
// mirror the standalone commands. A stage that failed to require them would
// reach the writer with an empty name or an absent source.
func TestPipe_NewStagesRejectTheirMissingRequiredFlags(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		stage []string
	}{
		{"attach without -name or -data", []string{"attach"}},
		{"attach with -name but no -data", []string{"attach", "-name", "x.csv"}},
		{"detach without -name", []string{"detach"}},
		{"insert without -source", []string{"insert", "-pos", "0"}},
		{"insert without -pos", []string{"insert", "-source", "whatever.pdf"}},
		{"set-bookmarks without a source of bookmarks", []string{"set-bookmarks"}},
		{"stamp-pdf without -stamp", []string{"stamp-pdf"}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			src := onePageTempPDF(t)
			out := filepath.Join(t.TempDir(), "out.pdf")

			args := append([]string{"pipe", src}, tc.stage...)
			args = append(args, "-o", out)

			rc, errOut := runPipeArgs(t, args)
			if rc == exitOK {
				t.Fatalf("%s succeeded; stderr: %s", tc.desc, errOut)
			}
			if errOut == "" {
				t.Errorf("%s failed silently: nothing on stderr", tc.desc)
			}
		})
	}
}

// TestPipe_SecondDocumentStagesCloseTheirSources extends to the new stages the
// guarantee TestPipe_MergeClosesItsSources pins for merge.
//
// insert and stamp-pdf are the only other stages that open a document of their
// own. Each hands its closer back to runPipe rather than closing it, because
// the reader must stay live until WriteTo executes the stage. Dropping that
// closer is precisely the defect merge once shipped: the *os.File behind
// Reader.closer is never released.
//
// Repeating the run is what makes it visible, and the assertion is any growth
// at all rather than a count. Measured by neutering each stage in turn, eight
// runs leaked 4 descriptors for insert and 2 for stamp-pdf, not 8, because the
// runtime finalizes some of the abandoned files first. openFDs documents the
// noise floor on this platform as zero, so any growth is a real descriptor.
func TestPipe_SecondDocumentStagesCloseTheirSources(t *testing.T) {
	const runs = 8

	target := onePageTempPDF(t)
	source := onePageTempPDF(t)
	stamp := writeTempPDF(t, untaggedTextPDF())

	for _, tc := range []struct {
		desc string
		args func(out string) []string
	}{
		{"insert", func(out string) []string {
			return []string{"pipe", target, "insert", "-source", source, "-pos", "0", "-o", out}
		}},
		{"stamp-pdf", func(out string) []string {
			return []string{"pipe", target, "stamp-pdf", "-stamp", stamp, "-o", out}
		}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			before := openFDs(t)
			for i := 0; i < runs; i++ {
				out := filepath.Join(t.TempDir(), "out.pdf")
				if rc, errOut := runPipeArgs(t, tc.args(out)); rc != exitOK {
					t.Fatalf("exit %d, stderr: %s", rc, errOut)
				}
			}
			if after := openFDs(t); after > before {
				t.Errorf("%s leaked %d descriptor(s) across %d runs (%d -> %d): "+
					"the stage's openPDF closer is not reaching runPipe",
					tc.desc, after-before, runs, before, after)
			}
		})
	}
}
