package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
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

// dirToWTPDFProfile maps the Well-Tagged PDF profile directory names. WTPDF
// adopts the ISO 14289-2 requirements, so VeraPDF validates these fixtures
// under its wt1a and wt1r flavours and litho under [pdf.Reader.ValidateWTPDF].
var dirToWTPDFProfile = map[string]models.WTPDFProfile{
	"WTPDF-1.0-Accessibility": models.WTPDF1Accessibility,
	"WTPDF-1.0-Reuse":         models.WTPDF1Reuse,
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
	return loadVeraPDFGoldens(dir, func(id string, g *complianceGolden) {
		f.mu.Lock()
		f.goldens[id] = g
		f.mu.Unlock()
	})
}

func (f *complianceFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.goldens[fixtureID]
	return ok
}

// complianceOutput is litho's verdict, the raw violation strings, and the
// distinct rule ids those violations fail.
type complianceOutput struct {
	Compliant  bool     `json:"compliant"`
	Violations []string `json:"violations,omitempty"`
	Rules      []string `json:"rules,omitempty"`
	ProfileDir string   `json:"profile_dir"`
}

func (f *complianceFeature) Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error) {
	f.mu.RLock()
	golden := f.goldens[fixtureID]
	f.mu.RUnlock()
	if golden == nil {
		return nil, nil
	}
	out := validateWithLitho(ctx, pdfPath, golden.ProfileDir)
	out.ProfileDir = golden.ProfileDir
	return json.Marshal(out)
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

	// Match rule: litho and VeraPDF must agree on the verdict. A fixture
	// the VeraPDF corpus was never designed for still counts: both tools
	// will usually call it non-compliant for unrelated reasons, which is
	// a real alignment data point.
	metrics := map[string]int{}
	reason := ""
	match := got.Compliant == golden.Compliant
	if !match {
		if got.Compliant && !golden.Compliant {
			metrics["false_negative"] = 1
			reason = "false negative: litho compliant, VeraPDF non-compliant"
		} else {
			metrics["false_positive"] = 1
			reason = "false positive: litho non-compliant, VeraPDF compliant"
		}
	}

	// The verdict alone cannot see a detection change on a fixture that
	// breaks many rules: both tools call it non-compliant whether litho
	// drops a rule class or invents one. The rule sets can, so every scored
	// cell reports ruleset_mismatch, zero included, which keeps the key in
	// the baseline and the bar in force when no cell diverges.
	lithoOnly, veraOnly := ruleSetDiff(got.Rules, goldenRuleIDs(golden.FailedRules))
	rulesDiffer := len(lithoOnly) > 0 || len(veraOnly) > 0
	metrics["ruleset_mismatch"] = 0
	if rulesDiffer {
		metrics["ruleset_mismatch"] = 1
		diff := "rule set differs: " + formatRuleSetDiff(lithoOnly, veraOnly)
		if reason == "" {
			reason = diff
		} else {
			reason += "; " + diff
		}
	}

	score := 0.0
	if match {
		score = 1.0
	}

	var mismatches []json.RawMessage
	if !match || rulesDiffer {
		raw, _ := json.Marshal(complianceMismatch{
			File:             fixtureID,
			Profile:          golden.ProfileDir,
			VerapdfRules:     golden.FailedRules,
			LithoViolations:  got.Violations,
			LithoOnlyRules:   lithoOnly,
			VerapdfOnlyRules: veraOnly,
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
// violation strings for the scoreboard, and names the rule ids only one
// side reports.
type complianceMismatch struct {
	File             string           `json:"file"`
	Profile          string           `json:"profile"`
	VerapdfRules     []complianceRule `json:"verapdf_rules,omitempty"`
	LithoViolations  []string         `json:"litho_violations,omitempty"`
	LithoOnlyRules   []string         `json:"litho_only_rules,omitempty"`
	VerapdfOnlyRules []string         `json:"verapdf_only_rules,omitempty"`
}

// goldenRuleIDs returns the ids of the rules VeraPDF failed, in the form
// [distinctRuleIDs] gives litho's.
func goldenRuleIDs(rules []complianceRule) []string {
	ids := make([]string, len(rules))
	for i, r := range rules {
		ids[i] = ruleID(r.Clause, r.TestNumber)
	}
	return ids
}

// ruleSetDiff returns the ids only litho reports and the ids only VeraPDF
// reports. It compares sets, so the order and multiplicity of either input
// do not matter, and both results come back sorted.
func ruleSetDiff(litho, vera []string) (lithoOnly, veraOnly []string) {
	inLitho := make(map[string]bool, len(litho))
	for _, id := range litho {
		inLitho[id] = true
	}
	inVera := make(map[string]bool, len(vera))
	for _, id := range vera {
		inVera[id] = true
	}
	for id := range inLitho {
		if !inVera[id] {
			lithoOnly = append(lithoOnly, id)
		}
	}
	for id := range inVera {
		if !inLitho[id] {
			veraOnly = append(veraOnly, id)
		}
	}
	sort.Strings(lithoOnly)
	sort.Strings(veraOnly)
	return lithoOnly, veraOnly
}

// formatRuleSetDiff renders a rule-set difference for the text scoreboard.
func formatRuleSetDiff(lithoOnly, veraOnly []string) string {
	var parts []string
	if len(lithoOnly) > 0 {
		parts = append(parts, "litho only "+strings.Join(lithoOnly, " "))
	}
	if len(veraOnly) > 0 {
		parts = append(parts, "VeraPDF only "+strings.Join(veraOnly, " "))
	}
	return strings.Join(parts, "; ")
}

// fixtureIDFromVeraPDFName derives the fixture ID from a VeraPDF job's
// itemDetails.name. Under the flat pdfs/ layout, the path is always
// "/output/pdfs/<basename>.pdf"; the fixture ID must be
// "<profile>/<basename>" to line up with walk-flat-x-profiles.
func fixtureIDFromVeraPDFName(name, profileDir string) string {
	base := filepath.Base(name)
	base = strings.TrimSuffix(base, ".pdf")
	return profileDir + "/" + base
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

// loadVeraPDFGoldens walks dir for <profile>.json files and calls
// store(id, golden) for every job it finds. Used by both the compliance
// feature (as its golden source) and the remediation feature (to pick
// up verapdf_pre counts without a second invocation).
func loadVeraPDFGoldens(dir string, store func(id string, g *complianceGolden)) error {
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
			store(id, &complianceGolden{
				Profile:     vr.ProfileName,
				ProfileDir:  profileDir,
				Compliant:   vr.Compliant,
				FailedRules: failedRules(vr),
			})
			loaded++
		}
	}
	if loaded == 0 {
		return ErrNoGoldens
	}
	return nil
}

// validateWithLitho opens pdfPath, dispatches to the litho validator for
// the matching PDF/A, PDF/UA or WTPDF profile, and returns the verdict, the
// violation strings, and the distinct rule ids they fail. A file litho
// cannot open or validate comes back non-compliant with the error as its
// only violation and no rule ids.
func validateWithLitho(ctx context.Context, pdfPath, profileDir string) complianceOutput {
	failed := func(format string, err error) complianceOutput {
		return complianceOutput{Violations: []string{fmt.Sprintf(format, err)}}
	}
	f, err := os.Open(pdfPath)
	if err != nil {
		return failed("open: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return failed("stat: %v", err)
	}

	r, err := pdf.Open(ctx, f, info.Size())
	if err != nil {
		return failed("pdf.Open: %v", err)
	}
	defer r.Close()

	violations, known, err := validateReader(ctx, r, profileDir)
	if !known {
		// A profile directory no map covers scores every fixture in it as
		// compliant, which reads as a feature that passed rather than one
		// that validated nothing. Add the profile to a map above when a new
		// golden directory appears.
		return complianceOutput{Compliant: true}
	}
	if err != nil {
		return failed("validate: %v", err)
	}
	if len(violations) == 0 {
		return complianceOutput{Compliant: true}
	}
	out := complianceOutput{
		Violations: make([]string, len(violations)),
		Rules:      distinctRuleIDs(violations),
	}
	for i, v := range violations {
		out.Violations[i] = v.Error()
	}
	return out
}

// validateReader runs litho's validator for the PDF/A, PDF/UA or WTPDF profile
// profileDir names on r. known is false for a directory no map covers, which
// validates nothing.
func validateReader(ctx context.Context, r *pdf.Reader, profileDir string) (violations []pdf.Violation, known bool, err error) {
	if pdfa, ok := dirToPDFAProfile[profileDir]; ok {
		violations, err = r.ValidatePDFAReport(ctx, pdfa)
		return violations, true, err
	}
	if pdfua, ok := dirToPDFUAProfile[profileDir]; ok {
		violations, err = r.ValidatePDFUAReport(ctx, pdfua)
		return violations, true, err
	}
	if wtpdf, ok := dirToWTPDFProfile[profileDir]; ok {
		violations, err = r.ValidateWTPDFReport(ctx, wtpdf)
		return violations, true, err
	}
	return nil, false, nil
}
