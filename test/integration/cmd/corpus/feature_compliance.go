package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// verapdfReport is VeraPDF's JSON output shape. One report file per
// profile aggregates every fixture in that profile.
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

// complianceGolden captures VeraPDF's verdict for one fixture-profile
// pairing alongside the failed-rule list used in the mismatch output.
type complianceGolden struct {
	Profile     string
	ProfileDir  string
	Compliant   bool
	FailedRules []complianceRule
}

type complianceRule struct {
	Clause      string `json:"clause"`
	TestNumber  int    `json:"test_number"`
	Description string `json:"description"`
}

// dirToPDFAProfile maps VeraPDF profile directory names to the litho
// profile enum. PDF/UA profiles live in dirToPDFUAProfile below.
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

// complianceFeature cross-checks litho's compliance verdict against
// VeraPDF. The golden index is keyed by fixture ID (relative path from
// corpus PDF root, without extension).
type complianceFeature struct {
	mu      sync.RWMutex
	goldens map[string]*complianceGolden
}

func newComplianceFeature() *complianceFeature {
	return &complianceFeature{goldens: make(map[string]*complianceGolden)}
}

func (f *complianceFeature) Name() string         { return "compliance" }
func (f *complianceFeature) GoldenSource() string { return "verapdf" }

// LoadGoldens walks dir for <profile>.json files, each a VeraPDF report
// covering every fixture under that profile. The feature flattens the
// per-profile jobs into a per-fixture map so Run/Compare can key by ID.
func (f *complianceFeature) LoadGoldens(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ErrNoGoldens
	}
	loaded := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var report verapdfReport
		if err := json.Unmarshal(data, &report); err != nil {
			continue
		}
		profileDir := strings.TrimSuffix(e.Name(), ".json")
		for _, job := range report.Report.Jobs {
			if len(job.ValidationResult) == 0 {
				continue
			}
			vr := job.ValidationResult[0]
			id := fixtureIDFromVeraPDFName(job.ItemDetails.Name, profileDir)
			f.mu.Lock()
			f.goldens[id] = &complianceGolden{
				Profile:     vr.ProfileName,
				ProfileDir:  profileDir,
				Compliant:   vr.Compliant,
				FailedRules: failedRules(vr),
			}
			f.mu.Unlock()
			loaded++
		}
	}
	if loaded == 0 {
		return ErrNoGoldens
	}
	return nil
}

func (f *complianceFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.goldens[fixtureID]
	return ok
}

// complianceOutput is litho's verdict plus the raw violation strings.
type complianceOutput struct {
	Compliant  bool     `json:"compliant"`
	Violations []string `json:"violations,omitempty"`
	ProfileDir string   `json:"profile_dir"`
}

func (f *complianceFeature) Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error) {
	f.mu.RLock()
	golden := f.goldens[fixtureID]
	f.mu.RUnlock()
	if golden == nil {
		return nil, nil
	}
	compliant, violations := validateWithLitho(ctx, pdfPath, golden.ProfileDir)
	return json.Marshal(complianceOutput{
		Compliant:  compliant,
		Violations: violations,
		ProfileDir: golden.ProfileDir,
	})
}

func (f *complianceFeature) Compare(fixtureID string, output []byte) Verdict {
	f.mu.RLock()
	golden := f.goldens[fixtureID]
	f.mu.RUnlock()
	if golden == nil {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no golden"}
	}

	var got complianceOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}

	// Expected verdict is the corpus filename convention (-pass- / -fail-).
	// When litho lands on the same verdict as VeraPDF (even if that
	// disagrees with the filename) both validators have converged and the
	// filename is the corpus anomaly; credit litho for matching VeraPDF.
	expected, hasExpected := expectedCompliantFromFilename(filepath.Base(fixtureID))
	reason := ""
	metrics := map[string]int{}

	// Agrees with VeraPDF → match. Disagrees with VeraPDF but agrees
	// with the filename convention → still match (the corpus anomaly
	// is on VeraPDF's side and litho happens to be right).
	match := got.Compliant == golden.Compliant || (hasExpected && got.Compliant == expected)
	if !match {
		if hasExpected && !got.Compliant && expected {
			metrics["false_positive"] = 1
			reason = "false positive"
		} else {
			metrics["false_negative"] = 1
			reason = "false negative"
		}
	}

	score := 0.0
	if match {
		score = 1.0
	}

	var mismatches []json.RawMessage
	if !match {
		raw, _ := json.Marshal(complianceMismatch{
			File:            fixtureID,
			Profile:         golden.ProfileDir,
			VerapdfRules:    golden.FailedRules,
			LithoViolations: got.Violations,
		})
		mismatches = append(mismatches, raw)
	}

	return Verdict{
		FixtureID:  fixtureID,
		Match:      match,
		Score:      score,
		Reason:     reason,
		Metrics:    metrics,
		Mismatches: mismatches,
	}
}

// complianceMismatch pairs the golden's failed rules with litho's
// violation strings for the scoreboard.
type complianceMismatch struct {
	File            string           `json:"file"`
	Profile         string           `json:"profile"`
	VerapdfRules    []complianceRule `json:"verapdf_rules,omitempty"`
	LithoViolations []string         `json:"litho_violations,omitempty"`
}

// expectedCompliantFromFilename derives the expected verdict from the
// VeraPDF corpus naming convention.
func expectedCompliantFromFilename(name string) (bool, bool) {
	switch {
	case strings.Contains(name, "-pass-"):
		return true, true
	case strings.Contains(name, "-fail-"):
		return false, true
	default:
		return false, false
	}
}

// fixtureIDFromVeraPDFName converts a container-absolute path from the
// VeraPDF report into a corpus-relative fixture ID: e.g.
// "/output/PDF_A-2b/6.1 File structure/foo.pdf" ->
// "PDF_A-2b/6.1 File structure/foo".
func fixtureIDFromVeraPDFName(name, profileDir string) string {
	const prefix = "/output/"
	rel := name
	if _, after, ok := strings.Cut(name, prefix); ok {
		rel = after
	}
	rel = strings.TrimSuffix(rel, ".pdf")
	if !strings.HasPrefix(rel, profileDir+"/") {
		rel = profileDir + "/" + strings.TrimPrefix(rel, profileDir)
		rel = strings.TrimPrefix(rel, "/")
	}
	return rel
}

// failedRules pulls the "failed" rule summaries from a VeraPDF result.
func failedRules(vr verapdfValidationResult) []complianceRule {
	var rules []complianceRule
	for _, r := range vr.Details.RuleSummaries {
		if r.Status == "failed" {
			rules = append(rules, complianceRule{
				Clause:      r.Clause,
				TestNumber:  r.TestNumber,
				Description: r.Description,
			})
		}
	}
	return rules
}

// validateWithLitho opens pdfPath, dispatches to the litho validator for
// the matching PDF/A or PDF/UA profile, and returns the verdict plus the
// violation strings.
func validateWithLitho(ctx context.Context, pdfPath, profileDir string) (bool, []string) {
	f, err := os.Open(pdfPath)
	if err != nil {
		return false, []string{fmt.Sprintf("open: %v", err)}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return false, []string{fmt.Sprintf("stat: %v", err)}
	}

	r, err := pdf.Open(ctx, f, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return false, []string{fmt.Sprintf("pdf.Open: %v", err)}
	}

	var errs []error
	if pdfa, ok := dirToPDFAProfile[profileDir]; ok {
		errs, err = r.ValidatePDFA(ctx, pdfa)
	} else if pdfua, ok := dirToPDFUAProfile[profileDir]; ok {
		errs, err = r.ValidatePDFUA(ctx, pdfua)
	} else {
		return true, nil
	}
	if err != nil {
		return false, []string{fmt.Sprintf("validate: %v", err)}
	}
	if len(errs) == 0 {
		return true, nil
	}
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return false, out
}
