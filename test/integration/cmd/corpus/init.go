package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	fmt.Printf("%s: running provisioning container...\n", m.Name)
	run := exec.Command("docker", "run", "--rm", //nolint:gosec // paths come from CLI flags / manifest, not untrusted input
		"-v", testdataRoot+":/output",
		image,
		m.Source,
		strings.Join(m.Profiles, ","),
	)
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr
	if err := run.Run(); err != nil {
		return fmt.Errorf("docker run: %w", err)
	}
	fmt.Printf("%s: provisioned.\n", m.Name)
	return nil
}

// provisioned reports whether the testdata root already carries a
// provisioned corpus. The test looks at whichever golden directory the
// manifest declares first; empty manifests provision unconditionally.
func provisioned(testdataRoot string, m *Manifest) bool {
	for _, rel := range m.Goldens {
		p := filepath.Join(testdataRoot, rel)
		entries, err := os.ReadDir(p)
		if err == nil && len(entries) > 0 {
			return true
		}
		return false
	}
	return false
}

// hasDockerfile reports whether dir contains a Dockerfile.
func hasDockerfile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "Dockerfile"))
	return err == nil
}
