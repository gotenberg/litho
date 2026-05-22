package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/models"
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

// embeddedAttachmentNames returns the filename of every entry the
// reader sees as an embedded file. Used by [validateFacturXFailures]
// to pick the right validator entry point based on whether the PDF
// carries a ZUGFeRD 1.0 ("ZUGFeRD-invoice.xml"), ZUGFeRD 2.0
// ("zugferd-invoice.xml"), or Factur-X 1.0+ / ZUGFeRD 2.1+
// ("factur-x.xml") payload.
func embeddedAttachmentNames(ctx context.Context, r *pdf.Reader) []string {
	files, err := r.EmbeddedFiles(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Name)
	}
	return out
}

// has reports whether name appears in the list (case-sensitive,
// matching the variants' on-the-wire filenames).
func has(list []string, name string) bool {
	return slices.Contains(list, name)
}

// validateFacturXFailures opens pdfPath, runs the appropriate
// validator entry point, and returns only the Schematron-clause
// rule descriptions. INVOICE-class failures (filename, AF, XMP,
// embedded stream Subtype) belong to the facturx-validity feature,
// and PDF/A-3 base failures belong to the compliance feature; both
// stay out of this view so a single fixture's defects do not
// double-count across cells.
//
// Dispatch by ZUGFeRD generation: ValidateFacturX targets the v2.1+
// (urn:factur-x.eu:1p0:*) variant and silently skips the schematron
// pass when the embedded invoice filename does not match. Legacy
// ZUGFeRD 1.0 and 2.0 fixtures use distinct filenames and namespaces
// and need the matching entry point; the corpus encodes the
// generation in the file basename ("v1__", "...2p0...") so the
// dispatch reads from there.
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

	// Dispatch by embedded filename: each variant expects a
	// different /AF entry, and only the matching ValidateZUGFeRD/
	// ValidateFacturX call runs the schematron pass. Filename-based
	// dispatch (over basename-prefix heuristics) catches fixtures
	// like Mustang's revision-numbered ZUGFeRD 2.0 samples that
	// don't follow the corpus's "zugferd_2p0_" naming.
	embedded := embeddedAttachmentNames(ctx, r)
	var errs []error
	switch {
	case has(embedded, "ZUGFeRD-invoice.xml") && strings.HasPrefix(filepath.Base(pdfPath), "v1__"):
		errs, err = r.ValidateZUGFeRD(ctx, models.ZUGFeRD1)
	case has(embedded, "zugferd-invoice.xml"):
		errs, err = r.ValidateZUGFeRD(ctx, models.ZUGFeRD2)
	case has(embedded, "ZUGFeRD-invoice.xml"):
		errs, err = r.ValidateZUGFeRD(ctx, models.ZUGFeRD1)
	default:
		errs, err = r.ValidateFacturX(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	var failures []string
	for _, e := range errs {
		msg := e.Error()
		if !strings.Contains(msg, "clause CII_SCH") {
			continue
		}
		// CII_SCH test 2 covers warning- and report-class firings.
		// KoSIT's validator returns rc=0 (pass) for warnings, so
		// counting them here turns flag="warning" rules (notably the
		// PEPPOL-EN16931 "Document MUST not contain empty elements"
		// advisory) into spurious false positives.
		if strings.Contains(msg, "clause CII_SCH test 2") {
			continue
		}
		failures = append(failures, msg)
	}
	return failures, nil
}
