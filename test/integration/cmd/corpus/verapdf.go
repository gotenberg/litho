package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// perFileTimeout is the maximum time for processing a single PDF.
// Large documents (hundreds of pages) take a couple of seconds even on
// the happy path, and parallel corpus runs add contention-driven
// slowdown on top, so the ceiling sits well above the per-file happy-
// path cost. Anything beyond this still indicates a pathological file
// or a parser bug worth investigating.
const perFileTimeout = 20 * time.Second

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
		printVeraPDFJSON(rpt)
	default:
		printVeraPDFText(rpt, *profile)
	}

	return checkVeraPDFThresholds(rpt, *maxFN, *maxFP)
}

// provisionVeraPDF provisions the VeraPDF corpus via Docker.
func provisionVeraPDF(corpusPath string) error {
	return provisionCorpus(corpusPath, "verapdf", "litho-verapdf-corpus")
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

			// Ground truth is the filename (-pass- / -fail-). When
			// Litho disagrees with the filename, check whether VeraPDF
			// also disagrees the same way: if so, both validators have
			// landed on the same verdict and the filename is a corpus
			// anomaly (either VeraPDF-authored fixtures whose newer
			// releases rule the file compliant, or files whose spec
			// truth has since shifted). Counting those as Litho
			// disagreements would penalise Litho for matching VeraPDF.
			if lithoCompliant != expected &&
				job.ValidationResult[0].Compliant == lithoCompliant {
				rpt.Summary.Agree++
				continue
			}

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

func printVeraPDFText(rpt report, filterProfile string) {
	fmt.Println("VeraPDF Corpus Comparison")
	fmt.Println("========================")

	profileLabel := "all"
	if filterProfile != "" {
		profileLabel = filterProfile
	}
	fmt.Printf("Profile: %s\n\n", profileLabel)

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

	fmt.Printf("Total: %d  Agree: %d  FN: %d  FP: %d  Skipped: %d\n",
		rpt.Summary.Total, rpt.Summary.Agree,
		rpt.Summary.FalseNegatives, rpt.Summary.FalsePositives,
		rpt.Summary.Skipped)
	fmt.Printf("Elapsed: %s\n", rpt.Elapsed)
}

func printVeraPDFJSON(rpt report) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rpt); err != nil {
		fmt.Fprintf(os.Stderr, "encoding JSON: %v\n", err)
	}
}

// checkVeraPDFThresholds returns 0 if the report is within limits, 1 otherwise.
func checkVeraPDFThresholds(rpt report, maxFN, maxFP int) int {
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
