package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const xfaWarning = "the input carries XFA form data, which the output will not"

// xfaFormFixturePDF builds a hybrid form: one real widget field AND an
// /AcroForm /XFA array, the shape ISO 32000-1, 12.7.8 describes.
//
// Hand-built because no litho command creates XFA, so the fixture cannot be
// produced through the CLI the way encryptTempPDF produces an encrypted one.
// It mirrors formFixturePDF's offset-computed xref for the same reason that one
// gives: a stale offset makes the file unopenable and would fail the test for a
// reason unrelated to the warning.
func xfaFormFixturePDF() []byte {
	const xfa = `<xdp:xdp xmlns:xdp="http://ns.adobe.com/xdp/"><config/></xdp:xdp>`

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (John) /Rect [100 700 300 720] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(xfa), xfa),
		"<< /Fields [4 0 R] /XFA [(datasets) 5 0 R] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestSplit_WarnsWhenXFAIsDropped pins the silent half of the form-data loss.
//
// A split or a merge can leave an XFA document with no form at all, because
// the page operations carry widgets and not /XFA. Dropping it is defensible;
// saying nothing is not.
//
// # Corpus evidence
//
//   - All corpora: a split or a merge leaves 19 of 23 XFA-bearing files with
//     no form at all, one of them losing 1455 fields.
func TestSplit_WarnsWhenXFAIsDropped(t *testing.T) {
	src := writeTempPDF(t, xfaFormFixturePDF())
	out := filepath.Join(t.TempDir(), "split.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"split", "-pages", "1", "-o", out, src}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("split: exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), xfaWarning) {
		t.Errorf("no warning that XFA was dropped; stderr = %q", stderr.String())
	}
}

// TestMerge_WarnsWhenAnySourceCarriesXFA covers the multi-input half, which is
// the only reason warnXFADropped is variadic.
//
// The XFA file goes SECOND on purpose: checking only the first reader would
// pass a test that put it first, and a merge that silently drops a later
// input's form is exactly what the warning reports.
func TestMerge_WarnsWhenAnySourceCarriesXFA(t *testing.T) {
	plain := writeTempPDF(t, untaggedTextPDF())
	xfa := writeTempPDF(t, xfaFormFixturePDF())
	out := filepath.Join(t.TempDir(), "merged.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"merge", "-o", out, plain, xfa}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("merge: exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), xfaWarning) {
		t.Errorf("no warning although a later source carried XFA; stderr = %q", stderr.String())
	}
}

// TestSplit_NoXFAWarningForAPlainInput is the first control. Without it the
// warning could fire unconditionally and both tests above would still pass.
func TestSplit_NoXFAWarningForAPlainInput(t *testing.T) {
	plain := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "split.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"split", "-pages", "1", "-o", out, plain}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("split: exit %d, stderr: %s", rc, stderr.String())
	}
	if strings.Contains(stderr.String(), xfaWarning) {
		t.Errorf("warned on an input with no form at all; stderr = %q", stderr.String())
	}
}

// TestSplit_NoXFAWarningForAnOrdinaryForm is the control that matters most.
//
// formFixturePDF carries a real /AcroForm with two widget fields and no /XFA.
// Its fields survive a split intact, so warning here would be noise, and a
// predicate keyed on /AcroForm presence rather than on /XFA would fire.
func TestSplit_NoXFAWarningForAnOrdinaryForm(t *testing.T) {
	form := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "split.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"split", "-pages", "1", "-o", out, form}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("split: exit %d, stderr: %s", rc, stderr.String())
	}
	if strings.Contains(stderr.String(), xfaWarning) {
		t.Errorf("warned on an ordinary AcroForm that survives the split; stderr = %q",
			stderr.String())
	}
}
