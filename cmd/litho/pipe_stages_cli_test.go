package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// TestPipe_SplitStageSelectsThePageRange pins semantics that read backwards
// from the stage's name.
//
// "split" here does not divide a document into several files. It SELECTS a page
// range into one, which is why the stage is happy with a single -o: the
// standalone command has the identical contract, an "Output file (required)"
// passed to pdf.Split as one destination. Measured both ways below, because the
// name suggests a multi-file output and invites a search for a defect that was
// never there.
func TestPipe_SplitStageSelectsThePageRange(t *testing.T) {
	src := twoPageTempPDF(t)

	for _, tc := range []struct {
		name  string
		pages string
		want  int
	}{
		{"one page of two", "1", 1},
		{"both pages", "1-2", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "split.pdf")

			rc, errOut := runPipeArgs(t, []string{
				"pipe", src, "split", "-pages", tc.pages, "-o", out,
			})
			if rc != exitOK {
				t.Fatalf("pipe split -pages %s: exit %d, stderr: %s", tc.pages, rc, errOut)
			}
			if got := pageCountOf(t, out); got != tc.want {
				t.Errorf("-pages %s produced %d page(s), want %d", tc.pages, got, tc.want)
			}
		})
	}
}

// TestPipe_EncryptStageProducesAnEncryptedDocument pins the one stage whose
// effect is invisible to a page count.
//
// The check is behavioural rather than a scan for /Encrypt in the output bytes:
// the token can sit inside an object stream, and an incremental update leaves
// superseded dictionaries behind, so bytes answer neither direction reliably.
// What a reader experiences is the contract.
func TestPipe_EncryptStageProducesAnEncryptedDocument(t *testing.T) {
	src := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "encrypted.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "encrypt", "-user-password", "s3cret", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe encrypt: exit %d, stderr: %s", rc, errOut)
	}
	if opensWithoutAPassword(t, out) {
		t.Error("the encrypt stage produced a document that opens with no password")
	}
}

// TestPipe_EncryptStageGrantsWhatTheEncryptCommandGrants pins the permissions
// the encrypt stage writes against those of the encrypt command, which grants
// every one (-1).
//
// The stage left [pdf.EncryptConfig.Permissions] at its zero value, so the P
// key of its output was 0: every permission denied, printing and copying
// included, and with them the extraction for accessibility that ISO 14289-1,
// 7.16 requires of an encrypted PDF/UA-1 file, so `pipe f encrypt ...
// remediate -profile PDF/UA-1` wrote an output that fails the profile it was
// remediated to.
func TestPipe_EncryptStageGrantsWhatTheEncryptCommandGrants(t *testing.T) {
	ctx := context.Background()
	src := onePageTempPDF(t)
	const password = "s3cret"
	command := encryptTempPDF(t, src, password)
	piped := filepath.Join(t.TempDir(), "piped.pdf")
	if rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "encrypt", "-user-password", password, "-o", piped,
	}); rc != exitOK {
		t.Fatalf("pipe encrypt: exit %d, stderr: %s", rc, errOut)
	}

	open := func(path string) *pdf.Reader {
		t.Helper()
		r, err := pdf.OpenFile(ctx, path, pdf.WithPassword(password))
		if err != nil {
			t.Fatalf("opening %s: %v", path, err)
		}
		t.Cleanup(func() { _ = r.Close() })
		if !r.IsEncrypted() {
			t.Fatalf("%s is not encrypted", path)
		}
		return r
	}
	want, got := open(command).Permissions(), open(piped).Permissions()
	if got != want {
		t.Errorf("the encrypt stage grants %+v, the encrypt command %+v", got, want)
	}

	violations, err := open(piped).ValidatePDFUAReport(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport: %v", err)
	}
	for _, v := range violations {
		if v.Clause == "7.16" {
			t.Errorf("the encrypt stage bars extraction for accessibility: %s", v.Context)
		}
	}
}

// TestPipe_MergeThenDeleteComposesInOrder pins ORDER, not just execution.
//
// merge is the only stage taking positional file arguments, so it is the one
// most likely to swallow the stage that follows it. Two one-page documents
// merged give two pages; deleting one then leaves exactly one. A result of two
// pages would mean delete never ran, and a result of one page with merge
// skipped is impossible because deleting the only page of a one-page document
// is refused outright.
func TestPipe_MergeThenDeleteComposesInOrder(t *testing.T) {
	src := onePageTempPDF(t)
	other := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "composed.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "merge", other, "delete", "-pages", "1", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe merge+delete: exit %d, stderr: %s", rc, errOut)
	}
	if got := pageCountOf(t, out); got != 1 {
		t.Errorf("output has %d page(s), want 1: the merge and delete stages did not both run", got)
	}
}
