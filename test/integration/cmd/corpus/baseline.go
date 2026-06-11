package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
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

func runBaseline(args []string) int {
	fs := flag.NewFlagSet("baseline", flag.ExitOnError)
	corpus := fs.String("corpus", "", "Comma-separated corpus names (empty = all)")
	feature := fs.String("feature", "", "Comma-separated feature names (empty = all)")
	output := fs.String("output", defaultBaselinePath(), "Baseline file path to write")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "Parallel fixture workers")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}

	opts := runOptions{
		corpora:  splitCSV(*corpus),
		features: splitCSV(*feature),
		workers:  *workers,
	}
	if opts.workers < 1 {
		opts.workers = 1
	}

	board, ok := buildScoreboard(context.Background(), opts)
	if !ok {
		return 1
	}

	bl := baselineFromScoreboard(board)
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

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	corpus := fs.String("corpus", "", "Comma-separated corpus names (empty = all)")
	feature := fs.String("feature", "", "Comma-separated feature names (empty = all)")
	baselinePath := fs.String("baseline", defaultBaselinePath(), "Baseline file to check against")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "Parallel fixture workers")
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
		corpora:  splitCSV(*corpus),
		features: splitCSV(*feature),
		workers:  *workers,
	}
	if opts.workers < 1 {
		opts.workers = 1
	}

	board, ok := buildScoreboard(context.Background(), opts)
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
			feats[fr.Name] = bf
		}
		if len(feats) > 0 {
			bl.Corpora[c.Name] = baselineCorpus{Features: feats}
		}
	}
	return bl
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
