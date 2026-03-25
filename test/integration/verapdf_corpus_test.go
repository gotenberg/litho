//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

const (
	verapdfCorpusDir     = "testdata/verapdf"
	verapdfReferencesDir = "testdata/verapdf/references"
)

// perVeraPDFSubtestTimeout is the maximum time for a single PDF validation.
const perVeraPDFSubtestTimeout = 30 * time.Second

// --- VeraPDF JSON output types ---

// verapdfReport is the top-level JSON structure produced by `verapdf --format json`.
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

// --- Profile mappings ---

// dirToPDFAProfile maps corpus directory names to litho PDF/A profiles.
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

// dirToPDFUAProfile maps corpus directory names to litho PDF/UA profiles.
var dirToPDFUAProfile = map[string]models.PDFUAProfile{
	"PDF_UA-1": models.PDFUA1,
	"PDF_UA-2": models.PDFUA2,
}

// --- Helpers ---

// expectedCompliantFromFilename derives the expected compliance result from
// the veraPDF corpus filename convention: files contain "-pass-" or "-fail-".
func expectedCompliantFromFilename(name string) (expected bool, ok bool) {
	if strings.Contains(name, "-pass-") {
		return true, true
	}
	if strings.Contains(name, "-fail-") {
		return false, true
	}
	return false, false
}

// pdfPathFromJobName converts the container-absolute path in the veraPDF
// output back to a local testdata path.
// Container paths look like: /output/PDF_A-1b/6.1 File structure/.../file.pdf
// Local paths should be:     testdata/verapdf/PDF_A-1b/6.1 File structure/.../file.pdf
func pdfPathFromJobName(name string) string {
	const containerPrefix = "/output/"
	if idx := strings.Index(name, containerPrefix); idx >= 0 {
		return filepath.Join(verapdfCorpusDir, name[idx+len(containerPrefix):])
	}
	return name
}

// loadVerapdfReference loads a single veraPDF batch JSON reference file.
func loadVerapdfReference(t *testing.T, path string) *verapdfReport {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading verapdf reference %s: %v", path, err)
	}
	var report verapdfReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parsing verapdf reference %s: %v", path, err)
	}
	return &report
}

// --- The test ---

// TestVeraPDFCorpus validates litho's PDF/A and PDF/UA compliance checks
// against the veraPDF test corpus. For each PDF, the test compares litho's
// pass/fail verdict against both the filename convention and veraPDF's result.
func TestVeraPDFCorpus(t *testing.T) {
	if !corpusEnabled("verapdf") {
		t.Skip("verapdf corpus not enabled (check LITHO_CORPUS_TAGS)")
	}

	refs, err := filepath.Glob(filepath.Join(verapdfReferencesDir, "*.json"))
	if err != nil || len(refs) == 0 {
		t.Skip("verapdf references not found")
	}

	for _, refPath := range refs {
		profileDir := strings.TrimSuffix(filepath.Base(refPath), ".json")
		report := loadVerapdfReference(t, refPath)

		for _, job := range report.Report.Jobs {
			job := job
			localPath := pdfPathFromJobName(job.ItemDetails.Name)
			testName := profileDir + "/" + filepath.Base(localPath)

			t.Run(testName, func(t *testing.T) {
				t.Parallel()

				if len(job.ValidationResult) == 0 {
					t.Skip("verapdf produced no validation result (parse error?)")
				}
				vResult := job.ValidationResult[0]

				expected, ok := expectedCompliantFromFilename(filepath.Base(localPath))
				if !ok {
					t.Skip("filename does not encode pass/fail")
				}

				// Cross-check: does veraPDF agree with the filename convention?
				if vResult.Compliant != expected {
					t.Logf("verapdf disagrees with filename: verapdf=%v filename=%v",
						vResult.Compliant, expected)
				}

				// Open the PDF with litho.
				f, err := os.Open(localPath)
				if err != nil {
					t.Skipf("pdf not available: %v", err)
				}
				t.Cleanup(func() {
					if err := f.Close(); err != nil {
						t.Errorf("closing %s: %v", localPath, err)
					}
				})

				info, err := f.Stat()
				if err != nil {
					t.Fatalf("stat: %v", err)
				}

				ctx, cancel := context.WithTimeout(context.Background(), perVeraPDFSubtestTimeout)
				defer cancel()

				reader, err := pdf.Open(ctx, f, info.Size(), pdf.WithFilterPlugin(jbig2.New()), pdf.WithFilterPlugin(jpx.New()))
				if err != nil {
					if expected {
						t.Errorf("litho cannot open a file expected to pass: %v", err)
					} else {
						t.Logf("litho open error on fail-expected file: %v", err)
					}
					return
				}

				// Run the appropriate validation.
				var violations []error
				if pdfaProfile, ok := dirToPDFAProfile[profileDir]; ok {
					violations, err = reader.ValidatePDFA(ctx, pdfaProfile)
				} else if pdfuaProfile, ok := dirToPDFUAProfile[profileDir]; ok {
					violations, err = reader.ValidatePDFUA(ctx, pdfuaProfile)
				} else {
					t.Skipf("no litho profile mapping for %s", profileDir)
					return
				}
				if err != nil {
					t.Fatalf("validation error: %v", err)
				}

				lithoCompliant := len(violations) == 0

				// Primary assertion: litho agrees with the expected result.
				if lithoCompliant != expected {
					if expected {
						t.Errorf("litho reports violations on a pass-expected file:")
						for _, v := range violations {
							t.Errorf("  %v", v)
						}
					} else {
						t.Errorf("litho reports compliant on a fail-expected file")
					}
				}

				// Informational: log litho violations and verapdf rules for comparison.
				if len(violations) > 0 {
					t.Logf("litho violations (%d):", len(violations))
					for _, v := range violations {
						t.Logf("  %v", v)
					}
				}
				if vResult.Details.FailedRules > 0 {
					t.Logf("verapdf failed rules (%d):", vResult.Details.FailedRules)
					for _, r := range vResult.Details.RuleSummaries {
						if r.Status == "failed" {
							t.Logf("  clause %s test %d: %s", r.Clause, r.TestNumber, r.Description)
						}
					}
				}
			})
		}
	}
}
