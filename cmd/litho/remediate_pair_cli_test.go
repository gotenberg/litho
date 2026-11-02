package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// remediate, validate and the pipe's remediate stage take --profile twice for
// a pair: a PDF/A, Factur-X or ZUGFeRD profile and the PDF/UA profile of its
// PDF version, in either order (pdf.WithPDFUA). PDF/UA-1 requires a PDF
// version from 1.0 to 1.7 (ISO 14289-1, 6.1), PDF/UA-2 version 2.0 (ISO
// 14289-2, 6.1) and PDF/A-4 a 2.x version (ISO 19005-4, 6.1.2), so any other
// pair is a usage error.

// pairFixture conforms to PDF/A-2b and not to PDF/UA-1: it carries no
// structure tree, no /Lang and no pdfuaid claim. A remediation to the pair
// fixes the PDF/UA-1 side and leaves the PDF/A-2b one conformant.
//
// # Corpus evidence
//
//   - litho qshyphen_2b.pdf: fully fixed in the remediation-pair cell, and
//     veraPDF 1.30.3 finds no violation of PDF/A-2b or PDF/UA-1 in the output.
const pairFixture = "../../test/integration/corpus/litho/pdfs/qshyphen_2b.pdf"

// pairOrders lists the pair in both orders a caller may give it.
var pairOrders = [][]string{{"PDF/A-2b", "PDF/UA-1"}, {"PDF/UA-1", "PDF/A-2b"}}

// profileArgs spells profiles as repeated --profile flags.
func profileArgs(profiles ...string) []string {
	var args []string
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	return args
}

// decodeRemediateResult decodes the JSON report of remediate.
func decodeRemediateResult(t *testing.T, stdout string) remediateResult {
	t.Helper()
	var got remediateResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a remediate report: %v\n%s", err, stdout)
	}
	return got
}

// decodeValidateResult decodes the JSON report of validate on one file.
func decodeValidateResult(t *testing.T, stdout string) validateResult {
	t.Helper()
	var got validateResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a validate report: %v\n%s", err, stdout)
	}
	return got
}

// TestRemediatePair_EitherOrderConformsToBoth pins that a pair given in either
// order remediates to both profiles: the report echoes the pair as the caller
// gave it and leaves no residual, validate with the pair finds the output
// compliant, and the two orders write the same bytes, since the order names
// the profiles and changes nothing of the remediation.
func TestRemediatePair_EitherOrderConformsToBoth(t *testing.T) {
	rc, stdout, _ := runPipeCLI(t, append([]string{"validate"}, append(profileArgs(pairOrders[0]...), pairFixture)...)...)
	if rc != exitValidationFail || !strings.Contains(stdout, "PDF/A-2b: compliant") || !strings.Contains(stdout, "PDF/UA-1: ") {
		t.Fatalf("precondition: the fixture should conform to PDF/A-2b alone; exit %d:\n%s", rc, stdout)
	}

	var outputs [][]byte
	for _, order := range pairOrders {
		out := filepath.Join(t.TempDir(), "out.pdf")
		args := append([]string{"remediate"}, profileArgs(order...)...)
		rc, stdout, stderr := runPipeCLI(t, append(args, "--strict", "-f", "json", "-o", out, pairFixture)...)
		if rc != exitOK {
			t.Fatalf("%v: exit %d, stderr: %s", order, rc, stderr)
		}
		got := decodeRemediateResult(t, stdout)
		if !slices.Equal(got.Profile, order) {
			t.Errorf("%v: the report names %v", order, got.Profile)
		}
		if len(got.Residual) != 0 {
			t.Errorf("%v: residual %v, want none", order, got.Residual)
		}

		rc, stdout, stderr = runPipeCLI(t, append(append([]string{"validate"}, profileArgs(order...)...), out)...)
		if rc != exitOK {
			t.Errorf("%v: validate with the pair exits %d on the output:\n%s%s", order, rc, stdout, stderr)
		}
		for _, p := range order {
			if !strings.Contains(stdout, p+": compliant\n") {
				t.Errorf("%v: validate does not find the output compliant with %s:\n%s", order, p, stdout)
			}
		}

		data, err := os.ReadFile(out) //nolint:gosec // the test's own temp path
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, data)
	}
	if !bytes.Equal(outputs[0], outputs[1]) {
		t.Error("the two orders of the pair wrote different bytes")
	}
}

// TestRemediatePair_ReportNamesTheProfileOfEachResidual pins that a pair's
// report says which profile each residual violation belongs to. An unembedded
// font breaks ISO 19005-2, 6.2.11.4.1 and ISO 14289-1, 7.21.4.1, whose
// messages read the same: without the profile, the two lines could not be
// told apart. The residual follows the order the caller named the profiles,
// and --strict fails on either. One profile keeps its report as it was.
func TestRemediatePair_ReportNamesTheProfileOfEachResidual(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())

	for _, order := range pairOrders {
		args := append([]string{"remediate"}, profileArgs(order...)...)
		rc, stdout, stderr := runPipeCLI(t, append(args, "-f", "json", "-o", filepath.Join(t.TempDir(), "out.pdf"), src)...)
		if rc != exitOK {
			t.Fatalf("%v: exit %d, stderr: %s", order, rc, stderr)
		}
		var raw struct {
			Profile []string `json:"profile"`
		}
		if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
			t.Errorf("%v: the pair's profile is not a JSON array: %v\n%s", order, err, stdout)
		}
		got := decodeRemediateResult(t, stdout)
		var named []string
		for _, line := range got.Residual {
			profile, _, ok := strings.Cut(line, ": clause ")
			if !ok || !slices.Contains(order, profile) {
				t.Errorf("%v: residual %q names no profile of the pair", order, line)
				continue
			}
			if len(named) == 0 || named[len(named)-1] != profile {
				named = append(named, profile)
			}
		}
		if !slices.Equal(named, order) {
			t.Errorf("%v: the residual runs through the profiles as %v, want both in the caller's order", order, named)
		}

		args = append([]string{"remediate"}, profileArgs(order...)...)
		rc, stdout, _ = runPipeCLI(t, append(args, "--strict", "-o", filepath.Join(t.TempDir(), "out.pdf"), src)...)
		if rc != exitValidationFail {
			t.Errorf("%v: --strict exits %d, want %d", order, rc, exitValidationFail)
		}
		if want := "Profiles: " + strings.Join(order, ", ") + "\n"; !strings.HasPrefix(stdout, want) {
			t.Errorf("%v: the text report does not open with %q:\n%s", order, want, stdout)
		}
		for _, p := range order {
			if !strings.Contains(stdout, "\n  "+p+": clause ") {
				t.Errorf("%v: the text report lists no residual under %s:\n%s", order, p, stdout)
			}
		}
	}

	rc, stdout, stderr := runPipeCLI(t, "remediate", "--profile", "PDF/A-2b", "-f", "json", "-o", filepath.Join(t.TempDir(), "out.pdf"), src)
	if rc != exitOK {
		t.Fatalf("one profile: exit %d, stderr: %s", rc, stderr)
	}
	var single struct {
		Profile  string   `json:"profile"`
		Residual []string `json:"residual"`
	}
	if err := json.Unmarshal([]byte(stdout), &single); err != nil || single.Profile != "PDF/A-2b" {
		t.Errorf("one profile is no longer a JSON string: %v\n%s", err, stdout)
	}
	for _, line := range single.Residual {
		if !strings.HasPrefix(line, "clause ") {
			t.Errorf("one profile's residual %q carries a prefix", line)
		}
	}
}

// invalidPairs lists one --profile list of each class no command takes.
var invalidPairs = []struct {
	name     string
	profiles []string
	want     string
}{
	{"two PDF/A profiles", []string{"PDF/A-2b", "PDF/A-3b"}, "a pair is a PDF/A"},
	{"two PDF/UA profiles", []string{"PDF/UA-1", "PDF/UA-2"}, "a pair is a PDF/A"},
	{"one PDF/UA profile twice", []string{"PDF/UA-1", "PDF/UA-1"}, "a pair is a PDF/A"},
	{"PDF/A-4 with PDF/UA-1", []string{"PDF/A-4", "PDF/UA-1"}, "PDF/A-4 pairs with PDF/UA-2, not PDF/UA-1"},
	{"PDF/UA-2 with PDF/A-2b", []string{"PDF/UA-2", "PDF/A-2b"}, "PDF/A-2b pairs with PDF/UA-1, not PDF/UA-2"},
	{"Factur-X with PDF/UA-2", []string{"Factur-X", "PDF/UA-2"}, "Factur-X pairs with PDF/UA-1, not PDF/UA-2"},
	{"WTPDF with PDF/UA-2", []string{"WTPDF-1.0-Reuse", "PDF/UA-2"}, "a pair is a PDF/A"},
	{"WTPDF with PDF/A-4", []string{"PDF/A-4", "WTPDF-1.0-Accessibility"}, "a pair is a PDF/A"},
	{"an unknown profile with PDF/UA-1", []string{"PDF/A-9z", "PDF/UA-1"}, "a pair is a PDF/A"},
	{"three profiles", []string{"PDF/A-2b", "PDF/UA-1", "PDF/UA-1"}, "a pair is a PDF/A"},
}

// TestRemediatePair_RefusesInvalidPairs pins that remediate and validate
// refuse every --profile list that is no pair as a usage error, before the
// document opens, naming the valid pairs. The input named here does not
// exist, so a command that got as far as opening it would fail with another
// message.
func TestRemediatePair_RefusesInvalidPairs(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pdf")
	for _, tc := range invalidPairs {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.pdf")
			for _, args := range [][]string{
				append(append([]string{"remediate"}, profileArgs(tc.profiles...)...), "-o", out, missing),
				append(append([]string{"validate"}, profileArgs(tc.profiles...)...), missing),
			} {
				rc, _, stderr := runPipeCLI(t, args...)
				if rc != exitError {
					t.Errorf("%s: exit %d, want %d; stderr: %s", args[0], rc, exitError, stderr)
				}
				for _, want := range []string{tc.want, "the pairs are ", validPairs()} {
					if !strings.Contains(stderr, want) {
						t.Errorf("%s: stderr %q lacks %q", args[0], stderr, want)
					}
				}
				if strings.Contains(stderr, "no such file") {
					t.Errorf("%s opened the input before refusing the pair: %q", args[0], stderr)
				}
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("remediate created %s for a refused pair", out)
			}
		})
	}
}

// TestValidPairs pins the list the usage and the refusals print, built from
// the profile tables.
func TestValidPairs(t *testing.T) {
	t.Parallel()
	const want = "PDF/A-1a, PDF/A-1b, PDF/A-2a, PDF/A-2b, PDF/A-2u, PDF/A-3a, PDF/A-3b, PDF/A-3u, " +
		"Factur-X, ZUGFeRD-1.0, ZUGFeRD-2.0 or ZUGFeRD-2.1 with PDF/UA-1, and PDF/A-4, PDF/A-4e or PDF/A-4f with PDF/UA-2"
	if got := validPairs(); got != want {
		t.Errorf("validPairs() = %q, want %q", got, want)
	}
}

// TestValidatePair_ReportsBothProfiles pins that validate judges a pair as two
// profiles: one verdict per profile in the order given, compliant only when
// both pass, and each JSON violation naming its profile. One profile keeps its
// report as it was: a JSON string, and violations naming no profile.
func TestValidatePair_ReportsBothProfiles(t *testing.T) {
	for _, order := range pairOrders {
		args := append([]string{"validate"}, profileArgs(order...)...)
		rc, stdout, stderr := runPipeCLI(t, append(args, pairFixture)...)
		if rc != exitValidationFail {
			t.Fatalf("%v: exit %d, want %d; stderr: %s", order, rc, exitValidationFail, stderr)
		}
		aAt, uaAt := strings.Index(stdout, "PDF/A-2b: compliant\n"), strings.Index(stdout, "PDF/UA-1: ")
		if aAt < 0 || uaAt < 0 || (aAt < uaAt) != (order[0] == "PDF/A-2b") {
			t.Errorf("%v: the verdicts are missing or out of order:\n%s", order, stdout)
		}
		if !strings.Contains(stdout, "violation(s)") {
			t.Errorf("%v: no PDF/UA-1 violation reported:\n%s", order, stdout)
		}

		rc, stdout, _ = runPipeCLI(t, append(args, "-f", "json", pairFixture)...)
		if rc != exitValidationFail {
			t.Errorf("%v: json exits %d, want %d", order, rc, exitValidationFail)
		}
		got := decodeValidateResult(t, stdout)
		if got.Compliant {
			t.Errorf("%v: compliant although the fixture fails PDF/UA-1", order)
		}
		if !slices.Equal(got.Profile, order) {
			t.Errorf("%v: the report names %v", order, got.Profile)
		}
		if len(got.Violations) == 0 {
			t.Fatalf("%v: no violation reported", order)
		}
		for _, v := range got.Violations {
			if v.Profile != "PDF/UA-1" || v.Specification != "ISO_14289_1" {
				t.Errorf("%v: violation of %s under %s, want ISO_14289_1 under PDF/UA-1", order, v.Specification, v.Profile)
			}
		}
	}

	rc, stdout, _ := runPipeCLI(t, "validate", "--profile", "PDF/UA-1", "-f", "json", pairFixture)
	if rc != exitValidationFail {
		t.Errorf("one profile: exit %d, want %d", rc, exitValidationFail)
	}
	var single struct {
		Profile    string           `json:"profile"`
		Violations []map[string]any `json:"violations"`
	}
	if err := json.Unmarshal([]byte(stdout), &single); err != nil || single.Profile != "PDF/UA-1" {
		t.Errorf("one profile is no longer a JSON string: %v\n%s", err, stdout)
	}
	for _, v := range single.Violations {
		if _, ok := v["profile"]; ok {
			t.Errorf("a violation of one profile names its profile: %v", v)
		}
	}
}

// TestPipeRemediatePair pins the pair on the pipe's remediate stage, in
// either order and in the --profile=X spelling: the output conforms to both
// profiles, and --strict fails on a residual of either.
func TestPipeRemediatePair(t *testing.T) {
	for _, stage := range [][]string{
		{"remediate", "-profile", "PDF/A-2b", "-profile", "PDF/UA-1"},
		{"remediate", "--profile=PDF/UA-1", "--profile=PDF/A-2b"},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		rc, _, stderr := runPipeCLI(t, append([]string{"pipe", "--strict", "-o", out, pairFixture}, stage...)...)
		if rc != exitOK {
			t.Fatalf("%v: exit %d, stderr: %s", stage, rc, stderr)
		}
		rc, stdout, stderr := runPipeCLI(t, "validate", "--profile", "PDF/A-2b", "--profile", "PDF/UA-1", out)
		if rc != exitOK {
			t.Errorf("%v: the output fails the pair: exit %d\n%s%s", stage, rc, stdout, stderr)
		}
	}

	src := writeTempPDF(t, redactableCLIPDF())
	rc, _, stderr := runPipeCLI(t, "pipe", "--strict", "-o", filepath.Join(t.TempDir(), "out.pdf"), src,
		"remediate", "-profile", "PDF/UA-1", "-profile", "PDF/A-2b")
	if rc != exitValidationFail {
		t.Errorf("--strict on a residual of the pair: exit %d, want %d; stderr: %s", rc, exitValidationFail, stderr)
	}
}

// TestPipeRemediatePair_RefusesInvalidPairs pins that the pipe's remediate
// stage refuses what the standalone command refuses, as a usage error that
// writes nothing.
func TestPipeRemediatePair_RefusesInvalidPairs(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	for _, tc := range invalidPairs {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.pdf")
			stage := []string{"remediate"}
			for _, p := range tc.profiles {
				stage = append(stage, "-profile", p)
			}
			rc, _, stderr := runPipeCLI(t, append([]string{"pipe", "-o", out, src}, stage...)...)
			if rc != exitError {
				t.Errorf("exit %d, want %d; stderr: %s", rc, exitError, stderr)
			}
			if !strings.Contains(stderr, `pipe stage "remediate"`) || !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, "the pairs are ") {
				t.Errorf("stderr %q does not refuse the pair", stderr)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("pipe created %s for a refused pair", out)
			}
		})
	}
}

// TestRemediatePair_CompactFollowsThePDFASide pins that the PDF/A side of a
// pair decides what --compact may do, whichever profile comes first. ISO
// 19005-1, 6.1.4 forbids the cross-reference stream packing requires and no
// part of ISO 14289 forbids it, so PDF/A-1b with PDF/UA-1 withholds packing,
// says so, and still merges duplicate streams, on the command and on the
// pipe's remediate stage. The output stays conformant to PDF/A-1b.
func TestRemediatePair_CompactFollowsThePDFASide(t *testing.T) {
	src := writeTempPDF(t, twinStreamsPDF())
	for _, order := range [][]string{{"PDF/A-1b", "PDF/UA-1"}, {"PDF/UA-1", "PDF/A-1b"}} {
		plain, _ := runRemediateTo(t, src, order[0], "--profile", order[1])
		deduped, stderr := runRemediateTo(t, src, order[0], "--profile", order[1], "--compact")

		pipeOut := filepath.Join(t.TempDir(), "pipe.pdf")
		rc, _, pipeStderr := runPipeCLI(t, "pipe", "--compact", "-o", pipeOut, src,
			"remediate", "-profile", order[0], "-profile", order[1])
		if rc != exitOK {
			t.Fatalf("%v: pipe exits %d, stderr: %s", order, rc, pipeStderr)
		}
		piped, err := os.ReadFile(pipeOut) //nolint:gosec // the test's own temp path
		if err != nil {
			t.Fatal(err)
		}

		for _, out := range []struct {
			name, stderr string
			data         []byte
		}{{"remediate", stderr, deduped}, {"pipe", pipeStderr, piped}} {
			if bytes.Contains(out.data, []byte("/Type /ObjStm")) || bytes.Contains(out.data, []byte("/Type /XRef")) {
				t.Errorf("%v %s: the output carries a construct ISO 19005-1, 6.1.4 forbids", order, out.name)
			}
			if !strings.Contains(out.stderr, warnCompactProfile) {
				t.Errorf("%v %s: no notice that packing was withheld; stderr: %q", order, out.name, out.stderr)
			}
			if got, want := streamCount(out.data), streamCount(plain)-1; got != want {
				t.Errorf("%v %s: the output carries %d streams with --compact and %d without, want %d", order, out.name, got, streamCount(plain), want)
			}
		}
		path := writeTempPDF(t, deduped)
		if v := pipeA1bViolations(t, path); len(v) != 0 {
			t.Errorf("%v: --compact cost the output PDF/A-1b: %v", order, v)
		}
	}

	packed, stderr := runRemediateTo(t, compactFixtureFile(t), "PDF/UA-1", "--profile", "PDF/A-2b", "--compact")
	if !bytes.Contains(packed, []byte("/Type /ObjStm")) || strings.Contains(stderr, "does not pack objects") {
		t.Errorf("PDF/A-2b with PDF/UA-1 withheld packing, which neither forbids; stderr: %q", stderr)
	}
}

// TestRemediatePair_FacturXWithPDFUA1 pins that Factur-X pairs with PDF/UA-1
// on its PDF/A-3 base: the invoice flags apply as they do alone, and the
// output conforms to PDF/A-3b and PDF/UA-1. The stub invoice fails the
// Factur-X rules whatever the pair, so the test judges the PDF/A-3b base.
func TestRemediatePair_FacturXWithPDFUA1(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.pdf")
	rc, stdout, stderr := runPipeCLI(t, "remediate", "--profile", "PDF/UA-1", "--profile", "Factur-X",
		"--invoice", writeTempCII(t), "--level", "EN 16931", "-o", out, pairFixture)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr)
	}
	if !strings.HasPrefix(stdout, "Profiles: PDF/UA-1, Factur-X\n") {
		t.Errorf("the report does not name the pair:\n%s", stdout)
	}
	if rc, stdout, stderr := runPipeCLI(t, "validate", "--profile", "PDF/A-3b", "--profile", "PDF/UA-1", out); rc != exitOK {
		t.Errorf("the output fails PDF/A-3b or PDF/UA-1: exit %d\n%s%s", rc, stdout, stderr)
	}
}

// TestRemediatePair_PDFASideDropsEncryption pins that a pair warns about an
// encrypted input as its PDF/A side does, whichever profile comes first: PDF/A
// forbids encryption (ISO 19005-2, 6.1.3), so the warning offers no litho
// encrypt, which PDF/UA-1 alone would leave open.
func TestRemediatePair_PDFASideDropsEncryption(t *testing.T) {
	src := encryptTempPDF(t, twoPageTempPDF(t), "pw")
	for _, profiles := range [][]string{{"PDF/UA-1", "PDF/A-2b"}, {"PDF/A-2b", "PDF/UA-1"}} {
		args := append([]string{"remediate", "-p", "pw"}, profileArgs(profiles...)...)
		rc, _, stderr := runPipeCLI(t, append(args, "-o", filepath.Join(t.TempDir(), "out.pdf"), src)...)
		if rc != exitOK {
			t.Fatalf("%v: exit %d, stderr: %s", profiles, rc, stderr)
		}
		line := encryptionWarningLine(t, stderr)
		if !strings.Contains(line, "PDF/A forbids encryption") || strings.Contains(line, "litho encrypt") {
			t.Errorf("%v: the warning does not follow the PDF/A side: %q", profiles, line)
		}
	}
}

// TestParseProfiles pins how a --profile list reads: one profile is the
// target, whatever it names, and a pair in either order has its PDF/A-based
// profile as the target and its PDF/UA one beside it.
func TestParseProfiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		names         []string
		target, pdfua string
	}{
		{[]string{"PDF/UA-1"}, "PDF/UA-1", ""},
		{[]string{"PDF/A-9z"}, "PDF/A-9z", ""},
		{[]string{"PDF/A-1b", "PDF/UA-1"}, "PDF/A-1b", "PDF/UA-1"},
		{[]string{"PDF/UA-1", "PDF/A-3u"}, "PDF/A-3u", "PDF/UA-1"},
		{[]string{"PDF/UA-2", "PDF/A-4f"}, "PDF/A-4f", "PDF/UA-2"},
		{[]string{"ZUGFeRD-1.0", "PDF/UA-1"}, "ZUGFeRD-1.0", "PDF/UA-1"},
		{[]string{"PDF/UA-1", "Factur-X"}, "Factur-X", "PDF/UA-1"},
	} {
		got, err := parseProfiles(tc.names)
		if err != nil {
			t.Errorf("parseProfiles(%v): %v", tc.names, err)
			continue
		}
		if got.target != tc.target || got.pdfua != tc.pdfua || !slices.Equal(got.names, tc.names) {
			t.Errorf("parseProfiles(%v) = %+v, want target %q, pdfua %q", tc.names, got, tc.target, tc.pdfua)
		}
	}
	if _, err := parseProfiles(nil); err == nil {
		t.Error("parseProfiles(nil) accepted no profile")
	}
}
