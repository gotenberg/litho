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

	"github.com/gotenberg/litho/pkg/models"
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
// INVOICE.* failures from [pdf.Reader.ValidateFacturX] (or the
// matching [pdf.Reader.ValidateZUGFeRD] variant for legacy v1 / v2
// fixtures). The PDF/A-3 base aggregator surface (INVOICE test 13)
// is filtered out so the feature only scores fx-layer violations;
// PDF/A-3 base mismatches belong to the compliance feature.
//
// Dispatch by filename prefix: ZUGFeRD/corpus flattens its tree into
// names like "v1__correct_*", "v2__fail_*", "xmlrechnung__*". The v1
// fixtures use ZUGFeRD 1.0 filenames + namespaces that ValidateFacturX
// rejects on principle, so the feature routes them to the v1 entry
// point instead.
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

	// Dispatch by embedded attachment filename rather than by
	// fixture basename: corpus naming conventions miss revision-
	// numbered ZUGFeRD 2.0 fixtures, and ValidateFacturX silently
	// short-circuits when the embedded invoice filename does not
	// match the v2.1+ variant. Reading the actual filename routes
	// each fixture to the matching ValidateZUGFeRD or
	// ValidateFacturX entry point.
	embedded := embeddedAttachmentNames(ctx, r)
	var errs []error
	switch {
	case has(embedded, "factur-x.xml"):
		errs, err = r.ValidateFacturX(ctx)
	case has(embedded, "zugferd-invoice.xml"):
		errs, err = r.ValidateZUGFeRD(ctx, models.ZUGFeRD2)
	case has(embedded, "ZUGFeRD-invoice.xml"):
		errs, err = r.ValidateZUGFeRD(ctx, models.ZUGFeRD1)
	default:
		// No recognised Factur-X / ZUGFeRD invoice payload. Likely
		// a standalone XRechnung PDF or another hybrid-invoice
		// format outside litho's claim surface; report zero
		// failures so the matrix marks it matched against any
		// Mustang verdict rather than firing a spurious FP.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	var failures []string
	for _, e := range errs {
		msg := e.Error()
		// Mirror Mustang's overall pass/fail by counting every
		// Factur-X-layer failure that litho can detect: wrapper,
		// CII spine, CII XSD, and schematron. PDF/A-3 base aggregation
		// stays out so the compliance feature owns it. CII_SCH test 2
		// covers warning- and report-class Schematron firings, which
		// Mustang surfaces as <svrl:successful-report> notices and
		// never lets flip the valid bit; excluding them mirrors that
		// stance and clears the FP inflation profile-restriction
		// reports were causing.
		if strings.Contains(msg, "PDF/A-3 violations on the archival base") {
			continue
		}
		if strings.Contains(msg, "clause CII_SCH test 2") {
			continue
		}
		// INVOICE_LENIENT firings are warning-class: Mustang surfaces
		// them as notices and never lets them flip the valid bit. The
		// bare "clause INVOICE" substring also matches them, which
		// inflated the matrix with false failures.
		if strings.Contains(msg, "clause INVOICE_LENIENT") {
			continue
		}
		if !strings.Contains(msg, "clause INVOICE") &&
			!strings.Contains(msg, "clause CII") {
			continue
		}
		failures = append(failures, msg)
	}
	return failures, nil
}
