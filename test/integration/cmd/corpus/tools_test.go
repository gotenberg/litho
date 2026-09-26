package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestLocalToolParsers feeds each parser the output its tool prints, as
// captured from the tools on a contributor machine.
func TestLocalToolParsers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		parse func([]byte) (string, string, error)
		out   string
		want  string
	}{
		{"qpdf", parseDottedVersion, "qpdf version 12.4.1\nRun qpdf --copyright to see copyright and license information.\n", "12.4.1"},
		{"pdfsig", parseDottedVersion, "pdfsig version 26.09.0\nCopyright 2005-2026 The Poppler Developers\n", "26.09.0"},
		{"node", parseDottedVersion, "v24.11.0\n", "24.11.0"},
		{"pikepdf", parseDottedVersion, "9.11.0\n", "9.11.0"},
		{"temurin", parseJavaVersion, "openjdk version \"17.0.20.1\" 2026-08-18\nOpenJDK Runtime Environment Temurin-17.0.20.1+1 (build 17.0.20.1+1)\n", "17.0.20.1"},
		{"java with a tool-options banner", parseJavaVersion, "Picked up JAVA_TOOL_OPTIONS: -Xmx1g 2.5\nopenjdk version \"21.0.4\" 2024-07-16\n", "21.0.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _, err := tc.parse([]byte(tc.out))
			if err != nil || got != tc.want {
				t.Fatalf("parse(%q) = %q, %v; want %q", tc.out, got, err, tc.want)
			}
		})
	}

	// macOS ships a java stub that answers without a runtime installed.
	if _, _, err := parseJavaVersion([]byte("The operation couldn’t be completed. Unable to locate a Java Runtime.\n")); err == nil {
		t.Error("parseJavaVersion found a version in the macOS stub's refusal")
	}
	if _, _, err := parseDottedVersion([]byte("command not found\n")); err == nil {
		t.Error("parseDottedVersion found a version in an error message")
	}
}

// TestParseVeraPDFProbeReadsCore pins that the probe reports the core
// version, not the apps version `verapdf --version` prints. A Homebrew build
// carries apps 1.30.0 over core and validation-model 1.30.2, and it is the
// core that validates.
func TestParseVeraPDFProbeReadsCore(t *testing.T) {
	t.Parallel()

	out := `{"report":{"buildInformation":{"releaseDetails":[{
  "id" : "core",
  "version" : "1.30.2",
  "buildDate" : 1780476120000
},{
  "id" : "validation-model",
  "version" : "1.30.2",
  "buildDate" : 1780476360000
},{
  "id" : "apps",
  "version" : "1.30.0",
  "buildDate" : 1780487220000
}]},"jobs":[]}}`
	core, detail, err := parseVeraPDFProbe([]byte(out))
	if err != nil || core != "1.30.2" {
		t.Fatalf("parseVeraPDFProbe = %q, %v; want core 1.30.2", core, err)
	}
	if detail != "core 1.30.2, validation-model 1.30.2, apps 1.30.0" {
		t.Errorf("detail = %q, want every component named", detail)
	}

	if _, _, err := parseVeraPDFProbe([]byte("Error: JAVA_HOME is not defined correctly.\n")); err == nil {
		t.Error("parseVeraPDFProbe accepted output with no report")
	}
	if _, _, err := parseVeraPDFProbe([]byte(`{"report":{"jobs":[]}}`)); err == nil {
		t.Error("parseVeraPDFProbe accepted a report with no core release")
	}
}

func TestJavaMatchesIgnoresTheBuildNumber(t *testing.T) {
	t.Parallel()

	if !javaMatches("17.0.20.1+1", "17.0.20.1") {
		t.Error("17.0.20.1 should satisfy the pin 17.0.20.1+1")
	}
	if javaMatches("17.0.20.1+1", "17.0.20") {
		t.Error("17.0.20 should not satisfy the pin 17.0.20.1+1")
	}
}

// TestMinimalPDFOpens checks the probe document is a well-formed PDF, so a
// veraPDF failure on it means a broken veraPDF rather than a broken probe.
func TestMinimalPDFOpens(t *testing.T) {
	t.Parallel()

	data := minimalPDF()
	r, err := pdf.Open(t.Context(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open probe PDF: %v", err)
	}
	defer r.Close()
	n, err := r.PageCount(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("probe PDF has %d pages (%v), want 1", n, err)
	}
	if !strings.HasPrefix(string(data), "%PDF-1.4\n") || !strings.HasSuffix(string(data), "%%EOF\n") {
		t.Fatalf("probe PDF is not framed by a header and %%%%EOF:\n%s", data)
	}
}

func TestPrintToolStatusAligns(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := printToolStatus(&buf, []toolStatus{
		{Name: "verapdf", Pin: "VERAPDF_VERSION=1.30.2", Local: "1.30.2", Status: "ok", Detail: "/opt/homebrew/bin/verapdf"},
		{Name: "java", Pin: "JRE_VERSION=17.0.20.1+1", Status: "missing", Detail: "not on PATH"},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "TOOL") {
		t.Fatalf("table = %q, want a header and two rows", buf.String())
	}
	if !strings.Contains(lines[2], "(none)") {
		t.Errorf("a missing tool's local version should read (none): %q", lines[2])
	}
}
