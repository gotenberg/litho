package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "verapdf":
		os.Exit(runVeraPDF(os.Args[2:]))
	case "remediate":
		os.Exit(runRemediate(os.Args[2:]))
	case "pdfjs":
		runPDFJS(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: corpus <verapdf|remediate|pdfjs> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Subcommands:")
	fmt.Fprintln(os.Stderr, "  verapdf     Compare litho against VeraPDF corpus")
	fmt.Fprintln(os.Stderr, "  remediate   Run VeraPDF remediation report")
	fmt.Fprintln(os.Stderr, "  pdfjs       Compare litho against PDF.js corpus")
}

// findCorpusPath locates the VeraPDF testdata directory. Used by both the
// verapdf and remediate subcommands.
func findCorpusPath() string {
	candidates := []string{
		"test/integration/testdata/verapdf",
		"testdata/verapdf",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return "test/integration/testdata/verapdf"
}

// provisionCorpus provisions a corpus via Docker. name is the corpus directory
// name (e.g., "verapdf", "pdf.js") and imageName is the Docker image tag.
func provisionCorpus(corpusPath, name, imageName string) error {
	refsDir := filepath.Join(corpusPath, "references")
	entries, err := os.ReadDir(refsDir)
	if err == nil && len(entries) > 0 {
		fmt.Printf("Corpus already provisioned. Delete testdata/%s to force regeneration.\n", name)
		return nil
	}

	dockerCtx := findDockerContext(name)
	if dockerCtx == "" {
		fmt.Fprintf(os.Stderr, "Cannot locate corpus/%s/Dockerfile.\n", name)
		fmt.Fprintln(os.Stderr, "Provision manually:")
		fmt.Fprintf(os.Stderr, "  cd test/integration && docker build -t %s corpus/%s && docker run --rm -v $(pwd)/testdata/%s:/output %s\n",
			imageName, name, name, imageName)
		return fmt.Errorf("docker context not found")
	}

	absOutput, err := filepath.Abs(corpusPath)
	if err != nil {
		return fmt.Errorf("resolving corpus path: %w", err)
	}

	if err := os.MkdirAll(absOutput, 0o755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	fmt.Println("Building Docker image...")
	build := exec.Command("docker", "build", "-t", imageName, dockerCtx)
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}

	fmt.Println("Running corpus generation container...")
	run := exec.Command("docker", "run", "--rm", //nolint:gosec // paths come from CLI flags, not untrusted input
		"-v", absOutput+":/output",
		imageName,
	)
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr
	if err := run.Run(); err != nil {
		return fmt.Errorf("docker run: %w", err)
	}

	fmt.Println("Corpus provisioned successfully.")
	return nil
}

// findDockerContext returns the path to the corpus Docker build context.
func findDockerContext(name string) string {
	candidates := []string{
		filepath.Join("test", "integration", "corpus", name),
		filepath.Join("corpus", name),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "Dockerfile")); err == nil {
			return c
		}
	}
	return ""
}
