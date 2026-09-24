package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// runInit provisions one or more corpora via Docker. For each corpus,
// the manifest's Source directory under test/integration/corpus/ must
// ship a Dockerfile; the image writes PDFs and goldens to the corpus
// testdata directory.
func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	corpus := fs.String("corpus", "", "Comma-separated corpus names (empty = all)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}

	names, err := resolveCorpora(splitCSV(*corpus))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	for _, name := range names {
		manifest, err := LoadManifest(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			return 1
		}
		if err := provisionCorpus(manifest); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			return 1
		}
	}
	return 0
}

// provisionCorpus builds the corpus Docker image and runs it with the
// testdata directory bind-mounted at /output.
func provisionCorpus(m *Manifest) error {
	testdataRoot, err := filepath.Abs(m.TestdataRoot())
	if err != nil {
		return fmt.Errorf("resolve testdata root: %w", err)
	}
	if err := os.MkdirAll(testdataRoot, 0o755); err != nil {
		return fmt.Errorf("mkdir testdata: %w", err)
	}

	if provisioned(testdataRoot, m) {
		fmt.Printf("%s: already provisioned (delete %s to force regeneration)\n", m.Name, testdataRoot)
		return nil
	}

	dockerCtx := corpusRoot()
	if dockerCtx == "" || !hasDockerfile(dockerCtx) {
		return fmt.Errorf("docker context not found at %q", dockerCtx)
	}
	const image = "litho-corpus-builder"

	fmt.Printf("%s: building shared corpus image (%s)...\n", m.Name, image)
	build := exec.Command("docker", "build", "-t", image, dockerCtx) //nolint:gosec // build context is the fixed corpus directory
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}

	// Auto-mode corpora rely on a small in-tree helper (the
	// `scan-claims` subcommand) to attach each PDF's claimed conformance
	// profile before VeraPDF runs. Cross-compile it for linux/amd64 on
	// the host, then bind-mount the binary into the container so the
	// provisioning image stays Go-free.
	binaryMount := []string{}
	if m.EffectiveProfileMode() == ProfileModeAuto {
		binPath, err := buildScanClaimsBinary()
		if err != nil {
			return fmt.Errorf("build scan-claims helper: %w", err)
		}
		defer os.RemoveAll(filepath.Dir(binPath))
		binaryMount = []string{"-v", binPath + ":/work/bin/scan-claims:ro"}
	}

	fmt.Printf("%s: running provisioning container...\n", m.Name)
	args := make([]string, 0, 4+len(binaryMount)+5)
	args = append(args, "run", "--rm", "-v", testdataRoot+":/output")
	args = append(args, binaryMount...)
	args = append(args, image,
		m.Source,
		strings.Join(m.Profiles, ","),
		strings.Join(featuresFromGoldens(m), ","),
		m.EffectiveProfileMode(),
	)
	run := exec.Command("docker", args...) //nolint:gosec // paths come from CLI flags / manifest, not untrusted input
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr
	if err := run.Run(); err != nil {
		return fmt.Errorf("docker run: %w", err)
	}
	fmt.Printf("%s: provisioned.\n", m.Name)
	return nil
}

// buildScanClaimsBinary cross-compiles the `corpus scan-claims` helper
// for linux/amd64 (matching the verapdf/cli base image) and returns
// the path to the resulting binary. The caller is responsible for
// cleaning up the binary's parent directory.
func buildScanClaimsBinary() (string, error) {
	dir, err := os.MkdirTemp("", "litho-scanclaims-")
	if err != nil {
		return "", fmt.Errorf("tempdir: %w", err)
	}
	binPath := filepath.Join(dir, "scan-claims")
	cmd := exec.Command("go", "build", "-o", binPath, "./test/integration/cmd/corpus") //nolint:gosec // package path is fixed
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("go build: %w", err)
	}
	return binPath, nil
}

// provisioned reports whether the testdata root already carries a
// provisioned corpus. The PDF tree must hold at least one PDF and every
// declared golden directory must exist; a partial run (e.g. download
// succeeded but VeraPDF crashed) returns false so the entrypoint resumes
// the missing stages instead of skipping straight to scoring. Empty
// manifests are not considered provisioned.
//
// One golden is legitimately empty. run-verapdf.sh buckets PDFs by the
// profile each one claims in XMP, so an auto-mode corpus whose scan found
// no claim at all (claims.csv present, zero rows) gets no VeraPDF report
// and no cell in the baseline. Demanding a non-empty compliance directory
// marked such a corpus unprovisioned forever, and every `make corpus-*`
// shelled out to a multi-minute emulated `docker build` that never needed
// to run.
//
// # Corpus evidence
//
//   - govdocs: its scan finds no profile claim, so it has no VeraPDF report
//     and no compliance cell.
//   - pdf20: its scan finds no profile claim either, so it is in the same
//     state.
func provisioned(testdataRoot string, m *Manifest) bool {
	if len(m.Goldens) == 0 {
		return false
	}

	pdfRoot := testdataRoot
	if m.PDFRoot != "" {
		pdfRoot = filepath.Join(testdataRoot, m.PDFRoot)
	}
	if !hasPDF(pdfRoot) {
		return false
	}

	claimless := m.EffectiveProfileMode() == ProfileModeAuto && claimsScannedEmpty(testdataRoot)

	for feature, rel := range m.Goldens {
		dir := filepath.Join(testdataRoot, rel)
		if claimless && veraPDFGolden(feature) {
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				return false
			}
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) == 0 {
			return false
		}
	}
	return true
}

// veraPDFGolden reports whether a feature's golden comes from the
// entrypoint's single VeraPDF stage. Mirrors the guard in
// test/integration/corpus/entrypoint.sh; keep the two in sync.
func veraPDFGolden(feature string) bool {
	switch feature {
	case "compliance", "remediation", "remediation-aggressive":
		return true
	default:
		return false
	}
}

// hasPDF reports whether dir holds at least one PDF at its top level.
// Every corpus lays its PDFs out flat, matching the download stage's own
// `find -maxdepth 1` count.
func hasPDF(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
			return true
		}
	}
	return false
}

// claimsScannedEmpty reports whether an auto-mode claim scan already ran
// and found nothing: claims.csv exists and holds zero rows. A missing
// claims.csv means the scan never ran, which is not provisioned.
func claimsScannedEmpty(testdataRoot string) bool {
	info, err := os.Stat(filepath.Join(testdataRoot, "claims.csv"))
	return err == nil && !info.IsDir() && info.Size() == 0
}

// hasDockerfile reports whether dir contains a Dockerfile.
func hasDockerfile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "Dockerfile"))
	return err == nil
}

// featuresFromGoldens returns the sorted set of feature names declared
// in the manifest's goldens map. The provisioning entrypoint uses this
// list to skip golden generators a corpus does not need (e.g. PDF.js
// text/metadata are pointless for the VeraPDF corpus).
func featuresFromGoldens(m *Manifest) []string {
	names := make([]string, 0, len(m.Goldens))
	for k := range m.Goldens {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
