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

// mustangReport is the JSON shape that lib/run-mustang.sh emits. One
// jobs[] entry per fixture captures Mustang's overall verdict plus the
// raw text report so the mismatch view can surface failed-rule context
// without re-running the validator.
type mustangReport struct {
	Jobs []mustangJob `json:"jobs"`
}

type mustangJob struct {
	File   string `json:"file"`
	Valid  bool   `json:"valid"`
	Report string `json:"report,omitempty"`
}

// facturxGolden is the per-fixture verdict the matrix uses to score
// litho's [Reader.ValidateFacturX] output. Report carries Mustang's raw
// text so the mismatch payload can quote whichever rules tripped.
type facturxGolden struct {
	Valid  bool
	Report string
}

// facturxFeature scores [Reader.ValidateFacturX] against Mustang. The
// PDF/A-3 archival base layer is scored separately by the existing
// compliance feature against VeraPDF; this feature isolates the
// hybrid-invoice carrier rules (filename, AFRelationship, fx:* XMP,
// extension schema, embedded CII XSD).
//
// Run memoises by PDFPath so corpora that expand each PDF into multiple
// (pdf, profile) fixtures only pay the litho validation cost once.
type facturxFeature struct {
	mu      sync.RWMutex
	goldens map[string]*facturxGolden
	cache   sync.Map // pdfPath -> *cachedRun
}

func newFacturxFeature() *facturxFeature {
	return &facturxFeature{goldens: make(map[string]*facturxGolden)}
}

func (f *facturxFeature) Name() string         { return "facturx-validity" }
func (f *facturxFeature) GoldenSource() string { return "mustang" }

// LoadGoldens reads the jobs.json emitted by lib/run-mustang.sh. The
// golden dir is opt-in via the corpus manifest, so corpora that do not
// declare the feature get [ErrNoGoldens] and render as "[no golden]".
func (f *facturxFeature) LoadGoldens(dir string) error {
	jobsPath := filepath.Join(dir, "jobs.json")
	data, err := os.ReadFile(jobsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoGoldens
		}
		return fmt.Errorf("read %s: %w", jobsPath, err)
	}
	var report mustangReport
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
		f.goldens[base] = &facturxGolden{Valid: job.Valid, Report: job.Report}
	}
	return nil
}

func (f *facturxFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.goldens[baseFixtureID(fixtureID)]
	return ok
}

// facturxOutput is litho's verdict captured by [Run]. Failures is the
// list of INVOICE.* rule descriptions; the PDF/A-3 base aggregator
// (INVOICE test 13) is filtered out so this feature only scores the
// fx-layer rules. The base PDF/A-3 layer is the compliance feature's
// job.
type facturxOutput struct {
	Valid    bool     `json:"valid"`
	Failures []string `json:"failures,omitempty"`
}

func (f *facturxFeature) Run(ctx context.Context, pdfPath, _ string) ([]byte, error) {
	if v, ok := f.cache.Load(pdfPath); ok {
		cr := v.(*cachedRun)
		return cr.output, cr.err
	}
	failures, err := validateFacturXWithLitho(ctx, pdfPath)
	if err != nil {
		f.cache.Store(pdfPath, &cachedRun{err: err})
		return nil, err
	}
	out, err := json.Marshal(facturxOutput{
		Valid:    len(failures) == 0,
		Failures: failures,
	})
	f.cache.Store(pdfPath, &cachedRun{output: out, err: err})
	return out, err
}

func (f *facturxFeature) Compare(fixtureID string, output []byte) Verdict {
	f.mu.RLock()
	golden := f.goldens[baseFixtureID(fixtureID)]
	f.mu.RUnlock()
	if golden == nil {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no golden"}
	}

	var got facturxOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}

	match := got.Valid == golden.Valid
	metrics := map[string]int{}
	reason := ""
	if !match {
		if got.Valid && !golden.Valid {
			metrics["false_negative"] = 1
			reason = "false negative: litho valid, Mustang invalid"
		} else {
			metrics["false_positive"] = 1
			reason = "false positive: litho invalid, Mustang valid"
		}
	}

	score := 0.0
	if match {
		score = 1.0
	}

	var mismatches []json.RawMessage
	if !match {
		raw, _ := json.Marshal(facturxMismatch{
			File:          fixtureID,
			MustangValid:  golden.Valid,
			LithoValid:    got.Valid,
			MustangReport: golden.Report,
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

// facturxMismatch pairs Mustang's report with litho's INVOICE.* failure
// list so the scoreboard can show both sides of a disagreement.
type facturxMismatch struct {
	File          string   `json:"file"`
	MustangValid  bool     `json:"mustang_valid"`
	LithoValid    bool     `json:"litho_valid"`
	MustangReport string   `json:"mustang_report,omitempty"`
	LithoFailures []string `json:"litho_failures,omitempty"`
}

// validateFacturXWithLitho opens pdfPath and returns the list of
// INVOICE.* failures from [pdf.Reader.ValidateFacturX]. The PDF/A-3
// base aggregator surface (INVOICE test 13) is filtered out so the
// feature only scores fx-layer violations; PDF/A-3 base mismatches
// belong to the compliance feature.
func validateFacturXWithLitho(ctx context.Context, pdfPath string) ([]string, error) {
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
		if !strings.Contains(msg, "clause INVOICE") {
			continue
		}
		// The aggregator that flags "N PDF/A-3 violations on the
		// archival base" duplicates the compliance feature's verdict;
		// keep this cell focused on the fx-layer rules.
		if strings.Contains(msg, "PDF/A-3 violations on the archival base") {
			continue
		}
		failures = append(failures, msg)
	}
	return failures, nil
}
