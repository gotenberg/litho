package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// redactableCLIPDF builds a one-page document drawing "SECRET" at y=700 and
// "KEEP" at y=600 in 12pt Helvetica on a 612x792 page.
//
// The geometry is the point: --region takes x,y,width,height, so 1:90,690,210,40
// is the box [90,690,300,730], which covers the whole "SECRET" run and meets
// nothing at y=600. Narrowing the width to 20 clips the run instead, which must
// be refused rather than half-removed.
func redactableCLIPDF() []byte {
	return redactableCLIBodyPDF("BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET")
}

// redactableCLIBodyPDF builds the document [redactableCLIPDF] does, with body
// as its content stream.
func redactableCLIBodyPDF(body string) []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

func TestRedact_RemovesTheRegionText(t *testing.T) {
	path := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}

	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if bytes.Contains(written, []byte("SECRET")) {
		t.Error("the redacted word is still in the written file")
	}
	if !bytes.Contains(written, []byte("KEEP")) {
		t.Error("text outside the region was removed too")
	}
}

// TestRedact_RefusesPartialCoverageAndWritesNoFile pins both halves of the
// refusal: the command fails, and it leaves NO output behind.
//
// Refusing beats half-redacting, but a refusal that still wrote a file would
// hand the caller a document that looks redacted and is not, which is the
// failure this command exists to prevent. The output is therefore created only
// after the redaction succeeds.
func TestRedact_RefusesPartialCoverageAndWritesNoFile(t *testing.T) {
	// Text rendering mode 7 adds the run to the clipping path, so a region
	// over part of it can neither take it whole nor split it.
	path := writeTempPDF(t, redactableCLIBodyPDF("BT 7 Tr /F1 12 Tf 100 700 Td (SECRET) Tj ET"))
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	// Width 20 clips "SECRET" instead of covering it.
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,20,40", "-o", out, path}, &stdout, &stderr)
	if rc == exitOK {
		t.Fatal("a region covering an operator only partly was accepted")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused redaction left an output file behind (stat err = %v)", err)
	}
}

// TestRedact_AcceptsRepeatedRegions proves the flag is what it advertises.
//
// --region is documented "(repeatable)", and every other test here passes it
// once, so without this the repeat path and the grouping behind it are
// untested: regions are collected per page and applied together, and a second
// region appending to the same page is the case that exercises it.
func TestRedact_AcceptsRepeatedRegions(t *testing.T) {
	path := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{
		"redact",
		"-region", "1:90,690,210,40", // covers SECRET at y=700
		"-region", "1:90,590,210,40", // covers KEEP at y=600
		"-o", out, path,
	}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}

	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	for _, gone := range []string{"SECRET", "KEEP"} {
		if bytes.Contains(written, []byte(gone)) {
			t.Errorf("%q survived: the second --region was not applied", gone)
		}
	}
}

func TestRedact_RejectsAMalformedRegion(t *testing.T) {
	path := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	for _, spec := range []string{"1:90,690", "90,690,210,40", "x:90,690,210,40", "1:90,690,0,40"} {
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(),
			[]string{"redact", "-region", spec, "-o", out, path}, &stdout, &stderr); rc == exitOK {
			t.Errorf("region %q was accepted", spec)
		}
	}
}

func TestRedact_JSONReportNamesWhatWentAway(t *testing.T) {
	path := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"redact", "-f", "json", "-region", "1:90,690,210,40", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}

	var got redactResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("report is not JSON: %v (%s)", err, stdout.String())
	}
	// The report must name the removed run: a bare success would not let a
	// caller check that what it meant to remove is what went.
	var found bool
	for _, run := range got.RemovedText {
		if run == "SECRET" {
			found = true
		}
	}
	if !found {
		t.Errorf("removedText = %v, want it to name the removed run", got.RemovedText)
	}
}
