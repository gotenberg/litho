package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"math"
	"os"
	"runtime"
	"sort"
	"time"
)

// Baseline captures the matrix's per-cell match counts in a stable,
// machine-comparable form. Lives in-repo (typically at
// test/integration/baseline.json) and acts as the regression oracle
// for `corpus check`: any cell whose current matched count drops
// below the baseline is treated as a regression.
//
// Only the counts that gate pass/fail behaviour are captured. Per-
// fixture verdicts and mismatch payloads are deliberately not stored
// because they grow without bound and change for non-load-bearing
// reasons (e.g. the order of mismatches in a textual diff).
type Baseline struct {
	Version    int                       `json:"version"`
	CapturedAt string                    `json:"captured_at"`
	Corpora    map[string]baselineCorpus `json:"corpora"`
}

type baselineCorpus struct {
	Features map[string]baselineFeature `json:"features"`
}

type baselineFeature struct {
	Matched    int            `json:"matched"`
	Mismatched int            `json:"mismatched"`
	Skipped    int            `json:"skipped"`
	Metrics    map[string]int `json:"metrics,omitempty"`

	// Outcomes maps each fixture to its outcome, for a feature that records
	// them (see [OutcomeFeature]). A fixture that passed in the baseline and
	// does not now is a regression.
	Outcomes map[string]string `json:"outcomes,omitempty"`
	// Scores maps each fixture to its named scores, for a feature that
	// records them (see [ScoreFeature]), and ScoreTolerance is the drop the
	// check forgives each one.
	Scores         map[string]map[string]float64 `json:"scores,omitempty"`
	ScoreTolerance float64                       `json:"score_tolerance,omitempty"`
}

const baselineCurrentVersion = 1

// costMetrics lists metrics where lower is better, inverting the
// regression comparison. Everything else (chars_match, fully_fixed,
// verapdf_fixed, ...) is good-when-higher.
//
//nolint:gochecknoglobals // static direction table for the comparator
var costMetrics = map[string]bool{
	"false_negative":    true,
	"false_positive":    true,
	"parse_failed":      true,
	"verapdf_regressed": true,
	"litho_regressed":   true,
	"disagreement":      true,
	"decode_failed":     true,
	metricRenderFailed:  true,
	// The render cell's size disagreements, split out of render_failed.
	metricRenderSizeMismatch: true,

	// The split catalog invariants count VIOLATIONS, so more is worse. Without
	// an entry here the default good-when-higher rule would report split
	// inventing an /AcroForm on more fixtures as an improvement, which is the
	// one reading that would let the defect these counters exist to catch pass
	// as progress. Their *_checked siblings stay on the default rule on
	// purpose: those count fixtures exercised, so a drop is lost coverage and
	// belongs in the regression column.
	"acroform_invented": true,
	"portfolio_dropped": true,
	"lexicons_dropped":  true,

	// The compliance cells whose failed-rule set differs from veraPDF's. A
	// rise is a detection change the verdict comparison in matched cannot
	// see, because a fixture that breaks many rules stays non-compliant on
	// both sides whatever rule class litho drops or invents.
	"ruleset_mismatch": true,

	// The mutate-* features count the outputs that failed a check, so more is
	// worse. See mutationFeature for what each check asks of an output.
	metricWriteFailed:      true,
	metricReopenFailed:     true,
	metricQPDFFailed:       true,
	metricPagesWrong:       true,
	metricTextWrong:        true,
	metricRulesetChanged:   true,
	metricPromiseBroken:    true,
	metricSignatureInvalid: true,

	// The determinism cell counts the (PDF, operation) cells whose two runs
	// wrote different bytes, and the pages its render pass drew different
	// pixels of; render_pages_checked, the pass's coverage, stays gated as
	// good-when-higher. See determinismFeature.
	metricNondeterministic:            true,
	metricRenderPagesNondeterministic: true,

	// The redact-render cell counts the redactions a check failed on, the
	// ones that failed outright, and the ones whose poppler half did not run,
	// which is no pass. See redactRenderFeature.
	metricOutsideChanged:        true,
	metricInsideLeaked:          true,
	metricOutsideChangedLitho:   true,
	metricOutsideChangedPoppler: true,
	metricInsideLeakedLitho:     true,
	metricInsideLeakedPoppler:   true,
	metricRedactFailed:          true,
	metricPopplerUnavailable:    true,
	// Its raster-fallback scenarios count the same failures;
	// fallback_scenarios_run and rasterised_units, their coverage, stay
	// gated as good-when-higher.
	metricFallbackOutsideChanged: true,
	metricFallbackInsideLeaked:   true,

	// The recolor cell counts the conversions that failed and the pages a
	// check failed on. See recolorFeature.
	metricRecolorFailed: true,
	metricOutOfTarget:   true,
	metricInkMoved:      true,
	metricColourDrift:   true,
	// Its JPEG run counts the same, and the outputs larger than the lossless
	// run's; jpeg_files and jpeg_pages_checked, its coverage, stay gated as
	// good-when-higher.
	metricJPEGFailed:      true,
	metricJPEGGrew:        true,
	metricJPEGOutOfTarget: true,
	metricJPEGInkMoved:    true,
	metricJPEGColourDrift: true,

	// The flatten cell counts the flattenings that failed and the PDFs and
	// pages a check failed on. See flattenFeature.
	metricFlattenFailed:           true,
	metricFlattenLithoMismatch:    true,
	metricFlattenPopplerMismatch:  true,
	metricFlattenTextChanged:      true,
	metricFlattenTransparencyLeft: true,

	// The converter cells count the fixtures litho failed to convert or read
	// back. pages_equal, the diff cell's count of fixtures with Chromium's
	// page count, stays gated as good-when-higher.
	metricConvertError: true,

	// The targets cell counts the (fixture, target) cells whose conversion
	// report disagrees with litho's validator, whose oracles disagree with
	// litho or did not run, and whose version pdfcpu refuses. Its pass_<target>
	// counts and oracle_targets, the sample's coverage, stay gated as
	// good-when-higher. See targetsFeature.
	metricReportDisagrees:     true,
	metricVeraPDFDisagrees:    true,
	metricVeraPDFUnavailable:  true,
	metricRendererDisagrees:   true,
	metricVersionStrictFailed: true,
	metricPdfcpuUnavailable:   true,
}

// uncomparedMetrics lists raw inventory totals that shift with any
// detection-coverage change and carry no pass/fail meaning of their
// own; they stay in the baseline for context but are not gated.
//
//nolint:gochecknoglobals // static skip table for the comparator
var uncomparedMetrics = map[string]bool{
	"litho_pre":     true,
	"litho_post":    true,
	"litho_fixed":   true,
	"verapdf_pre":   true,
	"verapdf_post":  true,
	"verapdf_fixed": true,

	// The mutate-sign inventory: the unsigned signature fields it signed in
	// place and the ones whose own /Lock or seed values refused it. They count
	// what the run reached, and the qpdf the fields are read with is
	// optional, so a run without it reads 0 in both. A defect in what they
	// count fails promise_broken or signature_invalid instead.
	metricFieldSigned:  true,
	metricFieldRefused: true,

	// The redact-render inventory: refusals are the documented outcome for
	// content a redaction cannot remove exactly, poppler_no_oracle counts
	// source pages poppler draws apart from litho, image_spill the
	// by-design reach of sample-exact blanking, and interpolation_spill the
	// kept samples an interpolated image blends into its blank inside the
	// region. A defect in what they count fails outside_changed or
	// inside_leaked instead; scenarios_run, the coverage, stays gated as
	// good-when-higher.
	metricRedactRefused:      true,
	metricRedactIncomplete:   true,
	metricPopplerNoOracle:    true,
	metricImageSpill:         true,
	metricInterpolationSpill: true,
	// The fallback scenarios' inventory, read the same way:
	// fallback_litho_reading and fallback_resampled count scenarios where
	// pixels changed outside show litho's own rendering of the source, which
	// the rasterised image holds, or the image's resampling of it, both by
	// design. fallback_strict_changed names, among the scenarios
	// fallback_outside_changed gates, those litho's stricter tolerance
	// failed where the images paint.
	metricFallbackStrictChanged:   true,
	metricFallbackRefused:         true,
	metricFallbackIncomplete:      true,
	metricFallbackPopplerNoOracle: true,
	metricFallbackLithoReading:    true,
	metricFallbackResampled:       true,

	// The recolor inventory: a refusal is the documented answer for a colour
	// the conversion cannot convert, and a group blending space is no colour
	// a page paints. A defect fails out_of_target or colour_drift instead;
	// pages_checked, the coverage, stays gated as good-when-higher.
	metricRecolorRefused:   true,
	metricGroupOutOfTarget: true,
	// The bytes the JPEG run saves move with any encoder change, which
	// jpeg_grew bounds from the side that matters.
	metricJPEGSavedKiB: true,

	// The diff fixtures left unscored because their reference draws a face
	// no fixture font stands in for: an inventory of the references.
	metricReferenceFontsMissing: true,
}

// defaultBaselinePath returns the canonical baseline location. The
// in-repo file lives under test/integration so the matrix tool, the
// Makefile, and CI all point at the same artifact.
func defaultBaselinePath() string {
	for _, candidate := range []string{
		"test/integration/baseline.json",
		"../../baseline.json", // when invoked from inside test/integration/cmd/corpus
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "test/integration/baseline.json"
}

func runBaseline(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("baseline", flag.ExitOnError)
	corpus := fs.String("corpus", "", corpusFlagUsage)
	feature := fs.String("feature", "", "Comma-separated feature names (empty = all)")
	output := fs.String("output", defaultBaselinePath(), "Baseline file path to write")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "Parallel fixture workers")
	allowDrift := fs.Bool("allow-version-drift", false, allowVersionDriftUsage)
	reportDir := fs.String("report-dir", defaultReportDir, reportDirUsage)
	oracleSample := fs.Int("oracle-sample", defaultOracleSample, oracleSampleUsage)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}

	opts := runOptions{
		corpora:           splitCSV(*corpus),
		features:          splitCSV(*feature),
		workers:           *workers,
		allowVersionDrift: *allowDrift,
		reportDir:         *reportDir,
		oracleSample:      *oracleSample,
	}
	if opts.workers < 1 {
		opts.workers = 1
	}

	board, ok := buildScoreboard(ctx, opts)
	if !ok {
		return 1
	}

	// A run narrowed to some corpora or features overlays its cells onto
	// the baseline: it scored nothing else, so it replaces nothing else.
	bl := baselineFromScoreboard(board)
	if len(opts.corpora) > 0 || len(opts.features) > 0 {
		prev, err := loadBaseline(*output)
		switch {
		case err == nil:
			bl = mergeBaseline(prev, bl)
		case errors.Is(err, os.ErrNotExist):
			// First capture: there is nothing to preserve.
		default:
			fmt.Fprintf(os.Stderr, "load %s for merge: %v\n", *output, err)
			return 1
		}
	}
	data, err := json.MarshalIndent(bl, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode baseline: %v\n", err)
		return 1
	}
	data = append(data, '\n')
	if err := os.WriteFile(*output, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *output, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "wrote baseline (%d corpora) to %s\n", len(bl.Corpora), *output)
	return 0
}

func runCheck(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	corpus := fs.String("corpus", "", corpusFlagUsage)
	feature := fs.String("feature", "", "Comma-separated feature names (empty = all)")
	baselinePath := fs.String("baseline", defaultBaselinePath(), "Baseline file to check against")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "Parallel fixture workers")
	allowDrift := fs.Bool("allow-version-drift", false, allowVersionDriftUsage)
	reportDir := fs.String("report-dir", defaultReportDir, reportDirUsage)
	oracleSample := fs.Int("oracle-sample", defaultOracleSample, oracleSampleUsage)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}

	bl, err := loadBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load baseline %s: %v\n", *baselinePath, err)
		return 1
	}

	opts := runOptions{
		corpora:           splitCSV(*corpus),
		features:          splitCSV(*feature),
		workers:           *workers,
		allowVersionDrift: *allowDrift,
		reportDir:         *reportDir,
		oracleSample:      *oracleSample,
	}
	if opts.workers < 1 {
		opts.workers = 1
	}

	board, ok := buildScoreboard(ctx, opts)
	if !ok {
		return 1
	}

	regressions, improvements, missing := diffBaseline(bl, baselineFromScoreboard(board))

	w := os.Stdout
	for _, line := range improvements {
		fmt.Fprintf(w, "improvement: %s\n", line)
	}
	for _, line := range missing {
		fmt.Fprintf(w, "missing in current run: %s\n", line)
	}
	for _, line := range regressions {
		fmt.Fprintf(w, "REGRESSION: %s\n", line)
	}

	if len(regressions) > 0 {
		fmt.Fprintf(os.Stderr, "%d regression(s) vs %s\n", len(regressions), *baselinePath)
		return 1
	}
	fmt.Fprintf(os.Stderr, "no regressions vs %s (improvements=%d, missing=%d)\n",
		*baselinePath, len(improvements), len(missing))
	return 0
}

// baselineFromScoreboard captures the matched/mismatched/skipped
// counts (and named metrics) the regression check actually compares.
// Features that loaded no goldens are skipped: there is nothing to
// compare against, and capturing them would pin "no golden" as a
// regression state.
func baselineFromScoreboard(board *Scoreboard) *Baseline {
	bl := &Baseline{
		Version:    baselineCurrentVersion,
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
		Corpora:    make(map[string]baselineCorpus, len(board.Corpora)),
	}
	for _, c := range board.Corpora {
		feats := make(map[string]baselineFeature, len(c.Features))
		for _, fr := range c.Features {
			if fr.NoGolden || fr.LoadError != "" {
				continue
			}
			bf := baselineFeature{
				Matched:    fr.Matched,
				Mismatched: fr.Mismatched,
				Skipped:    fr.Skipped,
			}
			if len(fr.Metrics) > 0 {
				bf.Metrics = make(map[string]int, len(fr.Metrics))
				maps.Copy(bf.Metrics, fr.Metrics)
			}
			recordPerFixture(&bf, fr)
			feats[fr.Name] = bf
		}
		if len(feats) > 0 {
			bl.Corpora[c.Name] = baselineCorpus{Features: feats}
		}
	}
	return bl
}

// recordPerFixture copies the per-fixture outcomes and scores of a cell whose
// feature records them into bf. A skipped verdict records nothing.
func recordPerFixture(bf *baselineFeature, fr *FeatureReport) {
	if fr.RecordsOutcomes {
		bf.Outcomes = make(map[string]string, len(fr.Verdicts))
		for _, v := range fr.Verdicts {
			if v.Skipped {
				continue
			}
			outcome := v.Outcome
			if outcome == "" {
				outcome = outcomeError
			}
			bf.Outcomes[v.FixtureID] = outcome
		}
	}
	if fr.ScoreTolerance > 0 {
		bf.ScoreTolerance = fr.ScoreTolerance
		bf.Scores = make(map[string]map[string]float64, len(fr.Verdicts))
		for _, v := range fr.Verdicts {
			if v.Skipped {
				continue
			}
			scores := make(map[string]float64, len(v.Scores))
			for k, val := range v.Scores {
				scores[k] = roundScore(val)
			}
			bf.Scores[v.FixtureID] = scores
		}
	}
}

// scoreDecimals is how many decimals a recorded score keeps: the baseline
// tolerance is a few hundredths, so a fourth decimal is noise that would
// churn the file on every capture.
const scoreDecimals = 1e4

// roundScore rounds a score to scoreDecimals.
func roundScore(v float64) float64 {
	return math.Round(v*scoreDecimals) / scoreDecimals
}

// mergeBaseline overlays the freshly measured corpora in fresh onto prev,
// replacing whole (corpus, feature) cells and preserving every cell the run
// did not measure.
//
// A run scoped with -corpus or -feature sees only the cells it asked for, and
// [runBaseline] writes the whole file, so capturing one corpus would silently
// drop the other six. Merging keeps a scoped refresh safe, which is what makes
// locking in an improvement cheap enough to do after every landed fix.
//
// Replacement is per feature, not per metric: a feature that stops emitting a
// metric must not keep gating on the stale value.
func mergeBaseline(prev, fresh *Baseline) *Baseline {
	out := &Baseline{
		Version:    fresh.Version,
		CapturedAt: fresh.CapturedAt,
		Corpora:    make(map[string]baselineCorpus, len(prev.Corpora)+len(fresh.Corpora)),
	}
	for name, corpus := range prev.Corpora {
		features := make(map[string]baselineFeature, len(corpus.Features))
		maps.Copy(features, corpus.Features)
		out.Corpora[name] = baselineCorpus{Features: features}
	}
	for name, corpus := range fresh.Corpora {
		merged, ok := out.Corpora[name]
		if !ok {
			merged = baselineCorpus{Features: make(map[string]baselineFeature, len(corpus.Features))}
			out.Corpora[name] = merged
		}
		maps.Copy(merged.Features, corpus.Features)
	}
	return out
}

func loadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bl Baseline
	if err := json.Unmarshal(data, &bl); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if bl.Version != baselineCurrentVersion {
		return nil, fmt.Errorf("unsupported baseline version %d (want %d)", bl.Version, baselineCurrentVersion)
	}
	return &bl, nil
}

// diffBaseline compares want (the stored baseline) against got (the
// current run's distilled baseline). Three slices come back:
//
//   - regressions: cells where Matched dropped, or where a Metric in
//     the baseline lost ground. These fail the check.
//   - improvements: cells where Matched grew. Informational; refresh
//     the baseline to lock them in.
//   - missing: baseline cells that did not appear in the current run
//     (corpus unprovisioned, feature filtered out). Non-fatal but
//     surfaced so the runner can choose to re-provision.
//
// A cell that records per-fixture outcomes or scores is also compared
// fixture by fixture (see [diffPerFixture]).
func diffBaseline(want, got *Baseline) (regressions, improvements, missing []string) {
	corpusNames := make([]string, 0, len(want.Corpora))
	for name := range want.Corpora {
		corpusNames = append(corpusNames, name)
	}
	sort.Strings(corpusNames)

	for _, corpus := range corpusNames {
		w := want.Corpora[corpus]
		g, ok := got.Corpora[corpus]
		if !ok {
			for fname := range w.Features {
				missing = append(missing, fmt.Sprintf("%s / %s", corpus, fname))
			}
			continue
		}
		featNames := make([]string, 0, len(w.Features))
		for fname := range w.Features {
			featNames = append(featNames, fname)
		}
		sort.Strings(featNames)
		for _, fname := range featNames {
			wf := w.Features[fname]
			gf, ok := g.Features[fname]
			if !ok {
				missing = append(missing, fmt.Sprintf("%s / %s", corpus, fname))
				continue
			}
			if gf.Matched < wf.Matched {
				regressions = append(regressions,
					fmt.Sprintf("%s / %s matched: got %d, baseline %d",
						corpus, fname, gf.Matched, wf.Matched))
			} else if gf.Matched > wf.Matched {
				improvements = append(improvements,
					fmt.Sprintf("%s / %s matched: got %d, baseline %d",
						corpus, fname, gf.Matched, wf.Matched))
			}
			// Compare named metrics (e.g. text feature's
			// chars_match and words_match). Cost metrics invert the
			// direction, and raw inventory totals are informational
			// only.
			r, i, m := diffPerFixture(corpus+" / "+fname, wf, gf)
			regressions = append(regressions, r...)
			improvements = append(improvements, i...)
			missing = append(missing, m...)
			metricNames := make([]string, 0, len(wf.Metrics))
			for mname := range wf.Metrics {
				metricNames = append(metricNames, mname)
			}
			sort.Strings(metricNames)
			for _, mname := range metricNames {
				if uncomparedMetrics[mname] {
					continue
				}
				wv := wf.Metrics[mname]
				gv := gf.Metrics[mname]
				worse := gv < wv
				if costMetrics[mname] {
					worse = gv > wv
				}
				switch {
				case gv == wv:
				case worse:
					regressions = append(regressions,
						fmt.Sprintf("%s / %s / %s: got %d, baseline %d",
							corpus, fname, mname, gv, wv))
				default:
					improvements = append(improvements,
						fmt.Sprintf("%s / %s / %s: got %d, baseline %d",
							corpus, fname, mname, gv, wv))
				}
			}
		}
	}
	return regressions, improvements, missing
}

// diffPerFixture compares the per-fixture outcomes and scores of one cell. A
// fixture that passed in the baseline and does not now is a regression, and
// one that passes now and did not is an improvement. A score below its
// baseline by more than the baseline's tolerance is a regression, one above
// it by more an improvement; a fixture the current run did not score reads
// as 0. A baseline fixture absent from the current run is missing.
func diffPerFixture(cell string, want, got baselineFeature) (regressions, improvements, missing []string) {
	ids := make([]string, 0, len(want.Outcomes))
	for id := range want.Outcomes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		w := want.Outcomes[id]
		g, ok := got.Outcomes[id]
		switch {
		case !ok:
			missing = append(missing, fmt.Sprintf("%s / %s", cell, id))
		case w == outcomePass && g != outcomePass:
			regressions = append(regressions, fmt.Sprintf("%s / %s: %s, baseline pass", cell, id, g))
		case w != outcomePass && g == outcomePass:
			improvements = append(improvements, fmt.Sprintf("%s / %s: pass, baseline %s", cell, id, w))
		}
	}

	ids = ids[:0]
	for id := range want.Scores {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		gs, ok := got.Scores[id]
		if !ok {
			missing = append(missing, fmt.Sprintf("%s / %s", cell, id))
			continue
		}
		ws := want.Scores[id]
		names := make([]string, 0, len(ws))
		for name := range ws {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			wv, gv := ws[name], gs[name]
			switch {
			case gv < wv-want.ScoreTolerance:
				regressions = append(regressions, fmt.Sprintf("%s / %s / %s: got %.4f, baseline %.4f", cell, id, name, gv, wv))
			case gv > wv+want.ScoreTolerance:
				improvements = append(improvements, fmt.Sprintf("%s / %s / %s: got %.4f, baseline %.4f", cell, id, name, gv, wv))
			}
		}
	}
	return regressions, improvements, missing
}
