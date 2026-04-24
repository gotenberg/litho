package main

import (
	"bytes"
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

// remediationGolden stores the expected delta for one (fixture, profile,
// aggressive?) pairing. Pre is the violation count before remediation;
// Post is what litho produced last time the golden was regenerated. The
// feature asserts that Post matches exactly so both regressions (more
// violations) and unreported improvements (fewer violations) surface.
type remediationGolden struct {
	Profile    string `json:"profile"`
	Aggressive bool   `json:"aggressive"`
	Pre        int    `json:"pre"`
	Post       int    `json:"post"`
}

// remediationFeature applies the remediation engine to a fixture for
// its declared profile and compares the resulting violation count to the
// golden. One instance runs in default mode; a second instance with
// aggressive=true handles content-modifying fixers.
type remediationFeature struct {
	aggressive bool
	mu         sync.RWMutex
	goldens    map[string]*remediationGolden
}

func newRemediationFeature(aggressive bool) *remediationFeature {
	return &remediationFeature{
		aggressive: aggressive,
		goldens:    make(map[string]*remediationGolden),
	}
}

func (f *remediationFeature) Name() string {
	if f.aggressive {
		return "remediation-aggressive"
	}
	return "remediation"
}

func (f *remediationFeature) GoldenSource() string { return "litho-self" }

// LoadGoldens reads one JSON file per fixture from dir. Missing dir is
// treated as [ErrNoGoldens] so the runner can print "[no golden]" and
// emit a bootstrap hint rather than fail.
func (f *remediationFeature) LoadGoldens(dir string) error {
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
		var g remediationGolden
		if err := json.Unmarshal(data, &g); err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		f.mu.Lock()
		f.goldens[id] = &g
		f.mu.Unlock()
		loaded++
	}
	if loaded == 0 {
		return ErrNoGoldens
	}
	return nil
}

func (f *remediationFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.goldens[fixtureID]
	return ok
}

// remediationOutput is litho's pre/post violation counts for the
// fixture's target profile.
type remediationOutput struct {
	Profile    string   `json:"profile"`
	Aggressive bool     `json:"aggressive"`
	Pre        int      `json:"pre"`
	Post       int      `json:"post"`
	Error      string   `json:"error,omitempty"`
	NewErrors  []string `json:"new_errors,omitempty"`
}

func (f *remediationFeature) Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error) {
	f.mu.RLock()
	golden := f.goldens[fixtureID]
	f.mu.RUnlock()

	profile := profileFromFixtureID(fixtureID)
	if golden != nil && golden.Profile != "" {
		profile = models.PDFAProfile(golden.Profile)
	}
	if profile == "" {
		return json.Marshal(remediationOutput{
			Aggressive: f.aggressive,
			Error:      "no profile target for fixture",
		})
	}

	out := remediationOutput{Profile: string(profile), Aggressive: f.aggressive}
	if err := runRemediation(ctx, pdfPath, profile, f.aggressive, &out); err != nil {
		out.Error = err.Error()
	}
	return json.Marshal(out)
}

func (f *remediationFeature) Compare(fixtureID string, output []byte) Verdict {
	f.mu.RLock()
	golden := f.goldens[fixtureID]
	f.mu.RUnlock()

	if golden == nil {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no golden"}
	}

	var got remediationOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}

	if got.Error != "" {
		return Verdict{FixtureID: fixtureID, Reason: got.Error}
	}

	metrics := map[string]int{
		"pre":  got.Pre,
		"post": got.Post,
	}
	if got.Post < got.Pre {
		metrics["fixed"] = got.Pre - got.Post
	}
	if got.Post == 0 {
		metrics["fully_fixed"] = 1
	}

	match := got.Pre == golden.Pre && got.Post == golden.Post
	score := 0.0
	if match {
		score = 1.0
	}

	reason := ""
	var mismatches []json.RawMessage
	if !match {
		switch {
		case got.Post > golden.Post:
			reason = fmt.Sprintf("regression: %d → %d (golden: %d → %d)", got.Pre, got.Post, golden.Pre, golden.Post)
			metrics["regression"] = 1
		case got.Post < golden.Post:
			reason = fmt.Sprintf("improved: %d → %d (golden: %d → %d); regenerate golden", got.Pre, got.Post, golden.Pre, golden.Post)
			metrics["improved"] = 1
		default:
			reason = fmt.Sprintf("pre-violation drift: %d vs golden %d", got.Pre, golden.Pre)
			metrics["drifted_pre"] = 1
		}
		raw, _ := json.Marshal(remediationMismatch{
			File:       fixtureID,
			Profile:    got.Profile,
			Aggressive: f.aggressive,
			LithoPre:   got.Pre,
			LithoPost:  got.Post,
			GoldenPre:  golden.Pre,
			GoldenPost: golden.Post,
			NewErrors:  got.NewErrors,
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

type remediationMismatch struct {
	File       string   `json:"file"`
	Profile    string   `json:"profile"`
	Aggressive bool     `json:"aggressive"`
	LithoPre   int      `json:"litho_pre"`
	LithoPost  int      `json:"litho_post"`
	GoldenPre  int      `json:"golden_pre"`
	GoldenPost int      `json:"golden_post"`
	NewErrors  []string `json:"new_errors,omitempty"`
}

// runRemediation opens pdfPath, validates for profile, applies the
// remediation engine, writes the result to a buffer, re-opens + re-
// validates, and populates out with pre/post counts.
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
	out.Pre = len(pre)

	w := pdf.NewWriter(r)
	var opts []pdf.RemediateOption
	if aggressive {
		opts = append(opts, pdf.WithAggressiveFixes())
	}
	if _, err := w.RemediatePDFA(ctx, profile, opts...); err != nil {
		// Treat remediation failure as "post == pre" (no improvement).
		out.Post = out.Pre
		return nil
	}

	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		out.Post = out.Pre
		return nil
	}
	r2, err := pdf.Open(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		out.Post = out.Pre
		return nil
	}
	post, err := r2.ValidatePDFA(ctx, profile)
	if err != nil {
		out.Post = out.Pre
		return nil
	}
	out.Post = len(post)
	if len(post) > len(pre) {
		for _, v := range post {
			if isNew(v, pre) {
				out.NewErrors = append(out.NewErrors, v.Error())
			}
		}
	}
	return nil
}

// profileFromFixtureID derives the PDF/A profile from the leading path
// component of the fixture ID. Returns "" when the prefix is not a
// known profile directory (e.g. pdf.js fixtures have no profile).
func profileFromFixtureID(fixtureID string) models.PDFAProfile {
	head, _, ok := strings.Cut(fixtureID, "/")
	if !ok {
		return ""
	}
	if p, ok := dirToPDFAProfile[head]; ok {
		return p
	}
	return ""
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
