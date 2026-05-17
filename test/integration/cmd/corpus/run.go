package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// FeatureFactory constructs a fresh [Feature] instance. Each (corpus,
// feature) pair gets its own instance so golden indices stay isolated.
type FeatureFactory func() Feature

// featureRegistry lists every feature the runner knows about. CLI
// --feature values are matched against the returned Feature.Name().
var featureRegistry = []FeatureFactory{
	func() Feature { return newTextFeature() },
	func() Feature { return newMetadataFeature() },
	func() Feature { return newComplianceFeature() },
	func() Feature { return newRemediationFeature(false) },
	func() Feature { return newRemediationFeature(true) },
	func() Feature { return newBookmarksFeature() },
	func() Feature { return newSplitFeature() },
	func() Feature { return newMergeFeature() },
	func() Feature { return newMergeEmptyFeature() },
	func() Feature { return newFacturxFeature() },
	func() Feature { return newSchematronFeature() },
}

// runOptions captures the CLI flags for the run subcommand.
type runOptions struct {
	corpora  []string // explicit corpora filter (empty = all)
	features []string // explicit feature filter (empty = all)
	profile  string   // when non-empty, fixtures not under this profile skip
	format   string   // "text" or "json"
	workers  int      // parallelism
}

func runRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	corpus := fs.String("corpus", "", "Comma-separated corpus names (empty = all)")
	feature := fs.String("feature", "", "Comma-separated feature names (empty = all)")
	profile := fs.String("profile", "", "Filter to fixtures under this profile directory")
	format := fs.String("format", "text", "Output format: text or json")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "Parallel fixture workers")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}

	opts := runOptions{
		corpora:  splitCSV(*corpus),
		features: splitCSV(*feature),
		profile:  *profile,
		format:   *format,
		workers:  *workers,
	}
	if opts.workers < 1 {
		opts.workers = 1
	}

	board, ok := buildScoreboard(context.Background(), opts)
	if !ok {
		return 1
	}

	switch opts.format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(board); err != nil {
			fmt.Fprintf(os.Stderr, "encode: %v\n", err)
			return 1
		}
	default:
		board.PrintText(os.Stdout)
	}
	return 0
}

// buildScoreboard runs the matrix described by opts and returns the
// aggregated [Scoreboard]. Shared between the run, baseline, and
// check subcommands. Returns ok=false when corpus resolution fails;
// per-corpus errors are reported to stderr and the offending corpus
// is omitted from the result.
func buildScoreboard(ctx context.Context, opts runOptions) (*Scoreboard, bool) {
	corpora, err := resolveCorpora(opts.corpora)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return nil, false
	}
	start := time.Now()
	board := &Scoreboard{Corpora: make([]*CorpusReport, 0, len(corpora))}
	for _, name := range corpora {
		report, err := runCorpus(ctx, name, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			continue
		}
		board.Corpora = append(board.Corpora, report)
	}
	board.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return board, true
}

// resolveCorpora returns the corpus names to run. When the filter is
// empty, every corpus with a manifest.json is included.
func resolveCorpora(filter []string) ([]string, error) {
	if len(filter) > 0 {
		return filter, nil
	}
	names, err := ListCorpora()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("no corpora found under test/integration/corpus/")
	}
	return names, nil
}

// selectedFeatures applies the --feature filter against the registry.
func selectedFeatures(filter []string) []Feature {
	var features []Feature
	if len(filter) == 0 {
		for _, ff := range featureRegistry {
			features = append(features, ff())
		}
		return features
	}
	want := make(map[string]bool, len(filter))
	for _, f := range filter {
		want[f] = true
	}
	for _, ff := range featureRegistry {
		inst := ff()
		if want[inst.Name()] {
			features = append(features, inst)
		}
	}
	return features
}

// runCorpus loads the manifest, enumerates fixtures, dispatches each
// fixture through every selected feature in parallel, and returns the
// aggregated [CorpusReport].
func runCorpus(ctx context.Context, name string, opts runOptions) (*CorpusReport, error) {
	manifest, err := LoadManifest(name)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	fixtures, err := LoadFixtures(manifest)
	if err != nil {
		return nil, fmt.Errorf("load fixtures: %w", err)
	}
	if opts.profile != "" {
		fixtures = filterByProfile(fixtures, opts.profile)
	}

	features := selectedFeatures(opts.features)
	if len(features) == 0 {
		return nil, errors.New("no features selected")
	}

	report := &CorpusReport{
		Name:     manifest.Name,
		Fixtures: len(fixtures),
		Features: make([]*FeatureReport, 0, len(features)),
	}

	for _, feat := range features {
		fr := &FeatureReport{
			Name:         feat.Name(),
			GoldenSource: feat.GoldenSource(),
		}
		goldenDir := manifest.GoldenDir(feat.Name())
		if goldenDir == "" {
			fr.NoGolden = true
			report.Features = append(report.Features, fr)
			continue
		}
		if err := feat.LoadGoldens(goldenDir); err != nil {
			if errors.Is(err, ErrNoGoldens) {
				fr.NoGolden = true
			} else {
				fr.LoadError = err.Error()
			}
			report.Features = append(report.Features, fr)
			continue
		}
		if p, ok := feat.(BatchPreparer); ok {
			if err := p.Prepare(ctx, fixtures); err != nil {
				fr.LoadError = fmt.Sprintf("prepare: %v", err)
				report.Features = append(report.Features, fr)
				continue
			}
		}
		runFeatureAcrossFixtures(ctx, feat, fixtures, opts.workers, fr)
		report.Features = append(report.Features, fr)
	}
	return report, nil
}

// runFeatureAcrossFixtures dispatches one feature over every fixture
// (skipping ones with no golden) using a bounded worker pool.
func runFeatureAcrossFixtures(ctx context.Context, feat Feature, fixtures []Fixture, workers int, fr *FeatureReport) {
	work := make(chan Fixture, len(fixtures))
	results := make(chan Verdict, len(fixtures))

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for fx := range work {
				results <- processFixture(ctx, feat, fx)
			}
		})
	}

	for _, fx := range fixtures {
		if fx.SkipReason != "" {
			fr.Skipped++
			continue
		}
		if !feat.HasGolden(fx.ID) {
			fr.NoGoldenFixtures++
			continue
		}
		work <- fx
	}
	close(work)

	go func() {
		wg.Wait()
		close(results)
	}()

	for v := range results {
		fr.Verdicts = append(fr.Verdicts, v)
		switch {
		case v.Skipped:
			fr.Skipped++
		case v.Match:
			fr.Matched++
			for k, val := range v.Metrics {
				if fr.Metrics == nil {
					fr.Metrics = make(map[string]int)
				}
				fr.Metrics[k] += val
			}
		default:
			fr.Mismatched++
			for k, val := range v.Metrics {
				if fr.Metrics == nil {
					fr.Metrics = make(map[string]int)
				}
				fr.Metrics[k] += val
			}
		}
	}

	sort.Slice(fr.Verdicts, func(i, j int) bool {
		return fr.Verdicts[i].FixtureID < fr.Verdicts[j].FixtureID
	})
}

// processFixture runs one feature against one fixture under a timeout
// and a panic guard. Panics degrade to a non-match Verdict so a single
// crashing fixture does not abort the corpus run.
func processFixture(ctx context.Context, feat Feature, fx Fixture) (v Verdict) {
	v = Verdict{FixtureID: fx.ID}
	defer func() {
		if r := recover(); r != nil {
			v.Match = false
			v.Reason = fmt.Sprintf("panic: %v", r)
		}
	}()

	fctx, cancel := context.WithTimeout(ctx, perFileTimeout)
	defer cancel()

	output, err := feat.Run(fctx, fx.PDFPath, fx.ID)
	if err != nil {
		v.Reason = err.Error()
		return v
	}
	if fctx.Err() != nil {
		v.Reason = "timeout"
		return v
	}
	return feat.Compare(fx.ID, output)
}

func filterByProfile(fixtures []Fixture, profile string) []Fixture {
	var out []Fixture
	prefix := profile + "/"
	for _, fx := range fixtures {
		if strings.HasPrefix(fx.ID, prefix) {
			out = append(out, fx)
		}
	}
	return out
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
