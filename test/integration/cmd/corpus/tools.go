package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"
)

// localToolTimeout caps one local tool probe. Anti-hang guard: the slowest
// probe starts a JVM and validates a one-page PDF in a few seconds, so a probe
// silent for a minute is wedged.
const localToolTimeout = time.Minute

//nolint:gochecknoglobals // static pattern
var dottedVersionPattern = regexp.MustCompile(`\d+(?:\.\d+)+`)

//nolint:gochecknoglobals // static pattern
var javaVersionPattern = regexp.MustCompile(`version "([^"]+)"`)

// localTool is one tool the host runs, probed against its pin.
type localTool struct {
	// Name is the command's name on PATH.
	Name string

	// PinKey is the versions.env key the local version is compared with.
	PinKey string

	// Args runs the command so that it prints its version.
	Args []string

	// StdoutOnly parses standard output alone. veraPDF needs it: its JVM
	// prints warnings on standard error that would corrupt the JSON report.
	// The other tools print their version on either stream.
	StdoutOnly bool

	// Parse extracts the compared version and any detail worth printing
	// from the command's output.
	Parse func(out []byte) (version, detail string, err error)

	// Matches reports whether the local version satisfies the pin.
	Matches func(pin, local string) bool
}

// toolStatus is one row of the `corpus tools` report.
type toolStatus struct {
	Name   string
	Pin    string
	Local  string
	Status string
	Detail string
}

// localTools lists the tools live checks and fixture builders run on the
// host. veraPDF is probed by validating a one-page PDF, since only a report
// names the core version: `verapdf --version` prints the apps version, and a
// Homebrew build reports apps 1.30.0 over core 1.30.2.
func localTools(veraPDFProbe string) []localTool {
	exact := func(pin, local string) bool { return pin == local }
	return []localTool{
		{
			Name: "verapdf", PinKey: "VERAPDF_VERSION",
			Args:       []string{"--format", "json", "--flavour", "1b", veraPDFProbe},
			StdoutOnly: true, Parse: parseVeraPDFProbe, Matches: exact,
		},
		{Name: "node", PinKey: "NODE_VERSION", Args: []string{"--version"}, Parse: parseDottedVersion, Matches: exact},
		{Name: "java", PinKey: "JRE_VERSION", Args: []string{"-version"}, Parse: parseJavaVersion, Matches: javaMatches},
		{Name: "qpdf", PinKey: "QPDF_VERSION", Args: []string{"--version"}, Parse: parseDottedVersion, Matches: exact},
		{Name: "pdfsig", PinKey: "POPPLER_VERSION", Args: []string{"-v"}, Parse: parseDottedVersion, Matches: exact},
		{
			Name: "python3", PinKey: "PIKEPDF_VERSION",
			Args:  []string{"-c", "import pikepdf; print(pikepdf.__version__)"},
			Parse: parseDottedVersion, Matches: exact,
		},
	}
}

// runTools prints the local tools' versions against the pins. It reports and
// never fails: a differing local tool is the operator's call.
func runTools(args []string) int {
	fs := flag.NewFlagSet("tools", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}
	pins, err := loadPins()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	for _, n := range pins.Notices {
		fmt.Fprintf(os.Stderr, "versions: %s\n", n)
	}

	dir, err := os.MkdirTemp("", "litho-corpus-tools-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tempdir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)
	probe := filepath.Join(dir, "probe.pdf")
	if err := os.WriteFile(probe, minimalPDF(), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write probe PDF: %v\n", err)
		return 1
	}

	rows := make([]toolStatus, 0, len(localTools(probe)))
	for _, tool := range localTools(probe) {
		rows = append(rows, probeTool(context.Background(), tool, pins.Values[tool.PinKey]))
	}
	if err := printToolStatus(os.Stdout, rows); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	return 0
}

// probeTool runs one tool and compares what it reports with its pin.
func probeTool(ctx context.Context, tool localTool, pin string) toolStatus {
	row := toolStatus{Name: tool.Name, Pin: tool.PinKey + "=" + printablePin(pin)}
	bin, err := exec.LookPath(tool.Name)
	if err != nil {
		row.Status = "missing"
		row.Detail = "not on PATH"
		return row
	}
	ctx, cancel := context.WithTimeout(ctx, localToolTimeout)
	defer cancel()
	// A non-zero exit is not a failure here: veraPDF exits 1 on a
	// non-compliant document, and the version is in the output either way.
	cmd := exec.CommandContext(ctx, bin, tool.Args...) //nolint:gosec // fixed tool names and arguments
	var out []byte
	var runErr error
	if tool.StdoutOnly {
		out, runErr = cmd.Output()
	} else {
		out, runErr = cmd.CombinedOutput()
	}
	version, detail, err := tool.Parse(out)
	if err != nil {
		row.Status = "missing"
		row.Detail = fmt.Sprintf("%s: %v", bin, err)
		if runErr != nil {
			row.Detail += fmt.Sprintf(" (%v)", runErr)
		}
		return row
	}
	row.Local = version
	row.Detail = bin
	if detail != "" {
		row.Detail = detail + " (" + bin + ")"
	}
	row.Status = "differs"
	if tool.Matches(pin, version) {
		row.Status = "ok"
	}
	return row
}

// printToolStatus writes the rows as an aligned table.
func printToolStatus(w io.Writer, rows []toolStatus) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TOOL\tPIN\tLOCAL\tSTATUS\tDETAIL")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Name, r.Pin, printablePin(r.Local), r.Status, r.Detail)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("print tool versions: %w", err)
	}
	return nil
}

// parseDottedVersion returns the first dotted version number in a tool's
// output: "qpdf version 12.4.1", "pdfsig version 26.09.0", "v24.11.0".
func parseDottedVersion(out []byte) (string, string, error) {
	v := dottedVersionPattern.Find(out)
	if v == nil {
		return "", "", fmt.Errorf("no version in %q", firstLine(out))
	}
	return string(v), "", nil
}

// parseJavaVersion returns the quoted version `java -version` prints, which
// reads the same for every vendor: `openjdk version "17.0.20.1" 2026-08-18`.
func parseJavaVersion(out []byte) (string, string, error) {
	m := javaVersionPattern.FindSubmatch(out)
	if m == nil {
		return "", "", fmt.Errorf("no version in %q", firstLine(out))
	}
	return string(m[1]), "", nil
}

// javaMatches compares a Temurin release name (17.0.20.1+1) with the version
// `java -version` quotes (17.0.20.1), which carries no build number.
func javaMatches(pin, local string) bool {
	release, _, _ := strings.Cut(pin, "+")
	return release == local
}

// parseVeraPDFProbe reads the build header of the JSON report veraPDF wrote
// for the probe PDF and returns the core version, the one that validates.
func parseVeraPDFProbe(out []byte) (string, string, error) {
	// veraPDF may log to the same stream before the report starts.
	start := bytes.IndexByte(out, '{')
	if start < 0 {
		return "", "", fmt.Errorf("no JSON report in %q", firstLine(out))
	}
	var report struct {
		Report struct {
			BuildInformation struct {
				ReleaseDetails []veraPDFRelease `json:"releaseDetails"`
			} `json:"buildInformation"`
		} `json:"report"`
	}
	dec := json.NewDecoder(bytes.NewReader(out[start:]))
	if err := dec.Decode(&report); err != nil {
		return "", "", fmt.Errorf("decode veraPDF report: %w", err)
	}
	releases := report.Report.BuildInformation.ReleaseDetails
	parts := make([]string, 0, len(releases))
	core := ""
	for _, r := range releases {
		parts = append(parts, r.ID+" "+r.Version)
		if r.ID == "core" {
			core = r.Version
		}
	}
	if core == "" {
		return "", "", errors.New("the veraPDF report names no core release")
	}
	return core, strings.Join(parts, ", "), nil
}

// firstLine returns the first line of a command's output, for messages.
func firstLine(out []byte) string {
	line, _, _ := bytes.Cut(bytes.TrimSpace(out), []byte("\n"))
	return string(line)
}

// minimalPDF returns a one-page PDF with a correct cross-reference table, the
// smallest input veraPDF validates without falling back to repair.
func minimalPDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects))
	for i, obj := range objects {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}
