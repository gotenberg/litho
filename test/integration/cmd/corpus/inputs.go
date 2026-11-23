package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// errInputsUnfetched reports a converter corpus whose sources are listed but
// not on disk: the corpus's make target has not run.
var errInputsUnfetched = errors.New("sources not provisioned")

// loadInputFixtures lists the fixtures of a converter corpus from its
// [InputSpec], and the paper documents it builds from its committed sources
// (see [hasFlatPapers]), in ID order, with the manifest's skips applied.
func loadInputFixtures(m *Manifest) ([]Fixture, error) {
	in := m.Inputs
	if in.List == "" && in.Glob == "" && in.Build == "" {
		return nil, errors.New("manifest inputs must set list, glob or build")
	}
	var fixtures []Fixture
	if in.Glob != "" {
		globbed, err := globInputFixtures(m.SourceDir(), in.Glob)
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, globbed...)
	}
	if in.List != "" {
		listed, err := listInputFixtures(filepath.Join(m.SourceDir(), in.List), filepath.Join(m.TestdataRoot(), filepath.FromSlash(in.ListRoot)), prepareRemedy(m.Name))
		if err != nil {
			return nil, err
		}
		if err := checkImportedOptions(m, listed); err != nil {
			return nil, err
		}
		fixtures = append(fixtures, listed...)
	}
	if in.Build != "" {
		built, err := builtInputFixtures(m)
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, built...)
	}
	papers, err := flatPaperFixtures(m)
	if err != nil {
		return nil, err
	}
	fixtures = append(fixtures, papers...)
	seen := make(map[string]bool, len(fixtures))
	for i := range fixtures {
		id := fixtures[i].ID
		if seen[id] {
			return nil, fmt.Errorf("fixture %q is named twice by the inputs", id)
		}
		seen[id] = true
		fixtures[i].SkipReason = m.Skips[id]
	}
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	return fixtures, nil
}

// listInputFixtures reads a committed list of {"id": ...} objects; each
// fixture's source is the file at its ID under root. A listed source missing
// on disk fails the load with the command that fetches it, remedy, rather
// than scoring every fixture as an error.
func listInputFixtures(listPath, root, remedy string) ([]Fixture, error) {
	data, err := os.ReadFile(listPath) //nolint:gosec // committed corpus list
	if err != nil {
		return nil, fmt.Errorf("read input list: %w", err)
	}
	var entries []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode input list %s: %w", listPath, err)
	}
	fixtures := make([]Fixture, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !validInputID(e.ID) {
			return nil, fmt.Errorf("input list %s: invalid id %q", listPath, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("input list %s: duplicate id %q", listPath, e.ID)
		}
		seen[e.ID] = true
		p := filepath.Join(root, filepath.FromSlash(e.ID))
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("%w: %s is missing (run %s)", errInputsUnfetched, p, remedy)
		}
		fixtures = append(fixtures, Fixture{ID: e.ID, PDFPath: p})
	}
	return fixtures, nil
}

// importedOptionsDir is the directory of a list corpus's source directory
// that holds the options the corpus keeps for some of its imported
// documents: one directory a fixture, named by its ID with "/" written "__"
// ([reportName]), holding an options.json and the expected text file it
// names ([fixture.LoadImportedOffice]). The documents are fetched and never
// committed, and corpus update rewrites the lists alone, so the options
// live beside the lists.
const importedOptionsDir = "options"

// ImportedOptions returns the directory of the options the corpus keeps for
// its listed fixture id, "" when it keeps none.
func (m *Manifest) ImportedOptions(id string) string {
	if m.Inputs == nil || m.Inputs.List == "" {
		return ""
	}
	dir := filepath.Join(m.SourceDir(), importedOptionsDir, reportName(id))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// checkImportedOptions fails when the options directory of a list corpus
// names a fixture its list does not, as when corpus update drops a
// document: the options would judge nothing and say nothing.
func checkImportedOptions(m *Manifest, listed []Fixture) error {
	entries, err := os.ReadDir(filepath.Join(m.SourceDir(), importedOptionsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	names := make(map[string]bool, len(listed))
	for _, f := range listed {
		names[reportName(f.ID)] = true
	}
	for _, e := range entries {
		if !e.IsDir() || !names[e.Name()] {
			return fmt.Errorf("%s/%s/%s: no fixture of %s is named so", m.Name, importedOptionsDir, e.Name(), m.Inputs.List)
		}
	}
	return nil
}

// validInputID reports whether id is a clean relative slash path that stays
// inside the directory it is joined to.
func validInputID(id string) bool {
	if id == "" || strings.HasPrefix(id, "/") || strings.Contains(id, `\`) {
		return false
	}
	for seg := range strings.SplitSeq(id, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// globInputFixtures matches pattern, which holds exactly one "*", under dir;
// each match is one fixture keyed on what the "*" matched.
func globInputFixtures(dir, pattern string) ([]Fixture, error) {
	prefix, suffix, ok := strings.Cut(pattern, "*")
	if !ok || strings.Contains(suffix, "*") || strings.ContainsAny(pattern, "?[") {
		return nil, fmt.Errorf("input glob %q must hold exactly one * and no other wildcard", pattern)
	}
	matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pattern)))
	if err != nil {
		return nil, fmt.Errorf("input glob %q: %w", pattern, err)
	}
	fixtures := make([]Fixture, 0, len(matches))
	for _, p := range matches {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil, fmt.Errorf("input glob %q: %w", pattern, err)
		}
		rel = filepath.ToSlash(rel)
		id := strings.TrimSuffix(strings.TrimPrefix(rel, prefix), suffix)
		if id == "" || strings.Contains(id, "/") {
			continue
		}
		fixtures = append(fixtures, Fixture{ID: id, PDFPath: p})
	}
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("input glob %q matches nothing under %s", pattern, dir)
	}
	return fixtures, nil
}
