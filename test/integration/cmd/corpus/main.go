// Corpus testing CLI. Compares litho's compliance verdicts against VeraPDF
// and PDF.js reference results.
//
// Usage:
//
//	go run ./test/integration/cmd/corpus verapdf [--init] [--format text|json] [--profile PDF_A-4] [--max-fn 3] [--max-fp 0]
//	go run ./test/integration/cmd/corpus pdfjs   [--init] [--format text|json] [--max-text-errors 50]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// perFileTimeout is the maximum time for processing a single PDF. Anything
// beyond 5s indicates a pathological file or a parser bug.
const perFileTimeout = 5 * time.Second

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "verapdf":
		os.Exit(runVeraPDF(os.Args[2:]))
	case "pdfjs":
		runPDFJS(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: corpus <verapdf|pdfjs> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Subcommands:")
	fmt.Fprintln(os.Stderr, "  verapdf   Compare litho against VeraPDF corpus")
	fmt.Fprintln(os.Stderr, "  pdfjs     Compare litho against PDF.js corpus")
}

// VeraPDF JSON reference types.

type verapdfReport struct {
	Report struct {
		Jobs []verapdfJob `json:"jobs"`
	} `json:"report"`
}

type verapdfJob struct {
	ItemDetails struct {
		Name string `json:"name"`
	} `json:"itemDetails"`
	ValidationResult []verapdfValidationResult `json:"validationResult"`
}

type verapdfValidationResult struct {
	ProfileName string `json:"profileName"`
	Compliant   bool   `json:"compliant"`
	Details     struct {
		PassedRules   int           `json:"passedRules"`
		FailedRules   int           `json:"failedRules"`
		RuleSummaries []verapdfRule `json:"ruleSummaries"`
	} `json:"details"`
}

type verapdfRule struct {
	Specification string `json:"specification"`
	Clause        string `json:"clause"`
	TestNumber    int    `json:"testNumber"`
	Status        string `json:"status"`
	Description   string `json:"description"`
}

var dirToPDFAProfile = map[string]models.PDFAProfile{
	"PDF_A-1a": models.PDFA1a,
	"PDF_A-1b": models.PDFA1b,
	"PDF_A-2a": models.PDFA2a,
	"PDF_A-2b": models.PDFA2b,
	"PDF_A-2u": models.PDFA2u,
	"PDF_A-3a": models.PDFA3a,
	"PDF_A-3b": models.PDFA3b,
	"PDF_A-3u": models.PDFA3u,
	"PDF_A-4":  models.PDFA4,
	"PDF_A-4e": models.PDFA4e,
	"PDF_A-4f": models.PDFA4f,
}

var dirToPDFUAProfile = map[string]models.PDFUAProfile{
	"PDF_UA-1": models.PDFUA1,
	"PDF_UA-2": models.PDFUA2,
}

type ruleInfo struct {
	Clause      string `json:"clause"`
	TestNumber  int    `json:"test_number"`
	Description string `json:"description"`
}

type disagreement struct {
	File            string     `json:"file"`
	Profile         string     `json:"profile"`
	VerapdfRules    []ruleInfo `json:"verapdf_rules"`
	LithoViolations []string   `json:"litho_violations"`
}

type summary struct {
	Total          int `json:"total"`
	Agree          int `json:"agree"`
	FalseNegatives int `json:"false_negatives"`
	FalsePositives int `json:"false_positives"`
	Skipped        int `json:"skipped"`
}

type report struct {
	Summary        summary        `json:"summary"`
	Elapsed        string         `json:"elapsed"`
	FalseNegatives []disagreement `json:"false_negatives"`
	FalsePositives []disagreement `json:"false_positives"`
}

// --- VeraPDF ---

func runVeraPDF(args []string) int {
	fs := flag.NewFlagSet("verapdf", flag.ExitOnError)
	init := fs.Bool("init", false, "Provision the corpus via Docker before running")
	format := fs.String("format", "text", "Output format: text or json")
	maxFN := fs.Int("max-fn", -1, "Maximum false negatives (-1 = no limit)")
	maxFP := fs.Int("max-fp", 0, "Maximum false positives")
	profile := fs.String("profile", "", "Filter to a single profile directory (e.g. PDF_A-4)")
	corpus := fs.String("corpus", "", "Path to corpus testdata (auto-detected if empty)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}

	corpusPath := *corpus
	if corpusPath == "" {
		corpusPath = findCorpusPath()
	}

	if *init {
		if err := provisionVeraPDF(corpusPath); err != nil {
			fmt.Fprintf(os.Stderr, "provisioning failed: %v\n", err)
			return 1
		}
	}

	refsDir := filepath.Join(corpusPath, "references")
	refs, err := filepath.Glob(filepath.Join(refsDir, "*.json"))
	if err != nil || len(refs) == 0 {
		fmt.Fprintln(os.Stderr, "No VeraPDF reference files found.")
		fmt.Fprintln(os.Stderr, "Run with --init to provision the corpus, or check --corpus path.")
		return 1
	}

	showProgress := *format != "json"
	start := time.Now()
	rpt := compareVeraPDF(corpusPath, refs, *profile, showProgress)
	rpt.Elapsed = time.Since(start).Round(time.Millisecond).String()

	switch *format {
	case "json":
		printJSON(rpt)
	default:
		printText(rpt, *profile)
	}

	return checkThresholds(rpt, *maxFN, *maxFP)
}

// findCorpusPath locates the VeraPDF testdata directory.
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

// provisionVeraPDF provisions the VeraPDF corpus via Docker.
func provisionVeraPDF(corpusPath string) error {
	return provisionCorpus(corpusPath, "verapdf", "litho-verapdf-corpus")
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

// compareVeraPDF loads all reference files and compares litho's verdicts.
func compareVeraPDF(corpusPath string, refs []string, filterProfile string, showProgress bool) report {
	var rpt report
	totalJobs := 0
	if showProgress {
		for _, refPath := range refs {
			profileDir := strings.TrimSuffix(filepath.Base(refPath), ".json")
			if filterProfile != "" && profileDir != filterProfile {
				continue
			}
			data, err := os.ReadFile(refPath)
			if err != nil {
				continue
			}
			var vr verapdfReport
			if err := json.Unmarshal(data, &vr); err == nil {
				totalJobs += len(vr.Report.Jobs)
			}
		}
	}

	processed := 0
	for _, refPath := range refs {
		profileDir := strings.TrimSuffix(filepath.Base(refPath), ".json")
		if filterProfile != "" && profileDir != filterProfile {
			continue
		}

		data, err := os.ReadFile(refPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "reading %s: %v\n", refPath, err)
			continue
		}

		var vReport verapdfReport
		if err := json.Unmarshal(data, &vReport); err != nil {
			fmt.Fprintf(os.Stderr, "parsing %s: %v\n", refPath, err)
			continue
		}

		for _, job := range vReport.Report.Jobs {
			processed++
			if showProgress && processed%50 == 0 {
				fmt.Fprintf(os.Stderr, "\r  processed %d/%d", processed, totalJobs)
			}

			localPath := pdfPathFromJobName(job.ItemDetails.Name, corpusPath)
			baseName := filepath.Base(localPath)

			rpt.Summary.Total++

			expected, ok := expectedCompliantFromFilename(baseName)
			if !ok {
				rpt.Summary.Skipped++
				continue
			}

			if len(job.ValidationResult) == 0 {
				rpt.Summary.Skipped++
				continue
			}

			lithoCompliant, lithoViolations := validateWithLitho(localPath, profileDir)

			if lithoCompliant == expected {
				rpt.Summary.Agree++
				continue
			}

			relPath := profileDir + "/" + baseName
			vResult := job.ValidationResult[0]

			if expected && !lithoCompliant {
				// False positive: litho flags a file that should pass.
				rpt.Summary.FalsePositives++
				rpt.FalsePositives = append(rpt.FalsePositives, disagreement{
					File:            relPath,
					Profile:         profileDir,
					VerapdfRules:    failedRules(vResult),
					LithoViolations: lithoViolations,
				})
			} else {
				// False negative: litho passes a file that should fail.
				rpt.Summary.FalseNegatives++
				rpt.FalseNegatives = append(rpt.FalseNegatives, disagreement{
					File:         relPath,
					Profile:      profileDir,
					VerapdfRules: failedRules(vResult),
				})
			}
		}
	}

	if showProgress {
		fmt.Fprintf(os.Stderr, "\r  processed %d/%d\n", processed, totalJobs)
	}

	sort.Slice(rpt.FalseNegatives, func(i, j int) bool {
		return rpt.FalseNegatives[i].File < rpt.FalseNegatives[j].File
	})
	sort.Slice(rpt.FalsePositives, func(i, j int) bool {
		return rpt.FalsePositives[i].File < rpt.FalsePositives[j].File
	})

	return rpt
}

// validateWithLitho opens a PDF and runs the appropriate compliance check.
func validateWithLitho(path, profileDir string) (compliant bool, violations []string) {
	f, err := os.Open(path)
	if err != nil {
		// Cannot open: treat as non-compliant (litho would reject it).
		return false, []string{fmt.Sprintf("open error: %v", err)}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return false, []string{fmt.Sprintf("stat error: %v", err)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), perFileTimeout)
	defer cancel()

	reader, err := pdf.Open(ctx, f, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return false, []string{fmt.Sprintf("open error: %v", err)}
	}

	var errs []error
	if pdfaProfile, ok := dirToPDFAProfile[profileDir]; ok {
		errs, err = reader.ValidatePDFA(ctx, pdfaProfile)
	} else if pdfuaProfile, ok := dirToPDFUAProfile[profileDir]; ok {
		errs, err = reader.ValidatePDFUA(ctx, pdfuaProfile)
	} else {
		return true, nil
	}

	if err != nil {
		return false, []string{fmt.Sprintf("validation error: %v", err)}
	}

	if len(errs) == 0 {
		return true, nil
	}

	strs := make([]string, len(errs))
	for i, e := range errs {
		strs[i] = e.Error()
	}
	return false, strs
}

// expectedCompliantFromFilename derives the expected result from VeraPDF
// filename convention: files contain "-pass-" or "-fail-".
func expectedCompliantFromFilename(name string) (expected bool, ok bool) {
	if strings.Contains(name, "-pass-") {
		return true, true
	}
	if strings.Contains(name, "-fail-") {
		return false, true
	}
	return false, false
}

// pdfPathFromJobName converts a container-absolute path to a local corpus path.
func pdfPathFromJobName(name, corpusPath string) string {
	const containerPrefix = "/output/"
	if _, after, ok := strings.Cut(name, containerPrefix); ok {
		return filepath.Join(corpusPath, after)
	}
	return name
}

// failedRules extracts failed rule summaries from a VeraPDF result.
func failedRules(vr verapdfValidationResult) []ruleInfo {
	var rules []ruleInfo
	for _, r := range vr.Details.RuleSummaries {
		if r.Status == "failed" {
			rules = append(rules, ruleInfo{
				Clause:      r.Clause,
				TestNumber:  r.TestNumber,
				Description: r.Description,
			})
		}
	}
	return rules
}

func printText(rpt report, filterProfile string) {
	fmt.Println("VeraPDF Corpus Comparison")
	fmt.Println("========================")

	profileLabel := "all"
	if filterProfile != "" {
		profileLabel = filterProfile
	}
	fmt.Printf("Profile: %s\n", profileLabel)
	fmt.Printf("Total: %d  Agree: %d  FN: %d  FP: %d  Skipped: %d  Elapsed: %s\n\n",
		rpt.Summary.Total, rpt.Summary.Agree,
		rpt.Summary.FalseNegatives, rpt.Summary.FalsePositives,
		rpt.Summary.Skipped, rpt.Elapsed)

	if len(rpt.FalseNegatives) > 0 {
		fmt.Printf("False Negatives (%d):\n", len(rpt.FalseNegatives))
		for _, d := range rpt.FalseNegatives {
			fmt.Printf("  %s\n", d.File)
			if len(d.VerapdfRules) > 0 {
				for _, r := range d.VerapdfRules {
					fmt.Printf("    VeraPDF: clause %s test %d: %s\n", r.Clause, r.TestNumber, r.Description)
				}
			} else {
				fmt.Println("    VeraPDF: compliant (corpus anomaly)")
			}
			fmt.Println("    Litho: compliant")
			fmt.Println()
		}
	}

	if len(rpt.FalsePositives) > 0 {
		fmt.Printf("False Positives (%d):\n", len(rpt.FalsePositives))
		for _, d := range rpt.FalsePositives {
			fmt.Printf("  %s\n", d.File)
			fmt.Println("    VeraPDF: compliant")
			for _, v := range d.LithoViolations {
				fmt.Printf("    Litho: %s\n", v)
			}
			fmt.Println()
		}
	}
}

func printJSON(rpt report) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rpt); err != nil {
		fmt.Fprintf(os.Stderr, "encoding JSON: %v\n", err)
	}
}

// checkThresholds returns 0 if the report is within limits, 1 otherwise.
func checkThresholds(rpt report, maxFN, maxFP int) int {
	exceeded := false
	if maxFN >= 0 && rpt.Summary.FalseNegatives > maxFN {
		fmt.Fprintf(os.Stderr, "FAIL: false negatives %d > max %d\n", rpt.Summary.FalseNegatives, maxFN)
		exceeded = true
	}
	if maxFP >= 0 && rpt.Summary.FalsePositives > maxFP {
		fmt.Fprintf(os.Stderr, "FAIL: false positives %d > max %d\n", rpt.Summary.FalsePositives, maxFP)
		exceeded = true
	}
	if exceeded {
		return 1
	}
	return 0
}

// --- PDF.js ---

type manifestEntry struct {
	ID       string `json:"id"`
	File     string `json:"file"`
	MD5      string `json:"md5"`
	Type     string `json:"type"`
	LastPage int    `json:"lastPage"`
	Link     any    `json:"link"`
}

type referenceFixture struct {
	ID       string          `json:"id"`
	File     string          `json:"file"`
	NumPages int             `json:"numPages"`
	Metadata refMetadata     `json:"metadata"`
	Outline  json.RawMessage `json:"outline"`
	Pages    []refPage       `json:"pages"`
}

type refMetadata struct {
	Info refInfo `json:"info"`
}

type refInfo struct {
	PDFFormatVersion    string `json:"PDFFormatVersion"`
	Language            string `json:"Language"`
	IsLinearized        bool   `json:"IsLinearized"`
	IsAcroFormPresent   bool   `json:"IsAcroFormPresent"`
	IsXFAPresent        bool   `json:"IsXFAPresent"`
	IsCollectionPresent bool   `json:"IsCollectionPresent"`
	IsSignaturesPresent bool   `json:"IsSignaturesPresent"`
	Author              string `json:"Author"`
	Title               string `json:"Title"`
	Creator             string `json:"Creator"`
	Producer            string `json:"Producer"`
	CreationDate        string `json:"CreationDate"`
	ModDate             string `json:"ModDate"`
	Keywords            string `json:"Keywords"`
	EncryptFilterName   any    `json:"EncryptFilterName"`
}

type refPage struct {
	PageNumber int       `json:"pageNumber"`
	Text       string    `json:"text"`
	ItemCount  int       `json:"itemCount"`
	Items      []refItem `json:"items"`
	Error      string    `json:"error,omitempty"`
}

type refItem struct {
	Str      string  `json:"str"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	FontName string  `json:"fontName"`
}

var pdfjsKnownSkips = map[string]string{
	"bug1393476": "infinite loop in xref parsing",
}

func shouldSkipPDFJS(id string) (string, bool) {
	if reason, ok := pdfjsKnownSkips[id]; ok {
		return reason, true
	}
	return "", false
}

type pdfjsSummary struct {
	Total           int `json:"total"`
	Survived        int `json:"survived"`
	Panics          int `json:"panics"`
	WithReferences  int `json:"with_references"`
	SurvivalOnly    int `json:"survival_only"`
	TextMatches     int `json:"text_matches"`
	TextMismatches  int `json:"text_mismatches"`
	MetadataMatches int `json:"metadata_matches"`
	Errors          int `json:"errors"`
	Skipped         int `json:"skipped"`
	Timeouts        int `json:"timeouts"`
}

type pdfjsTextMismatch struct {
	File     string `json:"file"`
	Page     int    `json:"page"`
	Position int    `json:"position"`
	Expected string `json:"expected"`
	Got      string `json:"got"`
}

type pdfjsMetadataMismatch struct {
	File  string `json:"file"`
	Field string `json:"field"`
	Litho string `json:"litho"`
	PDFJS string `json:"pdfjs"`
}

type pdfjsReport struct {
	Summary            pdfjsSummary            `json:"summary"`
	Elapsed            string                  `json:"elapsed"`
	TextMismatches     []pdfjsTextMismatch     `json:"text_mismatches"`
	MetadataMismatches []pdfjsMetadataMismatch `json:"metadata_mismatches"`
}

// pdfjsResult holds the outcome of processing a single manifest entry.
type pdfjsResult struct {
	ID                 string
	HasRef             bool
	Skipped            bool
	SkipReason         string
	Error              string
	Timeout            bool
	Panicked           bool
	TextMatch          bool
	MetadataMatch      bool
	TextMismatches     []pdfjsTextMismatch
	MetadataMismatches []pdfjsMetadataMismatch
}

func runPDFJS(args []string) {
	fs := flag.NewFlagSet("pdfjs", flag.ExitOnError)
	init := fs.Bool("init", false, "Provision the corpus via Docker before running")
	format := fs.String("format", "text", "Output format: text or json")
	maxTextErrors := fs.Int("max-text-errors", -1, "Maximum text mismatch percentage (-1 = no limit)")
	corpus := fs.String("corpus", "", "Path to pdf.js corpus testdata (auto-detected if empty)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		os.Exit(1)
	}

	corpusPath := *corpus
	if corpusPath == "" {
		corpusPath = findPDFJSCorpusPath()
	}

	if *init {
		if err := provisionPDFJS(corpusPath); err != nil {
			fmt.Fprintf(os.Stderr, "provisioning failed: %v\n", err)
			os.Exit(1)
		}
	}

	manifestPath := filepath.Join(corpusPath, "test_manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "PDF.js corpus not found.")
		fmt.Fprintln(os.Stderr, "Run with --init to provision the corpus, or check --corpus path.")
		os.Exit(1)
	}

	showProgress := *format != "json"
	start := time.Now()
	rpt := comparePDFJS(corpusPath, showProgress)
	rpt.Elapsed = time.Since(start).Round(time.Millisecond).String()

	switch *format {
	case "json":
		printPDFJSJSON(rpt)
	default:
		printPDFJSText(rpt)
	}

	if *maxTextErrors >= 0 && rpt.Summary.WithReferences > 0 {
		pct := float64(rpt.Summary.TextMismatches) / float64(rpt.Summary.WithReferences) * 100
		if pct > float64(*maxTextErrors) {
			fmt.Fprintf(os.Stderr, "FAIL: text mismatch %.1f%% > max %d%%\n", pct, *maxTextErrors)
			os.Exit(1)
		}
	}
}

// findPDFJSCorpusPath locates the pdf.js testdata directory.
func findPDFJSCorpusPath() string {
	candidates := []string{
		"test/integration/testdata/pdf.js",
		"testdata/pdf.js",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return "test/integration/testdata/pdf.js"
}

// provisionPDFJS provisions the PDF.js corpus via Docker.
func provisionPDFJS(corpusPath string) error {
	return provisionCorpus(corpusPath, "pdf.js", "litho-pdfjs-corpus")
}

func comparePDFJS(corpusPath string, showProgress bool) pdfjsReport {
	manifestPath := filepath.Join(corpusPath, "test_manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading manifest: %v\n", err)
		return pdfjsReport{}
	}

	var entries []manifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		fmt.Fprintf(os.Stderr, "parsing manifest: %v\n", err)
		return pdfjsReport{}
	}

	entries = dedupManifest(entries)
	refsDir := filepath.Join(corpusPath, "references")

	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers < 1 {
		numWorkers = 4
	}

	type workItem struct {
		entry  manifestEntry
		hasRef bool
	}

	work := make(chan workItem, len(entries))
	results := make(chan pdfjsResult, len(entries))

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Go(func() {
			for item := range work {
				results <- processPDFJSEntry(corpusPath, refsDir, item.entry, item.hasRef)
			}
		})
	}

	for _, entry := range entries {
		refPath := filepath.Join(refsDir, entry.ID+".json")
		_, refErr := os.Stat(refPath)
		work <- workItem{entry: entry, hasRef: refErr == nil}
	}
	close(work)

	go func() {
		wg.Wait()
		close(results)
	}()

	var rpt pdfjsReport
	processed := 0
	for res := range results {
		processed++
		if showProgress && processed%50 == 0 {
			fmt.Fprintf(os.Stderr, "\r  processed %d/%d", processed, len(entries))
		}

		rpt.Summary.Total++
		if res.Panicked {
			rpt.Summary.Panics++
			continue
		}
		if res.Skipped {
			rpt.Summary.Skipped++
			continue
		}
		if res.Timeout {
			rpt.Summary.Timeouts++
			continue
		}
		if res.Error != "" {
			rpt.Summary.Errors++
			continue
		}

		if res.HasRef {
			rpt.Summary.WithReferences++
			if res.TextMatch {
				rpt.Summary.TextMatches++
			} else {
				rpt.Summary.TextMismatches++
				rpt.TextMismatches = append(rpt.TextMismatches, res.TextMismatches...)
			}
			if res.MetadataMatch {
				rpt.Summary.MetadataMatches++
			} else {
				rpt.MetadataMismatches = append(rpt.MetadataMismatches, res.MetadataMismatches...)
			}
		} else {
			rpt.Summary.SurvivalOnly++
		}
	}
	if showProgress {
		fmt.Fprintf(os.Stderr, "\r  processed %d/%d\n", processed, len(entries))
	}

	rpt.Summary.Survived = rpt.Summary.Total - rpt.Summary.Panics

	sort.Slice(rpt.TextMismatches, func(i, j int) bool {
		if rpt.TextMismatches[i].File != rpt.TextMismatches[j].File {
			return rpt.TextMismatches[i].File < rpt.TextMismatches[j].File
		}
		return rpt.TextMismatches[i].Page < rpt.TextMismatches[j].Page
	})
	sort.Slice(rpt.MetadataMismatches, func(i, j int) bool {
		return rpt.MetadataMismatches[i].File < rpt.MetadataMismatches[j].File
	})

	return rpt
}

// processPDFJSEntry handles a single manifest entry.
func processPDFJSEntry(corpusPath, refsDir string, entry manifestEntry, hasRef bool) (res pdfjsResult) {
	res = pdfjsResult{ID: entry.ID, HasRef: hasRef}

	defer func() {
		if r := recover(); r != nil {
			res.Panicked = true
		}
	}()

	if reason, skip := shouldSkipPDFJS(entry.ID); skip {
		res.Skipped = true
		res.SkipReason = reason
		return res
	}

	pdfPath := filepath.Join(corpusPath, entry.File)
	f, err := os.Open(pdfPath)
	if err != nil {
		res.Error = fmt.Sprintf("open: %v", err)
		return res
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		res.Error = fmt.Sprintf("stat: %v", err)
		return res
	}

	ctx, cancel := context.WithTimeout(context.Background(), perFileTimeout)
	defer cancel()

	reader, err := pdf.Open(ctx, f, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		res.Error = fmt.Sprintf("open: %v", err)
		return res
	}

	if hasRef {
		refPath := filepath.Join(refsDir, entry.ID+".json")
		refData, err := os.ReadFile(refPath)
		if err != nil {
			res.Error = fmt.Sprintf("reading reference: %v", err)
			return res
		}
		var pdfjsRef referenceFixture
		if err := json.Unmarshal(refData, &pdfjsRef); err != nil {
			res.Error = fmt.Sprintf("parsing reference: %v", err)
			return res
		}

		lithoRef := lithoReference(ctx, reader, entry.ID, entry.LastPage)
		if lithoRef == nil {
			res.Error = "litho reference generation failed"
			return res
		}

		if ctx.Err() != nil {
			res.Timeout = true
			return res
		}

		res.TextMatch, res.MetadataMatch, res.TextMismatches, res.MetadataMismatches = compareFixtures(entry.File, &pdfjsRef, lithoRef)
	} else {
		// Survival test: open, count pages, extract text per page.
		numPages, err := reader.NumPages(ctx)
		if err != nil {
			res.Error = fmt.Sprintf("NumPages: %v", err)
			return res
		}
		lastPage := numPages
		if entry.LastPage > 0 && entry.LastPage < lastPage {
			lastPage = entry.LastPage
		}
		for i := 1; i <= lastPage; i++ {
			if ctx.Err() != nil {
				res.Timeout = true
				return res
			}
			if _, err := reader.ExtractText(ctx, i); err != nil {
				break
			}
		}
	}

	return res
}

// lithoReference builds a referenceFixture from a litho Reader.
func lithoReference(ctx context.Context, r *pdf.Reader, id string, lastPage int) *referenceFixture {
	fixture := &referenceFixture{ID: id}

	numPages, err := r.NumPages(ctx)
	if err != nil {
		return nil
	}
	fixture.NumPages = numPages

	fixture.Metadata.Info.PDFFormatVersion = r.Version()
	fixture.Metadata.Info.IsLinearized = r.IsLinearized()

	if meta, err := r.GetMetadata(ctx); err == nil {
		fixture.Metadata.Info.Author = meta["Author"]
		fixture.Metadata.Info.Title = meta["Title"]
		fixture.Metadata.Info.Creator = meta["Creator"]
		fixture.Metadata.Info.Producer = meta["Producer"]
		fixture.Metadata.Info.CreationDate = meta["CreationDate"]
		fixture.Metadata.Info.ModDate = meta["ModDate"]
		fixture.Metadata.Info.Keywords = meta["Keywords"]
		fixture.Metadata.Info.Language = meta["Language"]
	}

	if formData, err := r.GetFormData(ctx); err == nil {
		fixture.Metadata.Info.IsAcroFormPresent = len(formData) > 0
	}

	if isPortfolio, err := r.IsPortfolio(ctx); err == nil {
		fixture.Metadata.Info.IsCollectionPresent = isPortfolio
	}

	effectiveLastPage := numPages
	if lastPage > 0 && lastPage < effectiveLastPage {
		effectiveLastPage = lastPage
	}

	for i := 1; i <= effectiveLastPage; i++ {
		if ctx.Err() != nil {
			return fixture
		}

		page := refPage{PageNumber: i}

		text, err := r.ExtractText(ctx, i, pdf.WithHiddenText())
		if err != nil {
			page.Error = err.Error()
			fixture.Pages = append(fixture.Pages, page)
			continue
		}
		page.Text = text

		elems, err := r.ExtractTextElements(ctx, i)
		if err != nil {
			page.Error = err.Error()
			fixture.Pages = append(fixture.Pages, page)
			continue
		}

		page.Items = groupGlyphsIntoRuns(elems)
		page.ItemCount = len(page.Items)
		fixture.Pages = append(fixture.Pages, page)
	}

	return fixture
}

// groupGlyphsIntoRuns groups consecutive per-glyph TextElements into text runs
// matching the PDF.js item format. Starts a new run when the Y coordinate changes.
func groupGlyphsIntoRuns(elems []models.TextElement) []refItem {
	if len(elems) == 0 {
		return nil
	}

	var runs []refItem
	current := refItem{
		Str:    elems[0].Text,
		X:      elems[0].X,
		Y:      elems[0].Y,
		Width:  elems[0].Width,
		Height: elems[0].FontSize,
	}

	for i := 1; i < len(elems); i++ {
		e := elems[i]
		if math.Abs(e.Y-current.Y) < 0.5 {
			current.Str += e.Text
			current.Width = (e.X + e.Width) - current.X
		} else {
			runs = append(runs, current)
			current = refItem{
				Str:    e.Text,
				X:      e.X,
				Y:      e.Y,
				Width:  e.Width,
				Height: e.FontSize,
			}
		}
	}
	runs = append(runs, current)

	return runs
}

// compareFixtures compares litho's output against the PDF.js reference.
func compareFixtures(file string, pdfjs *referenceFixture, litho *referenceFixture) (textMatch, metaMatch bool, textMM []pdfjsTextMismatch, metaMM []pdfjsMetadataMismatch) {
	textMatch = true
	metaMatch = true

	metaMM = compareMetadata(file, pdfjs.Metadata.Info, litho.Metadata.Info)
	if len(metaMM) > 0 {
		metaMatch = false
	}

	for _, pdfjsPage := range pdfjs.Pages {
		if pdfjsPage.Error != "" {
			continue
		}

		var lithoPage *refPage
		for j := range litho.Pages {
			if litho.Pages[j].PageNumber == pdfjsPage.PageNumber {
				lithoPage = &litho.Pages[j]
				break
			}
		}

		if lithoPage == nil {
			textMatch = false
			textMM = append(textMM, pdfjsTextMismatch{
				File:     file,
				Page:     pdfjsPage.PageNumber,
				Position: 0,
				Expected: truncate(pdfjsPage.Text, 80),
				Got:      "(no output from litho)",
			})
			continue
		}

		if lithoPage.Error != "" {
			if strings.TrimSpace(pdfjsPage.Text) == "" {
				continue
			}
			textMatch = false
			textMM = append(textMM, pdfjsTextMismatch{
				File:     file,
				Page:     pdfjsPage.PageNumber,
				Position: 0,
				Expected: truncate(pdfjsPage.Text, 80),
				Got:      fmt.Sprintf("(error: %s)", lithoPage.Error),
			})
			continue
		}

		if mm := comparePageText(file, pdfjsPage.PageNumber, pdfjsPage.Text, lithoPage.Text); mm != nil {
			textMatch = false
			textMM = append(textMM, *mm)
		}
	}

	return textMatch, metaMatch, textMM, metaMM
}

// compareMetadata compares metadata fields and returns mismatches.
func compareMetadata(file string, pdfjs, litho refInfo) []pdfjsMetadataMismatch {
	var mm []pdfjsMetadataMismatch

	if litho.PDFFormatVersion != pdfjs.PDFFormatVersion && pdfjs.PDFFormatVersion != "" {
		mm = append(mm, pdfjsMetadataMismatch{
			File:  file,
			Field: "PDFFormatVersion",
			Litho: litho.PDFFormatVersion,
			PDFJS: pdfjs.PDFFormatVersion,
		})
	}
	if litho.IsLinearized != pdfjs.IsLinearized {
		mm = append(mm, pdfjsMetadataMismatch{
			File:  file,
			Field: "IsLinearized",
			Litho: fmt.Sprintf("%v", litho.IsLinearized),
			PDFJS: fmt.Sprintf("%v", pdfjs.IsLinearized),
		})
	}

	return mm
}

// comparePageText normalizes and compares text for a single PDF page.
func comparePageText(file string, pageNum int, pdfjsText, lithoText string) *pdfjsTextMismatch {
	pdfjsNorm := normalizeWhitespace(pdfjsText)
	lithoNorm := normalizeWhitespace(lithoText)

	if lithoNorm == pdfjsNorm {
		return nil
	}

	pos, expected, got := textDiffSummary(pdfjsNorm, lithoNorm, 80)
	return &pdfjsTextMismatch{
		File:     file,
		Page:     pageNum,
		Position: pos,
		Expected: expected,
		Got:      got,
	}
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// textDiffSummary finds the first differing position and returns context snippets.
func textDiffSummary(expected, got string, maxLen int) (pos int, expSnippet, gotSnippet string) {
	minLen := min(len(got), len(expected))
	pos = minLen
	for i := range minLen {
		if expected[i] != got[i] {
			pos = i
			break
		}
	}

	start := max(pos-40, 0)
	endExp := min(pos+maxLen, len(expected))
	endGot := min(pos+maxLen, len(got))

	expSnippet = "..." + expected[start:endExp] + "..."
	gotSnippet = "..." + got[start:endGot] + "..."

	if len(got) == 0 {
		gotSnippet = "(empty)"
	}

	return pos, expSnippet, gotSnippet
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func dedupManifest(entries []manifestEntry) []manifestEntry {
	seen := make(map[string]bool)
	var unique []manifestEntry
	for _, e := range entries {
		if seen[e.File] {
			continue
		}
		seen[e.File] = true
		unique = append(unique, e)
	}
	return unique
}

func printPDFJSText(rpt pdfjsReport) {
	fmt.Println("PDF.js Corpus Comparison")
	fmt.Println("========================")

	s := rpt.Summary
	survivalPct := 0.0
	if s.Total > 0 {
		survivalPct = float64(s.Survived) / float64(s.Total) * 100
	}
	fmt.Printf("Total: %d  With refs: %d  Survival only: %d\n", s.Total, s.WithReferences, s.SurvivalOnly)
	fmt.Printf("Survival: %d/%d (%.1f%%)  Panics: %d\n", s.Survived, s.Total, survivalPct, s.Panics)

	textPct := 0.0
	metaPct := 0.0
	if s.WithReferences > 0 {
		textPct = float64(s.TextMatches) / float64(s.WithReferences) * 100
		metaPct = float64(s.MetadataMatches) / float64(s.WithReferences) * 100
	}
	fmt.Printf("Text match: %d/%d (%.1f%%)  Metadata match: %d/%d (%.1f%%)\n",
		s.TextMatches, s.WithReferences, textPct,
		s.MetadataMatches, s.WithReferences, metaPct)
	fmt.Printf("Errors: %d  Skipped: %d  Timeouts: %d  Elapsed: %s\n\n", s.Errors, s.Skipped, s.Timeouts, rpt.Elapsed)

	if len(rpt.TextMismatches) > 0 {
		fmt.Printf("Text Mismatches (%d):\n", len(rpt.TextMismatches))
		for _, m := range rpt.TextMismatches {
			fmt.Printf("  %s (page %d): first difference at position %d\n", m.File, m.Page, m.Position)
			fmt.Printf("    expected: %s\n", m.Expected)
			fmt.Printf("    got:      %s\n\n", m.Got)
		}
	}

	if len(rpt.MetadataMismatches) > 0 {
		fmt.Printf("Metadata Mismatches (%d):\n", len(rpt.MetadataMismatches))
		for _, m := range rpt.MetadataMismatches {
			fmt.Printf("  %s: %s litho=%q pdfjs=%q\n", m.File, m.Field, m.Litho, m.PDFJS)
		}
	}
}

func printPDFJSJSON(rpt pdfjsReport) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rpt); err != nil {
		fmt.Fprintf(os.Stderr, "encoding JSON: %v\n", err)
	}
}
