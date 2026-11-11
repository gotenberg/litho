package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

// runPrepare brings the selected corpora to a state run can score: it runs,
// in order, every step a corpus needs that is missing or stale (see
// [prepare]). make corpus, corpus-check and corpus-baseline run it first.
func runPrepare(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("prepare", flag.ExitOnError)
	corpus := fs.String("corpus", "", corpusFlagUsage)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}
	ms, err := selectManifests(splitCSV(*corpus))
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepare: %v\n", err)
		return 1
	}
	if err := prepare(ctx, ms); err != nil {
		fmt.Fprintf(os.Stderr, "prepare: %v\n", err)
		return 1
	}
	return 0
}

// prepare runs the four steps a corpus goes through, each skipping what is
// already done:
//
//  1. fetch: the fixture fonts and the files of the fetched corpora, each
//     downloaded unless it is there with its listed hash;
//  2. build: the Office documents the build corpora zip into the testdata,
//     rebuilt every time since a build takes seconds; the builds that write
//     committed files run only through corpus build;
//  3. references: the references of the corpora that have some, those
//     missing or built from another source, options or image (see
//     [referenceProblems]), in one Gotenberg container, and the record of
//     the sources that set litho's own options, which asks no engine;
//  4. goldens: the goldens of the corpora that lack them, after removing
//     the ones made from references since rebuilt or from committed PDFs
//     since changed (see [dropStaleGoldens]).
func prepare(ctx context.Context, ms []*Manifest) error {
	if err := fetch(ctx, ms, true, fetchJobs, http.DefaultClient); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	// Only the builds that write into the testdata: a build that rewrites
	// committed files (the litho/text PDFs, converted by the current litho)
	// is run on purpose, with corpus build, never as a side effect of a run.
	var testdataBuilds []*Manifest
	for _, m := range ms {
		if isBuildCorpus(m) || hasFlatPapers(m) {
			testdataBuilds = append(testdataBuilds, m)
		}
	}
	if err := build(ctx, testdataBuilds); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	pins, err := loadPins()
	if err != nil {
		return fmt.Errorf("loading the pins: %w", err)
	}
	if err := prepareReferences(ctx, ms, pins); err != nil {
		return fmt.Errorf("references: %w", err)
	}
	for _, n := range pins.Notices {
		fmt.Printf("versions: %s\n", n)
	}
	for _, m := range ms {
		if err := dropStaleGoldens(m, pins); err != nil {
			return fmt.Errorf("%s: %w", m.Name, err)
		}
		if err := goldensCorpus(ctx, m, pins); err != nil {
			return fmt.Errorf("goldens: %s: %w", m.Name, err)
		}
	}
	return nil
}

// prepareReferences builds the references of ms that are missing or stale,
// starting the Gotenberg container only when one is.
func prepareReferences(ctx context.Context, ms []*Manifest, pins *resolvedPins) error {
	type pending struct {
		m    *Manifest
		only []string
	}
	var todo []pending
	for _, m := range ms {
		if m.References == nil {
			continue
		}
		fixtures, err := loadInputFixtures(m)
		if err != nil {
			return fmt.Errorf("%s: %w", m.Name, err)
		}
		if !microsoftSignInAvailable() {
			var unbuilt []string
			if fixtures, unbuilt = withoutUnbuiltMicrosoftReferences(m, m.TestdataRoot(), fixtures); len(unbuilt) > 0 {
				fmt.Println(unbuiltMicrosoftNotice(m, unbuilt))
			}
		}
		check, err := referenceProblems(m, m.TestdataRoot(), fixtures, pins.Values)
		switch {
		case err != nil:
			todo = append(todo, pending{m, nil})
		case len(check.rebuild) > 0:
			todo = append(todo, pending{m, check.rebuild})
		default:
			fmt.Printf("%s: references up to date\n", m.Name)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	b := &referenceBuilders{pins: pins}
	defer b.close()
	for _, p := range todo {
		if err := b.build(ctx, p.m, p.only); err != nil {
			return fmt.Errorf("%s: %w", p.m.Name, err)
		}
	}
	return nil
}

// dropStaleGoldens removes what a corpus's goldens step made from inputs
// that changed since, so the step derives it again: the provisioned PDFs and
// the diff goldens of a corpus whose references were rebuilt, and the
// provisioned PDFs and every golden of a corpus whose committed PDFs differ
// from their provisioned copy (a litho/pdf or litho/text fixture rebuilt,
// added or removed).
func dropStaleGoldens(m *Manifest, pins *resolvedPins) error {
	root := m.TestdataRoot()
	if !provisioned(root, m) {
		return nil
	}
	dirs := []string{filepath.Join(root, "pdfs")}
	switch {
	case m.References != nil:
		fixtures, err := loadInputFixtures(m)
		if err != nil {
			return fmt.Errorf("%s: %w", m.Name, err)
		}
		check, err := referenceProblems(m, root, fixtures, pins.Values)
		if err != nil || len(check.regolden) == 0 {
			return nil //nolint:nilerr // an unreadable reference manifest leaves the goldens step to skip the corpus
		}
		fmt.Printf("%s: %d references changed since the goldens were made: deriving them again\n", m.Name, len(check.regolden))
		// Only the PDFs and the diff goldens come from the references:
		// another feature's golden path may name inputs (robustness reads
		// the built documents).
		if rel, ok := m.Goldens["diff"]; ok {
			dirs = append(dirs, filepath.Join(root, filepath.FromSlash(rel)))
		}
	case m.CommittedPDFs != "":
		changed, err := committedPDFsChanged(m, root)
		if err != nil {
			return fmt.Errorf("%s: %w", m.Name, err)
		}
		if len(changed) == 0 {
			return nil
		}
		fmt.Printf("%s: %s changed since the goldens were made: deriving them again\n", m.Name, summarize(changed))
		for _, rel := range m.Goldens {
			dirs = append(dirs, filepath.Join(root, filepath.FromSlash(rel)))
		}
	default:
		return nil
	}
	for _, d := range dirs {
		if err := os.RemoveAll(d); err != nil {
			return fmt.Errorf("removing stale goldens: %w", err)
		}
	}
	return nil
}

// committedPDFsChanged names the committed PDFs of m whose provisioned copy
// under root differs from them, is missing, or has no committed PDF left.
func committedPDFsChanged(m *Manifest, root string) ([]string, error) {
	sums := func(dir string) (map[string]string, error) {
		pdfs, err := topPDFs(dir, false)
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", dir, err)
		}
		out := make(map[string]string, len(pdfs))
		for _, p := range pdfs {
			if out[filepath.Base(p)], err = fileSHA256(p); err != nil {
				return nil, fmt.Errorf("hashing %s: %w", p, err)
			}
		}
		return out, nil
	}
	want, err := sums(filepath.Join(m.SourceDir(), filepath.FromSlash(m.CommittedPDFs)))
	if err != nil {
		return nil, err
	}
	got, err := sums(filepath.Join(root, "pdfs"))
	if err != nil {
		return nil, err
	}
	var changed []string
	for name, sum := range want {
		if got[name] != sum {
			changed = append(changed, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	return changed, nil
}
