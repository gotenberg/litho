package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// remediationFeature remediates each fixture with litho, then validates
// the output with both litho and VeraPDF to surface producer-consumer
// disagreement. Implements [BatchPreparer]: remediation runs per fixture
// (parallel) but VeraPDF runs once per profile on a staging directory
// holding every remediated PDF for that profile. One VeraPDF invocation
// per (profile, aggressive?) collapses the cost from O(fixtures) shell-
// outs to O(profiles) shell-outs.
type remediationFeature struct {
	aggressive bool
	mu         sync.RWMutex
	// pre-counts carried over from the compliance golden, keyed by
	// fixture ID. Populated by LoadGoldens.
	complianceGoldens map[string]*complianceGolden
	// verdicts, populated by Prepare and consumed by Run.
	verdicts map[string]*remediationOutput
}

func newRemediationFeature(aggressive bool) *remediationFeature {
	return &remediationFeature{
		aggressive:        aggressive,
		complianceGoldens: make(map[string]*complianceGolden),
		verdicts:          make(map[string]*remediationOutput),
	}
}

func (f *remediationFeature) Name() string {
	if f.aggressive {
		return "remediation-aggressive"
	}
	return "remediation"
}

func (f *remediationFeature) GoldenSource() string { return "verapdf-batched" }

// LoadGoldens parses the compliance golden directory so Prepare can
// reach the verapdf_pre count without re-running VeraPDF on the
// originals. The manifest points "remediation" at the same directory
// "compliance" uses (usually goldens/compliance).
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

// Prepare remediates every fixture and runs VeraPDF in batches keyed
// by profile. Stage 1 (parallel): for each fixture, apply litho
// remediation, validate the buffer with litho, and write the result
// into the profile's staging dir. Stage 2 (sequential, one per
// profile): run `verapdf -r -f <flavour>` on the staging dir, parse
// the batch report, and compose the per-fixture verdict.
func (f *remediationFeature) Prepare(ctx context.Context, fixtures []Fixture) error {
	if _, err := exec.LookPath("verapdf"); err != nil {
		return errors.New("verapdf binary not found on PATH (install VeraPDF or run inside the provisioning container)")
	}

	byProfile := make(map[string][]Fixture)
	for _, fx := range fixtures {
		head, _, ok := strings.Cut(fx.ID, "/")
		if !ok || !isProfileDir(head) {
			continue
		}
		byProfile[head] = append(byProfile[head], fx)
	}

	for profile, profileFixtures := range byProfile {
		if err := f.preparePerProfile(ctx, profile, profileFixtures); err != nil {
			return fmt.Errorf("profile %s: %w", profile, err)
		}
	}
	return nil
}

// preparePerProfile remediates every fixture for one conformance
// profile, then asks VeraPDF for its verdict on the staging directory
// that holds the remediated copies.
func (f *remediationFeature) preparePerProfile(ctx context.Context, profile string, fixtures []Fixture) error {
	pdfaProfile, isPDFA := dirToPDFAProfile[profile]
	if !isPDFA {
		// Remediation engine only targets PDF/A. PDF/UA fixtures get a
		// stub verdict so Compare can render a "not supported" reason.
		f.recordSkippedProfile(profile, fixtures, "remediation does not target PDF/UA")
		return nil
	}

	stagingDir, err := os.MkdirTemp("", "litho-remediation-"+profile+"-")
	if err != nil {
		return fmt.Errorf("staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	flavour, ok := profileDirToVeraPDFFlavour(profile)
	if !ok {
		f.recordSkippedProfile(profile, fixtures, "no verapdf flavour")
		return nil
	}

	type stagingResult struct {
		fixtureID   string
		stagingPath string
		litho       remediationOutput
	}
	results := make([]stagingResult, len(fixtures))

	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, fx := range fixtures {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res := stagingResult{fixtureID: fx.ID}
			res.litho.Profile = profile
			res.litho.Aggressive = f.aggressive
			f.mu.RLock()
			golden := f.complianceGoldens[fx.ID]
			f.mu.RUnlock()
			if golden != nil {
				res.litho.VeraPre = len(golden.FailedRules)
			}
			if err := remediateOnce(ctx, fx.PDFPath, pdfaProfile, f.aggressive, stagingDir, profile, &res.litho, &res.stagingPath); err != nil {
				res.litho.Error = err.Error()
			}
			results[i] = res
		}()
	}
	wg.Wait()

	// Run VeraPDF once on the staging dir.
	veraByBase, err := batchValidate(ctx, stagingDir, flavour)
	if err != nil {
		return fmt.Errorf("verapdf batch: %w", err)
	}

	// Stitch verapdf_post counts into the per-fixture verdicts and
	// publish to the verdicts map.
	for _, r := range results {
		if r.fixtureID == "" {
			continue
		}
		out := r.litho
		if out.Error == "" {
			base := strings.TrimPrefix(r.fixtureID, profile+"/")
			if veraPost, ok := veraByBase[base]; ok {
				out.VeraPost = veraPost.count
				out.VeraRules = veraPost.rules
			} else {
				// VeraPDF skipped this file (write failed, etc). Treat as
				// no improvement so the disagreement metric stays sane.
				out.VeraPost = out.VeraPre
			}
		}
		f.mu.Lock()
		f.verdicts[r.fixtureID] = &out
		f.mu.Unlock()
	}
	return nil
}

// recordSkippedProfile marks every fixture under a profile as skipped
// with the given reason. Used for PDF/UA fixtures the remediation
// engine does not target.
func (f *remediationFeature) recordSkippedProfile(profile string, fixtures []Fixture, reason string) {
	for _, fx := range fixtures {
		f.mu.Lock()
		f.verdicts[fx.ID] = &remediationOutput{
			Profile:    profile,
			Aggressive: f.aggressive,
			Error:      reason,
		}
		f.mu.Unlock()
	}
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

func (f *remediationFeature) Run(_ context.Context, _, fixtureID string) ([]byte, error) {
	f.mu.RLock()
	v := f.verdicts[fixtureID]
	f.mu.RUnlock()
	if v == nil {
		return json.Marshal(remediationOutput{Error: "no precomputed verdict"})
	}
	return json.Marshal(*v)
}

func (f *remediationFeature) Compare(fixtureID string, output []byte) Verdict {
	var got remediationOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.Error != "" {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: got.Error}
	}

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

// remediateOnce opens pdfPath, validates with litho, applies the
// remediation engine, validates the buffer with litho, and writes the
// remediated bytes to stagingDir under <basename>.pdf. The basename
// comes from the fixture ID (profile prefix stripped).
func remediateOnce(ctx context.Context, pdfPath string, profile models.PDFAProfile, aggressive bool, stagingDir, profileDir string, out *remediationOutput, stagingPath *string) error {
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
	defer r.Close()
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
	defer r2.Close()
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

	// Write the remediated bytes for the upcoming VeraPDF batch run.
	base := strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath))
	*stagingPath = filepath.Join(stagingDir, base+".pdf")
	if err := os.WriteFile(*stagingPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write staging: %w", err)
	}
	return nil
}

// veraVerdict captures one VeraPDF result on a remediated PDF.
type veraVerdict struct {
	count int
	rules []string
}

// batchValidate runs `verapdf -r -f <flavour>` on dir and returns a
// map keyed by basename (without extension) with the failed-rule count
// and identifiers per file.
func batchValidate(ctx context.Context, dir, flavour string) (map[string]veraVerdict, error) {
	cmd := exec.CommandContext(ctx, "verapdf", //nolint:gosec // flavour from a static map, dir is a tempdir
		"-r", "-f", flavour, "--format", "json",
		"--maxfailuresdisplayed", "-1", dir)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// VeraPDF exits non-zero when files fail compliance. The JSON
		// is still emitted on stdout. Bail only if we got nothing.
		if stdout.Len() == 0 {
			return nil, fmt.Errorf("verapdf: %w", err)
		}
	}
	var report verapdfReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return nil, fmt.Errorf("parse verapdf output: %w", err)
	}
	out := make(map[string]veraVerdict, len(report.Report.Jobs))
	for _, job := range report.Report.Jobs {
		if len(job.ValidationResult) == 0 {
			continue
		}
		base := filepath.Base(job.ItemDetails.Name)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		rules := failedRules(job.ValidationResult[0])
		ids := make([]string, 0, len(rules))
		for _, r := range rules {
			ids = append(ids, fmt.Sprintf("%s-t%02d", r.Clause, r.TestNumber))
		}
		out[base] = veraVerdict{count: len(rules), rules: ids}
	}
	return out, nil
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
