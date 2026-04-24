package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// remediationFeature remediates each fixture with litho, then validates
// the output with both litho and VeraPDF to surface producer-consumer
// disagreement. There is no captured golden: the feature reports live
// numbers that a human reads on every run. Matches are the fixtures
// both validators rule compliant after remediation (fully fixed).
//
// The compliance goldens serve as the source of verapdf_pre — the
// VeraPDF verdict on the original PDF, already captured during init.
// One VeraPDF shell-out per fixture is enough.
type remediationFeature struct {
	aggressive bool
	mu         sync.RWMutex
	// pre-counts carried over from the compliance golden, keyed by
	// fixture ID. Populated by LoadGoldens.
	complianceGoldens map[string]*complianceGolden
}

func newRemediationFeature(aggressive bool) *remediationFeature {
	return &remediationFeature{
		aggressive:        aggressive,
		complianceGoldens: make(map[string]*complianceGolden),
	}
}

func (f *remediationFeature) Name() string {
	if f.aggressive {
		return "remediation-aggressive"
	}
	return "remediation"
}

func (f *remediationFeature) GoldenSource() string { return "verapdf-live" }

// LoadGoldens parses the compliance golden directory so Run can reach
// the verapdf_pre count without a second VeraPDF invocation per fixture.
// The manifest points "remediation" at the same directory "compliance"
// uses (usually goldens/compliance).
func (f *remediationFeature) LoadGoldens(dir string) error {
	return loadVeraPDFGoldens(dir, func(id string, g *complianceGolden) {
		f.mu.Lock()
		f.complianceGoldens[id] = g
		f.mu.Unlock()
	})
}

func (f *remediationFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.complianceGoldens[fixtureID]
	return ok
}

// remediationOutput is the per-fixture blob Compare decodes. The four
// violation counts plus the VeraPDF vs litho disagreement on the
// remediated buffer drive the scoreboard metrics.
type remediationOutput struct {
	Profile    string   `json:"profile"`
	Aggressive bool     `json:"aggressive"`
	LithoPre   int      `json:"litho_pre"`
	LithoPost  int      `json:"litho_post"`
	VeraPre    int      `json:"verapdf_pre"`
	VeraPost   int      `json:"verapdf_post"`
	NewErrors  []string `json:"new_errors,omitempty"`
	VeraRules  []string `json:"verapdf_post_rules,omitempty"`
	Error      string   `json:"error,omitempty"`
}

func (f *remediationFeature) Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error) {
	f.mu.RLock()
	golden := f.complianceGoldens[fixtureID]
	f.mu.RUnlock()
	if golden == nil {
		return json.Marshal(remediationOutput{Error: "no compliance golden for fixture"})
	}

	profile := models.PDFAProfile("")
	if p, ok := dirToPDFAProfile[golden.ProfileDir]; ok {
		profile = p
	} else if _, ok := dirToPDFUAProfile[golden.ProfileDir]; ok {
		// The remediation engine only targets PDF/A at the moment; PDF/UA
		// fixtures run a no-op remediation that still validates both ways.
		return json.Marshal(remediationOutput{
			Profile:    golden.ProfileDir,
			Aggressive: f.aggressive,
			Error:      "remediation does not target PDF/UA profiles",
		})
	}

	out := remediationOutput{
		Profile:    golden.ProfileDir,
		Aggressive: f.aggressive,
		VeraPre:    len(golden.FailedRules),
	}

	if err := runRemediation(ctx, pdfPath, profile, f.aggressive, &out); err != nil {
		out.Error = err.Error()
	}
	return json.Marshal(out)
}

func (f *remediationFeature) Compare(fixtureID string, output []byte) Verdict {
	var got remediationOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.Error != "" {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: got.Error}
	}

	// Match = both validators see 0 violations on the remediated buffer.
	fullyFixed := got.LithoPost == 0 && got.VeraPost == 0
	score := 0.0
	if fullyFixed {
		score = 1.0
	}

	disagreement := got.LithoPost - got.VeraPost
	if disagreement < 0 {
		disagreement = -disagreement
	}

	metrics := map[string]int{
		"litho_pre":     got.LithoPre,
		"litho_post":    got.LithoPost,
		"verapdf_pre":   got.VeraPre,
		"verapdf_post":  got.VeraPost,
		"litho_fixed":   max(0, got.LithoPre-got.LithoPost),
		"verapdf_fixed": max(0, got.VeraPre-got.VeraPost),
		"disagreement":  disagreement,
	}
	if fullyFixed {
		metrics["fully_fixed"] = 1
	}
	if got.LithoPost > got.LithoPre {
		metrics["litho_regressed"] = 1
	}
	if got.VeraPost > got.VeraPre {
		metrics["verapdf_regressed"] = 1
	}

	raw, _ := json.Marshal(remediationReport{
		File:       fixtureID,
		Profile:    got.Profile,
		Aggressive: f.aggressive,
		LithoPre:   got.LithoPre,
		LithoPost:  got.LithoPost,
		VeraPre:    got.VeraPre,
		VeraPost:   got.VeraPost,
		NewErrors:  got.NewErrors,
		VeraRules:  got.VeraRules,
	})

	return Verdict{
		FixtureID:  fixtureID,
		Match:      fullyFixed,
		Score:      score,
		Metrics:    metrics,
		Mismatches: []json.RawMessage{raw},
	}
}

type remediationReport struct {
	File       string   `json:"file"`
	Profile    string   `json:"profile"`
	Aggressive bool     `json:"aggressive"`
	LithoPre   int      `json:"litho_pre"`
	LithoPost  int      `json:"litho_post"`
	VeraPre    int      `json:"verapdf_pre"`
	VeraPost   int      `json:"verapdf_post"`
	NewErrors  []string `json:"new_errors,omitempty"`
	VeraRules  []string `json:"verapdf_post_rules,omitempty"`
}

// runRemediation opens pdfPath, validates with litho, applies the
// remediation engine, writes the result to a temp file, re-validates
// with litho, and shells out to VeraPDF on the same temp file. The out
// parameter is populated in place.
func runRemediation(ctx context.Context, pdfPath string, profile models.PDFAProfile, aggressive bool, out *remediationOutput) error {
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return fmt.Errorf("pdf.Open: %w", err)
	}
	pre, err := r.ValidatePDFA(ctx, profile)
	if err != nil {
		return fmt.Errorf("validate pre: %w", err)
	}
	out.LithoPre = len(pre)

	w := pdf.NewWriter(r)
	var opts []pdf.RemediateOption
	if aggressive {
		opts = append(opts, pdf.WithAggressiveFixes())
	}
	if _, err := w.RemediatePDFA(ctx, profile, opts...); err != nil {
		out.LithoPost = out.LithoPre
		out.VeraPost = out.VeraPre
		return nil
	}

	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		out.LithoPost = out.LithoPre
		out.VeraPost = out.VeraPre
		return nil
	}

	r2, err := pdf.Open(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		out.LithoPost = out.LithoPre
		out.VeraPost = out.VeraPre
		return nil
	}
	post, err := r2.ValidatePDFA(ctx, profile)
	if err != nil {
		out.LithoPost = out.LithoPre
		out.VeraPost = out.VeraPre
		return nil
	}
	out.LithoPost = len(post)
	if len(post) > len(pre) {
		for _, v := range post {
			if isNew(v, pre) {
				out.NewErrors = append(out.NewErrors, v.Error())
			}
		}
	}

	// VeraPDF cross-check on the remediated buffer.
	veraPost, veraRules, err := validateWithVeraPDF(ctx, buf.Bytes(), out.Profile)
	if err != nil {
		out.Error = fmt.Sprintf("verapdf: %v", err)
		out.VeraPost = out.VeraPre
		return nil
	}
	out.VeraPost = veraPost
	out.VeraRules = veraRules
	return nil
}

// validateWithVeraPDF writes buf to a temp file and invokes the
// `verapdf` binary on it, returning the failed-rule count and the list
// of failed clause/test identifiers. Errors if the binary is missing.
func validateWithVeraPDF(ctx context.Context, buf []byte, profileDir string) (int, []string, error) {
	flavour, ok := profileDirToVeraPDFFlavour(profileDir)
	if !ok {
		return 0, nil, fmt.Errorf("no verapdf flavour for profile %q", profileDir)
	}

	tmp, err := os.CreateTemp("", "litho-remediation-*.pdf")
	if err != nil {
		return 0, nil, fmt.Errorf("tempfile: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return 0, nil, fmt.Errorf("write temp: %w", err)
	}
	tmp.Close()

	cmd := exec.CommandContext(ctx, "verapdf", //nolint:gosec // fixed binary name, flavour from whitelist, path is temp file
		"-f", flavour, "--format", "json",
		"--maxfailuresdisplayed", "-1",
		tmp.Name())
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// VeraPDF exits non-zero when the file is non-compliant. Only
		// bail on the "binary not found" kind of error.
		if _, pathErr := exec.LookPath("verapdf"); pathErr != nil {
			return 0, nil, fmt.Errorf("verapdf binary missing from PATH: install VeraPDF or run under the provisioning container")
		}
	}

	var report verapdfReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return 0, nil, fmt.Errorf("parse verapdf output: %w", err)
	}
	for _, job := range report.Report.Jobs {
		if len(job.ValidationResult) == 0 {
			continue
		}
		vr := job.ValidationResult[0]
		rules := failedRules(vr)
		ids := make([]string, 0, len(rules))
		for _, r := range rules {
			ids = append(ids, fmt.Sprintf("%s-t%02d", r.Clause, r.TestNumber))
		}
		return len(rules), ids, nil
	}
	return 0, nil, nil
}

// profileDirToVeraPDFFlavour maps a profile directory name to the
// "-f" argument the VeraPDF CLI accepts.
func profileDirToVeraPDFFlavour(profileDir string) (string, bool) {
	m := map[string]string{
		"PDF_A-1a": "1a",
		"PDF_A-1b": "1b",
		"PDF_A-2a": "2a",
		"PDF_A-2b": "2b",
		"PDF_A-2u": "2u",
		"PDF_A-3a": "3a",
		"PDF_A-3b": "3b",
		"PDF_A-3u": "3u",
		"PDF_A-4":  "4",
		"PDF_A-4e": "4e",
		"PDF_A-4f": "4f",
		"PDF_UA-1": "ua1",
		"PDF_UA-2": "ua2",
	}
	v, ok := m[profileDir]
	return v, ok
}

func isNew(v error, before []error) bool {
	vs := v.Error()
	for _, b := range before {
		if b.Error() == vs {
			return false
		}
	}
	return true
}
