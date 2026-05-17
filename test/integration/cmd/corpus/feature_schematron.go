package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// kositReport mirrors the JSON shape emitted by lib/run-kosit.sh.
type kositReport struct {
	Jobs []kositJob `json:"jobs"`
}

type kositJob struct {
	File    string   `json:"file"`
	Verdict string   `json:"verdict"` // "pass" | "fail" | "skip"
	Reason  string   `json:"reason,omitempty"`
	Report  string   `json:"report,omitempty"`
	Rules   []string `json:"rules,omitempty"`
}

// schematronGolden captures KoSIT's verdict for one fixture plus the
// rule IDs that fired on a failure. Skip verdicts surface separately
// so the matrix can show "[not applicable]" rather than counting them
// against the score.
type schematronGolden struct {
	Verdict string
	Reason  string
	Report  string
	Rules   []string
}

// schematronFeature scores [Reader.ValidateFacturX] against KoSIT. The
// XRechnung scenarios bundle KoSIT loads routes each fixture to either
// XRechnung Schematron, generic EN 16931 CII Schematron, or a "no
// scenario applicable" outcome. The cell stays focused on Schematron
// rules; the PDF/A-3 base (VeraPDF) and PDF wrapper (Mustang) layers
// score separately in their own cells.
type schematronFeature struct {
	mu      sync.RWMutex
	goldens map[string]*schematronGolden
	cache   sync.Map // pdfPath -> *cachedRun
}

func newSchematronFeature() *schematronFeature {
	return &schematronFeature{goldens: make(map[string]*schematronGolden)}
}

func (f *schematronFeature) Name() string         { return "facturx-schematron" }
func (f *schematronFeature) GoldenSource() string { return "kosit" }

func (f *schematronFeature) LoadGoldens(dir string) error {
	jobsPath := filepath.Join(dir, "jobs.json")
	data, err := os.ReadFile(jobsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoGoldens
		}
		return fmt.Errorf("read %s: %w", jobsPath, err)
	}
	var report kositReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("decode %s: %w", jobsPath, err)
	}
	if len(report.Jobs) == 0 {
		return ErrNoGoldens
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, job := range report.Jobs {
		base := strings.TrimSuffix(job.File, ".pdf")
		f.goldens[base] = &schematronGolden{
			Verdict: job.Verdict,
			Reason:  job.Reason,
			Report:  job.Report,
			Rules:   job.Rules,
		}
	}
	return nil
}

func (f *schematronFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.goldens[baseFixtureID(fixtureID)]
	return ok
}

// schematronOutput is litho's verdict captured by Run. Failures lists
// the INVOICE-or-CII rule descriptions found, since either rule class
// is a legitimate Schematron-equivalent violation surface from
// litho's vantage point.
type schematronOutput struct {
	Valid    bool     `json:"valid"`
	Failures []string `json:"failures,omitempty"`
}

func (f *schematronFeature) Run(ctx context.Context, pdfPath, _ string) ([]byte, error) {
	if v, ok := f.cache.Load(pdfPath); ok {
		cr := v.(*cachedRun)
		return cr.output, cr.err
	}
	failures, err := validateFacturXFailures(ctx, pdfPath)
	if err != nil {
		f.cache.Store(pdfPath, &cachedRun{err: err})
		return nil, err
	}
	out, err := json.Marshal(schematronOutput{
		Valid:    len(failures) == 0,
		Failures: failures,
	})
	f.cache.Store(pdfPath, &cachedRun{output: out, err: err})
	return out, err
}

func (f *schematronFeature) Compare(fixtureID string, output []byte) Verdict {
	f.mu.RLock()
	golden := f.goldens[baseFixtureID(fixtureID)]
	f.mu.RUnlock()
	if golden == nil {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no golden"}
	}
	if golden.Verdict == "skip" {
		reason := golden.Reason
		if reason == "" {
			reason = "kosit: no scenario applicable"
		}
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: reason}
	}

	var got schematronOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}

	kositValid := golden.Verdict == "pass"
	match := got.Valid == kositValid
	metrics := map[string]int{}
	reason := ""
	if !match {
		if got.Valid && !kositValid {
			metrics["false_negative"] = 1
			reason = "false negative: litho valid, KoSIT invalid"
		} else {
			metrics["false_positive"] = 1
			reason = "false positive: litho invalid, KoSIT valid"
		}
	}

	score := 0.0
	if match {
		score = 1.0
	}

	var mismatches []json.RawMessage
	if !match {
		raw, _ := json.Marshal(schematronMismatch{
			File:          fixtureID,
			KositVerdict:  golden.Verdict,
			LithoValid:    got.Valid,
			KositRules:    golden.Rules,
			KositReport:   golden.Report,
			LithoFailures: got.Failures,
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

// schematronMismatch pairs KoSIT's failed-rule list with litho's
// INVOICE-or-CII rule list for the scoreboard's diff view. The full
// KoSIT report is included so post-mortems do not require re-running
// the oracle.
type schematronMismatch struct {
	File          string   `json:"file"`
	KositVerdict  string   `json:"kosit_verdict"`
	LithoValid    bool     `json:"litho_valid"`
	KositRules    []string `json:"kosit_rules,omitempty"`
	KositReport   string   `json:"kosit_report,omitempty"`
	LithoFailures []string `json:"litho_failures,omitempty"`
}

// validateFacturXFailures opens pdfPath, runs ValidateFacturX, and
// returns the INVOICE-or-CII rule descriptions. The PDF/A-3 base
// aggregator surface is filtered out so this feature only scores
// rule classes that overlap with Schematron checks.
func validateFacturXFailures(ctx context.Context, pdfPath string) ([]string, error) {
	f, err := os.Open(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}

	r, err := pdf.Open(ctx, f, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return nil, fmt.Errorf("pdf.Open: %w", err)
	}
	defer r.Close()

	errs, err := r.ValidateFacturX(ctx)
	if err != nil {
		return nil, fmt.Errorf("ValidateFacturX: %w", err)
	}
	var failures []string
	for _, e := range errs {
		msg := e.Error()
		if !strings.Contains(msg, "clause INVOICE") && !strings.Contains(msg, "clause CII") {
			continue
		}
		if strings.Contains(msg, "PDF/A-3 violations on the archival base") {
			continue
		}
		failures = append(failures, msg)
	}
	return failures, nil
}
