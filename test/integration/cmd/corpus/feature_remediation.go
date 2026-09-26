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
	"sort"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
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
// by profile. First, in parallel, it applies litho remediation to each
// fixture, validates the buffer with litho, and writes the result into
// the profile's staging dir. Then, sequentially and once per profile, it
// runs `verapdf -r -f <flavour>` on the staging dir, parses the batch
// report, and composes the per-fixture verdict.
func (f *remediationFeature) Prepare(ctx context.Context, fixtures []Fixture) error {
	if _, err := exec.LookPath("verapdf"); err != nil {
		return errors.New("verapdf binary not found on PATH (install VeraPDF or run inside the provisioning container)")
	}

	byProfile := make(map[string][]Fixture)
	for _, fx := range fixtures {
		// The manifest skips infinite loops and decompression bombs. The
		// scoring path honours that list (run.go), but the prepare stage did
		// not, so a fixture marked unsafe was still remediated here.
		if fx.SkipReason != "" {
			continue
		}
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

// remediators returns the litho validate and remediate closures for a profile
// directory, binding the PDF/A, PDF/UA or WTPDF conformance profile
// (auto-tagging makes remediation target PDF/UA, and WTPDF adopts the
// ISO 14289-2 rules, so all three are supported). ok is false for a profile
// the remediation engine does not target.
func (f *remediationFeature) remediators(profile string) (validate func(context.Context, *pdf.Reader) ([]pdf.Violation, error), remediate func(context.Context, *pdf.Writer) error, ok bool) {
	var opts []pdf.RemediateOption
	if f.aggressive {
		opts = append(opts, pdf.WithAggressiveFixes())
	}
	if p, isA := dirToPDFAProfile[profile]; isA {
		return func(ctx context.Context, r *pdf.Reader) ([]pdf.Violation, error) { return r.ValidatePDFAReport(ctx, p) },
			func(ctx context.Context, w *pdf.Writer) error { _, err := w.RemediatePDFA(ctx, p, opts...); return err },
			true
	}
	if p, isUA := dirToPDFUAProfile[profile]; isUA {
		return func(ctx context.Context, r *pdf.Reader) ([]pdf.Violation, error) {
				return r.ValidatePDFUAReport(ctx, p)
			},
			func(ctx context.Context, w *pdf.Writer) error {
				_, err := w.RemediatePDFUA(ctx, p, opts...)
				return err
			},
			true
	}
	if p, isWT := dirToWTPDFProfile[profile]; isWT {
		return func(ctx context.Context, r *pdf.Reader) ([]pdf.Violation, error) {
				return r.ValidateWTPDFReport(ctx, p)
			},
			func(ctx context.Context, w *pdf.Writer) error {
				_, err := w.RemediateWTPDF(ctx, p, opts...)
				return err
			},
			true
	}
	return nil, nil, false
}

// preparePerProfile remediates every fixture for one conformance
// profile, then asks VeraPDF for its verdict on the staging directory
// that holds the remediated copies.
func (f *remediationFeature) preparePerProfile(ctx context.Context, profile string, fixtures []Fixture) error {
	validate, remediate, ok := f.remediators(profile)
	if !ok {
		// Profile the remediation engine does not target (e.g. an unknown
		// directory name); record a stub verdict so Compare can render it.
		f.recordSkippedProfile(profile, fixtures, "unsupported remediation profile")
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
			// Record the result even if remediation panics: a lost worker
			// would otherwise leave a zero-valued entry that reads as a
			// clean run, and one panic must not lose the whole corpus.
			defer func() {
				if r := recover(); r != nil {
					res.litho.Error = fmt.Sprintf("panic: %v", r)
				}
				results[i] = res
			}()

			fctx, cancel := context.WithTimeout(ctx, prepareFixtureTimeout)
			defer cancel()
			f.mu.RLock()
			golden := f.complianceGoldens[fx.ID]
			f.mu.RUnlock()
			if golden != nil {
				res.litho.VeraPre = len(golden.FailedRules)
			}
			if err := remediateOnce(fctx, fx.PDFPath, validate, remediate, stagingDir, &res.litho, &res.stagingPath); err != nil {
				res.litho.Error = err.Error()
			}
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
				if veraPost.parseFailed {
					// A remediated output veraPDF cannot parse is strictly
					// worse than the original: flag it and count it as a
					// regression, never silently as "no improvement".
					out.ParseFailed = true
					out.VeraPost = out.VeraPre + 1
				} else {
					out.VeraPost = veraPost.count
					out.VeraRules = veraPost.rules
				}
			} else if vv, rerr := singleValidate(ctx, r.stagingPath, flavour); rerr == nil {
				// The batch run occasionally drops a job; a single-file
				// retry recovers the real verdict.
				if vv.parseFailed {
					out.ParseFailed = true
					out.VeraPost = out.VeraPre + 1
				} else {
					out.VeraPost = vv.count
					out.VeraRules = vv.rules
				}
			} else {
				// VeraPDF skipped this file twice (write failed, etc).
				// Treat as no improvement so the disagreement metric
				// stays sane.
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
	Profile     string   `json:"profile"`
	Aggressive  bool     `json:"aggressive"`
	LithoPre    int      `json:"litho_pre"`
	LithoPost   int      `json:"litho_post"`
	VeraPre     int      `json:"verapdf_pre"`
	VeraPost    int      `json:"verapdf_post"`
	NewErrors   []string `json:"new_errors,omitempty"`
	VeraRules   []string `json:"verapdf_post_rules,omitempty"`
	LithoRules  []string `json:"litho_post_rules,omitempty"`
	ParseFailed bool     `json:"parse_failed,omitempty"`
	Error       string   `json:"error,omitempty"`
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

	// Emit every metric unconditionally, zero included. A metric written only
	// when it fires disappears from the baseline once it reaches zero, and
	// diffBaseline iterates the baseline's own key set, so the bar stops
	// gating entirely: a later regression in that metric goes unreported.
	metrics := map[string]int{
		"litho_pre":         got.LithoPre,
		"litho_post":        got.LithoPost,
		"verapdf_pre":       got.VeraPre,
		"verapdf_post":      got.VeraPost,
		"litho_fixed":       max(0, got.LithoPre-got.LithoPost),
		"verapdf_fixed":     max(0, got.VeraPre-got.VeraPost),
		"disagreement":      disagreement,
		"parse_failed":      boolToInt(got.ParseFailed),
		"fully_fixed":       boolToInt(fullyFixed),
		"litho_regressed":   boolToInt(got.LithoPost > got.LithoPre),
		"verapdf_regressed": boolToInt(got.VeraPost > got.VeraPre),
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
		LithoRules: got.LithoRules,
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
	LithoRules []string `json:"litho_post_rules,omitempty"`
}

// remediateOnce opens pdfPath, validates with litho via validate, applies the
// remediation engine via remediate, validates the buffer with litho, and writes
// the remediated bytes to stagingDir under <basename>.pdf. The validate and
// remediate closures bind the conformance profile (PDF/A or PDF/UA), keeping the
// open/write/stage skeleton profile-agnostic. The basename comes from pdfPath.
func remediateOnce(ctx context.Context, pdfPath string, validate func(context.Context, *pdf.Reader) ([]pdf.Violation, error), remediate func(context.Context, *pdf.Writer) error, stagingDir string, out *remediationOutput, stagingPath *string) error {
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("pdf.Open: %w", err)
	}
	defer r.Close()
	pre, err := validate(ctx, r)
	if err != nil {
		return fmt.Errorf("validate pre: %w", err)
	}
	out.LithoPre = len(distinctRuleIDs(pre))

	w := pdf.NewWriter(r)
	// Removes the remediation output when the write below never runs.
	defer w.Close()
	if err := remediate(ctx, w); err != nil {
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

	r2, err := pdf.Open(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		out.LithoPost = out.LithoPre
		out.VeraPost = out.VeraPre
		return nil
	}
	defer r2.Close()
	post, err := validate(ctx, r2)
	if err != nil {
		out.LithoPost = out.LithoPre
		out.VeraPost = out.VeraPre
		return nil
	}
	// Count DISTINCT failed rules, the same unit batchValidate reports
	// for VeraPDF: counting per-occurrence violations against VeraPDF's
	// per-rule counts made the disagreement metric compare apples to
	// oranges (a 7-destination 8.8 failure read as 6 cells of fake
	// disagreement).
	out.LithoRules = distinctRuleIDs(post)
	out.LithoPost = len(out.LithoRules)
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

// boolToInt returns 1 for true and 0 for false, for metric tallies.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// veraVerdict captures one VeraPDF result on a remediated PDF.
type veraVerdict struct {
	count       int
	rules       []string
	parseFailed bool // veraPDF could not parse the file (broken output)
}

// batchValidate runs `verapdf -r -f <flavour>` on dir and returns a
// map keyed by basename (without extension) with the failed-rule count
// and identifiers per file.
func batchValidate(ctx context.Context, dir, flavour string) (map[string]veraVerdict, error) {
	ctx, cancel := context.WithTimeout(ctx, veraPDFBatchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "verapdf", //nolint:gosec // flavour from a static map, dir is a tempdir
		"-r", "-f", flavour, "--format", "json",
		"--maxfailuresdisplayed", "-1", dir)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// VeraPDF exits non-zero when files fail compliance. The JSON
		// is still emitted on stdout. Bail only if stdout is empty.
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
		base := filepath.Base(job.ItemDetails.Name)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		if len(job.ValidationResult) == 0 {
			// veraPDF emitted no validation result for this file: it failed
			// to parse (a malformed remediated output). Record it so the
			// caller counts it as a regression instead of silently masking
			// it as "no change".
			out[base] = veraVerdict{parseFailed: true}
			continue
		}
		out[base] = jobVerdict(job.ValidationResult[0])
	}
	return out, nil
}

// jobVerdict converts one VeraPDF validation result into a verdict.
//
// # Corpus evidence
//
//   - govdocs, 25 files: VeraPDF's rule order is stable batch against batch,
//     and batch against single permutes 5 of 6, which is why the ids are
//     sorted.
func jobVerdict(vr verapdfValidationResult) veraVerdict {
	rules := failedRules(vr)
	ids := make([]string, 0, len(rules))
	for _, r := range rules {
		ids = append(ids, fmt.Sprintf("%s-t%02d", r.Clause, r.TestNumber))
	}
	// VeraPDF lists rule summaries in a different order for a single-file run
	// than for the same file inside a batch, and singleValidate retries any
	// file the batch report drops, so identical failures reach
	// verapdf_post_rules permuted according to which path the file took.
	// Sort as distinctRuleIDs already does for the litho side, so diffing two
	// gate reports shows only what moved. The count stays len(rules): these
	// ids are reported, not deduplicated.
	sort.Strings(ids)
	return veraVerdict{count: len(rules), rules: ids}
}

// singleValidate runs VeraPDF on one file. Used as a retry when a file
// is missing from the batch report: the batch run occasionally drops a
// job, and falling back to "no improvement" inflates the disagreement
// metric with phantom residuals.
func singleValidate(ctx context.Context, path, flavour string) (veraVerdict, error) {
	ctx, cancel := context.WithTimeout(ctx, veraPDFSingleTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "verapdf", //nolint:gosec // flavour from a static map, path from our staging dir
		"-f", flavour, "--format", "json", "--maxfailuresdisplayed", "-1", path)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil && stdout.Len() == 0 {
		return veraVerdict{}, fmt.Errorf("verapdf single: %w", err)
	}
	var report verapdfReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return veraVerdict{}, fmt.Errorf("parse verapdf single output: %w", err)
	}
	if len(report.Report.Jobs) == 0 {
		return veraVerdict{}, fmt.Errorf("verapdf single: no job for %s", path)
	}
	job := report.Report.Jobs[0]
	if len(job.ValidationResult) == 0 {
		return veraVerdict{parseFailed: true}, nil
	}
	return jobVerdict(job.ValidationResult[0]), nil
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
		// WTPDF levels ride the ISO 14289-2 rule set; VeraPDF names them
		// wt1a and wt1r. A profile missing from this map is skipped with
		// "no verapdf flavour" even when remediationFeature.remediators
		// supports it, so both maps move together.
		"WTPDF-1.0-Accessibility": "wt1a",
		"WTPDF-1.0-Reuse":         "wt1r",
	}
	v, ok := m[profileDir]
	return v, ok
}

// distinctRuleIDs returns the sorted, deduplicated "clause-tNN" ids of
// the violations, the same identifiers batchValidate derives from
// VeraPDF rule summaries.
func distinctRuleIDs(violations []pdf.Violation) []string {
	seen := make(map[string]bool, len(violations))
	var ids []string
	for _, v := range violations {
		id := fmt.Sprintf("%s-t%02d", v.Clause, v.TestNumber)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func isNew(v pdf.Violation, before []pdf.Violation) bool {
	vs := v.Error()
	for _, b := range before {
		if b.Error() == vs {
			return false
		}
	}
	return true
}
